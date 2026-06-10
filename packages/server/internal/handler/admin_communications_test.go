package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// commsTestStore wraps InMemoryStore with in-memory implementations of
// the communications surface. The real InMemoryStore stubs return
// errCommunicationsRequiresPostgres; these overrides let handler tests
// exercise the real lifecycle without a Postgres dependency.
type commsTestStore struct {
	*store.InMemoryStore
	mu         sync.Mutex
	audiences  map[string]*model.Audience
	campaigns  map[string]*model.Campaign
	audit      []model.CommsAuditEntry
	users      map[string]*model.User
	totalUsers int
}

func newCommsTestStore() *commsTestStore {
	return &commsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		audiences:     map[string]*model.Audience{},
		campaigns:     map[string]*model.Campaign{},
		users:         map[string]*model.User{},
	}
}

// --- user helpers the handler depends on ---

func (s *commsTestStore) addUser(u *model.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *u
	s.users[u.ID] = &cp
	s.totalUsers++
}

func (s *commsTestStore) GetUser(id string) (*model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return u, nil
}

func (s *commsTestStore) GetUserByCanonicalEmail(email string) (*model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := strings.ToLower(strings.TrimSpace(email))
	for _, u := range s.users {
		if strings.ToLower(strings.TrimSpace(u.Email)) == target {
			return u, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (s *commsTestStore) ListUsers(_ context.Context, opts model.AdminUserListOpts) ([]model.User, string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	limit := opts.Limit
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out, "", s.totalUsers, nil
}

// --- audiences ---

func (s *commsTestStore) CreateAudience(_ context.Context, a *model.Audience) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	cp := *a
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.audiences[cp.ID] = &cp
	return nil
}

func (s *commsTestStore) GetAudience(_ context.Context, id string) (*model.Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.audiences[id]
	if !ok {
		return nil, store.ErrAudienceNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *commsTestStore) ListAudiences(_ context.Context) ([]model.Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Audience, 0, len(s.audiences))
	for _, a := range s.audiences {
		out = append(out, *a)
	}
	return out, nil
}

func (s *commsTestStore) UpdateAudience(_ context.Context, a *model.Audience) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.audiences[a.ID]; !ok {
		return store.ErrAudienceNotFound
	}
	cp := *a
	cp.UpdatedAt = time.Now().UTC()
	s.audiences[a.ID] = &cp
	return nil
}

func (s *commsTestStore) DeleteAudience(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.audiences[id]; !ok {
		return store.ErrAudienceNotFound
	}
	delete(s.audiences, id)
	return nil
}

// --- campaigns ---

func (s *commsTestStore) CreateCampaign(_ context.Context, c *model.Campaign) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	cp := *c
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.campaigns[cp.ID] = &cp
	return nil
}

func (s *commsTestStore) GetCampaign(_ context.Context, id string) (*model.Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return nil, store.ErrCampaignNotFound
	}
	cp := *c
	return &cp, nil
}

func (s *commsTestStore) ListCampaigns(_ context.Context, opts model.CampaignListOpts) ([]model.Campaign, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Campaign, 0, len(s.campaigns))
	for _, c := range s.campaigns {
		if opts.Status != "" && c.Status != opts.Status {
			continue
		}
		out = append(out, *c)
	}
	return out, "", nil
}

func (s *commsTestStore) UpdateCampaignDraft(_ context.Context, id string, patch store.CampaignDraftPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != model.CampaignStatusDraft {
		return store.ErrCampaignStateMismatch
	}
	if patch.Name != nil {
		c.Name = *patch.Name
	}
	if patch.AudienceID != nil {
		if *patch.AudienceID == "" {
			c.AudienceID = nil
		} else {
			v := *patch.AudienceID
			c.AudienceID = &v
		}
	}
	if patch.AudienceSnapshot != nil {
		c.AudienceSnapshot = patch.AudienceSnapshot
	}
	if patch.Content != nil {
		c.Content = patch.Content
	}
	c.UpdatedBy = &patch.UpdatedBy
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *commsTestStore) DeleteCampaign(_ context.Context, id string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != model.CampaignStatusDraft && c.Status != model.CampaignStatusCancelled {
		return store.ErrCampaignStateMismatch
	}
	delete(s.campaigns, id)
	return nil
}

func (s *commsTestStore) ScheduleCampaign(_ context.Context, id string, scheduledAt time.Time, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != model.CampaignStatusDraft {
		return store.ErrCampaignStateMismatch
	}
	c.Status = model.CampaignStatusScheduled
	c.ScheduledAt = &scheduledAt
	c.UpdatedBy = &actorID
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *commsTestStore) CancelCampaign(_ context.Context, id string, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != model.CampaignStatusDraft && c.Status != model.CampaignStatusScheduled {
		return store.ErrCampaignStateMismatch
	}
	c.Status = model.CampaignStatusCancelled
	c.CancelledBy = &actorID
	c.UpdatedBy = &actorID
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *commsTestStore) ClaimCampaignForSend(_ context.Context, id, expectedStatus string, startedAt time.Time, batchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != expectedStatus {
		return store.ErrCampaignStateMismatch
	}
	c.Status = model.CampaignStatusSending
	c.SendStartedAt = &startedAt
	c.BatchID = &batchID
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *commsTestStore) RevertCampaignToDraft(_ context.Context, id, fromStatus string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != fromStatus {
		return store.ErrCampaignStateMismatch
	}
	c.Status = model.CampaignStatusDraft
	c.ScheduledAt = nil
	c.SendStartedAt = nil
	c.BatchID = nil
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *commsTestStore) FinalizeCampaignSend(_ context.Context, id string, sentCount, failedCount int, finishedAt time.Time, errorMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return store.ErrCampaignNotFound
	}
	if c.Status != model.CampaignStatusSending {
		return store.ErrCampaignStateMismatch
	}
	c.SentCount = sentCount
	c.FailedCount = failedCount
	c.SendFinishedAt = &finishedAt
	if errorMsg != "" {
		c.Status = model.CampaignStatusFailed
		c.ErrorMessage = &errorMsg
	} else {
		c.Status = model.CampaignStatusSent
	}
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// --- audit ---

func (s *commsTestStore) AppendCommsAudit(_ context.Context, entry *model.CommsAuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *entry
	cp.CreatedAt = time.Now().UTC()
	s.audit = append(s.audit, cp)
	return nil
}

func (s *commsTestStore) ListCommsAudit(_ context.Context, resourceType, resourceID string) ([]model.CommsAuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.CommsAuditEntry, 0)
	for _, e := range s.audit {
		if e.ResourceType == resourceType && e.ResourceID == resourceID {
			out = append(out, e)
		}
	}
	return out, nil
}

// --- helpers ---

func withAdminUser(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), userIDKey, userID)
	return r.WithContext(ctx)
}

func decodeData(t *testing.T, body []byte, target any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, string(body))
	}
	if err := json.Unmarshal(env.Data, target); err != nil {
		t.Fatalf("decode data: %v; body=%s", err, string(env.Data))
	}
}

// --- tests ---

func TestAdminAudiences_CRUD(t *testing.T) {
	s := newCommsTestStore()
	s.addUser(&model.User{ID: "11111111-1111-1111-1111-111111111111", Email: "admin@memax.app"})
	h := NewAdminAudiencesHandler(s)

	// Create users audience.
	createBody := map[string]any{
		"name":      "Beta",
		"rule_type": "users",
		"rule":      map[string]any{"user_ids": []string{"11111111-1111-1111-1111-111111111111"}},
	}
	raw, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/audiences", bytes.NewReader(raw))
	req = withAdminUser(req, "11111111-1111-1111-1111-111111111111")
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d, body=%s", w.Code, w.Body.String())
	}
	var created model.Audience
	decodeData(t, w.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatalf("create: empty id")
	}

	// List.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/admin/audiences", nil)
	h.List(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", w.Code)
	}
	var listed struct {
		Audiences []model.Audience `json:"audiences"`
	}
	decodeData(t, w.Body.Bytes(), &listed)
	if len(listed.Audiences) != 1 {
		t.Fatalf("list: want 1 audience, got %d", len(listed.Audiences))
	}

	// Delete.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/v1/admin/audiences/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	req = withAdminUser(req, "11111111-1111-1111-1111-111111111111")
	h.Delete(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", w.Code)
	}
}

func TestAdminAudiences_EstimateAllUsers(t *testing.T) {
	s := newCommsTestStore()
	adminID := "11111111-1111-1111-1111-111111111111"
	s.addUser(&model.User{ID: adminID})
	s.addUser(&model.User{ID: uuid.New().String()})
	s.addUser(&model.User{ID: uuid.New().String()})

	h := NewAdminAudiencesHandler(s)

	body := map[string]any{
		"snapshot": map[string]any{"rule_type": "all", "rule": map[string]any{}},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/audiences/estimate", bytes.NewReader(raw))
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Estimate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("estimate all: want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	var est model.AudienceEstimate
	decodeData(t, w.Body.Bytes(), &est)
	if est.Count != 3 {
		t.Fatalf("estimate all: want count 3, got %d", est.Count)
	}
}

func TestAdminCampaigns_SendLocksDraft(t *testing.T) {
	s := newCommsTestStore()
	adminID := uuid.New().String()
	recipientID := uuid.New().String()
	s.addUser(&model.User{ID: adminID, Email: "admin@memax.app"})
	s.addUser(&model.User{ID: recipientID, Email: "user@memax.app"})

	var enqueueCalls int
	var enqueuedAt *time.Time
	h := NewAdminCampaignsHandler(s)
	h.SetEnqueue(func(_ context.Context, _, _ string, scheduledAt *time.Time) error {
		enqueueCalls++
		enqueuedAt = scheduledAt
		return nil
	})

	campaign := createInlineDraft(t, h, s, adminID, recipientID)

	// Send-now must CAS draft → sending before enqueue so edits and
	// deletes are blocked while the River job is outstanding.
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns/"+campaign.ID+"/send", bytes.NewReader([]byte("{}")))
	req.SetPathValue("id", campaign.ID)
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Send(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("send: want 202, got %d, body=%s", w.Code, w.Body.String())
	}
	if enqueueCalls != 1 || enqueuedAt != nil {
		t.Fatalf("send: want 1 immediate enqueue, got calls=%d at=%v", enqueueCalls, enqueuedAt)
	}

	after, _ := s.GetCampaign(context.Background(), campaign.ID)
	if after.Status != model.CampaignStatusSending {
		t.Fatalf("send: want status=sending after /send, got %s", after.Status)
	}
	if after.BatchID == nil || *after.BatchID == "" {
		t.Fatalf("send: want batch_id populated, got nil/empty")
	}
	if after.SendStartedAt == nil {
		t.Fatalf("send: want send_started_at populated, got nil")
	}

	// Editing a sending campaign must 409.
	patchRaw, _ := json.Marshal(map[string]string{"name": "after-send rename"})
	req = httptest.NewRequest(http.MethodPatch, "/v1/admin/campaigns/"+campaign.ID, bytes.NewReader(patchRaw))
	req.SetPathValue("id", campaign.ID)
	req = withAdminUser(req, adminID)
	w = httptest.NewRecorder()
	h.UpdateDraft(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("patch after send: want 409, got %d body=%s", w.Code, w.Body.String())
	}

	// Deleting a sending campaign must 409.
	req = httptest.NewRequest(http.MethodDelete, "/v1/admin/campaigns/"+campaign.ID, nil)
	req.SetPathValue("id", campaign.ID)
	req = withAdminUser(req, adminID)
	w = httptest.NewRecorder()
	h.Delete(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete after send: want 409, got %d body=%s", w.Code, w.Body.String())
	}

	// Audit should carry both creation and send-requested actions.
	entries, err := s.ListCommsAudit(context.Background(), model.CommsAuditResourceCampaign, campaign.ID)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if !containsAction(entries, model.CommsAuditActionCreated) {
		t.Fatalf("audit missing 'created'; got %+v", entries)
	}
	if !containsAction(entries, model.CommsAuditActionSent) {
		t.Fatalf("audit missing 'send_requested'; got %+v", entries)
	}
}

func TestAdminCampaigns_SendEnqueueFailureReverts(t *testing.T) {
	s := newCommsTestStore()
	adminID := uuid.New().String()
	recipientID := uuid.New().String()
	s.addUser(&model.User{ID: adminID})
	s.addUser(&model.User{ID: recipientID})

	h := NewAdminCampaignsHandler(s)
	h.SetEnqueue(func(_ context.Context, _, _ string, _ *time.Time) error {
		return fmt.Errorf("simulated queue outage")
	})

	campaign := createInlineDraft(t, h, s, adminID, recipientID)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns/"+campaign.ID+"/send", bytes.NewReader([]byte("{}")))
	req.SetPathValue("id", campaign.ID)
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Send(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("send enqueue fail: want 500, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := s.GetCampaign(context.Background(), campaign.ID)
	if after.Status != model.CampaignStatusDraft {
		t.Fatalf("send enqueue fail: want status reverted to draft, got %s", after.Status)
	}
	if after.BatchID != nil {
		t.Fatalf("send enqueue fail: want batch_id cleared, got %q", *after.BatchID)
	}
	if after.SendStartedAt != nil {
		t.Fatalf("send enqueue fail: want send_started_at cleared, got %v", after.SendStartedAt)
	}
}

func TestAdminCampaigns_ScheduleEnqueueFailureReverts(t *testing.T) {
	s := newCommsTestStore()
	adminID := uuid.New().String()
	recipientID := uuid.New().String()
	s.addUser(&model.User{ID: adminID})
	s.addUser(&model.User{ID: recipientID})

	h := NewAdminCampaignsHandler(s)
	h.SetEnqueue(func(_ context.Context, _, _ string, scheduledAt *time.Time) error {
		if scheduledAt == nil {
			t.Fatalf("schedule should set scheduledAt")
		}
		return fmt.Errorf("simulated queue outage")
	})

	campaign := createInlineDraft(t, h, s, adminID, recipientID)

	target := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]string{"scheduled_at": target})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns/"+campaign.ID+"/schedule", bytes.NewReader(body))
	req.SetPathValue("id", campaign.ID)
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Schedule(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("schedule enqueue fail: want 500, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := s.GetCampaign(context.Background(), campaign.ID)
	if after.Status != model.CampaignStatusDraft {
		t.Fatalf("schedule enqueue fail: want status reverted to draft, got %s", after.Status)
	}
	if after.ScheduledAt != nil {
		t.Fatalf("schedule enqueue fail: want scheduled_at cleared, got %v", after.ScheduledAt)
	}
}

func TestAdminCampaigns_UpdateResolvesSavedAudience(t *testing.T) {
	s := newCommsTestStore()
	adminID := uuid.New().String()
	recipientA := uuid.New().String()
	recipientB := uuid.New().String()
	s.addUser(&model.User{ID: adminID})
	s.addUser(&model.User{ID: recipientA})
	s.addUser(&model.User{ID: recipientB})

	audH := NewAdminAudiencesHandler(s)
	audienceID := createSavedAudience(t, audH, adminID, "Team A", recipientA)

	h := NewAdminCampaignsHandler(s)
	h.SetEnqueue(func(_ context.Context, _, _ string, _ *time.Time) error { return nil })

	campaign := createInlineDraft(t, h, s, adminID, recipientB)
	// Baseline: the inline draft snapshots recipientB.
	before, _ := s.GetCampaign(context.Background(), campaign.ID)
	if before.AudienceSnapshot == nil || len(before.AudienceSnapshot.Rule.UserIDs) != 1 ||
		before.AudienceSnapshot.Rule.UserIDs[0] != recipientB {
		t.Fatalf("baseline: want inline snapshot targeting recipientB, got %+v", before.AudienceSnapshot)
	}

	// Patch to the saved audience, sending only audience_id.
	patchBody, _ := json.Marshal(map[string]any{"audience_id": audienceID})
	req := httptest.NewRequest(http.MethodPatch, "/v1/admin/campaigns/"+campaign.ID, bytes.NewReader(patchBody))
	req.SetPathValue("id", campaign.ID)
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.UpdateDraft(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: want 200, got %d body=%s", w.Code, w.Body.String())
	}

	after, _ := s.GetCampaign(context.Background(), campaign.ID)
	if after.AudienceID == nil || *after.AudienceID != audienceID {
		t.Fatalf("patch: want audience_id=%s, got %v", audienceID, after.AudienceID)
	}
	if after.AudienceSnapshot == nil {
		t.Fatalf("patch: want audience_snapshot populated from saved audience")
	}
	if after.AudienceSnapshot.AudienceID == nil || *after.AudienceSnapshot.AudienceID != audienceID {
		t.Fatalf("patch: want snapshot.audience_id=%s, got %+v",
			audienceID, after.AudienceSnapshot.AudienceID)
	}
	if len(after.AudienceSnapshot.Rule.UserIDs) != 1 ||
		after.AudienceSnapshot.Rule.UserIDs[0] != recipientA {
		t.Fatalf("patch: want snapshot rule refreshed to recipientA, got %+v",
			after.AudienceSnapshot.Rule)
	}
}

// createInlineDraft is a helper: POST /admin/campaigns with an inline
// users audience targeting one recipient. Returns the freshly created
// campaign.
func createInlineDraft(t *testing.T, h *AdminCampaignsHandler, s *commsTestStore, adminID, recipientID string) model.Campaign {
	t.Helper()
	content, _ := json.Marshal(map[string]any{
		"title": "Heads up",
		"body":  "Update inside.",
	})
	body := map[string]any{
		"name":    "test campaign",
		"kind":    model.NotificationKindSystemNotice,
		"channel": "inapp",
		"audience_snapshot": map[string]any{
			"rule_type": "users",
			"rule":      map[string]any{"user_ids": []string{recipientID}},
		},
		"content": json.RawMessage(content),
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns", bytes.NewReader(raw))
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createInlineDraft: want 201, got %d body=%s", w.Code, w.Body.String())
	}
	var c model.Campaign
	decodeData(t, w.Body.Bytes(), &c)
	if c.Status != model.CampaignStatusDraft {
		t.Fatalf("createInlineDraft: want draft, got %s", c.Status)
	}
	// Quick sanity on test-store persistence.
	if _, err := s.GetCampaign(context.Background(), c.ID); err != nil {
		t.Fatalf("createInlineDraft: stored campaign unreadable: %v", err)
	}
	return c
}

func createSavedAudience(t *testing.T, h *AdminAudiencesHandler, adminID, name, userID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":      name,
		"rule_type": "users",
		"rule":      map[string]any{"user_ids": []string{userID}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/audiences", bytes.NewReader(body))
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createSavedAudience: want 201, got %d body=%s", w.Code, w.Body.String())
	}
	var a model.Audience
	decodeData(t, w.Body.Bytes(), &a)
	return a.ID
}

func TestAdminCampaigns_ScheduleThenCancel(t *testing.T) {
	s := newCommsTestStore()
	adminID := uuid.New().String()
	s.addUser(&model.User{ID: adminID})
	s.addUser(&model.User{ID: uuid.New().String()})

	h := NewAdminCampaignsHandler(s)
	var scheduled []*time.Time
	h.SetEnqueue(func(_ context.Context, _, _ string, at *time.Time) error {
		scheduled = append(scheduled, at)
		return nil
	})

	// Seed a draft directly.
	content, _ := json.Marshal(map[string]any{"title": "Test"})
	id := uuid.New().String()
	aid := uuid.New().String()
	s.campaigns[id] = &model.Campaign{
		ID:      id,
		Name:    "Later",
		Kind:    model.NotificationKindSystemNotice,
		Channel: model.CampaignChannelInapp,
		Status:  model.CampaignStatusDraft,
		Content: content,
		AudienceSnapshot: &model.AudienceSnapshot{
			RuleType: model.AudienceRuleUsers,
			Rule:     model.AudienceRule{UserIDs: []string{aid}},
		},
		CreatedBy: adminID,
	}

	// Schedule for 1 hour from now.
	target := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(map[string]string{"scheduled_at": target})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns/"+id+"/schedule", bytes.NewReader(raw))
	req.SetPathValue("id", id)
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.Schedule(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("schedule: want 202, got %d, body=%s", w.Code, w.Body.String())
	}
	if len(scheduled) != 1 || scheduled[0] == nil {
		t.Fatalf("schedule: want one scheduled enqueue, got %v", scheduled)
	}

	// Cancel it.
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns/"+id+"/cancel", bytes.NewReader([]byte("{}")))
	req.SetPathValue("id", id)
	req = withAdminUser(req, adminID)
	w = httptest.NewRecorder()
	h.Cancel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel: want 200, got %d, body=%s", w.Code, w.Body.String())
	}

	after, _ := s.GetCampaign(context.Background(), id)
	if after.Status != model.CampaignStatusCancelled {
		t.Fatalf("cancel: want status cancelled, got %s", after.Status)
	}

	// Attempting to send a cancelled campaign must 409.
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns/"+id+"/send", bytes.NewReader([]byte("{}")))
	req.SetPathValue("id", id)
	req = withAdminUser(req, adminID)
	w = httptest.NewRecorder()
	h.Send(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("send cancelled: want 409, got %d", w.Code)
	}
}

func TestAdminCampaigns_UpdateDraftRejectsNonDraft(t *testing.T) {
	s := newCommsTestStore()
	adminID := uuid.New().String()
	s.addUser(&model.User{ID: adminID})

	h := NewAdminCampaignsHandler(s)
	id := uuid.New().String()
	content, _ := json.Marshal(map[string]any{"title": "x"})
	s.campaigns[id] = &model.Campaign{
		ID:      id,
		Name:    "frozen",
		Kind:    model.NotificationKindSystemNotice,
		Channel: model.CampaignChannelInapp,
		Status:  model.CampaignStatusSent,
		Content: content,
	}

	newName := "should not apply"
	raw, _ := json.Marshal(map[string]any{"name": newName})
	req := httptest.NewRequest(http.MethodPatch, "/v1/admin/campaigns/"+id, bytes.NewReader(raw))
	req.SetPathValue("id", id)
	req = withAdminUser(req, adminID)
	w := httptest.NewRecorder()
	h.UpdateDraft(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("update sent campaign: want 409, got %d, body=%s", w.Code, w.Body.String())
	}
}

func containsAction(entries []model.CommsAuditEntry, action string) bool {
	for _, e := range entries {
		if e.Action == action {
			return true
		}
	}
	return false
}
