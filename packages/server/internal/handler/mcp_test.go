package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestChatGPTMCPToolsListUsesReviewFriendlySurface(t *testing.T) {
	h := NewChatGPTMCPHandler(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp/chatgpt", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Result struct {
			Tools []mcpTool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	names := make(map[string]mcpTool, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names[tool.Name] = tool
	}

	wantNames := []string{"search_memories", "get_memory", "list_hubs", "list_hub_members", "list_topics", "save_memory", "forget_memory"}
	for _, name := range wantNames {
		if _, ok := names[name]; !ok {
			t.Fatalf("tool %q missing from ChatGPT surface", name)
		}
	}
	if _, ok := names["memax_push"]; ok {
		t.Fatalf("agent-oriented tool name memax_push leaked into ChatGPT surface")
	}

	assertBoolAnnotation(t, names["search_memories"], "readOnlyHint", true)
	assertBoolAnnotation(t, names["save_memory"], "readOnlyHint", false)
	assertBoolAnnotation(t, names["save_memory"], "destructiveHint", false)
	assertBoolAnnotation(t, names["forget_memory"], "destructiveHint", true)
}

func TestChatGPTMCPInitializeUsesChatGPTServerInfo(t *testing.T) {
	h := NewChatGPTMCPHandler(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp/chatgpt", strings.NewReader(`{"jsonrpc":"2.0","id":"init","method":"initialize"}`))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if got := w.Header().Get("Mcp-Session-Id"); got == "" {
		t.Fatalf("Mcp-Session-Id header missing")
	}

	var resp struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Result.ServerInfo.Name != "memax-chatgpt" {
		t.Fatalf("server name = %q, want memax-chatgpt", resp.Result.ServerInfo.Name)
	}
	if !strings.Contains(resp.Result.Instructions, "only call save_memory or forget_memory") {
		t.Fatalf("instructions do not constrain write/destructive tools: %q", resp.Result.Instructions)
	}
}

func TestProtectedResourceMetadataCanDescribeChatGPTEndpoint(t *testing.T) {
	t.Setenv("API_BASE_URL", "https://api.memax.test")
	h := NewMCPOAuthHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/chatgpt", nil)
	w := httptest.NewRecorder()

	h.ProtectedResourceMetadata(w, req)

	var metadata struct {
		Resource string `json:"resource"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata.Resource != "https://api.memax.test/mcp/chatgpt" {
		t.Fatalf("resource = %q, want ChatGPT MCP endpoint", metadata.Resource)
	}
}

func TestMCPUnauthorizedChallengeUsesEndpointSpecificResourceMetadata(t *testing.T) {
	t.Setenv("API_BASE_URL", "https://api.memax.test")
	auth := RequireAuth([]byte("secret"), nil, nil)
	protected := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("protected handler should not be called")
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp/chatgpt", nil)
	w := httptest.NewRecorder()

	protected.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	challenge := w.Header().Get("WWW-Authenticate")
	want := `resource_metadata="https://api.memax.test/.well-known/oauth-protected-resource/mcp/chatgpt"`
	if !strings.Contains(challenge, want) {
		t.Fatalf("WWW-Authenticate = %q, want it to contain %q", challenge, want)
	}
}

func TestMCPOAuthScopeMapping(t *testing.T) {
	perms, normalized, invalid := oauthPermissionsFromScope("memax:read memax:write")
	if len(invalid) != 0 {
		t.Fatalf("invalid = %v, want none", invalid)
	}
	if normalized != "memax:read memax:write" {
		t.Fatalf("normalized = %q", normalized)
	}
	for _, perm := range []Permission{PermMemoryRead, PermTopicRead, PermDreamRead, PermHubRead, PermHubMembersRead, PermMemoryWrite} {
		if !perms.Has(perm) {
			t.Fatalf("expected scope mapping to include %s", perm)
		}
	}
	if perms.Has(PermMemoryDelete) {
		t.Fatalf("memax:write should not grant delete")
	}
}

func TestMCPOAuthScopeMappingRejectsUnknown(t *testing.T) {
	_, _, invalid := oauthPermissionsFromScope("memax:read memax:admin")
	if len(invalid) != 1 || invalid[0] != "memax:admin" {
		t.Fatalf("invalid = %v, want memax:admin", invalid)
	}
}

func TestMCPOAuthRedirectURIValidation(t *testing.T) {
	valid := []string{
		"https://chat.openai.com/aip/g-123/oauth/callback",
		"http://localhost:1455/callback",
		"http://127.0.0.1:1455/callback",
	}
	for _, uri := range valid {
		if !validOAuthRedirectURI(uri) {
			t.Fatalf("expected valid redirect URI: %s", uri)
		}
	}

	invalid := []string{
		"javascript:alert(1)",
		"http://example.com/callback",
		"/relative/callback",
	}
	for _, uri := range invalid {
		if validOAuthRedirectURI(uri) {
			t.Fatalf("expected invalid redirect URI: %s", uri)
		}
	}
}

func TestMCPOAuthRedirectsToClientCallbackWithSeeOther(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/consent", nil)
	w := httptest.NewRecorder()

	redirectWithCode(w, req, "https://claude.ai/api/mcp/auth_callback", "state-1", "code-1")

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "code=code-1") || !strings.Contains(location, "state=state-1") {
		t.Fatalf("Location = %q, want code and state query params", location)
	}
}

func TestMCPOAuthErrorRedirectsToClientCallbackWithSeeOther(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/consent", nil)
	w := httptest.NewRecorder()

	redirectOAuthError(w, req, "https://claude.ai/api/mcp/auth_callback", "state-1", "access_denied", "Canceled")

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	location := w.Header().Get("Location")
	for _, want := range []string{"error=access_denied", "error_description=Canceled", "state=state-1"} {
		if !strings.Contains(location, want) {
			t.Fatalf("Location = %q, want %q", location, want)
		}
	}
}

func TestConsentDecisionOnlyDeniesExplicitCancel(t *testing.T) {
	cases := map[string]bool{
		"deny":    true,
		" deny ":  true,
		"DENY":    true,
		"approve": false,
		"":        false,
		"unknown": false,
	}
	for decision, want := range cases {
		if got := consentDecisionDenied(decision); got != want {
			t.Fatalf("consentDecisionDenied(%q) = %t, want %t", decision, got, want)
		}
	}
}

func TestMCPOAuthWebConsentURLUsesConfiguredAppBaseURL(t *testing.T) {
	t.Setenv("APP_BASE_URL", "https://app.memax.test/")
	h := NewMCPOAuthHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil)

	got := h.webConsentURL(req, "request-1", "token-1")

	if !strings.HasPrefix(got, "https://app.memax.test/oauth/consent?") {
		t.Fatalf("consent URL = %q, want configured app consent route", got)
	}
	for _, want := range []string{"request_id=request-1", "consent_token=token-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("consent URL = %q, want %q", got, want)
		}
	}
}

func TestMCPOAuthWebConsentURLInfersStagingAppFromAPIBaseURL(t *testing.T) {
	t.Setenv("API_BASE_URL", "https://staging-api.memaxlabs.com")
	h := NewMCPOAuthHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil)

	got := h.webConsentURL(req, "request-1", "token-1")

	if !strings.HasPrefix(got, "https://staging-app.memaxlabs.com/oauth/consent?") {
		t.Fatalf("consent URL = %q, want staging app consent route", got)
	}
}

// Regression guard for the MCP → usage_events pipeline. The HTTP
// meter middleware only classifies REST paths (/v1/memories, /v1/recall,
// /v1/ask), so MCP tool calls write usage_events manually via an
// injected LogEventFn. If someone ever removes SetLogEvent or its
// invocation inside a tool handler, the agent-card "Recalled X" line
// silently stops populating for every MCP-native agent — most of our
// real agents. This test asserts the callback actually fires when a
// tool succeeds, with the expected op + summary.
func TestMCPRecallInvokesLogEventWithQuerySummary(t *testing.T) {
	s := store.NewInMemoryStore()
	recallH := NewRecallHandler(s, nil, nil, nil, nil)
	h := NewMCPHandler(s, recallH, nil, nil)

	type captured struct {
		userID    string
		op        string
		hubID     string
		source    string
		agentName string
		metadata  map[string]any
	}
	var (
		mu   sync.Mutex
		logs []captured
	)
	h.SetLogEvent(func(userID, op, hubID, source, agentName string, metadata map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, captured{
			userID: userID, op: op, hubID: hubID, source: source,
			agentName: agentName, metadata: metadata,
		})
	})

	// Active read hub set via hubIDKey — mirrors what HubContext
	// middleware does for a real MCP request with a personal hub.
	// The recall log must attribute to this hub for parity with the
	// REST meter's resolveBillingHub path.
	const activeHubID = "hub-personal-u1"

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memax_recall","arguments":{"query":"how does caching work"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			"": NewPermissionSet(PermMemoryRead),
		},
	})
	ctx = context.WithValue(ctx, agentNameKey, "claude-code")
	ctx = context.WithValue(ctx, hubIDKey, activeHubID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 {
		t.Fatalf("expected 1 LogEvent call, got %d", len(logs))
	}
	got := logs[0]
	if got.userID != "u1" {
		t.Errorf("userID = %q, want u1", got.userID)
	}
	if got.op != "recall" {
		t.Errorf("op = %q, want recall", got.op)
	}
	if got.source != "mcp" {
		t.Errorf("source = %q, want mcp", got.source)
	}
	if got.agentName != "claude-code" {
		t.Errorf("agentName = %q, want claude-code", got.agentName)
	}
	if got.hubID != activeHubID {
		t.Errorf("hubID = %q, want %q — regression in GetHubID(r) fallback for analytics parity", got.hubID, activeHubID)
	}
	if summary, _ := got.metadata["summary"].(string); summary != "how does caching work" {
		t.Errorf("metadata.summary = %q, want the query text", summary)
	}
}

// Nil LogEventFn must not crash any tool path. Dev/memory-mode runs
// without a meter wired; the handlers still need to work.
func TestMCPRecallTolerantOfNilLogEvent(t *testing.T) {
	s := store.NewInMemoryStore()
	recallH := NewRecallHandler(s, nil, nil, nil, nil)
	h := NewMCPHandler(s, recallH, nil, nil)
	// Deliberately do NOT call SetLogEvent.

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memax_recall","arguments":{"query":"anything"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			"": NewPermissionSet(PermMemoryRead),
		},
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	// Must not panic.
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}
}

// Push-side regression guard for the MCP → usage_events pipeline.
// toolPush has two semantics the caller cares about beyond "a row
// gets written":
//   - agent_name comes from the POST-heal sourceAgent (resolved by
//     resolveMemoryProvenance), not the pre-heal GetAgentName(r). For
//     an auth-path principal those match, but the test still pins the
//     field so a future refactor that swaps to GetAgentName surfaces.
//   - summary runs through compactActivitySummary(…, 96) so multiline
//     or long titles don't break the agent-card layout. We feed a
//     title with embedded newlines and >96 runes to assert both the
//     whitespace collapse and the truncation with an ellipsis.
func TestMCPPushInvokesLogEventWithCompactedSummary(t *testing.T) {
	s := store.NewInMemoryStore()
	personalHubID := "hub-personal-u1"
	if err := s.CreateHub(&model.Hub{
		ID:      personalHubID,
		OwnerID: "u1",
		Name:    "Personal",
		Slug:    "personal",
		HubType: "personal",
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	h := NewMCPHandler(s, nil, nil, nil)

	var (
		mu   sync.Mutex
		logs []map[string]any
	)
	h.SetLogEvent(func(userID, op, hubID, source, agentName string, metadata map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, map[string]any{
			"userID": userID, "op": op, "hubID": hubID,
			"source": source, "agentName": agentName, "metadata": metadata,
		})
	})

	// Title that will stress both normalizations: a newline in the
	// middle + total length well over 96 runes.
	longTitle := "Meeting notes:\nQ3 planning with the platform team — action items, open questions, and next steps for shipping the new pipeline"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memax_push","arguments":{"content":"body","title":` + jsonString(longTitle) + `,"hub_id":"personal"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			personalHubID: NewPermissionSet(PermMemoryRead, PermMemoryWrite),
		},
	})
	ctx = context.WithValue(ctx, agentNameKey, "claude-code")
	ctx = context.WithValue(ctx, hubIDsKey, []string{personalHubID})
	ctx = context.WithValue(ctx, writeHubIDKey, personalHubID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 {
		t.Fatalf("expected 1 LogEvent call, got %d", len(logs))
	}
	got := logs[0]
	if got["op"] != "push" {
		t.Errorf("op = %v, want push", got["op"])
	}
	if got["source"] != "mcp" {
		t.Errorf("source = %v, want mcp", got["source"])
	}
	if got["agentName"] != "claude-code" {
		t.Errorf("agentName = %v, want claude-code (post-heal sourceAgent)", got["agentName"])
	}
	if got["hubID"] != personalHubID {
		t.Errorf("hubID = %v, want %s", got["hubID"], personalHubID)
	}
	meta, _ := got["metadata"].(map[string]any)
	summary, _ := meta["summary"].(string)
	if strings.Contains(summary, "\n") {
		t.Errorf("summary should have no newlines; got %q", summary)
	}
	if runes := []rune(summary); len(runes) > 96 {
		t.Errorf("summary length %d runes exceeds 96", len(runes))
	}
	if !strings.HasSuffix(summary, "…") {
		t.Errorf("summary should be ellipsis-truncated; got %q", summary)
	}
}

// Regression guard specifically for the post-heal sourceAgent
// semantics. TestMCPPushInvokesLogEventWithCompactedSummary above
// uses an auth-path request where GetAgentName(r) already matches
// the final sourceAgent — meaning a regression to GetAgentName
// would still pass. This test takes the claim path instead:
//
//   - agentNameKey is unset, so GetAgentName(r) == "".
//   - grantContext identifies the principal as an api_key with no
//     pinned agent_name, so canClaimAgentFromRequest returns true.
//   - source_agent in the tool args provides the claimed slug.
//   - resolveMemoryProvenance accepts the claim, producing
//     sourceAgent="custom-agent-slug".
//
// If the LogEvent call ever reverts to passing GetAgentName(r),
// the agentName assertion below catches it because "" ≠
// "custom-agent-slug".
func TestMCPPushLogEventUsesPostHealSourceAgentOnClaimPath(t *testing.T) {
	s := store.NewInMemoryStore()
	personalHubID := "hub-personal-u1"
	if err := s.CreateHub(&model.Hub{
		ID:      personalHubID,
		OwnerID: "u1",
		Name:    "Personal",
		Slug:    "personal",
		HubType: "personal",
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	h := NewMCPHandler(s, nil, nil, nil)

	var (
		mu   sync.Mutex
		logs []map[string]any
	)
	h.SetLogEvent(func(userID, op, hubID, source, agentName string, metadata map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, map[string]any{
			"op": op, "hubID": hubID, "source": source,
			"agentName": agentName, "metadata": metadata,
		})
	})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memax_push","arguments":{"content":"body","title":"A claimed push","hub_id":"personal","source_agent":"custom-agent-slug"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			personalHubID: NewPermissionSet(PermMemoryRead, PermMemoryWrite),
		},
	})
	// Deliberately no agentNameKey — GetAgentName(r) must return "".
	ctx = context.WithValue(ctx, grantContextKey, GrantContext{
		UserID:        "u1",
		PrincipalType: "api_key",
	})
	ctx = context.WithValue(ctx, hubIDsKey, []string{personalHubID})
	ctx = context.WithValue(ctx, writeHubIDKey, personalHubID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 {
		t.Fatalf("expected 1 LogEvent call, got %d", len(logs))
	}
	got := logs[0]
	if got["agentName"] != "custom-agent-slug" {
		t.Errorf("agentName = %v, want custom-agent-slug — regression to GetAgentName(r) would yield \"\"", got["agentName"])
	}
}

// Capture's LogEvent contract differs from push in two places that
// are easy to regress: op stays "push" (capture is a write, same
// cost profile) but source becomes "mcp/capture" so analytics can
// slice it out. Summary normalization matches push.
func TestMCPCaptureInvokesLogEventWithCaptureSource(t *testing.T) {
	s := store.NewInMemoryStore()
	personalHubID := "hub-personal-u1"
	if err := s.CreateHub(&model.Hub{
		ID:      personalHubID,
		OwnerID: "u1",
		Name:    "Personal",
		Slug:    "personal",
		HubType: "personal",
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	h := NewMCPHandler(s, nil, nil, nil)

	var (
		mu   sync.Mutex
		logs []map[string]any
	)
	h.SetLogEvent(func(userID, op, hubID, source, agentName string, metadata map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, map[string]any{
			"op": op, "hubID": hubID, "source": source,
			"agentName": agentName, "metadata": metadata,
		})
	})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memax_capture","arguments":{"summary":"  Paired on the retrieval ranker:\n  rerank threshold tuned.  ","decisions":["adopt cohere v3"],"learnings":["boost recency weight"]}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			personalHubID: NewPermissionSet(PermMemoryRead, PermMemoryWrite),
		},
	})
	ctx = context.WithValue(ctx, agentNameKey, "claude-code")
	ctx = context.WithValue(ctx, hubIDsKey, []string{personalHubID})
	ctx = context.WithValue(ctx, writeHubIDKey, personalHubID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 {
		t.Fatalf("expected 1 LogEvent call, got %d", len(logs))
	}
	got := logs[0]
	if got["op"] != "push" {
		t.Errorf("op = %v, want push (capture is a write, cost profile matches push)", got["op"])
	}
	if got["source"] != "mcp/capture" {
		t.Errorf("source = %v, want mcp/capture", got["source"])
	}
	if got["agentName"] != "claude-code" {
		t.Errorf("agentName = %v, want claude-code", got["agentName"])
	}
	if got["hubID"] != personalHubID {
		t.Errorf("hubID = %v, want %s", got["hubID"], personalHubID)
	}
	meta, _ := got["metadata"].(map[string]any)
	summary, _ := meta["summary"].(string)
	if strings.Contains(summary, "\n") {
		t.Errorf("summary should be whitespace-compacted; got %q", summary)
	}
	if strings.HasPrefix(summary, " ") || strings.HasSuffix(summary, " ") {
		t.Errorf("summary should be trimmed; got %q", summary)
	}
}

// jsonString escapes s into a JSON-quoted literal so test bodies can
// embed titles/summaries containing newlines and special characters
// without hand-escaping. Fails the test on impossible errors — we're
// marshaling a plain string.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestMCPToolDeniedWhenGrantLacksPermission(t *testing.T) {
	h := NewMCPHandler(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memax_list","arguments":{}}}`))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			"hub1": NewPermissionSet(PermMemoryWrite),
		},
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp struct {
		Result mcpToolResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Result.IsError {
		t.Fatalf("expected MCP tool error when memory:read is missing")
	}
	if got := resp.Result.Content[0].Text; !strings.Contains(got, "not allowed") {
		t.Fatalf("response text = %q, want permission denial", got)
	}
}

func TestAgentNameFromClientNameRecognizesChatGPT(t *testing.T) {
	cases := map[string]string{
		"ChatGPT":             "chatgpt",
		"OpenAI ChatGPT Apps": "chatgpt",
		"Claude Desktop":      "claude-ai",
		"Claude Code":         "claude-code",
		"Gemini CLI":          "gemini",
		"Codex CLI":           "codex",
		"Cursor":              "cursor",
		"Windsurf":            "windsurf",
		"OpenCode":            "opencode",
		"OpenClaw":            "openclaw",
	}
	for clientName, want := range cases {
		if got := agentNameFromClientName(clientName); got != want {
			t.Fatalf("agentNameFromClientName(%q) = %q, want %q", clientName, got, want)
		}
	}
}

func assertBoolAnnotation(t *testing.T, tool mcpTool, key string, want bool) {
	t.Helper()
	got, ok := tool.Annotations[key].(bool)
	if !ok {
		t.Fatalf("%s annotation missing or not bool on tool %s: %#v", key, tool.Name, tool.Annotations)
	}
	if got != want {
		t.Fatalf("%s annotation on tool %s = %t, want %t", key, tool.Name, got, want)
	}
}
