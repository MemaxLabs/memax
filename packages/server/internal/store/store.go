package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// VisibilityScope defines what data a user can see.
//
// Read queries use this to generate:
//
//	(alias.owner_id = $N OR alias.hub_id = ANY($M::uuid[]))
//
// This is the SINGLE source of truth for access control on reads.
// Write queries still accept ownerID directly (you can only modify your own data).
//
// When RLS is added (Phase 3+), this struct becomes a thin wrapper around
// session-level Postgres settings and the SQL generation moves to policies.
type VisibilityScope struct {
	OwnerID string   // the requesting user
	HubIDs  []string // all hubs the user has access to (from middleware)
}

// SQLFilter returns a WHERE predicate and args for visibility filtering.
// alias is the table alias (e.g., "m" for memories, "t" for topics).
// startParam is the first $N placeholder number to use.
// Returns the predicate string and the args it consumes.
func (v VisibilityScope) SQLFilter(alias string, startParam int) (string, []any) {
	return v.SQLFilterFor(alias, startParam), v.filterArgs()
}

// SQLFilterFor returns the WHERE predicate string alone, without
// emitting args. Use this when you need to repeat the visibility
// filter against multiple aliases (e.g., joining topics-as-from and
// topics-as-to) while sharing one copy of the args. The caller is
// responsible for ensuring the param positions referenced here match
// the positions at which the args end up in the final arg slice.
func (v VisibilityScope) SQLFilterFor(alias string, startParam int) string {
	if len(v.HubIDs) > 0 {
		return fmt.Sprintf("(%s.owner_id = $%d::uuid OR %s.hub_id = ANY($%d::uuid[]))",
			alias, startParam, alias, startParam+1)
	}
	return fmt.Sprintf("%s.owner_id = $%d::uuid", alias, startParam)
}

func (v VisibilityScope) filterArgs() []any {
	if len(v.HubIDs) > 0 {
		return []any{v.OwnerID, v.HubIDs}
	}
	return []any{v.OwnerID}
}

// ParamCount returns how many $N parameters SQLFilter will consume.
func (v VisibilityScope) ParamCount() int {
	if len(v.HubIDs) > 0 {
		return 2
	}
	return 1
}

// ListOptions — composable query options for listing memories.
// Zero values are ignored (no filter applied).
type ListOptions struct {
	Scope        VisibilityScope // who can see what — required for all list queries
	HubID        string          // filter to specific hub
	TopicID      string          // filter to memories assigned to a specific topic
	CreatedAfter *time.Time      // only memories created after this time
	Actor        string          // "agent:slug" | "author:name" | "self"
	Kind         string          // machine-only retrieval kind filter
	Sort         string          // "newest" (default) or "relevant"
	Limit        int             // page size (default 20, max 50)
	Cursor       string          // pagination cursor
}

// StrictHubListOptions is the agent-runtime variant of ListOptions.
// It intentionally has no owner fallback: callers pass an already
// authorized non-empty hub allowlist and the query filters by
// `hub_id = ANY(hub_ids)` exactly. This avoids leaking user-owned
// memories from outside a single-hub or hub-subset agent session.
type StrictHubListOptions struct {
	HubIDs  []string // required, strict allowlist
	HubID   string   // optional narrowing; must be within HubIDs
	TopicID string
	Sort    string // "newest" (default) or "relevant"
	Limit   int
	Cursor  string
	Since   time.Time // optional; when non-zero filter to m.updated_at >= Since
}

// SearchOptions extends chunk search with optional field-aware matching.
// When FieldTerms is non-empty, an additional search lane queries memory
// titles for these terms, ensuring title-matching memories enter the
// candidate pool even when their chunk content is short.
type SearchOptions struct {
	FieldTerms  []string // discriminating terms for title matching (may be empty)
	ProjectRepo string   // if set, field lane excludes memories from other projects
}

// SuspiciousMetadataRow represents a memory with title/summary that looks
// like raw LLM protocol output. Used by the admin cleanup endpoint.
type SuspiciousMetadataRow struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

var (
	ErrHubInviteNotFound   = errors.New("hub invite not found")
	ErrHubInviteExpired    = errors.New("hub invite expired")
	ErrHubInviteUsed       = errors.New("hub invite already used")
	ErrHubInviteRevoked    = errors.New("hub invite revoked")
	ErrHubAlreadyMember    = errors.New("user already a hub member")
	ErrHubTransferNotFound = errors.New("hub ownership transfer not found")
	ErrHubTransferExpired  = errors.New("hub ownership transfer expired")
	ErrHubTransferAccepted = errors.New("hub ownership transfer already accepted")
	ErrHubTransferCanceled = errors.New("hub ownership transfer canceled")
	ErrHubTransferExists   = errors.New("hub ownership transfer already active")
	ErrHubTransferInvalid  = errors.New("hub ownership transfer invalid")
	ErrIdentityConflict    = errors.New("provider identity belongs to another user")
	// ErrEmailOTPCodeNotFound is returned by GetActiveEmailOTPCode
	// when no unconsumed, unexpired row exists for the email.
	// Distinct from a real DB error so the verify handler can map
	// "no active code" to a 400 code_expired response.
	ErrEmailOTPCodeNotFound = errors.New("email otp code not found")
	// ErrDreamRunConcurrent is returned by CreateDreamRun when another
	// run for the same hub is already in the `running` state. The
	// partial unique index idx_dream_runs_one_active_per_hub
	// (migration 012) enforces this at the DB layer; the store wraps
	// the pg unique_violation into this sentinel so the engine can
	// treat it as a skippable condition rather than a retryable
	// failure.
	ErrDreamRunConcurrent = errors.New("dream run already in progress for this hub")
	// ErrDreamRunNotFound is returned by GetDreamRunByID when the
	// row doesn't exist. Distinct sentinel so the admin handler
	// returns 404 for "missing" and 500 for "unexpected DB error"
	// — previously every scan error looked like a 404.
	ErrDreamRunNotFound = errors.New("dream run not found")
)

// DreamRunListForUserOpts controls ListDreamRunsForUser pagination
// and optional hub scoping. UserID is required; HubID narrows the
// result set to a single hub. Limit caps page size; Cursor is the
// RFC3339Nano-formatted started_at of the last row from the
// previous page.
type DreamRunListForUserOpts struct {
	UserID string
	HubID  string // optional; empty = all hubs user participates in
	Limit  int    // <=0 uses 20; capped at 100
	Cursor string // started_at of last row from previous page
}

// DreamTriggerInputRow is one memory's worth of pre-aggregated
// trigger signals, as returned by LoadDreamTriggerInputs. The
// dream engine assembles a triggers.Inputs from this and runs the
// pure evaluator. Empty SourceFetchHash + empty UserFollowupMarker
// is the pre-Phase-2a default — those triggers short-circuit at
// the evaluator layer.
//
// UserFollowupScanned preserves the database's three-state semantics
// across the Go boundary. The DB column is nullable: NULL means
// "scan never ran on this memory" (a pre-Phase-2a memory that
// hasn't been re-ingested), empty means "scan ran, no marker
// found", non-empty is the matched marker text. The marker string
// alone collapses NULL and empty into the same Go zero value, so
// without an explicit "scanned" boolean, calibration queries can't
// tell apart a scan-never-ran memory from a no-marker scan — and
// the unscanned ones dilute the no-fire denominator. The Go bool
// preserves the distinction without leaking sql.Null* into the
// engine.
type DreamTriggerInputRow struct {
	MemoryID            string
	ContentHash         string
	SourceFetchHash     string
	UserFollowupScanned bool
	UserFollowupMarker  string
	ContradictionCount  int
	TopicSiblingCount   int
}

// DreamTriggerDecisionRow is one dream-trigger evaluation result,
// formatted for batch-insert into dream_trigger_decisions. Fired,
// Kind, and SignalsJSON come straight from triggers.Decision; the
// caller is responsible for marshaling Signals before calling
// LogTriggerDecisions so the store stays JSONB-shape-agnostic.
type DreamTriggerDecisionRow struct {
	MemoryID    string
	Fired       bool
	Kind        string // matches dream_trigger_decisions.trigger_kind CHECK
	SignalsJSON []byte
}

// Store is the storage interface. Implementations: InMemoryStore (dev), PostgresStore (prod).
type Store interface {
	// Memories
	CreateMemory(memory *model.Memory) error
	GetMemory(id string, ownerID string) (*model.Memory, error)
	// GetMemoryForAdmin loads a memory by ID for admin-only maintenance routes.
	// Normal user-facing reads must use GetMemory/GetAccessibleMemory with owner or hub scope.
	GetMemoryForAdmin(id string) (*model.Memory, error)
	// FindSuspiciousMetadata returns memory IDs with title/summary that look like
	// raw LLM protocol output (JSON, field markers, etc.). Used by admin cleanup.
	FindSuspiciousMetadata(ctx context.Context, limit int) ([]SuspiciousMetadataRow, error)
	GetAccessibleMemory(id string, userID string, hubIDs []string) (*model.Memory, error)
	GetAccessibleMemories(ids []string, userID string, hubIDs []string) (map[string]*model.Memory, error)
	GetMemoryBySourcePath(sourcePath string, ownerID string, hubID string) (*model.Memory, error)
	GetMemoryByContentHash(hash string, ownerID string, hubID string) (*model.Memory, error)
	FindRelatedMemories(memoryID string, sourceHubID string, limit int) ([]model.RelatedMemory, error)
	FindRelatedForEnrichment(ctx context.Context, queryEmbedding []float64, ownerID string, readHubIDs []string, excludeMemoryID string, limit int) ([]model.EnrichmentCandidate, error)
	ListMemories(ownerID string, limit int) ([]model.Memory, error)
	ListMemoriesPaginated(opts ListOptions) ([]model.Memory, string, int, error) // returns memories + next cursor + total count
	ListMemoriesInHubs(ctx context.Context, opts StrictHubListOptions) ([]model.Memory, string, int, error)
	ListActorCounts(opts ListOptions) (map[string]int, error) // returns recent actor filter counts for current scope
	UpdateMemory(memory *model.Memory) error
	// SetUserFollowupMarker writes the user_followup_marker column
	// for a single memory. Called by the ingest pipeline after the
	// marker scan stage extracts an @TODO/@FIXME/@QUESTION text from
	// the memory body. Empty marker writes the empty string (NOT
	// SQL NULL) so a soft-mode trigger evaluator can distinguish
	// "scan ran and found nothing" (empty) from "scan never ran"
	// (NULL — the pre-Phase-2a state). The trigger fires only for
	// non-empty values; both NULL and empty short-circuit to "no
	// follow-up signal."
	SetUserFollowupMarker(ctx context.Context, memoryID string, ownerID string, marker string) error
	IncrementMemoryShownBatch(ctx context.Context, memoryIDs []string, ownerID string, hubIDs []string) error
	IncrementMemoryAccessed(ctx context.Context, memoryID string, ownerID string, hubIDs []string) error
	IncrementMemoryAccessedBatch(ctx context.Context, memoryIDs []string, ownerID string, hubIDs []string) error
	DeleteMemory(id string, ownerID string) error
	DeleteHubMemory(id string, hubID string) error // hub-authorized delete (caller checked role+policy)
	// BatchDeleteMemories deletes the subset of `ids` owned by `ownerID`
	// and returns the ids that were actually removed. Callers compute
	// `requested - deleted` to populate the skipped-not_found list for
	// the structured BatchDeleteResult contract.
	BatchDeleteMemories(ids []string, ownerID string) ([]string, error)
	// BatchDeleteHubMemories deletes the subset of `ids` scoped to `hubID`.
	// Caller is responsible for verifying the requester's role and the
	// hub's contributor_delete_policy before invoking. Returns the ids
	// actually removed for skipped-list accounting.
	BatchDeleteHubMemories(ids []string, hubID string) ([]string, error)
	BatchMoveMemories(ids []string, targetHubID string, targetTopicID string, ownerID string) (*model.BatchMoveResult, error)
	BatchMoveToTopic(ids []string, topicID string, hubID string, confidence float64) (int, error) // returns count moved, single transaction
	BatchMoveToHub(ids []string, targetHubID string, ownerID string) (int, error)                 // returns count moved, single transaction
	DeleteAllUserData(ownerID string) error                                                       // purge all user data (memories, topics, configs, dreams, reviews)
	ListArchiveCandidates(ownerID string, minAgeDays int, limit int) ([]model.Memory, error)      // pre-filtered candidates for archival
	// ListSeedMemoryTemplates returns active seed-memory templates from
	// the system tutorial hub (memories with source_kind =
	// 'onboarding-seed' + state = 'active'). Used by the
	// CopySeedMemoriesWorker (plan 23) to copy each enabled template
	// into a new user's personal hub on signup.
	ListSeedMemoryTemplates(ctx context.Context) ([]model.Memory, error)
	// ListSeedMemoryTemplatesAdmin returns ALL seed-memory templates
	// (active + archived) so admin surfaces can render the toggle for
	// archived rows. Plan 23 §5.7.
	ListSeedMemoryTemplatesAdmin(ctx context.Context) ([]model.Memory, error)
	// DeleteSeedCopiesForOwner deletes every memory the given owner
	// holds whose `source_kind = 'onboarding-seed'` — i.e. the per-
	// user copies the CopySeedMemoriesWorker produced. Used by the
	// admin "sync seeds to my account" path to reset the caller's
	// seed-copy set before re-running RunSeedCopy. Returns the number
	// of rows removed. Chunks cascade via FK; attachment objects on
	// blob storage are not touched (seeds don't carry attachments).
	DeleteSeedCopiesForOwner(ownerID string) (int64, error)
	// DeleteSeedMemoryTemplate hard-deletes one onboarding-seed
	// template from the system tutorial hub. The implementation MUST
	// scope the DELETE to (id, owner=SystemUserID, hub=TutorialHubID,
	// source_kind=onboarding-seed) as defense in depth — admin paths
	// must never be a vector for deleting arbitrary user memories.
	// Existing per-user copies (different owner_id) stay untouched
	// (plan 23 principle 4 — future-only edits). Returns whether a
	// row was actually deleted so handlers can surface 404 vs 200.
	DeleteSeedMemoryTemplate(id string) (bool, error)
	CreateMemoryAttachment(attachment *model.MemoryAttachment) error
	ListMemoryAttachments(memoryID string, ownerID string) ([]model.MemoryAttachment, error)
	ListMemoryAttachmentsByIDs(memoryIDs []string, ownerID string) ([]model.MemoryAttachment, error)
	GetMemoryAttachment(id string, memoryID string, ownerID string) (*model.MemoryAttachment, error)
	// GetAttachmentByID looks up an attachment row by its primary key
	// without any ownership filter. Used exclusively by the signed
	// view endpoint, where the HMAC signature IS the authorization —
	// the endpoint is intentionally public at the middleware layer.
	// Any caller that isn't downstream of a verified signature MUST
	// use GetMemoryAttachment instead, which enforces ownership.
	GetAttachmentByID(id string) (*model.MemoryAttachment, error)
	ListOwnerMemoryAttachments(ownerID string) ([]model.MemoryAttachment, error)

	// Chunks
	CreateChunks(chunks []model.Chunk) error
	UpdateChunk(chunk *model.Chunk) error
	DeleteChunksByMemory(memoryID string, ownerID string) error
	SearchChunks(ctx context.Context, query string, embedding []float64, ownerID string, limit int, filters *model.SearchFilters, opts *SearchOptions) ([]model.Chunk, error)
	GetChunksByMemory(memoryID string, ownerID string) ([]model.Chunk, error)
	GetAccessibleChunksByMemory(memoryID string, userID string, hubIDs []string) ([]model.Chunk, error)

	// All chunks (for recall scoring)
	AllChunks() []model.Chunk

	// Hub-scoped chunk search. Filter is `owner_id = $user OR
	// hub_id = ANY($hubs)` — cross-hub by design. Used by /v1/recall,
	// /v1/ask, the existing MCP tools. Users recalling their own
	// context shouldn't have to re-scope by hub to see content
	// they own in their personal hub.
	SearchChunksForHubs(ctx context.Context, query string, embedding []float64, ownerID string, hubIDs []string, limit int, filters *model.SearchFilters, opts *SearchOptions) ([]model.Chunk, error)

	// GetMemoryInHubs is the strict-hub variant of
	// GetAccessibleMemory. The memory matches ONLY when its hub_id
	// is in hubIDs — no owner-OR fallback. Used by every memory-
	// read surface in the request path when the principal is
	// scope-bounded (HubScopeAllowlist), so a scoped grant cannot
	// fetch a memory outside its granted hubs even if it knows
	// the ID and the user owns the memory.
	//
	// hubIDs MUST be non-empty; an empty list returns
	// ErrEmptyHubIDsForStrictSearch instead of falling through to
	// owner-only (which would defeat the strict semantics). The
	// scope-aware handler helper returns ErrMemoryNotFound for
	// the empty-hubs case BEFORE calling this method.
	GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error)

	// Strict-hub chunk search. Filter is `hub_id = ANY($hubs)`
	// EXACTLY — no owner-OR fallback. Used by the agent runtime
	// when the chat agent narrows recall to a specific hub
	// (`recall_memories({hub_id: "h-work"})`) or when a dream
	// agent runs against its single team-hub: in those paths a
	// user-owned memory in another hub MUST NOT leak into the
	// result. The caller is responsible for validating that
	// every hubID is one the requesting user has access to —
	// the store doesn't re-check authorization here, mirroring
	// the trust model SearchChunksForHubs already uses.
	//
	// hubIDs MUST be non-empty; pass-through to SearchChunks's
	// owner-only flavor would be the wrong fail-open behavior.
	// Empty hubIDs returns ErrEmptyHubIDsForStrictSearch.
	SearchChunksInHubs(ctx context.Context, query string, embedding []float64, hubIDs []string, limit int, filters *model.SearchFilters, opts *SearchOptions) ([]model.Chunk, error)

	// Users
	ListDistinctOwners() ([]string, error)
	GetUser(id string) (*model.User, error)
	GetUsersByIDs(ids []string) (map[string]*model.User, error) // batch lookup: id → User
	GetUserByCanonicalEmail(email string) (*model.User, error)  // lower(btrim(email)) match
	UpdateUserProfile(id string, displayName string) error
	GetUserPreferences(userID string) (*model.UserPreferences, error)
	UpsertUserPreferences(userID string, settings map[string]any) error

	// Auth Identities (multi-provider login)
	GetUserByAuthIdentity(provider, providerID string) (*model.User, error)
	CreateAuthIdentity(userID, provider, providerID, email, name, avatar string) error
	ListAuthIdentities(userID string) ([]model.AuthIdentity, error)
	DeleteAuthIdentity(userID, provider string) error

	// OAuth States (CSRF-safe, single-use)
	CreateOAuthState(ctx context.Context, state model.OAuthState) error
	ConsumeOAuthState(ctx context.Context, stateHash string) (*model.OAuthState, error)
	CleanupExpiredOAuthStates(ctx context.Context) (int64, error)

	// Email OTP codes (third sign-in path alongside GitHub + Google).
	// CreateEmailOTPCode inserts a freshly hashed code keyed by canonical
	// email; the verify path looks up the most recent active row via
	// GetActiveEmailOTPCode, increments attempts on a miss, and marks
	// consumed on a hit. Rate-limit counters use the per-email and
	// per-IP windows. CleanupExpiredEmailOTPCodes is a reaper.
	CreateEmailOTPCode(ctx context.Context, code model.EmailOTPCode) error
	GetActiveEmailOTPCode(ctx context.Context, email string) (*model.EmailOTPCode, error)
	IncrementEmailOTPCodeAttempts(ctx context.Context, id string) (int, error)
	ConsumeEmailOTPCode(ctx context.Context, id string) error
	CountRecentEmailOTPCodesForEmail(ctx context.Context, email string, since time.Time) (int, error)
	CountRecentEmailOTPCodesForIP(ctx context.Context, ip string, since time.Time) (int, error)
	CleanupExpiredEmailOTPCodes(ctx context.Context) (int64, error)

	// Hubs
	CreateHub(hub *model.Hub) error
	// CreateTeamHub atomically creates a team hub with ownership cap enforcement.
	// Acquires per-owner advisory lock, checks cap, creates hub + member + subscription.
	// Returns ErrOwnershipCapExceeded if at cap.
	CreateTeamHub(ctx context.Context, hub *model.Hub, ownerID string, maxFreeTeamHubs int) error
	// AddHubMemberWithCapCheck atomically adds a member with per-hub advisory lock.
	// Returns ErrMemberCapExceeded if at cap. maxMembers <= 0 means unlimited.
	AddHubMemberWithCapCheck(ctx context.Context, hubID, userID, role string, maxMembers int) error
	// GetHubMemberCap returns the max_hub_members for the hub's active subscription plan.
	// Returns -1 if no cap (unlimited).
	GetHubMemberCap(ctx context.Context, hubID string) (int, error)
	GetHub(id string) (*model.Hub, error)
	GetHubBySlug(slug string) (*model.Hub, error)
	UpdateHub(hub *model.Hub) error
	// PatchHubSettings atomically merges a partial patch into
	// hub.settings. Null values in the patch delete the
	// corresponding key (reverting to owner prefs / defaults on
	// the next dream cycle); boolean values set the override.
	//
	// Unlike a read-merge-write round-trip via UpdateHub, this
	// composes at the JSONB layer so two rapid partials from the
	// overrides panel can't drop each other. Allow-list validation
	// lives in the handler — this method trusts its input.
	PatchHubSettings(ctx context.Context, hubID string, patch map[string]any) error
	ListUserHubs(userID string) ([]model.HubWithRole, error)
	// HubMembershipsForUser returns (hub_id, role) pairs for every
	// hub the user belongs to. The agent runtime calls this on
	// every chat message-send for scope resolution; the focused
	// query avoids the JOIN-to-hubs and per-hub COUNT subquery
	// that ListUserHubs does. Returns an empty slice for a user
	// with no team-hub memberships.
	//
	// The role is what the agent uses to compute the SessionScope's
	// WritableHubIDs subset (hubs where role != "viewer") — the
	// fast-fail gate the chat tool wrappers use before mutating
	// store calls. Per-tool refinements (contributor-policy
	// flags, etc.) still call GetHubMemberRole at service time
	// for freshness.
	HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error)
	GetPersonalHub(userID string) (*model.Hub, error)
	ListDreamableHubs() ([]model.Hub, error)

	// Hub Members
	AddHubMember(hubID, userID, role string) error
	RemoveHubMember(hubID, userID string) error
	UpdateHubMemberRole(hubID, userID, role string) error
	ListHubMembers(hubID string) ([]model.HubMember, error)
	// ListHubMembersPaginated is the admin-UI variant of
	// ListHubMembers — same underlying rows, but returns a keyset-
	// paginated page plus nextCursor + total. Bulk callers
	// (notifications fan-out, audience resolution, plan resolver)
	// continue to use ListHubMembers for the full roster.
	ListHubMembersPaginated(ctx context.Context, hubID string, opts model.AdminHubMemberListOpts) ([]model.HubMember, string, int, error)
	GetHubMemberRole(hubID, userID string) (string, error)
	CountHubMembers(hubID string) (int, error)
	DeleteHub(hubID string) error
	GetHubVisit(userID, hubID string) (*model.HubVisit, error)
	UpsertHubVisit(userID, hubID string, visitedAt time.Time) error
	CountRecentMemoriesByHub(scope VisibilityScope, hubID string, createdAfter time.Time) (int, error)

	// Topic Visits — mirror of hub visits, anchors clear-on-visit
	// semantics for scan-surface dream-delta signals.
	GetTopicVisit(userID, topicID string) (*model.TopicVisit, error)
	UpsertTopicVisit(userID, topicID, hubID string, visitedAt time.Time) error

	// Lifecycle resolvers — read model for dream-delta signals.
	//
	// ResolveMemoryLifecycleForList batches the pending_dream_action
	// resolution for a page of memories. Returns a map keyed by memory
	// ID; missing entries mean no pending action. History is NOT
	// populated on list reads (empty slice) — callers that need full
	// history hit the detail resolver.
	//
	// ResolveMemoryLifecycleForDetail returns the full lifecycle for a
	// single memory: pending_dream_action scoped to topic_visits +
	// dream_history slice (up to 10 most recent, unscoped, durable).
	//
	// ResolveTopicLifecycle batches the delta_since_visit aggregate for
	// a set of topic IDs. Missing entries mean no delta since visit.
	//
	// All three enforce VisibilityScope at the query level — callers pass
	// the scope once; the store filters memories + topics via the same
	// predicate used elsewhere.
	ResolveMemoryLifecycleForList(ctx context.Context, scope VisibilityScope, viewerID string, memoryIDs []string) (map[string]*model.MemoryLifecycle, error)
	ResolveMemoryLifecycleForDetail(ctx context.Context, scope VisibilityScope, viewerID, memoryID string) (*model.MemoryLifecycle, error)
	ResolveTopicLifecycle(ctx context.Context, scope VisibilityScope, viewerID string, topicIDs []string) (map[string]*model.TopicLifecycle, error)

	// Hub Invites
	CreateHubInvite(invite *model.HubInvite) error
	GetHubInvite(id string) (*model.HubInvite, error)
	GetHubInviteByToken(token string) (*model.HubInvite, error)
	ListOutstandingHubInvites(hubID string) ([]model.HubInvite, error)
	// AcceptHubInvite accepts a token-based invite with atomic member cap enforcement.
	// maxMembers <= 0 means unlimited. Returns ErrMemberCapExceeded if at cap.
	AcceptHubInvite(token string, userID string, maxMembers int) (*model.HubInvite, error)
	// AcceptHubInviteByID is the by-id accept path used by the notification
	// resolver. Requires invitee_user_id IS NOT NULL and invitee_user_id == userID.
	// maxMembers <= 0 means unlimited. Returns ErrMemberCapExceeded if at cap.
	AcceptHubInviteByID(ctx context.Context, inviteID, userID string, maxMembers int) (*model.HubInvite, error)
	// DeclineHubInviteByID lets the addressed invitee decline the
	// invite from the inbox. Same access checks as AcceptHubInviteByID.
	DeclineHubInviteByID(ctx context.Context, inviteID, userID string) error
	RevokeHubInvite(hubID, inviteID, userID string) error
	// CountActiveHubInvites returns the number of outstanding invites
	// for quota enforcement (not expired, not accepted, not revoked).
	CountActiveHubInvites(hubID string) (int, error)
	// UpdateHubInviteEmailEnqueuedAt sets the email_enqueued_at timestamp.
	// Best-effort: callers log failures but do not propagate.
	UpdateHubInviteEmailEnqueuedAt(inviteID string, t time.Time) error
	// ListExpiringHubInvites returns email invites expiring within the
	// given duration. Used by the reminder worker.
	ListExpiringHubInvites(within time.Duration) ([]model.HubInvite, error)

	// Hub Ownership Transfers
	CreateHubOwnershipTransfer(transfer *model.HubOwnershipTransfer) error
	GetHubOwnershipTransfer(id string) (*model.HubOwnershipTransfer, error)
	GetActiveHubOwnershipTransfer(hubID string) (*model.HubOwnershipTransfer, error)
	// AcceptHubOwnershipTransfer completes a transfer with atomic ownership cap
	// enforcement. maxFreeTeamHubs is the target user's cap; pass -1 to skip.
	AcceptHubOwnershipTransfer(hubID, transferID, userID string, maxFreeTeamHubs int) (*model.HubOwnershipTransfer, error)
	CancelHubOwnershipTransfer(hubID, transferID string) error

	// Usage
	IncrementUsage(userID string, field string) error
	DecrementUsage(userID string, field string) error
	// IncrementUsageReturning atomically increments and returns the new count.
	// Single SQL statement — no TOCTOU race for quota enforcement.
	IncrementUsageReturning(userID string, field string) (int, error)
	GetCurrentUsage(userID string) (*model.Usage, error)

	// Dream quota usage — see docs/plans/23-dream-tiers.md.
	//
	// GetUserDreamUsage returns the current-period basic_dream_count
	// and lucid_dream_count for the user. Returns zeros (not an
	// error) when no usage row exists yet for the period — rows are
	// created lazily by ConsumeDreamRun on first increment.
	GetUserDreamUsage(ctx context.Context, userID string, periodStart time.Time) (basic, lucid int, err error)

	// GetHubDreamUsage returns the current-period lucid_dream_count
	// for the hub from hub_usage. Same lazy-creation contract as
	// GetUserDreamUsage.
	GetHubDreamUsage(ctx context.Context, hubID string, periodStart time.Time) (lucid int, err error)

	// ConsumeDreamRun is the atomic-consume primitive: writes the
	// idempotency ledger row, optionally increments the counter
	// (guarded in hard mode, unguarded in soft), records the
	// telemetry signal in soft mode, all in a single transaction.
	// See the SQL spec in docs/plans/23-dream-tiers.md.
	ConsumeDreamRun(ctx context.Context, params model.DreamConsumeParams) (model.DreamConsumeResult, error)

	// Reviews — retired in Phase 6. The inbox framework now reads
	// from the notifications surface exclusively. The reviews table
	// itself still exists in migration history as baseline schema,
	// but no producer writes to it and no handler reads from it.

	// Agent Configs — cross-device config sync
	UpsertAgentConfig(config *model.AgentConfig) error
	GetAgentConfig(id string, ownerID string) (*model.AgentConfig, error)
	GetAgentConfigByPath(agent, filePath, scope, ownerID string) (*model.AgentConfig, error)
	ListAgentConfigs(ownerID string) ([]model.AgentConfig, error)
	DeleteAgentConfig(id string, ownerID string) error
	ListAgentConfigSyncStates(ownerID string, deviceID string) ([]model.AgentConfigSyncState, error)
	UpsertAgentConfigSyncState(state *model.AgentConfigSyncState) error
	ListAgentConfigTombstones(ownerID string) ([]model.AgentConfigTombstone, error)
	GetAgentConfigTombstone(agent, filePath, scope, ownerID string) (*model.AgentConfigTombstone, error)
	CreateAgentConfigTombstone(tombstone *model.AgentConfigTombstone) error
	DeleteAgentConfigTombstone(agent, filePath, scope, ownerID string) error
	PurgeExpiredAgentConfigTombstoneContent(now time.Time) error
	CountExtractedMemories(ownerID string) (map[string]int, error) // configID -> count

	// Personas — identity objects derived from identity-class configs
	UpsertPersona(p *model.Persona) error
	ListPersonas(ownerID string) ([]model.Persona, error)
	GetPersona(id string, ownerID string) (*model.Persona, error)
	DeletePersona(id string, ownerID string) error
	DeletePersonaBySource(agent, filePath, scope, ownerID string) error
	ListPersonaRevisions(personaID, ownerID string) ([]model.PersonaRevision, error)
	GetPersonaRevision(personaID string, version int, ownerID string) (*model.PersonaRevision, error)

	// Boards — pulse boards (plan 25): per-hub slot surfaces.
	// Boards are hub-scoped, not owner-scoped: access control is hub
	// membership, checked by the handler before any board call (same
	// contract as the /v1/hubs/{id} routes). ResolveBoardSlot only
	// transitions slots still in fresh/seen; terminal slots return
	// ErrBoardSlotAlreadyResolved.
	GetOrCreateSystemBoard(hubID, createdBy string) (*model.Board, error)
	ListBoardSlots(boardID string) ([]model.BoardSlot, error)
	UpsertBoardSlot(slot *model.BoardSlot) error
	ResolveBoardSlot(boardID, slotKey, newState string, resolution model.BoardSlotResolution) (*model.BoardSlot, error)
	CreateBoardFeedback(f *model.BoardFeedback) error

	// Connected Agents — first-class agent registry
	UpsertConnectedAgent(agent *model.ConnectedAgent) error
	GetConnectedAgent(ownerID string, agentName string) (*model.ConnectedAgent, error)
	ListConnectedAgentsWithStats(ownerID string) ([]model.ConnectedAgentWithStats, error)
	UpdateConnectedAgent(agent *model.ConnectedAgent) error
	DeleteConnectedAgent(ownerID string, agentName string) error
	CountAgentApiKeys(ownerID string, agentName string) (int, error)
	CountAgentConfigs(ownerID string, agentName string) (int, error)

	// HealAPIKeyAgent is the auto-heal primitive for API keys whose
	// agent_name is still blank at request time. On the first accepted
	// source_agent claim, an atomic compare-and-set backfills the
	// column. Returns the effective agent_name on the row after the
	// call:
	//   - claimedSlug if we won the race (UPDATE affected the row).
	//   - the pre-existing agent_name if someone else won first.
	//   - "" if the key is now standalone, revoked, or missing.
	// Callers compare the returned slug to their own claim to decide
	// whether to proceed or reject with attribution_conflict.
	//
	// Owner-scoped per repo convention: keyID alone is not enough, the
	// caller must pass the authenticated user_id from the request's
	// AuthContext so a compromised key id from one user cannot trigger
	// writes on another user's rows.
	HealAPIKeyAgent(ctx context.Context, keyID, userID, claimedSlug string) (string, error)

	// Topics — Knowledge organization (human-facing tree)
	CreateTopic(topic *model.Topic) error
	GetTopic(id string, hubID string) (*model.Topic, error)
	// GetTopicAccessible resolves a topic by id for endpoints that don't
	// know (or shouldn't force) the hub. Enforces VisibilityScope so the
	// lookup cannot return a topic outside the viewer's reach — mirrors
	// GetAccessibleMemory. Used by RecordVisit to turn a bare topic id
	// into a hub-qualified topic row without requiring the caller to
	// pre-resolve the hub.
	GetTopicAccessible(id string, scope VisibilityScope) (*model.Topic, error)
	// ListTopics returns ACTIVE topics only — archived topics are invisible
	// to every tree consumer (handlers, dreams, classification, agent tools,
	// MCP) by construction. Use ListArchivedTopics for the archived surface.
	ListTopics(hubID string) ([]model.Topic, error)
	// ListArchivedTopics returns the hub's archived topics as a flat list,
	// most recently archived first.
	ListArchivedTopics(hubID string) ([]model.Topic, error)
	// ArchiveTopicSubtree archives the topic and all descendants atomically;
	// already-archived rows keep their original archived_at. Memory
	// assignments are untouched so restore is lossless. Returns the number
	// of rows newly archived.
	ArchiveTopicSubtree(id, hubID string, archivedAt time.Time) (int, error)
	// RestoreTopicSubtree clears archived_at on the topic and all
	// descendants, re-planting the topic at root when its parent is still
	// archived. Returns the number of rows restored.
	RestoreTopicSubtree(id, hubID string, restoredAt time.Time) (int, error)
	// SearchAccessibleTopics returns topics whose hub is in `hubIDs` and whose
	// name or description matches every whitespace-separated token in `query`
	// (case-insensitive AND). Used by the bar's quick-match endpoint to
	// surface jump-to-topic rows alongside memory matches.
	SearchAccessibleTopics(ctx context.Context, query string, hubIDs []string, limit int) ([]model.Topic, error)
	UpdateTopic(topic *model.Topic) error
	DeleteTopic(id string, hubID string) error
	// AssignMemoryToTopic is the AUTO-ASSIGNMENT primitive: confidence-gated
	// (`confidence > existing`, strict greater) so earlier user intent sticks
	// when ingest or dreams workers replay proposals at the same confidence.
	// User-initiated moves must go through BatchMoveMemories which is
	// authoritative and replaces unconditionally.
	AssignMemoryToTopic(memoryID, topicID, hubID string, confidence float64) error
	// UnassignMemoryFromTopic is the companion auto-assignment removal path,
	// used by ingest + dreams workers only.
	UnassignMemoryFromTopic(memoryID, topicID, hubID string) error
	CountMemoriesByTopic(scope VisibilityScope, hubID string) (map[string]int, error)
	CountUnassignedMemories(scope VisibilityScope, hubID string) (int, error)
	// CountTopicMemories returns per-topic counts and unassigned count in a single query.
	// Used by the topics list endpoint to avoid 2 separate round trips.
	CountTopicMemories(scope VisibilityScope, hubID string) (perTopic map[string]int, unassigned int, err error)
	ReorderTopics(hubID string, ops []model.ReorderOperation) error
	// GetTopicDepth walks parent_id from topicID upward and returns
	// the number of ancestors within hubID. The hubID scope prevents
	// a leaked / corrupted cross-hub parent_id from being traversed:
	// the walk stops at the first ancestor whose hub_id doesn't
	// match.
	GetTopicDepth(topicID, hubID string) (int, error)
	// MergeTopics is the destructive topic-merge primitive: rehomes
	// every memory in sourceIDs to targetID, reparents every child
	// topic of sourceIDs to targetID, deletes the source topics, and
	// returns the count of memories rehomed. Refuses on cross-hub
	// targets/sources, target ∈ sources, and topic-tree cycles. See
	// PostgresStore.MergeTopicsTx for the full flow and the Err* sentinels.
	MergeTopics(ctx context.Context, hubID, targetID string, sourceIDs []string) (int, error)
	// ApplyTopicRestructure is the focused reparent primitive used by
	// the dream restructure → review → resolve flow. newParentID may
	// be "" to promote the child to a root topic. See
	// PostgresStore.ApplyTopicRestructureTx for the contract.
	ApplyTopicRestructure(ctx context.Context, hubID, childID, newParentID string) error
	// IsTopicDescendant reports whether candidateID is ancestorID or a
	// transitive descendant of ancestorID, scoped to hubID. Used by the
	// Update handler to prevent cycles on reparent. Returns true when the
	// IDs are equal (self-parent is a trivial cycle).
	IsTopicDescendant(hubID, ancestorID, candidateID string) (bool, error)
	// GetSubtreeMaxDepth returns the depth of the deepest descendant of
	// topicID relative to topicID itself (0 if topicID has no children).
	// Used by the Update handler to verify a reparent does not push the
	// moving topic's subtree past the 5-level hard cap.
	GetSubtreeMaxDepth(hubID, topicID string) (int, error)
	GetTopicActivitySummary(scope VisibilityScope, topicID, hubID string, createdAfter time.Time, previewLimit int) (*model.TopicActivitySummary, error)
	ListUnassignedMemories(scope VisibilityScope, limit int) ([]model.Memory, error)
	ListUnassignedMemoriesByHub(hubID string, limit int) ([]model.Memory, error)
	GetMemoryTopicNameMap(scope VisibilityScope) (map[string]string, error) // memoryID → topicName
	GetMemoryTopicIDMap(scope VisibilityScope) (map[string]string, error)   // memoryID → topicID
	GetMemoryTopicNameMapForMemories(scope VisibilityScope, memoryIDs []string) (map[string]string, error)
	GetMemoryTopicIDMapForMemories(scope VisibilityScope, memoryIDs []string) (map[string]string, error)
	GetTopicKinds(hubID string) (map[string][]string, error) // topicID → top 3 memory kinds
	ListMemoriesByTopic(scope VisibilityScope, topicID string, limit int, cursor string) ([]model.Memory, string, error)

	// Waitlist
	CreateWaitlistEntry(ctx context.Context, entry *model.WaitlistEntry) error
	GetWaitlistEntry(ctx context.Context, id string) (*model.WaitlistEntry, error)
	GetWaitlistEntryByEmail(ctx context.Context, email string) (*model.WaitlistEntry, error)
	ListWaitlistEntries(ctx context.Context, opts model.WaitlistListOpts) ([]model.WaitlistEntry, string, int, error)
	UpdateWaitlistEntry(ctx context.Context, id string, status string, notes string, wave *int, approvedBy string) (*model.WaitlistEntry, error)
	BatchApproveWaitlist(ctx context.Context, ids []string, wave int, approvedBy string, notes string) (int, error)
	GetWaitlistStats(ctx context.Context) (*model.WaitlistStats, error)

	// Waitlist Invites
	CreateWaitlistInvite(ctx context.Context, invite *model.WaitlistInvite) error
	GetWaitlistInviteByToken(ctx context.Context, token string) (*model.WaitlistInvite, error)
	ConsumeWaitlistInvite(ctx context.Context, token string, userID string) error
	ExpireWaitlistInvites(ctx context.Context) (int, error)
	ListWaitlistInvitesByEntry(ctx context.Context, waitlistID string) ([]model.WaitlistInvite, error)
	// ListOrphanWaitlistCandidates returns users with an active,
	// not-yet-consumed waitlist invite — the cohort stranded by the
	// 2026-04 OAuth invite-token drop. Backs the admin reconcile
	// preview endpoint.
	ListOrphanWaitlistCandidates(ctx context.Context, opts model.OrphanWaitlistCandidateOpts) ([]model.OrphanWaitlistCandidate, error)
	// GetOrphanCandidateForUser is the user-scoped variant of
	// ListOrphanWaitlistCandidates. Backs POST /v1/admin/waitlist/reconcile
	// so the per-user lookup can never false-negative because a bulk
	// limit hid the target. Returns (nil, nil) when the user is not
	// an orphan.
	GetOrphanCandidateForUser(ctx context.Context, userID string) (*model.OrphanWaitlistCandidate, error)

	RevokeWaitlistInvite(ctx context.Context, inviteID string) error
	GetActiveInviteForEntry(ctx context.Context, waitlistID string) (*model.WaitlistInvite, error)

	// Waitlist Invite Reminders
	ListExpiringWaitlistInvites(ctx context.Context, withinHours int) ([]model.WaitlistInvite, error)

	// User hub creation permission
	SetCanCreateHub(ctx context.Context, userID string, canCreate bool) error
	GetCanCreateHub(ctx context.Context, userID string) (bool, error)

	// Admin Roles
	GetAdminRole(ctx context.Context, userID string) (string, error)
	ListAdminRoles(ctx context.Context) ([]model.AdminRole, error)
	EnsureAdminRolesByEmail(ctx context.Context, emails []string, role string) (int, error)

	// Admin Hub Queries
	// ListTeamHubs returns a paginated slice of team hubs.
	// Returns (hubs, nextCursor, total, err). Keyset pagination —
	// cursor is the last hub.ID of the previous page. Query filter
	// is an ILIKE on hub name + slug.
	ListTeamHubs(ctx context.Context, opts model.AdminHubListOpts) ([]model.Hub, string, int, error)

	// Hub Subscriptions
	CreateHubSubscription(ctx context.Context, sub *model.HubSubscription) error
	GetActiveHubSubscription(ctx context.Context, hubID string) (*model.HubSubscription, error)
	// GetHubSubscriptionAnyStatus returns the most recent subscription
	// row for a hub regardless of status. Used by freeze checks that
	// need to see cancelled / past_due rows. Active-plan reads continue
	// to use GetActiveHubSubscription.
	GetHubSubscriptionAnyStatus(ctx context.Context, hubID string) (*model.HubSubscription, error)
	UpdateHubSubscription(ctx context.Context, sub *model.HubSubscription) error
	// SetHubOverLimit marks a hub as having crossed its memory cap.
	// Idempotent — only updates when over_limit_since IS NULL.
	// Returns true if this call caused the transition (previously
	// null, now set), false when the marker was already present or
	// no matching row existed. Callers can use the bool to fire
	// state-change notifications exactly once per transition.
	SetHubOverLimit(ctx context.Context, hubID string, now time.Time) (bool, error)
	// ClearHubOverLimit clears the marker. Returns true when a row
	// actually transitioned (had a non-null over_limit_since); false
	// when there was nothing to clear.
	ClearHubOverLimit(ctx context.Context, hubID string) (bool, error)
	// ListFrozenHubIDs returns the subset of input hubIDs whose
	// subscription is currently frozen (status != active OR
	// over_limit_since < graceCutoff, where graceCutoff is typically
	// `now - HubGracePeriod`). Empty result when all are fine.
	// Used on read paths (recall, ask) to strip frozen hubs from the
	// read scope. Hubs without a subscription row are never frozen.
	ListFrozenHubIDs(ctx context.Context, hubIDs []string, graceCutoff time.Time) ([]string, error)
	// ListHubsPastGrace returns hub ids whose active subscription has
	// been over-limit longer than the grace window (over_limit_since
	// < graceCutoff). Used by the periodic River sweep to fire the
	// hub_frozen notification once the grace expires.
	ListHubsPastGrace(ctx context.Context, graceCutoff time.Time) ([]string, error)
	ListHubSubscriptionsByBillingUser(ctx context.Context, userID string) ([]model.HubSubscription, error)

	// Effective Plan Cache
	GetEffectivePlan(ctx context.Context, userID string) (*model.EffectivePlan, error)
	SetEffectivePlan(ctx context.Context, userID, planID, source string) error
	GetHubPlanID(ctx context.Context, hubID string) (string, error)
	GetUserHubPlans(ctx context.Context, userID string) ([]model.HubPlanRef, error)

	// Plans
	ListPlans(ctx context.Context) ([]model.Plan, error)
	ListPlansByScope(ctx context.Context, scope model.PlanScope) ([]model.Plan, error)
	GetPlan(ctx context.Context, planID string) (*model.Plan, error)
	UpdatePlan(ctx context.Context, plan *model.Plan) error

	// Ownership Caps
	CountOwnedFreeTeamHubs(ctx context.Context, ownerID string) (int, error)

	// Plan Overrides
	GetPlanOverride(ctx context.Context, userID string) (*model.PlanOverride, error)
	UpsertPlanOverride(ctx context.Context, override *model.PlanOverride) error
	DeletePlanOverride(ctx context.Context, userID string) error

	// Usage Events
	InsertUsageEvent(ctx context.Context, event *model.UsageEvent) error
	// SetUsageCounters syncs Redis committed counters to Postgres using
	// monotonic GREATEST() — values can only increase. Used by the sync job.
	SetUsageCounters(ctx context.Context, userID string, periodStart, periodEnd time.Time, push, recall, ask int) error
	// ResetUsageCounters forces all counters to zero for the given period.
	// Unlike SetUsageCounters, this overwrites unconditionally (no GREATEST).
	// Used only by the admin billing reset path.
	ResetUsageCounters(ctx context.Context, userID string, periodStart, periodEnd time.Time) error

	// Usage Sync
	ListUsageUserIDs(ctx context.Context, periodStart time.Time) ([]string, error)

	// Billing Subscriptions
	UpsertBillingSubscription(ctx context.Context, sub *model.BillingSubscription) error

	// Admin User Management
	ListUsers(ctx context.Context, opts model.AdminUserListOpts) ([]model.User, string, int, error)
	UpdateUserPlan(ctx context.Context, userID string, planID string) error
	CountMemoriesByOwner(ctx context.Context, ownerID string) (int, error)
	// CountMemoriesByOwnerExcludingSeeds returns the owner count with
	// `source_kind = 'onboarding-seed'` rows filtered out. Plan 23 §5.5
	// — usage/quota/activation-trigger code should call this so seeds
	// don't auto-fire onboarding triggers or eat free-tier quota.
	CountMemoriesByOwnerExcludingSeeds(ctx context.Context, ownerID string) (int, error)
	CountMemoriesByHub(ctx context.Context, hubID string) (int, error)
	// SumStorageBytesByOwner returns total persisted bytes across memory
	// attachments. Backs the meter's storage-bytes reservation when the
	// Redis cache is cold.
	SumStorageBytesByOwner(ctx context.Context, ownerID string) (int64, error)
	GetSystemStats(ctx context.Context) (*model.SystemStats, error)

	// Dreams — Memory consolidation
	FindRelevantTopicIDs(ownerID string, memoryIDs []string, limit int) ([]string, error) // topics relevant to a batch of memories (by embedding similarity)
	FindRelevantTopicIDsByHub(hubID string, memoryIDs []string, limit int) ([]string, error)
	FindSimilarMemories(ownerID string, threshold float64, limit int) ([]model.SimilarMemoryPair, error)
	FindSimilarMemoriesByHub(hubID string, threshold float64, limit int) ([]model.SimilarMemoryPair, error)
	ArchiveMemory(id string, ownerID string) error
	ArchiveMemoryInHub(id string, hubID string) error
	ExecuteDreamMerge(keeper *model.Memory, absorbedID string, ownerID string, chunks []model.Chunk) error
	ExecuteDreamMergeInHub(keeper *model.Memory, absorbedID string, hubID string, chunks []model.Chunk) error
	CreateDreamRun(run *model.DreamRun) error
	UpdateDreamRun(run *model.DreamRun) error
	// ClaimStaleDreamRun flips any `running` dream_run row for the
	// given hub that started before `olderThan` into status='failed'
	// (report="stale: run exceeded timeout", finished_at=now()).
	// Returns true iff a row was reclaimed.
	//
	// Exists to recover from the "run completed in memory but the
	// terminal UpdateDreamRun failed (pgbouncer blip, OOM, worker
	// killed)" case. Without this, migration 012's partial unique
	// index would permanently block future cycles for that hub. The
	// engine calls ClaimStaleDreamRun on ErrDreamRunConcurrent and
	// retries the insert once.
	ClaimStaleDreamRun(ctx context.Context, hubID string, olderThan time.Duration) (bool, error)
	// HeartbeatDreamRun bumps last_heartbeat_at=now() for a running
	// dream_runs row. The engine calls it at every phase boundary
	// so ClaimStaleDreamRun can tell a slow-but-alive cycle from
	// one that has actually stopped calling home.
	HeartbeatDreamRun(ctx context.Context, runID string) error
	CreateDreamAction(action *model.DreamAction) error
	GetLatestDreamRun(ownerID string) (*model.DreamRun, error)
	GetLatestDreamRunByHub(hubID string) (*model.DreamRun, error)
	ListDreamRuns(ownerID string, limit int) ([]model.DreamRun, error)
	ListDreamRunsByHub(hubID string, limit int) ([]model.DreamRun, error)
	// ListDreamRunsForUser returns dream_runs covering every hub
	// the user participates in: their personal hub (owner_id =
	// userID) plus any team hub where they're a hub_member. Used
	// by the admin "Dream runs" tab to answer "did dreams run for
	// this user last night?" without jumping between per-hub
	// queries. Keyset paginated by (started_at DESC, id); pass
	// the last row's started_at as cursor to fetch the next page.
	// If hubID is non-empty, results are filtered to that single
	// hub.
	ListDreamRunsForUser(ctx context.Context, opts DreamRunListForUserOpts) ([]model.DreamRun, string, error)
	GetDreamActions(runID string) ([]model.DreamAction, error)
	// LoadDreamTriggerInputs returns one row per active memory in
	// hubID with the four Phase 2a trigger signals pre-aggregated.
	// Used by the dream engine's soft-mode trigger scan to avoid
	// N+1 queries over a hub's memories. ContradictionLookback
	// bounds the count to recent dream_actions; pass 0 to use the
	// engine's default (30 days).
	//
	// The returned rows are not sorted by any guaranteed key — the
	// caller is expected to iterate, evaluate, batch-log, and not
	// rely on order.
	LoadDreamTriggerInputs(ctx context.Context, hubID string, contradictionLookback time.Duration) ([]DreamTriggerInputRow, error)
	// LogTriggerDecisions batch-inserts dream_trigger_decisions
	// rows for runID + hubID. Soft-mode writes one row per memory
	// considered, including non-fires (decision.Fired=false) — the
	// absence of a fire is calibration evidence too. Best-effort:
	// returns an error so callers can log + continue, but a write
	// failure does NOT roll back the dream run.
	LogTriggerDecisions(ctx context.Context, runID, hubID string, rows []DreamTriggerDecisionRow) error
	// GetDreamRunByID reads a single run by UUID. Used by the
	// admin detail view; returns ErrNoRows equivalent when the run
	// does not exist.
	GetDreamRunByID(ctx context.Context, runID string) (*model.DreamRun, error)
	// ListNotificationsByDreamRunID returns every notification
	// tagged with this run's ID. Powers the admin detail view's
	// "audit trail" section — what receipt + which reviews did
	// this cycle produce. Uses the partial index
	// idx_notifications_dream_run_id (migration 017).
	ListNotificationsByDreamRunID(ctx context.Context, runID string) ([]model.Notification, error)

	// Notifications — Phase 3a of the inbox notification framework
	// (docs/plans/17-inbox-notification-framework.md). Reads enforce
	// the union visibility rule from §4.4: a row is visible to the
	// caller if (a) audience in (hub, hub_member) and the caller is
	// a current member of hub_id, OR (b) audience = user and
	// recipient_user_id = caller.
	//
	// CreateNotification is the auto-commit entry point; producers
	// that need a shared transaction boundary with a review / audit /
	// merge write use WithTx + CreateNotificationTx (in TxRunner).
	// Phase 3a wires the store primitives; Phase 3b wires producers
	// and the /v1/notifications handler.
	CreateNotification(n *model.Notification) error
	// CreateNotificationIfAbsent upserts a notification row but
	// distinguishes insert from conflict. Returns true when a fresh
	// row was written, false when (source_kind, source_id) already
	// existed. Used by producers that need to avoid re-firing the
	// realtime "notification.created" event on retry upserts.
	CreateNotificationIfAbsent(n *model.Notification) (bool, error)
	// GetNotificationBySourceKey returns the notification row keyed
	// on (source_kind, source_id), regardless of status. Used by
	// idempotent producers (e.g. chat propose_topic_merge) that
	// followed CreateNotificationIfAbsent's false-return and need
	// to surface the existing row's id + status to the caller —
	// without it, the producer can only return the locally-generated
	// uuid (which doesn't identify the actual inbox row) and a
	// status that lies about whether the row is pending vs already
	// resolved/dismissed. Returns ErrNotificationNotFound when no
	// row matches.
	GetNotificationBySourceKey(ctx context.Context, sourceKind, sourceID string) (*model.Notification, error)
	ListNotificationsForUser(ctx context.Context, opts NotificationListOpts) ([]model.Notification, string, error)
	GetNotification(ctx context.Context, id string, userID string, hubIDs []string) (*model.Notification, error)
	GetNotificationSummary(ctx context.Context, opts NotificationSummaryOpts) (*model.NotificationSummary, error)
	MarkNotificationSeen(ctx context.Context, id string, userID string, hubIDs []string) error
	DismissNotification(ctx context.Context, id string, userID string, hubIDs []string) error
	ResolveNotification(ctx context.Context, id string, userID string, hubIDs []string, resolution model.NotificationResolution) error
	// ResolveNotificationsBySource flips pending notifications keyed by
	// (source_kind, source_id) to status=resolved with the given
	// resolution, returning affected rows for SSE fan-out. Used by the
	// legacy token-based hub invite accept path to converge the inbox
	// with a successful join from outside the notifications resolver.
	// Unscoped by user/hub — the caller is the policy boundary.
	ResolveNotificationsBySource(ctx context.Context, sourceKind, sourceID string, resolution model.NotificationResolution) ([]model.Notification, error)
	// ExpireNotifications flips pending receipt rows whose expires_at
	// is past the given instant to status=expired, returning the
	// affected rows so the caller can publish notification.updated
	// events with change=expired (plan §4.5).
	ExpireNotifications(ctx context.Context, now time.Time) ([]model.Notification, error)
	// BulkMarkNotificationsSeen / BulkDismissNotifications return the
	// affected rows so the handler can emit one notification.updated
	// SSE event per row (plan §4.5). The handler enforces the
	// decision-kind refusal rule before invoking these.
	BulkMarkNotificationsSeen(ctx context.Context, opts BulkNotificationMutationOpts) ([]model.Notification, error)
	BulkDismissNotifications(ctx context.Context, opts BulkNotificationMutationOpts) ([]model.Notification, error)

	// --- Super-notif sub-item mutations (plan 18 §4.3) ---
	//
	// ViewNotificationItem stamps payload.items[itemID].viewed_at if it
	// is currently null. Works for both `checklist` and `digest` rows.
	// Returns the post-update item snapshot for SSE fan-out
	// (notification.updated change=item_updated). Idempotent — a second
	// call on an already-viewed item returns the existing snapshot.
	// Enforces the §4.4 visibility predicate via the same union rule
	// used by GetNotification.
	ViewNotificationItem(ctx context.Context, id, userID string, hubIDs []string, itemID string) (*ItemMutationResult, error)
	// CompleteNotificationItem stamps payload.items[itemID].completed_at
	// (and viewed_at if nil) on a `checklist` row. Refuses non-checklist
	// kinds with ErrInvalidNotificationKindForOp, locked items with
	// ErrChecklistItemLocked, and already-terminal rows with
	// ErrNotificationNotPending. Idempotent. Returns the post-update
	// snapshot AND a flag indicating whether every required_ids item is
	// currently complete (true even on idempotent re-fires when the
	// retry path matters — see plan 18 §4.6).
	//
	// Recorder path: callers fire from server-internal handler paths
	// (memory create, ask, configs.Sync, hubs.Create+Accept, dream
	// completion) and pass the same userID the outer handler already
	// authorized. The §4.4 visibility predicate is defense-in-depth so
	// a stray id from the wrong user gets refused at the store boundary.
	CompleteNotificationItem(ctx context.Context, id, userID string, hubIDs []string, itemID string) (*ItemMutationResult, error)
	// TryAutoResolveChecklist atomically flips a pending checklist row
	// to status=resolved + resolution=applied_auto after re-verifying
	// (under FOR UPDATE) that every required_ids item is still complete.
	// Returns `flipped=true` iff this call performed the UPDATE.
	//
	// A concurrent /resolve {dismiss} that wins the race causes a
	// subsequent TryAutoResolve call to return flipped=false with the
	// dismissed row pointer, so the handler can avoid lying about
	// auto_resolved or double-firing the resolved SSE.
	//
	// On not-found / not-visible the row pointer is nil. On
	// already-non-pending the row pointer reflects the terminal state.
	TryAutoResolveChecklist(ctx context.Context, id, userID string, hubIDs []string) (flipped bool, post *model.Notification, err error)
	// UpdateChecklistItemProgress patches payload.items[itemID].progress
	// without completing the item. Used by the Recorder to surface
	// live progress on the `five_memories` step before the threshold
	// fires. Refuses non-checklist kinds + terminal rows with the same
	// sentinels as CompleteNotificationItem.
	UpdateChecklistItemProgress(ctx context.Context, id, userID string, hubIDs []string, itemID string, current, target int) (*ItemMutationResult, error)
	// LatestPendingChecklistByRecipient returns the most recent pending
	// checklist row for the given recipient + source_kind. Backed by
	// the partial index `notifications_pending_onboarding_by_recipient_idx`
	// (migration 012). Returns ErrNotificationNotFound when no pending
	// row exists — the Recorder treats this as a fast no-op for users
	// past onboarding.
	LatestPendingChecklistByRecipient(ctx context.Context, recipientUserID, sourceKind string) (*model.Notification, error)
	// LatestChecklistByRecipient returns the most recent checklist row
	// regardless of status. Used by the restart handler to find the
	// highest version `{user_id}:v{n}` so the new row gets v{n+1}.
	// Returns ErrNotificationNotFound when no row exists.
	LatestChecklistByRecipient(ctx context.Context, recipientUserID, sourceKind string) (*model.Notification, error)
	// CountChecklistsCreatedSince counts a user's checklist rows
	// (any status) created at-or-after `since`. Backs the
	// restart-onboarding rate limit.
	CountChecklistsCreatedSince(ctx context.Context, recipientUserID, sourceKind string, since time.Time) (int, error)
	// QuerySuperNotifParentFunnel returns (status, resolution) →
	// count for super-notif rows in the cohort window. Plan 18 §7.3
	// parent-funnel query. sourceKind="" = "any source_kind".
	QuerySuperNotifParentFunnel(ctx context.Context, kind, sourceKind string, since time.Time) ([]SuperNotifFunnelRow, error)
	// QuerySuperNotifItemFunnel returns per-item (recipients, viewed,
	// completed) counts grouped by cohort_day. Plan 18 §7.3
	// item-funnel query — one JSONB expansion via
	// jsonb_array_elements.
	QuerySuperNotifItemFunnel(ctx context.Context, kind, sourceKind string, since time.Time) ([]SuperNotifItemFunnelRow, error)

	// QueryAdminNotificationBatches aggregates admin-sent notifications by
	// batch (source_id prefix before ':'). Used by the admin sent-history
	// listing endpoint.
	QueryAdminNotificationBatches(ctx context.Context, opts AdminNotifBatchOpts) ([]AdminNotifBatchRow, error)

	// Audiences — saved recipient rules for campaigns.
	CreateAudience(ctx context.Context, a *model.Audience) error
	GetAudience(ctx context.Context, id string) (*model.Audience, error)
	ListAudiences(ctx context.Context) ([]model.Audience, error)
	UpdateAudience(ctx context.Context, a *model.Audience) error
	DeleteAudience(ctx context.Context, id string) error

	// Campaigns — persistent admin communications. Status transitions
	// are explicit methods; there is no free-form UpdateCampaign so the
	// worker and the UI can't accidentally corrupt lifecycle state.
	CreateCampaign(ctx context.Context, c *model.Campaign) error
	GetCampaign(ctx context.Context, id string) (*model.Campaign, error)
	ListCampaigns(ctx context.Context, opts model.CampaignListOpts) ([]model.Campaign, string, error)
	UpdateCampaignDraft(ctx context.Context, id string, patch CampaignDraftPatch) error
	DeleteCampaign(ctx context.Context, id string, actorID string) error
	ScheduleCampaign(ctx context.Context, id string, scheduledAt time.Time, actorID string) error
	CancelCampaign(ctx context.Context, id string, actorID string) error
	// ClaimCampaignForSend is a compare-and-swap: expectedStatus → "sending".
	// Returns ErrCampaignStateMismatch if the current status does not match.
	ClaimCampaignForSend(ctx context.Context, id, expectedStatus string, startedAt time.Time, batchID string) error
	// RevertCampaignToDraft reverses a sending/scheduled transition when
	// the subsequent River enqueue fails. Does not interact with queued
	// jobs — callers must use CancelCampaign when a job may already exist.
	RevertCampaignToDraft(ctx context.Context, id, fromStatus string) error
	// FinalizeCampaignSend records the terminal state of a send run. If
	// errorMsg is non-empty the final status is "failed"; otherwise "sent".
	FinalizeCampaignSend(ctx context.Context, id string, sentCount, failedCount int, finishedAt time.Time, errorMsg string) error

	// Comms audit — append-only log of actor/action/resource tuples.
	AppendCommsAudit(ctx context.Context, entry *model.CommsAuditEntry) error
	ListCommsAudit(ctx context.Context, resourceType, resourceID string) ([]model.CommsAuditEntry, error)

	// Email brand settings — singleton row wrapping every outbound email
	// (logo, footer, colors). Read by TemplateManager at render time.
	GetEmailBrandSettings(ctx context.Context) (*model.EmailBrandSettings, error)
	UpdateEmailBrandSettings(ctx context.Context, patch model.EmailBrandSettingsPatch, updatedBy *string) (*model.EmailBrandSettings, error)

	// Campaign email deliveries — one row per recipient for channel=email
	// campaigns. CreateCampaignDelivery is idempotent on
	// (campaign_id, recipient_email); the second return value reports
	// whether a new row was inserted.
	CreateCampaignDelivery(ctx context.Context, delivery *model.CampaignDelivery) (bool, error)
	MarkCampaignDeliverySent(ctx context.Context, deliveryID, providerMessageID string, sentAt time.Time) error
	MarkCampaignDeliveryFailed(ctx context.Context, deliveryID, errorCode, errorMessage string) error
	// AnnotateCampaignDelivery attaches error_code/error_message without
	// changing status. Used for informational annotations on suppressed
	// rows (e.g. opt_out_unknown).
	AnnotateCampaignDelivery(ctx context.Context, deliveryID, errorCode, errorMessage string) error
	// Webhook-driven delivery state transitions. All are keyed by
	// delivery row id (resolved from the webhook payload via
	// GetCampaignDeliveryByCampaignAndEmail).
	GetCampaignDeliveryByCampaignAndEmail(ctx context.Context, campaignID, email string) (*model.CampaignDelivery, error)
	MarkCampaignDeliveryDelivered(ctx context.Context, deliveryID string, deliveredAt time.Time) error
	MarkCampaignDeliveryBounced(ctx context.Context, deliveryID string, bouncedAt time.Time, errorCode, errorMessage string) error
	MarkCampaignDeliveryComplained(ctx context.Context, deliveryID string, complainedAt time.Time) error
	MarkCampaignDeliveryOpened(ctx context.Context, deliveryID string, openedAt time.Time) error
	GetCampaignDeliveryStats(ctx context.Context, campaignID string) (*model.CampaignDeliveryStats, error)

	// IsUserEmailOptedOut reports whether the user has opted out of
	// marketing emails. Transactional sends ignore this flag.
	IsUserEmailOptedOut(ctx context.Context, userID string) (bool, error)
	// GetOrCreateEmailOptOutToken returns a stable per-user unsubscribe
	// token, minting one on first call. Idempotent — subsequent calls
	// return the same token until the user actually unsubscribes (which
	// rotates it).
	GetOrCreateEmailOptOutToken(ctx context.Context, userID string) (string, error)
	// UnsubscribeByToken marks email_opt_out_marketing=true and rotates
	// the token so the old unsubscribe URL stops working. Returns the
	// affected user id for audit, or ErrUnsubscribeTokenNotFound.
	UnsubscribeByToken(ctx context.Context, token string) (string, error)

	// PublishEmailTemplateOverride atomically promotes the draft_*
	// columns to the published (subject/html/text) columns and stamps
	// published_at. Inserts an audit revision with action="published".
	// Returns ErrEmailTemplateNotFound if no override row exists for
	// that template name (admin never saved a draft).
	PublishEmailTemplateOverride(ctx context.Context, name string, publishedBy *string) (*model.EmailTemplateOverride, error)

	// Campaign templates — admin-authored reusable email campaign bodies
	// (marketing scaffolds). Referenced only at authoring time; copied
	// into a campaign's inline content on pick. Delete does NOT cascade
	// to campaigns that were previously scaffolded from the template.
	ListCampaignTemplates(ctx context.Context, opts model.CampaignTemplateListOpts) ([]model.CampaignTemplate, error)
	GetCampaignTemplate(ctx context.Context, id string) (*model.CampaignTemplate, error)
	GetCampaignTemplateBySlug(ctx context.Context, slug string) (*model.CampaignTemplate, error)
	CreateCampaignTemplate(ctx context.Context, tmpl *model.CampaignTemplate) error
	UpdateCampaignTemplate(ctx context.Context, id string, patch model.CampaignTemplateUpsert, updatedBy *string) (*model.CampaignTemplate, error)
	SetCampaignTemplateArchived(ctx context.Context, id string, archived bool, updatedBy *string) (*model.CampaignTemplate, error)
	DeleteCampaignTemplate(ctx context.Context, id string) error

	// Chat — Plan 24 §"Feature 2 — Agent Chat" Phase 3.
	//
	// All Chat* methods are owner-scoped: every read takes an
	// ownerID parameter and joins / filters via owner_id, NEVER
	// trusting the caller's session_id alone. Mirrors the
	// memories pattern. The handler layer adds an additional
	// HubScopeMode check for scope-bounded principals (OAuth
	// grants restricted to specific hubs); cookie-session and
	// unscoped-API-key callers see all sessions they own.

	// CreateChatSession inserts the session row. Caller pre-
	// validates scope_type/scope_hub_ids invariants AND verifies
	// every hub_id in scope_hub_ids is one the caller is a
	// member of (or, for hub-scoped API keys, in the key's
	// allowlist) — the SQL CHECK only enforces the static-shape
	// invariant.
	CreateChatSession(ctx context.Context, session *model.ChatSession) error

	// GetChatSession reads one session by id, owner-scoped. Returns
	// ErrChatSessionNotFound on miss (distinct from a generic DB
	// error so the handler can map cleanly to 404).
	GetChatSession(ctx context.Context, sessionID, ownerID string) (*model.ChatSession, error)

	// ListChatSessionsByOwner is the sidebar / list-recent query.
	// Excludes soft-deleted by default. Keyset pagination by
	// (last_message_at DESC NULLS LAST, id) — cursor is opaque
	// to the handler, internal shape is RFC3339Nano + id.
	ListChatSessionsByOwner(ctx context.Context, opts ChatSessionListOpts) ([]model.ChatSession, string, error)

	// UpdateChatSessionMeta updates the patchable session fields
	// (title, pinned, archived, tools, toolset_version). Owner-
	// scoped via the WHERE clause. Returns ErrChatSessionNotFound
	// when no row matched.
	UpdateChatSessionMeta(ctx context.Context, sessionID, ownerID string, patch ChatSessionMetaPatch) error

	// SoftDeleteChatSession sets deleted_at = now(); the
	// chat_session_purge_worker hard-deletes child messages 90d
	// later. Owner-scoped.
	SoftDeleteChatSession(ctx context.Context, sessionID, ownerID string) error

	// AppendChatMessage inserts a chat_messages row and
	// atomically bumps the parent session's message_count +
	// last_message_at counters. The whole op runs in one
	// transaction so a crash mid-insert can't leave the
	// counters out of sync with the row count.
	//
	// **Lock acquisition is a SEPARATE step.** This method does
	// NOT touch chat_sessions.status / active_message_id /
	// locked_at — those are the lock primitive's domain, owned
	// by the upcoming send-handler's `AcquireChatSessionLock`
	// (Phase 3.3) which transitions idle → running. Splitting
	// the writes lets a regenerate (which may not need the lock)
	// share AppendChatMessage with the normal send path.
	//
	// The caller assigns sequence (passing the prior session's
	// message_count + 1); the store does NOT auto-increment so
	// retries are deterministic and idempotency_key conflicts
	// surface as ErrChatMessageIdempotencyConflict.
	//
	// **Owner-scoped at write time.** The transaction's first
	// step verifies `chat_sessions.id = msg.SessionID AND
	// chat_sessions.owner_id = ownerID FOR UPDATE`; a mismatch
	// returns ErrChatSessionNotFound and rolls back the insert.
	// Without this, a buggy handler could pass another user's
	// session_id and write into their transcript.
	AppendChatMessage(ctx context.Context, ownerID string, msg *model.ChatMessage) error

	// BeginChatMessageTurn is the send-message critical section.
	// In one transaction it (a) verifies the session exists, is
	// owned by the caller, and is not soft-deleted; (b) consults
	// the idempotency index — a hit returns the prior turn's
	// assistant message with Outcome=Replay (terminal status) or
	// InFlight (queued/running); (c) on a miss, atomically
	// transitions the session from 'idle' to 'running', inserts
	// the user + assistant rows (sequence allocated from the
	// session's current message_count), and bumps counters.
	//
	// **Sentinel mapping:**
	//   - ErrChatSessionNotFound — session id missing or wrong owner
	//   - ErrChatSessionLocked — session was not idle (another
	//     turn in flight) AND there's no idempotency match
	//   - ErrChatSessionMsgCapReached — message_count + 2 > 250
	//   - ErrChatMessageIdempotencyConflict — race-edge where
	//     two concurrent inserts for the same idempotency_key
	//     both pass the lookup and one loses on the unique index
	//     (returned to the loser; the winner gets Fresh)
	//
	// Lock release is paired: FinalizeChatMessage flips the
	// session back to idle. A crashed run leaves the lock held
	// until the lease sweeper (Phase 3.5) finalizes the orphan.
	BeginChatMessageTurn(ctx context.Context, in BeginChatMessageTurnInput) (BeginChatMessageTurnResult, error)

	// GetChatMessage reads one message by id, owner-scoped via
	// the parent session. Returns ErrChatMessageNotFound on miss.
	GetChatMessage(ctx context.Context, messageID, ownerID string) (*model.ChatMessage, error)

	// GetChatMessageByIdempotencyKey looks up an in-flight or
	// completed message by (session_id, idempotency_key). Returns
	// (nil, nil) when the key is unused; the send handler treats
	// nil as "go ahead and create".
	GetChatMessageByIdempotencyKey(ctx context.Context, sessionID, key string) (*model.ChatMessage, error)

	// ListChatMessagesBySession reads transcript rows in
	// (sequence ASC) order. Owner-scoped via the parent session.
	// Cursor uses the last sequence + id. Excludes superseded
	// regenerate parents by default; pass IncludeSuperseded for
	// admin / audit views.
	ListChatMessagesBySession(ctx context.Context, opts ChatMessageListOpts) ([]model.ChatMessage, string, error)

	// UpdateChatMessageStatus moves an assistant row through its
	// lifecycle (running → completed/partial_failed/failed/
	// canceled, or running → canceling → canceled). Owner-scoped
	// via the parent session join. The status field also drives
	// the chat_sessions.status read so the lock primitive's
	// invariants don't drift.
	UpdateChatMessageStatus(ctx context.Context, messageID, ownerID, status string) error

	// FinalizeChatMessage writes the terminal state for an
	// assistant row in one shot (status, content, tool_calls,
	// citations, usage, error, finished_at) and resets the
	// parent session's lock state (status='idle',
	// active_message_id=NULL, locked_at=NULL). Atomic via a
	// single transaction so a crash mid-finalize never leaves
	// a session locked-but-pointing-at-a-finished-message.
	FinalizeChatMessage(ctx context.Context, messageID, ownerID string, final ChatMessageFinalize) error

	// CancelChatMessage marks an in-flight assistant turn for
	// soft cancellation. Plan 24 §"Cancel UX" pins the shape:
	//
	//   1. The message moves to status='canceling' (NOT terminal
	//      yet — terminal is the worker's responsibility).
	//   2. The session moves to status='canceling' so a
	//      same-session POST /messages returns 409 chat_session_locked
	//      with phase='canceling' until the worker finalizes.
	//   3. A non-terminal `cancel.requested` row is appended to
	//      chat_message_events so live SSE clients see the cancel
	//      intent immediately (in <100ms) without waiting for the
	//      worker to wrap up its current tool.
	//
	// All three writes commit in one transaction. The handler then
	// publishes a wake-up via chatstream.Signaler so the in-flight
	// goroutine wakes and the agent.Run sub-context cancels;
	// agent.Run returns ctx.Canceled, the worker calls
	// FinalizeChatMessage with status='canceled', and the session
	// returns to idle.
	//
	// **Idempotency.** A cancel on a row that's already canceling /
	// canceled / completed / partial_failed / failed returns
	// ErrChatMessageNotCancelable + the actual current status via
	// the outcome — the handler maps this to 200 + the row's
	// current state for a no-op user experience.
	//
	// Returns ErrChatMessageNotFound for unknown id / wrong owner.
	// Returns ErrChatMessageNotCancelable for non-assistant rows
	// or rows not in a cancelable state.
	CancelChatMessage(ctx context.Context, messageID, ownerID string) (ChatMessageCancelOutcome, error)

	// BeginChatRegenerate is the regenerate-assistant critical
	// section. Plan 24 §"Send-message + resume" — regenerate
	// produces a NEW assistant row whose parent_message_id points
	// at the PRIOR assistant (not the user message), which the
	// list query's "exclude superseded" filter uses to hide the
	// older assistant in conversation views while preserving it
	// for audit.
	//
	// The single transaction:
	//
	//   1. SELECT FOR UPDATE the prior assistant + its session.
	//      Validates owner, role='assistant', and status is
	//      eligible (completed / partial_failed / failed —
	//      NOT canceled, NOT in-flight).
	//   2. Validates the session is idle (no other in-flight
	//      message). If not idle → ErrChatSessionLocked.
	//   3. Inserts the new assistant row at the next sequence,
	//      with parent_message_id = prior_assistant.id and
	//      status='queued'. ScopeHubIDsUsed is re-stamped from
	//      caller's live accessible-hubs ∩ session.scope_hub_ids
	//      so a hub-loss between original send and regenerate is
	//      respected at the new turn (this MUST NOT inherit the
	//      prior assistant's snapshot).
	//   4. Flips the session lock: status='running',
	//      active_message_id = new assistant id, locked_at=now().
	//
	// The handler enqueues the chat-message-run worker after
	// commit; the worker is identical to the send-flow worker —
	// regenerate just reuses the same machinery.
	//
	// Returns ErrChatMessageNotFound for unknown id / wrong owner.
	// Returns ErrChatRegenerateNotEligible when the prior assistant
	// status excludes regeneration.
	// Returns ErrChatSessionLocked when another message is in
	// flight in the same session.
	// Returns ErrChatSessionArchived if the session was archived
	// between handler check and tx.
	// Returns ErrChatSessionNoAccessibleHubs when the caller has
	// no live hubs in the session's scope (same fail-closed shape
	// as BeginChatMessageTurn).
	BeginChatRegenerate(ctx context.Context, in BeginChatRegenerateInput) (BeginChatRegenerateResult, error)

	// AppendChatMessageEvent writes one row to the SSE replay
	// buffer. Returns ErrChatMessageEventSeqConflict on the
	// (message_id, seq) UNIQUE collision — caller is expected
	// to allocate seq monotonically; a conflict signals a
	// programmer bug, not a retry path.
	//
	// **Owner-scoped at write time.** Uses INSERT...SELECT with
	// an EXISTS predicate over chat_messages → chat_sessions on
	// owner_id; rejects (returns ErrChatMessageNotFound) when
	// the message doesn't belong to the caller. Without this, a
	// buggy handler could write SSE events into another user's
	// message stream.
	AppendChatMessageEvent(ctx context.Context, ownerID string, ev *model.ChatMessageEvent) error

	// ListChatMessageEvents replays the SSE buffer for a
	// resume request. Returns rows with seq > sinceSeq in
	// ascending order. Owner-scoped via the parent message →
	// session join.
	ListChatMessageEvents(ctx context.Context, messageID, ownerID string, sinceSeq int) ([]model.ChatMessageEvent, error)

	// DeleteChatMessageEventsOlderThan is the 24h replay-window
	// sweeper. Returns the count of rows deleted for telemetry.
	DeleteChatMessageEventsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)

	// ListExpiredChatLeases returns chat sessions whose lease has
	// expired — they entered a non-idle status (running or
	// canceling) past `lockedBefore` and never finalized.
	// Operationally these are sessions whose worker crashed,
	// timed out, or was deployed-out-of mid-turn; the lease
	// sweeper finalizes the corresponding assistant message as
	// `failed{server_crashed}` via FinalizeChatMessage, which
	// (per Phase 3.4b2.b v2) writes the terminal SSE row + resets
	// the session lock in one transaction.
	//
	// Limit caps the per-sweep batch so a long backlog (e.g.
	// thousands of stuck sessions after a deploy storm) is
	// processed in chunks across successive sweeps without
	// blocking other queue work.
	ListExpiredChatLeases(ctx context.Context, lockedBefore time.Time, limit int) ([]ExpiredChatLease, error)

	// CreateChatToolApproval inserts a 'pending' approval row.
	// V1 always inserts; idempotency at the approval layer is
	// enforced by DecideChatToolApproval (UPDATE WHERE
	// status='pending' returns 0 rows on a second decide). The
	// args_hash is stored for future use if we add (message_id,
	// args_hash) deduplication at the create step.
	//
	// **Owner-scoped at write time.** Uses INSERT...SELECT with
	// an EXISTS predicate over chat_messages → chat_sessions on
	// owner_id; rejects (returns ErrChatMessageNotFound) when
	// the message doesn't belong to the caller.
	CreateChatToolApproval(ctx context.Context, ownerID string, approval *model.ChatToolApproval) error

	// GetChatToolApproval reads one approval by id, owner-scoped
	// via the message → session join.
	GetChatToolApproval(ctx context.Context, approvalID, ownerID string) (*model.ChatToolApproval, error)

	// DecideChatToolApproval flips status from 'pending' to
	// 'approved' or 'denied' atomically via UPDATE...WHERE
	// status='pending'. Returns ErrChatApprovalAlreadyDecided
	// when the row is no longer pending (idempotency for
	// double-click clients). Owner-scoped via the message →
	// session → users join.
	//
	// **sessionID** is the path-id consistency check. Pass the
	// session id from the URL (`/v1/chat/sessions/{id}/approvals/...`);
	// the store filters on `m.session_id = $sessionID` in the
	// CAS predicate so a same-owner request can't decide an
	// approval that belongs to a different session of theirs.
	// Empty sessionID skips the check (used by internal
	// callers like the timeout sweeper that don't have a path
	// scope). Codex flagged the omission on Phase 3.6b1 v1.
	DecideChatToolApproval(ctx context.Context, approvalID, ownerID, sessionID, decision string) error

	// ListPendingChatToolApprovals returns pending approvals for
	// a single message — the UI surface for "you have N
	// approvals waiting on this turn". Owner-scoped.
	ListPendingChatToolApprovals(ctx context.Context, messageID, ownerID string) ([]model.ChatToolApproval, error)

	// ExpireChatToolApproval flips a single approval's status from
	// 'pending' to 'timed_out' if it's still pending. Used by the
	// SDK-host approver when its local deadline timer fires —
	// faster than waiting for the 1m global sweeper, and gives the
	// agent a clean timed_out result before the run's ctx
	// deadline. Owner-scoped and CAS-guarded: a concurrent user
	// decide that committed first wins, this method affects 0
	// rows and returns ErrChatApprovalAlreadyDecided so callers
	// can re-fetch and surface the user's decision instead.
	// Returns ErrChatApprovalNotFound if the row doesn't exist
	// for this owner.
	ExpireChatToolApproval(ctx context.Context, approvalID, ownerID string) error

	// ExpirePendingChatToolApprovals is the 10-minute sweeper.
	// Flips status pending → timed_out for rows whose
	// expires_at < now(). Returns the timed-out approvals so
	// the worker can publish approval.decided{state=timed_out}
	// events for each — the SDK-host approver subscribes
	// per-approval and needs the wake-up to unblock the agent
	// goroutine. Codex's Phase 3.6b1 v1 review flagged the
	// missing publish path; without it, a timeout-via-sweeper
	// would only release the goroutine after its own deadline
	// elapsed (inside the agent.Run timeout), not on the
	// expires_at boundary the row records.
	ExpirePendingChatToolApprovals(ctx context.Context) ([]ExpiredChatToolApproval, error)
}

// ExpiredChatToolApproval is one row that the sweeper flipped
// from pending to timed_out. Carries the routing fields the
// approval.decided publish needs.
type ExpiredChatToolApproval struct {
	ApprovalID string
	OwnerID    string
}

// AdminOpsStore is the narrow surface required by the admin ops
// dashboard handler. Kept separate from Store so InMemoryStore +
// test mocks don't have to stub a pile of Postgres/River-specific
// methods they can't implement meaningfully. Implemented by
// *PostgresStore via duck typing — see postgres_ops.go.
type AdminOpsStore interface {
	GetOpsPulse(ctx context.Context, windowMinutes int) (*model.OpsPulse, error)
	ListOpsJobs(ctx context.Context, opts model.JobListOpts) (*model.JobListResponse, error)
	GetOpsJob(ctx context.Context, id int64) (*model.JobDetail, error)
	ListOpsStuckMemories(ctx context.Context, minAgeSeconds int, limit int) ([]model.StuckMemory, error)
	ListOpsFailedIngestions(ctx context.Context, sinceMinutes int, limit int) ([]model.FailedIngestion, error)
	ListOpsIngestionBySource(ctx context.Context, sinceMinutes int) ([]model.IngestionBySource, error)
	// AppendAdminAudit writes one append-only audit row for a
	// destructive admin ops action. Non-fatal on the caller path —
	// the handler logs failures but the action itself still
	// succeeds. Stored in admin_audit (not comms_audit) because
	// ops resource IDs are bigints, not uuids.
	AppendAdminAudit(ctx context.Context, entry *model.AdminAuditEntry) error
}

// ErrCampaignTemplateNotFound — id/slug lookup fails.
var ErrCampaignTemplateNotFound = errors.New("campaign template not found")

// ErrCampaignTemplateSlugTaken — create conflict on slug.
var ErrCampaignTemplateSlugTaken = errors.New("campaign template slug already in use")

// ErrEmailTemplateNotFound is returned when a template publish or
// lookup fails because no override row exists for that template name.
var ErrEmailTemplateNotFound = errors.New("email template override not found")

// CampaignDraftPatch carries the subset of fields the admin may edit on
// a draft campaign. All fields are optional; a nil pointer leaves the
// existing value in place. Updates against non-draft campaigns return
// ErrCampaignStateMismatch.
type CampaignDraftPatch struct {
	Name             *string
	AudienceID       *string
	AudienceSnapshot *model.AudienceSnapshot
	Content          json.RawMessage
	UpdatedBy        string
}

// ErrCampaignNotFound is returned when a campaign lookup fails.
var ErrCampaignNotFound = errors.New("campaign not found")

// ErrCampaignStateMismatch is returned when a lifecycle transition is
// rejected because the campaign is not in the expected source state.
var ErrCampaignStateMismatch = errors.New("campaign state does not allow this transition")

// ErrAudienceNotFound is returned when an audience lookup fails.
var ErrAudienceNotFound = errors.New("audience not found")

// Chat MVP sentinel errors. The handler layer maps these to
// HTTP status / error codes; the store returns them so callers
// don't pattern-match on opaque DB error strings.
var (
	ErrChatSessionNotFound            = errors.New("chat session not found")
	ErrChatMessageNotFound            = errors.New("chat message not found")
	ErrChatMessageIdempotencyConflict = errors.New("chat message idempotency_key already used in this session")
	ErrChatMessageEventSeqConflict    = errors.New("chat message event seq already used")
	ErrChatApprovalNotFound           = errors.New("chat tool approval not found")
	ErrChatApprovalAlreadyDecided     = errors.New("chat tool approval is no longer pending")
	// ErrChatApprovalExpired distinguishes a pending row whose
	// expires_at has passed from a row in a non-pending status.
	// The HTTP decide path returns this when the user clicks
	// approve/deny too late — the approver's local timer or the
	// global sweeper would have flipped the row to timed_out
	// shortly anyway, but enforcing the boundary at the SQL layer
	// closes the window where a stale click could "win" against
	// a healthy approver waiting on its deadline timer. Codex's
	// Phase 3.6b2 v2 review flagged this gap.
	ErrChatApprovalExpired = errors.New("chat tool approval has expired")
	// ErrChatInvalidCursor signals client-supplied cursor is
	// malformed (not RFC3339Nano|uuid). Distinct sentinel so
	// the handler can surface 400 instead of 500.
	ErrChatInvalidCursor = errors.New("chat list cursor is malformed")
	// ErrChatSessionLocked signals the session was not idle when
	// BeginChatMessageTurn ran — another message is in flight.
	// Distinct from ErrChatMessageIdempotencyConflict because the
	// caller's recovery path differs: locked → wait or 409, replay
	// → return prior result. Locked sessions ALSO return 409 to the
	// client (see plan 24 §"Cancel UX" — chat_session_locked is
	// the canonical error code), but the sentinel separation keeps
	// the contract obvious to handler readers.
	ErrChatSessionLocked = errors.New("chat session is busy with another message")
	// ErrChatSessionMsgCapReached signals the session has reached
	// the 250-message hard cap. Plan 24 §"Errors" — UI shows
	// "Continue in a new session." This is hit ONLY at send-time;
	// reads stay unrestricted.
	ErrChatSessionMsgCapReached = errors.New("chat session has reached the 250-message hard cap")
	// ErrChatSessionNoAccessibleHubs signals the caller's live
	// accessible-hubs set has no overlap with the session's
	// scope_hub_ids — the user has lost access to every hub the
	// session was scoped to. The send must reject because we'd
	// have nothing to stamp into ScopeHubIDsUsed (which the
	// runtime later uses to redact transcript reads on hub-loss).
	// Phase 3.4's read-time redaction depends on this never
	// allowing an empty stamp.
	ErrChatSessionNoAccessibleHubs = errors.New("caller has no accessible hubs in scope of this chat session")
	// ErrChatSessionArchived signals the session is archived and
	// cannot be sent into. Plan 24 §"Errors" maps this to HTTP
	// 409 chat_session_archived. The handler also pre-checks but
	// this sentinel exists so a race-edge (PATCH archives between
	// pre-check and lock-flip) still surfaces cleanly via the
	// store's FOR UPDATE read.
	ErrChatSessionArchived = errors.New("chat session is archived")
	// ErrChatMessageAlreadyFinalized is returned by FinalizeChatMessage
	// when the row exists and is owned by the caller but has
	// already reached a terminal status (completed / failed /
	// partial_failed / canceled). The CAS gate inside the UPDATE
	// keeps the first finalize the winner; subsequent racing
	// finalizes (sweeper, cancel, or this worker after a slow run)
	// see this sentinel and log+skip rather than overwriting the
	// terminal state. Codex caught the gap on the Phase 3.4a review.
	ErrChatMessageAlreadyFinalized = errors.New("chat message is already in a terminal state")

	// ErrChatMessageNotCancelable signals that CancelChatMessage was
	// called on a row whose role disqualifies it from cancellation
	// (i.e. not 'assistant'). Cancel only applies to in-flight
	// assistant turns; canceling a user message has no defined
	// semantics (the user message is durable from the moment it's
	// persisted; "cancel the user message" would mean "delete it"
	// which is a different operation).
	//
	// **Already-terminal / already-canceling rows DO NOT return
	// this sentinel.** The store returns nil + an outcome carrying
	// CancelRegistered=false and the row's actual current status
	// so the handler can give an idempotent 200 response. Only
	// truly mis-shaped requests (wrong role) get this error.
	ErrChatMessageNotCancelable = errors.New("chat message is not in a cancelable state")

	// ErrChatMessageCancelPending signals that FinalizeChatMessage
	// was called with a non-canceled target while the row is in
	// 'canceling' state. The CAS gate inside FinalizeChatMessage
	// only lets a 'canceled' target overwrite a 'canceling' row;
	// any other target (failed / completed / partial_failed) is
	// rejected with this sentinel.
	//
	// **Why.** Without this protection, a worker that's bottoming
	// out via failMessage or the deferred safety net could stomp
	// the user's cancel intent with 'failed' — codex's Phase 3.7b
	// review caught the gap. The cleanup path then has a clear
	// recovery: re-issue the finalize as 'canceled' (the user
	// asked for it, that's the correct terminal). The lease
	// sweeper applies the same recovery for crashed workers.
	//
	// Callers that observe this error MUST retry with status=
	// canceled (preserving whatever partial Content/ToolCalls/
	// Usage the original finalize was about to write — those are
	// still durable bookkeeping for the user's view of the
	// canceled turn).
	ErrChatMessageCancelPending = errors.New("chat message is in canceling state; only a canceled finalize is permitted")

	// ErrChatRegenerateNotEligible signals that
	// BeginChatRegenerate was called against an assistant message
	// whose status is not eligible for regeneration. Eligible
	// statuses are the natural-terminal set: completed,
	// partial_failed, failed. Canceled and in-flight (queued /
	// running / canceling / awaiting_approval) are excluded —
	// canceled because the user already chose to stop, and
	// in-flight because the original turn isn't done yet
	// (the handler returns 409 chat_session_locked).
	ErrChatRegenerateNotEligible = errors.New("chat message is not eligible for regenerate")
)

// ChatSessionListOpts controls the recent-sessions list query.
// Cursor is opaque RFC3339Nano + id composite (last row of
// previous page); empty cursor reads the most recent page.
// Limit is capped at 100 server-side; 0 uses the 30-row default.
//
// IncludeArchived/IncludeDeleted let admin/audit surfaces see
// rows the default sidebar query filters out. Default is "show
// only active conversations the user can resume".
//
// ArchivedOnly is the user-facing "archived chats" filter: it
// returns ONLY rows with archived_at IS NOT NULL. Mutually
// exclusive with IncludeArchived (when ArchivedOnly is true the
// query uses a strict NOT NULL filter regardless of
// IncludeArchived). The two flags exist because the bare
// "include archived" semantic combines both states (admin/audit
// surface) — that's not what an end-user "view my archived
// chats" recovery surface wants.
type ChatSessionListOpts struct {
	OwnerID         string
	Cursor          string
	Limit           int
	IncludeArchived bool
	ArchivedOnly    bool
	IncludeDeleted  bool
}

// ChatSessionMetaPatch carries the subset of session fields the
// PATCH /v1/chat/sessions/{id} endpoint may update. All fields
// are pointers so a nil leaves the existing value in place. The
// store's UPDATE clause builds dynamically from the non-nil
// fields, mirroring the CampaignDraftPatch pattern above.
type ChatSessionMetaPatch struct {
	Title          *string
	PinnedAt       *time.Time // pointer-to-zero-value clears (sets NULL); nil leaves alone
	ArchivedAt     *time.Time
	Tools          *[]string // a non-nil empty slice empties the tools list
	ToolsetVersion *int
	WriteHubID     *string // empty string clears (NULL); nil leaves alone
	PersonaID      *string // "" clears (inherit default); "none" disables; else persona id
}

// ChatMessageListOpts controls transcript reads. Cursor is the
// last sequence the caller has already seen (1-indexed within the
// session); empty / zero reads from the start.
type ChatMessageListOpts struct {
	SessionID         string
	OwnerID           string
	AfterSequence     int  // 0 = from start; positive = sequence > this
	Limit             int  // 0 = 50; capped at 200
	IncludeSuperseded bool // include regenerate parents (UI normally hides)
}

// ExpiredChatLease describes one chat session whose worker
// dropped the lease — the session is in a non-idle status
// (running/canceling) past the lease duration and the
// active assistant message hasn't reached a terminal status
// yet. The lease sweeper picks these up and finalizes the
// orphaned message as failed{server_crashed}.
type ExpiredChatLease struct {
	SessionID       string
	OwnerID         string
	ActiveMessageID string // points at the queued/running assistant
	LockedAt        time.Time
	Status          string // session status: running or canceling
}

// ChatTurnOutcome distinguishes the three branches a send-message
// request can take: a fresh insert (we wrote new rows + locked
// the session), a replay (the idempotency key matched a turn
// whose assistant message is already terminal), or an in-flight
// hit (the idempotency key matched a turn that's still running —
// the client should connect to the resume stream instead of
// retrying the POST). Plan 24 §"API contract" pins the HTTP
// shape for each: 202 fresh, 200 replay, 202 in_flight.
type ChatTurnOutcome string

const (
	ChatTurnFresh    ChatTurnOutcome = "fresh"
	ChatTurnReplay   ChatTurnOutcome = "replay"
	ChatTurnInFlight ChatTurnOutcome = "in_flight"
)

// BeginChatMessageTurnInput is the payload for the atomic send-
// message store operation. The handler pre-generates message IDs
// (so the response can include them on every branch — fresh,
// replay, in_flight) and the store either inserts new rows or
// returns the prior pair via the idempotency lookup.
//
// **All-or-nothing transaction.** The store wraps verify-session →
// idempotency-check → insert-user → insert-assistant → flip-lock
// in one tx. A crash mid-flow leaves the row count and lock state
// consistent: either both messages land and the session is
// 'running', or neither lands and the session stays 'idle'.
//
// **Toolset_version + scope are read inside the tx**, NOT taken
// from the handler. Codex's Phase 3.3 v1 review caught the TOCTOU:
// a PATCH could update tools/version between the handler's
// pre-fetch and the lock-flip, leaving the new message stamped
// against a stale toolset. Reading inside the FOR UPDATE makes
// the toolset and scope used by THIS turn the source of truth
// for replay/audit.
//
// **CallerAccessibleHubs is the credential's live hub set** at
// send-time. The store intersects this with the session's
// scope_hub_ids (or, for user_all_hubs sessions, takes the live
// set wholesale) and stamps the result onto each chat_messages
// row's scope_hub_ids_used array. Phase 3.4's read-time redaction
// then has the per-turn snapshot it needs to redact tool-result
// payloads from hubs the user has since lost.
type BeginChatMessageTurnInput struct {
	SessionID            string
	OwnerID              string
	UserContent          string
	IdempotencyKey       string // empty → no idempotency tracking; rare in practice but allowed
	UserMessageID        string // pre-generated UUID (handler-side uuid.NewString)
	AssistantMessageID   string
	CallerAccessibleHubs []string // GetAccessibleHubIDs(r), already normalized
	Now                  time.Time
}

// BeginChatMessageTurnResult signals which branch ran. For
// `Fresh`, both message pointers are populated with the just-
// inserted rows. For `Replay`, AssistantMessage holds the prior
// completed assistant row (the user/handler already has it
// from the prior call but we return it for symmetry). For
// `InFlight`, AssistantMessage holds the still-running row so
// the handler can surface its id to the client.
type BeginChatMessageTurnResult struct {
	Outcome          ChatTurnOutcome
	UserMessage      *model.ChatMessage // non-nil only for Fresh
	AssistantMessage *model.ChatMessage // non-nil for Fresh / Replay / InFlight
}

// ChatMessageFinalize is the terminal-state payload for an
// assistant message. The store writes all of these in one
// UPDATE so a partial finalize can't leave the row in an
// inconsistent shape (e.g., status='completed' with empty
// content). The session-side reset (status='idle',
// active_message_id=NULL, locked_at=NULL) runs in the same
// transaction.
type ChatMessageFinalize struct {
	Status     string
	Content    string
	ToolCalls  json.RawMessage
	Citations  json.RawMessage
	Usage      json.RawMessage
	Error      json.RawMessage // nil on success terminals
	FinishedAt time.Time
}

// ChatMessageCancelOutcome carries the post-cancel disposition
// the handler needs to shape its HTTP response. CancelRegistered
// reports whether the call moved the row into 'canceling' (true)
// or whether the row was already terminal/canceling and the call
// was a no-op (false). Status is the row's current status AFTER
// the call — for the no-op case it's the actual terminal status
// the row has settled at, so the handler can return it verbatim.
//
// SessionID is included so the handler can build the response
// body (and emit an audit log line) without a follow-up query.
type ChatMessageCancelOutcome struct {
	AssistantMessageID string
	SessionID          string
	Status             string // current status after the call (canceling / terminal)
	CancelRegistered   bool   // true → moved to canceling; false → no-op (already terminal or canceling)
}

// BeginChatRegenerateInput is the regenerate-assistant call
// shape. PriorAssistantMessageID identifies the assistant
// message the user wants to re-run; the new assistant row is
// inserted at the next sequence with parent_message_id = prior
// id (the supersession link the list query honors).
//
// CallerAccessibleHubs is the caller's live hub-membership set
// at the moment of the regenerate call; the new turn's
// ScopeHubIDsUsed is computed as the intersection with the
// session's pinned scope_hub_ids. This re-stamping (rather
// than copying the prior assistant's snapshot) is deliberate:
// a hub the user has lost access to since the original turn
// MUST be redacted from the new turn's reads.
type BeginChatRegenerateInput struct {
	PriorAssistantMessageID string
	OwnerID                 string
	NewAssistantMessageID   string // pre-generated UUID (handler-side uuid.NewString)
	CallerAccessibleHubs    []string
	Now                     time.Time
}

// BeginChatRegenerateResult carries the freshly inserted
// assistant row plus the original session and prior-assistant
// metadata the handler needs for its 202 response (stream URL
// builds against session id + new message id).
type BeginChatRegenerateResult struct {
	NewAssistantMessage  *model.ChatMessage // inserted row, status='queued'
	SessionID            string
	PriorAssistantID     string
	PriorAssistantStatus string // status of the row being superseded (completed / partial_failed / failed)
}

// AdminNotifBatchOpts configures the admin notification batch listing query.
type AdminNotifBatchOpts struct {
	SourceKinds []string // e.g. ["system_notice", "gift"]
	KindFilter  string   // optional kind filter (e.g. "system_notice")
	Limit       int
	Cursor      string // RFC3339 timestamp cursor
}

// AdminNotifBatchRow is one row from the batch aggregation query.
type AdminNotifBatchRow struct {
	BatchID        string
	Kind           string
	PayloadPreview string
	RecipientCount int
	CreatedAt      time.Time
}
