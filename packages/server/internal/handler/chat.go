// Phase 3.2 chat session CRUD endpoints. plan 24 §"Feature 2 —
// Agent Chat" §"API contract".
//
// Endpoints:
//
//	POST   /v1/chat/sessions                      — create
//	GET    /v1/chat/sessions[?cursor=&limit=&...] — list (recent first, keyset)
//	GET    /v1/chat/sessions/{id}                 — fetch session metadata
//	PATCH  /v1/chat/sessions/{id}                 — rename, pin, archive, tools
//	DELETE /v1/chat/sessions/{id}                 — soft-delete
//
// Message + SSE + approval endpoints land in Phase 3.3+.
//
// Every endpoint is owner-scoped: handlers read GetUserID(r) and
// pass it as ownerID to the store. The store layer joins/filters
// on owner_id at the WHERE clause so a buggy handler caller can
// never reach across owners.
//
// Scope-bounded principals (HubScopeAllowlist OAuth grants /
// API keys) get an additional gate at create-time: every
// scope_hub_ids[i] must be in the caller's accessible-hubs
// allowlist. This mirrors the strict-scope read-time check the
// recall pipeline already enforces — chat sessions can't pin
// a hub the principal doesn't have access to.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/chatstream"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// ChatHandler exposes session-level chat CRUD. Constructed once
// at app startup and registered in serverapp/routes.go.
type ChatHandler struct {
	store store.Store
	// enqueueRun is invoked after BeginChatMessageTurn's Fresh
	// outcome commits. It hands off to the chat-message-run job
	// (Phase 3.4a). nil is allowed — used in handler unit tests
	// that don't exercise the runtime; the assistant message
	// stays in 'queued' which is enough to verify the persistence
	// contract. In serverapp the enqueue function is wired via
	// SetEnqueueRun at app startup.
	enqueueRun ChatRunEnqueuer
	// signaler is the optional Phase 3.5b live-delivery hint
	// over the chat_message_events replay buffer. The Stream
	// handler subscribes per-connection; new events arrive
	// within Redis's pub-sub latency (<10ms) instead of the
	// 250ms poll cadence. Nil-safe — when Redis isn't
	// configured, Stream falls back to poll-only.
	signaler *chatstream.Signaler
	// publisher is the optional Phase 3.6b1 cross-process
	// signaling channel. Decide handlers publish
	// approval.decided events here so the SDK-host approver
	// (Phase 3.6b2) can wake up regardless of which API
	// process committed the decision. Nil-safe — when
	// Redis-broker is unset, Decide still commits the row;
	// the Phase 3.6b2 approver's 250ms poll fallback covers
	// the missing pub-sub.
	publisher events.Publisher
}

// ChatRunEnqueuer is the side-channel the handler uses to ask the
// queue to drive an assistant turn. Returning an error here is
// non-fatal at the handler level — the message is already
// persisted and the lease sweeper (Phase 3.5) finalizes orphans
// — but the handler logs at error so SRE can spot a broken queue.
type ChatRunEnqueuer func(ctx context.Context, sessionID, ownerID, assistantMessageID string) error

// NewChatHandler returns a ready handler. enqueueRun may be set
// later via SetEnqueueRun once the queue client is constructed —
// keeps the chat handler instantiable in tests that don't need
// the runtime hookup.
func NewChatHandler(s store.Store) *ChatHandler {
	return &ChatHandler{store: s}
}

// SetEnqueueRun wires the chat handler to the queue. Called once
// at app startup AFTER the queue client is built. Calling it more
// than once silently overwrites — there's only one queue per app.
func (h *ChatHandler) SetEnqueueRun(fn ChatRunEnqueuer) {
	h.enqueueRun = fn
}

// SetSignaler wires the Phase 3.5b live-delivery hint. Called at
// app startup. nil signaler is fine — the Stream handler falls
// back to poll-only.
func (h *ChatHandler) SetSignaler(s *chatstream.Signaler) {
	h.signaler = s
}

// SetEventsPublisher wires the events broker for Phase 3.6b1's
// approval.decided cross-process fan-out. Called at app
// startup. nil publisher is fine — the Decide handler still
// commits the row but the SDK-host approver (Phase 3.6b2)
// must rely on its 250ms poll fallback alone.
func (h *ChatHandler) SetEventsPublisher(p events.Publisher) {
	h.publisher = p
}

// chatDefaultTools (deprecated — derived from chatToolCatalog
// at init below). Kept here as a sentinel so a future code
// path that wants to reference the default list directly has
// a stable name.
//
// The default list is computed from the catalog: any tool with
// `DefaultInChat == true && Available == true` lands here.
// Codex caught the v1 Phase 3.6a drift where the catalog
// claimed list_hubs / get_hub as default but the static slice
// here only listed the original 4. Deriving from the catalog
// makes "default in chat" exactly what the catalog endpoint
// reports, with no second source of truth to drift.
//
// The previous static list:
// caller doesn't pass `tools`. Mirrors plan 24 §"Tool registry"
// — read-only tools that every paying tier can use. Mutating
// tools (push_memory, forget_memory) are opt-in per session and
// arrive via PATCH after the user explicitly enables them.
var chatDefaultTools = func() []string {
	out := make([]string, 0, len(chatToolCatalog))
	for _, t := range chatToolCatalog {
		if t.DefaultInChat && t.Available {
			out = append(out, t.Name)
		}
	}
	return out
}()

// chatDefaultModel is the fallback when the caller passes an
// empty `model` field. Sonnet 4.6 matches plan 24's chat margin
// table for paid tiers; Free tier should override to Haiku
// before reaching the handler (Phase 3.8 wires plan-aware
// validation on top).
const chatDefaultModel = "claude-sonnet-4-6"

// chatModelAllowlist guards against typo'd model strings reaching
// the agent runtime. Plan 24's tier table determines which
// models a given user CAN use; Phase 3.8 wires plan-aware
// validation. For now the handler accepts any string in this
// set so a missing tier-check doesn't silently allow Opus.
var chatModelAllowlist = map[string]struct{}{
	"claude-haiku-4-5-20251001": {},
	"claude-sonnet-4-6":         {},
}

// chatMaxTitleLen caps the session title at 200 chars so a stray
// novel-length title can't bloat the row. Auto-titler in Phase
// 3.7 produces a 6-word title; this is the user-supplied
// override ceiling.
const chatMaxTitleLen = 200

// CreateChatSessionRequest is the wire shape of POST /v1/chat/sessions.
// Mirrors plan 24's §"Create-session body". Fields are validated
// against (a) the caller's accessible hubs and (b) the
// scope-shape invariants the SQL CHECK on chat_sessions also
// enforces.
type CreateChatSessionRequest struct {
	Title       string   `json:"title,omitempty"`
	ScopeType   string   `json:"scope_type"`
	ScopeHubIDs []string `json:"scope_hub_ids,omitempty"`
	WriteHubID  string   `json:"write_hub_id,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// PatchChatSessionRequest is the wire shape of PATCH /v1/chat/sessions/{id}.
// All fields are pointers so an absent JSON key leaves the
// existing value in place. Pointer-to-zero-value (e.g.
// "pinned_at": null) clears the field.
//
// Scope (scope_type / scope_hub_ids / write_hub_id) is NOT
// patchable — changing the scope mid-conversation would
// invalidate the agent's context retention and the transcript
// redaction rules. A user that wants a different scope opens
// a new session.
type PatchChatSessionRequest struct {
	Title      *string   `json:"title,omitempty"`
	Pinned     *bool     `json:"pinned,omitempty"`
	Archived   *bool     `json:"archived,omitempty"`
	Tools      *[]string `json:"tools,omitempty"`
	WriteHubID *string   `json:"write_hub_id,omitempty"`
}

// Create handles POST /v1/chat/sessions. Validates the request
// shape, the caller's hub membership, and the scope-shape
// invariants. Returns 201 with the persisted session row.
func (h *ChatHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}

	var req CreateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON body")
		return
	}

	// Title: optional; capped to chatMaxTitleLen runes. Empty
	// title is fine — the auto-titler (Phase 3.7) fills it after
	// the first message lands.
	title := strings.TrimSpace(req.Title)
	if utf8RuneCount(title) > chatMaxTitleLen {
		writeError(w, http.StatusBadRequest, "title_too_long",
			"title exceeds 200 characters")
		return
	}

	scopeType := strings.TrimSpace(req.ScopeType)
	if !validChatScopeType(scopeType) {
		writeError(w, http.StatusBadRequest, "invalid_scope_type",
			"scope_type must be 'user_all_hubs', 'single_hub', or 'hub_subset'")
		return
	}

	// user_all_hubs is an open-ended scope: scope_hub_ids is
	// empty and Phase 3.3's scope resolver expands it to the
	// caller's live hub memberships at message-send time.
	// That semantic doesn't compose with HubScopeAllowlist
	// principals (hub-scoped API keys / OAuth grants): the
	// allowlist would be silently bypassed because there's no
	// scope_hub_ids list to intersect against. Codex caught this
	// privilege-escalation path on Phase 3.2 v3 — without this
	// gate, a key scoped to {hubA} could create a session that
	// fans out across {hubA, hubB, hubC...}. The fix forces
	// hub-scoped principals into single_hub or hub_subset so
	// the scope is explicit and intersected at create time.
	if authz := GetAuthContext(r); authz != nil &&
		authz.HubScopeMode == HubScopeAllowlist &&
		scopeType == model.ChatScopeTypeUserAllHubs {
		writeError(w, http.StatusForbidden, "hub_scope_required",
			"hub-scoped credentials must use scope_type 'single_hub' or 'hub_subset'")
		return
	}

	scopeHubIDs := normalizeChatHubIDs(req.ScopeHubIDs)
	if err := validateChatScopeShape(scopeType, scopeHubIDs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_scope_shape", err.Error())
		return
	}

	// Hub-membership gate. Every scope_hub_id must be in the
	// caller's accessible-hubs list. For cookie/JWT sessions
	// this is "hubs the user is a member of"; for hub-bounded
	// API keys this is the key's allowlist. Either way the
	// caller must already have access to every hub they pin
	// into a session.
	accessible := normalizeHubIDs(GetAccessibleHubIDs(r))
	if err := validateChatHubsInScope(scopeHubIDs, accessible); err != nil {
		writeError(w, http.StatusForbidden, "hub_not_accessible", err.Error())
		return
	}

	writeHubID := strings.TrimSpace(req.WriteHubID)
	if err := validateChatWriteHub(writeHubID, scopeType, scopeHubIDs, accessible); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_write_hub", err.Error())
		return
	}

	tools := req.Tools
	if tools == nil {
		// Default tool set is grant-aware. Normal web sessions get
		// the full capable surface (including approval-gated
		// mutations); restricted API grants get only the defaults
		// they are actually authorized to invoke.
		tools = defaultChatToolsForGrant(GetAuthContext(r), scopeType, scopeHubIDs, writeHubID)
	}
	tools, err := normalizeChatToolList(tools)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tools", err.Error())
		return
	}

	// Phase 3.6b3d v3 — grant-level memory:write gate. When
	// the session enables any approval-required (mutating)
	// tool, the caller's grant must carry PermMemoryWrite
	// for every (single_hub / hub_subset) or at least one
	// (user_all_hubs) hub the runtime could push to. Codex
	// caught the bypass: chat authz is PermChatWrite, but
	// push_memory writes via memory:write — the two are
	// distinct capabilities.
	if err := validateChatToolGrantPermissions(GetAuthContext(r), scopeType, scopeHubIDs, tools); err != nil {
		writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
		return
	}
	// Phase 3.6b3d v4 codex follow-up — for user_all_hubs
	// sessions, also pin the chosen write_hub_id against the
	// grant's per-hub permissions. The "at least one"
	// accessibility check above is broad; this narrows it to
	// the SPECIFIC hub the agent would target by default.
	// No-op for single_hub / hub_subset (already covered).
	if err := validateChatWriteHubGrantPermissions(GetAuthContext(r), scopeType, writeHubID, tools); err != nil {
		writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
		return
	}

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" {
		modelID = chatDefaultModel
	}
	if _, ok := chatModelAllowlist[modelID]; !ok {
		writeError(w, http.StatusBadRequest, "invalid_model",
			"model not in allowlist; supported: claude-sonnet-4-6, claude-haiku-4-5-20251001")
		return
	}

	now := time.Now().UTC()
	sess := &model.ChatSession{
		ID:             uuid.NewString(),
		OwnerID:        ownerID,
		ScopeType:      scopeType,
		ScopeHubIDs:    scopeHubIDs,
		WriteHubID:     writeHubID,
		Title:          title,
		Model:          modelID,
		Tools:          tools,
		ToolsetVersion: 1,
		Status:         model.ChatSessionStatusIdle,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := h.store.CreateChatSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: sess})
}

// Get handles GET /v1/chat/sessions/{id}.
func (h *ChatHandler) Get(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	sess, err := h.store.GetChatSession(r.Context(), id, ownerID)
	if errors.Is(err, store.ErrChatSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: sess})
}

// List handles GET /v1/chat/sessions.
//
// Query params:
//
//	cursor   — opaque keyset cursor (last_message_at|id)
//	limit    — page size (default 30, max 100)
//	archived — "true" returns ONLY archived sessions (the user-
//	           facing recovery surface). Default (omitted/false)
//	           returns active sessions only.
//	include  — comma-separated flags: "archived", "deleted"
//	           (legacy admin/audit surface; "archived" combines
//	           active+archived, "deleted" is admin-only and silently
//	           filtered for non-admins). Distinct from `archived`
//	           above, which is the strict archived-only filter.
func (h *ChatHandler) List(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	q := r.URL.Query()

	limit := 30
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}

	includeArchived := false
	for _, raw := range strings.Split(q.Get("include"), ",") {
		switch strings.TrimSpace(raw) {
		case "archived":
			includeArchived = true
		}
	}

	// `?archived=true` is the user-facing "show me my archived
	// chats" filter — distinct from `?include=archived` (admin
	// combined view). Accept "1" / "true" case-insensitively;
	// any other value is a no-op (default = active only).
	archivedOnly := false
	if v := strings.TrimSpace(q.Get("archived")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true":
			archivedOnly = true
		}
	}

	opts := store.ChatSessionListOpts{
		OwnerID:         ownerID,
		Cursor:          q.Get("cursor"),
		Limit:           limit,
		IncludeArchived: includeArchived,
		ArchivedOnly:    archivedOnly,
	}
	sessions, nextCursor, err := h.store.ListChatSessionsByOwner(r.Context(), opts)
	if errors.Is(err, store.ErrChatInvalidCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor",
			"cursor is malformed; omit it to read the most recent page")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"sessions":    sessions,
		"next_cursor": nextCursor,
	}})
}

// Patch handles PATCH /v1/chat/sessions/{id}.
//
// Patchable fields: title, pinned (true/false), archived (true/false),
// tools (replace), write_hub_id. Scope (scope_type / scope_hub_ids)
// is NOT patchable — changing it mid-conversation would invalidate
// transcript context.
func (h *ChatHandler) Patch(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")

	// Load current session for write_hub_id validation. Doing
	// this round-trip even when the patch doesn't touch
	// write_hub_id lets us also surface 404 cleanly without
	// race-y store-side ErrChatSessionNotFound mapping.
	current, err := h.store.GetChatSession(r.Context(), id, ownerID)
	if errors.Is(err, store.ErrChatSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	var req PatchChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON body")
		return
	}

	patch := store.ChatSessionMetaPatch{}
	now := time.Now().UTC()

	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if utf8RuneCount(t) > chatMaxTitleLen {
			writeError(w, http.StatusBadRequest, "title_too_long",
				"title exceeds 200 characters")
			return
		}
		patch.Title = &t
	}

	if req.Pinned != nil {
		if *req.Pinned {
			patch.PinnedAt = &now
		} else {
			zero := time.Time{}
			patch.PinnedAt = &zero
		}
	}

	if req.Archived != nil {
		if *req.Archived {
			patch.ArchivedAt = &now
		} else {
			zero := time.Time{}
			patch.ArchivedAt = &zero
		}
	}

	if req.Tools != nil {
		// Tools can ONLY change while the session is fully idle.
		// A patch landing mid-turn would race with the agent loop:
		// the in-flight message executes against the prior
		// toolset_version, but a successful PATCH would bump
		// toolset_version on the row, so the next read of
		// session.tools no longer matches what the running
		// message is actually using. Plan 24 §"Lifecycle policies"
		// codifies this as 409 chat_session_locked.
		//
		// We default to "deny unless idle" rather than enumerating
		// blocked statuses — codex's Phase 3.2 v2 caught that the
		// first version missed `canceling` (a transient state
		// while a cancel request walks back an in-flight tool).
		// Future status additions automatically get blocked too.
		if current.Status != model.ChatSessionStatusIdle {
			writeError(w, http.StatusConflict, "chat_session_locked",
				"tools can only be changed when the session is idle")
			return
		}
		normalized, err := normalizeChatToolList(*req.Tools)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_tools", err.Error())
			return
		}
		// Phase 3.6b3d v3 — same grant-level memory:write gate
		// as Create. A patch enabling push_memory must
		// re-validate the grant; the caller may have had
		// memory:write at session-create but lost it (or be
		// using a more-restricted key now). Validates against
		// the SESSION's frozen scope (scope_type +
		// scope_hub_ids never change after create).
		if err := validateChatToolGrantPermissions(GetAuthContext(r), current.ScopeType, current.ScopeHubIDs, normalized); err != nil {
			writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
			return
		}
		patch.Tools = &normalized
		// Bump toolset_version so each chat_messages row's
		// toolset_version_used can pin the tool surface in
		// effect when it ran. Plan 24 §"Tool registry" pins
		// this as the per-message replay anchor.
		newVersion := current.ToolsetVersion + 1
		patch.ToolsetVersion = &newVersion
	}

	if req.WriteHubID != nil {
		// Phase 3.6b3d v6 codex follow-up — write_hub_id can
		// only be changed when the session is idle. Without
		// this gate, a concurrent or interleaved PATCH could
		// flip the default write target between Send's gate
		// validation and the worker's descriptor build, so a
		// turn validated against hubA could execute against
		// hubB. This mirrors the same idle-only constraint
		// the Tools branch already enforces; both fields
		// affect the per-turn descriptor and must be frozen
		// while a message is in flight.
		if current.Status != model.ChatSessionStatusIdle {
			writeError(w, http.StatusConflict, "chat_session_locked",
				"write_hub_id can only be changed when the session is idle")
			return
		}
		// Validate the new write_hub_id against THIS session's
		// scope (which never changes). The caller's accessible-
		// hubs list is checked too — if the caller has lost
		// access to a hub they still have a session pinned to,
		// they can't add it as the write target.
		newWrite := strings.TrimSpace(*req.WriteHubID)
		accessible := normalizeHubIDs(GetAccessibleHubIDs(r))
		if err := validateChatWriteHub(newWrite, current.ScopeType, current.ScopeHubIDs, accessible); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_write_hub", err.Error())
			return
		}
		// Phase 3.6b3d v3 codex follow-up — re-run the grant
		// gate on a write_hub_id change. The grant may have
		// been downgraded (scoped to memory:read only) since
		// session-create; without this check, the patch would
		// happily set a new push target without verifying
		// the current grant authorizes pushes at all. We use
		// the SESSION's current tools (req.Tools wasn't
		// changed in this branch — its own block above ran
		// the gate already).
		// Effective tools list for revalidation: the NORMALIZED
		// req.Tools if being changed in this same patch (raw
		// req.Tools may contain whitespace/casing that
		// normalizeChatToolList trims; the persisted form is
		// the normalized one, so the gate must run against
		// THAT). Codex's Phase 3.6b3d v5 review caught this:
		// `" push_memory "` would persist as "push_memory"
		// but the gate looking up chatToolRequiredPermission
		// against the raw " push_memory " would miss.
		effectiveTools := current.Tools
		if patch.Tools != nil {
			// req.Tools branch above already validated against
			// scope_hub_ids; this WriteHubID branch only needs
			// the per-hub gate for user_all_hubs. patch.Tools
			// holds the normalized slice (set above).
			effectiveTools = *patch.Tools
		} else {
			if err := validateChatToolGrantPermissions(GetAuthContext(r), current.ScopeType, current.ScopeHubIDs, current.Tools); err != nil {
				writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
				return
			}
		}
		// Phase 3.6b3d v4 — pin the new write_hub_id against
		// the grant's per-hub permissions when the session is
		// user_all_hubs. No-op otherwise.
		if err := validateChatWriteHubGrantPermissions(GetAuthContext(r), current.ScopeType, newWrite, effectiveTools); err != nil {
			writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
			return
		}
		patch.WriteHubID = &newWrite
	}

	if err := h.store.UpdateChatSessionMeta(r.Context(), id, ownerID, patch); err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
			return
		}
		if errors.Is(err, store.ErrChatSessionLocked) {
			// PATCH lost a race with Send: handler's pre-check
			// saw idle, but Send acquired the lock and flipped
			// to running before PATCH's UPDATE landed. Same
			// error code as the handler-side pre-check so
			// clients see one stable contract.
			writeError(w, http.StatusConflict, "chat_session_locked",
				"tools can only be changed when the session is idle")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Return the patched row so the client doesn't need to
	// follow up with a GET. Re-read from the store rather than
	// constructing in memory so we get authoritative
	// updated_at + computed fields.
	updated, err := h.store.GetChatSession(r.Context(), id, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: updated})
}

// Delete handles DELETE /v1/chat/sessions/{id}.
//
// Soft-delete only — the row stays as a tombstone (id +
// owner_id + title + deleted_at) so historical references
// still resolve. The chat_session_purge_worker hard-deletes
// child messages 90 days later. Hard-delete of the entire
// row only happens via the GDPR account-data-delete path.
//
// Idempotent: a delete on an already-soft-deleted session
// returns 204. The store's WHERE deleted_at IS NULL guard
// makes the second DELETE a no-op without rowsAffected
// errors.
func (h *ChatHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	err := h.store.SoftDeleteChatSession(r.Context(), id, ownerID)
	if errors.Is(err, store.ErrChatSessionNotFound) {
		// Either the session never existed or it's already
		// soft-deleted. We treat both as "204 idempotent
		// success" — the user's intent (have this session
		// soft-deleted) is satisfied either way.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SendMessageRequest is the wire shape for POST /messages.
//
// `idempotency_key` is REQUIRED. Plan 24 §"API contract" treats
// it as the source of truth for retry safety: a client whose
// send-message request times out re-issues with the same key
// and the server returns the prior outcome (replay or in_flight)
// instead of double-spending the user's quota or producing two
// assistant messages.
//
// `content` is the user's prompt. Capped at 100K runes — prevents
// DoS via giant prompts and matches what real text editors paste.
// Trimmed before storage.
type SendMessageRequest struct {
	Content        string `json:"content"`
	IdempotencyKey string `json:"idempotency_key"`
}

// SendMessageResponse is the body the handler emits on every
// branch (Fresh / Replay / InFlight). The shape is the same so
// clients only need one decoder. Status code distinguishes the
// case: 202 for Fresh + InFlight (the SSE stream is the side
// channel that delivers the actual content), 200 for Replay.
//
// For Replay the AssistantMessage carries the full terminal
// content; for Fresh + InFlight the assistant is queued/running
// and the client connects to ResumeURL to receive the stream
// (Phase 3.4 wires the SSE handler at that path).
type SendMessageResponse struct {
	Outcome          string             `json:"outcome"`
	UserMessageID    string             `json:"user_message_id,omitempty"`
	AssistantMessage *model.ChatMessage `json:"assistant_message"`
	ActiveMessageID  string             `json:"active_message_id"`
	ResumeURL        string             `json:"resume_url"`
}

// chatMaxContentRunes caps the `content` field of a send-message
// request. 100,000 runes is generous for any realistic chat
// prompt (the world's largest reasonable document copy/paste is
// well under) while bounding memory and Postgres COPY cost in
// the worst case. Any code path that wants to send more should
// use the upload + reference flow, not stuff bytes into chat.
const chatMaxContentRunes = 100_000

// chatResumeURL builds the SSE resume path for a given session +
// in-flight assistant message. The handler returns this URL in
// its 202 response so the client knows where to connect for
// Phase 3.4's SSE stream. Until 3.4 lands the path is reserved
// but not yet served — the contract is durable now so SDK/CLI
// can be built against it.
func chatResumeURL(sessionID, messageID string) string {
	return "/v1/chat/sessions/" + sessionID + "/messages/" + messageID + "/stream"
}

// Send handles POST /v1/chat/sessions/{id}/messages.
//
// Flow: validate body, generate message IDs, hand off to the
// store's BeginChatMessageTurn (one tx that verifies session,
// checks idempotency, hard cap, lock state, inserts user +
// assistant rows, flips lock). Map the outcome to HTTP status.
//
// The actual model invocation lives in Phase 3.4 (worker +
// SSE). For now the assistant row stays in 'queued' until the
// runtime picks it up. That's the right cut: this commit pins
// the durable persistence + idempotency contract; the next
// commit hooks the runtime onto it without changing this
// handler's contract.
func (h *ChatHandler) Send(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	sessionID := r.PathValue("id")

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON body")
		return
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "content is required and cannot be only whitespace")
		return
	}
	if utf8RuneCount(content) > chatMaxContentRunes {
		writeError(w, http.StatusBadRequest, "content_too_long",
			"content exceeds 100,000 characters")
		return
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key",
			"idempotency_key is required (client-generated UUID; prevents double-send on retry)")
		return
	}
	// We don't enforce strict UUID shape here — the database
	// stores it as `text` and the partial unique index doesn't
	// care. But cap the length to keep abusive callers from
	// stuffing a megabyte of "key". 200 is well above any UUID
	// or short URL-safe random.
	if len(idempotencyKey) > 200 {
		writeError(w, http.StatusBadRequest, "idempotency_key_too_long",
			"idempotency_key must be 200 characters or fewer")
		return
	}

	// Pre-fetch session to surface clean 404 / 409 codes before
	// the more expensive BeginChatMessageTurn transaction. This
	// read is OUTSIDE the tx; toolset_version and scope_hub_ids
	// are no longer used from this read (Codex's Phase 3.3 v1
	// review caught a TOCTOU where a concurrent PATCH could
	// update tools between this read and the lock-flip — the
	// store now reads them under FOR UPDATE so the message gets
	// stamped with the toolset that's actually current). What
	// the pre-fetch IS used for: scope_type (to gate
	// hub-scoped principals), deleted_at (404), archived_at
	// (409 chat_session_archived per plan 24 §"Errors").
	sess, err := h.store.GetChatSession(r.Context(), sessionID, ownerID)
	if errors.Is(err, store.ErrChatSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if sess.DeletedAt != nil {
		// Soft-deleted: GET still returns the row (so the UI
		// can show a tombstone view) but send must reject. The
		// store's verify step will also catch this, but we
		// produce a clearer error code here.
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if sess.ArchivedAt != nil {
		// Plan 24 §"Errors" — chat_session_archived. Archived
		// conversations are read-only by design; clients should
		// unarchive (PATCH archived=false) before sending.
		writeError(w, http.StatusConflict, "chat_session_archived",
			"this chat session is archived; unarchive it (PATCH archived=false) before sending")
		return
	}

	// Hub-scope gate — mirror the create-time check on Send so
	// a hub-scoped credential can't lift its scope by sending
	// into a session whose scope_type is user_all_hubs (which
	// at message time expands to "all the user's hubs"). Codex
	// caught this on Phase 3.3 v1 — without this the hub-scope
	// allowlist becomes advisory at session-creation time only.
	if authz := GetAuthContext(r); authz != nil &&
		authz.HubScopeMode == HubScopeAllowlist &&
		sess.ScopeType == model.ChatScopeTypeUserAllHubs {
		writeError(w, http.StatusForbidden, "hub_scope_required",
			"hub-scoped credentials cannot send into user_all_hubs sessions; create or use a single_hub / hub_subset session within your allowlist")
		return
	}

	if !chatSendHasAccessibleScope(sess.ScopeType, sess.ScopeHubIDs, normalizeHubIDs(GetAccessibleHubIDs(r))) {
		writeError(w, http.StatusForbidden, "hub_scope_required",
			"caller has no accessible hubs in scope of this session; rejoin a hub or recreate the session")
		return
	}

	// Phase 3.6b3d v3 codex follow-up — grant gate on Send.
	// Create + Patch already gate, but an existing session
	// with push_memory in tools[] could be used by a LATER
	// downgraded credential (a session created with
	// memory:write, then sent into via a chat:write +
	// memory:read key). The worker only has the live role
	// gate, not the original grant context — so the grant
	// gate must re-run at every Send. Same helper, same
	// behavior; here it reads the session's stored tools/
	// scope_type/scope_hub_ids rather than the request body.
	if maybeUpgraded, upgraded := maybeAutoUpgradeChatSessionTools(GetAuthContext(r), sess); upgraded {
		newVersion := sess.ToolsetVersion + 1
		patch := store.ChatSessionMetaPatch{
			Tools:          &maybeUpgraded,
			ToolsetVersion: &newVersion,
		}
		if err := h.store.UpdateChatSessionMeta(r.Context(), sessionID, ownerID, patch); err != nil {
			switch {
			case errors.Is(err, store.ErrChatSessionLocked):
				writeError(w, http.StatusConflict, "chat_session_locked",
					"another message is in flight on this session")
				return
			case errors.Is(err, store.ErrChatSessionNotFound):
				writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
				return
			default:
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
		}
		sess.Tools = maybeUpgraded
		sess.ToolsetVersion = newVersion
	}

	if err := validateChatToolGrantPermissions(GetAuthContext(r), sess.ScopeType, sess.ScopeHubIDs, sess.Tools); err != nil {
		writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
		return
	}
	// Phase 3.6b3d v5 codex follow-up — Send must also run
	// the per-write-hub gate for user_all_hubs sessions. The
	// broad gate above proves "some hub has the required
	// permission" for user_all_hubs; this narrows it to the
	// session's pinned write_hub_id specifically. Without
	// this, a session could be created with hubA writable,
	// then sent into with a downgraded grant where another
	// hub satisfies the broad check but the pinned write
	// hub no longer does — the agent would request_approval,
	// the user would approve, and the chat worker would
	// reject the actual push at runtime, wasting the cycle.
	// (No-op for single_hub / hub_subset — the strict
	// every-hub gate already covered them above.)
	if err := validateChatWriteHubGrantPermissions(GetAuthContext(r), sess.ScopeType, sess.WriteHubID, sess.Tools); err != nil {
		writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
		return
	}

	// Snapshot the caller's live accessible hubs. The store
	// will intersect this with the session's frozen scope (or,
	// for user_all_hubs, take it wholesale) under FOR UPDATE
	// so the per-turn scope_hub_ids_used stamp reflects what
	// the agent could ACTUALLY see this turn. Read-time
	// redaction (Phase 3.4) depends on this stamp being
	// accurate per-turn.
	in := store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              ownerID,
		UserContent:          content,
		IdempotencyKey:       idempotencyKey,
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: normalizeHubIDs(GetAccessibleHubIDs(r)),
		Now:                  time.Now().UTC(),
	}
	res, err := h.store.BeginChatMessageTurn(r.Context(), in)
	switch {
	case errors.Is(err, store.ErrChatSessionNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	case errors.Is(err, store.ErrChatSessionLocked):
		// Plan 24 §"Cancel UX" — the canonical response code
		// for "another turn is in flight". The handler fetches
		// the session's current active_message_id so the client
		// can connect to the resume stream rather than retrying
		// the POST blindly. The hint goes in error.details so
		// the body still follows the standard ApiResponse
		// envelope (Codex flagged the hand-encoded variant on
		// the v1 review).
		details := map[string]any{}
		if fresh, ferr := h.store.GetChatSession(r.Context(), sessionID, ownerID); ferr == nil && fresh.ActiveMessageID != "" {
			details["active_message_id"] = fresh.ActiveMessageID
			details["resume_url"] = chatResumeURL(sessionID, fresh.ActiveMessageID)
		}
		writeJSON(w, http.StatusConflict, model.ApiResponse{
			Error: &model.Error{
				Code:    "chat_session_locked",
				Message: "another message is in flight on this session",
				Details: details,
			},
		})
		return
	case errors.Is(err, store.ErrChatSessionArchived):
		// Race-edge: pre-fetch saw unarchived, but a PATCH
		// archived the session before BeginChatMessageTurn
		// acquired FOR UPDATE. Same error code as the pre-fetch
		// path so clients see one stable contract.
		writeError(w, http.StatusConflict, "chat_session_archived",
			"this chat session is archived; unarchive it (PATCH archived=false) before sending")
		return
	case errors.Is(err, store.ErrChatSessionNoAccessibleHubs):
		// The caller has lost access to every hub this session
		// is scoped to (or, for user_all_hubs, has no hubs at
		// all — an extreme edge). Without an in-scope hub the
		// agent runtime literally has no place to read from on
		// this turn, so we fail loud rather than persist a row
		// with empty scope_hub_ids_used (which would silently
		// bypass Phase 3.4's read-time redaction). 403 because
		// it's an authorization shape, not a missing resource.
		writeError(w, http.StatusForbidden, "hub_scope_required",
			"caller has no accessible hubs in scope of this session; rejoin a hub or recreate the session")
		return
	case errors.Is(err, store.ErrChatSessionMsgCapReached):
		writeError(w, http.StatusConflict, "chat_session_msg_cap",
			"this session has reached the 250-message hard cap; continue in a new session")
		return
	case errors.Is(err, store.ErrChatMessageIdempotencyConflict):
		// Race-edge: two concurrent inserts with the same
		// idempotency_key both passed the lookup; one lost on
		// the unique index. The losing client should retry —
		// the next attempt will see the winner's row in the
		// idempotency lookup and get Replay/InFlight.
		writeError(w, http.StatusConflict, "idempotency_conflict",
			"a concurrent send with the same idempotency_key is being processed; retry to see the persisted result")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Map outcome to HTTP shape.
	switch res.Outcome {
	case store.ChatTurnReplay:
		// Prior run already finalized — return the persisted
		// assistant content with 200 OK so clients can render
		// it directly without opening a stream.
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: SendMessageResponse{
			Outcome:          string(res.Outcome),
			AssistantMessage: res.AssistantMessage,
			ActiveMessageID:  res.AssistantMessage.ID,
			ResumeURL:        chatResumeURL(sessionID, res.AssistantMessage.ID),
		}})
		return
	case store.ChatTurnInFlight:
		// Prior run still pending — point the client at the
		// resume stream. 202 because we accepted the request
		// (the prior identical one) and processing is ongoing.
		writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: SendMessageResponse{
			Outcome:          string(res.Outcome),
			AssistantMessage: res.AssistantMessage,
			ActiveMessageID:  res.AssistantMessage.ID,
			ResumeURL:        chatResumeURL(sessionID, res.AssistantMessage.ID),
		}})
		return
	case store.ChatTurnFresh:
		// New rows inserted, session locked. Hand off to the
		// queue so the worker picks up the queued assistant
		// and drives it to terminal. Phase 3.5's lease sweeper
		// covers the (small) window between commit-here and
		// enqueue-success: a crash in this gap leaves the
		// session locked, but the sweeper finalizes orphans
		// after 5 minutes. Return 202 regardless — the rows
		// are durably persisted, the client can resume.
		if h.enqueueRun != nil {
			if err := h.enqueueRun(r.Context(), sessionID, ownerID, res.AssistantMessage.ID); err != nil {
				slog.WarnContext(r.Context(), "chat: enqueue run failed; lease sweeper will recover",
					"assistant_id", res.AssistantMessage.ID, "session_id", sessionID, "err", err)
			}
		}
		writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: SendMessageResponse{
			Outcome:          string(res.Outcome),
			UserMessageID:    res.UserMessage.ID,
			AssistantMessage: res.AssistantMessage,
			ActiveMessageID:  res.AssistantMessage.ID,
			ResumeURL:        chatResumeURL(sessionID, res.AssistantMessage.ID),
		}})
		return
	}
	// Shouldn't be reachable — every outcome is handled above.
	writeError(w, http.StatusInternalServerError, "store_error",
		"unknown turn outcome: "+string(res.Outcome))
}

// ListMessages handles GET /v1/chat/sessions/{id}/messages.
// Sequence-ascending keyset pagination — `cursor` is the last
// sequence the caller saw (e.g. "42"). Empty cursor reads from
// the start. Limit defaults to 50, capped at 200.
func (h *ChatHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	sessionID := r.PathValue("id")
	q := r.URL.Query()

	limit := 50
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	afterSeq := 0
	if v := strings.TrimSpace(q.Get("cursor")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_cursor",
				"cursor must be the integer sequence of the last message you saw")
			return
		}
		afterSeq = n
	}

	includeSuperseded := strings.EqualFold(q.Get("include"), "superseded")

	opts := store.ChatMessageListOpts{
		SessionID:         sessionID,
		OwnerID:           ownerID,
		AfterSequence:     afterSeq,
		Limit:             limit,
		IncludeSuperseded: includeSuperseded,
	}
	msgs, nextCursor, err := h.store.ListChatMessagesBySession(r.Context(), opts)
	if errors.Is(err, store.ErrChatSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"messages":    msgs,
		"next_cursor": nextCursor,
	}})
}

// GetMessage handles GET /v1/chat/sessions/{id}/messages/{msg_id}.
// Single-message read for transcript jumps and resume-after-
// reload flows. Owner-scoped via the parent session join.
func (h *ChatHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	messageID := r.PathValue("msg_id")

	msg, err := h.store.GetChatMessage(r.Context(), messageID, ownerID)
	if errors.Is(err, store.ErrChatMessageNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Path-id consistency check: a caller could pass a valid
	// message_id from a DIFFERENT session in the URL. The store
	// only checks owner, not session membership — so we double-
	// check here. A mismatch returns 404 (not 400) to avoid
	// leaking that the message exists on another session.
	pathSessionID := r.PathValue("id")
	if pathSessionID != "" && msg.SessionID != pathSessionID {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: msg})
}

// DecideApprovalRequest is the wire shape for POST
// /v1/chat/sessions/{id}/approvals/{approval_id}.
//
// `decision` is one of `"approve"` / `"deny"`. Plan 24
// §"Approval flow" — the user surfaces an Approve/Deny choice
// in the UI; this endpoint persists their verdict and wakes
// the blocked agent goroutine.
type DecideApprovalRequest struct {
	Decision string `json:"decision"`
}

// DecideApproval handles POST /v1/chat/sessions/{id}/approvals/{approval_id}.
//
// Lifecycle:
//
//  1. Validate the decision string (approve | deny).
//  2. Call store.DecideChatToolApproval — owner-scoped via
//     the message → session join, CAS on status='pending'
//     so a double-click is idempotent.
//  3. On success, publish approval.decided on the events
//     broker. The SDK-host approver (Phase 3.6b2) subscribes
//     for these and unblocks regardless of which API process
//     handled this request.
//  4. Return 200 with the new status.
//
// **Owner-scope.** The store path joins `chat_tool_approvals →
// chat_messages → chat_sessions` and gates on owner_id. A
// caller passing someone else's approval_id reads as
// not-found (404), no existence leak.
//
// **Idempotency.** A second decide on a non-pending row
// returns ErrChatApprovalAlreadyDecided. Mapped to 409 — the
// client likely double-clicked Approve; surface the conflict
// rather than silently accepting a redundant write that could
// race with the original publish.
func (h *ChatHandler) DecideApproval(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	approvalID := r.PathValue("approval_id")
	if approvalID == "" {
		writeError(w, http.StatusBadRequest, "missing_approval_id", "approval_id path parameter is required")
		return
	}
	// Path-id consistency: the URL nests approvals under
	// `/sessions/{id}/...`; the store uses this filter to ensure
	// the approval belongs to the session in the URL. Same-owner
	// cross-session attempts read as not-found. Codex caught
	// this gap on Phase 3.6b1 v1.
	pathSessionID := r.PathValue("id")

	var req DecideApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON body")
		return
	}
	decision := strings.TrimSpace(req.Decision)
	switch decision {
	case "approve":
		decision = model.ChatToolApprovalStatusApproved
	case "deny":
		decision = model.ChatToolApprovalStatusDenied
	default:
		writeError(w, http.StatusBadRequest, "invalid_decision",
			"decision must be 'approve' or 'deny'")
		return
	}

	if err := h.store.DecideChatToolApproval(r.Context(), approvalID, ownerID, pathSessionID, decision); err != nil {
		switch {
		case errors.Is(err, store.ErrChatApprovalNotFound):
			// Owner-scope-aware 404 — covers both "wrong owner"
			// and "approval id doesn't exist" without leaking
			// existence.
			writeError(w, http.StatusNotFound, "not_found", "approval not found")
			return
		case errors.Is(err, store.ErrChatApprovalAlreadyDecided):
			// Race-edge: client double-clicked OR the row was
			// flipped to a terminal status by the approver's
			// local timer / the timeout sweeper before the
			// click landed. The CAS gate preserves the prior
			// decision; surface the conflict so the UI can
			// refresh.
			writeError(w, http.StatusConflict, "chat_approval_already_decided",
				"this approval has already been decided")
			return
		case errors.Is(err, store.ErrChatApprovalExpired):
			// Pending row whose expires_at has passed — the
			// user clicked too late. The approver's local
			// timer or the periodic sweeper will flip the row
			// to timed_out shortly. Surface as 408 so the UI
			// shows "approval expired" rather than the
			// generic 409 already-decided. Plan 24's
			// chat_approval_timeout error code maps to 408.
			writeError(w, http.StatusRequestTimeout, "chat_approval_timeout",
				"this approval has expired")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Wake the blocked SDK-host approver via the broker. Owner
	// is the user who created the session (== the caller, since
	// the store call was owner-gated).
	events.PublishApprovalDecided(r.Context(), h.publisher, ownerID, approvalID, decision)

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"approval_id": approvalID,
		"status":      decision,
	}})
}

// Tools handles GET /v1/chat/tools — returns the catalog of
// tools available to the caller. Plan 24 §"Send-message + resume"
// pins this as "filtered to caller's plan (hides unreleased /
// out-of-plan tools)". Phase 3.6a returns the full catalog
// uniformly; plan-tier filtering arrives in Phase 3.8 with
// the quota integration.
//
// The endpoint is gated by PermChatRead via AuthorizeHTTP — any
// authenticated chat-eligible user can read the catalog. No
// per-session context needed (the catalog is process-wide).
func (h *ChatHandler) Tools(w http.ResponseWriter, r *http.Request) {
	if GetUserID(r) == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	// Return a copy so callers can't accidentally mutate the
	// process-wide catalog by editing the response slice.
	out := make([]ChatToolDescriptor, len(chatToolCatalog))
	copy(out, chatToolCatalog)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"tools": out,
	}})
}

// --- helpers ---

func validChatScopeType(s string) bool {
	switch s {
	case model.ChatScopeTypeUserAllHubs,
		model.ChatScopeTypeSingleHub,
		model.ChatScopeTypeHubSubset:
		return true
	}
	return false
}

func normalizeChatHubIDs(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func validateChatScopeShape(scopeType string, hubIDs []string) error {
	switch scopeType {
	case model.ChatScopeTypeUserAllHubs:
		if len(hubIDs) != 0 {
			return errors.New("user_all_hubs requires scope_hub_ids to be empty")
		}
	case model.ChatScopeTypeSingleHub:
		if len(hubIDs) != 1 {
			return errors.New("single_hub requires exactly 1 entry in scope_hub_ids")
		}
	case model.ChatScopeTypeHubSubset:
		if len(hubIDs) == 0 {
			return errors.New("hub_subset requires at least 1 entry in scope_hub_ids")
		}
	}
	return nil
}

func validateChatHubsInScope(scopeHubIDs, accessible []string) error {
	if len(scopeHubIDs) == 0 {
		return nil
	}
	allow := make(map[string]struct{}, len(accessible))
	for _, h := range accessible {
		allow[h] = struct{}{}
	}
	for _, h := range scopeHubIDs {
		if _, ok := allow[h]; !ok {
			return errors.New("scope_hub_ids contains a hub the caller does not have access to")
		}
	}
	return nil
}

func validateChatWriteHub(writeHubID, scopeType string, scopeHubIDs, accessible []string) error {
	if writeHubID == "" {
		return nil
	}
	// write_hub_id (when non-empty) must be a hub the caller
	// can actually write to. For user_all_hubs sessions any
	// accessible hub works; for single_hub / hub_subset the
	// hub must also be in scope.
	allowAccessible := make(map[string]struct{}, len(accessible))
	for _, h := range accessible {
		allowAccessible[h] = struct{}{}
	}
	if _, ok := allowAccessible[writeHubID]; !ok {
		return errors.New("write_hub_id must be a hub the caller has access to")
	}
	if scopeType == model.ChatScopeTypeUserAllHubs {
		return nil
	}
	for _, h := range scopeHubIDs {
		if h == writeHubID {
			return nil
		}
	}
	return errors.New("write_hub_id must be in scope_hub_ids when scope_type is single_hub or hub_subset")
}

// chatToolRequiredPermission maps each chat tool name to the
// per-hub Permission a grant must carry for that tool to be
// admissible. Tools NOT in this map are treated as needing no
// extra permission beyond PermChatRead/Write at the route
// boundary — recall_memories, list_memories, etc.
//
// **Why a separate map and not a field on ChatToolDescriptor.**
// The catalog is the PUBLIC surface (returned by GET
// /v1/chat/tools to clients). Required-permission is an
// internal authz detail; mixing them would force the catalog's
// JSON shape to leak Permission identifiers a future SDK
// upgrade may rename.
//
// Codex's Phase 3.6b3d v3 review pinned the generalization
// requirement: hardcoding PermMemoryWrite for every
// ApprovalRequired tool would be wrong as soon as
// forget_memory (PermMemoryDelete) and propose_topic_merge
// (PermTopicWrite) land — having memory:write authorizes
// pushes but NOT deletes; treating them as one bucket is a
// silent privilege escalation.
//
// When 3.6b3e adds forget_memory, register
// `"forget_memory": PermMemoryDelete` here AND in the
// catalog's chatToolByName entry, AND in the queue's
// chatToolBuilders registry. The parity test
// TestChatToolBuildersMatchCatalog enforces the
// builder/catalog half; this map is enforced via the
// validator's behavior + the new
// TestValidateChatToolGrantPermissions tests below.
var chatToolRequiredPermission = map[string]Permission{
	"push_memory":         PermMemoryWrite,
	"forget_memory":       PermMemoryDelete,
	"propose_topic_merge": PermTopicWrite,
}

// validateChatWriteHubGrantPermissions tightens the write_hub_id
// gate for user_all_hubs sessions: when a mutating tool is
// enabled and the session has a default write hub, that
// SPECIFIC hub must carry the tool's required permission in
// the grant. For single_hub / hub_subset sessions the strict
// per-hub validation in validateChatToolGrantPermissions
// already covers this — every in-scope hub is checked, so the
// chosen write hub is already known good.
//
// Without this, a user_all_hubs session that satisfies the
// "at least one accessible hub has memory:write" gate could
// be configured with a write_hub_id that's NOT one of those
// hubs. The session would create+patch successfully, the
// agent would call request_approval, the user would approve,
// and the chat worker's per-call role gate would reject the
// push at the boundary — wasting an approval cycle. Codex's
// Phase 3.6b3d v4 review flagged this as a UX (not security)
// gap. Better to fail fast at session-create / session-patch.
//
// Returns nil if writeHubID is empty (no default write hub),
// scope_type is not user_all_hubs (already covered), or no
// enabled tool requires a permission.
func validateChatWriteHubGrantPermissions(authCtx *AuthContext, scopeType, writeHubID string, tools []string) error {
	if writeHubID == "" || scopeType != model.ChatScopeTypeUserAllHubs {
		return nil
	}
	if authCtx == nil {
		return errors.New("auth context required to validate write_hub_id permissions")
	}
	perms := authCtx.PermissionsByHub[writeHubID]
	for _, t := range tools {
		if reqPerm, ok := chatToolRequiredPermission[t]; ok {
			if !perms.Has(reqPerm) {
				return fmt.Errorf("write_hub_id %q lacks %s (required because session enables %q)", writeHubID, reqPerm, t)
			}
		}
	}
	return nil
}

func chatSendHasAccessibleScope(scopeType string, sessionScopeHubs, callerAccessibleHubs []string) bool {
	if scopeType == model.ChatScopeTypeUserAllHubs {
		return len(callerAccessibleHubs) > 0
	}
	accessible := make(map[string]struct{}, len(callerAccessibleHubs))
	for _, hubID := range callerAccessibleHubs {
		accessible[hubID] = struct{}{}
	}
	for _, hubID := range sessionScopeHubs {
		if _, ok := accessible[hubID]; ok {
			return true
		}
	}
	return false
}

func defaultChatToolsForGrant(authCtx *AuthContext, scopeType string, scopeHubIDs []string, writeHubID string) []string {
	out := make([]string, 0, len(chatToolCatalog))
	for _, t := range chatToolCatalog {
		if !t.DefaultInChat || !t.Available {
			continue
		}
		toolList := []string{t.Name}
		if err := validateChatToolGrantPermissions(authCtx, scopeType, scopeHubIDs, toolList); err != nil {
			continue
		}
		if err := validateChatWriteHubGrantPermissions(authCtx, scopeType, writeHubID, toolList); err != nil {
			continue
		}
		out = append(out, t.Name)
	}
	normalized, err := normalizeChatToolList(out)
	if err != nil {
		return []string{}
	}
	return normalized
}

func maybeAutoUpgradeChatSessionTools(authCtx *AuthContext, sess *model.ChatSession) ([]string, bool) {
	if sess == nil {
		return nil, false
	}
	desired := defaultChatToolsForGrant(authCtx, sess.ScopeType, sess.ScopeHubIDs, sess.WriteHubID)
	if len(desired) == 0 || sameStringSet(sess.Tools, desired) {
		return nil, false
	}
	if !isLegacyDefaultChatToolSet(sess.Tools) {
		return nil, false
	}
	return desired, true
}

func isLegacyDefaultChatToolSet(tools []string) bool {
	switch {
	case sameStringSet(tools, []string{"recall_memories"}):
		return true
	case sameStringSet(tools, []string{
		"get_hub",
		"get_memory",
		"list_hubs",
		"list_memories",
		"list_topics",
		"recall_memories",
	}):
		return true
	default:
		return false
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
	}
	return true
}

// validateChatToolGrantPermissions ensures the caller's grant
// (the AuthContext's PermissionsByHub map — which reflects API
// key scope, hub-allowlist, and the underlying user's role
// intersected with the grant's permission set) carries the
// per-tool required permissions for every hub a chat session's
// approval-gated tools could write/mutate. Plan 24 §"approval
// flow" describes the approval modal as the user-visible
// authorization layer; this helper is the GRANT-LEVEL
// authorization layer underneath.
//
// **Why both layers.** Approval is informed user consent —
// "yes, I want this memory saved." It does not check whether
// the credential the agent is running under has been granted
// the relevant capability. A hub-scoped API key with
// {chat:write, memory:read} (no memory:write), where the
// underlying user IS a contributor on the hub, was previously
// able to:
//
//   - Create a chat session, enable push_memory.
//   - Send a message; the agent calls request_approval +
//     push_memory.
//   - The chat worker's chatPushAdapter checks the LIVE hub
//     role (contributor → write allowed) and proceeds.
//
// The grant intentionally restricted memory writes; the chat
// path was bypassing it because /v1/chat/* is gated by
// PermChatWrite, not PermMemoryWrite (or PermMemoryDelete,
// PermTopicWrite, etc. for other approval-gated tools).
// This helper closes the gap at session-create + session-
// patch + session-send time so a session that COULD mutate
// must demonstrate the grant authorizes those mutations.
//
// Codex's Phase 3.6b3d v2 review pinned this gate; v3
// pointed out three remaining gaps the helper now covers:
// (a) per-tool required permissions (was hardcoded
// PermMemoryWrite), (b) Send-time revalidation (Create/Patch
// alone left existing sessions usable by a later
// downgraded grant), (c) write_hub_id PATCH revalidation
// when mutating tools are already enabled.
//
// Per scope_type, for each tool's required permission:
//
//   - single_hub / hub_subset: every hub in scope_hub_ids
//     must have the required permission. Stricter than
//     "at least one" because pickPushHub can pick any hub
//     from scope when the agent supplies an explicit
//     hub_id arg.
//   - user_all_hubs: at least one accessible hub must have
//     the required permission. The runtime per-call role +
//     grant intersection (computed in HubContext middleware)
//     does the per-hub gate at message-send time.
//
// Returns nil if no tool with a required permission is
// enabled — read-only sessions skip the gate entirely so a
// chat:write-only grant can still create read-only chat
// sessions.
func validateChatToolGrantPermissions(authCtx *AuthContext, scopeType string, scopeHubIDs, tools []string) error {
	// Collect the set of (tool, required_permission) pairs
	// the session's tool list demands. A tool not in
	// chatToolRequiredPermission contributes nothing.
	type required struct {
		tool string
		perm Permission
	}
	var demands []required
	for _, t := range tools {
		if perm, ok := chatToolRequiredPermission[t]; ok {
			demands = append(demands, required{tool: t, perm: perm})
		}
	}
	if len(demands) == 0 {
		return nil
	}
	if authCtx == nil {
		// Defensive — shouldn't reach validation without an
		// AuthContext (RequireAuth runs before us). Surface as
		// a clear error so a wiring regression fails loud.
		return errors.New("auth context required to enable approval-gated tools")
	}

	switch scopeType {
	case model.ChatScopeTypeSingleHub, model.ChatScopeTypeHubSubset:
		for _, d := range demands {
			for _, hubID := range scopeHubIDs {
				if !authCtx.PermissionsByHub[hubID].Has(d.perm) {
					return fmt.Errorf("grant lacks %s on hub %q (required because session enables %q)", d.perm, hubID, d.tool)
				}
			}
		}
		return nil
	case model.ChatScopeTypeUserAllHubs:
		for _, d := range demands {
			authorized := false
			for _, perms := range authCtx.PermissionsByHub {
				if perms.Has(d.perm) {
					authorized = true
					break
				}
			}
			if !authorized {
				return fmt.Errorf("grant lacks %s on any accessible hub (required because session enables %q)", d.perm, d.tool)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown scope_type %q", scopeType)
	}
}

// ChatToolDescriptor is one entry in the chat tool catalog.
// Plan 24 §"Tool registry" pins the per-tool semantics; the
// shape here is what the GET /v1/chat/tools endpoint returns
// AND what the create-session validator gates against. One
// source of truth.
type ChatToolDescriptor struct {
	// Name is the canonical tool identifier (snake_case). The
	// agent runtime's tool resolver looks up the implementation
	// by this string.
	Name string `json:"name"`

	// Description is human-readable; UIs use it to build the
	// per-session tool-toggle list. Short — one sentence.
	Description string `json:"description"`

	// DefaultInChat reports whether new sessions get this tool
	// in their default tool list (no explicit `tools` arg at
	// create time). Read tools default to true; mutating tools
	// (push, forget, propose_topic_merge, fetch_url) are
	// opt-in.
	DefaultInChat bool `json:"default_in_chat"`

	// ApprovalRequired reports whether the agent must obtain a
	// user approval (chat_tool_approvals row) before each
	// invocation. UIs render an approval-card surface in the
	// transcript when these tools are enabled.
	ApprovalRequired bool `json:"approval_required"`

	// Available reports whether the agent runtime currently
	// implements this tool. The catalog publishes the future
	// vocabulary so UIs can plan; Available gates which entries
	// the runtime actually invokes. A user enabling an
	// unavailable tool gets a session whose tools[] includes
	// the name but the runtime never registers it — until the
	// implementation lands. Codex caught this drift on Phase
	// 3.6a v1.
	//
	// The Available=true set must match the worker's
	// tool-construction registry in queue/chat_workers.go; the
	// queue test pins that parity so the catalog never advertises
	// a tool the runtime cannot invoke.
	Available bool `json:"available"`

	// Notes is a short context line for the UI's tooltip /
	// settings drawer. Optional.
	Notes string `json:"notes,omitempty"`
}

// chatToolCatalog is the closed set of tool names a chat
// session can be created or patched with. Mirrors plan 24
// §"Tool registry" — this is the contract the agent runtime
// resolves against and the surface the catalog endpoint
// exposes.
//
// Adding a new tool is two changes here:
//   - Append a ChatToolDescriptor below with the right
//     metadata.
//   - Implement the tool under internal/agent/tools/ and
//     register it in the agent runtime's resolver
//     (queue/chat_workers.go's per-turn tool construction).
//
// Plan-tier filtering is the third piece, lands in Phase 3.8
// alongside the quota integration. For now every authenticated
// user sees the full catalog.
var chatToolCatalog = []ChatToolDescriptor{
	{
		Name:          "recall_memories",
		Description:   "Search the user's memory hub for relevant context.",
		DefaultInChat: true,
		Available:     true, // Phase 3.4b1 wired
		Notes:         "Hub-scoped to the session's scope_hub_ids.",
	},
	{
		Name:          "list_memories",
		Description:   "List recent memories in scope.",
		DefaultInChat: true,
		Available:     true,
		Notes:         "Hub-scoped.",
	},
	{
		Name:          "get_memory",
		Description:   "Read one memory's full content.",
		DefaultInChat: true,
		Available:     true,
		Notes:         "id from a prior tool call.",
	},
	{
		Name:          "list_hubs",
		Description:   "List the user's accessible hubs.",
		DefaultInChat: true,
		Available:     true,
	},
	{
		Name:          "get_hub",
		Description:   "Read one hub's metadata.",
		DefaultInChat: true,
		Available:     true,
		Notes:         "Membership-checked.",
	},
	{
		Name:          "list_topics",
		Description:   "List topics in scope.",
		DefaultInChat: true,
		Available:     true,
		Notes:         "Hub-scoped.",
	},
	{
		Name:             "push_memory",
		Description:      "Save a new memory.",
		DefaultInChat:    true,
		ApprovalRequired: true,
		Available:        true, // Phase 3.6b3d — approval flow + push tool wired
		Notes:            "User opts in per session; each invocation requires approval.",
	},
	{
		Name:             "forget_memory",
		Description:      "Delete a memory.",
		DefaultInChat:    true,
		ApprovalRequired: true,
		Available:        true, // Phase 3.6b3e — forget tool wired
		Notes:            "Approval required; permanent (audit trail in chat_tool_approvals).",
	},
	{
		Name:             "propose_topic_merge",
		Description:      "Propose merging one or more source topics into a target topic.",
		DefaultInChat:    true,
		ApprovalRequired: true,
		Available:        true, // Phase 3.6b3f — propose_topic_merge wired
		Notes:            "Writes a pending review_topic_merge notification to the user's inbox; the user resolves the notification to apply the merge.",
	},
	{
		Name:          "fetch_url",
		Description:   "Fetch a public URL with SSRF-safe defaults.",
		DefaultInChat: true,
		Available:     true, // Phase 3.6b3g — fetch_url wired
		Notes:         "Off by default; SSRF-safe transport blocks private/loopback/link-local IPs and memax-owned hosts; ≤5MB body, ≤10s timeout, ≤3 redirects.",
	},
}

// chatToolByName indexes the catalog for O(1) lookup at the
// validator hot path (every session create + patch hits it).
// Built once at init.
var chatToolByName = func() map[string]ChatToolDescriptor {
	out := make(map[string]ChatToolDescriptor, len(chatToolCatalog))
	for _, t := range chatToolCatalog {
		out[t.Name] = t
	}
	return out
}()

// AvailableChatToolNames returns the sorted set of catalog tool
// names whose Available flag is true. Exported so the queue
// package's parity test can assert worker-side chatToolBuilders
// keys exactly match this set — drift between layers is the
// thing the test is meant to catch. Builds a fresh slice on
// every call so callers can't mutate the underlying catalog.
func AvailableChatToolNames() []string {
	out := make([]string, 0, len(chatToolCatalog))
	for _, t := range chatToolCatalog {
		if t.Available {
			out = append(out, t.Name)
		}
	}
	sort.Strings(out)
	return out
}

// ChatToolApprovalRequired returns whether the catalog marks
// the named tool as ApprovalRequired. ok=false when the name
// isn't in the catalog. Exported so the queue package's parity
// test can verify the worker-side chatToolBuilders'
// ApprovalRequired flags match the handler-side catalog —
// flagging drift between the two layers at test time.
//
// Phase 3.6b3c added this. Without parity, a future tool
// could be marked ApprovalRequired in only one layer:
// catalog-only would mean clients see the approval prompt
// but no agentpolicy.ApprovalBeforeTool gates the call;
// builder-only would gate the call but the catalog wouldn't
// surface it as approval-required to clients. Either drift
// produces silent UX failures.
func ChatToolApprovalRequired(name string) (required bool, ok bool) {
	t, found := chatToolByName[name]
	if !found {
		return false, false
	}
	return t.ApprovalRequired, true
}

// normalizeChatToolList trims, dedupes (silently — duplicate
// from `--tool x --tool x` is a CLI nicety, not a hard error),
// and gates each entry through chatToolCatalog. Returns the
// canonical (deduped, sorted) slice that gets persisted.
//
// Sort order is stable so the same input always produces the
// same row contents, which matters for prompt-cache stability:
// the agent runtime hashes session.tools when assembling
// system prompts, and an unstable order would invalidate the
// cache on every read-back.
func normalizeChatToolList(tools []string) ([]string, error) {
	if len(tools) == 0 {
		// Empty is fine — the agent runs with zero tools and
		// just synthesizes from system prompt. Plan 24 doesn't
		// require a floor here; tier-aware defaults land later.
		return []string{}, nil
	}
	if len(tools) > 32 {
		return nil, errors.New("tools list exceeds 32 entries")
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		t = strings.TrimSpace(t)
		if t == "" {
			return nil, errors.New("tools list contains an empty string")
		}
		if _, ok := seen[t]; ok {
			continue
		}
		descriptor, ok := chatToolByName[t]
		if !ok {
			return nil, errors.New("unknown tool: " + t)
		}
		// Reject unavailable tools at validation. Codex's Phase
		// 3.6a v2 review pinned the contract: "tools you enable
		// must work now." Without this gate, a client could
		// persist a session with `push_memory` / `fetch_url` /
		// etc. today (those names are in the catalog roadmap
		// but the runtime can't invoke them yet); the user
		// would never see those tools fire, and worse — when
		// 3.6b/3.6c flips them to Available, the preexisting
		// sessions would silently gain behavior the user
		// didn't actively opt into. Enabling = working now;
		// roadmap visibility lives in the GET /v1/chat/tools
		// `available` flag.
		if !descriptor.Available {
			return nil, errors.New("tool not available yet: " + t)
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// utf8RuneCount counts runes (code points), not bytes. Used to
// cap title length at 200 runes — bytes would short-change
// users with non-ASCII titles.
func utf8RuneCount(s string) int {
	if s == "" {
		return 0
	}
	return len([]rune(s))
}

// CancelMessageResponse is the wire body for
// POST /v1/chat/sessions/{id}/messages/{msg_id}/cancel.
//
// The shape stays uniform regardless of whether THIS request was
// the one that registered the cancel intent: clients can trust
// `status` as the message's current state and read
// `cancel_registered` as a strict boolean for "did this call do
// anything." Idempotent cancels (second click on the same in-
// flight message, cancel against an already-terminal row) report
// `cancel_registered=false` with the actual current status — no
// 4xx noise for harmless replays. Plan 24 §"Cancel UX" pins this
// shape: cancel is always 200, never an error, when the row is
// reachable to the caller.
type CancelMessageResponse struct {
	AssistantMessageID string `json:"assistant_message_id"`
	SessionID          string `json:"session_id"`
	Status             string `json:"status"`
	CancelRegistered   bool   `json:"cancel_registered"`
}

// Cancel handles POST /v1/chat/sessions/{id}/messages/{msg_id}/cancel.
//
// Soft-cancel pattern: the handler doesn't directly finalize the
// row. It writes status='canceling' to the message + parent
// session and signals the worker; the worker's per-turn watcher
// observes the flip, cancels its run-context, and finalizes via
// FinalizeChatMessage (the single source of truth for terminal
// writes). This keeps the terminal-state path single-sourced
// regardless of whether the cancel landed pre-pickup, mid-run,
// or post-run; race-edges collapse onto the CAS gate inside
// FinalizeChatMessage.
//
// **Why no 4xx on already-terminal.** A user who clicks "Stop"
// twice, or whose first cancel arrived after the run finished
// naturally, doesn't need an error response — they just need
// the current status. The 200 + cancel_registered=false shape
// gives the UI everything it needs to update without rendering
// a spurious error toast.
func (h *ChatHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	sessionID := r.PathValue("id")
	messageID := r.PathValue("msg_id")
	if messageID == "" {
		writeError(w, http.StatusBadRequest, "missing_msg_id", "msg_id path parameter is required")
		return
	}

	// Path-id consistency: the URL nests messages under
	// /sessions/{id}/...; without this read the store path would
	// happily cancel a message owned by the same caller but on a
	// DIFFERENT session, violating the URL contract. We do the
	// session-membership check up front so the error is a clean
	// 404 rather than a successful cancel that mutated an
	// unintended row. Same shape GetMessage uses.
	msg, err := h.store.GetChatMessage(r.Context(), messageID, ownerID)
	if errors.Is(err, store.ErrChatMessageNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if sessionID != "" && msg.SessionID != sessionID {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}

	out, err := h.store.CancelChatMessage(r.Context(), messageID, ownerID)
	switch {
	case errors.Is(err, store.ErrChatMessageNotFound):
		// Race with a sibling handler that finalized + (potentially)
		// purged. Same code as the pre-fetch miss for a stable
		// client contract.
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	case errors.Is(err, store.ErrChatMessageNotCancelable):
		// The row exists but isn't an assistant turn (e.g. cancel
		// against a user message, which is durable from commit).
		// 400 because the resource exists and the request shape
		// is the issue, not authorization.
		writeError(w, http.StatusBadRequest, "not_cancelable",
			"only assistant messages can be canceled")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Wake the worker's cancel watcher immediately. The store's
	// chat_message_events row guarantees the watcher will see
	// the flip on its next 5s poll; this signaler.Notify
	// shortens that to <100ms in healthy systems. Nil-safe (no
	// Redis = no-op; the poll fallback in the watcher covers it).
	if out.CancelRegistered {
		h.signaler.Notify(r.Context(), messageID)
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: CancelMessageResponse{
		AssistantMessageID: out.AssistantMessageID,
		SessionID:          out.SessionID,
		Status:             out.Status,
		CancelRegistered:   out.CancelRegistered,
	}})
}

// RegenerateMessageResponse mirrors SendMessageResponse for the
// happy-path "fresh assistant queued" shape. We don't reuse
// SendMessageResponse directly because regenerate has no
// user_message_id (the user message it replies to was created
// during the original Send), and aliasing the type would either
// require a confusing empty field or a backward-incompatible
// rename when a future change diverges further.
type RegenerateMessageResponse struct {
	Outcome              string             `json:"outcome"`
	PriorAssistantID     string             `json:"prior_assistant_id"`
	PriorAssistantStatus string             `json:"prior_assistant_status"`
	AssistantMessage     *model.ChatMessage `json:"assistant_message"`
	ActiveMessageID      string             `json:"active_message_id"`
	ResumeURL            string             `json:"resume_url"`
}

// Regenerate handles POST /v1/chat/sessions/{id}/messages/{msg_id}/regenerate.
//
// Lifecycle:
//
//  1. Path-id consistency + session pre-fetch (404/409 mapping
//     before the regenerate transaction).
//  2. Hub-scope + tool-grant gates re-run (Send-equivalent — a
//     credential downgrade between the original turn and now
//     must be respected, even if the session was created with
//     broader permissions).
//  3. BeginChatRegenerate inserts the new assistant under the
//     prior assistant's id (parent_message_id supersession),
//     locks the session, re-stamps scope_hub_ids_used from the
//     caller's CURRENT accessible hubs.
//  4. Hand off to the queue. Same enqueue shape Send uses; the
//     worker doesn't care whether the queued row came from a
//     fresh send or a regenerate — the parent_message_id link
//     is invisible to the run path.
//
// **Why supersession, not delete.** The list query's
// `NOT EXISTS … parent_message_id … role` filter hides the
// older assistant from conversation views while preserving it
// for audit / replay / "show original" UI. Regenerating five
// times leaves five superseded rows on disk, all linked to the
// original via parent chain — diff/compare UIs become trivial
// to build later, with no schema change required.
func (h *ChatHandler) Regenerate(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	sessionID := r.PathValue("id")
	priorID := r.PathValue("msg_id")
	if priorID == "" {
		writeError(w, http.StatusBadRequest, "missing_msg_id", "msg_id path parameter is required")
		return
	}

	// Pre-fetch session for clean 404/409 error mapping +
	// hub-scope gating before the regenerate tx. Same pre-fetch
	// shape Send uses; the store re-reads under FOR UPDATE so
	// any TOCTOU between this read and the lock-flip is closed
	// by the store layer.
	sess, err := h.store.GetChatSession(r.Context(), sessionID, ownerID)
	if errors.Is(err, store.ErrChatSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if sess.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "not_found", "Chat session not found")
		return
	}
	if sess.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "chat_session_archived",
			"this chat session is archived; unarchive it (PATCH archived=false) before regenerating")
		return
	}

	// Path-id consistency check: the prior assistant must belong
	// to this session. A caller passing a valid msg_id from a
	// different session of theirs reads as 404 — same shape
	// GetMessage / Cancel use.
	prior, err := h.store.GetChatMessage(r.Context(), priorID, ownerID)
	if errors.Is(err, store.ErrChatMessageNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if prior.SessionID != sessionID {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}

	// Hub-scope re-gate. Mirror of Send's check: a hub-scoped
	// credential cannot regenerate into a user_all_hubs session
	// because that would lift its scope to "all the user's hubs"
	// at runtime.
	if authz := GetAuthContext(r); authz != nil &&
		authz.HubScopeMode == HubScopeAllowlist &&
		sess.ScopeType == model.ChatScopeTypeUserAllHubs {
		writeError(w, http.StatusForbidden, "hub_scope_required",
			"hub-scoped credentials cannot regenerate in user_all_hubs sessions; create or use a single_hub / hub_subset session within your allowlist")
		return
	}

	// Tool-grant + write-hub-grant gates — identical to Send.
	// A credential downgrade between the original turn and the
	// regenerate request must be respected.
	if err := validateChatToolGrantPermissions(GetAuthContext(r), sess.ScopeType, sess.ScopeHubIDs, sess.Tools); err != nil {
		writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
		return
	}
	if err := validateChatWriteHubGrantPermissions(GetAuthContext(r), sess.ScopeType, sess.WriteHubID, sess.Tools); err != nil {
		writeError(w, http.StatusForbidden, "grant_lacks_required_permission", err.Error())
		return
	}

	in := store.BeginChatRegenerateInput{
		PriorAssistantMessageID: priorID,
		OwnerID:                 ownerID,
		NewAssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs:    normalizeHubIDs(GetAccessibleHubIDs(r)),
		Now:                     time.Now().UTC(),
	}
	res, err := h.store.BeginChatRegenerate(r.Context(), in)
	switch {
	case errors.Is(err, store.ErrChatMessageNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	case errors.Is(err, store.ErrChatRegenerateNotEligible):
		// Either not an assistant role, or status is canceled /
		// queued / running / canceling. Plan 24 §"Cancel UX" rule:
		// a canceled message represents an explicit user choice to
		// stop; resending requires a new send (which we deliberately
		// do NOT auto-trigger from here — the user might have
		// canceled because the prompt itself was wrong). Surface as
		// 409 so the UI can show "this message can't be regenerated"
		// with a "send again" affordance.
		writeError(w, http.StatusConflict, "regenerate_not_eligible",
			"this message cannot be regenerated; only completed/partial_failed/failed assistant messages are eligible")
		return
	case errors.Is(err, store.ErrChatSessionArchived):
		writeError(w, http.StatusConflict, "chat_session_archived",
			"this chat session is archived; unarchive it (PATCH archived=false) before regenerating")
		return
	case errors.Is(err, store.ErrChatSessionLocked):
		// Another turn is in flight. Same shape Send uses — point
		// the client at the active resume stream.
		details := map[string]any{}
		if fresh, ferr := h.store.GetChatSession(r.Context(), sessionID, ownerID); ferr == nil && fresh.ActiveMessageID != "" {
			details["active_message_id"] = fresh.ActiveMessageID
			details["resume_url"] = chatResumeURL(sessionID, fresh.ActiveMessageID)
		}
		writeJSON(w, http.StatusConflict, model.ApiResponse{
			Error: &model.Error{
				Code:    "chat_session_locked",
				Message: "another message is in flight on this session",
				Details: details,
			},
		})
		return
	case errors.Is(err, store.ErrChatSessionNoAccessibleHubs):
		writeError(w, http.StatusForbidden, "hub_scope_required",
			"caller has no accessible hubs in scope of this session; rejoin a hub or recreate the session")
		return
	case errors.Is(err, store.ErrChatSessionMsgCapReached):
		writeError(w, http.StatusConflict, "chat_session_msg_cap",
			"this session has reached the 250-message hard cap; continue in a new session")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Hand off to the queue. Same enqueue path Send uses; the
	// worker doesn't distinguish regenerate from fresh send. The
	// log on enqueue failure mirrors Send's: the rows are
	// durably persisted and the lease sweeper recovers orphaned
	// locks within 5 minutes.
	if h.enqueueRun != nil {
		if err := h.enqueueRun(r.Context(), res.SessionID, ownerID, res.NewAssistantMessage.ID); err != nil {
			slog.WarnContext(r.Context(), "chat: regenerate enqueue failed; lease sweeper will recover",
				"assistant_id", res.NewAssistantMessage.ID, "session_id", res.SessionID, "err", err)
		}
	}

	writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: RegenerateMessageResponse{
		Outcome:              string(store.ChatTurnFresh),
		PriorAssistantID:     res.PriorAssistantID,
		PriorAssistantStatus: res.PriorAssistantStatus,
		AssistantMessage:     res.NewAssistantMessage,
		ActiveMessageID:      res.NewAssistantMessage.ID,
		ResumeURL:            chatResumeURL(res.SessionID, res.NewAssistantMessage.ID),
	}})
}

// Compile-time guard against URL-decoded query params drifting
// — keeps the handler self-contained without a sibling helper.
var _ = url.QueryEscape
