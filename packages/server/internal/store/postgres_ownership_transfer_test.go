package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestOwnershipTransfer_MissingSubscription_FailsClosed verifies that
// AcceptHubOwnershipTransfer fails when the hub has no active
// hub_subscriptions row, rather than silently skipping the ownership cap.
// This protects the invariant that every team hub has an authoritative
// subscription row.
//
// Skipped without DATABASE_URL.
func TestOwnershipTransfer_MissingSubscription_FailsClosed(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres ownership transfer test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	// Setup: create two users, a team hub owned by user A, and a pending
	// transfer to user B. Crucially, do NOT create a hub_subscriptions row.
	ownerID := "00000000-0000-0000-0000-0000000f0a01"
	targetID := "00000000-0000-0000-0000-0000000f0b01"
	hubID := "00000000-0000-0000-0000-0000000f0c01"
	transferID := "00000000-0000-0000-0000-0000000f0d01"

	// Cleanup from previous runs
	pool.Exec(ctx, `DELETE FROM hub_ownership_transfers WHERE hub_id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM hub_subscriptions WHERE hub_id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, targetID)

	now := time.Now()

	// Insert users
	for _, uid := range []string{ownerID, targetID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
			 VALUES ($1, $2, $3, 'free', 'personal_free', true, $4, $4)`,
			uid, uid+"@test.local", "test-"+uid[:8], now,
		); err != nil {
			t.Fatalf("insert user %s: %v", uid[:8], err)
		}
	}

	// Insert team hub WITHOUT subscription (the scenario we're testing)
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, plan, owner_id, created_at, updated_at)
		 VALUES ($1, 'Transfer Test', 'transfer-test', 'team', 'hub_free_team', $2, $3, $3)`,
		hubID, ownerID, now,
	); err != nil {
		t.Fatalf("insert hub: %v", err)
	}

	// Add both users as members
	for _, uid := range []string{ownerID, targetID} {
		role := "contributor"
		if uid == ownerID {
			role = "owner"
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, $3)`,
			hubID, uid, role,
		); err != nil {
			t.Fatalf("insert member: %v", err)
		}
	}

	// Create a pending transfer
	if _, err := pool.Exec(ctx,
		`INSERT INTO hub_ownership_transfers (id, hub_id, initiated_by, target_user_id, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		transferID, hubID, ownerID, targetID, now.Add(24*time.Hour), now,
	); err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	// ACT: Attempt the transfer with a maxFreeTeamHubs cap of 1.
	// The store should fail because the hub has no active subscription.
	_, err = s.AcceptHubOwnershipTransfer(hubID, transferID, targetID, 1)

	// ASSERT: Must fail — not succeed silently
	if err == nil {
		t.Fatal("expected error when hub has no active subscription, but transfer succeeded")
	}
	if !strings.Contains(err.Error(), "no active subscription") {
		t.Fatalf("expected 'no active subscription' error, got: %v", err)
	}

	// Verify ownership did NOT change
	var currentOwner string
	if err := pool.QueryRow(ctx,
		`SELECT owner_id FROM hubs WHERE id = $1`, hubID,
	).Scan(&currentOwner); err != nil {
		t.Fatalf("query owner: %v", err)
	}
	if currentOwner != ownerID {
		t.Errorf("ownership should not have changed; expected %s, got %s", ownerID, currentOwner)
	}

	// Verify transfer is NOT marked as accepted
	var isAccepted bool
	if err := pool.QueryRow(ctx,
		`SELECT accepted_at IS NOT NULL FROM hub_ownership_transfers WHERE id = $1`, transferID,
	).Scan(&isAccepted); err != nil {
		t.Fatalf("query transfer: %v", err)
	}
	if isAccepted {
		t.Error("transfer should not be marked as accepted")
	}

	// Cleanup
	pool.Exec(ctx, `DELETE FROM hub_ownership_transfers WHERE hub_id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1`, hubID)
	pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, targetID)
}
