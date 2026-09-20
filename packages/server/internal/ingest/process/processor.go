package process

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/categorize"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunker"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunkmeta"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/classify"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/extract"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/fileproc"
	ingestformat "github.com/MemaxLabs/memax/packages/server/internal/ingest/format"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/link"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/marker"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/summarize"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/language"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type Processor struct {
	store         store.Store
	events        events.Publisher
	embedder      embed.Embedder
	summarizer    *summarize.Summarizer
	extractor     *extract.Extractor
	categorizer   *categorize.Categorizer
	linkProcessor *link.Processor
	fileProcessor *fileproc.Processor
	formatter     *ingestformat.Formatter
	titleResolver *ingesttitle.Resolver
	objectStore   objectstore.Store
}

func New(store store.Store, publisher events.Publisher, embedder embed.Embedder, summarizer *summarize.Summarizer, extractor *extract.Extractor, categorizer *categorize.Categorizer, linkProcessor *link.Processor, fileProcessor *fileproc.Processor, formatter *ingestformat.Formatter, titleResolver *ingesttitle.Resolver, objectStore objectstore.Store) *Processor {
	return &Processor{
		store:         store,
		events:        publisher,
		embedder:      embedder,
		summarizer:    summarizer,
		extractor:     extractor,
		categorizer:   categorizer,
		linkProcessor: linkProcessor,
		fileProcessor: fileProcessor,
		formatter:     formatter,
		titleResolver: titleResolver,
		objectStore:   objectStore,
	}
}

func (p *Processor) Process(ctx context.Context, memoryID string, ownerID string, req model.PushRequest) error {
	ctx = anthropic.WithTracking(ctx, anthropic.Tracking{
		DistinctID: ownerID,
		Metadata: map[string]any{
			"owner_id":  ownerID,
			"memory_id": memoryID,
			"llm_flow":  "memory_ingest",
		},
	})
	processStart := time.Now()
	slog.InfoContext(ctx, "memory processing started",
		"memory_id", memoryID,
		"owner_id", ownerID,
		"content_type", req.ContentType,
		"content_len", len(req.Content),
		"has_file_ref", req.FileRef != nil,
		"source", req.Source,
		"source_agent", req.SourceAgent,
	)

	if req.Content == "" && req.FileRef != nil && p.objectStore != nil {
		loaded, err := p.loadObjectContent(ctx, req.FileRef, req.ContentType)
		if err != nil {
			return fmt.Errorf("load uploaded object: %w", err)
		}
		req.Content = loaded
		if req.SourcePath == "" {
			req.SourcePath = req.FileRef.Filename
		}
		if req.ContentType == "" {
			req.ContentType = req.FileRef.ContentType
		}
	}

	track := classify.DetectTrack(req.Content, req.ContentType)
	now := time.Now()

	if (req.ContentType == "pdf" || req.ContentType == "image") && p.fileProcessor != nil {
		extractStart := time.Now()
		extractedText, err := p.fileProcessor.ExtractTextContext(ctx, req.Content, req.ContentType)
		if err != nil {
			slog.WarnContext(ctx, "file text extraction failed",
				"error", err, "type", req.ContentType,
				"elapsed_ms", time.Since(extractStart).Milliseconds(),
			)
			extractedText = fmt.Sprintf("[%s file - text extraction failed]", req.ContentType)
		} else {
			slog.InfoContext(ctx, "file text extracted",
				"type", req.ContentType,
				"text_len", len(extractedText),
				"elapsed_ms", time.Since(extractStart).Milliseconds(),
			)
		}
		req.Content = extractedText
	}

	if track == classify.TrackLink && p.linkProcessor != nil {
		linkURL := strings.TrimSpace(req.Content)
		linkStart := time.Now()
		linkResult, err := p.linkProcessor.ProcessContext(ctx, linkURL)
		if err != nil {
			slog.WarnContext(ctx, "link processing failed",
				"url_host", hostFromURL(linkURL),
				"error", err,
				"elapsed_ms", time.Since(linkStart).Milliseconds(),
			)
		} else {
			// Log host only, not the full URL. Some inbound links
			// carry auth tokens, invite codes, or session ids in
			// query strings; we don't want those in Loki for every
			// successful ingest. host_from_url handles malformed
			// input gracefully.
			slog.InfoContext(ctx, "link processed",
				"url_host", hostFromURL(linkURL),
				"url_len", len(linkURL),
				"title_len", len(linkResult.Title),
				"tag_count", len(linkResult.KeyTopics),
				"elapsed_ms", time.Since(linkStart).Milliseconds(),
			)
			req.Title = linkResult.Title
			req.Content = fmt.Sprintf("URL: %s\n\n## Summary\n%s\n\n## Retrieval Hint\n%s", linkURL, linkResult.Summary, linkResult.RetrievalHint)
			req.ContentType = "link"
			req.SourcePath = linkURL
			if len(linkResult.KeyTopics) > 0 {
				req.Tags = linkResult.KeyTopics
			}
		}
	}

	if p.formatter != nil {
		if formatted := p.formatter.NormalizeContext(ctx, req.Content, req.ContentType, req.SourcePath); formatted.Changed {
			slog.InfoContext(ctx, "memory content normalized", "memory_id", memoryID, "reason", formatted.Reason, "content_type", req.ContentType)
			req.Content = formatted.Content
		} else {
			req.Content = formatted.Content
		}
	}

	memory, err := p.store.GetMemory(memoryID, ownerID)
	if err != nil {
		return fmt.Errorf("memory not found: %w", err)
	}
	ctx = anthropic.WithTracking(ctx, anthropic.Tracking{
		Metadata: map[string]any{
			"hub_id": memory.HubID,
		},
	})

	// Look up author name and timezone for categorizer context
	authorName, userTZ := "", "UTC"
	if user, err := p.store.GetUser(ownerID); err == nil && user != nil {
		authorName = user.Name
		if user.DisplayName != "" {
			authorName = user.DisplayName
		}
	}
	hubName, hubSlug := "", ""
	if hub, err := p.store.GetHub(memory.HubID); err == nil && hub != nil {
		hubName = hub.Name
		hubSlug = hub.Slug
	}
	if prefs, err := p.store.GetUserPreferences(ownerID); err == nil && prefs != nil {
		if tz, ok := prefs.Settings["timezone"].(string); ok && tz != "" {
			userTZ = tz
		}
	}

	// Convert now to user's local time so the categorizer resolves "today"/"yesterday" correctly
	localNow := now
	if loc, err := time.LoadLocation(userTZ); err == nil {
		localNow = now.In(loc)
	}

	var suggestedTopicID string
	req.Kind = model.NormalizeMemoryKind(req.Kind)
	req.Stability = model.NormalizeMemoryStability(req.Stability)
	if p.categorizer != nil {
		topicHints := BuildTopicHints(p.store, memory.HubID)
		var result *categorize.ClassifyResult
		if len(topicHints) > 0 {
			result = p.categorizer.ClassifyWithTopicsContext(ctx, req.Title, req.Content, req.Hint, topicHints, localNow, authorName)
		} else {
			result = p.categorizer.ClassifyContext(ctx, req.Title, req.Content, req.Hint, localNow, authorName)
		}
		if result != nil {
			req.Kind = result.Kind
			req.Stability = result.Stability
			if len(result.Tags) > 0 && len(req.Tags) == 0 {
				req.Tags = result.Tags
			}
			suggestedTopicID = result.TopicID
			// Apply extracted event dates — parse in user's timezone, store as UTC
			if len(result.EventDates) > 0 {
				memory.EventDates = parseEventDates(result.EventDates, userTZ)
			}
		}
	}
	cleanContent := strings.ReplaceAll(req.Content, "\x00", "")
	memory.Content = cleanContent
	memory.ContentType = req.ContentType
	memory.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(cleanContent)))
	titleInput := ingesttitle.ResolveInput{
		Title:       req.Title,
		SourcePath:  req.SourcePath,
		Content:     req.Content,
		ContentType: req.ContentType,
		Hint:        req.Hint,
	}
	titleResult := p.resolveTitle(ctx, titleInput, p.summarizer == nil || !summarize.ShouldSummarize(req.Content))
	if titleResult.Title != "" && titleResult.Title != memory.Title {
		slog.InfoContext(ctx, "memory title updated", "memory_id", memoryID, "old_title", memory.Title, "new_title", titleResult.Title, "reason", titleResult.Reason)
		memory.Title = titleResult.Title
	}
	memory.Kind = model.NormalizeMemoryKind(req.Kind)
	memory.Stability = model.NormalizeMemoryStability(req.Stability)
	memory.RetrievalWeight = model.DefaultRetrievalWeight(memory.RetrievalWeight)
	if req.Tags != nil {
		memory.Tags = req.Tags
	}
	if memory.Tags == nil {
		memory.Tags = []string{}
	}
	if req.SourcePath != "" {
		memory.SourcePath = req.SourcePath
	}
	memory.State = "active"
	memory.UpdatedAt = now
	if err := p.store.UpdateMemory(memory); err != nil {
		return fmt.Errorf("update memory: %w", err)
	}

	// User-followup-marker scan (Phase 2a / plan 24). Best-effort:
	// the @TODO / @FIXME / @QUESTION text in the body becomes the
	// `user_followup` dream trigger's evidence. We write the result
	// (matched marker OR empty string) on EVERY ingest so the
	// trigger evaluator can distinguish "scan ran, no marker" from
	// "scan never ran" (NULL — pre-Phase-2a memories that haven't
	// been re-ingested). A scan failure is non-fatal: log and
	// continue, the memory still ingests.
	if scanned := marker.Scan(req.Content); true {
		if err := p.store.SetUserFollowupMarker(ctx, memoryID, ownerID, scanned); err != nil {
			slog.WarnContext(ctx, "user_followup_marker write failed",
				"memory_id", memoryID, "error", err)
		} else if scanned != "" {
			slog.InfoContext(ctx, "user_followup_marker recorded",
				"memory_id", memoryID, "marker_len", len(scanned))
		}
	}

	// Search for related memories in the hub to provide context for the
	// summarizer. This helps produce better summaries for short or
	// context-dependent memories. Requires AllowRelatedContext flag
	// (set by handler based on read permission) and an embedder.
	var relatedContext string
	if req.AllowRelatedContext {
		relatedContext = p.searchRelatedContext(ctx, memory.Title, req.Content, memory.Tags, ownerID, memory.HubID, memoryID)
	}

	// Shadow mode: when ENRICHMENT_SHADOW_MODE=true, run both baseline and
	// enriched summarizer calls, but only persist the baseline. Normal logs
	// get only IDs and lengths (no user content). Detailed shadow data should
	// be reviewed via the restricted enrichment_shadow_log table (not yet
	// implemented — for now, shadow mode verifies candidate retrieval quality
	// and ensures the enriched call does not error).
	shadowMode := os.Getenv("ENRICHMENT_SHADOW_MODE") == "true"
	var shadowBaseline *summarize.Result
	if shadowMode && relatedContext != "" && p.summarizer != nil {
		baseline := p.summarizer.SummarizeWithTitleContext(ctx, memory.Title, req.Content, classificationText(memory), req.Hint)
		enriched := p.summarizer.SummarizeWithRelatedContext(ctx, memory.Title, req.Content, classificationText(memory), req.Hint, relatedContext)
		// Log only IDs, lengths, and whether outputs differ — never user content.
		slog.InfoContext(ctx, "enrichment shadow comparison",
			"memory_id", memoryID,
			"baseline_has_summary", baseline.Summary != "",
			"enriched_has_summary", enriched.Summary != "",
			"titles_differ", baseline.Title != enriched.Title,
			"summaries_differ", baseline.Summary != enriched.Summary,
			"related_context_len", len(relatedContext),
		)
		shadowBaseline = &baseline
		relatedContext = "" // clear so the main path doesn't re-call enriched
	}

	// Run summarization BEFORE chunking so the summary and refined title
	// are available to enrich chunk 0's embedding, improving semantic
	// retrieval for the memory's core topic.
	if p.summarizer != nil {
		var result summarize.Result
		if shadowBaseline != nil {
			// Reuse the baseline from shadow mode — avoids a third LLM call
			// and ensures the persisted result matches the logged baseline.
			result = *shadowBaseline
		} else {
			result = p.summarizer.SummarizeWithRelatedContext(ctx, memory.Title, req.Content, classificationText(memory), req.Hint, relatedContext)
		}
		changed := false
		if result.Summary != "" {
			memory.Summary = result.Summary
			changed = true
		}
		if result.Title != "" {
			candidate := ingesttitle.ResolveGeneratedCandidate(titleInput, result.Title)
			if candidate.Title != "" && candidate.Title != memory.Title && candidate.Reason != "user_preserved" {
				slog.InfoContext(ctx, "memory title updated", "memory_id", memoryID, "old_title", memory.Title, "new_title", candidate.Title, "reason", candidate.Reason)
				memory.Title = candidate.Title
				changed = true
			}
		}
		if changed {
			if err := p.store.UpdateMemory(memory); err != nil {
				slog.WarnContext(ctx, "summary update failed", "memory_id", memoryID, "error", err)
			}
		}
	}

	if suggestedTopicID != "" {
		if err := p.store.AssignMemoryToTopic(memoryID, suggestedTopicID, memory.HubID, model.ConfidenceAutoHigh); err != nil {
			slog.WarnContext(ctx, "inline topic assignment failed", "error", err, "memory_id", memoryID, "topic_id", suggestedTopicID)
		}
	}

	if err := p.store.DeleteChunksByMemory(memoryID, ownerID); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	chunkResults := chunker.ChunkMarkdown(req.Content)
	projectRepo := ""
	if req.ProjectContext != nil {
		projectRepo = req.ProjectContext["repo"]
	}
	tagsText := chunkmeta.TagsText(memory.Tags)
	metadataText := chunkmeta.MetadataText(authorName, hubName, hubSlug, memory.Source, model.EffectiveMemoryAgentSlug(memory), projectRepo)
	chunks := make([]model.Chunk, 0, len(chunkResults))
	for _, cr := range chunkResults {
		// Enrich chunk 0: prepend title + summary and append tags to
		// the hint field. The hint flows into both the embedding text
		// (vector lane) AND the persisted search_text (lexical lanes).
		// This is intentional: tags like "email", "选型" should boost
		// chunk 0 across all retrieval surfaces, not just vectors.
		// tags_text is indexed separately for BM25; the hint
		// duplication reinforces the signal without harming precision
		// because tags are already semantically aligned with the
		// memory content.
		chunkHint := req.Hint
		if cr.Index == 0 {
			var enrichParts []string
			if memory.Title != "" {
				enrichParts = append(enrichParts, memory.Title)
			}
			if memory.Summary != "" {
				enrichParts = append(enrichParts, memory.Summary)
			}
			if len(enrichParts) > 0 {
				prefix := strings.Join(enrichParts, "\n")
				if chunkHint != "" {
					chunkHint = prefix + "\n" + chunkHint
				} else {
					chunkHint = prefix
				}
			}
			if tagsSuffix := chunkmeta.EmbeddingTagsSuffix(memory.Tags); tagsSuffix != "" {
				if chunkHint != "" {
					chunkHint = chunkHint + "\n" + tagsSuffix
				} else {
					chunkHint = tagsSuffix
				}
			}
		}

		lang := language.DetectChunk(cr.HeadingChain, cr.Content, req.Hint)
		chunks = append(chunks, model.Chunk{
			ID:              GenerateID(),
			MemoryID:        memoryID,
			Content:         cr.Content,
			HeadingChain:    cr.HeadingChain,
			ChunkIndex:      cr.Index,
			TokenCount:      cr.TokenCount,
			Language:        lang.Code,
			SearchConfig:    lang.Config,
			Kind:            memory.Kind,
			Stability:       memory.Stability,
			RetrievalWeight: memory.RetrievalWeight,
			Hint:            chunkHint,
			TagsText:        tagsText,
			MetadataText:    metadataText,
			ProjectRepo:     projectRepo,
			CreatedAt:       now,
		})
	}
	embedStart := time.Now()
	p.embedChunks(ctx, chunks)
	slog.InfoContext(ctx, "memory chunks embedded",
		"memory_id", memoryID,
		"chunk_count", len(chunks),
		"elapsed_ms", time.Since(embedStart).Milliseconds(),
	)
	if err := p.store.CreateChunks(chunks); err != nil {
		return fmt.Errorf("create chunks: %w", err)
	}

	if track == classify.TrackExtraction && p.extractor != nil {
		factExtractStart := time.Now()
		facts := p.extractor.ExtractContext(ctx, req.Content, req.Hint)
		slog.InfoContext(ctx, "fact extraction complete",
			"memory_id", memoryID,
			"fact_count", len(facts),
			"elapsed_ms", time.Since(factExtractStart).Milliseconds(),
		)
		inherited := inheritProvenance(memory)
		for _, fact := range facts {
			factID := GenerateID()
			kind := model.NormalizeMemoryKind(fact.KindHint)
			stability := model.MemoryStabilityStable
			if kind == model.MemoryKindEpisodic {
				stability = model.MemoryStabilityVolatile
			}
			factMemory := &model.Memory{
				ID:                             factID,
				HubID:                          memory.HubID,
				OwnerID:                        memory.OwnerID,
				Title:                          GenerateTitle(fact.Content),
				Content:                        fact.Content,
				ContentType:                    "text",
				ContentHash:                    fmt.Sprintf("%x", sha256.Sum256([]byte(fact.Content))),
				Kind:                           kind,
				Stability:                      stability,
				RetrievalWeight:                1.0,
				Boundary:                       memory.Boundary,
				State:                          "active",
				Source:                         "extraction",
				SourceAgent:                    model.EffectiveMemoryAgentSlug(memory),
				AssistedByAgent:                inherited.AssistedByAgent,
				SourcePath:                     memory.ID,
				ProvenanceCreatedByType:        inherited.CreatedByType,
				ProvenanceCreatedBySlug:        inherited.CreatedBySlug,
				ProvenanceCreatedByDisplayName: inherited.CreatedByDisplayName,
				ProvenanceCreatedVia:           inherited.CreatedVia,
				ProvenanceAssistedByAgent:      inherited.AssistedByAgent,
				ProvenanceInitiationType:       inherited.InitiationType,
				ProvenanceAttributionSource:    inherited.AttributionSource,
				Version:                        1,
				CreatedAt:                      now,
				UpdatedAt:                      now,
				AccessedAt:                     now,
			}
			model.NormalizeMemoryProvenanceFields(factMemory)
			factMemory.Provenance = model.BuildMemoryProvenance(factMemory)
			if _, err := p.store.GetMemoryByContentHash(factMemory.ContentHash, ownerID, memory.HubID); err == nil {
				continue
			}
			if err := p.store.CreateMemory(factMemory); err != nil {
				slog.WarnContext(ctx, "fact memory create failed", "memory_id", factID, "error", err)
				continue
			}
			p.publishMemoryChanged(ctx, factMemory)
			factChunksRaw := chunker.ChunkMarkdown(fact.Content)
			factTagsText := chunkmeta.TagsText(factMemory.Tags)
			factMetadataText := chunkmeta.MetadataText(authorName, hubName, hubSlug, factMemory.Source, model.EffectiveMemoryAgentSlug(factMemory), projectRepo)
			factChunks := make([]model.Chunk, 0, len(factChunksRaw))
			for _, cr := range factChunksRaw {
				lang := language.DetectChunk(cr.HeadingChain, cr.Content, "")
				factChunks = append(factChunks, model.Chunk{
					ID:              GenerateID(),
					MemoryID:        factID,
					Content:         cr.Content,
					HeadingChain:    cr.HeadingChain,
					ChunkIndex:      cr.Index,
					TokenCount:      cr.TokenCount,
					Language:        lang.Code,
					SearchConfig:    lang.Config,
					Kind:            factMemory.Kind,
					Stability:       factMemory.Stability,
					RetrievalWeight: factMemory.RetrievalWeight,
					TagsText:        factTagsText,
					MetadataText:    factMetadataText,
					CreatedAt:       now,
				})
			}
			p.embedChunks(ctx, factChunks)
			if err := p.store.CreateChunks(factChunks); err != nil {
				slog.WarnContext(ctx, "fact chunks create failed", "memory_id", factID, "error", err)
			}
		}
	}

	slog.InfoContext(ctx, "memory processing complete",
		"memory_id", memoryID,
		"title", memory.Title,
		"kind", memory.Kind,
		"stability", memory.Stability,
		"content_type", memory.ContentType,
		"tag_count", len(memory.Tags),
		"elapsed_ms", time.Since(processStart).Milliseconds(),
	)
	p.publishMemoryChanged(ctx, memory)
	return nil
}

func inheritProvenance(parent *model.Memory) model.MemoryProvenance {
	if parent == nil {
		return model.MemoryProvenance{
			CreatedByType:     model.MemoryCreatedByHuman,
			CreatedVia:        "extraction",
			InitiationType:    model.MemoryInitiationAgentAutomatic,
			AttributionSource: model.MemoryAttributionSourceInherited,
		}
	}
	model.NormalizeMemoryProvenanceFields(parent)
	return model.MemoryProvenance{
		CreatedByType:        parent.ProvenanceCreatedByType,
		CreatedBySlug:        parent.ProvenanceCreatedBySlug,
		CreatedByDisplayName: parent.ProvenanceCreatedByDisplayName,
		CreatedVia:           "extraction",
		AssistedByAgent:      parent.ProvenanceAssistedByAgent,
		InitiationType:       model.MemoryInitiationAgentAutomatic,
		AttributionSource:    model.MemoryAttributionSourceInherited,
	}
}

func (p *Processor) publishMemoryChanged(ctx context.Context, memory *model.Memory) {
	if memory == nil {
		return
	}
	privateOnly := false
	if memory.Boundary == "private" {
		if hub, err := p.store.GetHub(memory.HubID); err == nil && hub.HubType == "personal" {
			privateOnly = true
		}
	}
	// Diagnostic log lives outside the publisher nil-guard so staging logs
	// can distinguish "this path ran with no publisher" (publisher_enabled=
	// false) from "this path never ran" (no log line at all). Triggered a
	// memory-detail-summary-stale investigation where we couldn't tell from
	// logs alone whether the event fired. Keep until the summary refresh
	// path has a week of production data.
	slog.InfoContext(ctx, "memory.publish",
		"memory_id", memory.ID,
		"state", memory.State,
		"has_summary", memory.Summary != "",
		"summary_len", len(memory.Summary),
		"boundary", memory.Boundary,
		"private_only", privateOnly,
		"publisher_enabled", p.events != nil,
		"site", "processor",
	)
	events.PublishMemoryChangedWithPrivacy(ctx, p.events, memory, memory.OwnerID, privateOnly)
}

func classificationText(memory *model.Memory) string {
	if memory == nil {
		return ""
	}
	return fmt.Sprintf("kind=%s, stability=%s", model.NormalizeMemoryKind(memory.Kind), model.NormalizeMemoryStability(memory.Stability))
}

func (p *Processor) loadObjectContent(ctx context.Context, ref *model.FileRef, contentType string) (string, error) {
	result, err := p.objectStore.Get(ctx, ref.ObjectKey)
	if err != nil {
		return "", err
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return "", fmt.Errorf("read object body: %w", err)
	}

	switch contentType {
	case "pdf", "image":
		return base64.StdEncoding.EncodeToString(data), nil
	default:
		return string(data), nil
	}
}

func (p *Processor) resolveTitle(ctx context.Context, in ingesttitle.ResolveInput, allowLLM bool) ingesttitle.ResolveResult {
	if !allowLLM {
		return ingesttitle.ResolveDeterministic(in)
	}
	return p.titleResolver.ResolveContext(ctx, in)
}

func (p *Processor) embedChunks(ctx context.Context, chunks []model.Chunk) {
	if p.embedder == nil || len(chunks) == 0 {
		return
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		text := c.HeadingChain + "\n" + c.Content
		if c.Hint != "" {
			text = c.Hint + "\n" + text
		}
		texts[i] = text
	}
	embeddings, err := p.embedder.EmbedContext(ctx, texts, "document")
	if err != nil {
		slog.ErrorContext(ctx, "embedding failed", "error", err)
		return
	}
	for i := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			chunks[i].Embedding = embeddings[i]
		}
	}
}

// searchRelatedContext finds semantically similar memories in the same hub
// and formats them as context for the summarizer. Returns an empty string if
// no related context is found or if the search signal is too weak.
//
// This is the core of Related-Context Assisted Summarization. See
// docs/engineering/hub-context-enrichment-design.md for the full design.
func (p *Processor) searchRelatedContext(
	ctx context.Context, title, content string,
	tags []string, ownerID, hubID, memoryID string,
) string {
	if p.embedder == nil {
		return ""
	}

	// Build search text from title + intro. Gate on usable search signal:
	// the combination must yield at least 2 meaningful terms.
	searchText := title
	intro := runesTruncate(content, 300)
	if intro != "" {
		searchText += "\n" + intro
	}
	if !hasMinSearchSignal(searchText, tags) {
		return ""
	}

	embeddings, err := p.embedder.EmbedContext(ctx, []string{searchText}, "query")
	if err != nil || len(embeddings) == 0 || embeddings[0] == nil {
		return ""
	}

	candidates, err := p.store.FindRelatedForEnrichment(
		ctx, embeddings[0], ownerID, []string{hubID}, memoryID, 5,
	)
	if err != nil {
		slog.WarnContext(ctx, "related context search failed", "error", err, "memory_id", memoryID)
		return ""
	}

	// Filter by similarity threshold and cap at 3 candidates.
	const minSimilarity = 0.55
	var related []model.EnrichmentCandidate
	for _, c := range candidates {
		if c.Similarity < minSimilarity {
			continue
		}
		related = append(related, c)
		if len(related) >= 3 {
			break
		}
	}
	if len(related) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Related knowledge in this hub (possibly relevant — ")
	b.WriteString("ignore unless this memory's own content supports the connection):\n")
	for i, r := range related {
		fmt.Fprintf(&b, "%d. \"%s\"", i+1, r.Title)
		if r.Summary != "" {
			fmt.Fprintf(&b, " — %s", r.Summary)
		}
		b.WriteString("\n")
	}

	slog.InfoContext(ctx, "related context found for enrichment",
		"memory_id", memoryID,
		"candidates", len(related),
		"top_similarity", fmt.Sprintf("%.3f", related[0].Similarity),
	)
	return b.String()
}

// runesTruncate truncates s to at most maxRunes runes without cutting
// multi-byte characters (important for CJK content).
func runesTruncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// hasMinSearchSignal returns true if the text + tags contain at least 2
// meaningful terms (non-stop-words, 3+ chars). This gates enrichment on
// usable search signal rather than raw content length.
func hasMinSearchSignal(text string, tags []string) bool {
	terms := 0
	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = strings.Trim(word, "?!.,;:'\"")
		if len(word) >= 3 && !isEnrichmentStopWord(word) {
			terms++
			if terms >= 2 {
				return true
			}
		}
	}
	for _, tag := range tags {
		if len(tag) >= 3 {
			terms++
			if terms >= 2 {
				return true
			}
		}
	}
	return false
}

func isEnrichmentStopWord(w string) bool {
	switch w {
	case "the", "and", "for", "are", "but", "not", "you", "all",
		"can", "had", "her", "was", "one", "our", "out", "has",
		"what", "how", "why", "where", "when", "who", "which",
		"this", "that", "with", "from", "have", "been", "will",
		"they", "were", "said", "each", "about", "their":
		return true
	}
	return false
}

func BuildTopicHints(s store.Store, hubID string) []categorize.TopicHint {
	topics, err := s.ListTopics(hubID)
	if err != nil || len(topics) == 0 {
		return nil
	}

	nameByID := make(map[string]string, len(topics))
	parentByID := make(map[string]*string, len(topics))
	for _, t := range topics {
		nameByID[t.ID] = t.Name
		parentByID[t.ID] = t.ParentID
	}

	getPath := func(t model.Topic) string {
		var parts []string
		cur := t.ParentID
		for cur != nil && len(parts) < 5 {
			parts = append([]string{nameByID[*cur]}, parts...)
			cur = parentByID[*cur]
		}
		return strings.Join(parts, " > ")
	}

	hints := make([]categorize.TopicHint, 0, len(topics))
	for _, t := range topics {
		hints = append(hints, categorize.TopicHint{ID: t.ID, Name: t.Name, Path: getPath(t)})
	}
	return hints
}

func GenerateTitle(content string) string {
	return ingesttitle.GenerateFromContent(content)
}

// parseEventDates converts ISO-8601 date strings from the categorizer into time.Time values.
// Dates are parsed in the user's timezone (since the LLM resolved dates in local context),
// then stored as UTC via Go's time.Time (PostgreSQL TIMESTAMPTZ handles the conversion).
func parseEventDates(dates []string, timezone string) []time.Time {
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	var result []time.Time
	for _, d := range dates {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		// Try ISO-8601 date (YYYY-MM-DD) — parse in user's timezone
		if t, err := time.ParseInLocation("2006-01-02", d, loc); err == nil {
			result = append(result, t)
			continue
		}
		// Try full ISO-8601 datetime (has its own timezone info)
		if t, err := time.Parse(time.RFC3339, d); err == nil {
			result = append(result, t)
		}
	}
	return result
}

func GenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b = []byte(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// hostFromURL returns the host portion of a URL for redacted logging.
// Falls back to "<malformed>" on parse failure. Never returns the
// path or query, which is where sensitive query tokens / session ids
// tend to live on inbound share links.
func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<malformed>"
	}
	return u.Host
}
