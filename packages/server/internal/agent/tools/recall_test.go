package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	memaxmodel "github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeMembership implements agent.MembershipReader for tests.
// Mirrors the shape used in the agent package's scope_test.go
// but duplicated here to keep the tests independent — a
// refactor of the resolver internals shouldn't ripple into
// tool tests. Recall is a READ tool; tests only care about
// hub membership for scope resolution, so role defaults to
// "owner" (writable) — recall doesn't read WritableHubIDs.
type fakeMembership struct {
	hubs map[string][]string
}

func (f *fakeMembership) HubMembershipsForUser(_ context.Context, userID string) ([]memaxmodel.HubMembership, error) {
	ids := f.hubs[userID]
	out := make([]memaxmodel.HubMembership, len(ids))
	for i, id := range ids {
		out[i] = memaxmodel.HubMembership{HubID: id, Role: "owner"}
	}
	return out, nil
}

// fakeRecallService captures whatever VisibilityScope the tool
// builds. Tests assert on cap.lastScope to verify the security
// pattern produced the expected scope (right OwnerID, right HubIDs).
// recall results are canned so the tool's output formatting can
// be tested without a real search backend.
type fakeRecallService struct {
	results   []RecallResult
	err       error
	lastReq   AgentRecallRequest // captured for assertions
	callCount int
}

func (f *fakeRecallService) AgentRecall(_ context.Context, req AgentRecallRequest) (AgentRecallResponse, error) {
	f.callCount++
	f.lastReq = req
	if f.err != nil {
		return AgentRecallResponse{}, f.err
	}
	return AgentRecallResponse{Memories: f.results}, nil
}

// newToolForTest is the standard test setup. Returns a tool wired
// against a single-hub session with a fake recall service. Tests
// override fields on the returned config as needed.
func newToolForTest(t *testing.T, ownerID string, hubIDs []string) (sdktool.Tool, *fakeRecallService) {
	t.Helper()
	svc := &fakeRecallService{}
	resolver := agent.NewScopeResolver(&fakeMembership{
		hubs: map[string][]string{ownerID: hubIDs},
	})
	desc := agent.SessionDescriptor{
		OwnerID: ownerID,
		Type:    agent.ScopeTypeHubSubset,
		HubIDs:  hubIDs,
	}
	tool, err := NewRecallTool(RecallToolConfig{
		Service:  svc,
		Resolver: resolver,
		Session:  desc,
	})
	if err != nil {
		t.Fatalf("NewRecallTool: %v", err)
	}
	return tool, svc
}

// callTool invokes the tool with the given args. Returns the
// ToolResult so tests can assert on Content / IsError.
func callTool(t *testing.T, tool sdktool.Tool, args RecallArgs) model.ToolResult {
	t.Helper()
	input, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := tool.Execute(context.Background(), sdktool.Call{
		Use: model.ToolUse{
			ID:    "tool_call_test_id",
			Name:  RecallMemoriesName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- spec / construction ---

func TestRecallToolSpec(t *testing.T) {
	tool, _ := newToolForTest(t, "u1", []string{"h1"})
	spec := tool.Spec()
	if spec.Name != RecallMemoriesName {
		t.Errorf("Name = %q, want %q", spec.Name, RecallMemoriesName)
	}
	if !spec.ReadOnly {
		t.Error("expected ReadOnly=true (recall is a read tool)")
	}
	if !spec.ConcurrencySafe {
		t.Error("expected ConcurrencySafe=true (parallel recall fan-out is fine)")
	}
	if spec.Destructive {
		t.Error("recall must not be marked Destructive")
	}
	if spec.Description == "" {
		t.Error("Description is required for the model to discover the tool")
	}
}

func TestNewRecallToolRejectsBadConfig(t *testing.T) {
	resolver := agent.NewScopeResolver(&fakeMembership{})
	desc := agent.SessionDescriptor{
		OwnerID: "u1", Type: agent.ScopeTypeUserAllHubs,
	}
	cases := []struct {
		name    string
		cfg     RecallToolConfig
		wantErr string
	}{
		{
			name:    "missing service",
			cfg:     RecallToolConfig{Resolver: resolver, Session: desc},
			wantErr: "Service is required",
		},
		{
			name:    "missing resolver",
			cfg:     RecallToolConfig{Service: &fakeRecallService{}, Session: desc},
			wantErr: "Resolver is required",
		},
		{
			name: "invalid session descriptor",
			cfg: RecallToolConfig{
				Service:  &fakeRecallService{},
				Resolver: resolver,
				Session:  agent.SessionDescriptor{}, // missing OwnerID
			},
			wantErr: "invalid session descriptor",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRecallTool(tc.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

// --- input validation ---

func TestExecuteRejectsEmptyQuery(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	result := callTool(t, tool, RecallArgs{Query: ""})
	if !result.IsError {
		t.Fatal("expected IsError=true on empty query")
	}
	if !strings.Contains(result.Content, "query is required") {
		t.Errorf("Content = %q, want 'query is required'", result.Content)
	}
	if svc.callCount != 0 {
		t.Errorf("service was called %d times, expected 0", svc.callCount)
	}
}

func TestExecuteRejectsWhitespaceQuery(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	result := callTool(t, tool, RecallArgs{Query: "   \t\n  "})
	if !result.IsError {
		t.Fatal("expected IsError on whitespace-only query")
	}
	if svc.callCount != 0 {
		t.Errorf("service was called %d times for whitespace query", svc.callCount)
	}
}

func TestExecuteRejectsLimitOutOfRange(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	for _, badLimit := range []int{-1, 21, 100} {
		result := callTool(t, tool, RecallArgs{Query: "x", Limit: badLimit})
		if !result.IsError {
			t.Errorf("limit=%d: expected IsError", badLimit)
		}
	}
	if svc.callCount != 0 {
		t.Errorf("service was called for out-of-range limits")
	}
}

func TestExecuteAppliesDefaultLimit(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	callTool(t, tool, RecallArgs{Query: "x"})
	if svc.lastReq.Limit != defaultRecallLimit {
		t.Errorf("Limit = %d, want default %d", svc.lastReq.Limit, defaultRecallLimit)
	}
}

// --- security pattern: scope construction ---

func TestExecuteFansOutAcrossScopeWhenHubIDOmitted(t *testing.T) {
	// Multi-hub session: omitted hub_id → scope.HubIDs flows
	// into VisibilityScope unchanged. This is the "where did we
	// decide X?" cross-hub recall path.
	tool, svc := newToolForTest(t, "u1", []string{"h-work", "h-personal", "h-research"})
	callTool(t, tool, RecallArgs{Query: "auth migration"})
	if svc.lastReq.OwnerID != "u1" {
		t.Errorf("Scope.OwnerID = %q, want u1", svc.lastReq.OwnerID)
	}
	got := svc.lastReq.HubIDs
	want := []string{"h-work", "h-personal", "h-research"}
	if len(got) != len(want) {
		t.Fatalf("Scope.HubIDs = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Scope.HubIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExecuteNarrowsScopeWhenHubIDProvided(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h-work", "h-personal"})
	callTool(t, tool, RecallArgs{Query: "x", HubID: "h-work"})
	if len(svc.lastReq.HubIDs) != 1 || svc.lastReq.HubIDs[0] != "h-work" {
		t.Errorf("Scope.HubIDs = %v, want [h-work]", svc.lastReq.HubIDs)
	}
}

func TestExecuteRejectsHubIDOutsideScope(t *testing.T) {
	// Caller (the model) tries to query a hub the session doesn't
	// have access to. Tool MUST refuse — never narrow to an
	// unallowed hub. This is the load-bearing security check.
	tool, svc := newToolForTest(t, "u1", []string{"h-work"})
	result := callTool(t, tool, RecallArgs{Query: "x", HubID: "h-other"})
	if !result.IsError {
		t.Fatal("expected IsError when hub_id outside scope")
	}
	if !strings.Contains(result.Content, "not in this session's scope") {
		t.Errorf("Content = %q, want 'not in this session's scope'", result.Content)
	}
	if svc.callCount != 0 {
		t.Errorf("service called for out-of-scope hub_id; security pattern broken")
	}
}

// CRITICAL test: the empty-HubIDs case. ScopeResolver returns an
// empty scope when the user has lost access to every hub in their
// session. The tool MUST reject explicitly — passing empty HubIDs
// into store.VisibilityScope falls back to owner-only reads.
func TestExecuteRejectsEmptyResolvedScope(t *testing.T) {
	// User has no membership, so scope.HubIDs resolves to empty
	// even though the session was created with [h-gone].
	resolver := agent.NewScopeResolver(&fakeMembership{
		hubs: map[string][]string{"u1": {}}, // user lost membership
	})
	desc := agent.SessionDescriptor{
		OwnerID: "u1",
		Type:    agent.ScopeTypeHubSubset,
		HubIDs:  []string{"h-gone"},
	}
	svc := &fakeRecallService{}
	tool, err := NewRecallTool(RecallToolConfig{
		Service:  svc,
		Resolver: resolver,
		Session:  desc,
	})
	if err != nil {
		t.Fatalf("NewRecallTool: %v", err)
	}

	result := callTool(t, tool, RecallArgs{Query: "anything"})
	if !result.IsError {
		t.Fatal("expected IsError when scope resolves empty (security leak otherwise)")
	}
	if !strings.Contains(result.Content, "no authorized hub targets") {
		t.Errorf("Content = %q, want 'no authorized hub targets'", result.Content)
	}
	if svc.callCount != 0 {
		t.Fatalf("CRITICAL: service was called with empty scope; would leak owner-only reads via VisibilityScope fallback")
	}
}

func TestExecuteRejectsEmptyResolvedScopeForUserAllHubs(t *testing.T) {
	// Same security check, different scope_type. A user_all_hubs
	// session for a user with zero hub memberships must reject
	// every recall, not silently fall through to owner-only.
	resolver := agent.NewScopeResolver(&fakeMembership{
		hubs: map[string][]string{"u-isolated": {}},
	})
	desc := agent.SessionDescriptor{
		OwnerID: "u-isolated", Type: agent.ScopeTypeUserAllHubs,
	}
	svc := &fakeRecallService{}
	tool, err := NewRecallTool(RecallToolConfig{
		Service: svc, Resolver: resolver, Session: desc,
	})
	if err != nil {
		t.Fatalf("NewRecallTool: %v", err)
	}
	result := callTool(t, tool, RecallArgs{Query: "x"})
	if !result.IsError {
		t.Fatal("expected IsError for user with zero memberships")
	}
	if svc.callCount != 0 {
		t.Errorf("service called with empty scope; security leak")
	}
}

// --- result formatting ---

func TestExecuteEmitsResultsAsJSONArray(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	svc.results = []RecallResult{
		{MemoryID: "mem-1", HubID: "h1", Snippet: "first match", Score: 0.92},
		{MemoryID: "mem-2", HubID: "h1", Snippet: "second match", Score: 0.81},
	}
	result := callTool(t, tool, RecallArgs{Query: "x"})
	if result.IsError {
		t.Fatalf("unexpected IsError: %v", result.Content)
	}

	var got []RecallResult
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("decode result: %v: %q", err, result.Content)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got[0].MemoryID != "mem-1" || got[1].MemoryID != "mem-2" {
		t.Errorf("MemoryID order wrong: %+v", got)
	}
}

func TestExecuteEmitsEmptyArrayOnNoMatches(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	svc.results = nil
	result := callTool(t, tool, RecallArgs{Query: "x"})
	if result.IsError {
		t.Fatalf("unexpected IsError on empty results: %v", result.Content)
	}
	// JSON null vs [] matters — the model's parser may choke on null;
	// json.Marshal of nil slice produces "null", but we want "[]" so
	// the model sees a consistent array shape.
	if result.Content == "null" {
		t.Errorf("Content = null; want []")
	}
}

// --- service error propagation ---

func TestExecuteSurfacesServiceError(t *testing.T) {
	tool, svc := newToolForTest(t, "u1", []string{"h1"})
	svc.err = errors.New("vector-store down")
	result := callTool(t, tool, RecallArgs{Query: "x"})
	if !result.IsError {
		t.Fatal("expected IsError when service fails")
	}
	if !strings.Contains(result.Content, "vector-store down") {
		t.Errorf("Content = %q, expected to contain underlying error", result.Content)
	}
}

// --- single_hub session: scope is one hub, tool fans-out trivially ---

func TestExecuteSingleHubSession(t *testing.T) {
	// Dream sessions always use single_hub. Verify the wrapper
	// produces a one-hub VisibilityScope.
	resolver := agent.NewScopeResolver(&fakeMembership{
		hubs: map[string][]string{"u1": {"h-team"}},
	})
	desc := agent.SessionDescriptor{
		OwnerID: "u1", Type: agent.ScopeTypeSingleHub,
		HubIDs: []string{"h-team"},
	}
	svc := &fakeRecallService{}
	tool, err := NewRecallTool(RecallToolConfig{
		Service: svc, Resolver: resolver, Session: desc,
	})
	if err != nil {
		t.Fatalf("NewRecallTool: %v", err)
	}
	callTool(t, tool, RecallArgs{Query: "x"})
	if len(svc.lastReq.HubIDs) != 1 || svc.lastReq.HubIDs[0] != "h-team" {
		t.Errorf("Scope.HubIDs = %v, want [h-team]", svc.lastReq.HubIDs)
	}
}
