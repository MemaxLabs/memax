package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// newTestEmailWorker builds a CampaignSendWorker wired against the
// in-memory store. EnqueueRenderedEmail defaults to a no-op; tests
// override it to inject failures.
func newTestEmailWorker(t *testing.T, s *store.InMemoryStore) (*CampaignSendWorker, *int) {
	t.Helper()

	manager, err := email.NewTemplateManager(s, s)
	if err != nil {
		t.Fatalf("new template manager: %v", err)
	}

	enqueueCount := 0
	return &CampaignSendWorker{
		Store:     s,
		Templates: manager,
		EnqueueRenderedEmail: func(_ context.Context, _, _, _, _ string, _ map[string]string, _ map[string]string) error {
			enqueueCount++
			return nil
		},
	}, &enqueueCount
}

func seedUser(t *testing.T, s *store.InMemoryStore, id, email string) {
	t.Helper()
	s.AddUser(&model.User{ID: id, Email: email})
}

func TestSendCampaignEmailFirstDispatchSendsAndCreatesDelivery(t *testing.T) {
	s := store.NewInMemoryStore()
	seedUser(t, s, "u1", "alice@example.com")
	w, enqueueCount := newTestEmailWorker(t, s)

	campaign := &model.Campaign{ID: "c1", Channel: model.CampaignChannelEmail}
	ec := &campaignEmailContext{Subject: "Hi", HTML: "<p>hi</p>", Text: "hi", BatchID: "batch-1"}

	outcome := w.sendCampaignEmail(context.Background(), campaign, ec, "u1")

	if outcome != dispatchSent {
		t.Fatalf("expected dispatchSent, got %v", outcome)
	}
	if *enqueueCount != 1 {
		t.Fatalf("expected 1 enqueue, got %d", *enqueueCount)
	}
	stats, _ := s.GetCampaignDeliveryStats(context.Background(), "c1")
	if stats.Sent != 1 || stats.Total != 1 {
		t.Fatalf("expected Total=1 Sent=1, got %+v", stats)
	}
}

func TestSendCampaignEmailRetryIsIdempotent(t *testing.T) {
	s := store.NewInMemoryStore()
	seedUser(t, s, "u1", "alice@example.com")
	w, enqueueCount := newTestEmailWorker(t, s)

	campaign := &model.Campaign{ID: "c1", Channel: model.CampaignChannelEmail}
	ec := &campaignEmailContext{Subject: "Hi", HTML: "<p>hi</p>", Text: "hi", BatchID: "batch-1"}

	// First dispatch: sends
	_ = w.sendCampaignEmail(context.Background(), campaign, ec, "u1")
	if *enqueueCount != 1 {
		t.Fatalf("expected 1 enqueue after first dispatch, got %d", *enqueueCount)
	}

	// Retry: must be a no-op — delivery row already exists on (c1, email).
	outcome := w.sendCampaignEmail(context.Background(), campaign, ec, "u1")
	if outcome != dispatchSkipped {
		t.Fatalf("retry: expected dispatchSkipped, got %v", outcome)
	}
	if *enqueueCount != 1 {
		t.Fatalf("retry: expected still 1 enqueue (no duplicate), got %d", *enqueueCount)
	}
}

func TestSendCampaignEmailSuppressedOptOutSkipsEnqueueAndDoesNotFail(t *testing.T) {
	s := store.NewInMemoryStore()
	seedUser(t, s, "u1", "alice@example.com")
	s.SetUserEmailOptedOut("u1", true)
	w, enqueueCount := newTestEmailWorker(t, s)

	campaign := &model.Campaign{ID: "c1", Channel: model.CampaignChannelEmail}
	ec := &campaignEmailContext{Subject: "Hi", HTML: "<p>hi</p>", Text: "hi", BatchID: "batch-1"}

	outcome := w.sendCampaignEmail(context.Background(), campaign, ec, "u1")

	if outcome != dispatchSkipped {
		t.Fatalf("opted-out: expected dispatchSkipped (not failed), got %v", outcome)
	}
	if *enqueueCount != 0 {
		t.Fatalf("opted-out: expected 0 enqueues, got %d", *enqueueCount)
	}
	stats, _ := s.GetCampaignDeliveryStats(context.Background(), "c1")
	if stats.Suppressed != 1 {
		t.Fatalf("expected Suppressed=1, got %+v", stats)
	}
}

func TestSendCampaignEmailEnqueueFailMarksDeliveryFailed(t *testing.T) {
	s := store.NewInMemoryStore()
	seedUser(t, s, "u1", "alice@example.com")
	manager, err := email.NewTemplateManager(s, s)
	if err != nil {
		t.Fatalf("new template manager: %v", err)
	}

	w := &CampaignSendWorker{
		Store:     s,
		Templates: manager,
		EnqueueRenderedEmail: func(_ context.Context, _, _, _, _ string, _ map[string]string, _ map[string]string) error {
			return errors.New("upstream unavailable")
		},
	}

	campaign := &model.Campaign{ID: "c1", Channel: model.CampaignChannelEmail}
	ec := &campaignEmailContext{Subject: "Hi", HTML: "<p>hi</p>", Text: "hi", BatchID: "batch-1"}

	outcome := w.sendCampaignEmail(context.Background(), campaign, ec, "u1")

	if outcome != dispatchFailed {
		t.Fatalf("enqueue-fail: expected dispatchFailed, got %v", outcome)
	}
	stats, _ := s.GetCampaignDeliveryStats(context.Background(), "c1")
	if stats.Failed != 1 {
		t.Fatalf("expected Failed=1, got %+v", stats)
	}
}

// ensure the suite compiles even when time isn't otherwise used.
var _ = time.Now
