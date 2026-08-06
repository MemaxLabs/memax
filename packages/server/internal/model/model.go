package model

import (
	"strings"
	"time"
)

const (
	MemoryKindEpisodic   = "episodic"
	MemoryKindSemantic   = "semantic"
	MemoryKindProcedural = "procedural"
	MemoryKindRationale  = "rationale"

	MemoryStabilityVolatile = "volatile"
	MemoryStabilityEvolving = "evolving"
	MemoryStabilityStable   = "stable"
)

func NormalizeMemoryKind(kind string) string {
	switch kind {
	case MemoryKindEpisodic, MemoryKindSemantic, MemoryKindProcedural, MemoryKindRationale:
		return kind
	default:
		return MemoryKindSemantic
	}
}

func NormalizeAgentSlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, " ", "-")
}

func NormalizeMemoryStability(stability string) string {
	switch stability {
	case MemoryStabilityVolatile, MemoryStabilityEvolving, MemoryStabilityStable:
		return stability
	default:
		return MemoryStabilityEvolving
	}
}

func DefaultRetrievalWeight(weight float64) float64 {
	if weight <= 0 {
		return 1.0
	}
	return weight
}

type Memory struct {
	ID              string         `json:"id"`
	HubID           string         `json:"hub_id"`
	OwnerID         string         `json:"owner_id"`
	Title           string         `json:"title"`
	Content         string         `json:"content"`
	ContentType     string         `json:"content_type"`
	ContentHash     string         `json:"content_hash"`
	Summary         string         `json:"summary"`
	Hint            string         `json:"hint,omitempty"`
	Kind            string         `json:"kind"`
	Stability       string         `json:"stability"`
	RetrievalWeight float64        `json:"retrieval_weight"`
	AccessIntents   map[string]int `json:"access_intents,omitempty"`
	Tags            []string       `json:"tags"`
	Boundary        string         `json:"boundary"`
	State           string         `json:"state"`
	Pinned          bool           `json:"pinned"`
	Source          string         `json:"source"`
	// SourceKind is a sub-classification under Source. For onboarding
	// seed memories: Source = "system", SourceKind = "onboarding-seed".
	// Plan 23 §4.1. Empty for legacy rows + non-seed memories.
	SourceKind string `json:"source_kind,omitempty"`
	// Metadata is a structured bag for per-memory facts that don't fit
	// existing columns. Used by seeds (`metadata.seed_origin_id`
	// references the source seed-template UUID for idempotency); future
	// callers may attach other keys. Empty/nil for memories that have
	// no structured metadata.
	Metadata        map[string]any     `json:"metadata,omitempty"`
	SourceAgent     string             `json:"source_agent,omitempty"` // agent identity: "claude-code", "cursor", "copilot", etc.
	AssistedByAgent string             `json:"assisted_by_agent,omitempty"`
	SourcePath      string             `json:"source_path,omitempty"`
	HubReason       string             `json:"hub_reason,omitempty"`        // rationale for pushing to a shared hub
	OriginalFileRef string             `json:"original_file_ref,omitempty"` // R2 key for original PDF/image
	Attachments     []MemoryAttachment `json:"attachments,omitempty"`
	ProjectContext  map[string]string  `json:"project_context,omitempty"` // {"repo": "github.com/org/repo", "project": "repo", "branch": "main"}
	EventDates      []time.Time        `json:"event_dates,omitempty"`     // dates mentioned in content, extracted at ingest
	BatchID         string             `json:"batch_id,omitempty"`        // Groups memories from the same multi-file drop
	// SourceFetchHash is the body hash of the URL-source memory's
	// content as last RE-FETCHED by the URL-drift worker. Distinct
	// from ContentHash, which is the hash of the body STORED at
	// ingest time. A non-null SourceFetchHash != ContentHash signals
	// URL drift — the canonical content has changed since we
	// ingested it. Empty string = never re-fetched (the column is
	// nullable + new-ingest-only by design; pre-Phase-2a memories
	// stay empty until a re-fetch lands).
	SourceFetchHash string `json:"source_fetch_hash,omitempty"`
	// UserFollowupMarker is the @TODO/@FIXME/@QUESTION text the
	// ingest distill stage extracted from the memory body. Holds
	// the matched marker text (e.g. "@TODO verify this number")
	// so the dream agent has actionable context, not just a
	// boolean. Empty string = no marker found (or pre-Phase-2a
	// memory that hasn't been re-ingested).
	UserFollowupMarker string    `json:"user_followup_marker,omitempty"`
	Version            int       `json:"version"`
	TopicID            string    `json:"topic_id,omitempty"` // populated at query time, not stored
	AccessCount        int       `json:"access_count"`
	ShownCount         int       `json:"shown_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	AccessedAt         time.Time `json:"accessed_at"`
	// Denormalized attribution info — populated at query time via JOIN, not stored on memory row
	AuthorName       string            `json:"author_name,omitempty"`
	AuthorAvatarURL  string            `json:"author_avatar_url,omitempty"`
	HubName          string            `json:"hub_name,omitempty"`
	AgentDisplayName string            `json:"agent_display_name,omitempty"`
	AgentIcon        string            `json:"agent_icon,omitempty"`
	Provenance       *MemoryProvenance `json:"provenance,omitempty"`
	// Internal storage-backed provenance fields. The API exposes the nested
	// Provenance object above; these remain server-only.
	ProvenanceCreatedByType        string `json:"-"`
	ProvenanceCreatedBySlug        string `json:"-"`
	ProvenanceCreatedByDisplayName string `json:"-"`
	ProvenanceCreatedVia           string `json:"-"`
	ProvenanceAssistedByAgent      string `json:"-"`
	ProvenanceInitiationType       string `json:"-"`
	ProvenanceAttributionSource    string `json:"-"`
	// Lifecycle carries server-resolved scan-surface + detail-durable
	// signals for the memory row and detail page. Always populated on
	// reads (pointer kept so the zero-value case is explicit). See
	// MemoryLifecycle documentation for field semantics.
	Lifecycle *MemoryLifecycle `json:"lifecycle,omitempty"`
}

// MemoryLifecycle holds the dream-delta signals attached to a memory on
// read. Two fields with distinct scopes (see lifecycle design doc):
//
//   - PendingDreamAction drives scan surfaces (row breadcrumb tint).
//     Scoped to the viewer's last visit of the memory's CURRENT topic;
//     server returns nil after the viewer visits (next read resolves to
//     nil; no client-side mutation needed to clear).
//
//   - DreamHistory drives the memory detail page provenance strip. Up
//     to 10 most recent dream actions touching this memory, unscoped by
//     topic_visits, durable regardless of viewer activity. Empty slice
//     on list reads (resolver does not fetch history for efficiency);
//     populated only on detail reads.
type MemoryLifecycle struct {
	PendingDreamAction *DreamActionRef  `json:"pending_dream_action"`
	DreamHistory       []DreamActionRef `json:"dream_history"`
}

// DreamActionRef is the client-facing shape of a dream action entry.
// Embedded in MemoryLifecycle (both pending and history) and Topic
// lifecycle payloads.
//
// FromTopic / ToTopic are nullable — pre-migration-069 historical rows
// stay nil and UI renders verb + reason only when either side is absent.
type DreamActionRef struct {
	RunID      string         `json:"run_id"`
	ActionType string         `json:"action_type"` // organize | merge | archive | restructure
	At         time.Time      `json:"at"`          // == dream_actions.created_at
	FromTopic  *DreamTopicRef `json:"from_topic"`
	ToTopic    *DreamTopicRef `json:"to_topic"`
	Reason     string         `json:"reason,omitempty"`
}

type DreamTopicRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Icon is the lucide icon name at the time the reference is resolved
	// (empty string = no icon). Kept so the dream-history and hover-popover
	// chips can render full topic identity (icon + name), matching the row
	// breadcrumb chip. If the icon becomes out-of-scope or the topic is
	// deleted, the whole DreamTopicRef will be nil via the scoped join —
	// this field never carries a stale icon when the name is absent.
	Icon string `json:"icon,omitempty"`
}

type MemoryProvenance struct {
	CreatedByType        string `json:"created_by_type"`
	CreatedBySlug        string `json:"created_by_slug,omitempty"`
	CreatedByDisplayName string `json:"created_by_display_name,omitempty"`
	CreatedVia           string `json:"created_via,omitempty"`
	AssistedByAgent      string `json:"assisted_by_agent,omitempty"`
	InitiationType       string `json:"initiation_type"`
	AttributionSource    string `json:"attribution_source,omitempty"`
}

const (
	MemoryCreatedByHuman = "human"
	MemoryCreatedByAgent = "agent"
)

// Plan 23 §5.4 — system actor + tutorial-template hub. Both UUIDs are
// inserted by migration 003. Application code references them through
// these constants instead of hardcoding.
const (
	SystemUserID            = "00000000-0000-0000-0000-00000000ada7"
	TutorialHubID           = "00000000-0000-0000-0000-00000000babe"
	TutorialHubSlug         = "__memax_tutorial__"
	MemorySourceSystem      = "system"
	MemorySourceKindOnboard = "onboarding-seed"
)

const (
	MemoryInitiationHumanDirect         = "human_direct"
	MemoryInitiationHumanRequestedAgent = "human_requested_agent"
	MemoryInitiationAgentProactive      = "agent_proactive"
	MemoryInitiationAgentAutomatic      = "agent_automatic"
	MemoryInitiationImport              = "import"
	MemoryInitiationUnknown             = "unknown"
)

const (
	MemoryAttributionSourceHuman         = "human"
	MemoryAttributionSourceAuth          = "auth"
	MemoryAttributionSourceClaim         = "claim"
	MemoryAttributionSourceInherited     = "inherited"
	MemoryAttributionSourceLegacyHuman   = "legacy_human"
	MemoryAttributionSourceLegacyAgent   = "legacy_source_agent"
	MemoryAttributionSourceServerDefault = "server_default"
)

func NormalizeMemoryCreatedByType(value string) string {
	switch value {
	case MemoryCreatedByHuman, MemoryCreatedByAgent:
		return value
	default:
		return MemoryCreatedByHuman
	}
}

func NormalizeMemoryInitiationType(value string) string {
	switch value {
	case MemoryInitiationHumanDirect,
		MemoryInitiationHumanRequestedAgent,
		MemoryInitiationAgentProactive,
		MemoryInitiationAgentAutomatic,
		MemoryInitiationImport,
		MemoryInitiationUnknown:
		return value
	default:
		return MemoryInitiationUnknown
	}
}

func EffectiveMemoryAgentSlug(m *Memory) string {
	if m == nil {
		return ""
	}
	if m.ProvenanceCreatedBySlug != "" {
		return m.ProvenanceCreatedBySlug
	}
	return m.SourceAgent
}

func NormalizeMemoryProvenanceFields(m *Memory) {
	if m == nil {
		return
	}

	slug := EffectiveMemoryAgentSlug(m)
	createdByType := NormalizeMemoryCreatedByType(m.ProvenanceCreatedByType)
	if slug != "" {
		createdByType = MemoryCreatedByAgent
	}

	initiationType := NormalizeMemoryInitiationType(m.ProvenanceInitiationType)
	if createdByType == MemoryCreatedByHuman && initiationType == MemoryInitiationUnknown {
		initiationType = MemoryInitiationHumanDirect
	}
	if createdByType == MemoryCreatedByHuman &&
		m.ProvenanceAssistedByAgent == "" &&
		initiationType == MemoryInitiationHumanRequestedAgent &&
		m.SourceAgent != "" {
		m.ProvenanceAssistedByAgent = m.SourceAgent
	}

	m.ProvenanceCreatedByType = createdByType
	m.ProvenanceCreatedBySlug = slug
	m.ProvenanceInitiationType = initiationType
}

func BuildMemoryProvenance(m *Memory) *MemoryProvenance {
	if m == nil {
		return nil
	}

	createdByType := NormalizeMemoryCreatedByType(m.ProvenanceCreatedByType)
	initiationType := NormalizeMemoryInitiationType(m.ProvenanceInitiationType)
	slug := EffectiveMemoryAgentSlug(m)
	if createdByType == MemoryCreatedByHuman && slug != "" {
		createdByType = MemoryCreatedByAgent
	}
	if createdByType == MemoryCreatedByHuman && initiationType == MemoryInitiationUnknown {
		initiationType = MemoryInitiationHumanDirect
	}
	return &MemoryProvenance{
		CreatedByType:        createdByType,
		CreatedBySlug:        slug,
		CreatedByDisplayName: m.ProvenanceCreatedByDisplayName,
		CreatedVia:           m.ProvenanceCreatedVia,
		AssistedByAgent:      firstNonEmptyMemoryAgentSlug(m.ProvenanceAssistedByAgent, createdByType, initiationType, m.SourceAgent),
		InitiationType:       initiationType,
		AttributionSource:    m.ProvenanceAttributionSource,
	}
}

func firstNonEmptyMemoryAgentSlug(value, createdByType, initiationType, legacySourceAgent string) string {
	if value != "" {
		return value
	}
	if createdByType == MemoryCreatedByHuman &&
		initiationType == MemoryInitiationHumanRequestedAgent &&
		legacySourceAgent != "" {
		return legacySourceAgent
	}
	return ""
}

// Batch move result reason codes.
const (
	// BatchMoveSkipNotOwned — the requested memory is not owned by the caller.
	// For team hubs the memory is visible but another user created it, so a
	// user-initiated move cannot reassign it. Returned verbatim to clients so
	// the UI can report a partial-skip count.
	BatchMoveSkipNotOwned = "not_owned"
	// BatchMoveSkipNotFound — the memory id does not exist (invalid id or
	// already deleted). Distinguished from not_owned so clients can render a
	// different message.
	BatchMoveSkipNotFound = "not_found"
	// BatchMoveSkipAlreadyAtTarget — the memory's current hub+topic already
	// matches the requested destination. The server-side no-op is a success
	// from a correctness standpoint but reported so the UI can avoid noisy
	// "moved" confirmations for identity moves.
	BatchMoveSkipAlreadyAtTarget = "already_at_target"
	// BatchMoveSkipSourceDeleteForbidden — the caller owns the memory but
	// lacks authority to remove it from its current hub. Move is
	// semantically delete-from-source + create-in-destination; the source
	// hub's contributor_delete_policy MUST be honored. Without this reason
	// code, a contributor in a "none"-policy hub could bypass the policy
	// by moving their own memories to a personal hub and deleting them
	// there. Applies only to cross-hub moves — same-hub topic
	// reassignments do not cross any authority boundary and skip this
	// check entirely.
	BatchMoveSkipSourceDeleteForbidden = "source_delete_forbidden"
)

// SkippedMemory identifies a memory that was not moved during a batch-move
// request, along with the reason the server refused or short-circuited it.
type SkippedMemory struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// BatchMoveResult is the structured response from a batch-move request.
// Moved is the count of memories actually reassigned in this transaction.
// Skipped lists every input id that was not moved, with a reason code from
// the BatchMoveSkip* constants above.
type BatchMoveResult struct {
	Moved   int             `json:"moved"`
	Skipped []SkippedMemory `json:"skipped"`
}

// Batch delete result reason codes.
const (
	// BatchDeleteSkipNotOwned — the caller does not own the memory and
	// does not hold a hub role + contributor_delete_policy that covers it.
	// Collapses the "wrong owner, no hub context" and "hub member but
	// policy forbids this delete" cases into one client-facing code so
	// the UI can show a single permission-denied message per id.
	BatchDeleteSkipNotOwned = "not_owned"
	// BatchDeleteSkipNotFound — the memory id does not exist at request
	// time. Covers unknown ids, already-deleted ids, and the race where a
	// row is removed concurrently between GetAccessibleMemories and the
	// actual DELETE. From the client's perspective all three are "the
	// memory is gone" and one code is sufficient.
	BatchDeleteSkipNotFound = "not_found"
	// BatchDeleteSkipDeleteFailed — a store-level error occurred while
	// attempting to delete this id (postgres returned an error mid-batch,
	// object-store transient failure, etc). Distinguished from
	// not_found so clients can retry infra failures without blaming the
	// user. Handler logs the underlying error via slog.Error.
	BatchDeleteSkipDeleteFailed = "delete_failed"
)

// BatchDeleteResult is the structured response from a batch-delete request.
// Deleted is the count of memories actually removed in this request.
// Skipped lists every input id that was not deleted, with a reason code from
// the BatchDeleteSkip* constants above. Reuses SkippedMemory (same shape as
// batch-move) so clients can share decode helpers.
type BatchDeleteResult struct {
	Deleted int             `json:"deleted"`
	Skipped []SkippedMemory `json:"skipped"`
}

// Agent disconnect result reason codes.
const (
	// AgentDisconnectSkipNotFound — GetConnectedAgent returned an error
	// (unknown slug or another user's agent). Treated as idempotent
	// success on the client since the user's target state is reached.
	AgentDisconnectSkipNotFound = "not_found"
	// AgentDisconnectSkipCascadeFailed — the atomic tx that revokes keys,
	// tombstones configs, and deletes the agent row returned an error.
	// The server still has the agent, so the client must roll back the
	// optimistic removal and surface an inline retry hint.
	AgentDisconnectSkipCascadeFailed = "cascade_failed"
)

// API key revoke result reason codes.
const (
	// ApiKeyRevokeSkipNotFound — the key id did not resolve for the
	// caller. Covers unknown ids, already-revoked keys, and keys owned
	// by a different user (the SQL WHERE clause makes them
	// indistinguishable). Client treats this as idempotent success —
	// the user's target state is reached.
	ApiKeyRevokeSkipNotFound = "not_found"
	// ApiKeyRevokeSkipRevokeFailed — the DELETE itself returned a
	// store-level error. Server still has the key; client rolls back
	// the optimistic removal and surfaces an inline retry hint.
	ApiKeyRevokeSkipRevokeFailed = "revoke_failed"
)

// ApiKeyRevokeResult is the structured response from DELETE /v1/auth/api-keys/{id}.
//
// Always 200 with the result in the ApiResponse envelope — 4xx/5xx are
// reserved for full-request failures (bad path, auth). Partial-success
// semantics mirror memories.batchDelete and configs.batchDelete:
// `revoked: false` with a non-empty `skipped` array is a normal outcome
// and the client branches on the reason to distinguish silent success
// (`not_found`) from rollback + retry (`revoke_failed`).
//
// Scripted CLI callers rely on `not_found` being treated as exit 0 so
// idempotent cleanup flows don't break on already-revoked keys — see
// `memax auth revoke-key` for the matching semantics.
type ApiKeyRevokeResult struct {
	Revoked bool            `json:"revoked"`
	Skipped []SkippedMemory `json:"skipped"`
}

// AgentDisconnectResult is the structured response from DELETE /v1/agents/{slug}.
//
// Disconnect is a single-entity cascade (revoke api keys + tombstone configs
// + delete agent row + delete sync states) run in one Postgres transaction.
// The counts reported here are a pre-query snapshot taken right before the
// tx runs — they describe what the disconnect is about to clean up, not the
// exact number of rows committed. This matches the memory-move pattern where
// the result describes the user-visible outcome ("disconnected claude-code,
// revoked 3 keys, forgot 12 configs") rather than the raw exec.RowsAffected.
//
// Only `cascade_failed` justifies an onError rollback. `not_found` means the
// agent is already gone — the client's optimistic removal was correct.
type AgentDisconnectResult struct {
	Disconnected      bool            `json:"disconnected"`
	KeysRevoked       int             `json:"keys_revoked"`
	ConfigsTombstoned int             `json:"configs_tombstoned"`
	Skipped           []SkippedMemory `json:"skipped"`
}

type MemoryAttachment struct {
	ID          string `json:"id"`
	MemoryID    string `json:"memory_id"`
	OwnerID     string `json:"owner_id"`
	Kind        string `json:"kind"` // MemoryAttachmentKind: "original"
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	StorageKey  string `json:"-"`
	// Width/Height are set only for images that decoded successfully at
	// upload time. Nil for non-images and for declared-image rows whose
	// bytes failed image.DecodeConfig. Callers use them to reserve layout
	// space and avoid CLS.
	Width  *int `json:"width,omitempty"`
	Height *int `json:"height,omitempty"`
	// InlineEligible gates whether the signed-view endpoint may serve
	// this row with Content-Disposition: inline. Set to true only when
	// (a) the declared content-type is on the raster inline whitelist
	// AND (b) the bytes decoded successfully as that image type. False
	// for every other row, including decode-failed declared images
	// (whose ContentType is also downgraded to application/octet-stream
	// so the view path cannot be coaxed inline even if the whitelist
	// later widens).
	InlineEligible bool      `json:"inline_eligible"`
	CreatedAt      time.Time `json:"created_at"`
}

type Chunk struct {
	ID              string    `json:"id"`
	MemoryID        string    `json:"memory_id"`
	Content         string    `json:"content"`
	HeadingChain    string    `json:"heading_chain"`
	ChunkIndex      int       `json:"chunk_index"`
	TokenCount      int       `json:"token_count"`
	Embedding       []float64 `json:"embedding,omitempty"`
	RelevanceScore  float64   `json:"relevance_score,omitempty"` // similarity score from vector search
	Language        string    `json:"language,omitempty"`
	SearchConfig    string    `json:"-"`
	Kind            string    `json:"kind"`
	Stability       string    `json:"stability"`
	RetrievalWeight float64   `json:"retrieval_weight"`
	Hint            string    `json:"hint,omitempty"`         // denormalized from parent memory for embeddings + search_text
	TagsText        string    `json:"-"`                      // denormalized from parent memory tags for lexical search
	MetadataText    string    `json:"-"`                      // denormalized author/hub/source metadata for lexical search
	ProjectRepo     string    `json:"project_repo,omitempty"` // denormalized from parent memory for search filtering
	CreatedAt       time.Time `json:"created_at"`
}

type RecallRequest struct {
	Query           string            `json:"query"`
	HubIDs          []string          `json:"hub_ids,omitempty"`
	Kind            string            `json:"kind,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	TopicID         string            `json:"topic_id,omitempty"`
	Limit           int               `json:"limit,omitempty"`
	IncludeArchived bool              `json:"include_archived,omitempty"`
	NoRerank        bool              `json:"no_rerank,omitempty"`
	Source          string            `json:"source,omitempty"`
	WorkingDir      string            `json:"working_dir,omitempty"`
	ProjectContext  map[string]string `json:"project_context,omitempty"` // current project for context-aware boosting
	CreatedAfter    *time.Time        `json:"created_after,omitempty"`   // explicit temporal lower bound
	CreatedBefore   *time.Time        `json:"created_before,omitempty"`  // explicit temporal upper bound
}

// SearchFilters holds structured constraints extracted from query understanding or explicit API params.
// Used as scoring boosts and an additional search lane — never as hard exclusion filters
// (except when Explicit=true, meaning the user set them intentionally via API params).
// Hub is an exception — it narrows search scope (hard filter) because the user explicitly named a hub.
type SearchFilters struct {
	TemporalStart *time.Time `json:"temporal_start,omitempty"`
	TemporalEnd   *time.Time `json:"temporal_end,omitempty"`
	People        []string   `json:"people,omitempty"`
	Authors       []string   `json:"authors,omitempty"`
	Kind          string     `json:"kind,omitempty"`
	Source        string     `json:"source,omitempty"`
	Hub           string     `json:"hub,omitempty"`      // hub name/slug — resolved to hubID for search scoping
	TopicID       string     `json:"topic_id,omitempty"` // restrict results to memories in this topic
	Explicit      bool       `json:"explicit,omitempty"` // true = from API params; false = LLM-extracted
}

type RecalledMemory struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Summary        string  `json:"summary,omitempty"`
	ChunkContent   string  `json:"chunk_content"`
	HeadingChain   string  `json:"heading_chain"`
	RelevanceScore float64 `json:"relevance_score"`
	Kind           string  `json:"kind"`
	Stability      string  `json:"stability"`
	Source         string  `json:"source"`
	Age            string  `json:"age"`
	CreatedAt      string  `json:"created_at,omitempty"`
	AuthorName     string  `json:"author_name,omitempty"`
	HubID          string  `json:"hub_id,omitempty"`
	HubName        string  `json:"hub_name,omitempty"`
	ProjectRepo    string  `json:"project_repo,omitempty"`
	Hint           string  `json:"hint,omitempty"`
	TopicID        string  `json:"topic_id,omitempty"`
	TopicName      string  `json:"topic_name,omitempty"`
}

type RecallResult struct {
	Memories      []RecalledMemory `json:"memories"`
	QueryMetadata QueryMetadata    `json:"query_metadata"`
}

type QueryMetadata struct {
	Intent          string         `json:"intent"`
	KindsSearched   []string       `json:"kinds_searched"`
	TotalCandidates int            `json:"total_candidates"`
	Reranked        bool           `json:"reranked,omitempty"`
	RerankReason    string         `json:"rerank_reason,omitempty"`
	LatencyMs       int64          `json:"latency_ms"`
	Filters         *SearchFilters `json:"filters,omitempty"`
}

type AskRequest struct {
	Query      string `json:"query"`
	Limit      int    `json:"limit,omitempty"`
	Source     string `json:"source,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	Model      string `json:"model,omitempty"`     // "auto", "haiku", "sonnet"
	Locale     string `json:"locale,omitempty"`    // "en", "zh" — respond in this language
	NoRerank   bool   `json:"no_rerank,omitempty"` // skip reranking for source retrieval
	Debug      bool   `json:"debug,omitempty"`     // include prompt and raw response in metadata
	TopicID    string `json:"topic_id,omitempty"`
}

type AskResult struct {
	Answer    string           `json:"answer"`
	Citations []Citation       `json:"citations"`
	Sources   []RecalledMemory `json:"sources"`
	Metadata  AskMetadata      `json:"metadata"`
}

type Citation struct {
	Index    int    `json:"index"`
	MemoryID string `json:"memory_id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
}

type AskMetadata struct {
	Model                  string `json:"model"`
	AnswerTokens           int    `json:"answer_tokens"`
	RetrievalLatencyMs     int64  `json:"retrieval_latency_ms"`
	SynthesisLatencyMs     int64  `json:"synthesis_latency_ms"`
	TotalLatencyMs         int64  `json:"total_latency_ms"`
	SourceContextTokens    int    `json:"source_context_tokens,omitempty"`
	SourceContextBudget    int    `json:"source_context_budget,omitempty"`
	PerSourceContextBudget int    `json:"per_source_context_budget,omitempty"`
	TrimmedSources         int    `json:"trimmed_sources,omitempty"`
	// Debug fields — only populated when debug=true in request
	DebugPrompt      string `json:"debug_prompt,omitempty"`
	DebugRawResponse string `json:"debug_raw_response,omitempty"`
}

// Dream models — Memory Dreams consolidation engine

// DreamRunStatus values. DB column has no CHECK constraint so new
// values can be introduced incrementally, but consumers (web hook,
// CLI renderer, SDK types) must stay in sync — see
// docs/engineering/dreams-engine-remediation-plan.md.
const (
	// DreamRunStatusRunning — cycle is in flight. Exactly one row per
	// hub can hold this status (migration 012 partial unique index).
	DreamRunStatusRunning = "running"

	// DreamRunStatusCompleted — cycle finished and every phase ran
	// cleanly (no LLM errors, no timeouts). The happy path.
	DreamRunStatusCompleted = "completed"

	// DreamRunStatusPartialFailed — cycle finished but at least one
	// phase hit LLM errors or timeouts. Distinct from Completed so
	// operators can tell "organize timed out on every batch but merge
	// still ran" apart from "everything worked". Memory mutations
	// that did commit stay committed — retry-driving Failed is not
	// appropriate because phase work is not idempotent.
	DreamRunStatusPartialFailed = "partial_failed"

	// DreamRunStatusFailed — catastrophic pre-scan failure. The
	// cycle could not even enumerate candidate memories. No phase
	// mutations were committed.
	DreamRunStatusFailed = "failed"

	// DreamRunStatusSkipped — the cycle was declined before phases
	// ran. Reasons: plan lacks dreams, user disabled, hub frozen, or
	// another cycle is already running for the hub. The receipt
	// report carries the human-readable reason.
	DreamRunStatusSkipped = "skipped"
)

type DreamRun struct {
	ID                  string                       `json:"id"`
	OwnerID             string                       `json:"owner_id"`
	HubID               string                       `json:"hub_id"`
	Mode                string                       `json:"mode,omitempty"` // DreamRunMode: maintenance, bootstrap
	Status              string                       `json:"status"`         // see DreamRunStatus* constants
	StartedAt           time.Time                    `json:"started_at"`
	FinishedAt          time.Time                    `json:"finished_at,omitempty"`
	MemoriesScanned     int                          `json:"memories_scanned"`
	DuplicatesMerged    int                          `json:"duplicates_merged"`
	ContradictionsFound int                          `json:"contradictions_found"`
	MemoriesArchived    int                          `json:"memories_archived"`
	MemoriesOrganized   int                          `json:"memories_organized"`
	TopicsRestructured  int                          `json:"topics_restructured"`
	PhaseMetrics        map[string]DreamPhaseMetrics `json:"phase_metrics,omitempty"`
	PhaseBudgets        map[string]DreamPhaseBudget  `json:"phase_budgets,omitempty"`
	Report              string                       `json:"report"`
	// LastHeartbeatAt is bumped by the engine after each phase
	// boundary. ClaimStaleDreamRun compares its age against the
	// stale threshold — a slow but alive cycle keeps its row safe
	// from reclaim. Nil on historical rows; reclaim falls back to
	// StartedAt in that case.
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
}

type DreamPhaseMetrics struct {
	Candidates       int `json:"candidates,omitempty"`
	Attempted        int `json:"attempted,omitempty"`
	Processed        int `json:"processed,omitempty"`
	Actions          int `json:"actions,omitempty"`
	Skipped          int `json:"skipped,omitempty"`
	Batches          int `json:"batches,omitempty"`
	CompletedBatches int `json:"completed_batches,omitempty"`
	TimedOutBatches  int `json:"timed_out_batches,omitempty"`
	LLMCalls         int `json:"llm_calls,omitempty"`
	LLMErrors        int `json:"llm_errors,omitempty"`
	LLMTimeouts      int `json:"llm_timeouts,omitempty"`
	// Errors counts non-LLM failures that were swallowed during the
	// phase: store-load failures, topic-create failures, memory-
	// assign failures, notification-publish failures, etc. Distinct
	// from LLMErrors so operators can tell a flaky LLM apart from a
	// flaky database. Bumping this field demotes the whole run to
	// status='partial_failed' at cycle end (see computeFinalStatus).
	Errors int `json:"errors,omitempty"`
	// TokensIn / TokensOut are Anthropic token counts summed across
	// every LLM call the phase made. Emitted in the usage_events
	// row so billing/admin dashboards can see real dream cost, not
	// just call counts.
	TokensIn   int64 `json:"tokens_in,omitempty"`
	TokensOut  int64 `json:"tokens_out,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`
}

type DreamPhaseBudget struct {
	CandidateLimit    int   `json:"candidate_limit,omitempty"`
	BatchSize         int   `json:"batch_size,omitempty"`
	PreviewCharBudget int   `json:"preview_char_budget,omitempty"`
	TopicContextLimit int   `json:"topic_context_limit,omitempty"`
	MaxLLMCalls       int   `json:"max_llm_calls,omitempty"`
	TimeoutMs         int64 `json:"timeout_ms,omitempty"`
}

type DreamAction struct {
	ID         string `json:"id"`
	RunID      string `json:"run_id"`
	ActionType string `json:"action_type"` // merge, contradiction, archive, organize, restructure
	// DreamActionType
	SourceMemoryIDs []string `json:"source_memory_ids"`
	ResultMemoryID  string   `json:"result_memory_id,omitempty"`
	// FromTopicID / ToTopicID carry explicit topic lineage for
	// organize/restructure actions (nullable; empty string for other
	// action types and for rows written before migration 069). The
	// lifecycle resolver uses these to render "moved from X → Y" in
	// the memory detail provenance strip. Historical nulls degrade
	// gracefully — verb + reason only.
	FromTopicID string    `json:"from_topic_id,omitempty"`
	ToTopicID   string    `json:"to_topic_id,omitempty"`
	Reason      string    `json:"reason"`
	Similarity  float64   `json:"similarity,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// Phase 2b agentic-dream audit (plan 24, migration 006).
	// AgentPath is "single_call" (default) or "lucid_active". Only
	// the latter populates AgentSessionID / AgentModelCalls /
	// AgentToolCalls. The CHECK constraint on the column mirrors
	// the constants below; tests pin the lockstep. Single-call
	// rows zero-default the counts so calibration queries can
	// AVG(agent_model_calls) WHERE agent_path = 'lucid_active'
	// without filtering NULLs.
	AgentPath       string `json:"agent_path,omitempty"`
	AgentSessionID  string `json:"agent_session_id,omitempty"`
	AgentModelCalls int    `json:"agent_model_calls,omitempty"`
	AgentToolCalls  int    `json:"agent_tool_calls,omitempty"`
}

// DreamActionAgentPath values mirror the dream_actions.agent_path
// CHECK constraint in migration 006 (Part B). Adding a kind
// requires a matching migration to extend the constraint.
const (
	DreamActionAgentPathSingleCall  = "single_call"
	DreamActionAgentPathLucidActive = "lucid_active"
)

// DreamLatestRunSummary is the semantic outcome slice for the latest dream run
// in a hub. It mirrors DreamRun counts without exposing execution fields.
type DreamLatestRunSummary struct {
	Merged              int `json:"merged"`
	ContradictionsFound int `json:"contradictions_found"`
	Archived            int `json:"archived"`
	Organized           int `json:"organized"`
	Restructured        int `json:"restructured"`
}

// DreamPendingReviewSummary is the live hub-scoped follow-up queue derived
// from notifications. This is intentionally separate from latest-run totals.
type DreamPendingReviewSummary struct {
	Contradictions    int `json:"contradictions"`
	TopicMerges       int `json:"topic_merges"`
	TopicRestructures int `json:"topic_restructures"`
	Total             int `json:"total"`
}

// DreamIntelligenceSummary separates latest-run outcome from current pending
// review state for one hub.
type DreamIntelligenceSummary struct {
	LatestRun     *DreamLatestRunSummary    `json:"latest_run,omitempty"`
	PendingReview DreamPendingReviewSummary `json:"pending_review"`
}

// DreamRunListResponse is the typed response for the public
// GET /v1/dreams list endpoint. Keyset pagination: NextCursor is
// the composite "<rfc3339nano>|<uuid>" marker from the last row of
// this page, empty when no more rows remain. Callers pass it back
// as ?cursor= to fetch the next page.
//
// Intentionally distinct from handler.AdminDreamRunListResponse so
// the admin surface can evolve without rippling into the public
// SDK (see repo admin-boundary rule in AGENTS.md).
type DreamRunListResponse struct {
	Runs       []DreamRun `json:"runs"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// DreamReport is the typed response for GET /v1/dreams/report.
type DreamReport struct {
	HasRun       bool                     `json:"has_run"`
	Message      string                   `json:"message,omitempty"`
	Run          *DreamRun                `json:"run,omitempty"`
	Actions      []DreamAction            `json:"actions,omitempty"`
	Intelligence DreamIntelligenceSummary `json:"intelligence"`
}

// SimilarMemoryPair represents two memories with high semantic similarity.
type SimilarMemoryPair struct {
	MemoryA    Memory  `json:"memory_a"`
	MemoryB    Memory  `json:"memory_b"`
	Similarity float64 `json:"similarity"`
}

// RelatedMemory is a memory with a similarity score, returned by nearest-neighbor search.
type RelatedMemory struct {
	Memory     Memory  `json:"memory"`
	Similarity float64 `json:"similarity"`
}

// EnrichmentCandidate is a lightweight result from the enrichment search,
// carrying only the fields needed to build context for the summarizer.
type EnrichmentCandidate struct {
	MemoryID   string  `json:"memory_id"`
	Title      string  `json:"title"`
	Summary    string  `json:"summary"`
	Similarity float64 `json:"similarity"`
}

// ReviewTopicRef is the shared topic-pointer shape used by enriched
// notification payloads for topic_merge / topic_restructure kinds.
// The "Review" prefix in the name is historical — these types were
// introduced during the Phase 1 empty-body fix when they also fed
// the reviews table, but Phase 6 retires the reviews surface and
// these now exist exclusively as notification payload shapes.
type ReviewTopicRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MemoryCount int    `json:"memory_count,omitempty"`
}

// ReviewTopicMergePayload is the payload shape for review_type =
// "topic_merge". The merge resolve action collapses every source topic
// into the target topic via store.MergeTopics; SourceTopics never
// includes TargetTopic (the dream restructure writer filters it out).
type ReviewTopicMergePayload struct {
	TargetTopic  ReviewTopicRef   `json:"target_topic"`
	SourceTopics []ReviewTopicRef `json:"source_topics"`
	Reason       string           `json:"reason"`
}

// ReviewTopicRestructurePayload is the payload shape for review_type =
// "topic_restructure". The apply resolve action reparents ChildTopic
// under ParentTopic via store.ApplyTopicRestructure.
type ReviewTopicRestructurePayload struct {
	ParentTopic ReviewTopicRef `json:"parent_topic"`
	ChildTopic  ReviewTopicRef `json:"child_topic"`
	Reason      string         `json:"reason"`
}

// UserPreferences holds user settings as a flexible JSONB map.
// Defaults are defined in code (see DefaultSettings); stored values override defaults.
type UserPreferences struct {
	UserID    string         `json:"user_id"`
	Settings  map[string]any `json:"settings"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// DefaultSettings returns the default user preferences.
// New settings can be added here without a migration.
func DefaultSettings() map[string]any {
	return map[string]any{
		"dreams_enabled":              true,
		"dreams_merge_enabled":        true,
		"dreams_archive_enabled":      true,
		"dreams_excluded_kinds":       []string{},
		"dreams_similarity_threshold": 0.85,
		"dreams_staleness_days":       60,
		"dreams_organize_enabled":     true,
		"dreams_restructure_enabled":  true,
		// Phase 2b agentic-dream gate (plan 24). Default OFF — when
		// false, every dream cycle uses the existing single-call
		// LLM path regardless of trigger fires (the soft-mode log
		// in dream_trigger_decisions still records what WOULD have
		// fired, for calibration). When true, fired triggers route
		// the contradict + organize phases through agent.Run with
		// profile=ProfileDreamActive; non-fired memories still use
		// the single-call path. Per-hub opt-in for staged rollout
		// per plan 24's calibration gate.
		"dreams_use_agent_runtime": false,
		// Lane B board synthesis (plan 25 P2). Deliberately a SEPARATE
		// gate from dreams_use_agent_runtime, and default ON: the two
		// are different features that only share a mechanism. The
		// runtime flag governs plan-24's experimental rerouting of the
		// contradict/organize phases (a correctness-sensitive change to
		// existing behavior, still staged); board synthesis only ADDS
		// cards to a surface that is otherwise a wall of counters, and
		// is the whole point of the board. It still requires a
		// configured AgentRuntime, so a deployment without an API key
		// degrades to Lane A instead of failing.
		"dreams_board_synthesis_enabled": true,
		"hub_header_aurora_mode":   "signature",
		"dev_flags": map[string]any{
			"mockDreams":      false,
			"mockDreaming":    false,
			"mockEmptyInbox":  false,
			"mockProUser":     false,
			"debuggerEnabled": false,
			"skipRerank":      false,
		},
		"notifications_enabled": true,
		"theme":                 "auto",
		// Default persona for the memax agent (Agent Chat). "" = none;
		// otherwise a personas.id. Sessions may override via persona_id.
		"chat_default_persona_id": "",
	}
}

// MergedSettings returns stored settings merged with defaults.
// Stored values take precedence over defaults.
func (p *UserPreferences) MergedSettings() map[string]any {
	merged := DefaultSettings()
	for k, v := range p.Settings {
		merged[k] = v
	}
	return merged
}

type PushRequest struct {
	Content             string            `json:"content"`
	Title               string            `json:"title,omitempty"`
	Hint                string            `json:"hint,omitempty"`
	Kind                string            `json:"kind,omitempty"`
	Stability           string            `json:"stability,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	Source              string            `json:"source,omitempty"`
	SourceAgent         string            `json:"source_agent,omitempty"` // "claude-code", "cursor", "copilot", etc.
	AssistedByAgent     string            `json:"assisted_by_agent,omitempty"`
	InitiationType      string            `json:"initiation_type,omitempty"`
	SourcePath          string            `json:"source_path,omitempty"`
	HubReason           string            `json:"hub_reason,omitempty"`
	ContentType         string            `json:"content_type,omitempty"`
	ProjectContext      map[string]string `json:"project_context,omitempty"` // {"repo": "...", "project": "...", "branch": "..."}
	BatchID             string            `json:"batch_id,omitempty"`        // Groups files from the same multi-file drop
	FileRef             *FileRef          `json:"file_ref,omitempty"`
	AllowRelatedContext bool              `json:"-"` // Internal: set by handler based on read permission, not from API input
}

type FileRef struct {
	ObjectKey   string `json:"object_key"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type ApiResponse struct {
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"` // machine-readable details (quota info, validation errors, etc.)
}
