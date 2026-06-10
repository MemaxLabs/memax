package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// seedUserForWaitlist inserts a minimal users row. Needed by tests
// that reference users (approver FK, can_create_hub toggle,
// invite consumer).
func seedUserForWaitlist(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()
	email := "seed-" + userID[:8] + "@wl.test"
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'W', 'personal_early_access', now(), now())`,
		userID, email,
	); err != nil {
		t.Fatalf("seed user %s: %v", userID[:8], err)
	}
}

func TestPostgresWaitlistEntryCRUD(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	entry := &model.WaitlistEntry{
		Email:   "  ALICE@wl.test  ", // whitespace + case to exercise normalization
		UseCase: "personal",
		AITools: []string{"claude-code", "cursor"},
		Role:    "engineer",
	}

	// --- CreateWaitlistEntry: email normalization (LOWER + BTRIM).
	if err := s.CreateWaitlistEntry(ctx, entry); err != nil {
		t.Fatalf("CreateWaitlistEntry: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("CreateWaitlistEntry didn't set ID")
	}
	if entry.Email != "alice@wl.test" {
		t.Errorf("email not normalized: %q", entry.Email)
	}

	// --- GetWaitlistEntry by ID round-trips everything.
	got, err := s.GetWaitlistEntry(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetWaitlistEntry: %v", err)
	}
	if got.UseCase != "personal" || len(got.AITools) != 2 {
		t.Errorf("round-trip lost fields: %+v", got)
	}
	if got.Status != model.WaitlistStatusPending {
		t.Errorf("new entry should be pending, got %q", got.Status)
	}

	// --- GetWaitlistEntryByEmail uses normalized comparison.
	got2, err := s.GetWaitlistEntryByEmail(ctx, "ALICE@wl.test")
	if err != nil {
		t.Fatalf("GetWaitlistEntryByEmail (upper): %v", err)
	}
	if got2.ID != entry.ID {
		t.Error("case-insensitive email lookup failed")
	}

	// --- ListWaitlistEntries with status filter.
	list, _, total, err := s.ListWaitlistEntries(ctx, model.WaitlistListOpts{
		Status: model.WaitlistStatusPending,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListWaitlistEntries: %v", err)
	}
	if total < 1 {
		t.Errorf("total_count = %d, want ≥ 1", total)
	}
	if len(list) < 1 {
		t.Errorf("list returned 0 entries")
	}

	// --- UpdateWaitlistEntry: approve with wave + approver.
	wave := 3
	adminID := uuid.NewString()
	seedUserForWaitlist(t, ctx, pool, adminID)
	updated, err := s.UpdateWaitlistEntry(ctx, entry.ID,
		model.WaitlistStatusApproved, "looks good", &wave, adminID)
	if err != nil {
		t.Fatalf("UpdateWaitlistEntry: %v", err)
	}
	if updated.Status != model.WaitlistStatusApproved {
		t.Errorf("status not updated: %q", updated.Status)
	}
	if updated.Wave == nil || *updated.Wave != 3 {
		t.Errorf("wave not updated: %v", updated.Wave)
	}
	if updated.ApprovedAt == nil {
		t.Error("approved_at should be set on approval")
	}

	// --- GetWaitlistStats aggregates.
	stats, err := s.GetWaitlistStats(ctx)
	if err != nil {
		t.Fatalf("GetWaitlistStats: %v", err)
	}
	if stats.Approved < 1 {
		t.Errorf("approved count = %d, want ≥ 1", stats.Approved)
	}
	foundWave := false
	for _, w := range stats.Waves {
		if w.Wave == 3 {
			foundWave = true
			break
		}
	}
	if !foundWave {
		t.Errorf("wave 3 missing from stats.Waves: %+v", stats.Waves)
	}
}

func TestPostgresWaitlistBatchApprove(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	adminID := uuid.NewString()
	seedUserForWaitlist(t, ctx, pool, adminID)

	var ids []string
	for i := 0; i < 2; i++ {
		e := &model.WaitlistEntry{
			Email:   "batch-" + uuid.NewString()[:8] + "@wl.test",
			AITools: []string{},
		}
		if err := s.CreateWaitlistEntry(ctx, e); err != nil {
			t.Fatalf("seed entry: %v", err)
		}
		ids = append(ids, e.ID)
	}

	n, err := s.BatchApproveWaitlist(ctx, ids, 5, adminID, "batch-note")
	if err != nil {
		t.Fatalf("BatchApproveWaitlist: %v", err)
	}
	if n != 2 {
		t.Errorf("BatchApproveWaitlist reported %d, want 2", n)
	}

	// Idempotency: re-running on already-approved rows affects 0 (the
	// UPDATE's WHERE filters status = 'pending').
	n, _ = s.BatchApproveWaitlist(ctx, ids, 5, adminID, "")
	if n != 0 {
		t.Errorf("idempotent batch: reported %d, want 0", n)
	}
}

func TestPostgresWaitlistInviteLifecycle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	entry := &model.WaitlistEntry{
		Email:   "invite-" + uuid.NewString()[:8] + "@wl.test",
		AITools: []string{},
	}
	if err := s.CreateWaitlistEntry(ctx, entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	token := "tkn-" + uuid.NewString()
	inv := &model.WaitlistInvite{
		WaitlistID: entry.ID,
		Email:      entry.Email,
		Token:      token,
		ExpiresAt:  time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond),
	}
	if err := s.CreateWaitlistInvite(ctx, inv); err != nil {
		t.Fatalf("CreateWaitlistInvite: %v", err)
	}

	// --- GetWaitlistInviteByToken ---
	got, err := s.GetWaitlistInviteByToken(ctx, token)
	if err != nil {
		t.Fatalf("GetWaitlistInviteByToken: %v", err)
	}
	if got.Status != model.WaitlistInviteStatusActive {
		t.Errorf("new invite should be active, got %q", got.Status)
	}

	// --- ListWaitlistInvitesByEntry ---
	list, err := s.ListWaitlistInvitesByEntry(ctx, entry.ID)
	if err != nil {
		t.Fatalf("ListWaitlistInvitesByEntry: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 invite, got %d", len(list))
	}

	// --- GetActiveInviteForEntry ---
	active, err := s.GetActiveInviteForEntry(ctx, entry.ID)
	if err != nil || active == nil {
		t.Fatalf("GetActiveInviteForEntry: got %v, err %v", active, err)
	}

	// --- ListExpiringWaitlistInvites: 48h window includes our 24h invite.
	expiring, err := s.ListExpiringWaitlistInvites(ctx, 48)
	if err != nil {
		t.Fatalf("ListExpiringWaitlistInvites: %v", err)
	}
	if len(expiring) == 0 {
		t.Error("expected at least the 24h invite in 48h window")
	}

	// --- ConsumeWaitlistInvite marks status=used on the first call,
	// returns error on the second (no active row to flip).
	userID := uuid.NewString()
	seedUserForWaitlist(t, ctx, pool, userID)
	if err := s.ConsumeWaitlistInvite(ctx, token, userID); err != nil {
		t.Fatalf("ConsumeWaitlistInvite: %v", err)
	}
	if err := s.ConsumeWaitlistInvite(ctx, token, userID); err == nil {
		t.Error("second consume should error (already used)")
	}

	// Consumed invite shouldn't appear as active.
	active, _ = s.GetActiveInviteForEntry(ctx, entry.ID)
	if active != nil {
		t.Error("consumed invite should not be active")
	}
}

func TestPostgresWaitlistExpireAndRevoke(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	ctx := context.Background()

	entry := &model.WaitlistEntry{
		Email:   "expire-" + uuid.NewString()[:8] + "@wl.test",
		AITools: []string{},
	}
	if err := s.CreateWaitlistEntry(ctx, entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	pastToken := "past-" + uuid.NewString()
	invPast := &model.WaitlistInvite{
		WaitlistID: entry.ID,
		Email:      entry.Email,
		Token:      pastToken,
		ExpiresAt:  time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond),
	}
	if err := s.CreateWaitlistInvite(ctx, invPast); err != nil {
		t.Fatalf("CreateWaitlistInvite (past): %v", err)
	}

	n, err := s.ExpireWaitlistInvites(ctx)
	if err != nil {
		t.Fatalf("ExpireWaitlistInvites: %v", err)
	}
	if n < 1 {
		t.Errorf("ExpireWaitlistInvites expired %d, want ≥ 1", n)
	}

	// --- RevokeWaitlistInvite on a fresh active invite ---
	futureToken := "future-" + uuid.NewString()
	invFuture := &model.WaitlistInvite{
		WaitlistID: entry.ID,
		Email:      entry.Email,
		Token:      futureToken,
		ExpiresAt:  time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
	}
	if err := s.CreateWaitlistInvite(ctx, invFuture); err != nil {
		t.Fatalf("CreateWaitlistInvite (future): %v", err)
	}
	if err := s.RevokeWaitlistInvite(ctx, invFuture.ID); err != nil {
		t.Fatalf("RevokeWaitlistInvite: %v", err)
	}
	// Re-revoke should error (status already expired).
	if err := s.RevokeWaitlistInvite(ctx, invFuture.ID); err == nil {
		t.Error("re-revoke should error")
	}
}

func TestPostgresCanCreateHubToggle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	seedUserForWaitlist(t, ctx, pool, userID)

	if err := s.SetCanCreateHub(ctx, userID, true); err != nil {
		t.Fatalf("SetCanCreateHub(true): %v", err)
	}
	if got, _ := s.GetCanCreateHub(ctx, userID); !got {
		t.Error("after SetCanCreateHub(true), got false")
	}
	if err := s.SetCanCreateHub(ctx, userID, false); err != nil {
		t.Fatalf("SetCanCreateHub(false): %v", err)
	}
	if got, _ := s.GetCanCreateHub(ctx, userID); got {
		t.Error("after SetCanCreateHub(false), got true")
	}
}
