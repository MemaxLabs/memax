package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/cache"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/decay"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/distill"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/expand"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/intent"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type RecallHandler struct {
	store       store.Store
	embedder    embed.Embedder
	distiller   *distill.Distiller
	reranker    rerank.Reranker
	cache       cache.Cache
	cacheTTL    time.Duration
	events      events.Publisher
	canRerankFn func(ownerID string, source string) bool
}

// WithEvents installs an events.Publisher so recall emits a
// coalesced `agent.changed` activity signal on success, keeping
// Settings → Your Agents "last observed" live across tabs/devices
// without refresh. Optional — recall remains fully functional on
// nil; the coalesce helper no-ops cleanly.
func (h *RecallHandler) WithEvents(p events.Publisher) *RecallHandler {
	h.events = p
	return h
}

func NewRecallHandler(s store.Store, embedder embed.Embedder, distiller *distill.Distiller, reranker rerank.Reranker, cacheClient cache.Cache) *RecallHandler {
	initRecallTelemetry()
	return &RecallHandler{
		store:     s,
		embedder:  embedder,
		distiller: distiller,
		reranker:  reranker,
		cache:     cacheClient,
		cacheTTL:  time.Duration(recallCacheTTLSeconds()) * time.Second,
		canRerankFn: func(string, string) bool {
			return true
		},
	}
}

type recallStage struct {
	name  string
	start time.Time
	span  trace.Span
}

func startRecallStage(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, recallStage) {
	if recallTracer == nil {
		recallTracer = otel.Tracer("memax.recall")
	}
	stageAttrs := append([]attribute.KeyValue{attribute.String("recall.stage", name)}, attrs...)
	ctx, span := recallTracer.Start(ctx, "recall."+name, trace.WithAttributes(stageAttrs...))
	return ctx, recallStage{name: name, start: time.Now(), span: span}
}

func finishRecallStage(ctx context.Context, stage recallStage, err error, attrs ...attribute.KeyValue) {
	stageAttrs := append([]attribute.KeyValue{attribute.String("recall.stage", stage.name)}, attrs...)
	recallStageHistogram.Record(ctx, time.Since(stage.start).Seconds(), metric.WithAttributes(stageAttrs...))
	stage.span.SetAttributes(attrs...)
	if err != nil {
		stage.span.RecordError(err)
		stage.span.SetStatus(codes.Error, err.Error())
	} else {
		stage.span.SetStatus(codes.Ok, "")
	}
	stage.span.End()
}

func (h *RecallHandler) SetRerankPolicy(fn func(ownerID string, source string) bool) {
	if fn == nil {
		h.canRerankFn = func(string, string) bool { return true }
		return
	}
	h.canRerankFn = fn
}

type scoredChunk struct {
	chunk  model.Chunk
	memory *model.Memory
	score  float64
}

const (
	defaultRecallLimit      = 10
	defaultCandidatePool    = 60
	maxCandidatePool        = 120
	candidatePoolMultiplier = 6
	minAbsoluteRecallScore  = 0.012

	// Adaptive filtering: when all results have low absolute scores
	// (indicating no strong match), use a tighter relative threshold
	// to limit weak/tangential results.
	weakResultCeiling    = 0.04 // top score below this triggers tighter filtering
	tightRelativeFilter  = 0.40 // normalized threshold when results are weak
	normalRelativeFilter = 0.15 // normalized threshold when results are strong
)

var (
	recallTelemetryOnce       sync.Once
	recallTracer              trace.Tracer
	recallDurationHistogram   metric.Float64Histogram
	recallStageHistogram      metric.Float64Histogram
	recallCandidatesHistogram metric.Int64Histogram
	recallResultsHistogram    metric.Int64Histogram
	recallRequestsCounter     metric.Int64Counter
	recallCacheHitsCounter    metric.Int64Counter
	recallRerankCounter       metric.Int64Counter
	recallErrorsCounter       metric.Int64Counter
)

const (
	cacheReadTimeout  = 100 * time.Millisecond
	cacheWriteTimeout = 200 * time.Millisecond
	queryEmbeddingTTL = 5 * time.Minute
)

func initRecallTelemetry() {
	recallTelemetryOnce.Do(func() {
		recallTracer = otel.Tracer("memax.recall")
		meter := otel.Meter("memax.recall")
		recallDurationHistogram, _ = meter.Float64Histogram(
			"memax.recall.duration",
			metric.WithDescription("Total recall pipeline duration"),
			metric.WithUnit("s"),
		)
		recallStageHistogram, _ = meter.Float64Histogram(
			"memax.recall.stage.duration",
			metric.WithDescription("Recall stage duration by stage name"),
			metric.WithUnit("s"),
		)
		recallCandidatesHistogram, _ = meter.Int64Histogram(
			"memax.recall.candidates",
			metric.WithDescription("Recall candidate count before local scoring"),
		)
		recallResultsHistogram, _ = meter.Int64Histogram(
			"memax.recall.results",
			metric.WithDescription("Recall result count after normalization"),
		)
		recallRequestsCounter, _ = meter.Int64Counter(
			"memax.recall.requests",
			metric.WithDescription("Recall pipeline requests"),
		)
		recallCacheHitsCounter, _ = meter.Int64Counter(
			"memax.recall.cache_hits",
			metric.WithDescription("Recall cache hits"),
		)
		recallRerankCounter, _ = meter.Int64Counter(
			"memax.recall.reranks",
			metric.WithDescription("Recall rerank applications"),
		)
		recallErrorsCounter, _ = meter.Int64Counter(
			"memax.recall.errors",
			metric.WithDescription("Recall pipeline errors"),
		)
	})
}

func (h *RecallHandler) RunPipeline(ctx context.Context, query string, source string, workingDir string, projectContext map[string]string, limit int, ownerID string, filters *model.SearchFilters, scope RecallScope) ([]model.RecalledMemory, model.QueryMetadata, error) {
	start := time.Now()
	if ctx == nil {
		ctx = context.Background()
	}
	// Drop frozen hubs before anything downstream sees the scope.
	// Centralized here so every RunPipeline caller (REST recall, ask,
	// MCP memax_recall) gets the same enforcement without each having
	// to remember to filter. The filter is a single SQL in the hot
	// path and fail-open on error.
	scope = stripFrozenHubsFromScope(ctx, h.store, scope)
	hubIDs := scope.HubIDs
	ctx = anthropic.WithTracking(ctx, anthropic.Tracking{
		DistinctID: ownerID,
		Metadata: map[string]any{
			"owner_id":            ownerID,
			"hub_ids":             hubIDs,
			"retrieval_boost_hub": scope.ActiveBoostHubID,
			"llm_flow":            "recall",
			"source":              source,
			"query_size":          len(query),
		},
	})
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if source == "" {
		source = "api"
	}
	ctx, span := recallTracer.Start(ctx, "memax.recall", trace.WithAttributes(
		attribute.String("memax.recall.source", source),
		attribute.Int("memax.recall.limit", limit),
		attribute.Int("memax.recall.query_length", len(query)),
		attribute.Int("memax.recall.hub_count", len(hubIDs)),
	))
	defer func() {
		recallRequestsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("memax.recall.source", source)))
		recallDurationHistogram.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("memax.recall.source", source)))
		span.End()
	}()

	cacheCtx, cacheStage := startRecallStage(ctx, "cache.lookup")
	if cached, ok := h.getCachedResult(cacheCtx, query, workingDir, projectContext, limit, ownerID, scope); ok {
		recallCacheHitsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("memax.recall.source", source)))
		finishRecallStage(cacheCtx, cacheStage, nil, attribute.Bool("memax.recall.cache_hit", true))
		span.SetAttributes(
			attribute.Bool("memax.recall.cache_hit", true),
			attribute.Int("memax.recall.result_count", len(cached.Memories)),
		)
		cached.QueryMetadata.LatencyMs = time.Since(start).Milliseconds()
		go h.reinforceResults(cached.Memories, ownerID, hubIDs)
		return cached.Memories, cached.QueryMetadata, nil
	}
	finishRecallStage(cacheCtx, cacheStage, nil, attribute.Bool("memax.recall.cache_hit", false))
	span.SetAttributes(attribute.Bool("memax.recall.cache_hit", false))

	// --- Phase 1: Query Understanding + Embedding (parallel) ---
	// Fetch user context for the distiller (name + timezone)
	userName, userTZ := "", "UTC"
	if user, err := h.store.GetUser(ownerID); err == nil && user != nil {
		userName = user.Name
	}
	// Timezone: prefer client header (X-Timezone), fall back to user preferences
	if tz, ok := ctx.Value(timezoneKey).(string); ok && tz != "" {
		userTZ = tz
	} else if prefs, err := h.store.GetUserPreferences(ownerID); err == nil && prefs != nil {
		if tz, ok := prefs.Settings["timezone"].(string); ok && tz != "" {
			userTZ = tz
		}
	}

	// Run distiller and embedder in parallel — hides distiller latency behind embedding
	var distillResult *distill.Result
	var queryEmbedding []float64
	// Embedding for the distilled query when the distiller rewrote it —
	// nil means "same as raw" (single-query mode).
	var distilledEmbedding []float64
	now := time.Now()

	var parallelWg sync.WaitGroup
	if h.distiller != nil {
		parallelWg.Go(func() {
			distillCtx, distillStage := startRecallStage(ctx, "distill.query")
			distillResult = h.distiller.DistillContext(distillCtx, query, source, workingDir, now, userName, userTZ)
			finishRecallStage(distillCtx, distillStage, nil)
		})
	}

	// Embed raw query in parallel; long/noisy inputs may be re-embedded after distillation.
	if h.embedder != nil {
		parallelWg.Go(func() {
			embedCtx, embedStage := startRecallStage(ctx, "embed.query")
			embeddings, err := h.embedQuery(embedCtx, query)
			if err != nil {
				finishRecallStage(embedCtx, embedStage, err)
				slog.Warn("query embedding failed, falling back to keyword search", "error", err)
			} else if len(embeddings) > 0 {
				queryEmbedding = embeddings[0]
				finishRecallStage(embedCtx, embedStage, nil, attribute.Bool("memax.recall.has_embedding", true))
			} else {
				finishRecallStage(embedCtx, embedStage, nil, attribute.Bool("memax.recall.has_embedding", false))
			}
		})
	}

	parallelWg.Wait()

	// --- Phase 2: Apply distiller results ---
	searchQuery := query
	if distillResult != nil {
		slog.Info("query understood",
			"original_len", len(query),
			"distilled", distillResult.Query,
			"keywords", distillResult.Keywords,
			"confidence", distillResult.Confidence,
			"has_filters", distillResult.Filters != nil,
		)
		if distillResult.Query != "" {
			searchQuery = distillResult.Query
		} else if distillResult.Confidence >= 0.7 && distillResult.Filters != nil {
			// High-confidence empty query = no topical constraint (e.g., "What did John push in May?").
			// Use filter terms only instead of the noisy raw query.
			searchQuery = ""
		}
		if len(distillResult.Keywords) > 0 {
			if searchQuery != "" {
				searchQuery = searchQuery + " " + strings.Join(distillResult.Keywords, " ")
			} else {
				searchQuery = strings.Join(distillResult.Keywords, " ")
			}
		}
		if h.embedder != nil && shouldEmbedDistilledQuery(query, searchQuery, distillResult.Confidence) {
			embedCtx, embedStage := startRecallStage(ctx, "embed.distilled_query")
			if embeddings, err := h.embedQuery(embedCtx, searchQuery); err != nil {
				finishRecallStage(embedCtx, embedStage, err)
				slog.Warn("distilled query embedding failed, keeping raw query embedding", "error", err)
			} else if len(embeddings) > 0 && embeddings[0] != nil {
				// The distilled embedding used to OVERWRITE the raw one
				// here — combined with searchQuery being the distilled
				// text, that made distillation the ONLY retrieval
				// channel, and every entity it dropped (Gracery, Kin
				// Khao, knee — eval p1/p3/p6) was unrecoverable. The
				// raw query is now its own always-on retrieval lane
				// (research-validated: rewriting degrades retrieval up
				// to ~9% by replacing domain terms, arXiv:2603.13301),
				// so the distilled embedding lands in its own variable.
				distilledEmbedding = embeddings[0]
				finishRecallStage(embedCtx, embedStage, nil, attribute.Bool("memax.recall.distilled_embedding", true))
			} else {
				finishRecallStage(embedCtx, embedStage, nil, attribute.Bool("memax.recall.distilled_embedding", false))
			}
		}

		// Merge distiller filters into search filters
		if distillResult.Filters != nil {
			if filters == nil {
				filters = &model.SearchFilters{}
			}
			df := distillResult.Filters
			if df.Temporal != nil && filters.TemporalStart == nil {
				if start, end, ok := parseTemporalBounds(df.Temporal.Start, df.Temporal.End, userTZ); ok {
					filters.TemporalStart = &start
					filters.TemporalEnd = &end
				}
			}
			if len(df.People) > 0 && len(filters.People) == 0 {
				filters.People = df.People
			}
			if len(df.Authors) > 0 && len(filters.Authors) == 0 {
				filters.Authors = df.Authors
			}
			if df.Kind != "" && filters.Kind == "" {
				filters.Kind = model.NormalizeMemoryKind(df.Kind)
			}
			if df.Source != "" && filters.Source == "" {
				filters.Source = df.Source
			}
			if df.Hub != "" && filters.Hub == "" {
				filters.Hub = df.Hub
			}
		}
	}
	if filters != nil {
		searchQuery = appendStructuredFilterTerms(searchQuery, filters)
	}

	// Preserve entity-like terms from the raw query that the distiller
	// dropped (hub slugs, repo paths, source agents). These match
	// metadata_text in FTS/trigram lanes but aren't useful for vector search.
	// The embedding was already computed above from the distilled query.
	if entityTerms := preserveRawEntityTerms(query, searchQuery); len(entityTerms) > 0 {
		searchQuery = searchQuery + " " + strings.Join(entityTerms, " ")
	}

	// The RAW query is a retrieval channel of its own (A1). Structured
	// filter terms still apply — they narrow, not rewrite. When the
	// distiller didn't change anything (or didn't run), raw and
	// distilled collapse to one query and search runs once, as before.
	rawSearchQuery := query
	if filters != nil {
		rawSearchQuery = appendStructuredFilterTerms(rawSearchQuery, filters)
	}
	// EqualFold: a case-only distiller rewrite is not a different
	// query — firing a second five-lane search for it is pure waste
	// (adversarial review finding 3).
	dualQuery := distillResult != nil && !strings.EqualFold(searchQuery, rawSearchQuery)

	// --- Phase 3: Intent classification (microseconds, regex fast-path) ---
	intentCtx, intentStage := startRecallStage(ctx, "intent.classify")
	intentResult := intent.Classify(searchQuery)
	if distillResult != nil && distillResult.InformationNeed != "" && distillResult.InformationNeed != "general" {
		intentResult.Intent = intent.FromDistillerNeed(distillResult.InformationNeed)
	}
	finishRecallStage(intentCtx, intentStage, nil, attribute.String("memax.recall.intent", string(intentResult.Intent)))

	slog.Info("intent classified",
		"intent", intentResult.Intent,
		"kind_hints", intentResult.KindHints,
		"recency", intentResult.RecencyPreference,
	)

	// Merge intent classifier temporal bounds as fallback (when distiller is nil or missed it)
	if intentResult.RecencyPreference == "temporal" && intentResult.TemporalStart != nil {
		if filters == nil {
			filters = &model.SearchFilters{}
		}
		if filters.TemporalStart == nil {
			filters.TemporalStart = intentResult.TemporalStart
			filters.TemporalEnd = intentResult.TemporalEnd
		}
	}

	// Resolve hub filter to hub ID for search scoping.
	// The distiller may extract hub filters from product/project names that
	// happen to match a hub slug. shouldApplyHubFilter gates this with
	// explicit intent detection so ambiguous names don't narrow search.
	if filters != nil && filters.Hub != "" {
		if hub, err := h.store.GetHubBySlug(filters.Hub); err == nil {
			if role, _ := h.store.GetHubMemberRole(hub.ID, ownerID); role != "" {
				if shouldApplyHubFilter(query, filters.Hub) {
					hubIDs = []string{hub.ID}
					slog.Info("hub filter applied",
						"hub_slug", filters.Hub, "hub_id", hub.ID)
				} else {
					slog.Info("hub filter suppressed — no explicit hub intent",
						"hub_slug", filters.Hub, "raw_query", query)
				}
			}
		}
	}

	// --- Phase 4: Query expansion + search ---
	expandCtx, expandStage := startRecallStage(ctx, "query.expand")
	expansion := expand.Expand(searchQuery)
	if len(expansion.Reformulations) > 0 {
		searchQuery = searchQuery + " " + expansion.Reformulations[0]
	}
	if len(expansion.Keywords) > 0 {
		searchQuery = searchQuery + " " + strings.Join(expansion.Keywords, " ")
	}
	finishRecallStage(expandCtx, expandStage, nil,
		attribute.Int("memax.recall.reformulation_count", len(expansion.Reformulations)),
		attribute.Int("memax.recall.cross_lingual_terms", len(expansion.Keywords)))

	// Compute field terms for title-matching search lane.
	// Excludes hub slugs/names (non-discriminating) and short tokens.
	fieldTerms := h.discriminatingFieldTerms(searchQuery, hubIDs)
	var searchOpts *store.SearchOptions
	if len(fieldTerms) > 0 {
		searchOpts = &store.SearchOptions{
			FieldTerms:  fieldTerms,
			ProjectRepo: projectContext["repo"],
		}
		slog.Info("field lane active", "terms", fieldTerms, "project_repo", projectContext["repo"])
	}

	var (
		matchedChunks []model.Chunk
		err           error
	)
	candidateLimit := recallCandidateLimit(limit)
	searchCtx, searchStage := startRecallStage(ctx, "search.hybrid",
		attribute.Int("memax.recall.candidate_limit", candidateLimit),
		attribute.Bool("memax.recall.dual_query", dualQuery))
	// One search variant per access scope; the scope selection is
	// UNCHANGED from the single-query code (the strict-scope branches
	// are the load-bearing leak fixes from plan-24 step 5b / commit
	// d6324ab5 — see the case comments in git history). The closure
	// exists so raw and distilled queries run through the SAME scope
	// decision — a scoped principal must not gain a broader raw lane.
	runSearch := func(q string, emb []float64) ([]model.Chunk, error) {
		switch {
		case scope.Strict && len(hubIDs) > 0:
			return h.store.SearchChunksInHubs(searchCtx, q, emb, hubIDs, candidateLimit, filters, searchOpts)
		case scope.Strict && len(hubIDs) == 0:
			// Empty strict scope MUST NOT fall through to owner-only
			// search — that would leak the user's full corpus.
			return nil, nil
		case len(hubIDs) > 0:
			return h.store.SearchChunksForHubs(searchCtx, q, emb, ownerID, hubIDs, candidateLimit, filters, searchOpts)
		default:
			return h.store.SearchChunks(searchCtx, q, emb, ownerID, candidateLimit, filters, searchOpts)
		}
	}
	if dualQuery {
		// A1: raw and distilled are independent result sets, fused
		// rank-based so neither channel's scores dominate. Run in
		// parallel — each is its own five-lane store search.
		distEmb := distilledEmbedding
		if distEmb == nil {
			distEmb = queryEmbedding
		}
		var rawChunks, distilledChunks []model.Chunk
		var rawErr, distilledErr error
		var dualWg sync.WaitGroup
		dualWg.Add(2)
		go func() {
			defer dualWg.Done()
			rawChunks, rawErr = runSearch(rawSearchQuery, queryEmbedding)
		}()
		go func() {
			defer dualWg.Done()
			distilledChunks, distilledErr = runSearch(searchQuery, distEmb)
		}()
		dualWg.Wait()
		switch {
		case rawErr != nil && distilledErr != nil:
			slog.Warn("both recall channels failed",
				"raw_error", rawErr, "distilled_error", distilledErr)
			err = rawErr
		case rawErr != nil:
			// One healthy channel beats a failed recall — degrade to
			// the surviving result set and log the dead lane.
			slog.Warn("raw-query search failed, using distilled channel only", "error", rawErr)
			matchedChunks = distilledChunks
		case distilledErr != nil:
			slog.Warn("distilled-query search failed, using raw channel only", "error", distilledErr)
			matchedChunks = rawChunks
		default:
			matchedChunks = fuseDualQueryChunks(rawChunks, distilledChunks, candidateLimit)
			slog.Info("dual-query fusion",
				"raw_count", len(rawChunks),
				"distilled_count", len(distilledChunks),
				"fused_count", len(matchedChunks),
			)
		}
	} else {
		matchedChunks, err = runSearch(searchQuery, queryEmbedding)
	}
	if err != nil {
		finishRecallStage(searchCtx, searchStage, err)
		recallErrorsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("memax.recall.stage", "search.hybrid")))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, model.QueryMetadata{}, fmt.Errorf("search: %w", err)
	}
	finishRecallStage(searchCtx, searchStage, nil, attribute.Int("memax.recall.candidate_count", len(matchedChunks)))
	recallCandidatesHistogram.Record(ctx, int64(len(matchedChunks)), metric.WithAttributes(attribute.String("memax.recall.source", source)))

	queryTerms := tokenizeForMatching(searchQuery)
	identifierTerms := extractIdentifierTerms(searchQuery)
	topicsCtx, topicsStage := startRecallStage(ctx, "topics.load")
	visibilityScope := store.VisibilityScope{OwnerID: ownerID, HubIDs: hubIDs}
	memoryIDs := uniqueChunkMemoryIDs(matchedChunks)
	var (
		topicNamesByMemory map[string]string
		topicIDsByMemory   map[string]string
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		topicNamesByMemory, _ = h.store.GetMemoryTopicNameMapForMemories(visibilityScope, memoryIDs)
	}()
	go func() {
		defer wg.Done()
		topicIDsByMemory, _ = h.store.GetMemoryTopicIDMapForMemories(visibilityScope, memoryIDs)
	}()
	wg.Wait()
	finishRecallStage(topicsCtx, topicsStage, nil, attribute.Int("memax.recall.topic_name_memory_count", len(topicNamesByMemory)))
	accessibleMemories, err := h.store.GetAccessibleMemories(memoryIDs, ownerID, hubIDs)
	if err != nil {
		recallErrorsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("memax.recall.stage", "score.local")))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, model.QueryMetadata{}, fmt.Errorf("load candidate memories: %w", err)
	}

	scoreCtx, scoreStage := startRecallStage(ctx, "score.local")
	scored := make([]scoredChunk, 0, len(matchedChunks))
	for _, chunk := range matchedChunks {
		memory, ok := accessibleMemories[chunk.MemoryID]
		if !ok {
			continue
		}
		sc, ok := h.scoreChunk(chunk, memory, workingDir, projectContext, queryTerms, identifierTerms, *intentResult, topicNamesByMemory, filters, scope.ActiveBoostHubID)
		if !ok {
			continue
		}
		scored = append(scored, sc)
	}
	finishRecallStage(scoreCtx, scoreStage, nil, attribute.Int("memax.recall.scored_count", len(scored)))

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].chunk.MemoryID != scored[j].chunk.MemoryID {
			return scored[i].chunk.MemoryID < scored[j].chunk.MemoryID
		}
		return scored[i].chunk.ID < scored[j].chunk.ID
	})

	bestByMemory := make(map[string]scoredChunk)
	for _, sc := range scored {
		if existing, ok := bestByMemory[sc.chunk.MemoryID]; !ok || sc.score > existing.score {
			bestByMemory[sc.chunk.MemoryID] = sc
		}
	}

	ranked := make([]scoredChunk, 0, len(bestByMemory))
	for _, sc := range bestByMemory {
		ranked = append(ranked, sc)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].chunk.MemoryID < ranked[j].chunk.MemoryID
	})

	metadata := model.QueryMetadata{
		Intent:          string(intentResult.Intent),
		KindsSearched:   []string{"all"},
		TotalCandidates: len(matchedChunks),
		Filters:         filters,
	}
	if len(intentResult.KindHints) > 0 {
		metadata.KindsSearched = intentResult.KindHints
	}
	if len(ranked) > 0 && ranked[0].score < minAbsoluteRecallScore {
		metadata.LatencyMs = time.Since(start).Milliseconds()
		h.setCachedResult(ctx, query, workingDir, projectContext, limit, ownerID, scope, model.RecallResult{
			Memories:      []model.RecalledMemory{},
			QueryMetadata: metadata,
		})
		return []model.RecalledMemory{}, metadata, nil
	}

	rerankCtx, rerankStage := startRecallStage(ctx, "rerank")
	if applied, reason := h.maybeRerank(rerankCtx, ownerID, source, query, searchQuery, queryTerms, ranked, scope.NoRerank); applied {
		metadata.Reranked = true
		metadata.RerankReason = reason
		recallRerankCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("memax.recall.reason", reason)))
		finishRecallStage(rerankCtx, rerankStage, nil, attribute.Bool("memax.recall.reranked", true), attribute.String("memax.recall.reason", reason))
	} else {
		finishRecallStage(rerankCtx, rerankStage, nil, attribute.Bool("memax.recall.reranked", false))
	}

	normalizeCtx, normalizeStage := startRecallStage(ctx, "results.normalize")
	ranked = h.expandRankedChunkContext(ctx, ranked, intentResult.Intent, ownerID, hubIDs)
	results := buildRecalledMemories(ranked, topicIDsByMemory, topicNamesByMemory)
	results = normalizeAndFilterResults(results)

	if len(results) > limit {
		results = results[:limit]
	}
	finishRecallStage(normalizeCtx, normalizeStage, nil, attribute.Int("memax.recall.result_count", len(results)))
	recallResultsHistogram.Record(ctx, int64(len(results)), metric.WithAttributes(attribute.String("memax.recall.source", source)))

	go h.reinforceResults(results, ownerID, hubIDs)

	metadata.LatencyMs = time.Since(start).Milliseconds()
	h.setCachedResult(ctx, query, workingDir, projectContext, limit, ownerID, scope, model.RecallResult{
		Memories:      results,
		QueryMetadata: metadata,
	})
	cacheStoreCtx, cacheStoreStage := startRecallStage(ctx, "cache.store")
	finishRecallStage(cacheStoreCtx, cacheStoreStage, nil)
	span.SetAttributes(
		attribute.Int("memax.recall.candidate_count", len(matchedChunks)),
		attribute.Int("memax.recall.result_count", len(results)),
		attribute.Bool("memax.recall.reranked", metadata.Reranked),
	)
	return results, metadata, nil
}

func recallCacheTTLSeconds() int {
	const fallback = 60
	if value := strings.TrimSpace(os.Getenv("RECALL_CACHE_TTL_SECONDS")); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return seconds
		}
	}
	return fallback
}

func (h *RecallHandler) getCachedResult(ctx context.Context, query string, workingDir string, projectContext map[string]string, limit int, ownerID string, scope RecallScope) (model.RecallResult, bool) {
	if h.cache == nil || ctx.Err() != nil {
		return model.RecallResult{}, false
	}
	cacheCtx, cancel := cacheReadContext(ctx)
	defer cancel()
	key := h.cacheKey(query, workingDir, projectContext, limit, ownerID, scope)
	payload, err := h.cache.Get(cacheCtx, key)
	if err != nil {
		slog.Debug("recall cache miss", "key", key, "query", query, "owner_id", ownerID, "hub_count", len(scope.HubIDs), "boost_hub_id", scope.ActiveBoostHubID, "error", err)
		return model.RecallResult{}, false
	}
	var result model.RecallResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		slog.Warn("recall cache decode failed", "key", key, "error", err)
		return model.RecallResult{}, false
	}
	slog.Info("recall cache hit", "key", key, "query", query, "owner_id", ownerID, "hub_count", len(scope.HubIDs), "boost_hub_id", scope.ActiveBoostHubID, "result_count", len(result.Memories))
	return result, true
}

func (h *RecallHandler) setCachedResult(ctx context.Context, query string, workingDir string, projectContext map[string]string, limit int, ownerID string, scope RecallScope, result model.RecallResult) {
	if h.cache == nil || h.cacheTTL <= 0 || ctx.Err() != nil {
		return
	}
	cacheCtx, cancel := detachedContext(cacheWriteTimeout)
	defer cancel()
	body, err := json.Marshal(result)
	if err != nil {
		return
	}
	key := h.cacheKey(query, workingDir, projectContext, limit, ownerID, scope)
	if err := h.cache.Set(cacheCtx, key, string(body), h.cacheTTL); err != nil {
		slog.Warn("recall cache store failed", "key", key, "query", query, "owner_id", ownerID, "hub_count", len(scope.HubIDs), "boost_hub_id", scope.ActiveBoostHubID, "error", err)
		return
	}
	slog.Info("recall cache stored", "key", key, "query", query, "owner_id", ownerID, "hub_count", len(scope.HubIDs), "boost_hub_id", scope.ActiveBoostHubID, "ttl_seconds", int(h.cacheTTL.Seconds()), "result_count", len(result.Memories))
}

func (h *RecallHandler) embedQuery(ctx context.Context, query string) ([][]float64, error) {
	if h.embedder == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	key := h.queryEmbeddingCacheKey(query)
	if h.cache != nil && ctx.Err() == nil {
		cacheCtx, cancel := cacheReadContext(ctx)
		payload, err := h.cache.Get(cacheCtx, key)
		cancel()
		if err == nil && payload != "" {
			var embedding []float64
			if err := json.Unmarshal([]byte(payload), &embedding); err == nil && len(embedding) > 0 {
				return [][]float64{embedding}, nil
			} else if err != nil {
				slog.Warn("query embedding cache decode failed", "key", key, "error", err)
			}
		}
	}

	embeddings, err := h.embedder.EmbedContext(ctx, []string{query}, "query")
	if err != nil || len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return embeddings, err
	}
	if h.cache != nil && ctx.Err() == nil {
		body, err := json.Marshal(embeddings[0])
		if err == nil {
			cacheCtx, cancel := detachedContext(cacheWriteTimeout)
			if err := h.cache.Set(cacheCtx, key, string(body), queryEmbeddingTTL); err != nil {
				slog.Warn("query embedding cache store failed", "key", key, "error", err)
			}
			cancel()
		}
	}
	return embeddings, nil
}

func (h *RecallHandler) queryEmbeddingCacheKey(query string) string {
	dimensions := 0
	if h.embedder != nil {
		dimensions = h.embedder.Dimensions()
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("v1\n%d\n%s", dimensions, strings.TrimSpace(query))))
	return fmt.Sprintf("memax:recall:query_embedding:%x", sum)
}

func (h *RecallHandler) cacheKey(query string, workingDir string, projectContext map[string]string, limit int, ownerID string, scope RecallScope) string {
	hubIDsCopy := append([]string(nil), scope.HubIDs...)
	sort.Strings(hubIDsCopy)
	var b strings.Builder
	b.WriteString(ownerID)
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(query))
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(workingDir))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%d\n", limit))
	b.WriteString("v3\n")
	b.WriteString(scope.ActiveBoostHubID)
	b.WriteString("\n")
	for _, hubID := range hubIDsCopy {
		b.WriteString(hubID)
		b.WriteString(",")
	}
	b.WriteString("\n")
	if projectContext != nil {
		keys := make([]string, 0, len(projectContext))
		for k := range projectContext {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(projectContext[k])
			b.WriteString("\n")
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("memax:recall:%x", sum)
}

func (h *RecallHandler) scoreChunk(
	chunk model.Chunk,
	memory *model.Memory,
	workingDir string,
	projectContext map[string]string,
	queryTerms []string,
	identifierTerms []string,
	intentResult intent.Result,
	topicNamesByMemory map[string]string,
	filters *model.SearchFilters,
	activeBoostHubID string,
) (scoredChunk, bool) {
	baseScore := chunk.RelevanceScore

	if activeBoostHubID != "" && memory.HubID == activeBoostHubID {
		baseScore *= 1.15
	}

	if workingDir != "" && memory.SourcePath != "" {
		if strings.HasPrefix(memory.SourcePath, workingDir) || strings.HasPrefix(workingDir, memory.SourcePath) {
			baseScore *= 1.2
		}
	}

	if len(projectContext) > 0 && len(memory.ProjectContext) > 0 {
		reqRepo := projectContext["repo"]
		memRepo := memory.ProjectContext["repo"]
		reqProject := projectContext["project"]
		memProject := memory.ProjectContext["project"]

		if reqRepo != "" && memRepo != "" && reqRepo == memRepo {
			baseScore *= 1.8
		} else if reqProject != "" && memProject != "" && reqProject == memProject {
			baseScore *= 1.3
		} else if memRepo != "" {
			// Memory belongs to a different project — strong penalty.
			// Infrastructure/code memories from other projects are rarely
			// relevant and can be harmful in retrieval results.
			baseScore *= 0.5
		}
	}

	titleLower := strings.ToLower(memory.Title)
	titleMatchCount := 0
	for _, term := range queryTerms {
		if strings.Contains(titleLower, term) {
			titleMatchCount++
		}
	}
	if titleMatchCount > 0 && len(queryTerms) > 0 {
		titleMatchRatio := float64(titleMatchCount) / float64(len(queryTerms))
		baseScore *= 1.0 + (titleMatchRatio * 0.5)
	}

	headingBoost := headingMatchBoost(chunk.HeadingChain, queryTerms)
	if headingBoost > 1 {
		baseScore *= headingBoost
	}

	identifierBoost := identifierMatchBoost(memory, chunk, identifierTerms)
	if identifierBoost > 1 {
		baseScore *= identifierBoost
	}

	metaBoost := metadataEntityBoost(chunk, identifierTerms)
	if metaBoost > 1 {
		baseScore *= metaBoost
	}

	if memory.RetrievalWeight > 0 {
		baseScore *= memory.RetrievalWeight
	}

	for _, hint := range intentResult.KindHints {
		if strings.EqualFold(memory.Kind, hint) {
			baseScore *= 2.2
			break
		}
	}

	if tagKind := kindHintFromTags(memory.Tags); tagKind != "" {
		for _, hint := range intentResult.KindHints {
			if tagKind == hint {
				baseScore *= 1.25
				break
			}
		}
	}

	for _, tag := range memory.Tags {
		tagLower := strings.ToLower(tag)
		matched := false
		for _, term := range queryTerms {
			if len(term) > 2 && (strings.Contains(tagLower, term) || strings.Contains(term, tagLower)) {
				baseScore *= 1.1
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}

	if topicName, ok := topicNamesByMemory[memory.ID]; ok {
		topicLower := strings.ToLower(topicName)
		for _, term := range queryTerms {
			if len(term) > 2 && strings.Contains(topicLower, term) {
				baseScore *= 1.05
				goto topicDone
			}
		}
	}
topicDone:

	if intentResult.RecencyPreference == "recent" {
		age := time.Since(memory.CreatedAt)
		switch {
		case age < 7*24*time.Hour:
			baseScore *= 1.4
		case age >= 30*24*time.Hour:
			baseScore *= 0.8
		}
	}

	if memory.Pinned {
		baseScore *= 1.1
	}

	daysSinceAccess := time.Since(memory.AccessedAt).Hours() / 24.0
	baseScore *= decay.Multiplier(daysSinceAccess, memory.AccessCount, model.NormalizeMemoryStability(memory.Stability))

	// --- Structured filter boosts (boost, never gate) ---
	if filters != nil {
		// Temporal proximity boost — checks created_at AND event_dates
		if filters.TemporalStart != nil && filters.TemporalEnd != nil {
			start, end := *filters.TemporalStart, *filters.TemporalEnd
			dayBefore := start.Add(-24 * time.Hour)
			dayAfter := end.Add(24 * time.Hour)

			temporalBoost := 1.0
			// Check created_at (push date)
			if !memory.CreatedAt.Before(start) && memory.CreatedAt.Before(end) {
				temporalBoost = 2.5
			} else if !memory.CreatedAt.Before(dayBefore) && memory.CreatedAt.Before(dayAfter) {
				temporalBoost = 1.5
			}
			// Check event_dates (dates mentioned in content) — take the best match
			for _, ed := range memory.EventDates {
				if !ed.Before(start) && ed.Before(end) {
					temporalBoost = max(temporalBoost, 2.5)
					break
				} else if !ed.Before(dayBefore) && ed.Before(dayAfter) {
					temporalBoost = max(temporalBoost, 1.5)
				}
			}
			baseScore *= temporalBoost
		}

		// Author intent boost — author means who created/pushed/saved the memory.
		// Strong boost (3.0x) for matching author. No penalty for non-matching
		// because the distiller's author extraction is non-deterministic —
		// a penalty causes result-set collapse when the filter is spurious.
		if len(filters.Authors) > 0 {
			for _, author := range filters.Authors {
				authorLower := strings.ToLower(strings.TrimSpace(author))
				if authorLower == "" {
					continue
				}
				if memory.AuthorName != "" && strings.Contains(strings.ToLower(memory.AuthorName), authorLower) {
					baseScore *= 3.0
					break
				}
			}
		}

		// People match boost
		if len(filters.People) > 0 {
			matched := false
			for _, person := range filters.People {
				personLower := strings.ToLower(person)
				// Check tags with person: prefix
				for _, tag := range memory.Tags {
					if strings.HasPrefix(tag, "person:") && strings.Contains(strings.ToLower(tag[7:]), personLower) {
						matched = true
						break
					}
				}
				if matched {
					break
				}
				// Check if the person is the memory author (handles "what did X push")
				if memory.AuthorName != "" && strings.Contains(strings.ToLower(memory.AuthorName), personLower) {
					matched = true
					break
				}
				// Fallback: check title and content for name mention
				if strings.Contains(strings.ToLower(memory.Title), personLower) ||
					strings.Contains(strings.ToLower(chunk.Content), personLower) {
					matched = true
					break
				}
			}
			if matched {
				baseScore *= 2.0
			}
		}

		// Kind match boost. This remains a soft boost, never a hard filter.
		if filters.Kind != "" {
			if strings.EqualFold(model.NormalizeMemoryKind(filters.Kind), memory.Kind) {
				baseScore *= 2.5
			}
		}

		// Source agent boost
		if filters.Source != "" {
			if strings.EqualFold(memory.SourceAgent, filters.Source) {
				baseScore *= 1.5
			}
		}
	}

	return scoredChunk{chunk: chunk, memory: memory, score: baseScore}, true
}

func appendStructuredFilterTerms(query string, filters *model.SearchFilters) string {
	if filters == nil {
		return query
	}
	terms := make([]string, 0, len(filters.Authors)+len(filters.People)+3)
	terms = append(terms, filters.Authors...)
	terms = append(terms, filters.People...)
	if filters.Source != "" {
		terms = append(terms, filters.Source)
	}
	if filters.Hub != "" {
		terms = append(terms, filters.Hub)
	}
	if len(terms) == 0 {
		return query
	}
	seen := map[string]bool{}
	var clean []string
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		key := strings.ToLower(term)
		if seen[key] {
			continue
		}
		seen[key] = true
		clean = append(clean, term)
	}
	if len(clean) == 0 {
		return query
	}
	if strings.TrimSpace(query) == "" {
		return strings.Join(clean, " ")
	}
	return strings.TrimSpace(query + " " + strings.Join(clean, " "))
}

// kindHintFromTags infers a memory kind from its tags for scoring purposes.
// Returns the first matching kind in tag order — if a memory is tagged both
// "meeting" and "review", the first tag wins. This is intentional: tag order
// reflects the author's primary categorization, and the function returns at
// most one kind to avoid double-boosting.
func kindHintFromTags(tags []string) string {
	for _, tag := range tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "how-to", "runbook", "playbook", "guide", "tutorial":
			return model.MemoryKindProcedural
		case "adr", "decision", "tradeoff", "postmortem", "选型", "决策",
			// Non-dev decision vocabulary
			"proposal", "recommendation", "evaluation", "assessment",
			"analysis", "review", "strategy", "rationale":
			return model.MemoryKindRationale
		case "fact", "reference", "policy", "guideline", "standard":
			return model.MemoryKindSemantic
		case "meeting", "standup", "daily", "minutes", "retro", "retrospective",
			"workshop", "offsite", "sync":
			return model.MemoryKindEpisodic
		}
	}
	return ""
}

func shouldEmbedDistilledQuery(original string, distilled string, confidence float64) bool {
	original = strings.TrimSpace(original)
	distilled = strings.TrimSpace(distilled)
	if original == "" || distilled == "" {
		return false
	}
	if confidence > 0 && confidence < 0.55 {
		return false
	}
	if strings.EqualFold(original, distilled) {
		return false
	}
	originalTerms := len(strings.Fields(original))
	distilledTerms := len(strings.Fields(distilled))
	if originalTerms >= 60 || len(original) >= 400 {
		return true
	}
	if distilledTerms > 0 && originalTerms >= distilledTerms*3 {
		return true
	}
	return false
}

func (h *RecallHandler) loadHubNames(ranked []scoredChunk) map[string]string {
	names := make(map[string]string)
	if h.store == nil {
		return names
	}
	for _, sc := range ranked {
		hubID := sc.memory.HubID
		if hubID == "" {
			continue
		}
		if _, seen := names[hubID]; seen {
			continue
		}
		hub, err := h.store.GetHub(hubID)
		if err != nil || hub == nil || strings.TrimSpace(hub.Name) == "" {
			names[hubID] = ""
			continue
		}
		names[hubID] = strings.TrimSpace(hub.Name)
	}
	return names
}

func (h *RecallHandler) expandRankedChunkContext(ctx context.Context, ranked []scoredChunk, queryIntent intent.IntentType, ownerID string, hubIDs []string) []scoredChunk {
	if h.store == nil || len(ranked) == 0 || !shouldExpandRecallContext(queryIntent) {
		return ranked
	}
	limit := len(ranked)
	if limit > 3 {
		limit = 3
	}
	expanded := append([]scoredChunk(nil), ranked...)
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			return expanded
		}
		chunks, err := h.store.GetAccessibleChunksByMemory(expanded[i].memory.ID, ownerID, hubIDs)
		if err != nil || len(chunks) <= 1 {
			continue
		}
		if content := buildExpandedChunkContent(chunks, expanded[i].chunk.ID); content != "" {
			expanded[i].chunk.Content = content
		}
	}
	return expanded
}

func shouldExpandRecallContext(queryIntent intent.IntentType) bool {
	switch queryIntent {
	case intent.HowTo, intent.Why, intent.WhatIs, intent.Debug, intent.Temporal:
		return true
	default:
		return false
	}
}

func buildExpandedChunkContent(chunks []model.Chunk, matchedChunkID string) string {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].ChunkIndex < chunks[j].ChunkIndex
	})
	matchedIndex := -1
	for i, chunk := range chunks {
		if chunk.ID == matchedChunkID {
			matchedIndex = i
			break
		}
	}
	if matchedIndex < 0 {
		return ""
	}
	start := matchedIndex - 1
	if start < 0 {
		start = 0
	}
	end := matchedIndex + 1
	if end >= len(chunks) {
		end = len(chunks) - 1
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		chunk := chunks[i]
		if strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		heading := strings.TrimSpace(chunk.HeadingChain)
		if heading == "" {
			heading = fmt.Sprintf("Chunk %d", chunk.ChunkIndex+1)
		}
		b.WriteString("[Section: ")
		b.WriteString(heading)
		if chunk.ID == matchedChunkID {
			b.WriteString(" | matched")
		}
		b.WriteString("]\n")
		b.WriteString(strings.TrimSpace(chunk.Content))
	}
	return b.String()
}

func cacheReadContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, cacheReadTimeout)
}

func detachedContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func uniqueChunkMemoryIDs(chunks []model.Chunk) []string {
	seen := make(map[string]struct{}, len(chunks))
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if _, ok := seen[chunk.MemoryID]; ok {
			continue
		}
		seen[chunk.MemoryID] = struct{}{}
		ids = append(ids, chunk.MemoryID)
	}
	return ids
}

func recallCandidateLimit(limit int) int {
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	candidateLimit := limit * candidatePoolMultiplier
	if candidateLimit < defaultCandidatePool {
		candidateLimit = defaultCandidatePool
	}
	if candidateLimit > maxCandidatePool {
		candidateLimit = maxCandidatePool
	}
	return candidateLimit
}

func headingMatchBoost(heading string, queryTerms []string) float64 {
	headingLower := strings.ToLower(strings.TrimSpace(heading))
	if headingLower == "" || len(queryTerms) == 0 {
		return 1
	}

	matchCount := 0
	for _, term := range queryTerms {
		if strings.Contains(headingLower, term) {
			matchCount++
		}
	}
	if matchCount == 0 {
		return 1
	}

	boost := 1.0 + (float64(matchCount) / float64(len(queryTerms)) * 0.35)
	if matchCount == len(queryTerms) && len(queryTerms) > 1 {
		boost += 0.15
	}

	normalizedHeading := strings.Join(strings.Fields(headingLower), " ")
	normalizedQuery := strings.Join(queryTerms, " ")
	switch {
	case normalizedHeading == normalizedQuery:
		boost += 0.25
	case strings.HasPrefix(normalizedHeading, normalizedQuery):
		boost += 0.15
	}
	return boost
}

// metadataEntityBoost rewards chunks whose metadata_text matches entity-like
// query terms (hub slugs, repo paths, source agents, author handles). A match
// means the memory belongs to the referenced entity, which is a stronger signal
// than a content mention. Separated from identifierMatchBoost so the weight
// can be tuned independently.
func metadataEntityBoost(chunk model.Chunk, identifierTerms []string) float64 {
	if len(identifierTerms) == 0 || chunk.MetadataText == "" {
		return 1
	}
	metaLower := strings.ToLower(chunk.MetadataText)
	matches := 0
	for _, term := range identifierTerms {
		if strings.Contains(metaLower, term) {
			matches++
		}
	}
	if matches == 0 {
		return 1
	}
	// 1.4x for a single metadata entity match, +0.1 per additional match,
	// capped at 1.8x. This is stronger than identifierMatchBoost (1.2x)
	// because metadata matches indicate structural affinity, not just
	// keyword overlap.
	boost := 1.4 + (float64(matches-1) * 0.1)
	if boost > 1.8 {
		boost = 1.8
	}
	return boost
}

func identifierMatchBoost(memory *model.Memory, chunk model.Chunk, identifierTerms []string) float64 {
	if len(identifierTerms) == 0 {
		return 1
	}

	fields := []string{
		strings.ToLower(memory.Title),
		strings.ToLower(memory.SourcePath),
		strings.ToLower(chunk.HeadingChain),
		strings.ToLower(chunk.Content),
		strings.ToLower(chunk.ProjectRepo),
	}

	matches := 0
	for _, term := range identifierTerms {
		found := false
		for _, field := range fields {
			if field != "" && strings.Contains(field, term) {
				found = true
				break
			}
		}
		if found {
			matches++
		}
	}
	if matches == 0 {
		return 1
	}

	boost := 1.2 + (float64(matches-1) * 0.08)
	if matches == len(identifierTerms) && len(identifierTerms) > 1 {
		boost += 0.08
	}
	return boost
}

// maybeRerank uses the distilled searchQuery for the rerank *decision*
// (shouldRerank checks score gaps, query length, lexical anchoring — all
// relative to what was searched), but passes the raw user query to the
// reranker model. Cross-encoders like Cohere Rerank do their own query
// understanding — they benefit from the full user intent (names, time
// references, comparison markers) that the distiller may have stripped.
func (h *RecallHandler) maybeRerank(ctx context.Context, ownerID string, source string, rawQuery string, searchQuery string, queryTerms []string, ranked []scoredChunk, noRerank bool) (bool, string) {
	if h.reranker == nil || len(ranked) == 0 || noRerank {
		return false, ""
	}
	if h.canRerankFn != nil && !h.canRerankFn(ownerID, source) {
		return false, ""
	}

	should, reason := shouldRerank(searchQuery, queryTerms, ranked)
	if !should {
		return false, ""
	}

	topN := h.reranker.TopN()
	if topN <= 0 || topN > len(ranked) {
		topN = len(ranked)
	}
	candidates := ranked[:topN]
	hubNamesByID := h.loadHubNames(candidates)

	docs := make([]rerank.Document, 0, len(candidates))
	for _, candidate := range candidates {
		docs = append(docs, rerank.Document{
			ID:      candidate.memory.ID,
			Content: serializeRerankDocument(candidate, hubNamesByID[candidate.memory.HubID]),
		})
	}

	results, err := h.reranker.Rerank(ctx, rawQuery, docs)
	if err != nil {
		slog.Warn("rerank failed; keeping local ranking", "error", err, "reason", reason)
		return false, ""
	}
	if len(results) == 0 {
		return false, ""
	}

	byID := make(map[string]rerank.Result, len(results))
	maxScore := 0.0
	for _, result := range results {
		byID[result.ID] = result
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	if maxScore <= 0 {
		return false, ""
	}

	localMax := candidates[0].score
	if localMax <= 0 {
		localMax = 1
	}
	for i := range ranked {
		ranked[i].score = ranked[i].score / localMax
	}
	for i := range candidates {
		if result, ok := byID[candidates[i].memory.ID]; ok {
			localNorm := candidates[i].score
			rerankNorm := result.Score / maxScore
			candidates[i].score = (0.35 * localNorm) + (0.65 * rerankNorm)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	copy(ranked[:topN], candidates)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	return true, reason
}

func shouldRerank(query string, queryTerms []string, ranked []scoredChunk) (bool, string) {
	if len(ranked) < 3 {
		return false, ""
	}

	top1 := ranked[0].score
	top2 := ranked[1].score
	if top1 <= 0 {
		return true, "low_confidence"
	}

	if top2 > 0 && (top1/top2) < 1.12 {
		return true, "tight_score_gap"
	}

	kinds := make(map[string]struct{})
	for i := 0; i < len(ranked) && i < 5; i++ {
		kinds[ranked[i].memory.Kind] = struct{}{}
	}
	if len(kinds) >= 3 {
		return true, "mixed_candidate_set"
	}

	if len(queryTerms) <= 2 && len(strings.TrimSpace(query)) <= 24 {
		return true, "broad_short_query"
	}

	titleLower := strings.ToLower(ranked[0].memory.Title)
	anchored := false
	for _, term := range queryTerms {
		if len(term) > 3 && strings.Contains(titleLower, term) {
			anchored = true
			break
		}
	}
	if !anchored {
		return true, "weak_lexical_anchor"
	}

	return false, ""
}

func serializeRerankDocument(candidate scoredChunk, hubName string) string {
	var b strings.Builder
	if candidate.memory.ID != "" {
		b.WriteString("Memory ID: ")
		b.WriteString(candidate.memory.ID)
		b.WriteString("\n")
	}
	if candidate.memory.Title != "" {
		b.WriteString("Title: ")
		b.WriteString(candidate.memory.Title)
		b.WriteString("\n")
	}
	if candidate.memory.AuthorName != "" {
		b.WriteString("Author: ")
		b.WriteString(candidate.memory.AuthorName)
		b.WriteString("\n")
	}
	if !candidate.memory.CreatedAt.IsZero() {
		b.WriteString("Pushed at: ")
		b.WriteString(candidate.memory.CreatedAt.Format(time.RFC3339))
		b.WriteString("\n")
	}
	if hubName != "" {
		b.WriteString("Hub: ")
		b.WriteString(hubName)
		b.WriteString("\n")
	}
	if candidate.memory.Source != "" {
		b.WriteString("Source: ")
		b.WriteString(candidate.memory.Source)
		b.WriteString("\n")
	}
	if candidate.memory.SourceAgent != "" {
		b.WriteString("Source agent: ")
		b.WriteString(candidate.memory.SourceAgent)
		b.WriteString("\n")
	}
	if candidate.memory.ProjectContext != nil && candidate.memory.ProjectContext["repo"] != "" {
		b.WriteString("Repo: ")
		b.WriteString(candidate.memory.ProjectContext["repo"])
		b.WriteString("\n")
	}
	if candidate.memory.Kind != "" {
		b.WriteString("Kind: ")
		b.WriteString(candidate.memory.Kind)
		b.WriteString("\n")
	}
	if candidate.memory.Stability != "" {
		b.WriteString("Stability: ")
		b.WriteString(candidate.memory.Stability)
		b.WriteString("\n")
	}
	if len(candidate.memory.Tags) > 0 {
		b.WriteString("Tags: ")
		b.WriteString(strings.Join(candidate.memory.Tags, ", "))
		b.WriteString("\n")
	}
	if candidate.memory.Summary != "" {
		b.WriteString("Summary: ")
		b.WriteString(candidate.memory.Summary)
		b.WriteString("\n")
	}
	if candidate.chunk.HeadingChain != "" {
		b.WriteString("Section: ")
		b.WriteString(candidate.chunk.HeadingChain)
		b.WriteString("\n")
	}
	b.WriteString("Chunk: ")
	b.WriteString(candidate.chunk.Content)
	return b.String()
}

func buildRecalledMemories(ranked []scoredChunk, topicIDsByMemory map[string]string, topicNamesByMemory map[string]string) []model.RecalledMemory {
	results := make([]model.RecalledMemory, 0, len(ranked))
	for _, sr := range ranked {
		projRepo := ""
		if sr.memory.ProjectContext != nil {
			projRepo = sr.memory.ProjectContext["repo"]
		}
		results = append(results, model.RecalledMemory{
			ID:             sr.memory.ID,
			Title:          sr.memory.Title,
			Summary:        sr.memory.Summary,
			ChunkContent:   sr.chunk.Content,
			HeadingChain:   sr.chunk.HeadingChain,
			RelevanceScore: sr.score,
			Kind:           sr.memory.Kind,
			Stability:      sr.memory.Stability,
			Source:         sr.memory.Source,
			Age:            formatAge(sr.memory.CreatedAt),
			CreatedAt:      sr.memory.CreatedAt.Format(time.RFC3339),
			AuthorName:     sr.memory.AuthorName,
			HubID:          sr.memory.HubID,
			ProjectRepo:    projRepo,
			Hint:           sr.memory.Hint,
			TopicID:        topicIDsByMemory[sr.memory.ID],
			TopicName:      topicNamesByMemory[sr.memory.ID],
		})
	}
	return results
}

func normalizeAndFilterResults(results []model.RecalledMemory) []model.RecalledMemory {
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})
	if len(results) == 0 {
		return results
	}

	maxScore := results[0].RelevanceScore

	// Adaptive threshold: when all results have low absolute scores
	// (no strong match found), use a tighter relative cutoff to limit
	// the number of weak/tangential results returned.
	threshold := normalRelativeFilter
	if maxScore < weakResultCeiling {
		threshold = tightRelativeFilter
	}

	if maxScore > 0 {
		for i := range results {
			results[i].RelevanceScore = results[i].RelevanceScore / maxScore
		}
	}

	filtered := results[:0]
	for _, result := range results {
		if result.RelevanceScore >= threshold {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

// reinforceResults records that these memories appeared in recall results.
// Increments shown_count (for analytics) — NOT access_count (which feeds
// decay scoring). This prevents feedback loops where irrelevant memories
// get reinforced just by being returned.
func (h *RecallHandler) reinforceResults(results []model.RecalledMemory, ownerID string, hubIDs []string) {
	if len(results) == 0 {
		return
	}
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	if err := h.store.IncrementMemoryShownBatch(context.Background(), ids, ownerID, hubIDs); err != nil {
		slog.Warn("failed to increment shown_count", "error", err, "count", len(ids))
	}
}

func (h *RecallHandler) Recall(w http.ResponseWriter, r *http.Request) {
	var req model.RecallRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "missing_query", "Query is required")
		return
	}
	setUsageEventSummary(r, req.Query)

	ownerID := GetUserID(r)
	scope := requestRecallScope(r, req.HubIDs)
	// Frozen-hub filtering happens inside RunPipeline.
	// Validate topic_id before it reaches the SQL layer (where a non-UUID
	// would cause a Postgres cast error surfaced as 500).
	if req.TopicID != "" && !isValidUUID(req.TopicID) {
		writeError(w, http.StatusBadRequest, "invalid_topic_id", "topic_id must be a valid UUID")
		return
	}
	// Build explicit search filters from API params
	var explicitFilters *model.SearchFilters
	if req.Kind != "" || req.CreatedAfter != nil || req.CreatedBefore != nil || req.TopicID != "" {
		explicitFilters = &model.SearchFilters{
			TemporalStart: req.CreatedAfter,
			TemporalEnd:   req.CreatedBefore,
			Kind:          model.NormalizeMemoryKind(req.Kind),
			TopicID:       req.TopicID,
			Explicit:      true,
		}
	}
	scope.NoRerank = req.NoRerank
	results, metadata, err := h.RunPipeline(r.Context(), req.Query, req.Source, req.WorkingDir, req.ProjectContext, req.Limit, ownerID, explicitFilters, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_error", err.Error())
		return
	}

	slog.Info("recall",
		"query", req.Query,
		"results", len(results),
		"candidates", metadata.TotalCandidates,
		"latency_ms", metadata.LatencyMs,
		"reranked", metadata.Reranked,
		"rerank_reason", metadata.RerankReason,
	)
	trackRequest(r, "api.recall", map[string]any{
		"results":       len(results),
		"candidates":    metadata.TotalCandidates,
		"latency_ms":    metadata.LatencyMs,
		"source":        req.Source,
		"reranked":      metadata.Reranked,
		"rerank_reason": metadata.RerankReason,
	})

	meterctx.CommitFromContext(r.Context())
	// Coalesced `agent.changed` activity signal — keeps Settings →
	// Your Agents "last observed" live cross-tab without flooding
	// the event stream on recall bursts. TryPublishAgentActivity
	// drops events within the 20s coalesce window per (user, agent).
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, GetAgentName(r))
	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: model.RecallResult{
			Memories:      results,
			QueryMetadata: metadata,
		},
	})
}

// preserveRawEntityTerms finds entity-like terms in the raw query that were
// dropped during distillation. These terms (hub slugs, repo paths, source
// agents, author handles) match indexed metadata_text and should remain in
// the lexical search even though they aren't useful for vector/semantic search.
//
// shouldApplyHubFilter determines whether a distiller-extracted hub filter
// should actually hard-scope search to that hub. LLMs frequently mistake
// product/project names for hub names (e.g., "memax" the product vs "memax"
// the hub). This guard checks for explicit hub-intent signals in the raw query.
//
// Without explicit intent, the hub term stays in the search query for semantic
// matching but does not narrow hubIDs.
// discriminatingFieldTerms extracts terms from the search query suitable for
// the title-matching field lane. It filters out:
// - Hub slugs and hub names (non-discriminating in hub-dense corpora)
// - Latin terms shorter than 3 characters (too noisy for LIKE patterns)
// - Single CJK characters (bypass pg_trgm and overmatch)
// Terms are lowercased and deduplicated.
func (h *RecallHandler) discriminatingFieldTerms(searchQuery string, hubIDs []string) []string {
	// Build set of non-discriminating terms from accessible hub slugs/names
	nonDiscriminating := make(map[string]bool)
	for _, hubID := range hubIDs {
		if hub, err := h.store.GetHub(hubID); err == nil && hub != nil {
			slug := strings.ToLower(hub.Slug)
			name := strings.ToLower(hub.Name)
			nonDiscriminating[slug] = true
			nonDiscriminating[name] = true
			// Split hyphenated/space parts: "memax-test" → "memax", "test"
			for _, part := range strings.FieldsFunc(slug, func(r rune) bool {
				return r == '-' || r == '_'
			}) {
				nonDiscriminating[part] = true
			}
			for _, part := range strings.Fields(name) {
				nonDiscriminating[strings.ToLower(part)] = true
			}
		}
	}

	// Tokenize with character-level CJK handling, matching
	// tokenizeForMatching's bigram+stopword logic. strings.Fields
	// can't split "memax的邮件方案" or apply CJK stopword filtering
	// within "的邮件方案".
	q := strings.ToLower(searchQuery)
	var raw []string
	var latin strings.Builder
	var cjkBuf []rune

	flushLatin := func() {
		if latin.Len() == 0 {
			return
		}
		w := strings.Trim(latin.String(), "?？.。!！,，:：;；\"'()（）")
		latin.Reset()
		if len(w) >= 3 {
			raw = append(raw, w)
		}
	}

	flushCJK := func() {
		if len(cjkBuf) == 0 {
			return
		}
		// emitSingles=false: field lane LIKE patterns need >= 2 chars.
		raw = append(raw, cjkBigrams(cjkBuf, false)...)
		cjkBuf = cjkBuf[:0]
	}

	for _, r := range q {
		if isCJKRune(r) {
			flushLatin()
			cjkBuf = append(cjkBuf, r)
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			flushCJK()
			flushLatin()
		} else {
			flushCJK()
			latin.WriteRune(r)
		}
	}
	flushCJK()
	flushLatin()

	// Filter non-discriminating terms (hub slugs/names) and dedup.
	seen := make(map[string]bool)
	var terms []string
	for _, w := range raw {
		if seen[w] || nonDiscriminating[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
	}
	return terms
}

// isValidUUID checks if s looks like a UUID (8-4-4-4-12 hex format).
// Does not require a specific UUID version — just validates shape.
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// isCJKRune returns true if the rune is a CJK unified ideograph.
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// cjkBigrams splits a CJK rune buffer by stopword characters and returns
// overlapping bigrams for each contiguous non-stopword run. Runs of length 1
// emit the single character when emitSingles is true (used by title/tag
// matching where single-char substring matches are valid), or are skipped
// when false (used by the field lane where LIKE patterns need >= 2 chars).
func cjkBigrams(buf []rune, emitSingles bool) []string {
	var result []string
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		if len(run) == 1 && emitSingles {
			result = append(result, string(run[0]))
		} else if len(run) >= 2 {
			for i := 0; i < len(run)-1; i++ {
				result = append(result, string(run[i:i+2]))
			}
		}
		run = run[:0]
	}
	for _, r := range buf {
		if cjkStopRunes[r] {
			flush()
		} else {
			run = append(run, r)
		}
	}
	flush()
	return result
}

func shouldApplyHubFilter(rawQuery, hubSlug string) bool {
	q := strings.ToLower(rawQuery)
	slug := strings.ToLower(hubSlug)

	// English explicit hub intent patterns
	patterns := []string{
		"in " + slug + " hub",
		"in the " + slug + " hub",
		"from " + slug + " hub",
		"from the " + slug + " hub",
		slug + " hub",
		slug + " team",
		slug + " workspace",
		"hub:" + slug,
	}
	// CJK explicit hub intent patterns
	patterns = append(patterns,
		slug+"里",
		slug+"里的",
		slug+" 里",
		slug+" 里的",
		slug+" 团队",
		slug+" 工作区",
		"在"+slug+" hub",
		"在 "+slug+" hub",
		"从"+slug+" hub",
		"从 "+slug+" hub",
		slug+" hub 里",
		slug+" hub 里的",
	)

	for _, p := range patterns {
		if strings.Contains(q, p) {
			return true
		}
	}
	return false
}

// This does NOT blindly re-add all dropped words — only terms that look like
// structured identifiers (contain hyphens, dots, slashes, colons, or
// camelCase). Ordinary words like "discuss", "notes", "roadmap" are left to
// the distiller.
// fuseDualQueryChunks merges the raw-query and distilled-query result
// sets with rank-based RRF (k=60, equal weight — learned/score-based
// blending measurably loses to fixed equal weights, arXiv:2608.00183).
// Rank-based fusion means neither channel's raw scores can dominate;
// a chunk found by BOTH channels rises, a chunk only the raw channel
// found (the entity the distiller dropped) still surfaces. Output
// order is a deterministic total order: fused score desc → memory ID
// → chunk ID, independent of input map/slice ordering.
//
// RelevanceScore is set to the max of the two channels' store scores
// so downstream per-chunk scoring keeps operating on the same scale
// it always has.
func fuseDualQueryChunks(rawChunks, distilledChunks []model.Chunk, limit int) []model.Chunk {
	const fusionK = 60
	type fusedEntry struct {
		chunk model.Chunk
		score float64
	}
	entries := make(map[string]*fusedEntry, len(rawChunks)+len(distilledChunks))
	absorb := func(chunks []model.Chunk) {
		for rank, c := range chunks {
			contribution := 1.0 / float64(fusionK+rank+1)
			if existing, ok := entries[c.ID]; ok {
				existing.score += contribution
				if c.RelevanceScore > existing.chunk.RelevanceScore {
					existing.chunk.RelevanceScore = c.RelevanceScore
				}
			} else {
				entries[c.ID] = &fusedEntry{chunk: c, score: contribution}
			}
		}
	}
	absorb(rawChunks)
	absorb(distilledChunks)

	fused := make([]fusedEntry, 0, len(entries))
	for _, e := range entries {
		fused = append(fused, *e)
	}
	sort.Slice(fused, func(i, j int) bool {
		if fused[i].score != fused[j].score {
			return fused[i].score > fused[j].score
		}
		if fused[i].chunk.MemoryID != fused[j].chunk.MemoryID {
			return fused[i].chunk.MemoryID < fused[j].chunk.MemoryID
		}
		return fused[i].chunk.ID < fused[j].chunk.ID
	})
	// Re-apply the store's per-memory cap: each channel already caps at
	// 3 chunks/memory, but with disjoint picks one memory could occupy
	// 6 of the limit slots and halve candidate memory diversity
	// (adversarial review finding 1).
	const fusedPerMemoryCap = 3
	perMemory := make(map[string]int)
	out := make([]model.Chunk, 0, minInt(len(fused), limit))
	for _, e := range fused {
		if perMemory[e.chunk.MemoryID] >= fusedPerMemoryCap {
			continue
		}
		perMemory[e.chunk.MemoryID]++
		out = append(out, e.chunk)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func preserveRawEntityTerms(rawQuery, distilledQuery string) []string {
	distilledLower := strings.ToLower(distilledQuery)

	raw := strings.Fields(rawQuery)
	var preserved []string
	seen := make(map[string]struct{})
	for _, token := range raw {
		token = strings.Trim(token, "?!.,;:'\"`()[]{}<>")
		if len(token) < 3 {
			continue
		}
		lower := strings.ToLower(token)
		// Skip if the distilled query already contains this term.
		if strings.Contains(distilledLower, lower) {
			continue
		}
		if _, ok := seen[lower]; ok {
			continue
		}
		// Only preserve terms that look like entity references, not
		// ordinary English words the distiller intentionally removed.
		if !looksLikeEntityRef(token) {
			continue
		}
		seen[lower] = struct{}{}
		preserved = append(preserved, lower)
	}
	// Cap to avoid one noisy prompt dominating the lexical query.
	const maxPreserved = 5
	if len(preserved) > maxPreserved {
		preserved = preserved[:maxPreserved]
	}
	return preserved
}

// looksLikeEntityRef returns true for tokens that resemble structured
// metadata references rather than ordinary prose:
//   - Hyphenated slugs: memax-test, platform-private, claude-code
//   - Dotted/slashed paths: github.com/MemaxLabs/memax-internal, packages/server
//   - Colon-prefixed metadata: hub:memax-test, author:jiahao
//   - CamelCase or mixed-case identifiers: MemaxLabs, pgVector
//
// Single ordinary words (discuss, notes, roadmap) return false.
func looksLikeEntityRef(token string) bool {
	if len(token) < 3 {
		return false
	}
	// Colon-prefixed metadata syntax (hub:x, author:x, repo:x)
	if strings.Contains(token, ":") {
		return true
	}
	// Paths and dotted references
	if strings.Contains(token, "/") || strings.Contains(token, ".") {
		return true
	}
	// Hyphenated slugs — but exclude common English hyphenations.
	// A slug typically has short segments: memax-test, claude-code.
	if strings.Contains(token, "-") {
		return true
	}
	// Underscored identifiers
	if strings.Contains(token, "_") {
		return true
	}
	// CamelCase (mixed upper+lower after first char)
	hasUpper, hasLower := false, false
	for i, r := range token {
		if i == 0 {
			continue
		}
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasUpper && hasLower
}

// latinStopWords contains English function words that never discriminate
// in title/heading/tag matching.
var latinStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "do": true, "does": true, "did": true,
	"for": true, "of": true, "in": true, "on": true, "to": true,
	"and": true, "or": true, "but": true, "not": true, "with": true,
	"what": true, "how": true, "why": true, "where": true, "when": true,
	"our": true, "my": true, "your": true, "we": true, "us": true,
	"it": true, "its": true, "this": true, "that": true,
}

// cjkStopRunes contains Chinese function characters that inflate match ratios
// without adding discriminating signal. Conservative set: only particles,
// copulas, conjunctions, adverbs, demonstratives, and interrogative particles.
var cjkStopRunes = map[rune]bool{
	// Structural particles
	'的': true, '了': true, '着': true, '过': true,
	// Copula / existential
	'是': true, '在': true,
	// Conjunctions
	'和': true, '与': true, '或': true, '但': true,
	// Common adverbs (不/没 excluded: negation is semantic, not disposable)
	'也': true, '都': true, '就': true, '还': true,
	'很': true, '太': true,
	// Demonstratives
	'这': true, '那': true,
	// Interrogative / sentence-final particles
	'什': true, '么': true, '吗': true, '呢': true,
	'吧': true, '啊': true,
}

// tokenizeForMatching splits a query into terms for title/heading/tag matching.
// Handles mixed Latin + CJK text:
//   - Latin tokens: split on whitespace, lowercased, stopword-filtered (min 2 chars)
//   - CJK characters: buffered into runs, stopwords removed, then emitted as
//     overlapping bigrams within each contiguous non-stopword run. Runs of length 1
//     emit the single character. This produces compound terms like "选型" or "邮件"
//     that match as units, and shrinks the denominator in ratio-based scoring.
//
// Example: "memax 用什么 email 选型" → ["memax", "用", "email", "选型"]
// (什/么 filtered as stopwords, 用 emitted as single char, 选型 as bigram)
func tokenizeForMatching(query string) []string {
	var terms []string
	var latin strings.Builder
	var cjkBuf []rune

	flushLatin := func() {
		if latin.Len() == 0 {
			return
		}
		w := strings.ToLower(latin.String())
		latin.Reset()
		w = strings.Trim(w, `?!.,;:'"`+"?？。！，、：；\u201c\u201d\u2018\u2019")
		if len(w) >= 2 && !latinStopWords[w] {
			terms = append(terms, w)
		}
	}

	flushCJK := func() {
		if len(cjkBuf) == 0 {
			return
		}
		terms = append(terms, cjkBigrams(cjkBuf, true)...)
		cjkBuf = cjkBuf[:0]
	}

	for _, r := range strings.ToLower(query) {
		if isCJKRune(r) {
			flushLatin()
			cjkBuf = append(cjkBuf, r)
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			flushCJK()
			flushLatin()
		} else {
			flushCJK()
			latin.WriteRune(r)
		}
	}
	flushCJK()
	flushLatin()
	return terms
}

func extractIdentifierTerms(query string) []string {
	raw := strings.Fields(query)
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	identifiers := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.Trim(token, "?!.,;:'\"`()[]{}<>")
		if !looksLikeIdentifier(token) {
			continue
		}
		tokenLower := strings.ToLower(token)
		if _, ok := seen[tokenLower]; ok {
			continue
		}
		seen[tokenLower] = struct{}{}
		identifiers = append(identifiers, tokenLower)
	}
	return identifiers
}

func looksLikeIdentifier(token string) bool {
	if len(token) < 3 {
		return false
	}
	if strings.Contains(token, "_") || strings.Contains(token, "/") || strings.Contains(token, ".") || strings.Contains(token, "-") {
		return true
	}
	hasUpper := false
	hasLower := false
	for _, r := range token {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	if hasUpper && hasLower {
		return true
	}
	return strings.IndexFunc(token, unicode.IsDigit) >= 0
}

// parseTemporalBounds converts ISO-8601 date strings from the distiller into time.Time.
// Dates are parsed in the user's timezone (since the distiller resolved dates in local context).
// This ensures "April 10" in PST becomes the correct UTC range for DB queries.
func parseTemporalBounds(startStr, endStr, timezone string) (start, end time.Time, ok bool) {
	if startStr == "" {
		return time.Time{}, time.Time{}, false
	}
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	s, err := time.ParseInLocation("2006-01-02", startStr, loc)
	if err != nil {
		s, err = time.Parse(time.RFC3339, startStr) // RFC3339 has its own TZ
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
	}
	if endStr == "" {
		// Default: single day range
		return s, s.AddDate(0, 0, 1), true
	}
	e, err := time.ParseInLocation("2006-01-02", endStr, loc)
	if err != nil {
		e, err = time.Parse(time.RFC3339, endStr) // RFC3339 has its own TZ
		if err != nil {
			return s, s.AddDate(0, 0, 1), true
		}
	}
	return s, e, true
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%d months ago", int(d.Hours()/(24*30)))
	}
}
