package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// nilPublisher satisfies events.Publisher for handler tests — publish
// is a no-op, close reports no error.
type nilPublisher struct{}

func (nilPublisher) Publish(_ context.Context, _ events.Event) error { return nil }
func (nilPublisher) Close() error                                    { return nil }

// ─── Unauthenticated paths ──────────────────────────────────────
//
// All notification endpoints require a valid userID from the auth
// middleware. Hitting them without one must return 401 before any
// store work, and without leaking state into events.

func TestNotificationsHandlerRejectsUnauthenticated(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	h := NewNotificationsHandler(s, nil)

	cases := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"List", http.MethodGet, "/v1/notifications", h.List},
		{"Summary", http.MethodGet, "/v1/notifications/summary", h.Summary},
		{"MarkSeen", http.MethodPost, "/v1/notifications/abc/seen", h.MarkSeen},
		{"Dismiss", http.MethodPost, "/v1/notifications/abc/dismiss", h.Dismiss},
		{"Resolve", http.MethodPost, "/v1/notifications/abc/resolve", h.Resolve},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			// No InjectUserIDForTest — userID is empty.
			rec := httptest.NewRecorder()
			c.handler(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: expected 401, got %d: %s", c.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// ─── 404 path: GetNotification returns ErrNotificationNotFound ─
//
// The InMemoryStore's GetNotification is a stub that always returns
// ErrNotificationNotFound. That lets us exercise the 404 branch of
// Dismiss/Resolve without spinning up a real DB.

func TestNotificationsDismissReturns404ForMissingNotification(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	h := NewNotificationsHandler(s, nilPublisher{})

	req := httptest.NewRequest(http.MethodPost, "/v1/notifications/missing/dismiss", nil)
	req = InjectUserIDForTest(req, "user-1")
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()

	h.Dismiss(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ─── Summary happy path: returns empty-but-well-formed payload ─
//
// InMemoryStore returns an empty NotificationSummary. Handler must
// wrap it in the ApiResponse envelope — no 500, no nil-pointer panic.

func TestNotificationsSummaryEmpty(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	h := NewNotificationsHandler(s, nilPublisher{})

	req := httptest.NewRequest(http.MethodGet, "/v1/notifications/summary", nil)
	req = InjectUserIDForTest(req, "user-1")
	rec := httptest.NewRecorder()

	h.Summary(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty JSON body")
	}
}

// ─── MarkSeen happy path: idempotent; InMemoryStore stub succeeds ──
//
// MarkNotificationSeen returns nil. The subsequent GetNotification
// returns ErrNotificationNotFound so the event publish is skipped —
// not an error.

func TestNotificationsMarkSeenSucceedsEvenWhenGetFails(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	h := NewNotificationsHandler(s, nilPublisher{})

	req := httptest.NewRequest(http.MethodPost, "/v1/notifications/abc/seen", nil)
	req = InjectUserIDForTest(req, "user-1")
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.MarkSeen(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ─── SetBilling / SetOwnershipResolver / SetInvalidateUserPlan ──
//
// Tiny coverage setters that shouldn't panic when called.

func TestNotificationsHandlerSetters(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	h := NewNotificationsHandler(s, nilPublisher{})

	h.SetInvalidateUserPlan(nil)
	h.SetBilling(nil)
	h.SetOwnershipResolver(nil)
	// no panic == pass
}
