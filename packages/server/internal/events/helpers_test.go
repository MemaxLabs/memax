package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// capturePublisher collects every event passed to Publish. Used by
// the helper-function tests to assert envelope shape without
// standing up a real Redis subscription.
type capturePublisher struct {
	mu     sync.Mutex
	events []Event
}

func (p *capturePublisher) Publish(_ context.Context, evt Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *capturePublisher) Close() error { return nil }

func (p *capturePublisher) last() Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return Event{}
	}
	return p.events[len(p.events)-1]
}

// ─── PublishMemoryChanged ────────────────────────────────────────

func TestPublishMemoryChangedBuildsEnvelope(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	mem := &model.Memory{
		ID:       "mem-1",
		HubID:    "hub-1",
		State:    "active",
		TopicID:  "topic-1",
		Boundary: "private",
	}
	PublishMemoryChanged(context.Background(), pub, mem, "actor-1")
	ev := pub.last()
	if ev.Type != EventTypeHubMemoriesChanged {
		t.Errorf("type = %q, want %q", ev.Type, EventTypeHubMemoriesChanged)
	}
	if ev.HubID != "hub-1" || ev.EntityID != "mem-1" || ev.TopicID != "topic-1" {
		t.Errorf("envelope routing fields wrong: %+v", ev)
	}
	if !ev.PrivateOnly {
		t.Error("private boundary should set PrivateOnly=true")
	}
}

func TestPublishMemoryChangedSkipsNilInputs(t *testing.T) {
	t.Parallel()

	// Nil publisher: helpers MUST tolerate — handlers pass whatever
	// is wired, and events is optional per SetBroker semantics.
	PublishMemoryChanged(context.Background(), nil, &model.Memory{ID: "m"}, "u")

	// Nil memory: helpers MUST tolerate — caller shape may vary.
	pub := &capturePublisher{}
	PublishMemoryChanged(context.Background(), pub, nil, "u")
	if len(pub.events) != 0 {
		t.Errorf("nil memory should not publish; got %d events", len(pub.events))
	}
}

func TestPublishMemoryChangedWithPrivacyOverridesBoundary(t *testing.T) {
	t.Parallel()

	// Shared boundary memory, but the caller flags privateOnly=true
	// — the override wins (boundary is NOT re-read). This is the
	// backstop for "private content crossed into a team hub by
	// accident": the caller knows and the event respects it.
	pub := &capturePublisher{}
	mem := &model.Memory{ID: "m", HubID: "h", Boundary: "shared"}
	PublishMemoryChangedWithPrivacy(context.Background(), pub, mem, "a", true)
	if !pub.last().PrivateOnly {
		t.Error("PrivateOnly override not respected")
	}
}

func TestPublishHubMemoriesChangedSkipsEmptyHubID(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	PublishHubMemoriesChanged(context.Background(), pub, "", "actor")
	if len(pub.events) != 0 {
		t.Errorf("empty hub id should produce no event; got %d", len(pub.events))
	}
	// Non-empty path
	PublishHubMemoriesChanged(context.Background(), pub, "hub-1", "actor")
	if pub.last().HubID != "hub-1" || pub.last().Type != EventTypeHubMemoriesChanged {
		t.Errorf("envelope mismatch: %+v", pub.last())
	}
}

func TestPublishTopicsChanged(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	PublishTopicsChanged(context.Background(), pub, "", "a", "t1")
	if len(pub.events) != 0 {
		t.Error("empty hub should skip publish")
	}
	PublishTopicsChanged(context.Background(), pub, "h1", "a1", "t1")
	ev := pub.last()
	if ev.Type != EventTypeHubTopicsChanged || ev.HubID != "h1" || ev.TopicID != "t1" || ev.EntityID != "t1" {
		t.Errorf("topic envelope wrong: %+v", ev)
	}
}

func TestPublishDreamsChanged(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	PublishDreamsChanged(context.Background(), pub, "", "a")
	if len(pub.events) != 0 {
		t.Error("empty hub should skip publish")
	}
	PublishDreamsChanged(context.Background(), pub, "h1", "a1")
	if pub.last().Type != EventTypeHubDreamsChanged {
		t.Error("wrong event type")
	}
}

func TestPublishHubListChanged(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}

	// Empty userID → no publish (user-axis event must have a recipient).
	PublishHubListChanged(context.Background(), pub, "", "h1", "created")
	if len(pub.events) != 0 {
		t.Error("empty userID should skip publish")
	}
	// Nil publisher → no panic.
	PublishHubListChanged(context.Background(), nil, "u1", "h1", "created")
	if len(pub.events) != 0 {
		t.Error("nil publisher should skip publish")
	}
	// Happy path — user-axis envelope.
	PublishHubListChanged(context.Background(), pub, "u1", "h1", "created")
	got := pub.last()
	if got.Type != EventTypeHubListChanged {
		t.Errorf("want type %q, got %q", EventTypeHubListChanged, got.Type)
	}
	if got.Audience != "user" {
		t.Errorf("want audience=user (user-axis routing), got %q", got.Audience)
	}
	if got.RecipientUserID != "u1" {
		t.Errorf("want recipient u1, got %q", got.RecipientUserID)
	}
	if got.EntityID != "h1" {
		t.Errorf("want entity_id h1, got %q", got.EntityID)
	}
	if got.State != "created" {
		t.Errorf("want state created, got %q", got.State)
	}
	// Empty HubID is fine — the entity id carries the hub and the
	// broker routes on RecipientUserID for user-axis events.
	if got.HubID != "" {
		t.Errorf("want empty hub_id (hub lives in entity_id), got %q", got.HubID)
	}
}

func TestPublishHubMembersChanged(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}

	// Empty hub id → no publish.
	PublishHubMembersChanged(context.Background(), pub, "", "a")
	if len(pub.events) != 0 {
		t.Error("empty hub should skip publish")
	}
	// Nil publisher → no panic, no publish.
	PublishHubMembersChanged(context.Background(), nil, "h1", "a1")
	if len(pub.events) != 0 {
		t.Error("nil publisher should skip publish")
	}
	// Happy path.
	PublishHubMembersChanged(context.Background(), pub, "h1", "a1")
	got := pub.last()
	if got.Type != EventTypeHubMembersChanged {
		t.Errorf("want type %q, got %q", EventTypeHubMembersChanged, got.Type)
	}
	if got.HubID != "h1" {
		t.Errorf("want hub_id h1, got %q", got.HubID)
	}
	if got.ActorID != "a1" {
		t.Errorf("want actor_id a1, got %q", got.ActorID)
	}
	// Empty audience = hub fan-out path in the broker.
	if got.Audience != "" {
		t.Errorf("want empty audience (hub fan-out), got %q", got.Audience)
	}
	if got.RecipientUserID != "" {
		t.Errorf("want empty recipient (not user-axis), got %q", got.RecipientUserID)
	}
}

// ─── Notification helpers ────────────────────────────────────────

func TestPublishNotificationCreatedEnvelope(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	n := &model.Notification{
		ID:              "n1",
		Audience:        model.AudienceUser,
		HubID:           "h1",
		RecipientUserID: "u1",
		HubMemberRole:   "owner",
		Kind:            "dream_run_completed",
		SourceKind:      "dream_run",
		SourceID:        "run-1",
	}
	PublishNotificationCreated(context.Background(), pub, n)
	ev := pub.last()
	if ev.Type != EventTypeNotificationCreated {
		t.Errorf("type = %q", ev.Type)
	}
	if ev.RecipientUserID != "u1" {
		t.Error("recipient should propagate for user-audience routing")
	}
	if ev.HubMemberRole != "owner" || ev.Kind != "dream_run_completed" {
		t.Errorf("kind/role missing: %+v", ev)
	}
	if ev.ActorID != "" {
		t.Error("ActorID must stay empty — see publishNotificationEnvelope comment on PrivateOnly semantics")
	}
}

func TestPublishNotificationResolvedIncludesResolution(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	n := &model.Notification{
		ID:         "n1",
		Kind:       "topic_merge",
		Resolution: model.ResolutionMerged,
	}
	PublishNotificationResolved(context.Background(), pub, n)
	ev := pub.last()
	if ev.Type != EventTypeNotificationResolved {
		t.Errorf("type wrong: %q", ev.Type)
	}
	if ev.Resolution != string(model.ResolutionMerged) {
		t.Errorf("resolution dropped: %q", ev.Resolution)
	}
}

func TestPublishNotificationResolvedSkipsEmptyResolution(t *testing.T) {
	t.Parallel()

	// No resolution set on the row → event shouldn't carry the
	// field. Empty string in the envelope is fine but shouldn't
	// look like a real resolution discriminator.
	pub := &capturePublisher{}
	n := &model.Notification{ID: "n1", Kind: "k"}
	PublishNotificationResolved(context.Background(), pub, n)
	if pub.last().Resolution != "" {
		t.Errorf("empty resolution leaked into envelope: %q", pub.last().Resolution)
	}
}

func TestPublishNotificationUpdatedCarriesChangeAndSeenAt(t *testing.T) {
	t.Parallel()

	pub := &capturePublisher{}
	seen := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n := &model.Notification{
		ID:     "n1",
		Kind:   "dream_run_completed",
		SeenAt: &seen,
	}
	PublishNotificationUpdated(context.Background(), pub, n, NotificationChangeSeen)
	ev := pub.last()
	if ev.Change != NotificationChangeSeen {
		t.Errorf("change = %q, want seen", ev.Change)
	}
	if ev.SeenAt == nil || !ev.SeenAt.Equal(seen) {
		t.Errorf("seen_at = %v, want %v", ev.SeenAt, seen)
	}
	// Defensive: mutating the envelope's SeenAt should not ripple
	// back into the caller's notification row (the helper must
	// copy the pointer's target, not pass it through).
	*ev.SeenAt = seen.Add(time.Hour)
	if n.SeenAt.Equal(*ev.SeenAt) {
		t.Error("helper leaked a mutable SeenAt pointer to the envelope")
	}
}

func TestPublishNotificationUpdatedNonSeenChangeLeavesSeenAtEmpty(t *testing.T) {
	t.Parallel()

	// Dismissed / expired events should NOT carry SeenAt even if
	// the row happens to have it set — only the seen change is
	// semantically about the timestamp transition.
	pub := &capturePublisher{}
	seen := time.Now()
	n := &model.Notification{ID: "n1", SeenAt: &seen}
	PublishNotificationUpdated(context.Background(), pub, n, NotificationChangeDismissed)
	if pub.last().SeenAt != nil {
		t.Error("dismissed event must not carry SeenAt")
	}
}

func TestPublishNotificationHelpersSkipNilInputs(t *testing.T) {
	t.Parallel()

	// Nil publisher / nil notification must be no-ops.
	PublishNotificationCreated(context.Background(), nil, &model.Notification{ID: "n"})
	PublishNotificationCreated(context.Background(), &capturePublisher{}, nil)
	PublishNotificationResolved(context.Background(), nil, &model.Notification{ID: "n"})
	PublishNotificationUpdated(context.Background(), nil, &model.Notification{ID: "n"}, NotificationChangeSeen)
	// If we got here without a panic, the guards worked.
}

// ─── Redis publisher construction edge cases ────────────────────

func TestNewPublisherRejectsBadURL(t *testing.T) {
	t.Parallel()

	if p := NewPublisher("not a redis url"); p != nil {
		t.Errorf("bad URL should return nil, got %T", p)
	}
}

func TestNewPublisherFromEnvReturnsNilWhenUnset(t *testing.T) {
	// No parallel — t.Setenv incompatible.
	t.Setenv("REDIS_URL", "")
	if p := NewPublisherFromEnv(); p != nil {
		t.Errorf("unset REDIS_URL should return nil, got %T", p)
	}
}

func TestRedisPublisherPublishTolerantOfNil(t *testing.T) {
	t.Parallel()

	// publish on a zero-value struct should be safe — the guard at
	// the top of Publish keeps callers from racing Close().
	var p *RedisPublisher
	if err := p.Publish(context.Background(), Event{}); err != nil {
		t.Errorf("nil receiver Publish should be a no-op, got %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("nil receiver Close should be a no-op, got %v", err)
	}
}

// ─── PublishAgentChanged ─────────────────────────────────────────

func TestPublishAgentChangedBuildsUserAxisEnvelope(t *testing.T) {
	t.Parallel()
	p := &capturePublisher{}
	PublishAgentChanged(context.Background(), p, "user-1", "claude-code", "upserted")
	evt := p.last()
	if evt.Type != EventTypeAgentChanged {
		t.Errorf("Type = %q, want %q", evt.Type, EventTypeAgentChanged)
	}
	if evt.Audience != "user" {
		t.Errorf("Audience = %q, want %q", evt.Audience, "user")
	}
	if evt.RecipientUserID != "user-1" {
		t.Errorf("RecipientUserID = %q, want %q", evt.RecipientUserID, "user-1")
	}
	if evt.EntityID != "claude-code" {
		t.Errorf("EntityID = %q, want %q", evt.EntityID, "claude-code")
	}
	if evt.State != "upserted" {
		t.Errorf("State = %q, want %q", evt.State, "upserted")
	}
	// Agents are user-owned — HubID intentionally empty so the broker
	// does not fan out to hub members.
	if evt.HubID != "" {
		t.Errorf("HubID = %q, want empty (user-axis)", evt.HubID)
	}
}

func TestPublishAgentChangedNoOpOnNilPublisher(t *testing.T) {
	t.Parallel()
	// Should not panic or error when publisher is nil (graceful
	// degradation when Redis is not configured).
	PublishAgentChanged(context.Background(), nil, "user-1", "claude-code", "upserted")
}

func TestPublishAgentChangedNoOpOnEmptyUserID(t *testing.T) {
	t.Parallel()
	p := &capturePublisher{}
	PublishAgentChanged(context.Background(), p, "", "claude-code", "upserted")
	if len(p.events) != 0 {
		t.Errorf("empty userID should not publish, got %d events", len(p.events))
	}
}

// ─── PublishKeyChanged ───────────────────────────────────────────

func TestPublishKeyChangedBuildsUserAxisEnvelope(t *testing.T) {
	t.Parallel()
	p := &capturePublisher{}
	PublishKeyChanged(context.Background(), p, "user-2", "key-abc", "revoked")
	evt := p.last()
	if evt.Type != EventTypeKeyChanged {
		t.Errorf("Type = %q, want %q", evt.Type, EventTypeKeyChanged)
	}
	if evt.Audience != "user" {
		t.Errorf("Audience = %q, want %q", evt.Audience, "user")
	}
	if evt.RecipientUserID != "user-2" {
		t.Errorf("RecipientUserID = %q, want %q", evt.RecipientUserID, "user-2")
	}
	if evt.EntityID != "key-abc" {
		t.Errorf("EntityID = %q, want %q", evt.EntityID, "key-abc")
	}
	if evt.State != "revoked" {
		t.Errorf("State = %q, want %q", evt.State, "revoked")
	}
	if evt.HubID != "" {
		t.Errorf("HubID = %q, want empty (user-axis)", evt.HubID)
	}
}

func TestPublishKeyChangedStateValues(t *testing.T) {
	t.Parallel()
	p := &capturePublisher{}
	PublishKeyChanged(context.Background(), p, "user-1", "k1", "created")
	PublishKeyChanged(context.Background(), p, "user-1", "k1", "updated")
	PublishKeyChanged(context.Background(), p, "user-1", "k1", "revoked")
	if len(p.events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(p.events))
	}
	for i, want := range []string{"created", "updated", "revoked"} {
		if p.events[i].State != want {
			t.Errorf("events[%d].State = %q, want %q", i, p.events[i].State, want)
		}
	}
}

func TestPublishKeyChangedNoOpOnNilPublisher(t *testing.T) {
	t.Parallel()
	PublishKeyChanged(context.Background(), nil, "user-1", "key-abc", "created")
}

func TestPublishKeyChangedNoOpOnEmptyUserID(t *testing.T) {
	t.Parallel()
	p := &capturePublisher{}
	PublishKeyChanged(context.Background(), p, "", "key-abc", "created")
	if len(p.events) != 0 {
		t.Errorf("empty userID should not publish, got %d events", len(p.events))
	}
}

// TestAgentAttributionTransferPattern documents the invariant
// handlers follow when a key's agent assignment changes — both the
// previous and the new agent (if any) must receive agent.changed so
// connected-agents key_count refreshes on all the user's devices.
// Mirrors the logic in AuthHandler.UpdateAPIKey / RevokeAPIKey per
// the PR C codex review.
func TestAgentAttributionTransferPattern(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, prev, next string
		wantEmits        []string // EntityIDs in publish order
	}{
		{"assign-from-empty", "", "codex", []string{"codex"}},
		{"clear-assignment", "codex", "", []string{"codex"}},
		{"reassign-between-agents", "codex", "cursor", []string{"codex", "cursor"}},
		{"no-change-same-agent", "codex", "codex", nil},
		{"no-change-both-empty", "", "", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &capturePublisher{}
			// Handler-equivalent conditional: fire for both sides when
			// the attribution actually changes.
			if tc.prev != tc.next {
				if tc.prev != "" {
					PublishAgentChanged(context.Background(), p, "user-x", tc.prev, "upserted")
				}
				if tc.next != "" {
					PublishAgentChanged(context.Background(), p, "user-x", tc.next, "upserted")
				}
			}
			if len(p.events) != len(tc.wantEmits) {
				t.Fatalf("emit count = %d, want %d", len(p.events), len(tc.wantEmits))
			}
			for i, want := range tc.wantEmits {
				if p.events[i].EntityID != want {
					t.Errorf("emit[%d].EntityID = %q, want %q", i, p.events[i].EntityID, want)
				}
				if p.events[i].Type != EventTypeAgentChanged {
					t.Errorf("emit[%d].Type = %q, want %q", i, p.events[i].Type, EventTypeAgentChanged)
				}
				if p.events[i].Audience != "user" {
					t.Errorf("emit[%d].Audience = %q, want user", i, p.events[i].Audience)
				}
			}
		})
	}
}
