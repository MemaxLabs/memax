package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	eventsAPI "github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Phase 3.2 chat-session CRUD endpoint tests.
//
// Run against a live Postgres at DATABASE_URL — InMemoryStore
// can't model the chat invariants (the foundation tests already
// pinned that decision). Each test creates a disposable user
// + hub, then exercises the handler's HTTP surface end-to-end
// (json decode → handler → store → reply json).

type chatTestEnv struct {
	pool  *pgxpool.Pool
	store store.Store
	owner string
	hubA  string
	hubB  string
	other string // a SECOND user, used for owner-isolation checks
}

func setupChatHandlerEnv(t *testing.T) (*handler.ChatHandler, *chatTestEnv) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping chat handler test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	s := store.NewPostgresStore(pool)
	owner := uuid.NewString()
	other := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'C', 'personal_early_access', now(), now()),
		        ($3::uuid, $4, 'O', 'personal_early_access', now(), now())`,
		owner, "ch-"+owner[:8]+"@x", other, "oth-"+other[:8]+"@x"); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	hubA := uuid.NewString()
	hubB := uuid.NewString()
	if err := s.CreateHub(&model.Hub{ID: hubA, Name: "A", Slug: "a-" + hubA[:6], HubType: "personal", OwnerID: owner}); err != nil {
		t.Fatalf("CreateHub A: %v", err)
	}
	if err := s.CreateHub(&model.Hub{ID: hubB, Name: "B", Slug: "b-" + hubB[:6], HubType: "personal", OwnerID: owner}); err != nil {
		t.Fatalf("CreateHub B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`,
			[]string{owner, other})
	})

	env := &chatTestEnv{
		pool: pool, store: s, owner: owner, other: other,
		hubA: hubA, hubB: hubB,
	}
	return handler.NewChatHandler(s), env
}

// reqWithCaller builds a request with the user-id + accessible-
// hubs slots populated, mirroring what the auth middleware
// would have set in production.
func reqWithCaller(t *testing.T, method, path string, body any, userID string, hubs []string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req = handler.InjectUserIDForTest(req, userID)
	req = handler.InjectAccessibleHubIDsForTest(req, hubs)
	permsByHub := make(map[string]handler.PermissionSet, len(hubs))
	for _, hubID := range hubs {
		permsByHub[hubID] = handler.DefaultUserPermissions()
	}
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:           userID,
		HubScopeMode:     handler.HubScopeAllAccessible,
		PermissionsByHub: permsByHub,
	})
	return req
}

func decodeChatSession(t *testing.T, body []byte) *model.ChatSession {
	t.Helper()
	var resp struct {
		Data model.ChatSession `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode session: %v (body=%q)", err, body)
	}
	return &resp.Data
}

func chatTestContainsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// --- Create ---

func TestChatHandler_Create_SingleHub(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		Title:       "First chat",
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories"},
		Model:       "claude-sonnet-4-6",
	}, env.owner, []string{env.hubA, env.hubB})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got := decodeChatSession(t, rec.Body.Bytes())
	if got.Title != "First chat" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.ScopeType != model.ChatScopeTypeSingleHub {
		t.Errorf("ScopeType = %q", got.ScopeType)
	}
	if len(got.ScopeHubIDs) != 1 || got.ScopeHubIDs[0] != env.hubA {
		t.Errorf("ScopeHubIDs = %v", got.ScopeHubIDs)
	}
	if got.Status != model.ChatSessionStatusIdle {
		t.Errorf("Status = %q", got.Status)
	}
	if got.ToolsetVersion != 1 {
		t.Errorf("ToolsetVersion = %d, want 1", got.ToolsetVersion)
	}
}

func TestChatHandler_Create_AppliesDefaults(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got := decodeChatSession(t, rec.Body.Bytes())
	// Default model + default tools.
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("default Model = %q, want claude-sonnet-4-6", got.Model)
	}
	if len(got.Tools) == 0 {
		t.Errorf("default Tools should not be empty")
	}
}

func TestChatHandler_Create_RejectsInvalidShape(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	cases := []struct {
		name string
		req  handler.CreateChatSessionRequest
		want int
	}{
		{"empty scope_type", handler.CreateChatSessionRequest{}, http.StatusBadRequest},
		{"single_hub with 0 hubs",
			handler.CreateChatSessionRequest{ScopeType: model.ChatScopeTypeSingleHub},
			http.StatusBadRequest},
		{"single_hub with 2 hubs",
			handler.CreateChatSessionRequest{ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{env.hubA, env.hubB}},
			http.StatusBadRequest},
		{"user_all_hubs with hubs",
			handler.CreateChatSessionRequest{ScopeType: model.ChatScopeTypeUserAllHubs, ScopeHubIDs: []string{env.hubA}},
			http.StatusBadRequest},
		{"hub_subset with 0 hubs",
			handler.CreateChatSessionRequest{ScopeType: model.ChatScopeTypeHubSubset},
			http.StatusBadRequest},
		{"unknown model",
			handler.CreateChatSessionRequest{ScopeType: model.ChatScopeTypeUserAllHubs, Model: "made-up-model"},
			http.StatusBadRequest},
		{"title too long",
			handler.CreateChatSessionRequest{ScopeType: model.ChatScopeTypeUserAllHubs, Title: strings.Repeat("a", 201)},
			http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := reqWithCaller(t, "POST", "/v1/chat/sessions", tc.req, env.owner, []string{env.hubA, env.hubB})
			h.Create(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestChatHandler_Create_RejectsHubNotInAccessibleSet(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	// Caller's accessible hubs only contain hubA, but request
	// pins hubB → 403.
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubB},
	}, env.owner, []string{env.hubA})
	h.Create(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_Create_RejectsWriteHubOutsideScope(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		WriteHubID:  env.hubB, // not in scope
	}, env.owner, []string{env.hubA, env.hubB})
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (write_hub outside scope) body=%s", rec.Code, rec.Body.String())
	}
}

// user_all_hubs sessions allow any accessible hub as the
// write_hub_id. Pinning hubA as the write target should succeed
// even though scope_hub_ids is empty.
func TestChatHandler_Create_UserAllHubsAcceptsWriteHubInAccessible(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:  model.ChatScopeTypeUserAllHubs,
		WriteHubID: env.hubA,
	}, env.owner, []string{env.hubA, env.hubB})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
}

// --- Get ---

func TestChatHandler_Get_OwnedSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Create a session via the handler so we exercise the
	// full path (rather than direct store call).
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", createRec.Code)
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET", "/v1/chat/sessions/"+created.ID, nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestChatHandler_Get_OtherUsersSessionReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	// Create owned by env.owner.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	created := decodeChatSession(t, createRec.Body.Bytes())

	// GET as the OTHER user.
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET", "/v1/chat/sessions/"+created.ID, nil, env.other, []string{env.hubA, env.hubB})
	req.SetPathValue("id", created.ID)
	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner GET = %d, want 404", rec.Code)
	}
}

// --- List ---

func TestChatHandler_List_RecentFirst(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Create 3 sessions with appended user messages so each has a
	// last_message_at that orders them. We use the store directly
	// for the message append (the message-send handler arrives in
	// 3.3) — the goal here is the ORDER of the list, not the
	// message lifecycle.
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	mkSess := func(title string, ts time.Time) string {
		sess := &model.ChatSession{
			ID: uuid.NewString(), OwnerID: env.owner,
			ScopeType: model.ChatScopeTypeUserAllHubs, ScopeHubIDs: []string{},
			Title: title, Model: "claude-sonnet-4-6",
			Tools:  []string{"recall_memories"},
			Status: model.ChatSessionStatusIdle, ToolsetVersion: 1,
			CreatedAt: ts, UpdatedAt: ts,
		}
		if err := env.store.CreateChatSession(ctx, sess); err != nil {
			t.Fatalf("CreateChatSession: %v", err)
		}
		// Bump last_message_at by appending a user message at ts.
		msg := &model.ChatMessage{
			ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
			Role: model.ChatMessageRoleUser, AuthorUserID: env.owner,
			Status:  model.ChatMessageStatusCompleted,
			Content: title + " body", ToolsetVersionUsed: 1, CreatedAt: ts,
		}
		if err := env.store.AppendChatMessage(ctx, env.owner, msg); err != nil {
			t.Fatalf("AppendChatMessage: %v", err)
		}
		return sess.ID
	}
	idOldest := mkSess("oldest", now.Add(-2*time.Hour))
	idMiddle := mkSess("middle", now.Add(-1*time.Hour))
	idNewest := mkSess("newest", now)

	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET", "/v1/chat/sessions", nil, env.owner, []string{})
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Data struct {
			Sessions   []model.ChatSession `json:"sessions"`
			NextCursor string              `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Sessions) < 3 {
		t.Fatalf("expected at least 3 sessions, got %d", len(resp.Data.Sessions))
	}
	wantOrder := []string{idNewest, idMiddle, idOldest}
	for i, want := range wantOrder {
		if resp.Data.Sessions[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, resp.Data.Sessions[i].ID, want)
		}
	}
}

// TestChatHandler_List_ArchivedOnly pins the archive recovery
// surface contract: `?archived=true` returns ONLY rows with
// archived_at IS NOT NULL. The default list (no flag) excludes
// them. Without this end-to-end gate the sidebar's "Show
// archived" accordion silently renders empty after the user
// archives a session — exactly the bug the user reported.
func TestChatHandler_List_ArchivedOnly(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	mk := func(title string, ts time.Time, archived bool) string {
		sess := &model.ChatSession{
			ID: uuid.NewString(), OwnerID: env.owner,
			ScopeType: model.ChatScopeTypeUserAllHubs, ScopeHubIDs: []string{},
			Title: title, Model: "claude-sonnet-4-6",
			Tools:  []string{"recall_memories"},
			Status: model.ChatSessionStatusIdle, ToolsetVersion: 1,
			CreatedAt: ts, UpdatedAt: ts,
		}
		if err := env.store.CreateChatSession(ctx, sess); err != nil {
			t.Fatalf("CreateChatSession: %v", err)
		}
		if archived {
			at := ts
			if err := env.store.UpdateChatSessionMeta(ctx, sess.ID, env.owner, store.ChatSessionMetaPatch{
				ArchivedAt: &at,
			}); err != nil {
				t.Fatalf("UpdateChatSessionMeta(archive): %v", err)
			}
		}
		return sess.ID
	}
	activeID := mk("active", now, false)
	archivedID := mk("archived", now.Add(-1*time.Hour), true)

	// Default list (no flag) → active only.
	rec := httptest.NewRecorder()
	h.List(rec, reqWithCaller(t, "GET", "/v1/chat/sessions", nil, env.owner, []string{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("default list status = %d", rec.Code)
	}
	defaultResp := decodeListResponse(t, rec)
	if !containsSession(defaultResp, activeID) || containsSession(defaultResp, archivedID) {
		t.Errorf("default list = %v, want only %s", listIDs(defaultResp), activeID)
	}

	// `?archived=true` → archived only.
	rec = httptest.NewRecorder()
	h.List(rec, reqWithCaller(t, "GET", "/v1/chat/sessions?archived=true", nil, env.owner, []string{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("archived-only list status = %d", rec.Code)
	}
	archivedResp := decodeListResponse(t, rec)
	if containsSession(archivedResp, activeID) || !containsSession(archivedResp, archivedID) {
		t.Errorf("archived-only list = %v, want only %s", listIDs(archivedResp), archivedID)
	}

	// Legacy `?include=archived` → combined (admin/audit).
	rec = httptest.NewRecorder()
	h.List(rec, reqWithCaller(t, "GET", "/v1/chat/sessions?include=archived", nil, env.owner, []string{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("include=archived list status = %d", rec.Code)
	}
	includeResp := decodeListResponse(t, rec)
	if !containsSession(includeResp, activeID) || !containsSession(includeResp, archivedID) {
		t.Errorf("include=archived list = %v, want both %s and %s",
			listIDs(includeResp), activeID, archivedID)
	}

	// Locks the FULL parser surface: aliases, case-insensitivity,
	// invalid no-op, and precedence between the two query params.
	// Without these subcases a future "tighten the parser" cleanup
	// could accidentally narrow the accepted SDK/server contract.
	cases := []struct {
		query        string
		wantArchived bool
		wantActive   bool
	}{
		{"?archived=1", true, false},
		{"?archived=TRUE", true, false},
		{"?archived=True", true, false},
		// Invalid value → treated as default (active only).
		{"?archived=yeah", false, true},
		{"?archived=", false, true},
		// archived=true + include=archived together → ArchivedOnly
		// wins (strict). The "user wants only archived" intent
		// trumps the legacy admin combined view.
		{"?archived=true&include=archived", true, false},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.List(rec, reqWithCaller(t, "GET", "/v1/chat/sessions"+tc.query, nil, env.owner, []string{}))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", tc.query, rec.Code)
			continue
		}
		resp := decodeListResponse(t, rec)
		gotArchived := containsSession(resp, archivedID)
		gotActive := containsSession(resp, activeID)
		if gotArchived != tc.wantArchived || gotActive != tc.wantActive {
			t.Errorf("%s: archived=%v active=%v, want archived=%v active=%v (got %v)",
				tc.query, gotArchived, gotActive, tc.wantArchived, tc.wantActive, listIDs(resp))
		}
	}
}

func decodeListResponse(t *testing.T, rec *httptest.ResponseRecorder) []model.ChatSession {
	t.Helper()
	var resp struct {
		Data struct {
			Sessions []model.ChatSession `json:"sessions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return resp.Data.Sessions
}

func containsSession(list []model.ChatSession, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

func listIDs(list []model.ChatSession) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.ID
	}
	return out
}

// --- Patch ---

func TestChatHandler_Patch_RenameAndPin(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	created := decodeChatSession(t, createRec.Body.Bytes())

	newTitle := "Renamed"
	pinned := true
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		Title:  &newTitle,
		Pinned: &pinned,
	}, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Patch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeChatSession(t, rec.Body.Bytes())
	if got.Title != "Renamed" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.PinnedAt == nil {
		t.Errorf("PinnedAt nil after pin patch")
	}
}

func TestChatHandler_Patch_ToolsBumpsVersion(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
		Tools:     []string{"recall_memories"},
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	created := decodeChatSession(t, createRec.Body.Bytes())
	if created.ToolsetVersion != 1 {
		t.Fatalf("initial ToolsetVersion = %d, want 1", created.ToolsetVersion)
	}

	// Patch to an empty tools list — the agent runs with no
	// tools (legal per normalizeChatToolList). Phase 3.6a v2's
	// `Available` gate restricts non-empty patches to the set
	// of Available tools (just recall_memories today); the
	// version-bump invariant is independent of multi-tool
	// shapes, so we exercise it via an empty patch here. As
	// more tools become Available in 3.6b/3.6c, we can extend
	// to multi-tool patches.
	newTools := []string{}
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		Tools: &newTools,
	}, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Patch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := decodeChatSession(t, rec.Body.Bytes())
	if got.ToolsetVersion != 2 {
		t.Errorf("after tools patch ToolsetVersion = %d, want 2", got.ToolsetVersion)
	}
	if len(got.Tools) != 0 {
		t.Errorf("Tools = %v, want empty (patched to [])", got.Tools)
	}
}

func TestChatHandler_Patch_OtherUsersSession_404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	created := decodeChatSession(t, createRec.Body.Bytes())

	rec := httptest.NewRecorder()
	newTitle := "x"
	req := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		Title: &newTitle,
	}, env.other, []string{env.hubA, env.hubB})
	req.SetPathValue("id", created.ID)
	h.Patch(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner PATCH = %d, want 404", rec.Code)
	}
}

// --- Delete ---

func TestChatHandler_Delete_SoftAndIdempotent(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	created := decodeChatSession(t, createRec.Body.Bytes())

	// First delete → 204.
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "DELETE", "/v1/chat/sessions/"+created.ID, nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("first delete = %d, want 204", rec.Code)
	}

	// Idempotent: second delete also 204 (already-deleted is
	// silent success — the user's intent was satisfied either
	// way).
	rec = httptest.NewRecorder()
	req = reqWithCaller(t, "DELETE", "/v1/chat/sessions/"+created.ID, nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("idempotent delete = %d, want 204", rec.Code)
	}

	// GET the deleted session → 404 (default list filter
	// excludes deleted; the GET handler currently doesn't,
	// but the underlying store filter should still hide it).
	// Actually the store's GetChatSession doesn't filter by
	// deleted_at, so GET DOES return the session row with
	// deleted_at populated. That's intentional — the UI may
	// want to render a "this conversation was deleted" state.
	rec = httptest.NewRecorder()
	req = reqWithCaller(t, "GET", "/v1/chat/sessions/"+created.ID, nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET deleted session = %d, want 200 (returns row with deleted_at)", rec.Code)
	}
	got := decodeChatSession(t, rec.Body.Bytes())
	if got.DeletedAt == nil {
		t.Errorf("expected deleted_at populated after soft-delete")
	}
}

func TestChatHandler_Delete_NonExistentReturns204(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	bogusID := uuid.NewString()
	req := reqWithCaller(t, "DELETE", "/v1/chat/sessions/"+bogusID, nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", bogusID)
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete-nonexistent = %d, want 204 (idempotent)", rec.Code)
	}
}

// Tools that aren't in the closed catalog must be rejected at
// create-time. Without this gate, an arbitrary string in the
// `tools` array would propagate into chat_sessions.tools and
// later confuse the agent runtime's tool resolver. Codex flagged
// the gap on the Phase 3.2 review.
func TestChatHandler_Create_RejectsUnknownTool(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
		Tools:     []string{"recall_memories", "definitely_not_a_tool"},
	}, env.owner, []string{env.hubA})
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Duplicate names in the tools array are silently deduped
// (a CLI passing `--tool x --tool x` shouldn't surface as a
// hard error). Phase 3.6a v2 restricted enable-able tools to
// the Available set (just recall_memories today), so this
// test asserts dedup of three duplicates → one entry. The
// alphabetical-sort invariant is preserved by the
// normalizeChatToolList implementation; once a second
// Available tool ships in 3.6b/3.6c we'll extend this test
// to also pin sort order.
func TestChatHandler_Create_DedupesAndSortsTools(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
		Tools:     []string{"recall_memories", "recall_memories", "recall_memories"},
	}, env.owner, []string{env.hubA})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	got := decodeChatSession(t, rec.Body.Bytes())
	if len(got.Tools) != 1 {
		t.Errorf("Tools = %v, want 1 (deduped from 3)", got.Tools)
	}
	if len(got.Tools) > 0 && got.Tools[0] != "recall_memories" {
		t.Errorf("Tools[0] = %q, want recall_memories", got.Tools[0])
	}
}

// PATCH on a session in any non-idle status must fail with 409 —
// the agent loop or its cancel-rollback is mid-flight, and a
// successful tools PATCH would race with the toolset_version
// the active turn is bound to. Plan 24 §"Lifecycle policies"
// codifies this as chat_session_locked. Codex caught the
// `canceling` gap on Phase 3.2 v2 — extending to a sub-table
// over every non-idle status pins the "default deny unless idle"
// invariant so future status additions can't regress this gate.
func TestChatHandler_Patch_Tools_RejectedWhenNonIdle(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Status values to flip through. ChatSessionStatusIdle is
	// covered by TestChatHandler_Patch_ToolsBumpsVersion.
	cases := []string{
		model.ChatSessionStatusRunning,
		model.ChatSessionStatusAwaitingApproval,
		model.ChatSessionStatusCanceling,
	}
	for _, status := range cases {
		t.Run(status, func(t *testing.T) {
			createRec := httptest.NewRecorder()
			createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
				ScopeType: model.ChatScopeTypeUserAllHubs,
			}, env.owner, []string{env.hubA})
			h.Create(createRec, createReq)
			created := decodeChatSession(t, createRec.Body.Bytes())

			// The public store API doesn't expose status mutation
			// at this phase — Phase 3.3's lock primitive lands that.
			// Raw SQL is the only path until then.
			if _, err := env.pool.Exec(context.Background(),
				`UPDATE chat_sessions SET status=$1, locked_at=now() WHERE id = $2::uuid`,
				status, created.ID); err != nil {
				t.Fatalf("flip status to %s: %v", status, err)
			}

			newTools := []string{"recall_memories", "list_topics"}
			rec := httptest.NewRecorder()
			req := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
				Tools: &newTools,
			}, env.owner, []string{env.hubA})
			req.SetPathValue("id", created.ID)
			h.Patch(rec, req)
			if rec.Code != http.StatusConflict {
				t.Errorf("PATCH while %s = %d, want 409 (body=%s)",
					status, rec.Code, rec.Body.String())
			}
		})
	}
}

// Malformed cursor → 400 invalid_cursor (not 500). Bad cursors
// are client input, not a server fault. Codex flagged this on
// the Phase 3.2 review.
func TestChatHandler_List_BadCursorReturns400(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions?cursor=not-a-real-cursor",
		nil, env.owner, []string{env.hubA})
	h.List(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad cursor = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Hub-scoped credentials cannot create user_all_hubs sessions —
// that scope expands to ALL the user's hubs at message-send time
// (Phase 3.3's resolver), which would silently bypass the
// hub-scope allowlist. Codex caught this privilege-escalation
// path on Phase 3.2 v3. The handler must reject the request
// before any session row lands.
func TestChatHandler_Create_HubScopedRejectsUserAllHubs(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	// Stamp the request as a hub-scoped principal — exactly the
	// shape an OAuth grant or hub-scoped API key would carry.
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:       env.owner,
		HubScopeMode: handler.HubScopeAllowlist,
		ScopedHubIDs: []string{env.hubA},
	})
	h.Create(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("hub-scoped + user_all_hubs = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Phase 3.6b3d v3 — grant-level memory:write gate. Codex's
// v2 review caught the P0: chat session-create only checked
// PermChatWrite (the route-level authz), but push_memory
// writes via PermMemoryWrite. A grant with chat:write +
// memory:read could enable push_memory and bypass the
// memory-permission restriction the grant intentionally
// imposed.
//
// Three scenarios:
//
//  1. Read-only tools (no ApprovalRequired) → gate skipped
//     entirely; even an empty PermissionsByHub passes.
//  2. push_memory + memory:write missing in grant → 403.
//  3. push_memory + memory:write present in grant → 201.
func TestChatHandler_Create_GrantMissingMemoryWrite_RejectsApprovalGatedTools(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA})
	// Grant explicitly carries memory:read but NOT memory:write
	// (the failure mode codex caught — chat:write is sufficient
	// at the route level but the chat path was bypassing the
	// finer memory:write capability).
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Create(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "grant_lacks_required_permission") {
		t.Errorf("body = %s, want grant_lacks_required_permission", rec.Body.String())
	}
}

// With memory:write in the grant, the same shape succeeds.
func TestChatHandler_Create_GrantHasMemoryWrite_AcceptsApprovalGatedTools(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA})
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
		},
	})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Read-only session (recall_memories only) skips the gate
// entirely — even a chat:read grant with no PermissionsByHub
// populated can create read-only sessions. Pins the
// "no false-positive on read-only sessions" property.
func TestChatHandler_Create_ReadOnlySession_SkipsGrantGate(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories"},
	}, env.owner, []string{env.hubA})
	// Empty PermissionsByHub — would fail the gate if the
	// session enabled push_memory. With recall_memories only,
	// no gate fires.
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID: env.owner,
	})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (read-only sessions don't need memory:write)", rec.Code)
	}
}

// Phase 3.6b3d v3 codex follow-up: Send must re-run the
// grant gate. Codex's P0 scenario: a session created with a
// permissive grant (memory:write present) is later sent into
// by a downgraded grant (memory:read only). Without the
// Send-time gate the request would proceed to enqueue and
// the worker (which only has the LIVE hub role gate, not
// the original grant context) would happily run push_memory.
//
// Setup:
//  1. Create a session with push_memory enabled, using a
//     grant that has memory:write.
//  2. Send a message using a grant that has only memory:read.
//  3. Assert 403 grant_lacks_required_permission.
func TestChatHandler_Send_DowngradedGrant_RejectsApprovalGatedSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Step 1: Create with full grant.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA})
	createReq = handler.InjectAuthContextForTest(createReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
		},
	})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	// Step 2: Send with downgraded grant (no memory:write).
	sendRec := httptest.NewRecorder()
	sendReq := sendMessageRequest(t, created.ID, "remember this", "", env.owner, []string{env.hubA})
	sendReq = handler.InjectAuthContextForTest(sendReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Send(sendRec, sendReq)
	if sendRec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", sendRec.Code, sendRec.Body.String())
	}
	if !strings.Contains(sendRec.Body.String(), "grant_lacks_required_permission") {
		t.Errorf("body = %s, want grant_lacks_required_permission", sendRec.Body.String())
	}
}

// Phase 3.6b3d v5 codex follow-up: Send must run the
// per-write-hub gate for user_all_hubs sessions. Scenario:
// session created when grant had memory:write on hubB
// (default write hub). Grant later downgraded to memory:read
// on hubB (still memory:write on hubA). The broad gate
// passes (hubA satisfies "at least one"); without the
// per-write-hub Send-time check, the session would proceed,
// the agent would request_approval against hubB, the user
// would approve, and the worker's role gate would reject
// at runtime — wasting an approval cycle.
func TestChatHandler_Send_UserAllHubs_DowngradedWriteHub_Rejects(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Step 1: Create with full grant (write on both hubs).
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:  model.ChatScopeTypeUserAllHubs,
		WriteHubID: env.hubB,
		Tools:      []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA, env.hubB})
	createReq = handler.InjectAuthContextForTest(createReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
		},
	})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	// Step 2: Send with downgraded grant — hubA still
	// writable so the broad gate passes, hubB (the pinned
	// write hub) is read-only.
	sendRec := httptest.NewRecorder()
	sendReq := sendMessageRequest(t, created.ID, "save this", "", env.owner, []string{env.hubA, env.hubB})
	sendReq = handler.InjectAuthContextForTest(sendReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Send(sendRec, sendReq)
	if sendRec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", sendRec.Code, sendRec.Body.String())
	}
	if !strings.Contains(sendRec.Body.String(), "grant_lacks_required_permission") {
		t.Errorf("body = %s, want grant_lacks_required_permission", sendRec.Body.String())
	}
	if !strings.Contains(sendRec.Body.String(), env.hubB) {
		t.Errorf("body = %s, want it to name the offending hub %q", sendRec.Body.String(), env.hubB)
	}
}

// Phase 3.6b3d v6 codex follow-up: PATCH write_hub_id is
// idle-only. Without this constraint, a concurrent PATCH
// could flip the default write target between Send's
// validation and the worker's descriptor build, so a turn
// approved against hubA would execute against hubB. The
// constraint mirrors the existing idle-only check on Tools
// patches.
func TestChatHandler_Patch_WriteHubID_RejectsLockedSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Create a multi-hub session with push_memory enabled.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeHubSubset,
		ScopeHubIDs: []string{env.hubA, env.hubB},
		WriteHubID:  env.hubA,
		Tools:       []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA, env.hubB})
	createReq = handler.InjectAuthContextForTest(createReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
		},
	})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	// Lock the session by flipping it to running directly via
	// the store (simulating "a message is in flight"). This
	// is the same shape the chat handler's BeginChatMessageTurn
	// uses internally, just driven from the test fixture.
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE chat_sessions SET status = 'running', updated_at = now() WHERE id = $1::uuid`,
		created.ID); err != nil {
		t.Fatalf("lock session: %v", err)
	}

	// PATCH write_hub_id while running → 409 chat_session_locked.
	newWrite := env.hubB
	patchRec := httptest.NewRecorder()
	patchReq := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		WriteHubID: &newWrite,
	}, env.owner, []string{env.hubA, env.hubB})
	patchReq.SetPathValue("id", created.ID)
	patchReq = handler.InjectAuthContextForTest(patchReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
		},
	})
	h.Patch(patchRec, patchReq)
	if patchRec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (body=%s)", patchRec.Code, patchRec.Body.String())
	}
	if !strings.Contains(patchRec.Body.String(), "chat_session_locked") {
		t.Errorf("body = %s, want chat_session_locked", patchRec.Body.String())
	}
}

// Phase 3.6b3d v5 codex follow-up: same-patch Tools +
// WriteHubID must validate against the NORMALIZED tool list,
// not the raw input. Scenario: client sends `" push_memory "`
// (with whitespace); normalizeChatToolList trims to
// "push_memory" and persists that. Without using the
// normalized list for the write-hub gate, lookups in
// chatToolRequiredPermission would miss the whitespace-
// padded entry and let a doomed write_hub_id slip through.
func TestChatHandler_Patch_NormalizedToolsUsedForWriteHubGate(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Step 1: Create a user_all_hubs session, no tools yet.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
		Tools:     []string{"recall_memories"},
	}, env.owner, []string{env.hubA, env.hubB})
	createReq = handler.InjectAuthContextForTest(createReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	// Step 2: PATCH adding push_memory (with whitespace) AND
	// write_hub_id = hubB (which lacks memory:write). The
	// gate using NORMALIZED tools must catch this; if it
	// used the raw `" push_memory "`, the chatToolRequiredPermission
	// lookup would miss and the doomed write target would
	// be persisted.
	newTools := []string{"recall_memories", " push_memory "}
	newWrite := env.hubB
	patchRec := httptest.NewRecorder()
	patchReq := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		Tools:      &newTools,
		WriteHubID: &newWrite,
	}, env.owner, []string{env.hubA, env.hubB})
	patchReq.SetPathValue("id", created.ID)
	patchReq = handler.InjectAuthContextForTest(patchReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Patch(patchRec, patchReq)
	if patchRec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", patchRec.Code, patchRec.Body.String())
	}
	if !strings.Contains(patchRec.Body.String(), "grant_lacks_required_permission") {
		t.Errorf("body = %s, want grant_lacks_required_permission", patchRec.Body.String())
	}
}

// Phase 3.6b3d v4 codex follow-up: user_all_hubs + push_memory
// + write_hub_id pointing at a hub the grant lacks
// memory:write on. The "at least one" gate passes via another
// hub (hubA), but the SPECIFIC write target (hubB) doesn't
// satisfy the per-tool permission. Without the new
// validateChatWriteHubGrantPermissions check, the session
// would create with a doomed default write target — the
// agent would call request_approval, the user would approve,
// and the chat worker's role gate would reject the actual
// push at runtime, wasting the approval cycle.
func TestChatHandler_Create_UserAllHubs_WriteHubID_LacksRequiredPerm_Rejects(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	writeHub := env.hubB
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:  model.ChatScopeTypeUserAllHubs,
		WriteHubID: writeHub,
		Tools:      []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA, env.hubB})
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite), // no write on the chosen write hub
		},
	})
	h.Create(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "grant_lacks_required_permission") {
		t.Errorf("body = %s, want grant_lacks_required_permission", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), env.hubB) {
		t.Errorf("body = %s, want it to name the offending hub %q", rec.Body.String(), env.hubB)
	}
}

// PATCH that ONLY changes write_hub_id (Tools unchanged) must
// re-run the grant gate when the session has approval-gated
// tools — codex caught that v1 only validated read-access via
// validateChatWriteHub. Without this revalidation, a
// downgraded grant could change the push target without
// proving it can push at all.
func TestChatHandler_Patch_WriteHubID_RevalidatesGrant(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Step 1: Create with full grant + push_memory + multi-hub
	// scope so write_hub_id is meaningful.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeHubSubset,
		ScopeHubIDs: []string{env.hubA, env.hubB},
		Tools:       []string{"recall_memories", "push_memory"},
	}, env.owner, []string{env.hubA, env.hubB})
	createReq = handler.InjectAuthContextForTest(createReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermMemoryWrite, handler.PermChatWrite),
		},
	})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	// Step 2: PATCH write_hub_id with a downgraded grant.
	newWrite := env.hubB
	patchRec := httptest.NewRecorder()
	patchReq := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		WriteHubID: &newWrite,
	}, env.owner, []string{env.hubA, env.hubB})
	patchReq.SetPathValue("id", created.ID)
	patchReq = handler.InjectAuthContextForTest(patchReq, &handler.AuthContext{
		UserID: env.owner,
		// No memory:write — grant was downgraded.
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
			env.hubB: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Patch(patchRec, patchReq)
	if patchRec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body=%s)", patchRec.Code, patchRec.Body.String())
	}
	if !strings.Contains(patchRec.Body.String(), "grant_lacks_required_permission") {
		t.Errorf("body = %s, want grant_lacks_required_permission", patchRec.Body.String())
	}
}

// Patch enabling push_memory on a session that didn't have
// it must re-run the grant gate.
func TestChatHandler_Patch_AddingPushMemory_GatesOnGrant(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Step 1: Create a read-only session (no gate).
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories"},
	}, env.owner, []string{env.hubA})
	createReq = handler.InjectAuthContextForTest(createReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())

	// Step 2: Patch to add push_memory. Grant has no
	// memory:write → 403.
	newTools := []string{"recall_memories", "push_memory"}
	patchRec := httptest.NewRecorder()
	patchReq := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		Tools: &newTools,
	}, env.owner, []string{env.hubA})
	patchReq.SetPathValue("id", created.ID)
	patchReq = handler.InjectAuthContextForTest(patchReq, &handler.AuthContext{
		UserID: env.owner,
		PermissionsByHub: map[string]handler.PermissionSet{
			env.hubA: handler.NewPermissionSet(handler.PermMemoryRead, handler.PermChatWrite),
		},
	})
	h.Patch(patchRec, patchReq)
	if patchRec.Code != http.StatusForbidden {
		t.Errorf("patch enabling push_memory without memory:write: status = %d, want 403 (body=%s)",
			patchRec.Code, patchRec.Body.String())
	}
}

// Same hub-scoped principal CAN create a single_hub session
// scoped to a hub in their allowlist — proves the gate above
// is surgical, not a blanket reject of hub-scoped chat.
func TestChatHandler_Create_HubScopedAcceptsSingleHubInAllowlist(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
	}, env.owner, []string{env.hubA})
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:       env.owner,
		HubScopeMode: handler.HubScopeAllowlist,
		ScopedHubIDs: []string{env.hubA},
	})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("hub-scoped + single_hub in allowlist = %d, want 201 (body=%s)",
			rec.Code, rec.Body.String())
	}
}

// AccountPermissions must include PermChatRead/Write so the
// HubContext middleware places them in the synthetic
// accountPermissionScope slot — without this, /v1/chat/* is
// effectively unreachable for normal authenticated users
// (CanAny returns false because no permsByHub entry contains
// chat:* permissions). Codex caught this on the Phase 3.2
// review and it would have been a production-only failure
// (handler unit tests use InjectUserIDForTest, which bypasses
// the auth middleware chain entirely).
func TestAccountPermissions_IncludesChatPerms(t *testing.T) {
	t.Parallel()
	perms := handler.AccountPermissions()
	if !perms.Has(handler.PermChatRead) {
		t.Errorf("AccountPermissions missing PermChatRead — /v1/chat GET would 403 in prod")
	}
	if !perms.Has(handler.PermChatWrite) {
		t.Errorf("AccountPermissions missing PermChatWrite — /v1/chat POST/PATCH/DELETE would 403 in prod")
	}
}

// --- Phase 3.3 message-send tests ---

// createSessionForSendTest is a tiny helper that calls the
// Create handler and returns the session id. Each Send test
// starts the same way; pulling this out keeps the tests focused
// on the send-specific behavior.
func createSessionForSendTest(t *testing.T, h *handler.ChatHandler, env *chatTestEnv) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
	}, env.owner, []string{env.hubA})
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session for send test: %d (body=%s)", rec.Code, rec.Body.String())
	}
	return decodeChatSession(t, rec.Body.Bytes()).ID
}

// sendMessageRequest builds a request for POST /messages — the
// path includes the session id so the handler's PathValue
// extraction works. We always populate idempotency_key with a
// fresh UUID unless the caller overrides it.
func sendMessageRequest(t *testing.T, sessionID, content, idempotencyKey, owner string, hubs []string) *http.Request {
	t.Helper()
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sessionID+"/messages",
		handler.SendMessageRequest{Content: content, IdempotencyKey: idempotencyKey},
		owner, hubs)
	req.SetPathValue("id", sessionID)
	return req
}

func TestChatHandler_Send_FreshHappyPath(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	req := sendMessageRequest(t, sessionID, "Hi there", "", env.owner, []string{env.hubA})
	h.Send(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("Send fresh = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Outcome != "fresh" {
		t.Errorf("Outcome = %q, want fresh", resp.Data.Outcome)
	}
	if resp.Data.UserMessageID == "" {
		t.Errorf("UserMessageID empty")
	}
	if resp.Data.AssistantMessage == nil || resp.Data.AssistantMessage.Status != model.ChatMessageStatusQueued {
		t.Errorf("AssistantMessage = %+v, want queued status", resp.Data.AssistantMessage)
	}
	if resp.Data.ResumeURL == "" {
		t.Errorf("ResumeURL empty")
	}
}

// Idempotency replay: same key + completed prior turn → 200 with
// the persisted assistant content.
func TestChatHandler_Send_IdempotencyReplay(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	key := uuid.NewString()
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", key, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first send = %d", rec.Code)
	}
	var first struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &first)

	// Drive the assistant to terminal + release the lock — same
	// transition the runtime does at the end of a successful
	// turn (Phase 3.4 will own this).
	if err := env.store.FinalizeChatMessage(context.Background(),
		first.Data.AssistantMessage.ID, env.owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCompleted,
			Content:    "synthesized reply",
			ToolCalls:  []byte(`[]`),
			Citations:  []byte(`[]`),
			Usage:      []byte(`{}`),
			FinishedAt: time.Now().UTC(),
		}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Replay — same key, fresh request.
	rec = httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", key, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusOK {
		t.Fatalf("replay = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var replay struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &replay); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if replay.Data.Outcome != "replay" {
		t.Errorf("Outcome = %q, want replay", replay.Data.Outcome)
	}
	if replay.Data.AssistantMessage.ID != first.Data.AssistantMessage.ID {
		t.Errorf("replay returned different assistant id")
	}
	if replay.Data.AssistantMessage.Content != "synthesized reply" {
		t.Errorf("replay assistant content = %q, want persisted reply",
			replay.Data.AssistantMessage.Content)
	}
}

// Idempotency in-flight: same key while prior turn is still
// running → 202 with the existing assistant id (not a new one).
func TestChatHandler_Send_IdempotencyInFlight(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	key := uuid.NewString()
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", key, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first send = %d", rec.Code)
	}
	var first struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &first)

	// Don't finalize — assistant stays queued. Re-issue the same
	// idempotency key.
	rec = httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", key, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("in_flight replay = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}
	var again struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if again.Data.Outcome != "in_flight" {
		t.Errorf("Outcome = %q, want in_flight", again.Data.Outcome)
	}
	if again.Data.AssistantMessage.ID != first.Data.AssistantMessage.ID {
		t.Errorf("in_flight returned different assistant id")
	}
}

// A second send with a DIFFERENT idempotency key while the
// session is locked → 409 chat_session_locked with active_message_id
// + resume_url surfaced so the client can attach to the
// in-flight stream rather than blindly retrying.
func TestChatHandler_Send_LockContentionReturns409(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first send = %d", rec.Code)
	}
	var first struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &first)

	rec = httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "second", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second send = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ActiveMessageID string `json:"active_message_id"`
				ResumeURL       string `json:"resume_url"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conflict.Error.Code != "chat_session_locked" {
		t.Errorf("error.code = %q, want chat_session_locked", conflict.Error.Code)
	}
	if conflict.Error.Details.ActiveMessageID != first.Data.AssistantMessage.ID {
		t.Errorf("error.details.active_message_id = %q, want %q",
			conflict.Error.Details.ActiveMessageID, first.Data.AssistantMessage.ID)
	}
	if conflict.Error.Details.ResumeURL == "" {
		t.Errorf("error.details.resume_url empty — client can't resume")
	}
}

// Cross-owner: a caller passing someone else's session id → 404
// (not 403 / not 409). Owner isolation must not leak existence.
func TestChatHandler_Send_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.other, []string{env.hubA, env.hubB}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner Send = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Soft-deleted session: GET still returns the row (tombstone view)
// but Send must reject. Returns 404 to keep the surface symmetric
// with cross-owner.
func TestChatHandler_Send_RejectsSoftDeletedSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	if err := env.store.SoftDeleteChatSession(context.Background(), sessionID, env.owner); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Send into soft-deleted = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Validation surface: empty content, missing idempotency_key,
// and oversize content all → 400 with distinct error codes.
func TestChatHandler_Send_RejectsBadInput(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	cases := []struct {
		name    string
		req     handler.SendMessageRequest
		wantErr string
	}{
		{"empty content", handler.SendMessageRequest{IdempotencyKey: uuid.NewString()}, "missing_content"},
		{"whitespace content", handler.SendMessageRequest{Content: "   ", IdempotencyKey: uuid.NewString()}, "missing_content"},
		{"missing idempotency_key", handler.SendMessageRequest{Content: "hi"}, "missing_idempotency_key"},
		{"oversize content",
			handler.SendMessageRequest{
				Content:        strings.Repeat("a", 100_001),
				IdempotencyKey: uuid.NewString(),
			},
			"content_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := reqWithCaller(t, "POST",
				"/v1/chat/sessions/"+sessionID+"/messages",
				tc.req, env.owner, []string{env.hubA})
			req.SetPathValue("id", sessionID)
			h.Send(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
			var got struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if got.Error.Code != tc.wantErr {
				t.Errorf("error.code = %q, want %q", got.Error.Code, tc.wantErr)
			}
		})
	}
}

func TestChatHandler_ListMessages_HappyPath(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	// Send a few turns.
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.Send(rec, sendMessageRequest(t, sessionID, "msg", "", env.owner, []string{env.hubA}))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("send %d: %d", i, rec.Code)
		}
		// Need to release the lock between sends or the second
		// will get 409. Finalize the assistant via the store.
		var resp struct {
			Data handler.SendMessageResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if err := env.store.FinalizeChatMessage(context.Background(),
			resp.Data.AssistantMessage.ID, env.owner, store.ChatMessageFinalize{
				Status:     model.ChatMessageStatusCompleted,
				Content:    "ack",
				ToolCalls:  []byte(`[]`),
				Citations:  []byte(`[]`),
				Usage:      []byte(`{}`),
				FinishedAt: time.Now().UTC(),
			}); err != nil {
			t.Fatalf("finalize %d: %v", i, err)
		}
	}

	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionID+"/messages",
		nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionID)
	h.ListMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListMessages = %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Messages   []model.ChatMessage `json:"messages"`
			NextCursor string              `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Messages) != 6 {
		t.Errorf("messages = %d, want 6 (3 user + 3 assistant)", len(resp.Data.Messages))
	}
	// Sequence ascending.
	for i := 1; i < len(resp.Data.Messages); i++ {
		if resp.Data.Messages[i].Sequence <= resp.Data.Messages[i-1].Sequence {
			t.Errorf("sequence not ascending at i=%d: %d then %d",
				i, resp.Data.Messages[i-1].Sequence, resp.Data.Messages[i].Sequence)
		}
	}
}

func TestChatHandler_ListMessages_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionID+"/messages",
		nil, env.other, []string{env.hubA, env.hubB})
	req.SetPathValue("id", sessionID)
	h.ListMessages(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_ListMessages_BadCursorReturns400(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionID+"/messages?cursor=not-an-int",
		nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionID)
	h.ListMessages(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad cursor = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_GetMessage_HappyPath(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	var sent struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)
	msgID := sent.Data.UserMessageID

	rec = httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionID+"/messages/"+msgID,
		nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionID)
	req.SetPathValue("msg_id", msgID)
	h.GetMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetMessage = %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// A message id from a DIFFERENT session embedded in the URL must
// 404 — the handler shouldn't trust path-id as a session-membership
// proof. Covers the case where a curious client tries to read
// across session boundaries even though the message id IS theirs.
func TestChatHandler_GetMessage_PathSessionMismatchReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Two distinct sessions, both owned by env.owner.
	sessionA := createSessionForSendTest(t, h, env)
	sessionB := createSessionForSendTest(t, h, env)

	// Send a message into sessionA so we have a real id to use.
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionA, "hi", "", env.owner, []string{env.hubA}))
	var sent struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)
	msgID := sent.Data.UserMessageID

	// Read the message via sessionB's URL — wrong session in path.
	rec = httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionB+"/messages/"+msgID,
		nil, env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionB)
	req.SetPathValue("msg_id", msgID)
	h.GetMessage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("session-id mismatch = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// SendEnqueueRun-style handoff: when SetEnqueueRun is wired, the
// handler MUST invoke it on Fresh outcome and pass the right ids.
// Phase 3.4a — without this hook, the worker has no way to know a
// queued assistant exists; the lease sweeper would eventually
// finalize it as a server crash. Codex caught this kind of gap
// repeatedly across phases — keep the contract pinned.
func TestChatHandler_Send_EnqueueRunCalledOnFresh(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	type call struct {
		sessionID, ownerID, assistantID string
	}
	var got call
	h.SetEnqueueRun(func(_ context.Context, sid, oid, aid string) error {
		got = call{sid, oid, aid}
		return nil
	})

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}

	var resp struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if got.sessionID != sessionID {
		t.Errorf("enqueue session_id = %q, want %q", got.sessionID, sessionID)
	}
	if got.ownerID != env.owner {
		t.Errorf("enqueue owner_id = %q, want %q", got.ownerID, env.owner)
	}
	if got.assistantID != resp.Data.AssistantMessage.ID {
		t.Errorf("enqueue assistant_id = %q, want %q",
			got.assistantID, resp.Data.AssistantMessage.ID)
	}
}

// Replay outcome must NOT enqueue (the prior turn's worker already
// finalized — re-enqueueing would either no-op or attempt to
// refinalize). InFlight likewise: the original enqueue is still
// running.
func TestChatHandler_Send_EnqueueRunNotCalledOnReplay(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	calls := 0
	h.SetEnqueueRun(func(_ context.Context, _, _, _ string) error {
		calls++
		return nil
	})

	key := uuid.NewString()
	// First send: Fresh → should enqueue once.
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", key, env.owner, []string{env.hubA}))
	var first struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &first)

	// Drive the assistant to terminal — what the worker would do
	// at the end of a successful turn.
	if err := env.store.FinalizeChatMessage(context.Background(),
		first.Data.AssistantMessage.ID, env.owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCompleted,
			Content:    "ack",
			ToolCalls:  []byte(`[]`),
			Citations:  []byte(`[]`),
			Usage:      []byte(`{}`),
			FinishedAt: time.Now().UTC(),
		}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Replay: same key → must NOT enqueue.
	rec = httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "first", key, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusOK {
		t.Fatalf("replay = %d, want 200", rec.Code)
	}
	if calls != 1 {
		t.Errorf("enqueue calls = %d, want 1 (no enqueue on replay)", calls)
	}
}

// Hub-scoped credentials cannot send into a user_all_hubs session
// (mirrors the create-time gate). Codex caught this on Phase 3.3
// v1 — without it the hub-scope allowlist becomes advisory at
// session-creation time only; an existing user_all_hubs session
// could be used by a hub-scoped principal to lift its scope.
func TestChatHandler_Send_HubScopedRejectsUserAllHubsSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Create a user_all_hubs session as a normal (cookie-style) caller.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create user_all_hubs: %d", createRec.Code)
	}
	sessionID := decodeChatSession(t, createRec.Body.Bytes()).ID

	// Now try to Send as a hub-scoped principal.
	rec := httptest.NewRecorder()
	req := sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA})
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:       env.owner,
		HubScopeMode: handler.HubScopeAllowlist,
		ScopedHubIDs: []string{env.hubA},
	})
	h.Send(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("hub-scoped + user_all_hubs Send = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Archived sessions reject Send with chat_session_archived (409).
// Plan 24 §"Errors" pins this code; codex caught the gap on
// Phase 3.3 v1 — archived was being silently allowed to mutate.
func TestChatHandler_Send_RejectsArchivedSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	// Archive via PATCH.
	archived := true
	patchRec := httptest.NewRecorder()
	patchReq := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+sessionID,
		handler.PatchChatSessionRequest{Archived: &archived},
		env.owner, []string{env.hubA})
	patchReq.SetPathValue("id", sessionID)
	h.Patch(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("archive PATCH: %d (body=%s)", patchRec.Code, patchRec.Body.String())
	}

	// Send must reject with 409 chat_session_archived.
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("Send into archived = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Error.Code != "chat_session_archived" {
		t.Errorf("error.code = %q, want chat_session_archived", got.Error.Code)
	}
}

// Caller has lost access to every hub the session is scoped to →
// 403 hub_scope_required. Phase 3.4's read-time redaction depends
// on never persisting a turn with empty scope_hub_ids_used, so
// this needs to fail loud at send time. Codex caught this on
// Phase 3.3 v1.
func TestChatHandler_Send_RejectsWhenCallerHasNoAccessibleHubsInScope(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	// Session is scoped to hubA. Send as a caller whose live
	// accessible-hubs set is hubB only — no overlap.
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	req := sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubB})
	h.Send(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Send with no hub overlap = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Error.Code != "hub_scope_required" {
		t.Errorf("error.code = %q, want hub_scope_required", got.Error.Code)
	}
}

// --- Phase 3.6b1 approval workflow tests ---

// seedPendingApproval creates a session + assistant message +
// chat_tool_approvals row in `pending` state. Returns the
// approvalID. Used by the DecideApproval tests.
func seedPendingApproval(t *testing.T, env *chatTestEnv, h *handler.ChatHandler, toolName string) (sessionID, assistantID, approvalID string) {
	t.Helper()
	sessionID = createSessionForSendTest(t, h, env)

	// Send a message to get a real assistant message id (the
	// approval row's message_id FK requires it).
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "test", "", env.owner, []string{env.hubA}))
	var sent struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)
	assistantID = sent.Data.AssistantMessage.ID

	// Insert the approval row directly via the store.
	approvalID = uuid.NewString()
	if err := env.store.CreateChatToolApproval(context.Background(), env.owner, &model.ChatToolApproval{
		ID:          approvalID,
		MessageID:   assistantID,
		ToolName:    toolName,
		Args:        []byte(`{"id":"mem_xxx"}`),
		ArgsHash:    "deadbeef",
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	return sessionID, assistantID, approvalID
}

// recordingChatPublisher implements events.Publisher and
// records every Publish call so tests can assert on event
// shape (type, audience, recipient_user_id, entity_id, state).
// Wired into the handler via SetEventsPublisher.
type recordingChatPublisher struct {
	events []eventsAPI.Event
}

func (p *recordingChatPublisher) Publish(_ context.Context, ev eventsAPI.Event) error {
	p.events = append(p.events, ev)
	return nil
}
func (p *recordingChatPublisher) Close() error { return nil }

// recordingPublisherForChat wires a fresh recordingChatPublisher
// into the handler and returns it. Tests that need to assert on
// published events use this; tests that don't can ignore the
// return value (the handler accepts a nil publisher fine).
func recordingPublisherForChat(t *testing.T, h *handler.ChatHandler) *recordingChatPublisher {
	t.Helper()
	p := &recordingChatPublisher{}
	h.SetEventsPublisher(p)
	return p
}

// Happy path — approve a pending row → 200 + status=approved
// + broker event published with the right routing keys.
//
// Codex v2 review: pass the REAL sessionID through PathValue("id")
// so this exercises the routed handler path (path-id consistency
// check active). Without setting it, the handler's pathSessionID
// is empty and the store skips the session_id filter — meaning
// any bug that rejects every non-empty session id would silently
// pass this suite. The mismatch test pins the negative side; this
// test pins the positive side.
func TestChatHandler_DecideApproval_ApprovesPending(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	rec := recordingPublisherForChat(t, h)
	sessionID, _, approvalID := seedPendingApproval(t, env, h, "forget_memory")

	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sessionID+"/approvals/"+approvalID,
		handler.DecideApprovalRequest{Decision: "approve"},
		env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionID)
	req.SetPathValue("approval_id", approvalID)
	w := httptest.NewRecorder()
	h.DecideApproval(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// Row updated to approved.
	got, err := env.store.GetChatToolApproval(context.Background(), approvalID, env.owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval: %v", err)
	}
	if got.Status != model.ChatToolApprovalStatusApproved {
		t.Errorf("status = %q, want approved", got.Status)
	}

	// Broker published one approval.decided event with the right
	// fields so the SDK-host approver (Phase 3.6b2) can route on
	// EntityID = approvalID.
	if len(rec.events) != 1 {
		t.Fatalf("publisher saw %d events, want 1", len(rec.events))
	}
	ev := rec.events[0]
	if ev.Type != "approval.decided" {
		t.Errorf("event type = %q, want approval.decided", ev.Type)
	}
	if ev.EntityID != approvalID {
		t.Errorf("event entity_id = %q, want %q", ev.EntityID, approvalID)
	}
	if ev.RecipientUserID != env.owner {
		t.Errorf("event recipient_user_id = %q, want %q", ev.RecipientUserID, env.owner)
	}
	if ev.Audience != "user" {
		t.Errorf("event audience = %q, want user", ev.Audience)
	}
	if ev.State != "approved" {
		t.Errorf("event state = %q, want approved", ev.State)
	}
}

// Same shape, decision=deny → status=denied + broker event.
func TestChatHandler_DecideApproval_DeniesPending(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_ = recordingPublisherForChat(t, h)
	sessionID, _, approvalID := seedPendingApproval(t, env, h, "push_memory")

	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sessionID+"/approvals/"+approvalID,
		handler.DecideApprovalRequest{Decision: "deny"},
		env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionID)
	req.SetPathValue("approval_id", approvalID)
	w := httptest.NewRecorder()
	h.DecideApproval(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got, _ := env.store.GetChatToolApproval(context.Background(), approvalID, env.owner)
	if got.Status != model.ChatToolApprovalStatusDenied {
		t.Errorf("status = %q, want denied", got.Status)
	}
}

// Idempotency: a second decide on a non-pending row returns
// 409 chat_approval_already_decided. Pins the CAS gate at the
// HTTP layer.
func TestChatHandler_DecideApproval_DoubleDecideReturns409(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_ = recordingPublisherForChat(t, h)
	sessionID, _, approvalID := seedPendingApproval(t, env, h, "forget_memory")

	// First decide — succeeds.
	req1 := reqWithCaller(t, "POST", "/v1/chat/...",
		handler.DecideApprovalRequest{Decision: "approve"},
		env.owner, []string{env.hubA})
	req1.SetPathValue("id", sessionID)
	req1.SetPathValue("approval_id", approvalID)
	w1 := httptest.NewRecorder()
	h.DecideApproval(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first decide: %d", w1.Code)
	}

	// Second decide — 409.
	req2 := reqWithCaller(t, "POST", "/v1/chat/...",
		handler.DecideApprovalRequest{Decision: "deny"},
		env.owner, []string{env.hubA})
	req2.SetPathValue("id", sessionID)
	req2.SetPathValue("approval_id", approvalID)
	w2 := httptest.NewRecorder()
	h.DecideApproval(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Errorf("second decide = %d, want 409 (body=%s)", w2.Code, w2.Body.String())
	}
}

// Cross-owner: a caller passing someone else's approval_id →
// 404 (no existence leak).
func TestChatHandler_DecideApproval_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_ = recordingPublisherForChat(t, h)
	sessionID, _, approvalID := seedPendingApproval(t, env, h, "forget_memory")

	req := reqWithCaller(t, "POST", "/v1/chat/...",
		handler.DecideApprovalRequest{Decision: "approve"},
		env.other, []string{env.hubA, env.hubB})
	req.SetPathValue("id", sessionID)
	req.SetPathValue("approval_id", approvalID)
	w := httptest.NewRecorder()
	h.DecideApproval(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-owner = %d, want 404", w.Code)
	}
}

// Late-decide on an expired pending row → 408
// chat_approval_timeout. Phase 3.6b2 v2 codex follow-up: the
// approver's local timer flips healthy waits at the boundary,
// but a user click that arrives after expires_at on a still-
// pending row (e.g., approver crashed before its timer fired)
// must NOT be silently committed. The store returns
// ErrChatApprovalExpired and the handler maps to 408.
func TestChatHandler_DecideApproval_LateDecideOnExpiredReturns408(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_ = recordingPublisherForChat(t, h)
	sessionID, _, approvalID := seedPendingApproval(t, env, h, "forget_memory")

	// Backdate expires_at — simulates a late click.
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE chat_tool_approvals SET expires_at = now() - interval '1 second' WHERE id = $1::uuid`,
		approvalID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sessionID+"/approvals/"+approvalID,
		handler.DecideApprovalRequest{Decision: "approve"},
		env.owner, []string{env.hubA})
	req.SetPathValue("id", sessionID)
	req.SetPathValue("approval_id", approvalID)
	w := httptest.NewRecorder()
	h.DecideApproval(w, req)

	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408 (body=%s)", w.Code, w.Body.String())
	}
	// Body carries the chat_approval_timeout error code.
	if !strings.Contains(w.Body.String(), "chat_approval_timeout") {
		t.Errorf("body = %s, want chat_approval_timeout", w.Body.String())
	}
	// Row stays pending — the user's click never committed.
	got, err := env.store.GetChatToolApproval(context.Background(), approvalID, env.owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval: %v", err)
	}
	if got.Status != model.ChatToolApprovalStatusPending {
		t.Errorf("Status = %q, want pending (late-decide must not commit)", got.Status)
	}
}

// Path-id consistency: a same-owner caller passing a wrong
// session id in the URL → 404. Pins the store-layer
// `m.session_id = $4::uuid` filter so an attacker who learned
// an approval id from one session can't decide it from a URL
// nested under a different session. Codex caught the gap on
// Phase 3.6b1 v1.
func TestChatHandler_DecideApproval_PathSessionMismatchReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_ = recordingPublisherForChat(t, h)
	_, _, approvalID := seedPendingApproval(t, env, h, "forget_memory")

	// A different session for the SAME owner — proves the filter
	// is on session_id, not just owner. seedPendingApproval already
	// burned one session; spin up another and use ITS id in the URL.
	otherSessionID := createSessionForSendTest(t, h, env)

	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+otherSessionID+"/approvals/"+approvalID,
		handler.DecideApprovalRequest{Decision: "approve"},
		env.owner, []string{env.hubA})
	req.SetPathValue("id", otherSessionID)
	req.SetPathValue("approval_id", approvalID)
	w := httptest.NewRecorder()
	h.DecideApproval(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}

	// The approval row stays pending — no half-applied decision.
	got, err := env.store.GetChatToolApproval(context.Background(), approvalID, env.owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval: %v", err)
	}
	if got.Status != model.ChatToolApprovalStatusPending {
		t.Errorf("status = %q, want pending (mismatch must not mutate)", got.Status)
	}
}

// Bad input: missing decision / unknown decision → 400.
func TestChatHandler_DecideApproval_RejectsBadInput(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_, _, approvalID := seedPendingApproval(t, env, h, "forget_memory")

	cases := []struct {
		name    string
		req     handler.DecideApprovalRequest
		wantErr string
	}{
		{"empty decision", handler.DecideApprovalRequest{}, "invalid_decision"},
		{"unknown decision", handler.DecideApprovalRequest{Decision: "maybe"}, "invalid_decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := reqWithCaller(t, "POST", "/v1/chat/...",
				tc.req, env.owner, []string{env.hubA})
			req.SetPathValue("approval_id", approvalID)
			w := httptest.NewRecorder()
			h.DecideApproval(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}

// Phase 3.6a — tool catalog endpoint. The Tools handler returns
// the canonical chat tool catalog with metadata: name +
// description + default_in_chat + approval_required + notes.
// Plan 24's tool-registry table is the source of truth; this
// test pins the contract a UI / SDK consumes.
func TestChatHandler_Tools_ReturnsCatalog(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET", "/v1/chat/tools", nil, env.owner, []string{env.hubA})
	h.Tools(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Tools []handler.ChatToolDescriptor `json:"tools"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tools := resp.Data.Tools
	if len(tools) != 10 {
		t.Errorf("got %d tools, want 10 (plan 24 §Tool registry)", len(tools))
	}
	byName := make(map[string]handler.ChatToolDescriptor, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	// Spot-check the load-bearing invariants:
	//   - recall_memories is default + no approval + Available
	//     (the chat foundation tool every session gets; wired
	//     in Phase 3.4b1).
	//   - forget_memory is default-capable + approval required
	//     so the web agent can act for the user while still
	//     stopping for explicit consent.
	//   - fetch_url is default-capable but read-only; the SSRF
	//     guard lives in the tool service, not in catalog hiding.
	if r := byName["recall_memories"]; !r.DefaultInChat || r.ApprovalRequired || !r.Available {
		t.Errorf("recall_memories = %+v, want default=true approval=false available=true", r)
	}
	if f := byName["forget_memory"]; !f.DefaultInChat || !f.ApprovalRequired {
		t.Errorf("forget_memory = %+v, want default=true approval=true", f)
	}
	if u := byName["fetch_url"]; !u.DefaultInChat || u.ApprovalRequired {
		t.Errorf("fetch_url = %+v, want default=true approval=false", u)
	}
}

// Catalog endpoint requires authentication. The AuthorizeHTTP
// middleware gates this via PermChatRead, but the handler
// itself also rejects an empty user_id as defense-in-depth.
func TestChatHandler_Tools_RequiresAuth(t *testing.T) {
	t.Parallel()
	h, _ := setupChatHandlerEnv(t)
	rec := httptest.NewRecorder()
	// Build a request with no InjectUserID — empty user_id.
	req := httptest.NewRequest("GET", "/v1/chat/tools", nil)
	h.Tools(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d, want 401", rec.Code)
	}
}

// PATCH equivalent of TestChatHandler_Create_RejectsUnknownTool.
// A tools update with a name not in the catalog must 400 — the
// PATCH validator runs the same gate.
//
// Phase 3.6b3g context: previous Phase 3.6a–f revisions of this
// test used a still-unavailable tool name as the fixture; the
// fetch_url Available flip in 3.6b3g leaves no Available=false
// tool to point at, so the test now exercises the
// unrecognized-name path. Both branches share the validator
// implementation, so the gate stays pinned.
func TestChatHandler_Patch_RejectsUnknownTool(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	created := decodeChatSession(t, createRec.Body.Bytes())

	newTools := []string{"recall_memories", "this_tool_does_not_exist"}
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "PATCH", "/v1/chat/sessions/"+created.ID, handler.PatchChatSessionRequest{
		Tools: &newTools,
	}, env.owner, []string{env.hubA})
	req.SetPathValue("id", created.ID)
	h.Patch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Catalog-default consistency: a session created without an
// explicit `tools` arg gets exactly the tools the catalog
// reports as `default_in_chat=true` AND `available=true`.
// Codex caught this drift on Phase 3.6a v1 — the static
// chatDefaultTools list had drifted from the catalog. The fix
// derives chatDefaultTools from the catalog at init; this test
// pins the invariant against future drift.
func TestChatHandler_Tools_DefaultsMatchSessionDefaults(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	// Read the catalog.
	rec := httptest.NewRecorder()
	req := reqWithCaller(t, "GET", "/v1/chat/tools", nil, env.owner, []string{env.hubA})
	h.Tools(rec, req)
	var catalog struct {
		Data struct {
			Tools []handler.ChatToolDescriptor `json:"tools"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &catalog)
	expected := map[string]struct{}{}
	for _, t := range catalog.Data.Tools {
		if t.DefaultInChat && t.Available {
			expected[t.Name] = struct{}{}
		}
	}

	// Create a session WITHOUT an explicit tools list.
	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d", createRec.Code)
	}
	got := decodeChatSession(t, createRec.Body.Bytes())
	if len(got.Tools) != len(expected) {
		t.Errorf("session.Tools = %v (count=%d), want catalog default-and-available set (count=%d)",
			got.Tools, len(got.Tools), len(expected))
	}
	for _, name := range got.Tools {
		if _, ok := expected[name]; !ok {
			t.Errorf("session has %q which is NOT a default-and-available catalog tool", name)
		}
		delete(expected, name)
	}
	for name := range expected {
		t.Errorf("catalog default-and-available tool %q missing from session defaults", name)
	}
}

func TestChatHandler_Send_UpgradesLegacyDefaultTools(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)

	createRec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
		Tools:       []string{"recall_memories"},
	}, env.owner, []string{env.hubA})
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: %d (body=%s)", createRec.Code, createRec.Body.String())
	}
	created := decodeChatSession(t, createRec.Body.Bytes())
	if got := created.Tools; len(got) != 1 || got[0] != "recall_memories" {
		t.Fatalf("seed tools = %v, want legacy recall-only", got)
	}

	sendRec := httptest.NewRecorder()
	h.Send(sendRec, sendMessageRequest(t, created.ID, "what can you do?", "", env.owner, []string{env.hubA}))
	if sendRec.Code != http.StatusAccepted {
		t.Fatalf("send: %d (body=%s)", sendRec.Code, sendRec.Body.String())
	}

	upgraded, err := env.store.GetChatSession(context.Background(), created.ID, env.owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	for _, name := range []string{"list_memories", "get_memory", "forget_memory", "push_memory", "fetch_url"} {
		if !chatTestContainsString(upgraded.Tools, name) {
			t.Errorf("upgraded tools = %v, missing %q", upgraded.Tools, name)
		}
	}
	if upgraded.ToolsetVersion != created.ToolsetVersion+1 {
		t.Errorf("ToolsetVersion = %d, want %d", upgraded.ToolsetVersion, created.ToolsetVersion+1)
	}
}

func TestChatHandler_GetMessage_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	var sent struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)
	msgID := sent.Data.UserMessageID

	rec = httptest.NewRecorder()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionID+"/messages/"+msgID,
		nil, env.other, []string{env.hubA, env.hubB})
	req.SetPathValue("id", sessionID)
	req.SetPathValue("msg_id", msgID)
	h.GetMessage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner GetMessage = %d, want 404", rec.Code)
	}
}

// --- Cancel handler tests ---

// sendForCancelTest creates a session, sends one message, and
// returns (sessionID, assistantID). The assistant row stays in
// 'queued' so the cancel path can register canceling on a
// non-terminal row.
func sendForCancelTest(t *testing.T, h *handler.ChatHandler, env *chatTestEnv) (string, string) {
	t.Helper()
	sessionID := createSessionForSendTest(t, h, env)
	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hello", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send for cancel: %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode send: %v", err)
	}
	return sessionID, resp.Data.AssistantMessage.ID
}

func cancelRequest(t *testing.T, sessionID, msgID, owner string, hubs []string) *http.Request {
	t.Helper()
	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sessionID+"/messages/"+msgID+"/cancel",
		nil, owner, hubs)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("msg_id", msgID)
	return req
}

func decodeCancelResp(t *testing.T, body []byte) handler.CancelMessageResponse {
	t.Helper()
	var resp struct {
		Data handler.CancelMessageResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode cancel: %v (body=%q)", err, body)
	}
	return resp.Data
}

func TestChatHandler_Cancel_QueuedRegistersCanceling(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, asstID := sendForCancelTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Cancel(rec, cancelRequest(t, sessionID, asstID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusOK {
		t.Fatalf("Cancel = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	got := decodeCancelResp(t, rec.Body.Bytes())
	if got.AssistantMessageID != asstID {
		t.Errorf("AssistantMessageID = %q, want %q", got.AssistantMessageID, asstID)
	}
	if got.Status != model.ChatMessageStatusCanceling {
		t.Errorf("Status = %q, want %q", got.Status, model.ChatMessageStatusCanceling)
	}
	if !got.CancelRegistered {
		t.Errorf("CancelRegistered = false, want true on a fresh queued row")
	}

	// The row should now be 'canceling' in storage; a second cancel
	// is a no-op (CancelRegistered=false) but still 200.
	rec = httptest.NewRecorder()
	h.Cancel(rec, cancelRequest(t, sessionID, asstID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusOK {
		t.Fatalf("second Cancel = %d, want 200", rec.Code)
	}
	got = decodeCancelResp(t, rec.Body.Bytes())
	if got.CancelRegistered {
		t.Errorf("second Cancel CancelRegistered = true, want false (idempotent no-op)")
	}
	if got.Status != model.ChatMessageStatusCanceling {
		t.Errorf("second Cancel Status = %q, want canceling", got.Status)
	}
}

func TestChatHandler_Cancel_TerminalRowReportsActualStatus(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, asstID := sendForCancelTest(t, h, env)

	if err := env.store.FinalizeChatMessage(context.Background(),
		asstID, env.owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCompleted,
			Content:    "done",
			ToolCalls:  []byte(`[]`),
			Citations:  []byte(`[]`),
			Usage:      []byte(`{}`),
			FinishedAt: time.Now().UTC(),
		}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	rec := httptest.NewRecorder()
	h.Cancel(rec, cancelRequest(t, sessionID, asstID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusOK {
		t.Fatalf("Cancel = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	got := decodeCancelResp(t, rec.Body.Bytes())
	if got.CancelRegistered {
		t.Errorf("CancelRegistered = true, want false against terminal row")
	}
	if got.Status != model.ChatMessageStatusCompleted {
		t.Errorf("Status = %q, want completed (the row's actual current status)", got.Status)
	}
}

func TestChatHandler_Cancel_UserMessageRejected(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d", rec.Code)
	}
	var resp struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	userMsgID := resp.Data.UserMessageID

	rec = httptest.NewRecorder()
	h.Cancel(rec, cancelRequest(t, sessionID, userMsgID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Cancel user msg = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_Cancel_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, asstID := sendForCancelTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Cancel(rec, cancelRequest(t, sessionID, asstID, env.other, []string{env.hubA, env.hubB}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner Cancel = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_Cancel_MismatchedSessionPathReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	_, asstID := sendForCancelTest(t, h, env)

	// Same owner, different session — path-id consistency must
	// return 404 to avoid leaking that the message exists on
	// another session of theirs.
	otherSessionID := createSessionForSendTest(t, h, env)
	rec := httptest.NewRecorder()
	h.Cancel(rec, cancelRequest(t, otherSessionID, asstID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("mismatched session-path Cancel = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// --- Regenerate handler tests ---

func regenerateRequest(t *testing.T, sessionID, priorID, owner string, hubs []string) *http.Request {
	t.Helper()
	req := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sessionID+"/messages/"+priorID+"/regenerate",
		nil, owner, hubs)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("msg_id", priorID)
	return req
}

func decodeRegenerateResp(t *testing.T, body []byte) handler.RegenerateMessageResponse {
	t.Helper()
	var resp struct {
		Data handler.RegenerateMessageResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode regenerate: %v (body=%q)", err, body)
	}
	return resp.Data
}

// finalizedAssistantForRegenerate sends a message and finalizes
// the assistant to 'completed' so it's eligible for regenerate.
// Returns (sessionID, priorAssistantID).
func finalizedAssistantForRegenerate(t *testing.T, h *handler.ChatHandler, env *chatTestEnv) (string, string) {
	t.Helper()
	sessionID, asstID := sendForCancelTest(t, h, env)
	if err := env.store.FinalizeChatMessage(context.Background(),
		asstID, env.owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCompleted,
			Content:    "first reply",
			ToolCalls:  []byte(`[]`),
			Citations:  []byte(`[]`),
			Usage:      []byte(`{}`),
			FinishedAt: time.Now().UTC(),
		}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return sessionID, asstID
}

func TestChatHandler_Regenerate_HappyPath(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, priorID := finalizedAssistantForRegenerate(t, h, env)

	rec := httptest.NewRecorder()
	h.Regenerate(rec, regenerateRequest(t, sessionID, priorID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("Regenerate = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}
	got := decodeRegenerateResp(t, rec.Body.Bytes())
	if got.Outcome != string(store.ChatTurnFresh) {
		t.Errorf("Outcome = %q, want fresh", got.Outcome)
	}
	if got.PriorAssistantID != priorID {
		t.Errorf("PriorAssistantID = %q, want %q", got.PriorAssistantID, priorID)
	}
	if got.AssistantMessage == nil || got.AssistantMessage.Status != model.ChatMessageStatusQueued {
		t.Errorf("new AssistantMessage = %+v, want queued", got.AssistantMessage)
	}
	if got.AssistantMessage.ParentMessageID != priorID {
		t.Errorf("new assistant ParentMessageID = %q, want supersession link to %q",
			got.AssistantMessage.ParentMessageID, priorID)
	}
	if got.ResumeURL == "" {
		t.Errorf("ResumeURL empty")
	}
}

func TestChatHandler_Regenerate_InFlightLocked(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	// Don't finalize — the assistant is queued and the session is
	// locked to it. Per codex's Phase 3.7b feedback, regenerate
	// against a locked session should map to chat_session_locked
	// (with active_message_id + resume_url details) rather than
	// regenerate_not_eligible. The two map to different UI
	// affordances: locked → "wait or cancel", not_eligible →
	// "send again".
	sessionID, asstID := sendForCancelTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Regenerate(rec, regenerateRequest(t, sessionID, asstID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("Regenerate queued = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "chat_session_locked") {
		t.Errorf("body should carry chat_session_locked code; got %s", rec.Body.String())
	}
	// active_message_id pointer surfaces so the client can connect
	// to the resume stream. Match the substring rather than full
	// JSON parse to keep the test resilient to envelope changes.
	if !strings.Contains(rec.Body.String(), asstID) {
		t.Errorf("body should carry active_message_id=%s; got %s", asstID, rec.Body.String())
	}
}

func TestChatHandler_Regenerate_NotEligibleCanceled(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, asstID := sendForCancelTest(t, h, env)

	if err := env.store.FinalizeChatMessage(context.Background(),
		asstID, env.owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCanceled,
			Content:    "",
			ToolCalls:  []byte(`[]`),
			Citations:  []byte(`[]`),
			Usage:      []byte(`{}`),
			Error:      []byte(`{"code":"user_canceled","message":"the user canceled this turn"}`),
			FinishedAt: time.Now().UTC(),
		}); err != nil {
		t.Fatalf("finalize canceled: %v", err)
	}

	rec := httptest.NewRecorder()
	h.Regenerate(rec, regenerateRequest(t, sessionID, asstID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("Regenerate canceled = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "regenerate_not_eligible") {
		t.Errorf("body should carry regenerate_not_eligible code; got %s", rec.Body.String())
	}
}

func TestChatHandler_Regenerate_UserMessageRejected(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID := createSessionForSendTest(t, h, env)

	rec := httptest.NewRecorder()
	h.Send(rec, sendMessageRequest(t, sessionID, "hi", "", env.owner, []string{env.hubA}))
	var resp struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	userMsgID := resp.Data.UserMessageID

	rec = httptest.NewRecorder()
	h.Regenerate(rec, regenerateRequest(t, sessionID, userMsgID, env.owner, []string{env.hubA}))
	// User message hits store.ErrChatRegenerateNotEligible (role
	// gate) → 409 regenerate_not_eligible.
	if rec.Code != http.StatusConflict {
		t.Errorf("Regenerate user msg = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_Regenerate_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, priorID := finalizedAssistantForRegenerate(t, h, env)

	rec := httptest.NewRecorder()
	h.Regenerate(rec, regenerateRequest(t, sessionID, priorID, env.other, []string{env.hubA, env.hubB}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner Regenerate = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChatHandler_Regenerate_ArchivedSession(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	sessionID, priorID := finalizedAssistantForRegenerate(t, h, env)

	// Archive directly — the patch handler exists but for this
	// test we just need the row state. Mirrors the
	// postgres_chat_test.go approach.
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE chat_sessions SET archived_at = now() WHERE id = $1::uuid`, sessionID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	rec := httptest.NewRecorder()
	h.Regenerate(rec, regenerateRequest(t, sessionID, priorID, env.owner, []string{env.hubA}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("Regenerate archived = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "chat_session_archived") {
		t.Errorf("body should carry chat_session_archived code; got %s", rec.Body.String())
	}
}
