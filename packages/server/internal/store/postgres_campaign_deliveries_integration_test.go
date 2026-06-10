package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupCampaignDeliveryFixture seeds a user, a campaign, and returns
// everything a delivery test needs.
func setupCampaignDeliveryFixture(t *testing.T) (store.Store, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'C', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@cd",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	campaignID := uuid.NewString()
	if err := s.CreateCampaign(ctx, &model.Campaign{
		ID: campaignID, Name: "Test", Channel: "email",
		Kind: model.CampaignKindSystemNotice, Status: model.CampaignStatusDraft,
		Content: json.RawMessage(`{}`), CreatedBy: userID,
	}); err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	return s, userID, campaignID
}

func TestCampaignDeliveriesCreateAndMarkSent(t *testing.T) {
	t.Parallel()
	s, userID, campaignID := setupCampaignDeliveryFixture(t)
	ctx := context.Background()

	d := &model.CampaignDelivery{
		CampaignID:      campaignID,
		RecipientEmail:  "a@b.test",
		RecipientUserID: &userID,
		Status:          "queued",
	}
	inserted, err := s.CreateCampaignDelivery(ctx, d)
	if err != nil {
		t.Fatalf("CreateCampaignDelivery: %v", err)
	}
	if !inserted {
		t.Error("first insert should return true")
	}
	if d.ID == "" {
		t.Fatal("ID should be set on insert")
	}

	// Idempotent retry: same campaign_id + recipient_email → no
	// new row.
	dup := *d
	dup.ID = ""
	inserted, err = s.CreateCampaignDelivery(ctx, &dup)
	if err != nil {
		t.Fatalf("duplicate CreateCampaignDelivery: %v", err)
	}
	if inserted {
		t.Error("duplicate insert should return false")
	}

	// MarkSent flips status + records provider message ID.
	if err := s.MarkCampaignDeliverySent(ctx, d.ID, "resend-abc", time.Now()); err != nil {
		t.Fatalf("MarkCampaignDeliverySent: %v", err)
	}
	got, err := s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "a@b.test")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Status != "sent" {
		t.Errorf("status after MarkSent = %q", got.Status)
	}
	if got.ProviderMessageID != "resend-abc" {
		t.Errorf("provider_message_id = %q", got.ProviderMessageID)
	}
	if got.SentAt == nil {
		t.Error("sent_at not stamped")
	}
}

func TestCampaignDeliveriesWebhookTransitions(t *testing.T) {
	t.Parallel()
	s, userID, campaignID := setupCampaignDeliveryFixture(t)
	ctx := context.Background()

	// Seed delivered → open → bounce → complain sequence on a fresh row.
	d := &model.CampaignDelivery{
		CampaignID: campaignID, RecipientEmail: "wh@b.test",
		RecipientUserID: &userID, Status: "queued",
	}
	_, _ = s.CreateCampaignDelivery(ctx, d)
	if err := s.MarkCampaignDeliverySent(ctx, d.ID, "m1", time.Now()); err != nil {
		t.Fatalf("sent: %v", err)
	}
	if err := s.MarkCampaignDeliveryDelivered(ctx, d.ID, time.Now()); err != nil {
		t.Fatalf("delivered: %v", err)
	}
	got, _ := s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "wh@b.test")
	if got.Status != "delivered" {
		t.Errorf("after delivered = %q", got.Status)
	}

	// Open stamps opened_at without changing status.
	if err := s.MarkCampaignDeliveryOpened(ctx, d.ID, time.Now()); err != nil {
		t.Fatalf("opened: %v", err)
	}
	got, _ = s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "wh@b.test")
	if got.Status != "delivered" {
		t.Errorf("open mutated status: %q", got.Status)
	}
	if got.OpenedAt == nil {
		t.Error("opened_at not stamped")
	}

	// Bounce transitions from delivered.
	if err := s.MarkCampaignDeliveryBounced(ctx, d.ID, time.Now(), "hard", "no such addr"); err != nil {
		t.Fatalf("bounced: %v", err)
	}
	got, _ = s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "wh@b.test")
	if got.Status != "bounced" {
		t.Errorf("after bounce = %q", got.Status)
	}

	// Complain on bounced row — WHERE IN ('queued','sent','delivered')
	// excludes 'bounced', so complain is a no-op. Verify bounced sticks.
	if err := s.MarkCampaignDeliveryComplained(ctx, d.ID, time.Now()); err != nil {
		t.Fatalf("complain (no-op expected): %v", err)
	}
	got, _ = s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "wh@b.test")
	if got.Status != "bounced" {
		t.Errorf("complain should not overwrite terminal bounce, got %q", got.Status)
	}
}

func TestCampaignDeliveriesFailedMark(t *testing.T) {
	t.Parallel()
	s, userID, campaignID := setupCampaignDeliveryFixture(t)
	ctx := context.Background()
	d := &model.CampaignDelivery{
		CampaignID: campaignID, RecipientEmail: "f@b.test",
		RecipientUserID: &userID, Status: "queued",
	}
	_, _ = s.CreateCampaignDelivery(ctx, d)
	if err := s.MarkCampaignDeliveryFailed(ctx, d.ID, "send_error", "smtp timeout"); err != nil {
		t.Fatalf("MarkCampaignDeliveryFailed: %v", err)
	}
	got, _ := s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "f@b.test")
	if got.Status != "failed" {
		t.Errorf("status = %q", got.Status)
	}
	if got.ErrorCode != "send_error" {
		t.Errorf("error_code = %q", got.ErrorCode)
	}
}

func TestCampaignDeliveriesAnnotatePreservesStatus(t *testing.T) {
	t.Parallel()
	s, userID, campaignID := setupCampaignDeliveryFixture(t)
	ctx := context.Background()
	d := &model.CampaignDelivery{
		CampaignID: campaignID, RecipientEmail: "n@b.test",
		RecipientUserID: &userID, Status: "queued",
	}
	_, _ = s.CreateCampaignDelivery(ctx, d)
	if err := s.AnnotateCampaignDelivery(ctx, d.ID, "opt_out", "user unsubscribed"); err != nil {
		t.Fatalf("AnnotateCampaignDelivery: %v", err)
	}
	got, _ := s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "n@b.test")
	// Status must be preserved — annotate is NOT a state transition.
	if got.Status != "queued" {
		t.Errorf("annotate mutated status: %q", got.Status)
	}
	if got.ErrorCode != "opt_out" {
		t.Errorf("error_code = %q", got.ErrorCode)
	}
}

func TestCampaignDeliveriesGetStats(t *testing.T) {
	t.Parallel()
	s, userID, campaignID := setupCampaignDeliveryFixture(t)
	ctx := context.Background()

	// Seed 3 deliveries with different statuses.
	for i, email := range []string{"s1@b", "s2@b", "s3@b"} {
		d := &model.CampaignDelivery{
			CampaignID: campaignID, RecipientEmail: email,
			RecipientUserID: &userID, Status: "queued",
		}
		_, _ = s.CreateCampaignDelivery(ctx, d)
		if i == 0 {
			_ = s.MarkCampaignDeliverySent(ctx, d.ID, "m", time.Now())
		}
		if i == 1 {
			_ = s.MarkCampaignDeliveryFailed(ctx, d.ID, "x", "y")
		}
	}

	stats, err := s.GetCampaignDeliveryStats(ctx, campaignID)
	if err != nil {
		t.Fatalf("GetCampaignDeliveryStats: %v", err)
	}
	if stats == nil {
		t.Fatal("nil stats")
	}
	// 1 queued + 1 sent + 1 failed = 3 total.
	if stats.Queued+stats.Sent+stats.Failed < 3 {
		t.Errorf("stats counts too low: %+v", stats)
	}
}

func TestCampaignDeliveriesLookupMissingReturnsSentinel(t *testing.T) {
	t.Parallel()
	s, _, campaignID := setupCampaignDeliveryFixture(t)
	ctx := context.Background()
	_, err := s.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, "nope@b.test")
	if !errors.Is(err, store.ErrCampaignDeliveryNotFound) {
		t.Errorf("missing row: got %v, want ErrCampaignDeliveryNotFound", err)
	}
}

func TestEmailOptOutRoundTrip(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'O', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@oo",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Initial opt-in.
	optedOut, err := s.IsUserEmailOptedOut(ctx, userID)
	if err != nil {
		t.Fatalf("initial IsUserEmailOptedOut: %v", err)
	}
	if optedOut {
		t.Error("new user should not be opted out")
	}

	// Generate token; second call returns the same one (idempotent).
	t1, err := s.GetOrCreateEmailOptOutToken(ctx, userID)
	if err != nil {
		t.Fatalf("GetOrCreateEmailOptOutToken: %v", err)
	}
	if t1 == "" {
		t.Fatal("empty token")
	}
	t2, _ := s.GetOrCreateEmailOptOutToken(ctx, userID)
	if t1 != t2 {
		t.Errorf("token should be stable: %q vs %q", t1, t2)
	}

	// UnsubscribeByToken flips opt-out AND rotates the token.
	gotUserID, err := s.UnsubscribeByToken(ctx, t1)
	if err != nil {
		t.Fatalf("UnsubscribeByToken: %v", err)
	}
	if gotUserID != userID {
		t.Errorf("UnsubscribeByToken returned wrong user: %q", gotUserID)
	}
	optedOut, _ = s.IsUserEmailOptedOut(ctx, userID)
	if !optedOut {
		t.Error("user should be opted out after unsubscribe")
	}

	// Re-unsubscribe with the old token — the rotation means the
	// old token is no longer valid.
	if _, err := s.UnsubscribeByToken(ctx, t1); err == nil {
		t.Error("stale token should not work after rotation")
	}
}
