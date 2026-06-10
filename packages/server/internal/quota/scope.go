package quota

import (
	"fmt"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// HubLookup is the focused interface SelectScope depends on. Lets
// callers stub it cheaply in tests without standing up a full Store.
type HubLookup interface {
	GetHub(id string) (*model.Hub, error)
}

// SelectScope returns the right Scope for a dream trigger given the
// hub context.
//
// Rules (load-bearing — match docs/plans/23-dream-tiers.md "Strict
// scoping"):
//
//   - hubID empty + userID set: ScopeUser. Used by API-key callers
//     with no hub context. The user's max(personal, best_hub) is
//     baked into the resolver's ResolveDreamLimits.
//   - hubID set, hub.HubType == "personal": ScopeUser, with the
//     hub's owner. Personal-hub triggers count against the user's
//     quota even though they happen "in a hub" — there's only ever
//     one user per personal hub.
//   - hubID set, hub.HubType == "team" (or anything non-personal):
//     ScopeHub. The hub's fixed-per-hub quota is the only one that
//     applies; the user's personal pool is irrelevant for team-hub
//     content scans.
//   - both hubID and userID empty: error. We can't pick a counter.
//
// triggered_by (in dream_runs) is for actor audit, not scope
// selection — never let the caller flip the scope based on who
// pressed the button. The hub's hub_type alone determines it.
func SelectScope(hubLookup HubLookup, hubID, userID string) (Scope, error) {
	if hubID == "" {
		if userID == "" {
			return Scope{}, fmt.Errorf("quota: SelectScope: neither hub_id nor user_id provided")
		}
		return Scope{Type: ScopeUser, UserID: userID}, nil
	}

	if hubLookup == nil {
		return Scope{}, fmt.Errorf("quota: SelectScope: hub lookup not configured")
	}

	hub, err := hubLookup.GetHub(hubID)
	if err != nil {
		return Scope{}, fmt.Errorf("quota: SelectScope: hub lookup: %w", err)
	}
	if hub == nil {
		return Scope{}, fmt.Errorf("quota: SelectScope: hub %q not found", hubID)
	}

	if hub.HubType == "personal" {
		// Personal hubs charge the owner's personal quota. We
		// intentionally use hub.OwnerID rather than the caller's
		// userID so a third party with manual-trigger permission
		// (e.g., a co-owner via future role expansion) charges the
		// hub owner, not themselves. Today only the owner can
		// trigger their personal hub anyway, but pinning the rule
		// at scope-selection time avoids the ambiguity.
		return Scope{Type: ScopeUser, UserID: hub.OwnerID}, nil
	}

	// Team / shared / anything-else hubs use the hub's fixed pool.
	return Scope{Type: ScopeHub, HubID: hub.ID}, nil
}
