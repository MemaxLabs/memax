package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type dreamsListTestStore struct {
	*store.InMemoryStore
	runs       []model.DreamRun
	nextCursor string
	gotOpts    store.DreamRunListForUserOpts
}

func (s *dreamsListTestStore) ListDreamRunsForUser(_ context.Context, opts store.DreamRunListForUserOpts) ([]model.DreamRun, string, error) {
	s.gotOpts = opts
	return s.runs, s.nextCursor, nil
}

func newDreamsListTestStore(runs []model.DreamRun) *dreamsListTestStore {
	return &dreamsListTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		runs:          runs,
	}
}

const (
	testHubID       = "11111111-1111-1111-1111-111111111111"
	testOtherHubID  = "22222222-2222-2222-2222-222222222222"
	testScopedHubID = "33333333-3333-3333-3333-333333333333"
)

// seedDreamsRequest builds a request with a user-scoped (all
// accessible) AuthContext that grants PermDreamRead on each of
// `accessHubs`. The scope model matches a normal web login: user
// has membership + default permissions on every hub.
func seedDreamsRequest(req *http.Request, userID string, accessHubs ...string) *http.Request {
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	permsByHub := make(map[string]PermissionSet, len(accessHubs))
	for _, h := range accessHubs {
		permsByHub[h] = NewPermissionSet(PermDreamRead)
	}
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID:           userID,
		PrincipalType:    "session",
		HubScopeMode:     HubScopeAllAccessible,
		PermissionsByHub: permsByHub,
	})
	return req.WithContext(ctx)
}

// seedHubScopedDreamsRequest builds a request with a hub-scoped
// credential — only ScopedHubIDs[0] is accessible regardless of
// what the caller asks for.
func seedHubScopedDreamsRequest(req *http.Request, userID, scopedHubID string) *http.Request {
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID:        userID,
		PrincipalType: "oauth_grant",
		HubScopeMode:  HubScopeAllowlist,
		ScopedHubIDs:  []string{scopedHubID},
		PermissionsByHub: map[string]PermissionSet{
			scopedHubID: NewPermissionSet(PermDreamRead),
		},
	})
	return req.WithContext(ctx)
}

func TestDreamsListSpansAllHubsWhenNoHubGiven(t *testing.T) {
	// Bare request: no ?hub_id, no X-Hub-ID. The handler must
	// pass HubID="" to the store so ListDreamRunsForUser's own
	// "hubs owned by user OR in hub_members" filter runs. If the
	// handler fell back to the middleware-populated personal hub,
	// the store query would silently collapse to personal-only
	// and the "All hubs" default would be misleading.
	s := newDreamsListTestStore([]model.DreamRun{
		{ID: "run-a", OwnerID: "u1", HubID: testHubID},
		{ID: "run-b", OwnerID: "u1", HubID: testOtherHubID},
	})
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/dreams", nil)
	req = seedDreamsRequest(req, "u1", testHubID, testOtherHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if s.gotOpts.HubID != "" {
		t.Fatalf("no hub context should forward empty HubID to store, got %q", s.gotOpts.HubID)
	}
	if s.gotOpts.UserID != "u1" {
		t.Fatalf("UserID not forwarded: %q", s.gotOpts.UserID)
	}
	// Limit parsed as 0 when absent so the store default (20)
	// fires — keeps the public contract honest with the SDK doc.
	if s.gotOpts.Limit != 0 {
		t.Fatalf("expected Limit=0 (store defaults to 20), got %d", s.gotOpts.Limit)
	}
}

func TestDreamsListScopesToExplicitQueryHubID(t *testing.T) {
	s := newDreamsListTestStore(nil)
	s.nextCursor = "2026-04-20T10:00:00Z|aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/dreams?hub_id="+testHubID+"&limit=20&cursor=2026-04-20T12%3A00%3A00Z%7Cbbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		nil,
	)
	req = seedDreamsRequest(req, "u1", testHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if s.gotOpts.HubID != testHubID {
		t.Fatalf("expected HubID=%s, got %q", testHubID, s.gotOpts.HubID)
	}
	if s.gotOpts.Cursor != "2026-04-20T12:00:00Z|bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" {
		t.Fatalf("cursor not forwarded, got %q", s.gotOpts.Cursor)
	}
	if s.gotOpts.Limit != 20 {
		t.Fatalf("expected Limit=20, got %d", s.gotOpts.Limit)
	}

	var resp struct {
		Data model.DreamRunListResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.NextCursor != s.nextCursor {
		t.Fatalf("next_cursor not surfaced: got %q want %q", resp.Data.NextCursor, s.nextCursor)
	}
}

func TestDreamsListFallsBackToXHubIDHeader(t *testing.T) {
	// The legacy SDK/CLI pass the hub via the X-Hub-ID request
	// header. The handler must honor that even when ?hub_id is
	// absent so pre-3.7 callers keep working.
	s := newDreamsListTestStore(nil)
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/dreams", nil)
	req.Header.Set("X-Hub-ID", testHubID)
	req = seedDreamsRequest(req, "u1", testHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if s.gotOpts.HubID != testHubID {
		t.Fatalf("X-Hub-ID header not forwarded, got %q", s.gotOpts.HubID)
	}
}

func TestDreamsListRejectsNonMemberHub(t *testing.T) {
	// AuthContext grants PermDreamRead on testHubID; caller asks
	// for testOtherHubID which isn't in PermissionsByHub → Can()
	// returns false → 403. This is the non-member path.
	s := newDreamsListTestStore(nil)
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/dreams?hub_id="+testOtherHubID, nil)
	req = seedDreamsRequest(req, "u1", testHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d: %s", rec.Code, rec.Body.String())
	}
	if s.gotOpts.UserID != "" {
		t.Fatalf("store must not be queried on forbidden path, got opts=%+v", s.gotOpts)
	}
}

// TestDreamsListRejectsScopedCredentialCrossHubBypass covers the
// Codex 3.7 High finding: a hub-scoped credential must not be
// able to read another hub's runs via an explicit ?hub_id=
// query, even when the underlying user is also a member of that
// other hub.
func TestDreamsListRejectsScopedCredentialCrossHubBypass(t *testing.T) {
	s := newDreamsListTestStore(nil)
	h := NewDreamsHandler(s, nil)

	// Credential is scoped to testScopedHubID. User asks for
	// testHubID via query param (a hub they're a member of
	// through their account, but not via this credential).
	req := httptest.NewRequest(http.MethodGet, "/v1/dreams?hub_id="+testHubID, nil)
	req = seedHubScopedDreamsRequest(req, "u1", testScopedHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (scoped hub is forced), got %d: %s", rec.Code, rec.Body.String())
	}
	if s.gotOpts.HubID != testScopedHubID {
		t.Fatalf("hub-scoped credential must force HubID to its scoped hub; got %q, want %q", s.gotOpts.HubID, testScopedHubID)
	}
}

// Belt-and-braces: if a hub-scoped credential comes in without
// specifying any hub, the handler still forces the scope hub
// rather than letting the store's "all user hubs" path leak
// data beyond the grant.
func TestDreamsListScopedCredentialIgnoresImplicitAllHubs(t *testing.T) {
	s := newDreamsListTestStore(nil)
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/dreams", nil)
	req = seedHubScopedDreamsRequest(req, "u1", testScopedHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if s.gotOpts.HubID != testScopedHubID {
		t.Fatalf("scoped credential must narrow to scoped hub even with no query param; got %q", s.gotOpts.HubID)
	}
}

func TestDreamsListRejectsInvalidLimit(t *testing.T) {
	s := newDreamsListTestStore(nil)
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/dreams?limit=abc", nil)
	req = seedDreamsRequest(req, "u1", testHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDreamsListForwardsStartedAtUnaffected(t *testing.T) {
	// Smoke: non-empty runs flow back through the response body
	// with the right shape. Belt-and-braces regression for the
	// envelope rewrite (array → {runs, next_cursor}).
	now := time.Now().UTC()
	s := newDreamsListTestStore([]model.DreamRun{
		{
			ID:                "run-1",
			OwnerID:           "u1",
			HubID:             testHubID,
			Status:            "completed",
			StartedAt:         now,
			FinishedAt:        now.Add(2 * time.Minute),
			MemoriesScanned:   42,
			MemoriesOrganized: 8,
			Report:            "done",
		},
	})
	h := NewDreamsHandler(s, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/dreams?hub_id="+testHubID, nil)
	req = seedDreamsRequest(req, "u1", testHubID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.DreamRunListResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Runs) != 1 || resp.Data.Runs[0].ID != "run-1" {
		t.Fatalf("unexpected response runs: %#v", resp.Data.Runs)
	}
}
