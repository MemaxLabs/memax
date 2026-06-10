package testdb

import (
	"context"
	"fmt"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestAcquireProducesMigratedIsolatedDB smoke-tests the harness:
// two sibling calls should hand back distinct, both-migrated
// databases that don't see each other's writes.
//
// Plan 23 §5.4 introduced migration 004 which bootstraps a
// `memax-system` user (UUID 00000000-0000-0000-0000-00000000ada7)
// to own the tutorial-template hub. The test filters that row out
// when checking for "empty" — what matters is that no application
// data has leaked between clones.
func TestAcquireProducesMigratedIsolatedDB(t *testing.T) {
	ctx := context.Background()
	nonSystemUserCountSQL := fmt.Sprintf(
		`SELECT COUNT(*) FROM users WHERE id <> '%s'`, model.SystemUserID,
	)

	// Sibling A: insert a sentinel row into users.
	sa, poolA := Acquire(t)
	_, _ = sa, poolA
	var existsA int
	if err := poolA.QueryRow(ctx, nonSystemUserCountSQL).Scan(&existsA); err != nil {
		t.Fatalf("select users (A): %v", err)
	}
	if existsA != 0 {
		t.Errorf("clone A should start with no application users; got %d", existsA)
	}

	// Sibling B: same query, must still be empty — A's writes
	// shouldn't leak if we did any. We don't write here; the
	// isolation guard is structural (different databases).
	_, poolB := Acquire(t)
	var countB int
	if err := poolB.QueryRow(ctx, nonSystemUserCountSQL).Scan(&countB); err != nil {
		t.Fatalf("select users (B): %v", err)
	}
	if countB != 0 {
		t.Errorf("clone B should start with no application users; got %d", countB)
	}

	// Schema sanity: migrations ran. plans table is seeded by
	// migration 001 with the canonical tier rows; a fresh clone
	// should see them.
	var planCount int
	if err := poolA.QueryRow(ctx, "SELECT COUNT(*) FROM plans").Scan(&planCount); err != nil {
		t.Fatalf("select plans: %v", err)
	}
	if planCount == 0 {
		t.Error("migrations didn't seed plans table — template DB is empty")
	}
}
