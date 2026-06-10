package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Broker struct {
	logger  *slog.Logger
	client  *redis.Client
	channel string

	startOnce sync.Once
	closeOnce sync.Once

	mu   sync.RWMutex
	subs map[string]*Subscription
}

type Subscription struct {
	ID       string
	UserID   string
	HubRoles map[string]string
	Events   chan Event
	broker   *Broker
}

func NewBrokerFromEnv(logger *slog.Logger) *Broker {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		return nil
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Broker{
		logger:  logger,
		client:  redis.NewClient(opts),
		channel: "memax:events",
		subs:    map[string]*Subscription{},
	}
}

func (b *Broker) Start(ctx context.Context) {
	if b == nil || b.client == nil {
		return
	}
	b.startOnce.Do(func() {
		pubsub := b.client.Subscribe(ctx, b.channel)
		go func() {
			defer pubsub.Close()
			ch := pubsub.Channel()
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-ch:
					if !ok {
						return
					}
					var evt Event
					if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
						b.logger.Warn("events: invalid payload", "error", err)
						continue
					}
					b.fanout(evt)
				}
			}
		}()
	})
}

func (b *Broker) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	var err error
	b.closeOnce.Do(func() {
		err = b.client.Close()
	})
	return err
}

func (b *Broker) Publish(ctx context.Context, evt Event) error {
	if b == nil || b.client == nil {
		return nil
	}
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = timeNow()
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, b.channel, payload).Err()
}

func (b *Broker) Subscribe(userID string, hubRoles map[string]string) *Subscription {
	if b == nil {
		return nil
	}
	sub := &Subscription{
		ID:       uuid.NewString(),
		UserID:   userID,
		HubRoles: hubRoles,
		Events:   make(chan Event, 32),
		broker:   b,
	}
	b.mu.Lock()
	b.subs[sub.ID] = sub
	b.mu.Unlock()
	return sub
}

func (s *Subscription) Close() {
	if s == nil || s.broker == nil {
		return
	}
	b := s.broker
	b.mu.Lock()
	delete(b.subs, s.ID)
	b.mu.Unlock()
	close(s.Events)
}

func (b *Broker) fanout(evt Event) {
	// A valid event must carry at least one routing identity: either
	// a hub id (hub fan-out path) or a recipient user id (user-direct
	// path from Phase 3a of the inbox notification framework). Events
	// with neither are dropped as a defensive measure — they would
	// otherwise have no addressable subscriber.
	if evt.HubID == "" && evt.RecipientUserID == "" {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subs {
		if !b.shouldDeliver(sub, evt) {
			continue
		}
		select {
		case sub.Events <- evt:
		default:
			// Drop on slow consumers; invalidation events are best-effort.
		}
	}
}

// shouldDeliver determines whether a given event should be delivered
// to a given subscription. Routing is audience-aware rather than
// field-emptiness-inferred, so a notification row that carries
// hub_id as a display-context decoration cannot leak via the hub
// fan-out path.
//
// Three routing modes, checked in order:
//
//  1. User-audience notification: evt.Audience == "user". Delivers
//     to the single subscription whose UserID matches
//     RecipientUserID, and to no one else — even if the event
//     ALSO carries HubID for UI decoration, and even if the
//     current subscriber shares that hub with the sender. This is
//     the fix for the Phase 3b leak where user-addressed invites
//     fanned out to every hub member because the broker inferred
//     "user-direct" from HubID==""", and producers set both.
//
//  2. Hub fan-out: evt.Audience is "hub" / "hub_member" (explicit)
//     or empty + HubID set (legacy non-notification events like
//     hub.memories.changed). Delivers to every subscriber whose
//     HubRoles map contains HubID, regardless of role.
//
//  3. Implicit user-direct (legacy): evt.Audience empty, HubID
//     empty, RecipientUserID set. Preserves the pre-Phase-3a
//     behavior for any non-notification event that may have used
//     the old shape.
//
// The PrivateOnly gate applies across all modes unchanged.
func (b *Broker) shouldDeliver(sub *Subscription, evt Event) bool {
	if sub == nil {
		return false
	}
	if evt.PrivateOnly && evt.ActorID != "" && sub.UserID != evt.ActorID {
		return false
	}

	// Mode 1 — explicit user-audience notification. Route by
	// recipient only, regardless of HubID decoration.
	if evt.Audience == "user" {
		return sub.UserID != "" && sub.UserID == evt.RecipientUserID
	}

	// Mode 3 fallback — legacy non-notification event shape with
	// no hub identity and only a recipient. Kept for backwards
	// compatibility; notification events never land here because
	// they now carry explicit Audience.
	if evt.HubID == "" {
		return sub.UserID != "" && sub.UserID == evt.RecipientUserID
	}

	// Mode 2 — hub fan-out. Either an explicit "hub" / "hub_member"
	// notification event or a legacy non-notification event.
	role, ok := sub.HubRoles[evt.HubID]
	if !ok || role == "" {
		return false
	}
	// hub.members.changed used to be owner/admin-only, but the
	// Settings → Members tab is visible to every team member
	// (settings-nav.tsx gates it on isTeam, not canManage), and the
	// hub detail response at handler/hubs.go returns the full member
	// list to any member. Narrowing SSE to owner/admin left
	// contributors and viewers with a stale member list until
	// refresh. Delivery is now any-role; the UI still filters what
	// each role can DO (promote/demote/remove) via the management
	// tab's canManage checks — READING the list is the same for all
	// roles.
	// Optional hub_member role narrowing. The notification model
	// supports per-row HubMemberRole to restrict a row to a single
	// role inside the hub (e.g. an "admin-only" hub_member
	// notification must not fan out to contributors or viewers).
	// The visibility SQL filters reads by hub_member_role, but
	// without this SSE check the real-time delivery would still
	// push the row to every member — causing a flash of the row
	// in the wrong audience's inbox before their client re-filters.
	// Treat empty HubMemberRole as "any role" so existing untyped
	// hub events are unchanged.
	if evt.HubMemberRole != "" && evt.HubMemberRole != role {
		return false
	}
	return true
}
