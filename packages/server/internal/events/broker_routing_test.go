package events

import (
	"testing"
)

// TestBrokerUserAudienceRouting proves the Phase 3a guarantee from
// docs/plans/17-inbox-notification-framework.md §4.0: a user-audience
// event reaches only the addressed subscriber and never leaks to a
// subscriber in an unrelated hub.
//
// Scenario:
//   - subA: logged-in user "alice", member of hub H with role owner.
//   - subB: logged-in user "bob",   NOT a member of hub H (no roles).
//
// Events dispatched directly via fanout (no Redis round-trip):
//  1. A user-audience event addressed to bob must reach subB only.
//  2. A hub-audience event for hub H must reach subA only (subB has
//     no role and bob is not the recipient).
//  3. A malformed event with neither HubID nor RecipientUserID must
//     be dropped by fanout — no subscriber receives it.
func TestBrokerUserAudienceRouting(t *testing.T) {
	b := &Broker{
		subs: map[string]*Subscription{},
	}

	subA := &Subscription{
		ID:       "sub-a",
		UserID:   "alice",
		HubRoles: map[string]string{"hub-h": "owner"},
		Events:   make(chan Event, 4),
		broker:   b,
	}
	subB := &Subscription{
		ID:       "sub-b",
		UserID:   "bob",
		HubRoles: map[string]string{}, // not a member of any hub
		Events:   make(chan Event, 4),
		broker:   b,
	}
	b.subs[subA.ID] = subA
	b.subs[subB.ID] = subB

	drain := func(ch chan Event) []Event {
		out := []Event{}
		for {
			select {
			case e := <-ch:
				out = append(out, e)
			default:
				return out
			}
		}
	}

	t.Run("user-audience event reaches only the addressed subscriber", func(t *testing.T) {
		b.fanout(Event{
			Type:            "notification.created",
			RecipientUserID: "bob",
		})
		a := drain(subA.Events)
		if len(a) != 0 {
			t.Errorf("subA received %d events for a user-audience event addressed to bob; want 0", len(a))
		}
		bEvents := drain(subB.Events)
		if len(bEvents) != 1 {
			t.Errorf("subB received %d events for a user-audience event addressed to bob; want 1", len(bEvents))
		}
	})

	t.Run("hub-audience event reaches only hub members", func(t *testing.T) {
		b.fanout(Event{
			Type:  EventTypeHubMemoriesChanged,
			HubID: "hub-h",
		})
		a := drain(subA.Events)
		if len(a) != 1 {
			t.Errorf("subA (owner of hub-h) received %d events for a hub-audience event on hub-h; want 1", len(a))
		}
		bEvents := drain(subB.Events)
		if len(bEvents) != 0 {
			t.Errorf("subB (not a hub-h member) received %d events for a hub-audience event on hub-h; want 0", len(bEvents))
		}
	})

	t.Run("event with neither routing identity is dropped", func(t *testing.T) {
		b.fanout(Event{
			Type: "notification.created",
			// No HubID, no RecipientUserID — fanout must drop.
		})
		a := drain(subA.Events)
		bEvents := drain(subB.Events)
		if len(a) != 0 || len(bEvents) != 0 {
			t.Errorf("routing-less event leaked: subA=%d subB=%d; want 0/0", len(a), len(bEvents))
		}
	})

	t.Run("user-audience event does not leak to unrelated hub membership", func(t *testing.T) {
		// Alice is a member of hub-h. A user-audience event addressed
		// to alice must reach her, but ONLY because she is the
		// recipient — not because she happens to share any hub with
		// the event producer. subB is the negative control: even if
		// bob were a hub-h member (he is not), he should still NOT
		// receive an event addressed to alice.
		b.fanout(Event{
			Type:            "notification.created",
			RecipientUserID: "alice",
		})
		a := drain(subA.Events)
		if len(a) != 1 {
			t.Errorf("subA received %d events for a user-audience event addressed to alice; want 1", len(a))
		}
		bEvents := drain(subB.Events)
		if len(bEvents) != 0 {
			t.Errorf("subB received %d events for a user-audience event addressed to alice; want 0", len(bEvents))
		}
	})

	// REGRESSION: the broker's hub fan-out path ignored
	// evt.HubMemberRole, so an "admin-only" hub_member notification
	// would still reach every hub member over SSE, letting
	// contributors / viewers see a flash of the row before their
	// client-side filter kicked in. The event envelope carries
	// HubMemberRole explicitly, and the broker now honors it.
	t.Run("hub_member event honors per-row role narrowing", func(t *testing.T) {
		// Re-seed subs fresh for clarity. subA = admin of hub-h,
		// subB = contributor of the same hub.
		subA.UserID = "ava"
		subA.HubRoles = map[string]string{"hub-h": "admin"}
		subB.UserID = "cody"
		subB.HubRoles = map[string]string{"hub-h": "contributor"}
		drain(subA.Events)
		drain(subB.Events)

		// Admin-only hub_member row: only ava should receive it.
		b.fanout(Event{
			Type:          "notification.created",
			Audience:      "hub_member",
			HubID:         "hub-h",
			HubMemberRole: "admin",
			Kind:          "system_notice",
		})
		adminGot := drain(subA.Events)
		if len(adminGot) != 1 {
			t.Errorf("admin received %d events for an admin-only hub_member event; want 1", len(adminGot))
		}
		contributorGot := drain(subB.Events)
		if len(contributorGot) != 0 {
			t.Errorf("contributor received %d events for an admin-only hub_member event; want 0 (SSE role-narrowing leak)", len(contributorGot))
		}

		// Untargeted hub_member row (HubMemberRole empty) should
		// reach every member regardless of role — preserves the
		// default fan-out behavior.
		b.fanout(Event{
			Type:     "notification.created",
			Audience: "hub_member",
			HubID:    "hub-h",
			Kind:     "hub_member_joined",
		})
		adminAllGot := drain(subA.Events)
		if len(adminAllGot) != 1 {
			t.Errorf("admin received %d events for an untargeted hub_member event; want 1", len(adminAllGot))
		}
		contributorAllGot := drain(subB.Events)
		if len(contributorAllGot) != 1 {
			t.Errorf("contributor received %d events for an untargeted hub_member event; want 1", len(contributorAllGot))
		}
	})

	// REGRESSION: the pre-fix producers wrote user-audience
	// notifications with BOTH RecipientUserID and HubID set — the
	// latter for UI display context. The old broker inferred
	// "user-direct" from HubID=="" and fell through to hub fan-out
	// when HubID was present, so every invite addressed to an
	// outside user leaked to every hub member via the hub axis.
	// The reporter's "why am I seeing my own invite?" bug was this
	// exact scenario: Jiahao invited ziyang to hub H, the event
	// carried HubID=H and RecipientUserID=ziyang, and Jiahao (owner
	// of H) received it via the hub axis fall-through.
	//
	// This test pins the fix: explicit Audience="user" must route
	// on recipient only, regardless of whether HubID is set for
	// display context.
	t.Run("user-audience with hub_id decoration does not fan out to hub members", func(t *testing.T) {
		// Rename subA to "jiahao" for narrative clarity — he is the
		// hub owner here, not the addressed invitee.
		subA.UserID = "jiahao"
		subB.UserID = "ziyang"
		// Drain anything still buffered from prior sub-tests.
		drain(subA.Events)
		drain(subB.Events)

		b.fanout(Event{
			Type:            "notification.created",
			Audience:        "user",
			HubID:           "hub-h", // decoration, NOT a routing key
			RecipientUserID: "ziyang",
			Kind:            "hub_invite",
		})

		jiahao := drain(subA.Events)
		if len(jiahao) != 0 {
			t.Errorf("hub owner received %d events for a user-audience hub_invite addressed to ziyang; want 0 (cross-hub leak)", len(jiahao))
		}
		ziyang := drain(subB.Events)
		if len(ziyang) != 1 {
			t.Errorf("ziyang received %d events for a user-audience hub_invite addressed to him; want 1", len(ziyang))
		}
	})
}
