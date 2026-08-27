package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/attachments"
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
	ingestprocess "github.com/MemaxLabs/memax/packages/server/internal/ingest/process"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/summarize"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/language"
	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/sanitize"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// maxBodySize is the maximum request body size in bytes.
// Configurable via MAX_BODY_SIZE env var (default: 3MB).
var maxBodySize = func() int64 {
	if v := os.Getenv("MAX_BODY_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 3 * 1024 * 1024 // 3MB
}()

type MemoriesHandler struct {
	store         store.Store
	events        events.Publisher
	embedder      embed.Embedder          // nil = no embeddings, keyword search only
	summarizer    *summarize.Summarizer   // nil = no summaries
	extractor     *extract.Extractor      // nil = no fact extraction
	categorizer   *categorize.Categorizer // nil = no auto-categorization
	linkProcessor *link.Processor         // nil = no link processing
	fileProcessor *fileproc.Processor     // nil = no PDF/image text extraction
	formatter     *ingestformat.Formatter
	titleResolver *ingesttitle.Resolver
	processor     *ingestprocess.Processor
	objectStore   objectstore.Store
	// attachmentSigner issues signed view URLs. Nil = signing disabled,
	// in which case CreateAttachmentViewURL returns 503 and callers
	// fall back to the authenticated blob-download path.
	attachmentSigner *attachments.Signer
	// attachmentViewBaseURL is the absolute origin used when signing
	// view URLs, e.g. "https://api.memax.app". Read once at startup
	// (see SetAttachmentSigner).
	attachmentViewBaseURL string
	enqueue               func(memoryID, ownerID string, req model.PushRequest) // nil = goroutine fallback
	meter                 meterInstance                                         // nil = no memory count enforcement
	hubQuota              hubQuotaResolver                                      // nil = hub memory cap not enforced
	// cache is used for cross-request dedup windows (plan 21 §4.4: the
	// access-tracking handler uses an atomic SetNX so two near-
	// simultaneous accesses don't both increment access_count). Nil =
	// not configured; callers fall open (treat every request as the
	// "winner" of dedup) so a missing Redis doesn't block writes.
	cache cacheInstance
}

// SetAttachmentSigner wires the HMAC signer used to produce view URLs.
// Called once at startup from serverapp/app.go with the value of
// ATTACHMENT_VIEW_SIGNING_KEY + PUBLIC_API_URL. Safe to pass nil — the
// view-url endpoint will then return 503 and clients fall back to the
// download path.
func (h *MemoriesHandler) SetAttachmentSigner(signer *attachments.Signer, baseURL string) {
	h.attachmentSigner = signer
	h.attachmentViewBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// meterInstance is a minimal interface for the meter APIs used by
// memory handlers. Avoids importing the meter package (which would
// create an import cycle).
type meterInstance interface {
	ReserveMemoryCount(ctx context.Context, ownerID string, limit int) (bool, int)
	CommitMemoryCount(ctx context.Context, ownerID string)
	RollbackMemoryCount(ctx context.Context, ownerID string)
	AdjustMemoryCount(ctx context.Context, ownerID string, delta int)

	// Hub-scoped memory count — enforces the hub's subscription-plan
	// memory_limit independently of the owner cap. hubID is always the
	// push target hub; empty / non-team hubs are skipped by the meter.
	ReserveHubMemoryCount(ctx context.Context, hubID string, limit int) (bool, int)
	CommitHubMemoryCount(ctx context.Context, hubID string)
	RollbackHubMemoryCount(ctx context.Context, hubID string)
	AdjustHubMemoryCount(ctx context.Context, hubID string, delta int)

	// Storage bytes (lifetime cumulative). Reserve/Commit/Rollback match
	// the memory-count pattern: Reserve at persist time, Commit after DB
	// insert succeeds, Rollback when the insert fails. Adjust handles
	// deletes with a negative delta.
	ReserveStorageBytes(ctx context.Context, ownerID string, bytes int64, limit int64) (bool, int64)
	CommitStorageBytes(ctx context.Context, ownerID string, bytes int64)
	RollbackStorageBytes(ctx context.Context, ownerID string, bytes int64)
	AdjustStorageBytes(ctx context.Context, ownerID string, delta int64)
}

// hubQuotaResolver resolves the memory_limit for a hub's subscription
// plan (not a user's entitlement). Separate interface so the handler
// doesn't have to import planresolver.
type hubQuotaResolver interface {
	GetHubMemoryLimit(ctx context.Context, hubID string) int
}

// cacheInstance is the minimal cache surface the memories handler
// uses — SetNX for atomic dedup, Del to release the dedup lock when
// the protected operation fails. Defined as a local interface so the
// handler doesn't require a non-nil `cache.Cache` for tests that
// don't exercise the dedup path. The real `*cache.RedisCache` (and
// the test stubs in this package) all satisfy this interface.
type cacheInstance interface {
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}

// SetMeter wires the metering service for memory count enforcement.
func (h *MemoriesHandler) SetMeter(m meterInstance) {
	h.meter = m
}

// SetHubQuotaResolver wires the resolver used to look up a hub's
// plan-level memory_limit at push time. Optional — without it, the
// hub memory cap is not enforced (fail-open, same as the rest of the
// meter's degraded-mode behavior).
func (h *MemoriesHandler) SetHubQuotaResolver(r hubQuotaResolver) {
	h.hubQuota = r
}

// SetCache wires the dedup cache used by `TrackAccessed` (plan 21
// §4.4). Pass nil to disable atomic dedup; the handler then increments
// on every request without a server-side window. The client-side
// sessionStorage tracker (`useTrackMemoryAccessed` in use-memories.ts)
// still absorbs most of the duplicate burden in that mode, but two
// tabs / a private-mode tab that bypasses the client dedup will both
// count without a configured cache. Codex 5.5 L-finding (comment
// drift): there is no in-DB updated_at floor today.
func (h *MemoriesHandler) SetCache(c cacheInstance) {
	h.cache = c
}

func NewMemoriesHandler(s store.Store, publisher events.Publisher, embedder embed.Embedder, summarizer *summarize.Summarizer, extractor *extract.Extractor, categorizer *categorize.Categorizer, linkProcessor *link.Processor, fileProcessor *fileproc.Processor, formatter *ingestformat.Formatter, titleResolver *ingesttitle.Resolver, objectStore objectstore.Store) *MemoriesHandler {
	return &MemoriesHandler{
		store:         s,
		events:        publisher,
		embedder:      embedder,
		summarizer:    summarizer,
		extractor:     extractor,
		categorizer:   categorizer,
		linkProcessor: linkProcessor,
		fileProcessor: fileProcessor,
		formatter:     formatter,
		titleResolver: titleResolver,
		processor:     ingestprocess.New(s, publisher, embedder, summarizer, extractor, categorizer, linkProcessor, fileProcessor, formatter, titleResolver, objectStore),
		objectStore:   objectStore,
	}
}

// SetEnqueue sets the function used to enqueue background memory processing.
// When set, Create and ProcessMemoryBackground use the queue instead of goroutines.
func (h *MemoriesHandler) SetEnqueue(fn func(memoryID, ownerID string, req model.PushRequest)) {
	h.enqueue = fn
}

func (h *MemoriesHandler) publishMemoryChanged(ctx context.Context, memory *model.Memory, actorID string) {
	if memory == nil {
		return
	}
	privateOnly := false
	if memory.Boundary == "private" {
		if hub, err := h.store.GetHub(memory.HubID); err == nil && hub.HubType == "personal" {
			privateOnly = true
		}
	}
	// Diagnostic log outside publisher nil-guard — see processor.go's
	// matching publishMemoryChanged for rationale. Parallel log sites
	// let a single grep answer which publish fired (push vs processor)
	// for any given memory id.
	slog.InfoContext(ctx, "memory.publish",
		"memory_id", memory.ID,
		"state", memory.State,
		"has_summary", memory.Summary != "",
		"summary_len", len(memory.Summary),
		"boundary", memory.Boundary,
		"private_only", privateOnly,
		"publisher_enabled", h.events != nil,
		"site", "handler",
	)
	events.PublishMemoryChangedWithPrivacy(ctx, h.events, memory, actorID, privateOnly)
}

// embedChunks generates and attaches embeddings to chunks if an embedder is configured.
func (h *MemoriesHandler) embedChunks(ctx context.Context, chunks []model.Chunk) {
	if h.embedder == nil || len(chunks) == 0 {
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
	embeddings, err := h.embedder.EmbedContext(ctx, texts, "document")
	if err != nil {
		slog.Error("embedding failed, falling back to keyword search", "error", err)
		return
	}
	for i := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			chunks[i].Embedding = embeddings[i]
		}
	}
}

func (h *MemoriesHandler) syncChunkMetadata(memory *model.Memory, ownerID string) error {
	chunks, err := h.store.GetChunksByMemory(memory.ID, ownerID)
	if err != nil {
		return err
	}
	tagsText := chunkmeta.TagsText(memory.Tags)
	metadataText := h.chunkMetadataText(memory)
	projectRepo := ""
	if memory.ProjectContext != nil {
		projectRepo = memory.ProjectContext["repo"]
	}
	for i := range chunks {
		chunks[i].Kind = memory.Kind
		chunks[i].Stability = memory.Stability
		chunks[i].RetrievalWeight = memory.RetrievalWeight
		chunks[i].Hint = memory.Hint
		chunks[i].TagsText = tagsText
		chunks[i].MetadataText = metadataText
		chunks[i].ProjectRepo = projectRepo
		if err := h.store.UpdateChunk(&chunks[i]); err != nil {
			return err
		}
	}
	return nil
}

func (h *MemoriesHandler) chunkMetadataText(memory *model.Memory) string {
	authorName := memory.AuthorName
	if authorName == "" {
		if user, err := h.store.GetUser(memory.OwnerID); err == nil && user != nil {
			authorName = user.Name
			if user.DisplayName != "" {
				authorName = user.DisplayName
			}
		}
	}
	hubName, hubSlug := "", ""
	if hub, err := h.store.GetHub(memory.HubID); err == nil && hub != nil {
		hubName = hub.Name
		hubSlug = hub.Slug
	}
	projectRepo := ""
	if memory.ProjectContext != nil {
		projectRepo = memory.ProjectContext["repo"]
	}
	return chunkmeta.MetadataText(authorName, hubName, hubSlug, memory.Source, model.EffectiveMemoryAgentSlug(memory), projectRepo)
}

func memoryClassification(memory *model.Memory) string {
	if memory == nil {
		return ""
	}
	return fmt.Sprintf("kind=%s, stability=%s", model.NormalizeMemoryKind(memory.Kind), model.NormalizeMemoryStability(memory.Stability))
}

func (h *MemoriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	var req model.PushRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("Request body exceeds %dMB limit. Set MAX_BODY_SIZE env var to increase.", maxBodySize/(1024*1024)))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	originalContent := req.Content
	tagsProvided := req.Tags != nil

	if req.Content == "" && req.FileRef == nil {
		writeError(w, http.StatusBadRequest, "missing_content", "Content is required")
		return
	}
	// Secret gate (E2): memories are chunked, embedded and recalled
	// VERBATIM across every connected agent — a credential that enters
	// a memory becomes a replay device. Reject, never silently redact:
	// the pusher must know their paste carried a key. Covers REST, SDK
	// and CLI (all funnel here); the MCP push path gates separately.
	if hits := secrets.DetectCredentials(req.Content); len(hits) > 0 {
		writeError(w, http.StatusBadRequest, "secret_detected",
			fmt.Sprintf("Content appears to contain a credential (%s). Memories are recalled verbatim across your agents — store secrets in a secret manager and push a reference instead.", strings.Join(hits, ", ")))
		return
	}
	hubID := resolvedHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub_context", "No active hub could be resolved for this request")
		return
	}
	role, _ := h.store.GetHubMemberRole(hubID, ownerID)
	if !canWriteMemories(role) {
		hub, err := h.store.GetHub(hubID)
		if err != nil || hub.OwnerID != ownerID {
			writeError(w, http.StatusForbidden, "no_write_access", "Write access to this hub is required")
			return
		}
	}
	// Related-context enrichment requires read access to the destination hub.
	// Write access alone is not sufficient — enrichment is an implicit read.
	// If an explicit credential (API key/OAuth) is present, check its grant.
	// Otherwise (web session), fall back to hub role.
	if authCtx := GetAuthContext(r); authCtx != nil {
		req.AllowRelatedContext = authCtx.PermissionsByHub[hubID].Has(PermMemoryRead)
	} else {
		req.AllowRelatedContext = canReadMemories(role)
	}
	if req.FileRef != nil {
		if h.objectStore == nil {
			writeError(w, http.StatusServiceUnavailable, "uploads_unavailable", "Object storage is not configured")
			return
		}
		if strings.TrimSpace(req.FileRef.ObjectKey) == "" || strings.TrimSpace(req.FileRef.Filename) == "" {
			writeError(w, http.StatusBadRequest, "invalid_file_ref", "file_ref.object_key and file_ref.filename are required")
			return
		}
		// Memories must use keys issued under the memory_attachment
		// purpose (which carries the per-plan cap). VerifyUploadKey
		// pins the prefix so a caller can't repurpose a key issued
		// under a different purpose to bypass the plan cap.
		if err := VerifyUploadKey(req.FileRef.ObjectKey, ownerID, UploadPurposeMemoryAttachment); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_file_ref", err.Error())
			return
		}
	}

	// Defaults
	if req.ContentType == "" {
		req.ContentType = defaultContentType(req.FileRef, req.SourcePath)
	}
	if req.Source == "" {
		req.Source = "api"
	}
	// Server-side XSS sanitization. Runs AFTER content_type is resolved
	// so we know whether to apply the HTML-body policy. Every storage
	// path funnels through Create/Update, so this is the single point
	// where a safe `content` / `title` / `hint` invariant is enforced —
	// downstream renderers (web, CLI, third-party API consumers) don't
	// need to re-derive sanitization.
	sanitizePushRequest(&req)
	provenance, sourceAgent, claimRejected, provErr := resolveMemoryProvenance(h.store, ownerID, req, r)
	if provErr != nil {
		if valErr, ok := asAttributionValidationError(provErr); ok {
			writeError(w, http.StatusBadRequest, valErr.code, valErr.message)
			return
		}
		writeError(w, http.StatusBadRequest, "attribution_conflict", "Request agent attribution conflicts with authenticated agent identity")
		return
	}
	if claimRejected {
		setMemaxWarningHeader(w, memaxWarningClaimRejected)
	} else if requestNeedsReconnectWarning(r) {
		setMemaxWarningHeader(w, memaxWarningReconnectNeeded)
	}
	req.SourceAgent = sourceAgent
	req.AssistedByAgent = provenance.AssistedByAgent
	req.InitiationType = provenance.InitiationType
	if req.SourceAgent != "" && provenance.AttributionSource == model.MemoryAttributionSourceAuth {
		go EnsureConnectedAgent(h.store, ownerID, req.SourceAgent)
	}
	// Auto-heal: when an API-key principal claimed an agent via body,
	// pin the key's agent_name so subsequent pushes go through the auth
	// path (and connected_agents stays in sync). Rejects with
	// attribution_conflict if another concurrent claim already won.
	if conflict, _ := tryAutoHealAPIKeyAgent(r.Context(), h.store, r, req.SourceAgent, provenance.AttributionSource); conflict {
		writeError(w, http.StatusBadRequest, "attribution_conflict", "Request agent attribution conflicts with authenticated agent identity")
		return
	}
	if req.Title == "" && req.FileRef != nil {
		req.Title = req.FileRef.Filename
	}
	req.Title = ingesttitle.PrepareIncoming(req.Title, req.SourcePath, req.Content, req.ContentType)
	setUsageEventSummary(r, req.Title)
	req.Kind = model.NormalizeMemoryKind(req.Kind)
	req.Stability = model.NormalizeMemoryStability(req.Stability)
	if req.Tags == nil {
		req.Tags = []string{}
	}

	// Track whether the user explicitly provided a hint before we add
	// system provenance. Related-context enrichment skips when the user
	// provided their own hint (respecting user intent).
	userProvidedHint := strings.TrimSpace(req.Hint) != ""

	// Build ingestion hint: combine system context with user-provided hint
	req.Hint = buildHint(req.Hint, req.Source, req.SourcePath, req.ProjectContext)

	// Only allow enrichment when the user did NOT provide an explicit hint.
	// System provenance hints ("Pushed via CLI") should not block enrichment.
	if userProvidedHint {
		req.AllowRelatedContext = false
	}

	if h.formatter != nil {
		req.Content = h.formatter.Prepare(req.Content, req.ContentType, req.SourcePath).Content
	}

	// Any memory backed by a file_ref but missing inline content must enter the
	// async processing path first. Otherwise the API would return an "active"
	// memory whose content is still empty and only exists in object storage.
	needsAsyncProcessing := (req.FileRef != nil && strings.TrimSpace(req.Content) == "") ||
		req.ContentType == "pdf" || req.ContentType == "image" ||
		classify.DetectTrack(req.Content, req.ContentType) == classify.TrackLink

	hash := deriveInitialContentHash(req)
	now := time.Now()

	// Dedup: check if a memory with the same source_path already exists (scoped to owner)
	if req.SourcePath != "" {
		if existing, err := h.store.GetMemoryBySourcePath(req.SourcePath, ownerID, hubID); err == nil {
			if req.Content == "" && req.FileRef != nil {
				existing.ContentType = req.ContentType
				existing.ContentHash = hash
				existing.State = "processing"
				existing.UpdatedAt = now
				existing.Version++
				if req.Title != "" {
					existing.Title = req.Title
				}
				existing.Kind = req.Kind
				existing.Stability = req.Stability
				existing.RetrievalWeight = model.DefaultRetrievalWeight(existing.RetrievalWeight)
				existing.Source = req.Source
				existing.SourceAgent = req.SourceAgent
				existing.AssistedByAgent = req.AssistedByAgent
				existing.Provenance = provenance
				existing.ProvenanceCreatedByType = provenance.CreatedByType
				existing.ProvenanceCreatedBySlug = provenance.CreatedBySlug
				existing.ProvenanceCreatedByDisplayName = provenance.CreatedByDisplayName
				existing.ProvenanceCreatedVia = provenance.CreatedVia
				existing.ProvenanceAssistedByAgent = provenance.AssistedByAgent
				existing.ProvenanceInitiationType = provenance.InitiationType
				existing.ProvenanceAttributionSource = provenance.AttributionSource
				if tagsProvided {
					existing.Tags = req.Tags
				}
				if err := h.store.UpdateMemory(existing); err != nil {
					writeError(w, http.StatusInternalServerError, "store_error", err.Error())
					return
				}
				h.publishMemoryChanged(r.Context(), existing, ownerID)
				if err := h.persistOriginalAttachment(r.Context(), existing, req, originalContent); err != nil {
					slog.Warn("failed to persist original attachment on object update", "memory_id", existing.ID, "error", err)
				}
				meterctx.CommitFromContext(r.Context()) // push quota: commit (COGS for file update pipeline)
				h.returnMemory(w, http.StatusOK, existing.ID, ownerID)
				if h.enqueue != nil {
					h.enqueue(existing.ID, ownerID, req)
				} else {
					go h.processMemory(existing.ID, ownerID, req)
				}
				return
			}

			if existing.ContentHash == hash {
				// Same content, same path — skip (idempotent)
				slog.Info("memory skipped (unchanged)", "id", existing.ID, "source_path", req.SourcePath)
				meterctx.CommitFromContext(r.Context()) // push quota: commit (COGS for dedup check)
				h.returnMemory(w, http.StatusOK, existing.ID, ownerID)
				return
			}
			// Same path, different content — update in place
			existing.Content = req.Content
			existing.ContentHash = hash
			existing.UpdatedAt = now
			existing.Version++
			if req.Title != "" {
				existing.Title = req.Title
			}
			existing.Kind = req.Kind
			existing.Stability = req.Stability
			existing.RetrievalWeight = model.DefaultRetrievalWeight(existing.RetrievalWeight)
			existing.Hint = req.Hint
			existing.Source = req.Source
			existing.SourceAgent = req.SourceAgent
			existing.AssistedByAgent = req.AssistedByAgent
			existing.Provenance = provenance
			existing.ProvenanceCreatedByType = provenance.CreatedByType
			existing.ProvenanceCreatedBySlug = provenance.CreatedBySlug
			existing.ProvenanceCreatedByDisplayName = provenance.CreatedByDisplayName
			existing.ProvenanceCreatedVia = provenance.CreatedVia
			existing.ProvenanceAssistedByAgent = provenance.AssistedByAgent
			existing.ProvenanceInitiationType = provenance.InitiationType
			existing.ProvenanceAttributionSource = provenance.AttributionSource
			if tagsProvided {
				existing.Tags = req.Tags
			}
			if err := h.store.UpdateMemory(existing); err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
			if err := h.persistOriginalAttachment(r.Context(), existing, req, originalContent); err != nil {
				slog.Warn("failed to persist original attachment on update", "memory_id", existing.ID, "error", err)
			}

			// Re-chunk
			if err := h.store.DeleteChunksByMemory(existing.ID, ownerID); err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
			chunkResults := chunker.ChunkMarkdown(req.Content)
			existingRepo := ""
			if existing.ProjectContext != nil {
				existingRepo = existing.ProjectContext["repo"]
			}
			var chunks []model.Chunk
			tagsText := chunkmeta.TagsText(existing.Tags)
			metadataText := h.chunkMetadataText(existing)
			for _, cr := range chunkResults {
				lang := language.DetectChunk(cr.HeadingChain, cr.Content, existing.Hint)
				chunks = append(chunks, model.Chunk{
					ID:              generateID(),
					MemoryID:        existing.ID,
					Content:         cr.Content,
					HeadingChain:    cr.HeadingChain,
					ChunkIndex:      cr.Index,
					TokenCount:      cr.TokenCount,
					Language:        lang.Code,
					SearchConfig:    lang.Config,
					Kind:            existing.Kind,
					Stability:       existing.Stability,
					RetrievalWeight: existing.RetrievalWeight,
					Hint:            existing.Hint,
					TagsText:        tagsText,
					MetadataText:    metadataText,
					ProjectRepo:     existingRepo,
					CreatedAt:       now,
				})
			}
			h.embedChunks(r.Context(), chunks)
			if err := h.store.CreateChunks(chunks); err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}

			if h.summarizer != nil {
				llmCtx := anthropic.WithTracking(r.Context(), anthropic.Tracking{
					DistinctID: ownerID,
					Metadata: map[string]any{
						"owner_id":  ownerID,
						"memory_id": existing.ID,
						"hub_id":    existing.HubID,
						"llm_flow":  "memory_update",
					},
				})
				result := h.summarizer.SummarizeWithTitleContext(llmCtx, existing.Title, req.Content, memoryClassification(existing), existing.Hint)
				if applySummaryResult(existing, req.Content, result) {
					if err := h.store.UpdateMemory(existing); err != nil {
						slog.Warn("summary update failed", "memory_id", existing.ID, "error", err)
					}
				}
			}

			slog.Info("memory updated (content changed)", "id", existing.ID, "source_path", req.SourcePath, "version", existing.Version)
			h.publishMemoryChanged(r.Context(), existing, ownerID)
			meterctx.CommitFromContext(r.Context()) // push quota: commit (COGS for update pipeline)
			h.returnMemory(w, http.StatusOK, existing.ID, ownerID)
			return
		}
	}

	// Dedup: check by content hash within the target hub only.
	// Creating the same content in another hub is valid and should not collapse
	// into a personal/team memory elsewhere.
	if existing, err := h.store.GetMemoryByContentHash(hash, ownerID, hubID); err == nil {
		slog.Info("memory skipped (duplicate content)", "id", existing.ID, "hash", hash[:12])
		meterctx.CommitFromContext(r.Context()) // push quota: commit (COGS for hash check)
		h.returnMemory(w, http.StatusOK, existing.ID, ownerID)
		return
	}

	// Memory count enforcement (handler-managed, not middleware).
	// Only reserve on confirmed new-memory path (dedup/update paths above already returned).
	var attachmentBytes int64
	if req.FileRef != nil {
		attachmentBytes = req.FileRef.SizeBytes
	}
	// hubMemoryReserved tracks whether the hub-level reservation landed
	// so the CreateMemory rollback path can release it too. The hub
	// path only runs for team-hub targets; personal hubs are skipped
	// because they have no subscription plan.
	hubMemoryReserved := false
	if h.meter != nil {
		limits := meterctx.UserLimitsFromContext(r.Context())
		if limits != nil {
			allowed, current := h.meter.ReserveMemoryCount(r.Context(), ownerID, limits.MemoryLimit)
			if !allowed {
				WriteErrorWithDetails(w, http.StatusPaymentRequired, "memory_limit_reached",
					fmt.Sprintf("Memory limit reached (%d/%d). Upgrade your plan for more storage.", current, limits.MemoryLimit),
					map[string]any{
						"current": current,
						"limit":   limits.MemoryLimit,
						"plan":    limits.PlanID,
					})
				return
			}
			// Hub-level memory cap (team hubs only). Runs AFTER the
			// owner check so the owner always gets a clear signal first
			// if they've personally run out. If the hub cap blocks, the
			// error explicitly says so — a member of a full team hub
			// needs to know it's the hub that's out of room, not them.
			//
			// We refetch the hub here to check HubType; hubID alone
			// doesn't tell us whether it's team or personal and the
			// personal hub's own quota path is already covered by the
			// owner check above.
			var targetHub *model.Hub
			if hubID != "" {
				targetHub, _ = h.store.GetHub(hubID)
			}
			if h.hubQuota != nil && targetHub != nil && targetHub.HubType == "team" {
				// Frozen-hub short-circuit. Covers (a) inactive
				// subscription and (b) grace-period expired. Runs
				// BEFORE reserving so we never increment a counter
				// for a hub that won't accept the memory anyway.
				// Frozen hubs return 402 with a distinct error so
				// the client can show a resolve-to-resume message.
				//
				// Uses AnyStatus read so cancelled / past_due rows
				// still light up the freeze; GetActiveHubSubscription
				// would return nil and hide those states.
				sub, _ := h.store.GetHubSubscriptionAnyStatus(r.Context(), hubID)
				if model.IsHubFrozen(sub, time.Now().UTC()) {
					h.meter.RollbackMemoryCount(r.Context(), ownerID)
					WriteErrorWithDetails(w, http.StatusPaymentRequired, "hub_frozen",
						"Hub is frozen. The hub owner needs to resolve the over-limit state (upgrade the plan or delete memories) or restore the subscription before pushes can resume.",
						map[string]any{
							"hub_id": hubID,
						})
					return
				}
				hubLimit := h.hubQuota.GetHubMemoryLimit(r.Context(), hubID)
				allowedHub, currentHub := h.meter.ReserveHubMemoryCount(r.Context(), hubID, hubLimit)
				if !allowedHub {
					h.meter.RollbackMemoryCount(r.Context(), ownerID)
					// Start (or leave set) the grace-period clock for
					// this hub. The store method is idempotent — an
					// already-set over_limit_since isn't overwritten,
					// so repeated rejections don't reset the window.
					// transitioned=true is the edge we notify on:
					// fire hub_over_limit to the hub owner + admins
					// exactly once per grace window, not on every
					// rejected push.
					transitioned, err := h.store.SetHubOverLimit(r.Context(), hubID, time.Now().UTC())
					if err != nil {
						slog.Warn("set hub over_limit_since failed",
							"hub_id", hubID, "error", err)
					}
					if transitioned {
						now := time.Now().UTC()
						payload := buildHubQuotaPayload(r.Context(), h.store, targetHub, "", hubLimit)
						payload.MemoryCount = currentHub + 1
						payload.OverLimitSince = &now
						// Cycle id = the just-set over_limit_since. If the hub
						// restores and later goes over again, the new cycle
						// uses a new over_limit_since, so the source_id is
						// distinct and a fresh notification fires.
						dispatchHubQuotaNotification(r.Context(), h.store, h.events,
							targetHub, model.NotificationKindHubOverLimit,
							hubQuotaOverLimit, now, payload)
					}
					WriteErrorWithDetails(w, http.StatusPaymentRequired, "hub_memory_limit_reached",
						fmt.Sprintf("Hub is at capacity (%d/%d memories). The hub owner needs to upgrade the hub plan or delete memories before new ones can be pushed.", currentHub, hubLimit),
						map[string]any{
							"current":    currentHub,
							"limit":      hubLimit,
							"hub_id":     hubID,
							"cap_source": "hub",
						})
					return
				}
				hubMemoryReserved = true
			}
			// Storage-bytes enforcement (only for file-backed memories).
			// Reserve AFTER memory-count so the two quotas fail in the
			// order the user most cares about (row count first, then
			// bytes) and so we only have to roll back one on this
			// branch. If storage reserve fails, roll back the memory
			// count we just took and delete the orphaned blob.
			if attachmentBytes > 0 {
				allowed, currentBytes := h.meter.ReserveStorageBytes(r.Context(), ownerID, attachmentBytes, limits.StorageBytesLimit)
				if !allowed {
					h.meter.RollbackMemoryCount(r.Context(), ownerID)
					if hubMemoryReserved {
						h.meter.RollbackHubMemoryCount(r.Context(), hubID)
					}
					if h.objectStore != nil && req.FileRef != nil && req.FileRef.ObjectKey != "" {
						// Best-effort orphan cleanup. If this fails,
						// the bucket lifecycle rule for unreferenced
						// keys handles it eventually.
						_ = h.objectStore.Delete(r.Context(), req.FileRef.ObjectKey)
					}
					WriteErrorWithDetails(w, http.StatusPaymentRequired, "storage_bytes_limit_reached",
						fmt.Sprintf("Storage limit reached (%d / %d bytes). Upgrade your plan or delete old attachments.", currentBytes+attachmentBytes, limits.StorageBytesLimit),
						map[string]any{
							"current_bytes": currentBytes,
							"claimed_bytes": attachmentBytes,
							"limit_bytes":   limits.StorageBytesLimit,
							"plan":          limits.PlanID,
						})
					return
				}
			}
		}
	}

	memoryID := generateID()

	// Set initial state: "processing" for heavy content, "active" for text
	initialState := "active"
	if needsAsyncProcessing {
		initialState = "processing"
	}

	// Normalize project context
	projCtx := req.ProjectContext
	if projCtx == nil {
		projCtx = map[string]string{}
	}

	memory := &model.Memory{
		ID:                             memoryID,
		HubID:                          hubID,
		OwnerID:                        ownerID,
		Title:                          req.Title,
		Content:                        req.Content,
		ContentType:                    req.ContentType,
		ContentHash:                    hash,
		Hint:                           req.Hint,
		Kind:                           req.Kind,
		Stability:                      req.Stability,
		RetrievalWeight:                1.0,
		Tags:                           req.Tags,
		Boundary:                       "private",
		State:                          initialState,
		Source:                         req.Source,
		SourceAgent:                    req.SourceAgent,
		AssistedByAgent:                req.AssistedByAgent,
		SourcePath:                     req.SourcePath,
		Provenance:                     provenance,
		ProvenanceCreatedByType:        provenance.CreatedByType,
		ProvenanceCreatedBySlug:        provenance.CreatedBySlug,
		ProvenanceCreatedByDisplayName: provenance.CreatedByDisplayName,
		ProvenanceCreatedVia:           provenance.CreatedVia,
		ProvenanceAssistedByAgent:      provenance.AssistedByAgent,
		ProvenanceInitiationType:       provenance.InitiationType,
		ProvenanceAttributionSource:    provenance.AttributionSource,
		HubReason:                      strings.TrimSpace(req.HubReason),
		ProjectContext:                 projCtx,
		BatchID:                        req.BatchID,
		Version:                        1,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
	}

	if err := h.store.CreateMemory(memory); err != nil {
		// Roll back all reservations if DB insert fails.
		if h.meter != nil {
			h.meter.RollbackMemoryCount(r.Context(), ownerID)
			if hubMemoryReserved {
				h.meter.RollbackHubMemoryCount(r.Context(), hubID)
			}
			if attachmentBytes > 0 {
				h.meter.RollbackStorageBytes(r.Context(), ownerID, attachmentBytes)
			}
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Commit reservations — DB insert succeeded.
	if h.meter != nil {
		h.meter.CommitMemoryCount(r.Context(), ownerID)
		if hubMemoryReserved {
			h.meter.CommitHubMemoryCount(r.Context(), hubID)
		}
		if attachmentBytes > 0 {
			h.meter.CommitStorageBytes(r.Context(), ownerID, attachmentBytes)
		}
	}
	h.publishMemoryChanged(r.Context(), memory, ownerID)

	if err := h.persistOriginalAttachment(r.Context(), memory, req, originalContent); err != nil {
		slog.Warn("failed to persist original attachment", "memory_id", memoryID, "error", err)
	}

	slog.Info("memory created", "id", memoryID, "title", memory.Title, "state", initialState, "async", needsAsyncProcessing)
	trackRequest(r, "api.memories.create", map[string]any{"memory_id": memoryID, "kind": memory.Kind, "stability": memory.Stability, "source": req.Source, "content_type": req.ContentType})

	// Commit push quota — operation succeeded
	meterctx.CommitFromContext(r.Context())

	// Return canonical representation with author JOIN + topic IDs.
	h.returnMemory(w, http.StatusCreated, memoryID, ownerID)

	// Plan 18 onboarding tick used to fire here via the recorder.
	// Now the materializer at internal/onboarding/materialize.go
	// computes first_memory / five_memories completion lazily on
	// notification read from the memories table, so the SSE
	// memory.changed → ["notifications"] invalidate path is the
	// only sync chain needed. No write to payload here.

	// Background processing — via queue if available, goroutine fallback
	if h.enqueue != nil {
		h.enqueue(memoryID, ownerID, req)
	} else {
		go h.processMemory(memoryID, ownerID, req)
	}
}

// processMemory runs all heavy processing in the background after the memory is stored.
// This includes: file extraction (OCR), link processing, auto-classification,
// chunking, embedding, summarization, and fact extraction.
// Updates the memory state from "processing" to "active" when done.

// ProcessMemoryBackground enqueues memory processing via queue or runs directly.
// Used by MCP handler.
func (h *MemoriesHandler) ProcessMemoryBackground(memoryID string, ownerID string, req model.PushRequest) {
	if h.enqueue != nil {
		h.enqueue(memoryID, ownerID, req)
	} else {
		go h.processMemory(memoryID, ownerID, req)
	}
}

// FallbackProcessMemory runs memory processing directly (goroutine fallback when queue fails).
func (h *MemoriesHandler) FallbackProcessMemory(memoryID string, ownerID string, req model.PushRequest) {
	h.processMemory(memoryID, ownerID, req)
}

func (h *MemoriesHandler) processMemory(memoryID string, ownerID string, req model.PushRequest) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("processMemory panicked", "id", memoryID, "panic", r)
			if mem, err := h.store.GetMemory(memoryID, ownerID); err == nil && mem.State == "processing" {
				mem.State = "active"
				h.store.UpdateMemory(mem)
			}
		}
	}()

	ctx := anthropic.WithTracking(context.Background(), anthropic.Tracking{
		DistinctID: ownerID,
		Metadata: map[string]any{
			"owner_id":  ownerID,
			"memory_id": memoryID,
			"llm_flow":  "memory_ingest",
		},
	})
	if err := h.processor.Process(ctx, memoryID, ownerID, req); err != nil {
		slog.Error("processMemory failed", "id", memoryID, "error", err)
		h.finalizeFailedProcessing(memoryID, ownerID, req, err)
	}
}

func (h *MemoriesHandler) finalizeFailedProcessing(memoryID string, ownerID string, req model.PushRequest, cause error) {
	mem, err := h.store.GetMemory(memoryID, ownerID)
	if err != nil {
		slog.Warn("failed memory processing could not load memory", "memory_id", memoryID, "error", err, "cause", cause)
		return
	}
	if mem.State != "processing" {
		return
	}
	if mem.Content == "" {
		mem.Content = req.Content
	}
	if mem.ContentType == "" {
		mem.ContentType = req.ContentType
	}
	if mem.Summary == "" {
		mem.Summary = fmt.Sprintf("Processing failed: %v", cause)
	}
	mem.State = "active"
	mem.UpdatedAt = time.Now()
	if err := h.store.UpdateMemory(mem); err != nil {
		slog.Warn("failed to mark memory active after processing failure", "memory_id", memoryID, "error", err, "cause", cause)
		return
	}
	h.publishMemoryChanged(context.Background(), mem, ownerID)
}

func (h *MemoriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	// Scope-aware load: cookie sessions / unscoped API keys get the
	// owner-OR-hub path (cross-hub by design); OAuth grants and API
	// keys with HubScopeAllowlist get strict hub-only filtering so
	// they can't fetch a memory outside their granted hubs by ID.
	hubIDs := GetAccessibleHubIDs(r)
	memory, err := loadMemoryRespectingScope(r, h.store, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	h.attachAttachments(memory)
	h.attachTopicID(ownerID, memory)

	// Detail-scope lifecycle: pending_dream_action scoped to topic_visits
	// PLUS dream_history (durable, unscoped, cap 10). Failures degrade
	// gracefully — memory renders without lifecycle signals.
	scope := store.VisibilityScope{OwnerID: ownerID, HubIDs: hubIDs}
	if lifecycle, lcErr := h.store.ResolveMemoryLifecycleForDetail(r.Context(), scope, ownerID, id); lcErr == nil {
		memory.Lifecycle = lifecycle
	}

	// access_count and accessed_at are NOT touched here — GET is a
	// pure read. Callers that represent deliberate user engagement
	// (web detail page mount, modal open, CLI `memax get`, MCP
	// memax_get) must POST to /v1/memories/{id}/access to signal
	// the view. This decoupling keeps speculative prefetches (hover
	// prefetch, React Query refetch-on-focus) from inflating the
	// decay signal.

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: memory})
}

// trackAccessedDedupTTL is the window over which a single (owner, memory)
// pair counts as ONE access regardless of how many times the client
// fires the signal. Plan 21 §4.4: the client tracker debounces with a
// 2s dwell + sessionStorage; this window is the server-side belt.
// 30s is long enough to catch rapid SPA nav loops + tab restore but
// short enough that a deliberate re-open after coffee still counts.
const trackAccessedDedupTTL = 30 * time.Second

// TrackAccessed records that a memory was deliberately viewed. This
// bumps access_count (which feeds the decay multiplier) and refreshes
// accessed_at. It is the explicit companion to the now side-effect-free
// GET handler — callers that represent user engagement must POST here
// separately.
//
// POST /v1/memories/{id}/access
//
// 204 on success, 404 if the memory is not accessible to the caller.
// Errors are best-effort — a transient store failure returns 500 but
// the caller should treat the signal as advisory, not transactional.
//
// Plan 21 §4.4: when a Redis cache is configured, the handler uses
// atomic SetNX on a `mem:access:<owner>:<id>` key with a 30s TTL to
// dedup near-simultaneous accesses. If the key already exists (loser
// of the race), we still return 204 — the user's intent was honored
// by the prior winner; double-incrementing access_count would inflate
// the decay signal.
//
// Fail-open on cache outage: a Redis hiccup that returns an error from
// SetNX is treated as "go ahead and do the work" so transient infra
// problems don't drop legitimate access signals.
func (h *MemoriesHandler) TrackAccessed(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	hubIDs := GetAccessibleHubIDs(r)

	// Existence + visibility gate. Without this, any authenticated
	// caller could bump the decay signal for memories they can't see
	// (by guessing IDs) — and the store increment would silently no-op
	// without returning an error, giving no feedback. Route the read
	// through the same scope-aware path as GET so the access rules
	// stay in one place (and scope-bounded principals can't poke the
	// signal for memories outside their granted hubs).
	if _, err := loadMemoryRespectingScope(r, h.store, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Atomic dedup. Key is owner-scoped because two users viewing the
	// same memory are independent events that BOTH should bump
	// access_count. If we keyed only by memory id we'd serialize
	// access events across the entire user base for that memory.
	//
	// `dedupAcquired` records whether THIS request won the SetNX. We
	// release the lock (via Del) on store failure so a retry within
	// the original window can succeed. Without that release, a Redis
	// success + DB failure would silently swallow the retry for 30s.
	// Codex 5.5 L-finding.
	dedupKey := ""
	dedupAcquired := false
	if h.cache != nil {
		dedupKey = "mem:access:" + ownerID + ":" + id
		set, err := h.cache.SetNX(r.Context(), dedupKey, "1", trackAccessedDedupTTL)
		if err == nil && !set {
			// Loser of the race — a prior request within the dedup
			// window already incremented. Treat as success: the user
			// engagement WAS counted, just not by this exact request.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// err != nil → fall through to the increment path (fail-open).
		dedupAcquired = err == nil && set
	}

	if err := h.store.IncrementMemoryAccessed(r.Context(), id, ownerID, hubIDs); err != nil {
		// Release the dedup lock on store failure so a retry within
		// the 30s window can attempt the increment again. Best-effort —
		// if the Del itself fails, the worst case is a 30s blackout
		// for THIS (owner, memory) pair, which the client tracker's
		// own dedup absorbs.
		if dedupAcquired && h.cache != nil {
			_ = h.cache.Del(r.Context(), dedupKey)
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Related returns semantically related memories using vector nearest-neighbor search.
// GET /v1/memories/{id}/related
//
// Neighbor scope is derived from the source memory's hub, NOT the session's
// active hub. The session hub (X-Hub-ID) is read-context UI state and must
// not gate content relationships — a memory's neighbors should be the same
// set regardless of which hub the user happens to be "in".
func (h *MemoriesHandler) Related(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")

	// Authorize the source memory first — same rule as GET /v1/memories/{id}.
	// This closes the similarity-oracle surface: without this check the store
	// query_vec CTE would happily read any memory's embedding by ID. Use the
	// scope-aware helper so a hub-bounded principal cannot turn Related into
	// a cross-hub embedding probe by guessing IDs they own elsewhere.
	memory, err := loadMemoryRespectingScope(r, h.store, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Processing memories have no chunks yet — short-circuit instead of
	// running an empty query. Archived memories similarly have no useful
	// neighbors to surface in the reading UI.
	if memory.State != "active" {
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: []model.RelatedMemory{}})
		return
	}

	related, err := h.store.FindRelatedMemories(id, memory.HubID, 3)
	if err != nil {
		slog.Warn("find related memories failed", "memory_id", id, "user_id", ownerID, "hub_id", memory.HubID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if related == nil {
		related = []model.RelatedMemory{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: related})
}

// AttachmentViewURLResponse is the success payload for
// POST /v1/memories/{id}/attachments/{attachmentID}/view-url.
//
// ExpiresAt is unix seconds UTC — easier for clients to compare than
// an ISO string. Clients cache the URL and refresh near expiry.
type AttachmentViewURLResponse struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

// CreateAttachmentViewURL issues a short-lived signed URL that renders
// the attachment inline via a direct <img src>. This is the
// authenticated entry point — it boundary-checks ownership, verifies
// the attachment exists, and only then issues a signature.
//
// The resulting URL is public within its TTL (see attachments.Signer
// doc for the explicit tradeoff). Reuse across surfaces in the same
// session is expected behavior, not a bug.
func (h *MemoriesHandler) CreateAttachmentViewURL(w http.ResponseWriter, r *http.Request) {
	memoryID := r.PathValue("id")
	attachmentID := r.PathValue("attachmentID")

	if h.attachmentSigner == nil {
		writeError(w, http.StatusServiceUnavailable, "signing_unavailable", "Attachment view URL signing is not configured")
		return
	}
	// Authorize via the parent memory using the scope-aware helper.
	// Without this gate, the GetMemoryAttachment owner filter would
	// reject team-hub viewers who don't own the memory (false 404
	// in a cross-hub flow) AND fail to constrain a scope-bounded
	// principal whose granted hubs don't include this memory's hub
	// but who happens to own a different memory by the same id-pair
	// guess. Loading the memory through the helper returns the
	// canonical owner_id and the strict-vs-cross-hub access check
	// in one call.
	memory, err := loadMemoryRespectingScope(r, h.store, memoryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment not found")
		return
	}
	if _, err := h.store.GetMemoryAttachment(attachmentID, memoryID, memory.OwnerID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment not found")
		return
	}

	viewURL, expiresAt, err := h.attachmentSigner.Sign(h.attachmentViewBaseURL, attachmentID, 0)
	if err != nil {
		slog.Warn("attachment view sign failed", "attachment_id", attachmentID, "error", err)
		writeError(w, http.StatusInternalServerError, "sign_failed", "Failed to sign view URL")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: AttachmentViewURLResponse{
		URL:       viewURL,
		ExpiresAt: expiresAt.Unix(),
	}})
}

// ServeAttachmentView streams an attachment at the signed view URL.
// Unauthenticated at the middleware layer — the HMAC signature is the
// authorization, the same way signed S3 URLs work.
//
// Failure shape is deliberately narrow:
//   - bad/expired signature → 403 with no detail (attacker cannot
//     distinguish tamper from expiry)
//   - missing row OR missing object → 404 (both are "the thing you
//     think exists is not here")
//
// Inline vs attachment disposition is decided purely from the DB row
// (inline_eligible AND content_type on the whitelist), not from
// object-store metadata, so client-declared MIME at upload time
// cannot coax this handler into serving attacker-controlled bytes
// inline.
func (h *MemoriesHandler) ServeAttachmentView(w http.ResponseWriter, r *http.Request) {
	if h.attachmentSigner == nil || h.objectStore == nil {
		writeError(w, http.StatusServiceUnavailable, "attachments_unavailable", "Attachment view is not configured")
		return
	}

	q := r.URL.Query()
	attachmentID := strings.TrimSpace(q.Get("id"))
	expStr := strings.TrimSpace(q.Get("exp"))
	sig := strings.TrimSpace(q.Get("sig"))
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Invalid signature")
		return
	}
	if err := h.attachmentSigner.Verify(attachmentID, exp, sig); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Invalid signature")
		return
	}

	attachment, err := h.store.GetAttachmentByID(attachmentID)
	if err != nil || attachment == nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment not found")
		return
	}

	result, err := h.objectStore.Get(r.Context(), attachment.StorageKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment not found")
		return
	}
	defer result.Body.Close()

	// Disposition + content-type come from the DB row only. Object-
	// store metadata is treated as untrusted (it was populated from
	// client-declared values at upload time).
	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	disposition := "attachment"
	if attachment.InlineEligible && attachments.IsInlineWhitelisted(contentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, attachment.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Cache-Control: private, within the TTL window. Storage keys are
	// content-addressed (sha256 embedded) but the attachment row id in
	// the URL is stable across re-uploads of the same bytes, so we
	// err on the conservative side with a TTL-bounded cache.
	w.Header().Set("Cache-Control", "private, max-age=600")
	if attachment.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
	}
	if _, err := io.Copy(w, result.Body); err != nil {
		slog.Warn("attachment view stream failed", "attachment_id", attachmentID, "error", err)
	}
}

func (h *MemoriesHandler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	memoryID := r.PathValue("id")
	attachmentID := r.PathValue("attachmentID")
	if h.objectStore == nil {
		writeError(w, http.StatusServiceUnavailable, "attachments_unavailable", "Attachment storage is not configured")
		return
	}

	// Authorize via the parent memory first — see CreateAttachmentViewURL
	// for the rationale. Loading through loadMemoryRespectingScope
	// closes the scope-bounded leak class and aligns attachment access
	// with team-hub memory visibility (a hub viewer can download the
	// attachments of a memory they're allowed to read).
	memory, err := loadMemoryRespectingScope(r, h.store, memoryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment not found")
		return
	}
	attachment, err := h.store.GetMemoryAttachment(attachmentID, memoryID, memory.OwnerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment not found")
		return
	}

	result, err := h.objectStore.Get(r.Context(), attachment.StorageKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Attachment content not found")
		return
	}
	defer result.Body.Close()

	contentType := attachment.ContentType
	if contentType == "" {
		contentType = result.ContentType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", attachment.Filename))
	if attachment.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
	}
	if _, err := io.Copy(w, result.Body); err != nil {
		slog.Warn("attachment download stream failed", "memory_id", memoryID, "attachment_id", attachmentID, "error", err)
	}
}

func (h *MemoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	// Parse query params into ListOptions
	q := r.URL.Query()
	limit := 20
	if s := q.Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}

	opts := store.ListOptions{
		Scope: store.VisibilityScope{
			OwnerID: ownerID,
			HubIDs:  GetAccessibleHubIDs(r),
		},
		HubID:  GetHubID(r),
		Actor:  q.Get("actor"),
		Sort:   q.Get("sort"),
		Kind:   q.Get("kind"),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	}
	if topicID := strings.TrimSpace(q.Get("topic_id")); topicID != "" {
		if !isValidUUID(topicID) {
			writeError(w, http.StatusBadRequest, "invalid_topic_id", "topic_id must be a valid UUID")
			return
		}
		opts.TopicID = topicID
	}

	// Time filter: created_after (ISO8601 or duration like "12h", "7d")
	if ca := q.Get("created_after"); ca != "" {
		if t, err := time.Parse(time.RFC3339, ca); err == nil {
			opts.CreatedAfter = &t
		} else if dur, err := parseDuration(ca); err == nil {
			t := time.Now().Add(-dur)
			opts.CreatedAfter = &t
		}
	}

	memories, nextCursor, totalCount, err := h.store.ListMemoriesPaginated(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Attach topic IDs to each memory
	h.attachTopicIDSlice(ownerID, memories)

	// List-scope lifecycle: batched pending_dream_action resolution across
	// the page. History is NOT fetched (list surfaces don't render it);
	// the resolver returns DreamHistory: [] on every entry. Failures
	// degrade gracefully — rows render without lifecycle signals.
	if len(memories) > 0 {
		ids := make([]string, 0, len(memories))
		for i := range memories {
			ids = append(ids, memories[i].ID)
		}
		if lifecycleByID, lcErr := h.store.ResolveMemoryLifecycleForList(r.Context(), opts.Scope, ownerID, ids); lcErr == nil {
			for i := range memories {
				if lifecycle := lifecycleByID[memories[i].ID]; lifecycle != nil {
					memories[i].Lifecycle = lifecycle
				}
			}
		}
	}

	actors := map[string]int{}
	if opts.Cursor == "" {
		actorCounts, err := h.store.ListActorCounts(opts)
		if err == nil && actorCounts != nil {
			actors = actorCounts
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]any{
			"memories":    memories,
			"next_cursor": nextCursor,
			"has_more":    nextCursor != "",
			"total":       totalCount,
			"actors":      actors,
		},
	})
}

func (h *MemoriesHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	existing, err := h.store.GetMemory(id, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	var req model.PushRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("Request body exceeds %dMB limit.", maxBodySize/(1024*1024)))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	// Update may replace content + content_type together, or either
	// alone. Resolve the effective content_type before sanitizing so
	// a Content-only update reuses the stored content_type (no silent
	// downgrade to text for an existing html memory). Track whether
	// the client explicitly sent content_type so we can distinguish
	// "inherit for sanitizer only" from "client wants to re-label the
	// row". Sanitizer is idempotent so running it on partial input is
	// safe.
	explicitContentType := req.ContentType != ""
	if !explicitContentType {
		req.ContentType = existing.ContentType
	}
	sanitizePushRequest(&req)

	now := time.Now()
	contentChanged := req.Content != ""
	metadataChanged := req.Kind != "" || req.Stability != "" || req.Tags != nil || req.Hint != ""
	if contentChanged {
		content := req.Content
		if h.formatter != nil {
			content = h.formatter.Prepare(content, req.ContentType, existing.SourcePath).Content
		}
		existing.Content = content
		existing.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		// Persist the effective content_type alongside the new body
		// so the row is consistently labeled — otherwise a text → html
		// update would store sanitized HTML but leave content_type
		// as "text", and the next content-only update would inherit
		// "text" and skip HTML sanitization.
		existing.ContentType = req.ContentType
	} else if explicitContentType {
		// Label-only change: the client renamed the content type
		// without sending new content. Update the label so future
		// sanitization decisions reflect the client's intent, AND if
		// the new label is HTML, run the existing body through the
		// HTML sanitizer now — otherwise a client could write raw
		// HTML into a text-labeled row at Create time and then
		// relabel it to html without ever triggering sanitization
		// (regression for Codex round-2 finding).
		existing.ContentType = req.ContentType
		if isHTMLContentType(req.ContentType) && existing.Content != "" {
			sanitized := sanitize.Body(existing.Content)
			if sanitized != existing.Content {
				existing.Content = sanitized
				existing.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(sanitized)))
			}
		}
	}
	if req.Title != "" {
		existing.Title = req.Title
	}
	if req.Kind != "" {
		existing.Kind = model.NormalizeMemoryKind(req.Kind)
	}
	if req.Stability != "" {
		existing.Stability = model.NormalizeMemoryStability(req.Stability)
	}
	existing.RetrievalWeight = model.DefaultRetrievalWeight(existing.RetrievalWeight)
	if req.Tags != nil {
		existing.Tags = req.Tags
	}
	if req.Hint != "" {
		existing.Hint = req.Hint
	}
	existing.UpdatedAt = now
	existing.Version++

	if err := h.store.UpdateMemory(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Re-chunk if content changed; otherwise keep denormalized chunk metadata in sync.
	if contentChanged {
		if err := h.store.DeleteChunksByMemory(id, ownerID); err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		chunkResults := chunker.ChunkMarkdown(existing.Content)
		existingRepo := ""
		if existing.ProjectContext != nil {
			existingRepo = existing.ProjectContext["repo"]
		}
		tagsText := chunkmeta.TagsText(existing.Tags)
		metadataText := h.chunkMetadataText(existing)
		var chunks []model.Chunk
		for _, cr := range chunkResults {
			lang := language.DetectChunk(cr.HeadingChain, cr.Content, existing.Hint)
			chunks = append(chunks, model.Chunk{
				ID:              generateID(),
				MemoryID:        id,
				Content:         cr.Content,
				HeadingChain:    cr.HeadingChain,
				ChunkIndex:      cr.Index,
				TokenCount:      cr.TokenCount,
				Language:        lang.Code,
				SearchConfig:    lang.Config,
				Kind:            existing.Kind,
				Stability:       existing.Stability,
				RetrievalWeight: existing.RetrievalWeight,
				Hint:            existing.Hint,
				TagsText:        tagsText,
				MetadataText:    metadataText,
				ProjectRepo:     existingRepo,
				CreatedAt:       now,
			})
		}
		h.embedChunks(r.Context(), chunks)
		if err := h.store.CreateChunks(chunks); err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}

		if h.summarizer != nil {
			llmCtx := anthropic.WithTracking(r.Context(), anthropic.Tracking{
				DistinctID: ownerID,
				Metadata: map[string]any{
					"owner_id":  ownerID,
					"memory_id": existing.ID,
					"hub_id":    existing.HubID,
					"llm_flow":  "memory_update",
				},
			})
			result := h.summarizer.SummarizeWithTitleContext(llmCtx, existing.Title, existing.Content, memoryClassification(existing), existing.Hint)
			if applySummaryResult(existing, existing.Content, result) {
				if err := h.store.UpdateMemory(existing); err != nil {
					slog.Warn("summary update failed", "memory_id", existing.ID, "error", err)
				}
			}
		}
	} else if metadataChanged {
		if err := h.syncChunkMetadata(existing, ownerID); err != nil {
			slog.Warn("failed to sync chunk metadata", "memory_id", existing.ID, "error", err)
		}
	}

	slog.Info("memory updated", "id", id, "title", existing.Title, "version", existing.Version)
	h.publishMemoryChanged(r.Context(), existing, ownerID)
	h.returnMemory(w, http.StatusOK, id, ownerID)
}

func (h *MemoriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	id := r.PathValue("id")

	// Load memory with scope-aware visibility. Default sessions get
	// owner-OR-hub (cross-hub by design); scope-bounded principals
	// (OAuth grants, hub-allowlisted API keys) get strict hub-only
	// — they cannot delete a memory outside their granted hubs even
	// if they own it elsewhere.
	memory, err := loadMemoryRespectingScope(r, h.store, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Memory not found")
		return
	}

	// Authorization: owner of the memory can always delete.
	// For hub memories where user is not the owner, check role + policy.
	isMemoryOwner := memory.OwnerID == userID
	if !isMemoryOwner {
		if memory.HubID == "" {
			writeError(w, http.StatusForbidden, "forbidden", "You can only delete your own memories")
			return
		}
		role, _ := h.store.GetHubMemberRole(memory.HubID, userID)
		hub, hubErr := h.store.GetHub(memory.HubID)
		if hubErr != nil || role == "" {
			writeError(w, http.StatusForbidden, "forbidden", "You are not a member of this hub")
			return
		}
		if !canDeleteMemory(role, hub, false) {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to delete this memory")
			return
		}
	}

	// Collect attachments before deletion (use memory's owner for attachment query).
	// We sum sizes here so we can release the storage-bytes quota after
	// the DB cascade removes the rows — the row is gone by the time we
	// want to adjust the counter.
	attachments, _ := h.store.ListMemoryAttachments(id, memory.OwnerID)
	var freedBytes int64
	for _, att := range attachments {
		freedBytes += att.SizeBytes
	}

	// Delete — use hub-scoped delete when user is not the memory owner
	if isMemoryOwner {
		if err := h.store.DeleteMemory(id, userID); err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
	} else {
		if err := h.store.DeleteHubMemory(id, memory.HubID); err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
	}

	// Adjust memory count and storage bytes for the memory's owner
	// (not the acting user — team-hub deletes by a member free the
	// owner's quota, not the deleter's). Also decrement the hub-level
	// counter so the hub's room for new memories recovers as memories
	// leave it.
	if h.meter != nil {
		h.meter.AdjustMemoryCount(r.Context(), memory.OwnerID, -1)
		h.meter.AdjustHubMemoryCount(r.Context(), memory.HubID, -1)
		if freedBytes > 0 {
			h.meter.AdjustStorageBytes(r.Context(), memory.OwnerID, -freedBytes)
		}
		// If the deleted memory's hub was flagged over-limit, see if
		// the post-delete count is back under. Clear the marker and
		// stop the grace-period clock when it is. Best-effort: a
		// failed clear just means the marker stays set and another
		// delete will retry.
		h.maybeClearHubOverLimit(r.Context(), memory.HubID)
	}
	h.deleteAttachmentObjects(r.Context(), attachments)
	h.publishMemoryChanged(r.Context(), memory, userID)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]bool{"deleted": true}})
	trackRequest(r, "api.memories.delete", map[string]any{"memory_id": id})
}

// BatchDelete deletes multiple memories by ID and returns a structured
// result with per-id skipped reasons. Mirrors BatchMove's contract so
// clients can share decode helpers and report partial-success uniformly.
// POST /v1/memories/batch-delete
func (h *MemoriesHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)

	var req struct {
		IDs []string `json:"ids"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "missing_ids", "No memory IDs provided")
		return
	}
	if len(req.IDs) > 100 {
		writeError(w, http.StatusBadRequest, "too_many", "Maximum 100 memories per batch")
		return
	}

	hubIDs := GetAccessibleHubIDs(r)
	memoriesByID, _ := h.store.GetAccessibleMemories(req.IDs, userID, hubIDs)

	// Partition ids into four disjoint buckets in the caller's order:
	//   notFoundIDs  — not in memoriesByID (not accessible or unknown id)
	//   ownedIDs     — accessible + caller owns it
	//   hubAuthorized — accessible + caller holds hub role + delete policy
	//   notOwnedIDs  — accessible + caller lacks any delete permission
	var notFoundIDs, ownedIDs, notOwnedIDs []string
	hubAuthorized := map[string][]string{}
	hubCache := map[string]*model.Hub{}
	roleCache := map[string]string{}

	for _, id := range req.IDs {
		memory, ok := memoriesByID[id]
		if !ok {
			notFoundIDs = append(notFoundIDs, id)
			continue
		}
		if memory.OwnerID == userID {
			ownedIDs = append(ownedIDs, id)
			continue
		}
		if memory.HubID == "" {
			notOwnedIDs = append(notOwnedIDs, id)
			continue
		}
		role, cached := roleCache[memory.HubID]
		if !cached {
			role, _ = h.store.GetHubMemberRole(memory.HubID, userID)
			roleCache[memory.HubID] = role
		}
		hub, hubCached := hubCache[memory.HubID]
		if !hubCached {
			hub, _ = h.store.GetHub(memory.HubID)
			hubCache[memory.HubID] = hub
		}
		if hub != nil && canDeleteMemory(role, hub, false) {
			hubAuthorized[memory.HubID] = append(hubAuthorized[memory.HubID], id)
		} else {
			notOwnedIDs = append(notOwnedIDs, id)
		}
	}

	// Seed skipped with pre-execution reasons. Each id appears in skipped
	// exactly once by construction (partition buckets are disjoint).
	skipped := make([]model.SkippedMemory, 0, len(req.IDs))
	for _, id := range notFoundIDs {
		skipped = append(skipped, model.SkippedMemory{
			ID:     id,
			Reason: model.BatchDeleteSkipNotFound,
		})
	}
	for _, id := range notOwnedIDs {
		skipped = append(skipped, model.SkippedMemory{
			ID:     id,
			Reason: model.BatchDeleteSkipNotOwned,
		})
	}

	deletedSet := map[string]struct{}{}
	// freedBytesByOwner accumulates released storage-bytes quota across
	// both the owned and hub deletion phases so a single post-loop pass
	// can call AdjustStorageBytes once per owner. Keyed by the memory
	// OWNER (not the acting user) because hub-scoped deletes by a
	// moderator free the contributor's quota.
	freedBytesByOwner := map[string]int64{}

	// ── Owned-phase ──
	// Collect attachments BEFORE the delete so we can filter by deletedSet
	// after. If the store call errors, we do NOT delete attachment objects
	// (the DB rows still exist and an object cleanup would orphan them).
	if len(ownedIDs) > 0 {
		ownedAttachments, _ := h.store.ListMemoryAttachmentsByIDs(ownedIDs, userID)

		deletedIDs, err := h.store.BatchDeleteMemories(ownedIDs, userID)
		if err != nil {
			slog.Error("batch delete owned memories failed",
				"error", err, "count", len(ownedIDs))
			// All attempted ids couldn't be deleted. Mark delete_failed
			// for each — do NOT run race-fallback (that would misreport
			// infra failure as not_found).
			for _, id := range ownedIDs {
				skipped = append(skipped, model.SkippedMemory{
					ID:     id,
					Reason: model.BatchDeleteSkipDeleteFailed,
				})
			}
		} else {
			for _, id := range deletedIDs {
				deletedSet[id] = struct{}{}
			}
			// Race-fallback: any attempted id missing from the store's
			// returned set was concurrently removed elsewhere — report
			// as not_found so the client sees "that memory is gone."
			for _, id := range ownedIDs {
				if _, ok := deletedSet[id]; !ok {
					skipped = append(skipped, model.SkippedMemory{
						ID:     id,
						Reason: model.BatchDeleteSkipNotFound,
					})
				}
			}
			for _, att := range ownedAttachments {
				if _, ok := deletedSet[att.MemoryID]; ok {
					freedBytesByOwner[att.OwnerID] += att.SizeBytes
				}
			}
			h.deleteAttachmentObjects(r.Context(),
				filterAttachmentsByDeleted(ownedAttachments, deletedSet))
		}
	}

	// ── Hub-phase ──
	// ListMemoryAttachmentsByIDs is owner-scoped, not hub-scoped. When a
	// moderator deletes a contributor's memory, the attachment rows are
	// owned by the original contributor — so we must group ids by the
	// memory's actual owner before querying attachments. Parallel to how
	// the single Delete handler passes memory.OwnerID (not userID) at
	// memories.go:880.
	for hubID, ids := range hubAuthorized {
		byOwner := map[string][]string{}
		for _, id := range ids {
			mem := memoriesByID[id]
			byOwner[mem.OwnerID] = append(byOwner[mem.OwnerID], id)
		}
		var hubAttachments []model.MemoryAttachment
		for ownerID, ownerIDs := range byOwner {
			atts, _ := h.store.ListMemoryAttachmentsByIDs(ownerIDs, ownerID)
			hubAttachments = append(hubAttachments, atts...)
		}

		deletedIDs, err := h.store.BatchDeleteHubMemories(ids, hubID)
		if err != nil {
			slog.Error("batch delete hub memories failed",
				"hub_id", hubID, "error", err, "count", len(ids))
			for _, id := range ids {
				skipped = append(skipped, model.SkippedMemory{
					ID:     id,
					Reason: model.BatchDeleteSkipDeleteFailed,
				})
			}
			continue
		}
		for _, id := range deletedIDs {
			deletedSet[id] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := deletedSet[id]; !ok {
				skipped = append(skipped, model.SkippedMemory{
					ID:     id,
					Reason: model.BatchDeleteSkipNotFound,
				})
			}
		}
		for _, att := range hubAttachments {
			if _, ok := deletedSet[att.MemoryID]; ok {
				freedBytesByOwner[att.OwnerID] += att.SizeBytes
			}
		}
		h.deleteAttachmentObjects(r.Context(),
			filterAttachmentsByDeleted(hubAttachments, deletedSet))
	}

	// Adjust memory counts + storage bytes for deleted memories. Keyed
	// by the memory OWNER (not the acting user) because hub-scoped
	// deletes by a moderator free the contributor's quota. Same rule
	// as the single Delete handler above. Hub counter decrements in
	// parallel so hub-level room recovers as memories leave.
	if h.meter != nil && len(deletedSet) > 0 {
		ownerDeleted := map[string]int{}
		hubDeleted := map[string]int{}
		for id := range deletedSet {
			if mem, ok := memoriesByID[id]; ok {
				ownerDeleted[mem.OwnerID]++
				if mem.HubID != "" {
					hubDeleted[mem.HubID]++
				}
			}
		}
		for oid, count := range ownerDeleted {
			h.meter.AdjustMemoryCount(r.Context(), oid, -count)
		}
		for hid, count := range hubDeleted {
			h.meter.AdjustHubMemoryCount(r.Context(), hid, -count)
			h.maybeClearHubOverLimit(r.Context(), hid)
		}
		for oid, bytes := range freedBytesByOwner {
			if bytes > 0 {
				h.meter.AdjustStorageBytes(r.Context(), oid, -bytes)
			}
		}
	}

	result := &model.BatchDeleteResult{
		Deleted: len(deletedSet),
		Skipped: skipped,
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: result})

	// Phantom-event filter: only publish for memories actually deleted.
	// Mirrors the BatchMove fix — without this, skipped memories emit
	// "changed" events that lie about the memory's state.
	for id, memory := range memoriesByID {
		if _, wasDeleted := deletedSet[id]; !wasDeleted {
			continue
		}
		h.publishMemoryChanged(r.Context(), memory, userID)
	}
	trackRequest(r, "api.memories.batch_delete", map[string]any{
		"deleted": result.Deleted,
		"skipped": len(result.Skipped),
	})
}

// filterAttachmentsByDeleted returns the subset of attachments whose
// memory_id was actually removed from the store in this batch. Used by
// BatchDelete to avoid orphaning R2 objects for memories the store
// didn't delete (race-condition not_found, store error), and to avoid
// deleting objects for memories that skipped the delete entirely.
func filterAttachmentsByDeleted(
	atts []model.MemoryAttachment,
	deletedSet map[string]struct{},
) []model.MemoryAttachment {
	out := make([]model.MemoryAttachment, 0, len(atts))
	for _, a := range atts {
		if _, ok := deletedSet[a.MemoryID]; ok {
			out = append(out, a)
		}
	}
	return out
}

// BatchMove moves multiple memories to a topic or hub in a single transaction.
// POST /v1/memories/batch-move
func (h *MemoriesHandler) BatchMove(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	hubID := GetHubID(r)

	var req struct {
		IDs     []string `json:"ids"`
		TopicID string   `json:"topic_id"`
		HubID   string   `json:"hub_id"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "missing_ids", "No memory IDs provided")
		return
	}
	if req.TopicID == "" && req.HubID == "" {
		writeError(w, http.StatusBadRequest, "missing_target", "topic_id or hub_id is required")
		return
	}
	if len(req.IDs) > 100 {
		writeError(w, http.StatusBadRequest, "too_many", "Maximum 100 memories per batch")
		return
	}

	hubIDs := GetAccessibleHubIDs(r)
	memoriesByID, _ := h.store.GetAccessibleMemories(req.IDs, ownerID, hubIDs)

	targetHubID := req.HubID
	if targetHubID == "" {
		targetHubID = hubID
	}
	if targetHubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Destination hub is required")
		return
	}

	targetHub, err := h.store.GetHub(targetHubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub_not_found", "Target hub not found")
		return
	}
	role, _ := h.store.GetHubMemberRole(targetHubID, ownerID)
	if role == "" {
		writeError(w, http.StatusForbidden, "not_member", "You are not a member of the target hub")
		return
	}
	if req.TopicID != "" {
		if !canWriteTopics(role, targetHub) {
			writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
			return
		}
		targetTopic, err := h.store.GetTopic(req.TopicID, targetHubID)
		if err != nil {
			writeError(w, http.StatusNotFound, "topic_not_found", "Target topic not found")
			return
		}
		if targetTopic.ArchivedAt != nil {
			writeError(w, http.StatusConflict, "topic_archived", "Target topic is archived — restore it before moving memories into it")
			return
		}
	} else if !canWriteMemories(role) {
		writeError(w, http.StatusForbidden, "no_write_access", "Write access to this hub is required")
		return
	}

	// Source-hub delete permission check.
	//
	// Move is semantically delete-from-source + create-in-destination.
	// The destination half is already enforced above. This loop enforces
	// the source half: for every cross-hub move, the caller must hold
	// delete authority in the memory's CURRENT hub, not just the target.
	// Without this, contributors in a "none"-policy hub could bypass
	// the retention policy by moving their own memories to a personal
	// hub and deleting them there.
	//
	// Two carve-outs skip the check entirely:
	//   (1) sourceHubID == "" — personal-hub memory, no team policy applies.
	//   (2) sourceHubID == targetHubID — same-hub topic reassignment;
	//       the memory stays in the same hub with the same policy, so
	//       no authority boundary is crossed. Destination topic-write
	//       permission (checked above) is sufficient.
	//
	// Handler-level skips are accumulated in handlerSkipped and merged
	// into result.Skipped after the store call. If every id is blocked
	// at the handler level, the store is short-circuited — no SQL call,
	// no phantom events, return the handler-only result directly.
	sourceHubCache := map[string]*model.Hub{}
	sourceRoleCache := map[string]string{}
	movableIDs := make([]string, 0, len(req.IDs))
	handlerSkipped := []model.SkippedMemory{}

	for _, id := range req.IDs {
		memory, ok := memoriesByID[id]
		if !ok {
			// Not accessible to the caller. Let the store report not_found
			// so the reason code is consistent with existing behavior for
			// unknown ids.
			movableIDs = append(movableIDs, id)
			continue
		}

		sourceHubID := memory.HubID

		// Carve-out 1: personal hub — no team policy applies.
		if sourceHubID == "" {
			movableIDs = append(movableIDs, id)
			continue
		}

		// Carve-out 2: same-hub topic reassignment — no authority
		// boundary crossed.
		if sourceHubID == targetHubID {
			movableIDs = append(movableIDs, id)
			continue
		}

		// Cross-hub move: enforce source-hub delete permission.
		sourceHub, cached := sourceHubCache[sourceHubID]
		if !cached {
			sourceHub, _ = h.store.GetHub(sourceHubID)
			sourceHubCache[sourceHubID] = sourceHub
		}
		sourceRole, cached := sourceRoleCache[sourceHubID]
		if !cached {
			sourceRole, _ = h.store.GetHubMemberRole(sourceHubID, ownerID)
			sourceRoleCache[sourceHubID] = sourceRole
		}
		isMemoryOwner := memory.OwnerID == ownerID
		if !canDeleteMemory(sourceRole, sourceHub, isMemoryOwner) {
			handlerSkipped = append(handlerSkipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipSourceDeleteForbidden,
			})
			continue
		}

		movableIDs = append(movableIDs, id)
	}

	// Short-circuit: every id was blocked at the handler level. No store
	// call, no phantom events. Return the handler-only result with 200.
	if len(movableIDs) == 0 {
		result := &model.BatchMoveResult{
			Moved:   0,
			Skipped: handlerSkipped,
		}
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: result})
		trackRequest(r, "api.memories.batch_move", map[string]any{
			"count":    0,
			"skipped":  len(result.Skipped),
			"topic_id": req.TopicID,
			"hub_id":   targetHubID,
		})
		return
	}

	// Target-hub frozen + capacity guard (team hubs only). Mirrors the
	// push-path checks so the move endpoint can't be used to
	// bypass the hub cap (push to personal → move to team). Only
	// cross-hub moves into a NEW target hub count against capacity;
	// same-hub topic reassigns are no-ops for the counter.
	if targetHubID != "" && h.hubQuota != nil {
		targetHubInfo, _ := h.store.GetHub(targetHubID)
		if targetHubInfo != nil && targetHubInfo.HubType == "team" {
			sub, _ := h.store.GetHubSubscriptionAnyStatus(r.Context(), targetHubID)
			if model.IsHubFrozen(sub, time.Now().UTC()) {
				WriteErrorWithDetails(w, http.StatusPaymentRequired, "hub_frozen",
					"Target hub is frozen. Resolve the over-limit state or restore the subscription before moving memories in.",
					map[string]any{"hub_id": targetHubID})
				return
			}
			limit := h.hubQuota.GetHubMemoryLimit(r.Context(), targetHubID)
			if !model.IsUnlimited(limit) {
				incoming := 0
				for _, id := range movableIDs {
					mem, ok := memoriesByID[id]
					if !ok || mem == nil {
						continue
					}
					if mem.HubID == targetHubID {
						continue // same-hub, doesn't change count
					}
					incoming++
				}
				if incoming > 0 {
					current, _ := h.store.CountMemoriesByHub(r.Context(), targetHubID)
					if current+incoming > limit {
						WriteErrorWithDetails(w, http.StatusPaymentRequired, "hub_memory_limit_reached",
							fmt.Sprintf("Target hub can't accept %d more memories (%d/%d after move). Upgrade the plan or move fewer.", incoming, current+incoming, limit),
							map[string]any{
								"current":    current,
								"incoming":   incoming,
								"limit":      limit,
								"hub_id":     targetHubID,
								"cap_source": "hub",
							})
						return
					}
				}
			}
		}
	}

	result, err := h.store.BatchMoveMemories(movableIDs, targetHubID, req.TopicID, ownerID)
	if err != nil {
		slog.Error("batch move store failure",
			"error", err,
			"owner_id", ownerID,
			"target_hub", targetHubID,
			"target_topic", req.TopicID,
			"ids", movableIDs,
		)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to move memories")
		return
	}

	// Adjust hub counters for every memory that actually changed hubs.
	// The store returns a full move result; we compute per-hub deltas
	// from the pre-move memoriesByID snapshot against the targetHubID.
	// Same-hub topic-only moves contribute zero. A source-hub
	// decrement might push that hub back under its cap — run
	// maybeClearHubOverLimit on each touched source hub.
	if h.meter != nil {
		skippedIDs := make(map[string]struct{}, len(result.Skipped))
		for _, s := range result.Skipped {
			skippedIDs[s.ID] = struct{}{}
		}
		sourceDeltas := map[string]int{}
		targetIncrement := 0
		for _, id := range movableIDs {
			if _, skipped := skippedIDs[id]; skipped {
				continue
			}
			mem, ok := memoriesByID[id]
			if !ok || mem == nil {
				continue
			}
			if mem.HubID == targetHubID {
				continue
			}
			sourceDeltas[mem.HubID]++
			targetIncrement++
		}
		for srcHub, n := range sourceDeltas {
			h.meter.AdjustHubMemoryCount(r.Context(), srcHub, -n)
			h.maybeClearHubOverLimit(r.Context(), srcHub)
		}
		if targetIncrement > 0 && targetHubID != "" {
			h.meter.AdjustHubMemoryCount(r.Context(), targetHubID, targetIncrement)
		}
	}

	// Merge handler-level skips into the store's result. Handler skips
	// are prepended so the ordering is stable (handler-skipped ids
	// appear first in req.IDs order, then store-skipped ids).
	if len(handlerSkipped) > 0 {
		result.Skipped = append(handlerSkipped, result.Skipped...)
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: result})
	// Only publish change events for memories the store actually moved.
	// Without this filter, skipped memories (not_owned, not_found,
	// already_at_target, or source_delete_forbidden) emit phantom
	// "moved" events that tell clients a memory changed location when
	// it did not — causing false UI refetches and, worse, broadcasting
	// reassignment events for memories owned by other users in a
	// shared hub. Handler-level skips also land in result.Skipped, so
	// the lookup naturally excludes them from the publish loop.
	skippedIDs := make(map[string]struct{}, len(result.Skipped))
	for _, s := range result.Skipped {
		skippedIDs[s.ID] = struct{}{}
	}
	for id, memory := range memoriesByID {
		if _, isSkipped := skippedIDs[id]; isSkipped {
			continue
		}
		oldCopy := *memory
		h.publishMemoryChanged(r.Context(), &oldCopy, ownerID)
		newCopy := *memory
		newCopy.HubID = targetHubID
		newCopy.TopicID = req.TopicID
		h.publishMemoryChanged(r.Context(), &newCopy, ownerID)
	}
	trackRequest(r, "api.memories.batch_move", map[string]any{
		"count":    result.Moved,
		"skipped":  len(result.Skipped),
		"topic_id": req.TopicID,
		"hub_id":   targetHubID,
	})
}

// DeleteAllData purges all user data: memories, topics, configs, dreams, reviews.
// DELETE /v1/account/data
func (h *MemoriesHandler) DeleteAllData(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	// Count memories + sum storage bytes before deletion for meter adjustment.
	// We use the authoritative Postgres SUM here (not the Redis cache)
	// because an account wipe should zero the counters regardless of
	// whether Redis drifted.
	var memCount int
	var freedBytes int64
	if h.meter != nil {
		// Excluding seeds (plan 23 §5.5) so the meter adjustment
		// matches the seed-excluded count the meter actually tracks
		// (see meter.backfillMemoryCountIfMissing). Mismatched
		// adjustment sources would drift the cached counter on
		// account-wipe.
		memCount, _ = h.store.CountMemoriesByOwnerExcludingSeeds(r.Context(), ownerID)
		freedBytes, _ = h.store.SumStorageBytesByOwner(r.Context(), ownerID)
	}
	attachments, _ := h.store.ListOwnerMemoryAttachments(ownerID)
	if err := h.store.DeleteAllUserData(ownerID); err != nil {
		slog.Error("failed to delete all user data", "error", err, "owner_id", ownerID)
		writeError(w, http.StatusInternalServerError, "delete_failed", "Failed to delete all data")
		return
	}
	// Adjust counters after successful deletion.
	if h.meter != nil {
		if memCount > 0 {
			h.meter.AdjustMemoryCount(r.Context(), ownerID, -memCount)
		}
		if freedBytes > 0 {
			h.meter.AdjustStorageBytes(r.Context(), ownerID, -freedBytes)
		}
	}
	h.deleteAttachmentObjects(r.Context(), attachments)
	slog.Info("deleted all user data", "owner_id", ownerID)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]bool{"deleted": true}})
	trackRequest(r, "api.account.delete_all_data", nil)
}

// Share moves a memory to a different hub. The user must own the memory
// and have write access (owner/admin/contributor) in the target hub.
// POST /v1/memories/{id}/share
// Body: { "target_hub_id": "uuid" }
func (h *MemoriesHandler) Share(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")

	var req struct {
		TargetHubID string `json:"target_hub_id"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if req.TargetHubID == "" {
		writeError(w, http.StatusBadRequest, "missing_target", "target_hub_id is required")
		return
	}

	// Verify memory exists and user owns it
	memory, err := h.store.GetMemory(id, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Memory not found")
		return
	}
	oldHubID := memory.HubID

	// Verify target hub exists
	targetHub, err := h.store.GetHub(req.TargetHubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub_not_found", "Target hub not found")
		return
	}

	// Verify user has write access to target hub
	role, err := h.store.GetHubMemberRole(targetHub.ID, ownerID)
	if err != nil || !canWriteMemories(role) {
		writeError(w, http.StatusForbidden, "no_access", "You don't have write access to the target hub")
		return
	}

	// Move the memory
	memory.HubID = targetHub.ID
	memory.UpdatedAt = time.Now()
	if err := h.store.UpdateMemory(memory); err != nil {
		writeError(w, http.StatusInternalServerError, "update_error", err.Error())
		return
	}

	slog.Info("memory shared to hub", "memory_id", id, "target_hub", targetHub.ID, "user", ownerID)
	trackRequest(r, "api.memories.share", map[string]any{"memory_id": id, "target_hub": targetHub.ID})
	if oldHubID != "" {
		oldCopy := *memory
		oldCopy.HubID = oldHubID
		h.publishMemoryChanged(r.Context(), &oldCopy, ownerID)
	}
	h.publishMemoryChanged(r.Context(), memory, ownerID)

	h.returnMemory(w, http.StatusOK, id, ownerID)
}

// Reindex is an admin-only endpoint that re-generates embeddings for ALL chunks in the database.
// This is used when the embedding model or its dimensions change.
// Authentication: requires ADMIN_TOKEN environment variable.
func (h *MemoriesHandler) Reindex(w http.ResponseWriter, r *http.Request) {
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		writeError(w, http.StatusForbidden, "reindex_disabled", "Admin reindexing is disabled (no ADMIN_TOKEN set)")
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader != "Bearer "+adminToken {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid admin token")
		return
	}

	if h.embedder == nil {
		writeError(w, http.StatusServiceUnavailable, "no_embedder", "No embedder configured")
		return
	}

	// Run in background as this can take a long time
	go h.runReindex()

	writeJSON(w, http.StatusAccepted, model.ApiResponse{
		Data: map[string]string{
			"status":  "reindexing_started",
			"message": "Full re-indexing of all chunks started in background. Check server logs for progress.",
		},
	})
}

// RegenerateMetadata is an admin-only maintenance endpoint that re-enqueues the
// existing memory processing pipeline for one memory. This lets operators repair
// bad generated title/summary metadata after validation fixes deploy while
// preserving the same summarization, chunking, embedding, and title-resolution
// semantics as normal ingestion.
func (h *MemoriesHandler) RegenerateMetadata(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_memory_id", "Memory ID is required")
		return
	}
	memory, err := h.store.GetMemoryForAdmin(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory_not_found", "Memory not found")
		return
	}
	if h.summarizer == nil {
		writeError(w, http.StatusServiceUnavailable, "summarizer_disabled", "Metadata regeneration is disabled because the summarizer is not configured")
		return
	}

	h.ProcessMemoryBackground(memory.ID, memory.OwnerID, buildRegenRequest(memory))

	writeJSON(w, http.StatusAccepted, model.ApiResponse{
		Data: map[string]string{
			"status":    "queued",
			"memory_id": memory.ID,
		},
	})
}

// CleanupMetadata is an admin-only endpoint that finds memories with
// suspicious LLM-generated metadata (raw JSON, protocol text) and
// optionally clears + re-enqueues them for regeneration.
//
// POST /v1/admin/memories/cleanup-metadata
// Body: {"dry_run": true, "limit": 100}
//
// In dry-run mode (default): returns suspicious IDs + count without modifying data.
// In apply mode (dry_run=false): requires summarizer to be configured (returns 503
// otherwise), clears bad fields, re-enqueues for regeneration, and reports per-row results.
func (h *MemoriesHandler) CleanupMetadata(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DryRun bool `json:"dry_run"`
		Limit  int  `json:"limit"`
	}
	req.DryRun = true // default to dry run
	req.Limit = 100
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
				return
			}
		}
	}
	if req.Limit <= 0 || req.Limit > 1000 {
		req.Limit = 100
	}

	// Apply mode requires summarizer for regeneration — fail fast to prevent
	// permanent metadata loss without re-enqueue capability.
	if !req.DryRun && h.summarizer == nil {
		writeError(w, http.StatusServiceUnavailable, "summarizer_disabled",
			"Apply mode requires the summarizer to be configured for metadata regeneration.")
		return
	}

	rows, err := h.store.FindSuspiciousMetadata(r.Context(), req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	cleaned := 0
	var failedIDs []string
	if !req.DryRun {
		for _, row := range rows {
			mem, err := h.store.GetMemoryForAdmin(row.ID)
			if err != nil {
				failedIDs = append(failedIDs, row.ID)
				continue
			}

			// Determine what needs clearing
			badSummary := looksLikeBadMetadata(mem.Summary)
			badTitle := looksLikeBadMetadata(mem.Title)
			if !badSummary && !badTitle {
				continue
			}

			// Build regen request BEFORE clearing — it uses the old content
			// but clears bad titles so the pipeline generates fresh ones.
			regenReq := buildRegenRequest(mem)

			// Clear bad fields and persist
			if badSummary {
				mem.Summary = ""
			}
			if badTitle {
				mem.Title = ""
			}
			if err := h.store.UpdateMemory(mem); err != nil {
				failedIDs = append(failedIDs, row.ID)
				continue
			}

			// Enqueue regeneration — if this fails asynchronously, the row
			// is blank but the admin can re-run cleanup or use regenerate-metadata.
			// This is acceptable because: (a) the summarizer will be called,
			// (b) blank metadata is better than broken JSON displayed to users.
			h.ProcessMemoryBackground(mem.ID, mem.OwnerID, regenReq)
			cleaned++
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"found":      len(rows),
		"cleaned":    cleaned,
		"failed":     len(failedIDs),
		"failed_ids": failedIDs,
		"dry_run":    req.DryRun,
		"ids":        ids,
	}})
}

// buildRegenRequest creates a PushRequest from an existing memory for
// re-enqueuing through the processing pipeline. Used by both
// RegenerateMetadata and CleanupMetadata so regeneration preserves
// all metadata context (ProjectContext, HubReason, BatchID, etc.).
//
// Bad titles are cleared so the pipeline treats the memory as needing
// a new title instead of preserving the corrupted one as "user-provided".
func buildRegenRequest(mem *model.Memory) model.PushRequest {
	title := mem.Title
	if looksLikeBadMetadata(title) {
		title = "" // force pipeline to generate a new title
	}
	return model.PushRequest{
		Content:             mem.Content,
		Title:               title,
		Hint:                mem.Hint,
		Kind:                mem.Kind,
		Stability:           mem.Stability,
		Tags:                append([]string(nil), mem.Tags...),
		ContentType:         mem.ContentType,
		Source:              mem.Source,
		SourceAgent:         mem.SourceAgent,
		SourcePath:          mem.SourcePath,
		HubReason:           mem.HubReason,
		ProjectContext:      cloneStringMap(mem.ProjectContext),
		BatchID:             mem.BatchID,
		AllowRelatedContext: true,
	}
}

// badMetadataFieldMarker matches JSON field syntax: "title"\s*: or "summary"\s*:
// This is more precise than a plain substring match — it won't false-positive on
// valid content that mentions "title" or "summary" as prose (e.g. a memory about
// JSON Schema or OpenAPI specs).
var badMetadataFieldMarker = regexp.MustCompile(`"(?:title|summary)"\s*:`)

// badMetadataLabelPrefix matches "Summary:" or "Title:" at the start of a string
// (case-insensitive). These are LLM failure shapes where the model outputs labeled
// fields instead of prose.
var badMetadataLabelPrefix = regexp.MustCompile(`(?i)^\s*(?:summary|title)\s*:`)

// looksLikeBadMetadata returns true if a string looks like raw LLM protocol output.
func looksLikeBadMetadata(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	if badMetadataFieldMarker.MatchString(trimmed) {
		return true
	}
	return badMetadataLabelPrefix.MatchString(trimmed)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (h *MemoriesHandler) runReindex() {
	slog.Info("starting full re-indexing of all chunks")
	start := time.Now()

	chunks := h.store.AllChunks()
	if len(chunks) == 0 {
		slog.Info("re-indexing finished: no chunks found")
		return
	}

	slog.Info("re-indexing chunks", "count", len(chunks))

	// Process in batches to avoid API limits and excessive memory usage
	batchSize := 50
	successCount := 0
	errorCount := 0

	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		batch := chunks[i:end]
		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.HeadingChain + "\n" + c.Content
		}

		embeddings, err := h.embedder.EmbedContext(context.Background(), texts, "document")
		if err != nil {
			slog.Error("reindex batch failed", "startIndex", i, "error", err)
			errorCount += len(batch)
			continue
		}

		for j := range batch {
			if j < len(embeddings) && embeddings[j] != nil {
				batch[j].Embedding = embeddings[j]
				if err := h.store.UpdateChunk(&batch[j]); err != nil {
					slog.Error("failed to update chunk embedding", "id", batch[j].ID, "error", err)
					errorCount++
				} else {
					successCount++
				}
			}
		}

		if (i/batchSize)%10 == 0 && i > 0 {
			slog.Info("re-indexing progress", "processed", i, "total", len(chunks), "success", successCount, "errors", errorCount)
		}
	}

	slog.Info("re-indexing complete",
		"total", len(chunks),
		"success", successCount,
		"errors", errorCount,
		"duration", time.Since(start).String(),
	)
}

// returnMemory re-fetches a memory with the author JOIN,
// then writes the canonical representation. All mutating endpoints
// (create, update, share) should use this instead of returning the raw struct.
func (h *MemoriesHandler) returnMemory(w http.ResponseWriter, status int, id, ownerID string) {
	canonical, err := h.store.GetMemory(id, ownerID)
	if err != nil {
		// Fallback: the mutation succeeded but the re-fetch failed — return 204
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, status, model.ApiResponse{Data: canonical})
}

// resolvedHubID returns the hub that writes should target.
// Active hub state is client-local; writes require explicit targeting or fall
// back to the personal hub in middleware.
func resolvedHubID(r *http.Request) string {
	return GetWriteHubID(r)
}

// attachTopicID populates TopicID on one or more memories from the memory_topics table.
// Uses a single batch query — no N+1.
func (h *MemoriesHandler) attachTopicID(ownerID string, memories ...*model.Memory) {
	topicMap, err := h.store.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil || len(topicMap) == 0 {
		return
	}
	for _, m := range memories {
		if topicID, ok := topicMap[m.ID]; ok {
			m.TopicID = topicID
		}
	}
}

// attachTopicIDSlice is like attachTopicID but for a slice (used by List).
func (h *MemoriesHandler) attachTopicIDSlice(ownerID string, memories []model.Memory) {
	topicMap, err := h.store.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil || len(topicMap) == 0 {
		return
	}
	for i := range memories {
		if topicID, ok := topicMap[memories[i].ID]; ok {
			memories[i].TopicID = topicID
		}
	}
}

// buildTopicHints loads existing topics for inline topic suggestion during classification.
func (h *MemoriesHandler) buildTopicHints(hubID string) []categorize.TopicHint {
	topics, err := h.store.ListTopics(hubID)
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
		hints = append(hints, categorize.TopicHint{
			ID:   t.ID,
			Name: t.Name,
			Path: getPath(t),
		})
	}
	return hints
}

func applySummaryResult(memory *model.Memory, content string, result summarize.Result) bool {
	if memory == nil {
		return false
	}
	changed := false
	if result.Summary != "" {
		memory.Summary = result.Summary
		changed = true
	}
	if result.Title == "" {
		return changed
	}
	candidate := ingesttitle.ResolveGeneratedCandidate(ingesttitle.ResolveInput{
		Title:       memory.Title,
		SourcePath:  memory.SourcePath,
		Content:     content,
		ContentType: memory.ContentType,
		Hint:        memory.Hint,
	}, result.Title)
	if candidate.Title != "" && candidate.Title != memory.Title && candidate.Reason != "user_preserved" {
		slog.Info("memory title updated", "memory_id", memory.ID, "old_title", memory.Title, "new_title", candidate.Title, "reason", candidate.Reason)
		memory.Title = candidate.Title
		changed = true
	}
	return changed
}

// buildHint combines a user-provided hint with auto-generated system context.
// System context describes provenance (source, filename, project) to help
// the LLM processing pipeline produce better classification, summaries, and tags.
func buildHint(userHint string, source string, sourcePath string, projectContext map[string]string) string {
	var sys string
	switch source {
	case "web":
		if sourcePath != "" {
			sys = fmt.Sprintf("Uploaded file '%s' on web app", sourcePath)
		} else {
			sys = "Note taken on web app"
		}
	case "cli":
		if sourcePath != "" {
			sys = fmt.Sprintf("Pushed file '%s' via CLI", sourcePath)
		} else {
			sys = "Pushed via CLI"
		}
	case "mcp":
		sys = "Saved by AI agent via MCP"
	case "sdk":
		sys = "Pushed via API"
	default:
		if source != "" {
			sys = fmt.Sprintf("Pushed via %s", source)
		}
	}

	// Append project context if available
	if projectContext != nil {
		if repo := projectContext["repo"]; repo != "" {
			sys += fmt.Sprintf(" in project %s", repo)
		}
	}

	// Combine system + user hint
	sys = strings.TrimSpace(sys)
	userHint = strings.TrimSpace(userHint)
	if len(userHint) > 500 {
		userHint = userHint[:500]
	}

	switch {
	case sys != "" && userHint != "":
		return sys + ". " + userHint
	case sys != "":
		return sys
	default:
		return userHint
	}
}

// attachAttachments populates Memory.Attachments for each memory.
// Caller MUST have authorized access to the parent memory already (via
// loadMemoryRespectingScope or equivalent). This helper queries the
// owner-scoped attachments index using memory.OwnerID — NOT the
// requester — because attachments are owned by the memory's owner,
// and a team-hub member viewing someone else's hub memory must still
// see the attachment list. The view/download endpoints likewise
// authorize via memory.OwnerID after their own scope-aware memory
// load, so the discovery path here matches the access path there.
func (h *MemoriesHandler) attachAttachments(memories ...*model.Memory) {
	for _, memory := range memories {
		if memory.OwnerID == "" {
			continue
		}
		attachments, err := h.store.ListMemoryAttachments(memory.ID, memory.OwnerID)
		if err != nil {
			continue
		}
		memory.Attachments = attachments
	}
}

// maybeClearHubOverLimit checks whether the hub's memory count is
// back under its plan's memory_limit and clears the
// hub_subscriptions.over_limit_since marker if so. Best-effort:
// failures log but don't affect the delete's success.
//
// Uses the authoritative Postgres COUNT (CountMemoriesByHub) rather
// than the Redis committed counter. The Redis counter is prone to
// cold-start false-zeros: after a Redis flush, AdjustHubMemoryCount
// materializes missing keys at 0, which would trick an any-Redis
// check into clearing a marker even when the hub is still over. PG
// is the source of truth for the membership count; the ~5ms extra
// is acceptable on a delete path.
func (h *MemoriesHandler) maybeClearHubOverLimit(ctx context.Context, hubID string) {
	if hubID == "" || h.hubQuota == nil {
		return
	}
	limit := h.hubQuota.GetHubMemoryLimit(ctx, hubID)
	unlimited := model.IsUnlimited(limit)
	var count int
	if !unlimited {
		c, err := h.store.CountMemoriesByHub(ctx, hubID)
		if err != nil {
			slog.Warn("count hub memories failed; leaving over_limit_since in place",
				"hub_id", hubID, "error", err)
			return
		}
		if c > limit {
			return
		}
		count = c
	}
	transitioned, err := h.store.ClearHubOverLimit(ctx, hubID)
	if err != nil {
		slog.Warn("clear hub over_limit_since failed",
			"hub_id", hubID, "error", err)
		return
	}
	if !transitioned {
		return
	}
	// Edge: over_limit_since moved from set → null. Notify owner +
	// admins once. Hub lookup failure is non-fatal — the DB write
	// already succeeded; we just don't send the receipt.
	hub, _ := h.store.GetHub(hubID)
	if hub == nil {
		return
	}
	payload := buildHubQuotaPayload(ctx, h.store, hub, "", limit)
	if !unlimited {
		payload.MemoryCount = count
	}
	// Cycle id = now(). Each restore is a unique moment; back-to-back
	// restores (if they ever occurred) would still dedupe within a
	// nanosecond window, which is fine — the notification is a
	// one-shot receipt per cycle close.
	dispatchHubQuotaNotification(ctx, h.store, h.events, hub,
		model.NotificationKindHubRestored, hubQuotaRestored, time.Now().UTC(), payload)
}

// NotifyHubRestored dispatches the hub_restored notification for a
// hub that just had over_limit_since cleared by a path OTHER than
// the normal delete flow (today: the billing service's plan-upgrade
// path). Exported so the composition root can wire it as the
// billing.Service.SetNotifyHubRestored callback without billing
// having to depend on this package.
//
// Caller has already cleared over_limit_since. This function loads
// the hub + plan limit + count, builds the payload, and dispatches
// to owner + admins. Silent no-op for nil hub / empty hubID.
func (h *MemoriesHandler) NotifyHubRestored(ctx context.Context, hubID string) {
	if hubID == "" {
		return
	}
	hub, _ := h.store.GetHub(hubID)
	if hub == nil {
		return
	}
	var limit int = -1
	if h.hubQuota != nil {
		limit = h.hubQuota.GetHubMemoryLimit(ctx, hubID)
	}
	payload := buildHubQuotaPayload(ctx, h.store, hub, "", limit)
	// buildHubQuotaPayload already fills MemoryCount from PG.
	dispatchHubQuotaNotification(ctx, h.store, h.events, hub,
		model.NotificationKindHubRestored, hubQuotaRestored, time.Now().UTC(), payload)
}

func (h *MemoriesHandler) deleteAttachmentObjects(ctx context.Context, attachments []model.MemoryAttachment) {
	if h.objectStore == nil || len(attachments) == 0 {
		return
	}
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.StorageKey) == "" {
			continue
		}
		if err := h.objectStore.Delete(ctx, attachment.StorageKey); err != nil {
			slog.Warn("failed to delete attachment object", "attachment_id", attachment.ID, "memory_id", attachment.MemoryID, "error", err)
		}
	}
}

func (h *MemoriesHandler) persistOriginalAttachment(ctx context.Context, memory *model.Memory, req model.PushRequest, originalContent string) error {
	if !shouldPersistOriginal(req) || h.objectStore == nil {
		return nil
	}

	var (
		objectKey   string
		filename    string
		contentType string
		sizeBytes   int64
		sha         string
		// inMemoryBytes is populated only on the non-FileRef path,
		// where the server already holds the payload. Used for a cheap
		// in-memory image probe; avoids the extra bucket GET the
		// FileRef path has to pay.
		inMemoryBytes []byte
	)

	if req.FileRef != nil {
		objectKey = req.FileRef.ObjectKey
		filename = chooseFilename(req.FileRef.Filename, req.SourcePath)
		contentType = chooseAttachmentContentType(req.FileRef.ContentType, req.ContentType, filename)
		sizeBytes = req.FileRef.SizeBytes
		sha = req.FileRef.SHA256
	} else {
		data, derivedContentType, err := originalAttachmentBytes(req, originalContent)
		if err != nil {
			return err
		}
		filename = chooseFilename(req.SourcePath, req.Title)
		contentType = chooseAttachmentContentType(derivedContentType, req.ContentType, filename)
		sizeBytes = int64(len(data))
		sum := sha256.Sum256(data)
		sha = fmt.Sprintf("%x", sum[:])
		objectKey = buildAttachmentObjectKey(memory.OwnerID, memory.ID, filename)
		if err := h.objectStore.Put(ctx, objectstore.PutInput{
			Key:         objectKey,
			Body:        bytes.NewReader(data),
			ContentType: contentType,
			SizeBytes:   sizeBytes,
		}); err != nil {
			return err
		}
		inMemoryBytes = data
	}

	// Decode-gate: only rows whose bytes decode as one of the inline
	// whitelist raster formats become inline-eligible. Declared images
	// that fail the decode are downgraded to an opaque download-only
	// attachment so the /view endpoint cannot be tricked into serving
	// attacker-controlled bytes inline. Never fail the push — a bad
	// image probe is a product problem, not a rejection.
	width, height, inlineEligible := probeAttachmentImage(ctx, h.objectStore, contentType, inMemoryBytes, objectKey)
	if !inlineEligible && isDeclaredImage(contentType) {
		slog.Warn("attachment image decode failed, downgrading to download-only",
			"memory_id", memory.ID,
			"filename", filename,
			"declared_content_type", contentType,
		)
		contentType = "application/octet-stream"
	}

	attachment := &model.MemoryAttachment{
		ID:             generateID(),
		MemoryID:       memory.ID,
		OwnerID:        memory.OwnerID,
		Kind:           "original",
		Filename:       filename,
		ContentType:    contentType,
		SizeBytes:      sizeBytes,
		SHA256:         sha,
		StorageKey:     objectKey,
		Width:          width,
		Height:         height,
		InlineEligible: inlineEligible,
		CreatedAt:      time.Now(),
	}
	if err := h.store.CreateMemoryAttachment(attachment); err != nil {
		return err
	}
	memory.Attachments = append(memory.Attachments, *attachment)
	if memory.OriginalFileRef == "" {
		memory.OriginalFileRef = objectKey
		if err := h.store.UpdateMemory(memory); err != nil {
			slog.Warn("failed to update original file ref", "memory_id", memory.ID, "error", err)
		}
	}
	return nil
}

func shouldPersistOriginal(req model.PushRequest) bool {
	if req.FileRef != nil {
		return true
	}
	if req.SourcePath == "" {
		return false
	}
	switch req.Source {
	case "import", "extraction":
		return false
	default:
		return true
	}
}

func originalAttachmentBytes(req model.PushRequest, originalContent string) ([]byte, string, error) {
	switch req.ContentType {
	case "pdf", "image":
		data, err := base64.StdEncoding.DecodeString(originalContent)
		if err != nil {
			return nil, "", fmt.Errorf("decode base64 attachment: %w", err)
		}
		if req.ContentType == "pdf" {
			return data, "application/pdf", nil
		}
		return data, "", nil
	default:
		return []byte(originalContent), "", nil
	}
}

func chooseFilename(primary string, fallback string) string {
	name := strings.TrimSpace(primary)
	if name == "" {
		name = strings.TrimSpace(fallback)
	}
	name = filepath.Base(name)
	if name == "." || name == "" {
		return "attachment"
	}
	return name
}

func chooseAttachmentContentType(explicit string, memoryContentType string, filename string) string {
	if explicit != "" && explicit != "text" && explicit != "markdown" && explicit != "code" && explicit != "link" {
		return explicit
	}
	if guessed := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); guessed != "" {
		return guessed
	}
	switch memoryContentType {
	case "pdf":
		return "application/pdf"
	case "image":
		return "image/*"
	case "markdown":
		return "text/markdown; charset=utf-8"
	case "html":
		return "text/html; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func defaultContentType(fileRef *model.FileRef, sourcePath string) string {
	filename := sourcePath
	if fileRef != nil && fileRef.Filename != "" {
		filename = fileRef.Filename
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md":
		return "markdown"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return "image"
	case ".html", ".htm":
		return "html"
	default:
		return "text"
	}
}

func deriveInitialContentHash(req model.PushRequest) string {
	if req.FileRef != nil && req.FileRef.SHA256 != "" {
		return req.FileRef.SHA256
	}
	if req.FileRef != nil {
		basis := fmt.Sprintf("%s\n%s\n%d", req.FileRef.ObjectKey, req.FileRef.Filename, req.FileRef.SizeBytes)
		return fmt.Sprintf("%x", sha256.Sum256([]byte(basis)))
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(req.Content)))
}

func buildAttachmentObjectKey(ownerID, memoryID, filename string) string {
	return fmt.Sprintf("owners/%s/memories/%s/originals/%s-%s", ownerID, memoryID, generateID(), sanitizeObjectName(filename))
}

// probeAttachmentImage returns (width, height, true) only for
// declared-image attachments whose bytes decode as one of the inline
// whitelist raster formats. It prefers the in-memory buffer (cheap,
// non-FileRef path) and falls back to a bounded object-store GET for
// the FileRef path. Any non-image or non-whitelisted content-type
// returns (nil, nil, false) without touching bytes.
func probeAttachmentImage(
	ctx context.Context,
	store objectstore.Store,
	contentType string,
	inMemory []byte,
	objectKey string,
) (width *int, height *int, inlineEligible bool) {
	if !attachments.IsInlineWhitelisted(contentType) {
		return nil, nil, false
	}
	var w, h int
	var ok bool
	if len(inMemory) > 0 {
		w, h, ok = attachments.ProbeImageBytes(inMemory)
	} else {
		w, h, ok = attachments.ProbeImageObject(ctx, store, objectKey)
	}
	if !ok {
		return nil, nil, false
	}
	return &w, &h, true
}

// isDeclaredImage reports whether the content-type would be treated
// as an image absent the decode gate. Used purely to scope the
// downgrade + warning log: non-image rows skip the probe and retain
// their original content-type untouched.
func isDeclaredImage(contentType string) bool {
	media := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(media, ";"); idx >= 0 {
		media = strings.TrimSpace(media[:idx])
	}
	return strings.HasPrefix(media, "image/")
}

func sanitizeObjectName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" || name == "." {
		return "attachment"
	}
	return name
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based if crypto/rand fails
		b = []byte(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	// Format as UUID v4: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// writeJSON writes an ApiResponse to the client. Accepts only model.ApiResponse
// to enforce the { data, error } envelope at compile time — raw JSON responses
// bypass the CLI/SDK's response parser and cause silent failures.
//
// Defaults Cache-Control to `no-store` when the caller hasn't set one.
// This is defense-in-depth for when a CDN (Cloudflare) sits in front —
// even a misconfigured cache rule won't cache an API response that
// explicitly opts out. Handlers that DO want caching (e.g. /v1/plans
// with `public, max-age=60`) set Cache-Control BEFORE calling writeJSON
// and the default is skipped. Binary streamers like ServeAttachmentView
// don't go through writeJSON at all; they write their own Cache-Control
// on the raw ResponseWriter.
func writeJSON(w http.ResponseWriter, status int, v model.ApiResponse) {
	w.Header().Set("Content-Type", "application/json")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	WriteError(w, status, code, message)
}

// WriteError writes a standard error response using the ApiResponse envelope.
// Exported for use by middleware packages (e.g., meter) that need to write
// error responses consistent with the handler conventions.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, model.ApiResponse{
		Error: &model.Error{Code: code, Message: message},
	})
}

// WriteErrorWithDetails writes a standard error response with machine-readable
// details. Used for quota exceeded responses where clients need structured data
// to render upgrade CTAs and usage bars.
func WriteErrorWithDetails(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, model.ApiResponse{
		Error: &model.Error{Code: code, Message: message, Details: details},
	})
}

// memorySearchRow is the chunk-grouped, title-enriched memory match
// returned by the fast full-text search path. Shared between the
// `/v1/memories/search` endpoint (emitted as a flat array on the wire)
// and `/v1/bar/search` (nested under `memories`), so both consumers see
// the same row shape.
//
// Hub and topic attribution is enriched server-side so quick-match rows
// can render the correct hub/topic chip even for cross-hub results that
// the client has no cached memory row for. The zero values (empty ids
// and names) are valid — they mean "not applicable" (e.g. unassigned
// topic, or a hub context the viewer already sits in).
type memorySearchRow struct {
	MemoryID     string `json:"memory_id"`
	Title        string `json:"title"`
	Snippet      string `json:"snippet"`
	Kind         string `json:"kind"`
	Stability    string `json:"stability"`
	HeadingChain string `json:"heading_chain,omitempty"`
	HubID        string `json:"hub_id,omitempty"`
	HubName      string `json:"hub_name,omitempty"`
	TopicID      string `json:"topic_id,omitempty"`
	TopicName    string `json:"topic_name,omitempty"`
}

// searchMemoriesCore runs the fast full-text search path (trigram +
// tsvector, no embeddings), groups chunks by memory_id, and enriches
// titles from the memories table. Shared by `Search` and BarHandler.
//
// hubIDs should be the request's accessible hub list (from
// GetAccessibleHubIDs); pass nil / empty slice for an owner-only
// query (only valid when strict=false).
//
// strict mirrors RecallScope.Strict — when true (OAuth grant or API
// key with HubScopeAllowlist), the search routes to
// SearchChunksInHubs (strict hub-only) and an empty hubIDs returns
// zero results rather than falling through to owner-only search.
// This is the same security boundary the recall pipeline enforces;
// keeping it consistent across surfaces is the load-bearing property.
func (h *MemoriesHandler) searchMemoriesCore(ctx context.Context, q string, ownerID string, hubIDs []string, topicID string, limit int, strict bool) ([]memorySearchRow, error) {
	var filters *model.SearchFilters
	if topicID != "" {
		filters = &model.SearchFilters{TopicID: topicID, Explicit: true}
	}

	searchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var chunks []model.Chunk
	var err error
	switch {
	case strict && len(hubIDs) > 0:
		// Scope-bounded principal. owner-OR-hub would leak owner
		// content from non-granted hubs.
		chunks, err = h.store.SearchChunksInHubs(searchCtx, q, nil, hubIDs, limit*3, filters, nil)
	case strict && len(hubIDs) == 0:
		// Scope-bounded principal with no resolvable hubs (frozen,
		// disjoint allowlist, etc.). MUST NOT fall through to
		// SearchChunks — that would leak the owner's full corpus.
		// Return zero matches.
		return nil, nil
	case len(hubIDs) > 0:
		// Default cross-hub search. Same semantics as the existing
		// path: owner_id = $user OR hub_id = ANY($hubs). Web/CLI
		// session paths.
		chunks, err = h.store.SearchChunksForHubs(searchCtx, q, nil, ownerID, hubIDs, limit*3, filters, nil)
	default:
		// Unscoped session with no accessible hubs (unusual; the
		// personal hub is typically present).
		chunks, err = h.store.SearchChunks(searchCtx, q, nil, ownerID, limit*3, filters, nil)
	}
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	results := make([]memorySearchRow, 0, limit)
	for _, c := range chunks {
		if seen[c.MemoryID] {
			continue
		}
		seen[c.MemoryID] = true

		snippet := c.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}

		results = append(results, memorySearchRow{
			MemoryID:     c.MemoryID,
			Title:        c.HeadingChain,
			Snippet:      snippet,
			Kind:         c.Kind,
			Stability:    c.Stability,
			HeadingChain: c.HeadingChain,
		})
		if len(results) >= limit {
			break
		}
	}

	// Enrich rows with canonical title + hub/topic attribution. Without
	// this, quick-match rows for memories outside the user's cached
	// recents (typically cross-hub or older results) render with no
	// attribution chip, and the user cannot tell which hub/topic a
	// result belongs to before opening it. Cache hub/topic name
	// lookups per request — at limit=10 we often revisit the same
	// hub multiple times and pay N lookups, not N×M.
	hubNameCache := make(map[string]string, len(hubIDs))
	type topicKey struct{ id, hubID string }
	topicNameCache := make(map[topicKey]string)
	for i := range results {
		// Enrichment must use the SAME access predicate as the search
		// itself, otherwise a scoped principal could see attribution
		// (hub/topic name) for memories the search correctly filtered
		// out — a metadata leak of equal severity to leaking the row.
		// strict mode → strict hub-only lookup; default → owner-OR-hub.
		var (
			m   *model.Memory
			err error
		)
		if strict {
			m, err = h.store.GetMemoryInHubs(searchCtx, results[i].MemoryID, hubIDs)
		} else {
			m, err = h.store.GetAccessibleMemory(results[i].MemoryID, ownerID, hubIDs)
		}
		if err != nil || m == nil {
			continue
		}
		results[i].Title = m.Title
		if m.HubID != "" {
			results[i].HubID = m.HubID
			name, cached := hubNameCache[m.HubID]
			if !cached {
				if hub, err := h.store.GetHub(m.HubID); err == nil && hub != nil {
					name = hub.Name
				}
				hubNameCache[m.HubID] = name
			}
			results[i].HubName = name
		}
		if m.TopicID != "" {
			results[i].TopicID = m.TopicID
			key := topicKey{id: m.TopicID, hubID: m.HubID}
			name, cached := topicNameCache[key]
			if !cached {
				if topic, err := h.store.GetTopic(m.TopicID, m.HubID); err == nil && topic != nil {
					name = topic.Name
				}
				topicNameCache[key] = name
			}
			results[i].TopicName = name
		}
	}
	return results, nil
}

// Search performs a fast full-text search (trigram + tsvector only, no embeddings).
// Returns memory-level results grouped from chunk matches.
// Target latency: <200ms for the progressive search middle layer.
func (h *MemoriesHandler) Search(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing_query", "q parameter is required")
		return
	}

	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	topicID := r.URL.Query().Get("topic_id")
	if topicID != "" && !isValidUUID(topicID) {
		writeError(w, http.StatusBadRequest, "invalid_topic_id", "topic_id must be a valid UUID")
		return
	}

	hubIDs := GetAccessibleHubIDs(r)
	results, err := h.searchMemoriesCore(r.Context(), q, ownerID, hubIDs, topicID, limit, isHubScopeBounded(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: results,
	})
}

// parseDuration parses human-friendly durations like "12h", "1d", "7d", "30d".
// Falls back to time.ParseDuration for standard Go durations.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
