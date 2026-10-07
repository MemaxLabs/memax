package ledger_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The compile sweeper reads dirty targets across spaces as its own role
// (migration 040); memax_v2, which every request runs as, can't borrow the
// sweep's policy by setting app.sweep, nor call v2.dirty_targets.
func TestDirtyTargetsAreTheSweepersOnly(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	mine := f.space(zz, policy.SpaceProject, "mine")
	theirs := f.space(jy, policy.SpaceProject, "theirs")
	f.brief(zz, mine, 0, nil)
	f.brief(jy, theirs, 0, nil)
	t1 := f.target(zz, mine, ledger.TargetAgentsMD)
	t2 := f.target(jy, theirs, ledger.TargetAgentsMD)

	// The sweeper finds both, across spaces.
	refs, err := f.l.DirtyTargets(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, r.TargetID.String())
	}
	if !slices.Contains(got, t1.ID.String()) || !slices.Contains(got, t2.ID.String()) {
		t.Fatalf("the sweeper found %v, want both targets", got)
	}

	// memax_v2 with app.sweep set sees its own scope's targets, never
	// another space's, and can't call the sweep's function.
	for _, scope := range [][]string{nil, {mine.String()}} {
		var seen []string
		err := f.asV2(nil, nil, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', 'dirty_targets', true), set_config('app.space_ids', $1, true)`,
				"{"+strings.Join(scope, ",")+"}"); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT id::text FROM v2.targets`)
			if err != nil {
				return err
			}
			seen, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(seen, t2.ID.String()) || (scope == nil && len(seen) != 0) {
			t.Errorf("memax_v2 with app.sweep and scope %v sees %v", scope, seen)
		}
	}
	err = f.asV2(nil, nil, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT * FROM v2.dirty_targets(10)`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("memax_v2 called v2.dirty_targets: %v", err)
	}
}

// Every policy in schema v2 that admits rows on app.sweep (a setting any
// session can set) applies only to roles memax_v2 isn't: a dedicated
// sweeper role, or a SECURITY DEFINER function's owner. A new one written
// for every role fails here.
func TestSweepPoliciesAreRoleBound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rows, err := f.pool.Query(context.Background(), `
		SELECT p.tablename || '.' || p.policyname, r.role,
		       CASE WHEN r.role = 'public' THEN true ELSE pg_has_role('memax_v2', r.role, 'USAGE') END
		  FROM pg_policies p, unnest(p.roles) AS r(role)
		 WHERE p.schemaname = 'v2'
		   AND (COALESCE(p.qual, '') LIKE '%app.sweep%' OR COALESCE(p.with_check, '') LIKE '%app.sweep%')
		 ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	type bound struct {
		policy, role string
		open         bool
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (bound, error) {
		var b bound
		err := r.Scan(&b.policy, &b.role, &b.open)
		return b, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 8 {
		t.Fatalf("found %d sweep policies, want the 8 of migrations 038–040: %v", len(got), got)
	}
	for _, b := range got {
		if b.open {
			t.Errorf("%s applies to %s, which memax_v2 can act as: setting app.sweep would admit rows across spaces", b.policy, b.role)
		}
	}
}
