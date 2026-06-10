package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// reconcileTestStore stubs the slice of Store that the reconcile
// handler exercises. Kept minimal; everything else embeds
// InMemoryStore to satisfy the interface.
type reconcileTestStore struct {
	*store.InMemoryStore
	users    map[string]*model.User
	orphans  []model.OrphanWaitlistCandidate
	listErr  error
	gotOpts  model.OrphanWaitlistCandidateOpts
	getCalls int
}

func (s *reconcileTestStore) GetUser(id string) (*model.User, error) {
	s.getCalls++
	u, ok := s.users[id]
	if !ok {
		return nil, store.ErrHubNotFound // reuse any not-found sentinel
	}
	cp := *u
	return &cp, nil
}

func (s *reconcileTestStore) ListOrphanWaitlistCandidates(_ context.Context, opts model.OrphanWaitlistCandidateOpts) ([]model.OrphanWaitlistCandidate, error) {
	s.gotOpts = opts
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.orphans, nil
}

// GetOrphanCandidateForUser stubs the user-scoped lookup by filtering
// the orphans slice. Mirrors the Postgres implementation's contract:
// (nil, nil) when the user isn't in the cohort.
func (s *reconcileTestStore) GetOrphanCandidateForUser(_ context.Context, userID string) (*model.OrphanWaitlistCandidate, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	for i := range s.orphans {
		if s.orphans[i].UserID == userID {
			cp := s.orphans[i]
			return &cp, nil
		}
	}
	return nil, nil
}

// fakeInviteConsumer records each call + optionally mutates the user
// (e.g. upgrading the in-memory user's PersonalPlanID) to simulate
// the real ConsumeInviteForUser's plan transition. The tests inspect
// the captured calls to assert behavior.
type fakeInviteConsumer struct {
	calls     []fakeConsumerCall
	planAfter string
}

type fakeConsumerCall struct {
	Token  string
	UserID string
}

func (f *fakeInviteConsumer) ConsumeInviteForUser(_ context.Context, token string, user *model.User) {
	f.calls = append(f.calls, fakeConsumerCall{Token: token, UserID: user.ID})
	if f.planAfter != "" {
		user.PersonalPlanID = f.planAfter
	}
}

func newReconcileTestStore() *reconcileTestStore {
	return &reconcileTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		users:         map[string]*model.User{},
	}
}

// TestAdminGetUserOrphanStatusReturnsCandidateWhenOrphan covers
// the user-detail crosslink endpoint. Without this the admin
// user-detail page can't conditionally render the reconcile card
// without loading the bulk list.
func TestAdminGetUserOrphanStatusReturnsCandidateWhenOrphan(t *testing.T) {
	s := newReconcileTestStore()
	s.orphans = []model.OrphanWaitlistCandidate{{
		UserID:    "u1",
		UserEmail: "a@b",
		InviteID:  "inv1",
	}}
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/u1/orphan-status", nil)
	req.SetPathValue("id", "u1")
	rec := httptest.NewRecorder()
	h.GetUserOrphanStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data AdminUserOrphanStatusResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.IsOrphan {
		t.Fatalf("expected is_orphan=true, got false")
	}
	if resp.Data.Candidate == nil || resp.Data.Candidate.InviteID != "inv1" {
		t.Fatalf("expected invite_id=inv1, got %+v", resp.Data.Candidate)
	}
}

func TestAdminGetUserOrphanStatusFalseWhenNotOrphan(t *testing.T) {
	s := newReconcileTestStore() // empty orphans slice
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/u-other/orphan-status", nil)
	req.SetPathValue("id", "u-other")
	rec := httptest.NewRecorder()
	h.GetUserOrphanStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Data AdminUserOrphanStatusResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.IsOrphan {
		t.Fatalf("expected is_orphan=false for non-orphan user")
	}
	if resp.Data.Candidate != nil {
		t.Fatalf("candidate should be omitted when !is_orphan, got %+v", resp.Data.Candidate)
	}
}

func TestAdminGetUserOrphanStatusRequiresID(t *testing.T) {
	s := newReconcileTestStore()
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users//orphan-status", nil)
	// No SetPathValue → id is empty.
	rec := httptest.NewRecorder()
	h.GetUserOrphanStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing id, got %d", rec.Code)
	}
}

// TestAdminListOrphansRequiresSinceParam locks down the
// narrow-the-scan contract: without ?since, the endpoint must refuse
// so a dashboard load never triggers a full-table scan by accident.
func TestAdminListOrphansRequiresSinceParam(t *testing.T) {
	s := newReconcileTestStore()
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/waitlist/orphans", nil)
	rec := httptest.NewRecorder()
	h.ListOrphans(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without since, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("missing_since")) {
		t.Fatalf("expected missing_since error code, got %s", rec.Body.String())
	}
}

func TestAdminListOrphansRejectsInvalidSince(t *testing.T) {
	s := newReconcileTestStore()
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/waitlist/orphans?since=yesterday", nil)
	rec := httptest.NewRecorder()
	h.ListOrphans(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unparseable since, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("invalid_since")) {
		t.Fatalf("expected invalid_since error code, got %s", rec.Body.String())
	}
}

func TestAdminListOrphansForwardsSinceAndLimit(t *testing.T) {
	s := newReconcileTestStore()
	when := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	s.orphans = []model.OrphanWaitlistCandidate{
		{UserID: "u1", UserEmail: "a@b", InviteID: "inv1", UserCreatedAt: when, InviteExpiresAt: when.Add(72 * time.Hour)},
	}
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/waitlist/orphans?since=2026-04-14T00:00:00Z&limit=7", nil)
	rec := httptest.NewRecorder()
	h.ListOrphans(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !s.gotOpts.Since.Equal(when) {
		t.Fatalf("since not forwarded: got %v", s.gotOpts.Since)
	}
	if s.gotOpts.Limit != 7 {
		t.Fatalf("limit not forwarded: got %d", s.gotOpts.Limit)
	}

	var resp struct {
		Data AdminOrphanCandidatesResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Candidates) != 1 || resp.Data.Candidates[0].UserID != "u1" {
		t.Fatalf("unexpected candidates: %#v", resp.Data.Candidates)
	}
	if resp.Data.Scanned != 1 {
		t.Fatalf("scanned=%d, want 1", resp.Data.Scanned)
	}
}

// TestAdminListOrphansHidesTokenInJSON covers the model annotation
// `json:"-"` on InviteToken — operators shouldn't see raw waitlist
// tokens in admin responses.
func TestAdminListOrphansHidesTokenInJSON(t *testing.T) {
	s := newReconcileTestStore()
	s.orphans = []model.OrphanWaitlistCandidate{
		{UserID: "u1", UserEmail: "a@b", InviteID: "inv1", InviteToken: "SECRET-TOKEN-abc123"},
	}
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/waitlist/orphans?since=2026-04-14T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ListOrphans(rec, req)

	if bytes.Contains(rec.Body.Bytes(), []byte("SECRET-TOKEN-abc123")) {
		t.Fatalf("raw invite token leaked into JSON response: %s", rec.Body.String())
	}
}

// TestAdminReconcileRequiresUserIDs locks down the
// "no reconcile-everything shorthand" guard. Operators must always
// hand the endpoint the specific ids they want to repair.
func TestAdminReconcileRequiresUserIDs(t *testing.T) {
	s := newReconcileTestStore()
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty user_ids, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("missing_user_ids")) {
		t.Fatalf("expected missing_user_ids error code, got %s", rec.Body.String())
	}
}

func TestAdminReconcileRejectsBatchOverLimit(t *testing.T) {
	s := newReconcileTestStore()
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	ids := make([]string, reconcileBatchLimit+1)
	for i := range ids {
		ids[i] = "u" + string(rune('a'+i%26))
	}
	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: ids})

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized batch, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("too_many_user_ids")) {
		t.Fatalf("expected too_many_user_ids error, got %s", rec.Body.String())
	}
}

func TestAdminReconcileInvokesConsumerForMatchingOrphan(t *testing.T) {
	s := newReconcileTestStore()
	now := time.Now().UTC()
	s.users["u1"] = &model.User{
		ID:             "u1",
		Email:          "a@b",
		CreatedAt:      now.Add(-48 * time.Hour),
		PersonalPlanID: model.PersonalFreePlanID,
	}
	s.orphans = []model.OrphanWaitlistCandidate{{
		UserID:          "u1",
		UserEmail:       "a@b",
		InviteID:        "inv1",
		InviteToken:     "TOKEN",
		InviteExpiresAt: now.Add(72 * time.Hour),
		UserCreatedAt:   s.users["u1"].CreatedAt,
	}}

	fake := &fakeInviteConsumer{planAfter: model.PersonalEarlyAccessPlanID}
	// Flip the stored user's plan after ConsumeInviteForUser fires so
	// the handler's GetUser after the call observes the upgrade.
	// fakeInviteConsumer mutates the caller-passed *User but the
	// handler re-fetches from the store; mirror the real planChanger
	// behavior by updating the store too.
	originalConsume := fake.ConsumeInviteForUser
	fake.calls = nil
	// We can't easily monkey-patch method receivers in Go; instead,
	// after the handler runs we inspect the fake's captured calls
	// and separately update s.users["u1"] to simulate the live
	// ChangePlan side effect. The handler's GetUser-after-consume
	// will then reflect the upgrade.
	_ = originalConsume
	fake2 := &planUpgradingConsumer{
		inner:     fake,
		store:     s,
		planAfter: model.PersonalEarlyAccessPlanID,
	}

	h := NewAdminWaitlistReconcileHandler(s, fake2)

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"u1"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 ConsumeInviteForUser call, got %d", len(fake.calls))
	}
	if fake.calls[0].Token != "TOKEN" || fake.calls[0].UserID != "u1" {
		t.Fatalf("unexpected consume call: %+v", fake.calls[0])
	}

	var resp struct {
		Data AdminReconcileResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Reconciled != 1 || resp.Data.Skipped != 0 || resp.Data.Errored != 0 {
		t.Fatalf("unexpected roll-up: %+v", resp.Data)
	}
	r := resp.Data.Results[0]
	if r.Action != "reconciled" {
		t.Fatalf("action=%q, want reconciled", r.Action)
	}
	if r.BeforePlan != model.PersonalFreePlanID {
		t.Fatalf("before_plan=%q, want %q", r.BeforePlan, model.PersonalFreePlanID)
	}
	if r.AfterPlan != model.PersonalEarlyAccessPlanID {
		t.Fatalf("after_plan=%q, want %q", r.AfterPlan, model.PersonalEarlyAccessPlanID)
	}
	if r.InviteID != "inv1" {
		t.Fatalf("invite_id=%q, want inv1", r.InviteID)
	}
}

// TestAdminReconcileErrorsWhenConsumeDoesNotUpgradePlan covers
// the postcondition check: ConsumeInviteForUser silently swallows
// failures (e.g. planChanger.ChangePlan fails mid-flight), so the
// handler must re-fetch and verify the plan actually flipped. If
// it didn't, the result must surface as "error" so the operator
// investigates — not "reconciled", which would hide the partial
// failure.
func TestAdminReconcileErrorsWhenConsumeDoesNotUpgradePlan(t *testing.T) {
	s := newReconcileTestStore()
	now := time.Now().UTC()
	s.users["u1"] = &model.User{
		ID:             "u1",
		Email:          "a@b",
		CreatedAt:      now.Add(-48 * time.Hour),
		PersonalPlanID: model.PersonalFreePlanID,
	}
	s.orphans = []model.OrphanWaitlistCandidate{{
		UserID: "u1", InviteID: "inv1", InviteToken: "TOKEN",
		UserCreatedAt: s.users["u1"].CreatedAt,
	}}

	// Fake consumer does NOT mutate the stored user's plan,
	// simulating a planChanger partial failure.
	fake := &fakeInviteConsumer{}
	h := NewAdminWaitlistReconcileHandler(s, fake)

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"u1"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if len(fake.calls) != 1 {
		t.Fatalf("consumer should have fired once, got %d", len(fake.calls))
	}

	var resp struct {
		Data AdminReconcileResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Errored != 1 {
		t.Fatalf("expected errored=1 when plan fails to upgrade, got %+v", resp.Data)
	}
	r := resp.Data.Results[0]
	if r.Action != "error" {
		t.Fatalf("action=%q, want error", r.Action)
	}
	if r.Reason != "plan_upgrade_did_not_apply" {
		t.Fatalf("reason=%q, want plan_upgrade_did_not_apply", r.Reason)
	}
	if r.BeforePlan != model.PersonalFreePlanID || r.AfterPlan != model.PersonalFreePlanID {
		t.Fatalf("expected before/after to both be free, got before=%q after=%q", r.BeforePlan, r.AfterPlan)
	}
}

// TestAdminReconcileDistinguishesAlreadyUpgradedFromNonOrphan covers
// the reason-string refinement: when a user isn't returned by the
// orphan lookup AND they're already on personal_early_access, we
// report "already_upgraded" so the operator knows a concurrent
// writer repaired them — not "not_an_orphan" which would be
// ambiguous.
func TestAdminReconcileDistinguishesAlreadyUpgradedFromNonOrphan(t *testing.T) {
	s := newReconcileTestStore()
	s.users["u-pre"] = &model.User{
		ID:             "u-pre",
		Email:          "e@x",
		PersonalPlanID: model.PersonalEarlyAccessPlanID, // already upgraded
		CreatedAt:      time.Now().UTC(),
	}
	// No orphan row for this user.
	h := NewAdminWaitlistReconcileHandler(s, &fakeInviteConsumer{})

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"u-pre"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	var resp struct {
		Data AdminReconcileResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Skipped != 1 {
		t.Fatalf("expected skipped=1, got %+v", resp.Data)
	}
	if resp.Data.Results[0].Reason != "already_upgraded" {
		t.Fatalf("reason=%q, want already_upgraded", resp.Data.Results[0].Reason)
	}
}

func TestAdminReconcileSkipsNonOrphans(t *testing.T) {
	// User exists on the free plan but has no matching active
	// invite → skip with reason=not_an_orphan (distinct from the
	// already_upgraded branch exercised in
	// TestAdminReconcileDistinguishesAlreadyUpgradedFromNonOrphan).
	s := newReconcileTestStore()
	s.users["u2"] = &model.User{
		ID:             "u2",
		Email:          "c@d",
		PersonalPlanID: model.PersonalFreePlanID,
		CreatedAt:      time.Now().UTC().Add(-24 * time.Hour),
	}
	// No orphans returned → user-scoped lookup returns nil.
	fake := &fakeInviteConsumer{}
	h := NewAdminWaitlistReconcileHandler(s, fake)

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"u2"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("consumer should not fire for non-orphan: got %d calls", len(fake.calls))
	}

	var resp struct {
		Data AdminReconcileResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Skipped != 1 || resp.Data.Reconciled != 0 {
		t.Fatalf("expected skipped=1, got %+v", resp.Data)
	}
	if resp.Data.Results[0].Reason != "not_an_orphan" {
		t.Fatalf("reason=%q, want not_an_orphan", resp.Data.Results[0].Reason)
	}
}

func TestAdminReconcileErrorsOnMissingUser(t *testing.T) {
	s := newReconcileTestStore() // empty users map
	fake := &fakeInviteConsumer{}
	h := NewAdminWaitlistReconcileHandler(s, fake)

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"ghost"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	var resp struct {
		Data AdminReconcileResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Errored != 1 {
		t.Fatalf("expected errored=1, got %+v", resp.Data)
	}
	if resp.Data.Results[0].Reason != "user_not_found" {
		t.Fatalf("reason=%q, want user_not_found", resp.Data.Results[0].Reason)
	}
}

func TestAdminReconcileDeduplicatesWithinBatch(t *testing.T) {
	s := newReconcileTestStore()
	s.users["u1"] = &model.User{
		ID:             "u1",
		Email:          "a@b",
		PersonalPlanID: model.PersonalFreePlanID,
		CreatedAt:      time.Now().UTC(),
	}
	s.orphans = []model.OrphanWaitlistCandidate{{
		UserID: "u1", InviteID: "inv1", InviteToken: "T",
		UserCreatedAt: s.users["u1"].CreatedAt,
	}}
	fake := &fakeInviteConsumer{}
	fake2 := &planUpgradingConsumer{inner: fake, store: s, planAfter: model.PersonalEarlyAccessPlanID}
	h := NewAdminWaitlistReconcileHandler(s, fake2)

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"u1", "u1", "u1"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if len(fake.calls) != 1 {
		t.Fatalf("expected single consume call across 3 duplicated ids, got %d", len(fake.calls))
	}

	var resp struct {
		Data AdminReconcileResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Reconciled != 1 || resp.Data.Skipped != 2 {
		t.Fatalf("expected reconciled=1 + skipped=2, got %+v", resp.Data)
	}
}

// TestAdminReconcileFailsWithoutConsumer covers the serverapp wiring
// guard — if the handler is built without an invite consumer (e.g.
// test harness forgot to pass one) the endpoint must refuse rather
// than silently no-op. This is belt-and-braces; production always
// wires authH as the consumer.
func TestAdminReconcileFailsWithoutConsumer(t *testing.T) {
	s := newReconcileTestStore()
	h := NewAdminWaitlistReconcileHandler(s, nil)

	body, _ := json.Marshal(AdminReconcileRequest{UserIDs: []string{"u1"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/waitlist/reconcile",
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reconcile(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when consumer is nil, got %d", rec.Code)
	}
}

// planUpgradingConsumer wraps a fakeInviteConsumer and mutates the
// store after each call so the handler's post-consume GetUser sees
// the upgrade — mirroring the real planChanger.ChangePlan side
// effect. Extracted as a test helper because otherwise every
// positive-case test would need to replicate this shim inline.
type planUpgradingConsumer struct {
	inner     *fakeInviteConsumer
	store     *reconcileTestStore
	planAfter string
}

func (p *planUpgradingConsumer) ConsumeInviteForUser(ctx context.Context, token string, user *model.User) {
	p.inner.ConsumeInviteForUser(ctx, token, user)
	if p.planAfter != "" {
		if stored, ok := p.store.users[user.ID]; ok {
			stored.PersonalPlanID = p.planAfter
		}
	}
}
