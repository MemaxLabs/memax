package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type RecallScope struct {
	HubIDs           []string
	ActiveBoostHubID string
	NoRerank         bool // per-request opt-out from reranking

	// Strict pins the recall pipeline to hub-only filtering
	// (`m.hub_id = ANY(HubIDs)` exactly — no owner-OR fallback).
	// Set to true when the request's authorization is bounded to
	// a hub allowlist (OAuth grant or API key with HubScopeMode =
	// HubScopeAllowlist), so a scoped principal can NEVER see
	// content outside their granted hubs even if they own it.
	//
	// Set to false for the default web/CLI/session path where
	// users recall their own content cross-hub by design and the
	// active hub is just a ranking-relevance boost. Codex review
	// on plan-24 step 5b confirmed this is the right boundary —
	// scope mode is a security-grade primitive, active-hub-boost
	// is a UX primitive.
	//
	// Build via requestRecallScope() — never set this field by
	// hand from caller-supplied data, or a misconfigured caller
	// could flip a scoped principal back to cross-hub by accident.
	Strict bool
}

func requestRecallScope(r *http.Request, requested []string) RecallScope {
	scope := RecallScope{
		HubIDs:           readHubScope(r, requested),
		ActiveBoostHubID: strings.TrimSpace(GetRetrievalBoostHubID(r)),
		Strict:           isHubScopeBounded(r),
	}
	if scope.ActiveBoostHubID == "" || !hubIDInScope(scope.ActiveBoostHubID, scope.HubIDs) {
		scope.ActiveBoostHubID = ""
	}
	return scope
}

// loadMemoryRespectingScope is the SINGLE entry point handlers use
// to fetch a memory by ID with the correct authorization shape for
// the request. It picks between strict-hub (scope-bounded
// principals) and cross-hub owner-OR-hub (default sessions)
// automatically based on the request's AuthContext.
//
// Why a helper, not just "if isHubScopeBounded { ... } else { ... }"
// in every handler: the strict-vs-cross-hub decision IS the
// scoped-OAuth-leak primitive. Centralizing it here means a future
// audit can grep for `loadMemoryRespectingScope` to find every
// memory-detail surface and verify the boundary holds; it also
// eliminates per-handler footguns where a refactor accidentally
// drops the strict path.
//
// All `GET /v1/memories/{id}`-style reads, attachment lookups, and
// the recall-result enrichment loop call this. The narrow internal
// store interface keeps the helper tight without dragging the full
// Store dependency tree into hub_scope.
func loadMemoryRespectingScope(r *http.Request, s memoryDetailStore, id string) (*model.Memory, error) {
	return loadMemoryWithScope(r.Context(), s, id, GetUserID(r), GetAccessibleHubIDs(r), isHubScopeBounded(r))
}

// loadMemoryWithScope is the request-free form of
// loadMemoryRespectingScope. Callers that have already extracted
// (ctx, ownerID, hubIDs, strict) — typically inner authorization
// helpers and tests — call this directly. The two helpers MUST
// stay in sync: the strict-vs-cross-hub decision is the same
// security primitive whichever entry point is used.
func loadMemoryWithScope(ctx context.Context, s memoryDetailStore, id string, ownerID string, hubIDs []string, strict bool) (*model.Memory, error) {
	if strict {
		// Scope-bounded principal. Strict hub-only path: a memory
		// outside the granted hubs MUST NOT surface even if the
		// principal owns it. Empty hubs short-circuit to "not
		// found" rather than fall through to owner-only — the
		// scope's whole point is that hubs are the access boundary.
		if len(hubIDs) == 0 {
			return nil, store.ErrMemoryNotFound
		}
		return s.GetMemoryInHubs(ctx, id, hubIDs)
	}

	// Default cross-hub session. owner_id = $user OR
	// hub_id = ANY($hubs). Web sessions and unscoped API keys.
	return s.GetAccessibleMemory(id, ownerID, hubIDs)
}

// memoryDetailStore is the focused subset of store.Store the helper
// depends on. Defined here so the helper signature stays narrow
// and tests can substitute a small fake.
type memoryDetailStore interface {
	GetAccessibleMemory(id string, userID string, hubIDs []string) (*model.Memory, error)
	GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error)
}

// isHubScopeBounded reports whether the request's authorization is
// strictly limited to a hub allowlist (OAuth grant or API key
// scoped to specific hubs). This is the load-bearing decision for
// strict-vs-cross-hub recall — a hub-bounded principal must never
// see content outside their granted hubs, regardless of ownership.
//
// **Fail-closed semantics.** When AuthContext is missing or has no
// HubScopeMode set, this returns true (strict). The reasoning:
// every recall surface in the production app sits behind auth
// middleware that populates AuthContext. If a request reaches the
// recall pipeline without one, something is wrong upstream — and
// the safer failure mode is "scoped principal with no hubs"
// (returns empty results) rather than "unscoped principal with
// owner-OR-hub" (leaks owner content). A misconfigured route is
// detectable as zero recall; a leak isn't detectable until it's
// already happened.
//
// HubScopeAllAccessible explicitly returns false — that's the
// default cookie-session / unscoped-API-key shape, where users
// recall their own content cross-hub by design. The auth layer's
// HubScopeMode is the SINGLE source of truth — no caller should
// re-derive scope-bounded-ness from credential type.
func isHubScopeBounded(r *http.Request) bool {
	auth := GetAuthContext(r)
	if auth == nil {
		// Fail-closed: an unauthenticated request shouldn't reach
		// recall at all (the route gate rejects it), so this case
		// indicates a wiring bug. Treat as scope-bounded so
		// downstream recall returns empty rather than leak.
		return true
	}
	// Empty HubScopeMode is also a fail-closed signal — a half-
	// constructed AuthContext (rare, but possible during a
	// migration) shouldn't open the gate.
	return auth.HubScopeMode != HubScopeAllAccessible
}

func readHubScope(r *http.Request, requested []string) []string {
	accessible := normalizeHubIDs(GetAccessibleHubIDs(r))
	if len(requested) == 0 {
		if len(accessible) > 0 {
			return accessible
		}
		return normalizeHubIDs([]string{GetHubID(r)})
	}

	if len(accessible) == 0 {
		activeHub := strings.TrimSpace(GetHubID(r))
		if activeHub == "" {
			return nil
		}
		accessible = []string{activeHub}
	}

	allowed := make(map[string]struct{}, len(accessible))
	for _, hubID := range accessible {
		allowed[hubID] = struct{}{}
	}

	scoped := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, hubID := range requested {
		hubID = strings.TrimSpace(hubID)
		if hubID == "" {
			continue
		}
		if _, ok := allowed[hubID]; !ok {
			continue
		}
		if _, ok := seen[hubID]; ok {
			continue
		}
		seen[hubID] = struct{}{}
		scoped = append(scoped, hubID)
	}
	sort.Strings(scoped)
	return scoped
}

func NewRecallScope(hubIDs []string, activeBoostHubID string) RecallScope {
	scope := RecallScope{
		HubIDs:           normalizeHubIDs(hubIDs),
		ActiveBoostHubID: strings.TrimSpace(activeBoostHubID),
	}
	if scope.ActiveBoostHubID == "" || !hubIDInScope(scope.ActiveBoostHubID, scope.HubIDs) {
		scope.ActiveBoostHubID = ""
	}
	return scope
}

func normalizeHubIDs(hubIDs []string) []string {
	if len(hubIDs) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(hubIDs))
	seen := make(map[string]struct{}, len(hubIDs))
	for _, hubID := range hubIDs {
		hubID = strings.TrimSpace(hubID)
		if hubID == "" {
			continue
		}
		if _, ok := seen[hubID]; ok {
			continue
		}
		seen[hubID] = struct{}{}
		normalized = append(normalized, hubID)
	}
	sort.Strings(normalized)
	return normalized
}

func hubIDInScope(hubID string, hubIDs []string) bool {
	if hubID == "" {
		return false
	}
	for _, allowed := range hubIDs {
		if hubID == allowed {
			return true
		}
	}
	return false
}

// stripFrozenHubsFromScope removes any hub ids that are currently
// frozen (inactive subscription or grace-period expired) from the
// recall scope. Used on the read path so frozen hubs' memories are
// excluded from recall / ask results for all members. Browse paths
// intentionally don't use this — they render frozen hubs with a
// visual indicator instead.
//
// Best-effort: a store error logs and returns the scope unchanged
// rather than silently dropping access. An outage on the
// subscriptions table shouldn't take down recall.
func stripFrozenHubsFromScope(ctx context.Context, s store.Store, scope RecallScope) RecallScope {
	if s == nil || len(scope.HubIDs) == 0 {
		return scope
	}
	cutoff := time.Now().UTC().Add(-model.HubGracePeriod)
	frozen, err := s.ListFrozenHubIDs(ctx, scope.HubIDs, cutoff)
	if err != nil {
		slog.Warn("list frozen hubs failed; including all hubs in scope",
			"error", err, "hub_count", len(scope.HubIDs))
		return scope
	}
	if len(frozen) == 0 {
		return scope
	}
	frozenSet := make(map[string]struct{}, len(frozen))
	for _, id := range frozen {
		frozenSet[id] = struct{}{}
	}
	kept := make([]string, 0, len(scope.HubIDs))
	for _, id := range scope.HubIDs {
		if _, isFrozen := frozenSet[id]; isFrozen {
			continue
		}
		kept = append(kept, id)
	}
	scope.HubIDs = kept
	if _, frozenBoost := frozenSet[scope.ActiveBoostHubID]; frozenBoost {
		scope.ActiveBoostHubID = ""
	}
	return scope
}
