// Package agent is the shared autonomous-agent runtime that powers
// both Lucid Dream (the contradict + organize phases for paid users)
// and Agent Chat (the user-facing claude.ai-style surface).
//
// See docs/plans/24-agent-runtime-lucid-and-chat.md for the full
// design. This file implements the SessionScope primitive: the single
// source of truth for who a running agent acts on behalf of, and
// which hubs it's allowed to read or mutate.
//
// Both agent profiles consume this primitive identically. Dream
// always uses ScopeTypeSingleHub. Chat picks among all three at
// session-create time. The primitive's job is to keep tool wrappers
// honest: they call Resolve once per agent invocation, get back a
// fully-checked HubIDs list, and use it to construct the
// store.VisibilityScope they pass into the existing memax store
// methods. No tool wrapper is allowed to skip Resolve — the CI lint
// at scripts/check-tool-store-calls.go enforces this.
package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ScopeType identifies the agent's operating boundary at the user level.
// See docs/plans/24-agent-runtime-lucid-and-chat.md "Session scope".
type ScopeType string

const (
	// ScopeTypeUserAllHubs — agent operates across every hub the user
	// is currently a member of. Membership is resolved fresh at every
	// agent invocation, NOT snapshotted at session-create. Joining a
	// new hub immediately exposes it; leaving a hub immediately
	// removes it. Chat-only.
	ScopeTypeUserAllHubs ScopeType = "user_all_hubs"

	// ScopeTypeSingleHub — agent operates against exactly one hub. The
	// dream agent always uses this scope; chat sessions pick it when
	// the user wants focused single-hub work.
	ScopeTypeSingleHub ScopeType = "single_hub"

	// ScopeTypeHubSubset — agent operates across a chosen subset of
	// the user's hubs. The list is pinned at session-create but each
	// invocation intersects it with current membership (a hub the
	// user has since left drops from the resolved set automatically).
	ScopeTypeHubSubset ScopeType = "hub_subset"
)

// Valid reports whether t is a known ScopeType. The value is also
// enforced by a Postgres CHECK constraint on chat_sessions.scope_type
// and by the SessionDescriptor validator in this package — that
// belt-and-suspenders combo means a corrupted row can never silently
// produce wrong scope behavior at runtime.
func (t ScopeType) Valid() bool {
	switch t {
	case ScopeTypeUserAllHubs, ScopeTypeSingleHub, ScopeTypeHubSubset:
		return true
	}
	return false
}

// SessionDescriptor is the minimal session shape the resolver needs
// to compute a SessionScope for a given invocation. Both dream and
// chat sessions provide this — dreams synthesize it inline from the
// dream-run context (always ScopeTypeSingleHub); chat reads it from
// the chat_sessions row.
//
// The descriptor is intentionally separate from the full chat_session
// model so the resolver doesn't take a dependency on chat-specific
// types. Anything that can hand the resolver these four fields can
// resolve a scope.
type SessionDescriptor struct {
	// OwnerID is the user the agent acts on behalf of. Never empty.
	OwnerID string

	// Type controls how HubIDs are computed. See ScopeType constants.
	Type ScopeType

	// HubIDs is the session's pinned hub list. Empty when
	// Type == ScopeTypeUserAllHubs. Length 1 when ScopeTypeSingleHub.
	// Length >= 1 when ScopeTypeHubSubset.
	HubIDs []string

	// WriteHubID is the optional default-write target. Tools that
	// write may use this when args.HubID is omitted. Always
	// re-validated against the resolved scope at call time, never
	// trusted blindly. Zero value = no default.
	WriteHubID string
}

// Validate checks the descriptor's invariants. Tool wrappers don't
// call this directly — they call Resolve, which calls Validate as
// step 1. Exposed for testing and for the chat handler's create
// validation path.
func (d SessionDescriptor) Validate() error {
	if d.OwnerID == "" {
		return errors.New("agent: SessionDescriptor.OwnerID is required")
	}
	if !d.Type.Valid() {
		return fmt.Errorf("agent: invalid SessionDescriptor.Type %q", d.Type)
	}
	switch d.Type {
	case ScopeTypeUserAllHubs:
		if len(d.HubIDs) != 0 {
			return fmt.Errorf("agent: ScopeTypeUserAllHubs must have empty HubIDs, got %d", len(d.HubIDs))
		}
	case ScopeTypeSingleHub:
		if len(d.HubIDs) != 1 {
			return fmt.Errorf("agent: ScopeTypeSingleHub must have exactly 1 HubID, got %d", len(d.HubIDs))
		}
	case ScopeTypeHubSubset:
		if len(d.HubIDs) == 0 {
			return errors.New("agent: ScopeTypeHubSubset must have at least 1 HubID")
		}
	}
	seen := make(map[string]struct{}, len(d.HubIDs))
	for i, h := range d.HubIDs {
		if h == "" {
			return fmt.Errorf("agent: SessionDescriptor.HubIDs[%d] is empty", i)
		}
		if _, dup := seen[h]; dup {
			// Duplicate-rejection in Validate prevents the resolver
			// from emitting duplicate hubs in scope.HubIDs (which
			// would inflate fan-out tool calls and confuse audit
			// logs). The chat-create handler ALSO dedupes before
			// insert per the schema-comment, so a duplicate here
			// indicates a programmer bug — fail fast at the boundary.
			return fmt.Errorf("agent: SessionDescriptor.HubIDs has duplicate %q at index %d", h, i)
		}
		seen[h] = struct{}{}
	}
	if d.WriteHubID != "" && d.Type != ScopeTypeUserAllHubs && !slices.Contains(d.HubIDs, d.WriteHubID) {
		return fmt.Errorf("agent: WriteHubID %q not in HubIDs", d.WriteHubID)
	}
	return nil
}

// SessionScope is the resolved, runtime-authoritative answer to
// "who is the agent acting for, and which hubs is it allowed to
// touch right now?" Tool wrappers receive this via the resolver and
// MUST NOT mutate it.
//
// The shape mirrors SessionDescriptor but with HubIDs guaranteed to
// be the *current* allowed set after intersecting with live hub
// membership. ALL three scope types (UserAllHubs, SingleHub,
// HubSubset) go through that intersection — see Resolve for why.
//
// **HubIDs vs WritableHubIDs.** The two slices answer different
// questions:
//
//   - HubIDs: hubs the agent can READ from (any role, including
//     viewer). Tools like recall_memories, list_memories, get_memory
//     pass this set into store.VisibilityScope.HubIDs.
//   - WritableHubIDs: hubs the agent can MUTATE — a strict subset
//     of HubIDs filtered to roles that grant write capability
//     (owner / admin / contributor; viewers excluded). Mutating
//     tools (push_memory, forget_memory, propose_topic_merge) check
//     this set via IsHubWritable before hitting their service.
//
// Phase 3.7 step 1 added the WritableHubIDs split. Before, mutating
// tools relied on a per-call GetHubMemberRole lookup at the service
// layer — the writable-set is a wrapper-level fast-fail layer ON
// TOP of that, not a replacement for it (membership can change
// mid-session; the per-call check stays as the freshness layer).
//
// **Empty HubIDs is meaningful.** A user who created a single_hub
// session and then got removed from that hub resolves to an empty
// HubIDs slice; downstream tool wrappers MUST treat that as "no
// authorized hub targets" and reject the call. Don't pass empty
// HubIDs straight into store.VisibilityScope: that path falls
// back to owner_id reads, which is the wrong fail-open behavior.
type SessionScope struct {
	OwnerID        string
	Type           ScopeType
	HubIDs         []string
	WritableHubIDs []string
	WriteHubID     string
}

// IsHubAllowed reports whether the given hub is in this scope's
// READ-allowed set. Tool wrappers use this to validate a caller-
// supplied hub_id arg before constructing the
// store.VisibilityScope. The check is exact — empty hubID is
// never allowed.
func (s SessionScope) IsHubAllowed(hubID string) bool {
	if hubID == "" {
		return false
	}
	return slices.Contains(s.HubIDs, hubID)
}

// IsHubWritable reports whether the given hub is in this scope's
// WRITE-eligible set — the subset of HubIDs where the user has a
// non-viewer role (owner / admin / contributor) at the moment the
// scope was resolved.
//
// Mutating tool wrappers call this BEFORE invoking their service
// to fast-fail when the agent picked a hub the user can only read
// from. Per-tool refinements (forget's contributor-policy check,
// propose's AllowContributorTopics check, push's hub-quota check)
// stay in the per-tool service code; this gate is the prerequisite
// the wrapper enforces.
//
// **Defense-in-depth note.** Membership can change mid-session
// (an owner can demote a contributor to viewer between
// scope-resolve and the tool call). The fast-fail here doesn't
// replace the service-side per-call GetHubMemberRole lookup —
// that check is the freshness layer; this one is the speed layer.
func (s SessionScope) IsHubWritable(hubID string) bool {
	if hubID == "" {
		return false
	}
	return slices.Contains(s.WritableHubIDs, hubID)
}

// MembershipReader is the dependency the resolver needs from the
// store layer to compute the live membership set for a user.
// Defined here as a focused interface so the agent package doesn't
// import the full store interface (and tests can supply a small
// fake).
//
// Implementations return (hub_id, role) pairs for every hub the
// user is currently a member of. Ordering doesn't matter to the
// resolver, but the slice MUST NOT contain duplicate hub_id
// values or empty strings. The Postgres implementation enforces
// this naturally because hub_members has a unique key on
// (user_id, hub_id).
//
// Phase 3.7 step 1 widened the interface from IDs-only to
// (hub_id, role) so the resolver can compute SessionScope's
// WritableHubIDs subset (hubs where role grants write capability)
// in the same query path it already used for HubIDs.
type MembershipReader interface {
	HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error)
}

// ScopeResolver computes a SessionScope for an invocation.
//
// Concurrency: ScopeResolver is safe for concurrent use. The
// underlying MembershipReader is expected to be the production
// store, which is itself goroutine-safe.
//
// Error semantics: Resolve returns the descriptor's validation error
// if the descriptor is malformed (programmer error in the caller),
// or a wrapped error from MembershipReader for transient lookup
// failures. The caller decides whether to retry (chat: surface as
// 5xx) or abort (dreams: fail the run).
type ScopeResolver struct {
	members MembershipReader
}

// NewScopeResolver constructs a resolver. members must not be nil.
func NewScopeResolver(members MembershipReader) *ScopeResolver {
	if members == nil {
		panic("agent: NewScopeResolver requires non-nil MembershipReader")
	}
	return &ScopeResolver{members: members}
}

// Resolve returns the runtime-authoritative SessionScope for an
// invocation.
//
// **All three scope types intersect against live membership.** Even
// ScopeTypeSingleHub — chat sessions can pick single_hub, and a
// chat session created weeks ago for hub A must NOT keep returning
// hub A if the user has since been removed from it. The intersection
// is cheap (one indexed query on hub_members) and unconditional.
//
// The dream engine, which always uses ScopeTypeSingleHub, separately
// validates hub access at run-start (freeze state, plan, etc.). That
// check guards the dream-cycle decision to invoke the agent at all;
// the resolver's intersection is a defense-in-depth check that
// caught a real correctness bug (a user kicked from a hub mid-cycle
// would otherwise see the hub stay live for the agent's tool calls).
// The cost is negligible vs. LLM latency.
//
// All branches preserve descriptor order across the intersection so
// debugging stays deterministic — a user who created a hub_subset
// [A, B, C] and got booted from B always sees the resolved scope as
// [A, C], never [C, A].
func (r *ScopeResolver) Resolve(ctx context.Context, d SessionDescriptor) (SessionScope, error) {
	if err := d.Validate(); err != nil {
		return SessionScope{}, err
	}

	live, err := r.members.HubMembershipsForUser(ctx, d.OwnerID)
	if err != nil {
		return SessionScope{}, fmt.Errorf("agent: scope resolve: %w", err)
	}

	// Build a (hub_id → role) map once; both branches below use it
	// (UserAllHubs to enumerate ALL live memberships and pull roles;
	// SingleHub/HubSubset to intersect-by-id and pull the matching
	// role for each kept entry).
	liveRoles := make(map[string]string, len(live))
	for _, m := range live {
		liveRoles[m.HubID] = m.Role
	}

	// resolvedRoles preserves the resolved order — same shape as
	// resolvedHubIDs but with the role alongside. We derive both
	// HubIDs and WritableHubIDs from this slice in a single pass
	// below so the order invariant ("descriptor order preserved
	// across the intersection") holds for both.
	var resolvedRoles []model.HubMembership
	switch d.Type {
	case ScopeTypeUserAllHubs:
		// The agent operates over the user's entire current
		// membership. Copy so the caller can't mutate our
		// internal slice.
		resolvedRoles = append([]model.HubMembership(nil), live...)

	case ScopeTypeSingleHub, ScopeTypeHubSubset:
		// Intersect descriptor's pinned hubs against current
		// membership. Preserve descriptor order; pick up role
		// from the live map.
		resolvedRoles = make([]model.HubMembership, 0, len(d.HubIDs))
		for _, h := range d.HubIDs {
			if role, ok := liveRoles[h]; ok {
				resolvedRoles = append(resolvedRoles, model.HubMembership{HubID: h, Role: role})
			}
		}

	default:
		// Unreachable: Validate already rejected unknown types.
		return SessionScope{}, fmt.Errorf("agent: unreachable scope type %q", d.Type)
	}

	// Single-pass derivation of HubIDs + WritableHubIDs from the
	// resolved (hub, role) list. WritableHubIDs is a strict subset
	// — hubs where the user has owner/admin/contributor role.
	// model.IsWritableHubRole is the canonical predicate.
	resolvedHubIDs := make([]string, len(resolvedRoles))
	resolvedWritable := make([]string, 0, len(resolvedRoles))
	for i, m := range resolvedRoles {
		resolvedHubIDs[i] = m.HubID
		if model.IsWritableHubRole(m.Role) {
			resolvedWritable = append(resolvedWritable, m.HubID)
		}
	}

	return SessionScope{
		OwnerID:        d.OwnerID,
		Type:           d.Type,
		HubIDs:         resolvedHubIDs,
		WritableHubIDs: resolvedWritable,
		// WriteHubID is re-validated per-call by the tool wrapper
		// against scope.HubIDs — no need to filter here.
		WriteHubID: d.WriteHubID,
	}, nil
}
