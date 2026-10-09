package onboarding

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

type capturePublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *capturePublisher) Publish(_ context.Context, evt events.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *capturePublisher) Close() error { return nil }

func (p *capturePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

// seedChecklist writes a v1 onboarding checklist for a fresh user and
// returns (userID, hubID, notificationID). `done` marks items complete
// before the row is written.
func seedChecklist(t *testing.T, ctx context.Context, s store.Store, pool *pgxpool.Pool, done ...string) (string, string, string) {
	t.Helper()
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'T', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@tick",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "H", Slug: "h-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	payload := buildChecklistV1Payload()
	at := time.Now().UTC().Add(-time.Hour)
	for i := range payload.Items {
		for _, id := range done {
			if payload.Items[i].ID == id {
				payload.Items[i].CompletedAt = &at
			}
		}
	}
	raw, _ := json.Marshal(payload)
	n := &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		HubID:           hubID,
		RecipientUserID: userID,
		Kind:            model.NotificationKindChecklist,
		Status:          model.NotificationStatusPending,
		SourceKind:      model.NotificationSourceOnboarding,
		SourceID:        checklistSourceID(userID, 1),
		Payload:         raw,
		CreatedAt:       time.Now().Truncate(time.Microsecond),
	}
	if err := s.CreateNotification(n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	return userID, hubID, n.ID
}

func readChecklist(t *testing.T, ctx context.Context, s store.Store, userID, hubID, id string) (*model.Notification, model.ChecklistPayload) {
	t.Helper()
	n, err := s.GetNotification(ctx, id, userID, []string{hubID})
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	var p model.ChecklistPayload
	if err := json.Unmarshal(n.Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return n, p
}

func itemDone(p model.ChecklistPayload, id string) bool {
	for _, it := range p.Items {
		if it.ID == id {
			return it.CompletedAt != nil
		}
	}
	return false
}

// A finished dream on a checklist whose five_memories gate is still
// open must NOT tick first_dream (locked_by) and must stay quiet.
func TestTickFirstDream_LockedStaysQuiet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := testdb.Acquire(t)
	userID, hubID, id := seedChecklist(t, ctx, s, pool)
	pub := &capturePublisher{}

	TickFirstDream(ctx, s, pub, userID)

	_, p := readChecklist(t, ctx, s, userID, hubID, id)
	if itemDone(p, ItemFirstDream) {
		t.Fatal("first_dream must stay locked until five_memories completes")
	}
	if pub.count() != 0 {
		t.Fatalf("expected no events, got %d", pub.count())
	}
}

// With every other required item done, a finished dream ticks
// first_dream, publishes the item update, and stamps all_done_at while
// leaving the row pending with a one-day expiry cap.
func TestTickFirstDream_TicksAndFinishes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := testdb.Acquire(t)
	userID, hubID, id := seedChecklist(t, ctx, s, pool,
		ItemConnectAgent, ItemFirstMemory, ItemFirstAsk, ItemFiveMemories)
	pub := &capturePublisher{}

	TickFirstDream(ctx, s, pub, userID)

	n, p := readChecklist(t, ctx, s, userID, hubID, id)
	if !itemDone(p, ItemFirstDream) {
		t.Fatal("first_dream should be completed")
	}
	if p.AllDoneAt == nil {
		t.Fatal("all_done_at should be stamped once every required item is done")
	}
	if n.Status != model.NotificationStatusPending {
		t.Fatalf("row must stay pending, got %q", n.Status)
	}
	if n.ExpiresAt == nil || n.ExpiresAt.After(time.Now().Add(24*time.Hour+time.Minute)) {
		t.Fatalf("expires_at should be capped at ~1 day, got %v", n.ExpiresAt)
	}
	if pub.count() < 2 {
		t.Fatalf("expected item_updated + all_done events, got %d", pub.count())
	}

	// Idempotent: a second finished dream changes nothing and stays quiet.
	before := pub.count()
	TickFirstDream(ctx, s, pub, userID)
	if pub.count() != before {
		t.Fatalf("second tick must not publish, got %d new events", pub.count()-before)
	}
}

// Another user's dream must never touch this user's checklist.
func TestTickFirstDream_OtherUserUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, pool := testdb.Acquire(t)
	userID, hubID, id := seedChecklist(t, ctx, s, pool,
		ItemConnectAgent, ItemFirstMemory, ItemFirstAsk, ItemFiveMemories)
	pub := &capturePublisher{}

	TickFirstDream(ctx, s, pub, uuid.NewString())

	_, p := readChecklist(t, ctx, s, userID, hubID, id)
	if itemDone(p, ItemFirstDream) {
		t.Fatal("a stranger's dream ticked this user's first_dream")
	}
	if pub.count() != 0 {
		t.Fatalf("expected no events, got %d", pub.count())
	}
}
