package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// adminNotifTestStore extends InMemoryStore with the methods needed by
// AdminNotificationsHandler tests.
type adminNotifTestStore struct {
	*store.InMemoryStore
	users        map[string]*model.User
	usersByEmail map[string]*model.User
	hubMembers   map[string][]model.HubMember
	notifs       []*model.Notification
}

func newAdminNotifTestStore() *adminNotifTestStore {
	return &adminNotifTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		users:         make(map[string]*model.User),
		usersByEmail:  make(map[string]*model.User),
		hubMembers:    make(map[string][]model.HubMember),
	}
}

func (s *adminNotifTestStore) addUser(u *model.User) {
	copy := *u
	s.users[u.ID] = &copy
	if email := strings.ToLower(strings.TrimSpace(u.Email)); email != "" {
		s.usersByEmail[email] = &copy
	}
}

func (s *adminNotifTestStore) GetUser(id string) (*model.User, error) {
	u, ok := s.users[id]
	if !ok {
		return nil, http.ErrMissingFile
	}
	return u, nil
}

func (s *adminNotifTestStore) GetUserByCanonicalEmail(email string) (*model.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	u, ok := s.usersByEmail[key]
	if !ok {
		return nil, http.ErrMissingFile
	}
	return u, nil
}

func (s *adminNotifTestStore) ListHubMembers(hubID string) ([]model.HubMember, error) {
	return s.hubMembers[hubID], nil
}

func (s *adminNotifTestStore) CreateNotification(n *model.Notification) error {
	copy := *n
	s.notifs = append(s.notifs, &copy)
	return nil
}

func (s *adminNotifTestStore) ListUsers(_ context.Context, opts model.AdminUserListOpts) ([]model.User, string, int, error) {
	var users []model.User
	for _, u := range s.users {
		users = append(users, *u)
	}
	// Simple pagination stub: no cursor support, just return all
	limit := opts.Limit
	if limit <= 0 || limit > len(users) {
		limit = len(users)
	}
	return users[:limit], "", len(users), nil
}

func (s *adminNotifTestStore) QueryAdminNotificationBatches(_ context.Context, _ store.AdminNotifBatchOpts) ([]store.AdminNotifBatchRow, error) {
	// Aggregate notifs by batch prefix
	batches := make(map[string]*store.AdminNotifBatchRow)
	for _, n := range s.notifs {
		parts := strings.SplitN(n.SourceID, ":", 2)
		if len(parts) < 2 {
			continue
		}
		batchID := parts[0]
		if b, ok := batches[batchID]; ok {
			b.RecipientCount++
		} else {
			preview := ""
			var m map[string]any
			if err := json.Unmarshal(n.Payload, &m); err == nil {
				if t, ok := m["title"].(string); ok {
					preview = t
				}
			}
			batches[batchID] = &store.AdminNotifBatchRow{
				BatchID:        batchID,
				Kind:           n.Kind,
				PayloadPreview: preview,
				RecipientCount: 1,
				CreatedAt:      n.CreatedAt,
			}
		}
	}
	var result []store.AdminNotifBatchRow
	for _, b := range batches {
		result = append(result, *b)
	}
	return result, nil
}

func withAdminIdentity(req *http.Request, userID string) *http.Request {
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	return req.WithContext(ctx)
}

// --- Tests ---

func TestAdminNotifSendToSpecificUsers(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "alice@example.com", Name: "Alice"})
	s.addUser(&model.User{ID: "u2", Email: "bob@example.com", Name: "Bob"})

	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "system_notice",
		"targeting": {
			"mode": "users",
			"user_ids": ["u1"],
			"emails": ["bob@example.com"]
		},
		"payload": {"title": "Test notice", "body": "Hello world"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resp.Data)
	var result struct {
		BatchID      string   `json:"batch_id"`
		SentCount    int      `json:"sent_count"`
		FailedEmails []string `json:"failed_emails"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}

	if result.SentCount != 2 {
		t.Fatalf("expected sent_count=2, got %d", result.SentCount)
	}
	if result.BatchID == "" {
		t.Fatal("expected non-empty batch_id")
	}
	if len(result.FailedEmails) != 0 {
		t.Fatalf("expected no failed emails, got %v", result.FailedEmails)
	}

	// Verify notification rows
	if len(s.notifs) != 2 {
		t.Fatalf("expected 2 notification rows, got %d", len(s.notifs))
	}
	for _, n := range s.notifs {
		if n.Audience != model.AudienceUser {
			t.Errorf("expected audience=user, got %s", n.Audience)
		}
		if n.Kind != model.NotificationKindSystemNotice {
			t.Errorf("expected kind=system_notice, got %s", n.Kind)
		}
		if n.SourceKind != model.NotificationSourceSystemNotice {
			t.Errorf("expected source_kind=system_notice, got %s", n.SourceKind)
		}
		if !strings.HasPrefix(n.SourceID, result.BatchID+":") {
			t.Errorf("expected source_id to start with batch_id, got %s", n.SourceID)
		}
	}
}

func TestAdminNotifSendWithFailedEmails(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "alice@example.com", Name: "Alice"})

	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "system_notice",
		"targeting": {
			"mode": "users",
			"emails": ["alice@example.com", "nobody@example.com"]
		},
		"payload": {"title": "Test", "body": "Hello"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, _ := json.Marshal(resp.Data)
	var result struct {
		SentCount    int      `json:"sent_count"`
		FailedEmails []string `json:"failed_emails"`
	}
	json.Unmarshal(raw, &result)

	if result.SentCount != 1 {
		t.Fatalf("expected sent_count=1, got %d", result.SentCount)
	}
	if len(result.FailedEmails) != 1 || result.FailedEmails[0] != "nobody@example.com" {
		t.Fatalf("expected failed_emails=[nobody@example.com], got %v", result.FailedEmails)
	}
}

func TestAdminNotifSendHubTargeted(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "owner@example.com", Name: "Owner"})
	s.addUser(&model.User{ID: "u2", Email: "member@example.com", Name: "Member"})
	s.addUser(&model.User{ID: "u3", Email: "viewer@example.com", Name: "Viewer"})
	s.hubMembers["hub1"] = []model.HubMember{
		{HubID: "hub1", UserID: "u1", Role: "owner"},
		{HubID: "hub1", UserID: "u2", Role: "contributor"},
		{HubID: "hub1", UserID: "u3", Role: "viewer"},
	}

	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "system_notice",
		"targeting": {
			"mode": "hub",
			"hub_id": "hub1"
		},
		"payload": {"title": "Hub update", "body": "Something changed"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// All 3 hub members should get per-user notifications
	if len(s.notifs) != 3 {
		t.Fatalf("expected 3 notifications (per-user fanout), got %d", len(s.notifs))
	}

	recipients := make(map[string]bool)
	for _, n := range s.notifs {
		recipients[n.RecipientUserID] = true
		if n.Audience != model.AudienceUser {
			t.Errorf("expected audience=user (per-user fanout), got %s", n.Audience)
		}
	}
	if !recipients["u1"] || !recipients["u2"] || !recipients["u3"] {
		t.Fatalf("expected all 3 hub members, got %v", recipients)
	}
}

func TestAdminNotifSendHubTargetedWithRoleFilter(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "owner@example.com", Name: "Owner"})
	s.addUser(&model.User{ID: "u2", Email: "member@example.com", Name: "Member"})
	s.hubMembers["hub1"] = []model.HubMember{
		{HubID: "hub1", UserID: "u1", Role: "owner"},
		{HubID: "hub1", UserID: "u2", Role: "contributor"},
	}

	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "system_notice",
		"targeting": {
			"mode": "hub",
			"hub_id": "hub1",
			"hub_member_role": "owner"
		},
		"payload": {"title": "Owner only", "body": "VIP notice"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(s.notifs) != 1 {
		t.Fatalf("expected 1 notification (owner only), got %d", len(s.notifs))
	}
	if s.notifs[0].RecipientUserID != "u1" {
		t.Fatalf("expected recipient=u1 (owner), got %s", s.notifs[0].RecipientUserID)
	}
}

func TestAdminNotifSendAllUsersBroadcast(t *testing.T) {
	s := newAdminNotifTestStore()
	h := NewAdminNotificationsHandler(s)

	var broadcastCalled bool
	var broadcastKind, broadcastBatchID, broadcastAdminID string
	h.SetEnqueueBroadcast(func(kind string, _ json.RawMessage, batchID, adminID string) error {
		broadcastCalled = true
		broadcastKind = kind
		broadcastBatchID = batchID
		broadcastAdminID = adminID
		return nil
	})

	body := `{
		"kind": "gift_invite_link",
		"targeting": {"mode": "all"},
		"payload": {"sender": {"id": "", "display": "memax"}, "token": "gift-123", "expires_at": "2026-05-01T00:00:00Z"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if !broadcastCalled {
		t.Fatal("expected broadcast job to be enqueued")
	}
	if broadcastKind != "gift_invite_link" {
		t.Fatalf("expected kind=gift_invite_link, got %s", broadcastKind)
	}
	if broadcastBatchID == "" {
		t.Fatal("expected non-empty batch_id")
	}
	if broadcastAdminID != "admin1" {
		t.Fatalf("expected admin_id=admin1, got %s", broadcastAdminID)
	}
	// No inline notifications for broadcast mode
	if len(s.notifs) != 0 {
		t.Fatalf("expected 0 inline notifications (async broadcast), got %d", len(s.notifs))
	}
}

func TestAdminNotifSendGiftToUser(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "alice@example.com", Name: "Alice"})

	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "gift_invite_link",
		"targeting": {"mode": "users", "user_ids": ["u1"]},
		"payload": {"sender": {"id": "admin1", "display": "memax team"}, "token": "gift-abc", "url": "https://memax.app/gift/redeem?code=gift-abc", "expires_at": "2026-05-01T00:00:00Z"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(s.notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(s.notifs))
	}
	n := s.notifs[0]
	if n.Kind != model.NotificationKindGiftInviteLink {
		t.Fatalf("expected kind=gift_invite_link, got %s", n.Kind)
	}
	if n.SourceKind != model.NotificationSourceGift {
		t.Fatalf("expected source_kind=gift, got %s", n.SourceKind)
	}
}

func TestAdminNotifListSent(t *testing.T) {
	s := newAdminNotifTestStore()
	// Seed some notifications with batch source_id format
	s.notifs = []*model.Notification{
		{ID: "n1", Audience: model.AudienceUser, RecipientUserID: "u1", Kind: "system_notice", SourceKind: "system_notice", SourceID: "batch1:u1", Payload: json.RawMessage(`{"title":"Test"}`), CreatedAt: time.Now()},
		{ID: "n2", Audience: model.AudienceUser, RecipientUserID: "u2", Kind: "system_notice", SourceKind: "system_notice", SourceID: "batch1:u2", Payload: json.RawMessage(`{"title":"Test"}`), CreatedAt: time.Now()},
	}

	h := NewAdminNotificationsHandler(s)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/notifications/sent", nil)
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.ListSent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, _ := json.Marshal(resp.Data)
	var result struct {
		Batches []struct {
			BatchID        string `json:"batch_id"`
			Kind           string `json:"kind"`
			RecipientCount int    `json:"recipient_count"`
			PayloadPreview string `json:"payload_preview"`
		} `json:"batches"`
	}
	json.Unmarshal(raw, &result)

	if len(result.Batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(result.Batches))
	}
	batch := result.Batches[0]
	if batch.BatchID != "batch1" {
		t.Fatalf("expected batch_id=batch1, got %s", batch.BatchID)
	}
	if batch.RecipientCount != 2 {
		t.Fatalf("expected recipient_count=2, got %d", batch.RecipientCount)
	}
	if batch.Kind != "system_notice" {
		t.Fatalf("expected kind=system_notice, got %s", batch.Kind)
	}
	if batch.PayloadPreview != "Test" {
		t.Fatalf("expected payload_preview=Test, got %s", batch.PayloadPreview)
	}
}

func TestAdminNotifSendInvalidKind(t *testing.T) {
	s := newAdminNotifTestStore()
	h := NewAdminNotificationsHandler(s)

	body := `{"kind": "invalid", "targeting": {"mode": "users", "user_ids": ["u1"]}, "payload": {}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_kind") {
		t.Fatalf("expected invalid_kind error, got %s", rec.Body.String())
	}
}

func TestAdminNotifSendNoRecipients(t *testing.T) {
	s := newAdminNotifTestStore()
	h := NewAdminNotificationsHandler(s)

	body := `{"kind": "system_notice", "targeting": {"mode": "users", "emails": ["nobody@example.com"]}, "payload": {"title": "x"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no_recipients") {
		t.Fatalf("expected no_recipients error, got %s", rec.Body.String())
	}
}

func TestAdminNotifBroadcastEnqueueFailure(t *testing.T) {
	s := newAdminNotifTestStore()
	h := NewAdminNotificationsHandler(s)

	h.SetEnqueueBroadcast(func(_ string, _ json.RawMessage, _, _ string) error {
		return fmt.Errorf("queue connection refused")
	})

	body := `{
		"kind": "system_notice",
		"targeting": {"mode": "all"},
		"payload": {"title": "Test", "body": "Hello"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "enqueue_failed") {
		t.Fatalf("expected enqueue_failed error, got %s", rec.Body.String())
	}
}

func TestAdminNotifInvalidSystemNoticePayload(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "a@example.com", Name: "A"})
	h := NewAdminNotificationsHandler(s)

	tests := []struct {
		name    string
		payload string
		errSub  string
	}{
		{
			name:    "empty title and body",
			payload: `{"title": "", "body": ""}`,
			errSub:  "requires at least a title or body",
		},
		{
			name:    "missing both fields",
			payload: `{}`,
			errSub:  "requires at least a title or body",
		},
		{
			name:    "not a JSON object",
			payload: `"just a string"`,
			errSub:  "Payload must be a JSON object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"kind": "system_notice", "targeting": {"mode": "users", "user_ids": ["u1"]}, "payload": ` + tt.payload + `}`
			req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
			req = withAdminIdentity(req, "admin1")
			rec := httptest.NewRecorder()

			h.Send(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.errSub) {
				t.Fatalf("expected error containing %q, got %s", tt.errSub, rec.Body.String())
			}
		})
	}
}

func TestAdminNotifInvalidGiftPayload(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "a@example.com", Name: "A"})
	h := NewAdminNotificationsHandler(s)

	tests := []struct {
		name    string
		payload string
		errSub  string
	}{
		{
			name:    "missing token",
			payload: `{"sender": {"id": "x", "display": "admin"}, "expires_at": "2026-05-01T00:00:00Z"}`,
			errSub:  "requires a token",
		},
		{
			name:    "missing sender",
			payload: `{"token": "abc", "expires_at": "2026-05-01T00:00:00Z"}`,
			errSub:  "requires sender",
		},
		{
			name:    "missing expires_at",
			payload: `{"sender": {"id": "x", "display": "admin"}, "token": "abc"}`,
			errSub:  "requires expires_at",
		},
		{
			name:    "empty sender display and id",
			payload: `{"sender": {"id": "", "display": ""}, "token": "abc", "expires_at": "2026-05-01T00:00:00Z"}`,
			errSub:  "requires sender",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"kind": "gift_invite_link", "targeting": {"mode": "users", "user_ids": ["u1"]}, "payload": ` + tt.payload + `}`
			req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
			req = withAdminIdentity(req, "admin1")
			rec := httptest.NewRecorder()

			h.Send(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.errSub) {
				t.Fatalf("expected error containing %q, got %s", tt.errSub, rec.Body.String())
			}
		})
	}
}

func TestAdminNotifBroadcastSuccessPath(t *testing.T) {
	s := newAdminNotifTestStore()
	h := NewAdminNotificationsHandler(s)

	var enqueuedKind, enqueuedBatchID string
	h.SetEnqueueBroadcast(func(kind string, _ json.RawMessage, batchID, _ string) error {
		enqueuedKind = kind
		enqueuedBatchID = batchID
		return nil
	})

	body := `{
		"kind": "system_notice",
		"targeting": {"mode": "all"},
		"payload": {"title": "Product launch", "body": "We shipped v2!"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if enqueuedKind != "system_notice" {
		t.Fatalf("expected enqueued kind=system_notice, got %s", enqueuedKind)
	}

	var resp model.ApiResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, _ := json.Marshal(resp.Data)
	var result struct {
		BatchID string `json:"batch_id"`
	}
	json.Unmarshal(raw, &result)

	if result.BatchID == "" || result.BatchID != enqueuedBatchID {
		t.Fatalf("batch_id mismatch: response=%s, enqueued=%s", result.BatchID, enqueuedBatchID)
	}
}

func TestAdminNotifSendInvalidUserIDs(t *testing.T) {
	s := newAdminNotifTestStore()
	// Only u1 exists; u-bogus does not
	s.addUser(&model.User{ID: "u1", Email: "a@example.com", Name: "A"})
	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "system_notice",
		"targeting": {"mode": "users", "user_ids": ["u-bogus", "u-also-bogus"]},
		"payload": {"title": "Test", "body": "Hello"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	// All user_ids are invalid → no valid recipients → 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no_recipients") {
		t.Fatalf("expected no_recipients error, got %s", rec.Body.String())
	}
}

func TestAdminNotifSendMixedValidInvalidUserIDs(t *testing.T) {
	s := newAdminNotifTestStore()
	s.addUser(&model.User{ID: "u1", Email: "a@example.com", Name: "A"})
	h := NewAdminNotificationsHandler(s)

	body := `{
		"kind": "system_notice",
		"targeting": {"mode": "users", "user_ids": ["u1", "u-bogus"]},
		"payload": {"title": "Test", "body": "Hello"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notifications/send", bytes.NewReader([]byte(body)))
	req = withAdminIdentity(req, "admin1")
	rec := httptest.NewRecorder()

	h.Send(rec, req)

	// u1 is valid, u-bogus is dropped → 200 with sent_count=1
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	raw, _ := json.Marshal(resp.Data)
	var result struct {
		SentCount int `json:"sent_count"`
	}
	json.Unmarshal(raw, &result)

	if result.SentCount != 1 {
		t.Fatalf("expected sent_count=1 (u-bogus dropped), got %d", result.SentCount)
	}
}
