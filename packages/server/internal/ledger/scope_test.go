package ledger_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func TestResolveUserScope(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	outsider := f.user("out")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	team := f.space(jy, policy.SpaceTeam, "MemaxLabs")
	f.join(team, zz, "admin")
	viewing := f.space(jy, policy.SpaceTeam, "Docs")
	f.join(viewing, zz, "viewer")
	contrib := f.space(jy, policy.SpaceTeam, "Ops")
	f.join(contrib, zz, "contributor")
	// An owner whose membership row went missing is still the owner.
	orphan := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, 'Orphan', $3, 'team', $2)`, orphan, zz, orphan.String())

	scope := f.scope(zz)
	want := map[uuid.UUID]ledger.SpaceGrant{
		personal: {SpaceID: personal, TenantID: zz, Kind: policy.SpacePersonal, Role: policy.RoleOwner, CanForget: true},
		project:  {SpaceID: project, TenantID: zz, Kind: policy.SpaceProject, Role: policy.RoleOwner, CanForget: true},
		team:     {SpaceID: team, TenantID: team, Kind: policy.SpaceTeam, Role: policy.RoleMember, CanForget: true},
		viewing:  {SpaceID: viewing, TenantID: viewing, Kind: policy.SpaceTeam, Role: policy.RoleViewer},
		contrib:  {SpaceID: contrib, TenantID: contrib, Kind: policy.SpaceTeam, Role: policy.RoleMember},
		orphan:   {SpaceID: orphan, TenantID: orphan, Kind: policy.SpaceTeam, Role: policy.RoleOwner, CanForget: true},
	}
	if len(scope.Spaces) != len(want) {
		t.Fatalf("scope has %d spaces, want %d: %+v", len(scope.Spaces), len(want), scope.Spaces)
	}
	for id, w := range want {
		g, ok := scope.Grant(id)
		if !ok || g != w {
			t.Errorf("grant for %s = %+v (%v), want %+v", id, g, ok, w)
		}
	}
	if got := len(scope.TenantIDs()); got != 5 {
		t.Errorf("tenants = %d, want 5 (zz + four team spaces)", got)
	}
	if n := scope.Narrow(team, uuid.New()); len(n.Spaces) != 1 || n.Spaces[0].SpaceID != team {
		t.Errorf("Narrow = %+v", n)
	}
	empty, err := ledger.ResolveUserScope(context.Background(), f.pool, outsider)
	if err != nil || len(empty.Spaces) != 0 {
		t.Errorf("a user with no spaces: %+v %v", empty, err)
	}
}

// Migration 027: spaces live on V1's hubs table, with a tenant fixed
// at creation.
func TestSpaceColumnsOnHubs(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	jy := f.user("jy")

	read := func(id uuid.UUID) (kind string, tenant uuid.UUID, rules string) {
		t.Helper()
		if err := f.pool.QueryRow(ctx, `SELECT space_kind, tenant_id, rules::text FROM hubs WHERE id = $1`, id).Scan(&kind, &tenant, &rules); err != nil {
			t.Fatalf("read hub: %v", err)
		}
		return
	}
	// V1 inserts don't know the new columns: the trigger fills them.
	personal, team := uuid.New(), uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, 'P', $3, 'personal', $2)`, personal, zz, personal.String())
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, 'T', $3, 'team', $2)`, team, zz, team.String())
	if k, ten, r := read(personal); k != "personal" || ten != zz || r != "{}" {
		t.Errorf("personal hub = %s %s %s", k, ten, r)
	}
	if k, ten, _ := read(team); k != "team" || ten != team {
		t.Errorf("team hub = %s %s", k, ten)
	}
	// A project space is a 'team' hub to V1, owned by one person's tenant;
	// callers can't pick a tenant.
	project := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, tenant_id) VALUES ($1, 'Pr', $4, 'team', $2, 'project', $3)`, project, zz, jy, project.String())
	if k, ten, _ := read(project); k != "project" || ten != zz {
		t.Errorf("project hub = %s %s, want project owned by zz's tenant", k, ten)
	}

	refused := []struct{ name, sql string }{
		{"tenant change", `UPDATE hubs SET tenant_id = gen_random_uuid() WHERE id = '` + project.String() + `'`},
		{"kind change", `UPDATE hubs SET space_kind = 'team' WHERE id = '` + project.String() + `'`},
		{"personal hub as a team space", `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES (gen_random_uuid(), 'X', 'x-1', 'personal', '` + zz.String() + `', 'team')`},
		{"unknown kind", `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES (gen_random_uuid(), 'X', 'x-2', 'team', '` + zz.String() + `', 'org')`},
		{"rules must be an object", `UPDATE hubs SET rules = '[]' WHERE id = '` + team.String() + `'`},
	}
	for _, c := range refused {
		if _, err := f.pool.Exec(ctx, c.sql); sqlstate(err) != "23514" {
			t.Errorf("%s: %v, want a check violation", c.name, err)
		}
	}
	// An ownership transfer keeps the tenant (and so every M- number).
	f.exec(`UPDATE hubs SET owner_id = $2 WHERE id = $1`, project, jy)
	if _, ten, _ := read(project); ten != zz {
		t.Errorf("tenant after transfer = %s, want the original %s", ten, zz)
	}
	// V1 updates that don't touch the space columns are unaffected.
	f.exec(`UPDATE hubs SET name = 'Renamed', settings = '{"x":1}' WHERE id = $1`, team)
}

// Space rules from hubs.rules reach the policy: an owners-keep space
// sends a member's Remember to Review, and unreadable rules fail closed.
func TestSpaceRulesReachPolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, jy, "contributor")
	f.setRules(space, `{"keep": "owners"}`)

	res := f.apply(&ledger.Remember{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), NewMemory: fact(space, "Members propose here.")})
	if res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeOwnersKeep {
		t.Errorf("member in an owners-keep space: %s/%s", res.Outcome, res.Policy.Code)
	}
	f.setRules(space, `{"keep": 5}`)
	if _, err := f.l.Apply(context.Background(), &ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: fact(space, "x")}); err == nil {
		t.Error("unreadable rules should fail the command, not fall back to defaults")
	}
}
