package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// protocolServer serves both profiles over HTTP for a signed-in user with
// a personal hub, as the auth and hub middleware would leave the request.
func protocolServer(t *testing.T) (*httptest.Server, *MCPHandler) {
	t.Helper()
	s := store.NewInMemoryStore()
	if err := s.CreateHub(&model.Hub{ID: "hub-personal-u1", OwnerID: "u1", Name: "Personal", Slug: "personal", HubType: "personal"}); err != nil {
		t.Fatal(err)
	}
	recallH := NewRecallHandler(s, nil, nil, nil, nil)
	agent := NewMCPHandler(s, recallH, nil, nil)
	agent.SetInstance("m1")
	chat := NewChatGPTMCPHandler(s, recallH, nil, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", agent)
	mux.Handle("/mcp/chatgpt", chat)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userIDKey, "u1")
		ctx = context.WithValue(ctx, agentNameKey, "claude-code")
		ctx = context.WithValue(ctx, hubIDKey, "hub-personal-u1")
		ctx = context.WithValue(ctx, hubIDsKey, []string{"hub-personal-u1"})
		ctx = context.WithValue(ctx, writeHubIDKey, "hub-personal-u1")
		ctx = context.WithValue(ctx, authContextKey, &AuthContext{UserID: "u1", HubScopeMode: HubScopeAllAccessible,
			PermissionsByHub: map[string]PermissionSet{"hub-personal-u1": NewPermissionSet(PermMemoryRead, PermMemoryWrite,
				PermMemoryDelete, PermHubRead, PermHubMembersRead, PermTopicRead)}})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return srv, agent
}

func connectProtocol(t *testing.T, url, version string, elicit bool) *mcp.ClientSession {
	t.Helper()
	opts := &mcp.ClientOptions{}
	if elicit {
		opts.ElicitationHandler = func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "decline"}, nil
		}
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "protocol-test", Version: "1"}, opts).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: url, MaxRetries: -1}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatalf("connect %s: %v", version, err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// Every supported protocol version negotiates on the same endpoint: the
// 2026-07-28 client through server/discover, older ones through
// initialize, and each lists and calls the tools.
func TestMCPBothErasNegotiate(t *testing.T) {
	srv, _ := protocolServer(t)
	for _, version := range []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"} {
		for _, elicit := range []bool{false, true} {
			t.Run(version, func(t *testing.T) {
				cs := connectProtocol(t, srv.URL+"/mcp", version, elicit)
				init := cs.InitializeResult()
				if init.ProtocolVersion != version {
					t.Errorf("negotiated %s, want %s", init.ProtocolVersion, version)
				}
				if init.ServerInfo == nil || init.ServerInfo.Name != "memax" || init.ServerInfo.Version != MCPServerVersion {
					t.Errorf("server info = %+v", init.ServerInfo)
				}
				tools, err := cs.ListTools(context.Background(), nil)
				if err != nil || len(tools.Tools) != 11 {
					t.Fatalf("tools/list: %v (%d tools)", err, len(tools.Tools))
				}
				res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_hubs"})
				if err != nil || res.IsError || !strings.Contains(textOf(res), "**Personal**") {
					t.Errorf("memax_hubs: %v %s", err, textOf(res))
				}
			})
		}
	}
}

// server/discover (2026-07-28) answers with the versions, capabilities,
// instructions and cache hints; tools/list carries cache hints too.
func TestMCPDiscoverAndCacheHints(t *testing.T) {
	srv, _ := protocolServer(t)
	meta := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "raw", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	for _, method := range []string{"server/discover", "tools/list"} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": map[string]any{"_meta": meta}})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", method)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var out struct {
			Result map[string]any `json:"result"`
		}
		if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || out.Result == nil {
			t.Fatalf("%s: %d %s", method, res.StatusCode, raw)
		}
		if out.Result["ttlMs"] != float64(300000) || out.Result["cacheScope"] != "public" || out.Result["resultType"] != "complete" {
			t.Errorf("%s cache hints: %v %v %v", method, out.Result["ttlMs"], out.Result["cacheScope"], out.Result["resultType"])
		}
		if res.Header.Get("Mcp-Session-Id") != "" {
			t.Errorf("%s: a stateless request got a session", method)
		}
		if method == "server/discover" {
			versions, _ := out.Result["supportedVersions"].([]any)
			if len(versions) != 5 || out.Result["instructions"] == "" || out.Result["capabilities"].(map[string]any)["tools"] == nil {
				t.Errorf("discover = %s", raw)
			}
		}
	}
}

// Every tool has a title and annotations: read tools are read-only, forget
// is destructive, nothing is open-world. Descriptions say what a tool does,
// not how an agent should behave (directory review).
func TestMCPToolMetadata(t *testing.T) {
	reads := map[string]bool{"memax_recall": true, "memax_search": true, "memax_get": true, "memax_list": true,
		"memax_hubs": true, "memax_hub_members": true, "memax_topics": true, "search_memories": true,
		"get_memory": true, "list_hubs": true, "list_hub_members": true, "list_topics": true}
	destructive := map[string]bool{"memax_forget": true, "forget_memory": true}
	steering := []string{"ALWAYS", "Always ", "Use this when", "Use this after", "Do NOT", "Don't ", "Call at the end", "You must", "should call"}
	for _, profile := range []string{"agent", "chatgpt"} {
		tools, err := mcpProfileTools(profile)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[string]int{"agent": 11, "chatgpt": 7}[profile]; len(tools) != want {
			t.Errorf("%s profile has %d tools, want %d", profile, len(tools), want)
		}
		for _, tool := range tools {
			a := tool.Annotations
			switch {
			case tool.Title == "" || a == nil || a.Title == "":
				t.Errorf("%s has no title", tool.Name)
			case a.OpenWorldHint == nil || *a.OpenWorldHint:
				t.Errorf("%s: openWorldHint must be false", tool.Name)
			case a.ReadOnlyHint != reads[tool.Name]:
				t.Errorf("%s: readOnlyHint = %t", tool.Name, a.ReadOnlyHint)
			case !reads[tool.Name] && (a.DestructiveHint == nil || *a.DestructiveHint != destructive[tool.Name]):
				t.Errorf("%s: destructiveHint = %v", tool.Name, a.DestructiveHint)
			}
			for _, s := range steering {
				if strings.Contains(tool.Description, s) {
					t.Errorf("%s description steers the agent (%q): %s", tool.Name, s, tool.Description)
				}
			}
			if out := map[string]bool{"memax_recall": true, "memax_search": true, "memax_get": true, "memax_list": true,
				"memax_push": true, "search_memories": true, "get_memory": true, "save_memory": true}[tool.Name]; out != (tool.OutputSchema != nil) {
				t.Errorf("%s: output schema present = %t", tool.Name, tool.OutputSchema != nil)
			}
		}
	}
}

// The read tools' structured content validates against their output
// schemas, with the V1 text unchanged beside it.
func TestMCPStructuredOutputValidates(t *testing.T) {
	srv, _ := protocolServer(t)
	cs := connectProtocol(t, srv.URL+"/mcp", "2025-11-25", false)
	push, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_push", Arguments: map[string]any{"content": "Structured output carries a text mirror", "title": "Mirror"}})
	if err != nil || push.IsError {
		t.Fatalf("push: %v %s", err, textOf(push))
	}
	var pushed MCPPushOutput
	remarshalTest(t, push.StructuredContent, &pushed)
	calls := map[string]map[string]any{
		"memax_push":   nil,
		"memax_recall": {"query": "text mirror"},
		"memax_search": {"query": "text mirror"},
		"memax_list":   {},
		"memax_get":    {"id": pushed.ID},
	}
	for name, args := range calls {
		res := push
		if args != nil {
			if res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args}); err != nil || res.IsError {
				t.Fatalf("%s: %v %s", name, err, textOf(res))
			}
		}
		if res.StructuredContent == nil || textOf(res) == "" {
			t.Errorf("%s: structured %v, text %q", name, res.StructuredContent, textOf(res))
			continue
		}
		raw, err := MCPOutputSchema("agent", name)
		if err != nil {
			t.Fatal(err)
		}
		sch := compileSchema(t, raw)
		var inst any
		remarshalTest(t, res.StructuredContent, &inst)
		if err := sch.Validate(inst); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !strings.HasPrefix(textOf(push), "Saved: Mirror (id: ") {
		t.Errorf("V1 push text changed: %q", textOf(push))
	}
}

// A legacy session exists only for a client that can answer
// elicitation/create; its ID names this machine. Everything else is
// served statelessly.
func TestMCPLegacySessionOnlyForElicitation(t *testing.T) {
	srv, _ := protocolServer(t)
	for elicit, wantSession := range map[string]bool{`"elicitation":{}`: true, `"elicitation":{"form":{}}`: true, `"roots":{}`: false} {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{` + elicit + `},"clientInfo":{"name":"c","version":"1"}}}`
		res := rawPost(t, srv.URL+"/mcp", body, nil)
		sid := res.Header.Get("Mcp-Session-Id")
		if (sid != "") != wantSession {
			t.Errorf("%s: session %q, want session %t", elicit, sid, wantSession)
		}
		if wantSession && !strings.HasPrefix(sid, "m1.") {
			t.Errorf("session %q doesn't name the machine m1", sid)
		}
	}
}

// A session this process doesn't hold is replayed on the machine that
// minted it (fly-replay), or, when that's gone or unknown, served
// statelessly instead of failing.
func TestMCPSessionRouting(t *testing.T) {
	srv, _ := protocolServer(t)
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	res := rawPost(t, srv.URL+"/mcp", list, map[string]string{"Mcp-Session-Id": "m2.abc"})
	if res.StatusCode != http.StatusTemporaryRedirect || res.Header.Get("Fly-Replay") != "prefer_instance=m2" {
		t.Errorf("another machine's session: %d %q", res.StatusCode, res.Header.Get("Fly-Replay"))
	}
	for name, h := range map[string]map[string]string{
		"replayed":         {"Mcp-Session-Id": "m2.abc", "Fly-Replay-Src": "instance=m1"},
		"machine gone":     {"Mcp-Session-Id": "m2.abc", "Fly-Preferred-Instance-Unavailable": "m2"},
		"expired":          {"Mcp-Session-Id": "m1.expired"},
		"pre-machine ID":   {"Mcp-Session-Id": "0123456789abcdef"},
		"without a header": nil,
	} {
		res := rawPost(t, srv.URL+"/mcp", list, h)
		if res.StatusCode != http.StatusOK || !strings.Contains(res.body, `"memax_recall"`) {
			t.Errorf("%s: %d %s", name, res.StatusCode, res.body)
		}
	}
}

type rawResponse struct {
	*http.Response
	body string
}

func rawPost(t *testing.T, url, body string, headers map[string]string) rawResponse {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return rawResponse{res, string(b)}
}

func textOf(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func remarshalTest(t *testing.T, from, to any) {
	t.Helper()
	b, err := json.Marshal(from)
	if err != nil || json.Unmarshal(b, to) != nil {
		t.Fatalf("remarshal %T: %v", from, err)
	}
}

func compileSchema(t *testing.T, raw json.RawMessage) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}
