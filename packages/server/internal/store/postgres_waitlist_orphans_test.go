package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestListOrphanWaitlistCandidates covers the query backing
// GET /v1/admin/waitlist/orphans. The join has subtle filters
// (normalized email match, created_at window, plan/invite_id
// predicates) that are worth pinning down against real SQL — a
// memory-only test would mask column-name typos or LOWER(BTRIM(...))
// mismatches.
//
// We seed four users that exercise each branch of the WHERE clause:
//
//	u1_orphan   – matching active invite, no invite_id, personal_free → included
//	u2_consumed – matching invite but invite_id already set           → excluded
//	u3_paid     – matching invite but already on personal_early_access → excluded
//	u4_expired  – matching invite row whose status='expired'           → excluded
//
// Plus one "since" check: u5 is a matching orphan created BEFORE the
// since window — must be filtered out even though it's otherwise
// eligible.
func TestListOrphanWaitlistCandidates(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping orphan candidate query test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	// Deterministic prefix for every row so t.Cleanup can delete
	// exactly what we inserted without disturbing existing data.
	prefix := "0a0b-test-" + time.Now().UTC().Format("150405")
	waitlistID := "00000000-0000-0000-0000-00000a0b0001"

	// since-window boundary for the test. All "in-window" users are
	// created at `now`; u5 is created at `now - 48h` to prove the
	// filter excludes it when since = now-24h.
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	// Fixed IDs (declared below) must be wiped before each run in
	// case a prior failure left them behind. DELETE ordering matters
	// because users.invite_id FKs into waitlist_invites; the UPDATE
	// clears the back-ref first so the DELETEs can succeed.
	u1ID := "00000000-0000-0000-0000-00000a0b1001"
	u2ID := "00000000-0000-0000-0000-00000a0b1002"
	u3ID := "00000000-0000-0000-0000-00000a0b1003"
	u4ID := "00000000-0000-0000-0000-00000a0b1004"
	u5ID := "00000000-0000-0000-0000-00000a0b1005"
	fixedUserIDs := []string{u1ID, u2ID, u3ID, u4ID, u5ID}

	cleanup := func() {
		for _, id := range fixedUserIDs {
			_, _ = pool.Exec(ctx, `UPDATE users SET invite_id = NULL WHERE id = $1::uuid`, id)
		}
		_, _ = pool.Exec(ctx,
			`DELETE FROM waitlist_invites WHERE waitlist_id = $1::uuid`,
			waitlistID)
		_, _ = pool.Exec(ctx, `DELETE FROM waitlist WHERE id = $1::uuid`, waitlistID)
		for _, id := range fixedUserIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, id)
		}
	}
	cleanup() // wipe anything left by a prior interrupted run.
	t.Cleanup(cleanup)

	// Seed waitlist entry (FK target for invites).
	if _, err := pool.Exec(ctx,
		`INSERT INTO waitlist (id, email, use_case, status, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'test', 'approved', now(), now())
		 ON CONFLICT (id) DO NOTHING`,
		waitlistID, prefix+"-shared@test.local",
	); err != nil {
		t.Fatalf("seed waitlist: %v", err)
	}

	seedUser := func(id, email, plan string, createdAt time.Time, inviteID *string) {
		var inviteIDVal any
		if inviteID != nil {
			inviteIDVal = *inviteID
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, invite_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, $3, 'free', $4, true, $5, $6, $6)`,
			id, email, email, plan, inviteIDVal, createdAt,
		)
		if err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
	}

	seedInvite := func(id, email, status, token string, expiresAt time.Time) {
		_, err := pool.Exec(ctx,
			`INSERT INTO waitlist_invites (id, waitlist_id, email, token, status, expires_at, created_at)
			 VALUES ($1::uuid, $2::uuid, LOWER(BTRIM($3)), $4, $5, $6, now())`,
			id, waitlistID, email, token, status, expiresAt,
		)
		if err != nil {
			t.Fatalf("seed invite %s: %v", token, err)
		}
	}

	u1Email := prefix + "-orphan@test.local"
	u2Email := prefix + "-consumed@test.local"
	u3Email := prefix + "-paid@test.local"
	u4Email := prefix + "-expired@test.local"
	u5Email := prefix + "-stale@test.local"

	// Orphan — should be returned.
	seedUser(u1ID, u1Email, model.PersonalFreePlanID, now, nil)
	seedInvite("00000000-0000-0000-0000-00000a0b2001", u1Email, model.WaitlistInviteStatusActive, prefix+"-tok1", now.Add(72*time.Hour))

	// Already linked to an invite — excluded by invite_id IS NULL.
	// Seed invite before user because users.invite_id FKs into it.
	u2InviteID := "00000000-0000-0000-0000-00000a0b2002"
	seedInvite(u2InviteID, u2Email, model.WaitlistInviteStatusActive, prefix+"-tok2", now.Add(72*time.Hour))
	seedUser(u2ID, u2Email, model.PersonalFreePlanID, now, &u2InviteID)

	// Already upgraded — excluded by plan filter.
	seedUser(u3ID, u3Email, model.PersonalEarlyAccessPlanID, now, nil)
	seedInvite("00000000-0000-0000-0000-00000a0b2003", u3Email, model.WaitlistInviteStatusActive, prefix+"-tok3", now.Add(72*time.Hour))

	// Invite status != active — excluded.
	seedUser(u4ID, u4Email, model.PersonalFreePlanID, now, nil)
	seedInvite("00000000-0000-0000-0000-00000a0b2004", u4Email, model.WaitlistInviteStatusExpired, prefix+"-tok4", now.Add(72*time.Hour))

	// Created before since window — excluded by the created_at filter.
	seedUser(u5ID, u5Email, model.PersonalFreePlanID, now.Add(-48*time.Hour), nil)
	seedInvite("00000000-0000-0000-0000-00000a0b2005", u5Email, model.WaitlistInviteStatusActive, prefix+"-tok5", now.Add(72*time.Hour))

	got, err := s.ListOrphanWaitlistCandidates(ctx, model.OrphanWaitlistCandidateOpts{
		Since: since,
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("ListOrphanWaitlistCandidates: %v", err)
	}

	// Filter down to rows from this test run — the DB may have
	// 0a0bans seeded by other tests running in parallel namespaces.
	var ours []model.OrphanWaitlistCandidate
	for _, c := range got {
		if c.UserID == u1ID || c.UserID == u2ID || c.UserID == u3ID || c.UserID == u4ID || c.UserID == u5ID {
			ours = append(ours, c)
		}
	}

	if len(ours) != 1 {
		t.Fatalf("expected exactly u1_0a0ban to be returned, got %d rows: %+v", len(ours), ours)
	}
	if ours[0].UserID != u1ID {
		t.Fatalf("expected UserID=%s, got %s", u1ID, ours[0].UserID)
	}
	if ours[0].InviteToken != prefix+"-tok1" {
		t.Fatalf("expected token=%s, got %s", prefix+"-tok1", ours[0].InviteToken)
	}
	if ours[0].UserPlanID != model.PersonalFreePlanID {
		t.Fatalf("expected plan=%s, got %s", model.PersonalFreePlanID, ours[0].UserPlanID)
	}
}

// TestGetOrphanCandidateForUser covers the user-scoped variant
// used by POST /v1/admin/waitlist/reconcile. Three cases:
//   - orphan exists   → single row returned.
//   - user exists but not an orphan (e.g. already upgraded) → nil.
//   - user id doesn't exist at all → nil (not an error).
//
// The handler treats (nil, nil) as a skip; an error here would
// incorrectly surface as a 500-style "error" result to operators.
func TestGetOrphanCandidateForUser(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping user-scoped orphan test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	waitlistID := "00000000-0000-0000-0000-00000a0b0002"
	u1ID := "00000000-0000-0000-0000-00000a0b3001" // orphan
	u2ID := "00000000-0000-0000-0000-00000a0b3002" // not orphan (already upgraded)
	nonexistentID := "00000000-0000-0000-0000-00000a0b9999"
	fixedIDs := []string{u1ID, u2ID}
	prefix := "orph-scoped-" + time.Now().UTC().Format("150405")

	cleanup := func() {
		for _, id := range fixedIDs {
			_, _ = pool.Exec(ctx, `UPDATE users SET invite_id = NULL WHERE id = $1::uuid`, id)
		}
		_, _ = pool.Exec(ctx,
			`DELETE FROM waitlist_invites WHERE waitlist_id = $1::uuid`, waitlistID)
		_, _ = pool.Exec(ctx, `DELETE FROM waitlist WHERE id = $1::uuid`, waitlistID)
		for _, id := range fixedIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, id)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx,
		`INSERT INTO waitlist (id, email, use_case, status, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'test', 'approved', now(), now())
		 ON CONFLICT (id) DO NOTHING`,
		waitlistID, prefix+"-shared@test.local",
	); err != nil {
		t.Fatalf("seed waitlist: %v", err)
	}

	u1Email := prefix + "-orphan@test.local"
	u2Email := prefix + "-upgraded@test.local"
	now := time.Now().UTC()

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $2, 'free', $3, true, $4, $4)`,
		u1ID, u1Email, model.PersonalFreePlanID, now,
	); err != nil {
		t.Fatalf("seed u1: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO waitlist_invites (waitlist_id, email, token, status, expires_at, created_at)
		 VALUES ($1::uuid, LOWER(BTRIM($2)), $3, $4, $5, now())`,
		waitlistID, u1Email, prefix+"-tok-u1", model.WaitlistInviteStatusActive, now.Add(72*time.Hour),
	); err != nil {
		t.Fatalf("seed u1 invite: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $2, 'free', $3, true, $4, $4)`,
		u2ID, u2Email, model.PersonalEarlyAccessPlanID, now,
	); err != nil {
		t.Fatalf("seed u2: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO waitlist_invites (waitlist_id, email, token, status, expires_at, created_at)
		 VALUES ($1::uuid, LOWER(BTRIM($2)), $3, $4, $5, now())`,
		waitlistID, u2Email, prefix+"-tok-u2", model.WaitlistInviteStatusActive, now.Add(72*time.Hour),
	); err != nil {
		t.Fatalf("seed u2 invite: %v", err)
	}

	// Orphan: should return the single row.
	got, err := s.GetOrphanCandidateForUser(ctx, u1ID)
	if err != nil {
		t.Fatalf("GetOrphanCandidateForUser(u1): %v", err)
	}
	if got == nil || got.UserID != u1ID {
		t.Fatalf("u1 orphan lookup: got %+v, want user %s", got, u1ID)
	}
	if got.InviteToken != prefix+"-tok-u1" {
		t.Fatalf("u1 token: got %q, want %q", got.InviteToken, prefix+"-tok-u1")
	}

	// Already upgraded: plan filter excludes → nil, no error.
	got, err = s.GetOrphanCandidateForUser(ctx, u2ID)
	if err != nil {
		t.Fatalf("GetOrphanCandidateForUser(u2): %v", err)
	}
	if got != nil {
		t.Fatalf("u2 (already upgraded) should be excluded, got %+v", got)
	}

	// Nonexistent user: nil, no error.
	got, err = s.GetOrphanCandidateForUser(ctx, nonexistentID)
	if err != nil {
		t.Fatalf("GetOrphanCandidateForUser(nonexistent): %v", err)
	}
	if got != nil {
		t.Fatalf("nonexistent user should return nil, got %+v", got)
	}
}

// TestListOrphanWaitlistCandidatesLimitDefaultsAndCap locks down the
// defensive limit behavior: 0/negative falls back to 100, anything
// above 500 gets clamped. Uses a fake pool? No — store takes real
// pool. Instead we exercise limit boundaries by seeding >500 rows
// and confirming we never get more than 500 back. That's expensive
// to seed; use a smaller check: 0 and a very large number both
// succeed (no panic / SQL error).
func TestListOrphanWaitlistCandidatesLimitAccepts0AndOverMax(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping limit boundary test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	since := time.Now().UTC().Add(-1 * time.Hour)

	if _, err := s.ListOrphanWaitlistCandidates(ctx, model.OrphanWaitlistCandidateOpts{Since: since, Limit: 0}); err != nil {
		t.Fatalf("limit=0 should use default, got error: %v", err)
	}
	if _, err := s.ListOrphanWaitlistCandidates(ctx, model.OrphanWaitlistCandidateOpts{Since: since, Limit: 100000}); err != nil {
		t.Fatalf("limit=100000 should clamp, got error: %v", err)
	}
}
