package ledger

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Scope is the set of spaces an actor may touch, with the actor's role
// in each. Every ledger transaction sets app.space_ids and
// app.tenant_ids from it, so row-level security hides everything else.
//
// The caller resolves it from the request's identity (ResolveUserScope
// for a person and the agents working for them) and may narrow it (an
// API key bound to one space, an agent connected to some spaces).
type Scope struct {
	Spaces []SpaceGrant
	// PersonID is the person the actor is, or works for. Every ledger
	// transaction sets app.person_id from it: a person's agent
	// connections are visible to them in any space, and other people's
	// only through the spaces in the scope. ResolveUserScope sets it.
	PersonID uuid.UUID
}

// SpaceGrant is one space in a scope.
type SpaceGrant struct {
	SpaceID  uuid.UUID
	TenantID uuid.UUID
	Kind     policy.SpaceKind
	// Role is the person's role; RoleNone for system actors (Dream,
	// Memax, the repository), which act on a space without membership.
	Role policy.Role
	// CanForget carries V1's admin role (member + can_forget).
	CanForget bool
	// Autonomy and AgentStatus are an agent's level in this space and
	// whether it is connected here, from its connection (WithConnection).
	// Both are empty for people; an empty Autonomy falls back to
	// Actor.Autonomy.
	Autonomy    policy.Autonomy
	AgentStatus policy.AgentStatus
	// Slug and Name are the space's, as ResolveUserScope and SpaceScope
	// read them with the grant: /v2 resolves a slug, and Ask names the
	// space, without another round trip. Empty in a scope built otherwise.
	Slug string
	Name string
}

// Grant returns the grant for a space, if the scope includes it.
func (s Scope) Grant(spaceID uuid.UUID) (SpaceGrant, bool) {
	for _, g := range s.Spaces {
		if g.SpaceID == spaceID {
			return g, true
		}
	}
	return SpaceGrant{}, false
}

// SpaceIDs lists the scope's spaces.
func (s Scope) SpaceIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(s.Spaces))
	for _, g := range s.Spaces {
		if !slices.Contains(out, g.SpaceID) {
			out = append(out, g.SpaceID)
		}
	}
	return out
}

// TenantIDs lists the tenants of the scope's spaces.
func (s Scope) TenantIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(s.Spaces))
	for _, g := range s.Spaces {
		if !slices.Contains(out, g.TenantID) {
			out = append(out, g.TenantID)
		}
	}
	return out
}

// Narrow keeps only the given spaces (and the person).
func (s Scope) Narrow(spaceIDs ...uuid.UUID) Scope {
	out := Scope{PersonID: s.PersonID}
	for _, g := range s.Spaces {
		if slices.Contains(spaceIDs, g.SpaceID) {
			out.Spaces = append(out.Spaces, g)
		}
	}
	return out
}

// Querier is the read side of a pgx pool, connection or transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ResolveUserScope returns every space the user belongs to, with V1 hub
// roles mapped onto V2 roles (owner → owner; admin → member who can
// forget; contributor → member; viewer → viewer). The space's owner is
// always its owner, even if a hub_members row is missing.
//
// It reads V1's public.hubs and public.hub_members as the login role,
// before any ledger transaction starts: identity resolution is the trust
// root that scope is built from, so it can't itself run under that scope.
func ResolveUserScope(ctx context.Context, db Querier, userID uuid.UUID) (Scope, error) {
	rows, err := db.Query(ctx, `
		SELECT h.id, h.tenant_id, h.space_kind,
		       CASE WHEN h.owner_id = $1 THEN 'owner' ELSE m.role END, h.slug, h.name
		  FROM public.hubs h
		  LEFT JOIN public.hub_members m ON m.hub_id = h.id AND m.user_id = $1
		 WHERE h.owner_id = $1 OR m.user_id = $1
		 ORDER BY h.id`, userID)
	if err != nil {
		return Scope{}, fmt.Errorf("ledger: resolve scope: %w", err)
	}
	defer rows.Close()
	s := Scope{PersonID: userID}
	for rows.Next() {
		var g SpaceGrant
		var kind, v1Role string
		if err := rows.Scan(&g.SpaceID, &g.TenantID, &kind, &v1Role, &g.Slug, &g.Name); err != nil {
			return Scope{}, fmt.Errorf("ledger: resolve scope: %w", err)
		}
		g.Kind = policy.SpaceKind(kind)
		g.Role, g.CanForget = policy.RoleFromV1(v1Role)
		s.Spaces = append(s.Spaces, g)
	}
	if err := rows.Err(); err != nil {
		return Scope{}, fmt.Errorf("ledger: resolve scope: %w", err)
	}
	return s, nil
}

// uuidArray renders ids as a Postgres uuid[] literal for set_config.
func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}
