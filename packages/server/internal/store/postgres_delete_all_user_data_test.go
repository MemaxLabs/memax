package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// TestPostgresDeleteAllUserData runs the account-wipe path against the
// real schema. Every statement in DeleteAllUserData names a table, so
// a table dropped by a migration (as `reviews` was in 025) fails here
// instead of in production. It also pins owner isolation: another
// user's memories survive the wipe.
func TestPostgresDeleteAllUserData(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	owner := seedUser(t, pool, "wipe-owner")
	other := seedUser(t, pool, "wipe-other")
	ownerHub := uuid.NewString()
	otherHub := uuid.NewString()
	for _, h := range []struct{ id, owner string }{{ownerHub, owner}, {otherHub, other}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1::uuid, 'Personal', $1, 'personal', $2::uuid)`,
			h.id, h.owner,
		); err != nil {
			t.Fatalf("seed hub: %v", err)
		}
	}
	seedMemory(t, pool, owner, ownerHub, "active", time.Now(), "mine")
	keep := seedMemory(t, pool, other, otherHub, "active", time.Now(), "theirs")

	if err := s.DeleteAllUserData(owner); err != nil {
		t.Fatalf("DeleteAllUserData: %v", err)
	}

	var mine, theirs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memories WHERE owner_id = $1::uuid`, owner).Scan(&mine); err != nil {
		t.Fatalf("count mine: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM memories WHERE id = $1::uuid`, keep).Scan(&theirs); err != nil {
		t.Fatalf("count theirs: %v", err)
	}
	if mine != 0 {
		t.Errorf("owner memories after wipe = %d, want 0", mine)
	}
	if theirs != 1 {
		t.Errorf("other user's memory was removed by the wipe")
	}
}
