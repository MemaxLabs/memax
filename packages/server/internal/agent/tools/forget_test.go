package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeForgetService records each ForgetMemory call so tests
// can assert on the request shape (incl. AllowedHubIDs scope-
// snapshot the tool wrapper passes through).
type fakeForgetService struct {
	mu         sync.Mutex
	requests   []tools.ForgetMemoryRequest
	err        error
	outcome    tools.ForgetOutcome
	hubID      string
	title      string
	autoStatus bool // when true, returns tools.ForgetStatusForgotten
}

func (f *fakeForgetService) ForgetMemory(_ context.Context, in tools.ForgetMemoryRequest) (tools.ForgetOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, in)
	if f.err != nil {
		return tools.ForgetOutcome{}, f.err
	}
	out := f.outcome
	if out.HubID == "" {
		out.HubID = f.hubID
	}
	if out.Title == "" {
		out.Title = f.title
	}
	if f.autoStatus && out.Status == "" {
		out.Status = tools.ForgetStatusForgotten
	}
	return out, nil
}

func makeForgetCall(t *testing.T, args tools.ForgetArgs) sdktool.Call {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return sdktool.Call{
		Use: sdkmodel.ToolUse{
			ID:    "call-forget-1",
			Name:  "forget_memory",
			Input: body,
		},
	}
}

func invokeForgetHandler(t *testing.T, tool sdktool.Tool, args tools.ForgetArgs) sdkmodel.ToolResult {
	t.Helper()
	def, ok := tool.(sdktool.Definition)
	if !ok {
		t.Fatalf("tool is not a Definition: %T", tool)
	}
	res, err := def.Handler(context.Background(), makeForgetCall(t, args))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

// Happy path: forget succeeds → wire result has status=forgotten.
func TestForgetTool_HappyPath(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{
		autoStatus: true,
		hubID:      hubA,
		title:      "Canceled launch notes",
	}
	tool, err := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	if err != nil {
		t.Fatalf("NewForgetTool: %v", err)
	}
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{
		MemoryID: "11111111-1111-1111-1111-111111111111",
		Reason:   "user asked to forget the canceled launch notes",
	})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.ForgetResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, res.Content)
	}
	if got.Status != tools.ForgetStatusForgotten {
		t.Errorf("Status = %q, want %q", got.Status, tools.ForgetStatusForgotten)
	}
	if got.HubID != hubA {
		t.Errorf("HubID = %q, want %q", got.HubID, hubA)
	}
	if got.MemoryID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("MemoryID = %q, want match", got.MemoryID)
	}

	// Service saw the right request shape.
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(svc.requests))
	}
	req := svc.requests[0]
	if req.OwnerID != testOwner {
		t.Errorf("OwnerID = %q, want %q", req.OwnerID, testOwner)
	}
	if req.MemoryID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("MemoryID = %q, want match", req.MemoryID)
	}
	if len(req.AllowedHubIDs) != 1 || req.AllowedHubIDs[0] != hubA {
		t.Errorf("AllowedHubIDs = %v, want [%q]", req.AllowedHubIDs, hubA)
	}
	if req.Reason == "" {
		t.Errorf("Reason empty; want passthrough")
	}
}

// Empty memory_id → validation error.
func TestForgetTool_EmptyMemoryID_Errors(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "  "})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "memory_id") {
		t.Errorf("err content = %q, want mention of memory_id", res.Content)
	}
}

// Service returns ErrForgetMemoryNotFound → tool surfaces a
// "not found in scope" error to the agent.
func TestForgetTool_NotFound_Surfaces(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{err: tools.ErrForgetMemoryNotFound}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "missing"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "not found") {
		t.Errorf("err content = %q, want mention of not found", res.Content)
	}
}

// Service returns ErrForgetMemoryNoPermission → tool surfaces
// a "no permission" error (distinct from "not found" so the
// agent can give a meaningful answer).
func TestForgetTool_NoPermission_Surfaces(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{err: tools.ErrForgetMemoryNoPermission}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "exists-but-readonly"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "permission") {
		t.Errorf("err content = %q, want mention of permission", res.Content)
	}
}

// Generic service error propagates with a "forget: ..."
// prefix so the agent can distinguish from validation errors.
func TestForgetTool_GenericServiceError_Propagates(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{err: errors.New("synthetic db failure")}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "any"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "synthetic db failure") {
		t.Errorf("err content = %q, want propagated error", res.Content)
	}
}

// AllowedHubIDs from the resolved scope are passed through.
// This is the SECURITY contract — the service receives the
// allowed-hub list and must scope its memory lookup to it.
func TestForgetTool_PassesScopeHubIDsToService(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{autoStatus: true, hubID: hubA, title: "x"}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA, hubB),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeHubSubset,
			HubIDs:  []string{hubA, hubB},
		},
	})
	if _, err := tool.(sdktool.Definition).Handler(context.Background(), makeForgetCall(t, tools.ForgetArgs{MemoryID: "any"})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	got := svc.requests[0].AllowedHubIDs
	wantSet := map[string]bool{hubA: true, hubB: true}
	if len(got) != 2 {
		t.Fatalf("AllowedHubIDs = %v, want 2 entries", got)
	}
	for _, h := range got {
		if !wantSet[h] {
			t.Errorf("AllowedHubIDs has unexpected %q", h)
		}
	}
}

// Constructor validates dependencies.
func TestForgetTool_NewForgetTool_Validation(t *testing.T) {
	t.Parallel()
	cases := map[string]tools.ForgetToolConfig{
		"nil service":     {Resolver: newResolver(hubA), Session: agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}}},
		"nil resolver":    {Service: &fakeForgetService{}, Session: agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}}},
		"invalid session": {Service: &fakeForgetService{}, Resolver: newResolver(hubA), Session: agent.SessionDescriptor{}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tools.NewForgetTool(cfg)
			if err == nil {
				t.Errorf("err = nil; want validation failure for %q", name)
			}
		})
	}
}

// Tool spec metadata.
func TestForgetTool_SpecShape(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{}
	tool, err := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	if err != nil {
		t.Fatalf("NewForgetTool: %v", err)
	}
	spec := tool.Spec()
	if spec.Name != "forget_memory" {
		t.Errorf("Name = %q, want forget_memory", spec.Name)
	}
	if spec.ReadOnly {
		t.Errorf("ReadOnly = true, want false (mutating tool)")
	}
}

// Empty status from service → tool falls back to forgotten
// (defensive default).
func TestForgetTool_EmptyStatusDefaultsToForgotten(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{outcome: tools.ForgetOutcome{HubID: hubA, Title: "x"}}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "any"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.ForgetResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.Status != tools.ForgetStatusForgotten {
		t.Errorf("Status = %q, want %q (default)", got.Status, tools.ForgetStatusForgotten)
	}
}

// Regression: the model-retries-after-success pattern. Two
// invocations of forget_memory with the SAME memory_id in the
// same turn should resolve to:
//
//   - call #1: status=forgotten, service hit exactly once
//   - call #2: status=already_forgotten, service NOT hit again
//
// The second-call short-circuit is what breaks the historical
// loop where call #2 hit ErrForgetMemoryNotFound, the model
// narrated it as a failure, and a third request_approval prompt
// landed on the user. With the tracker in place call #2 returns
// terminal success without re-entering the service.
func TestForgetTool_RetryWithinTurn_ReturnsAlreadyForgotten(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{
		autoStatus: true,
		hubID:      hubA,
		title:      "resume",
	}
	tracker := &tools.ForgottenTurnTracker{}
	tool, err := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
		Tracker: tracker,
	})
	if err != nil {
		t.Fatalf("NewForgetTool: %v", err)
	}
	const memID = "92b3c251-91e7-46da-9153-e1917b3fecf0"

	// Call #1 — real forget.
	res1 := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: memID})
	if res1.IsError {
		t.Fatalf("call #1 IsError = true: %s", res1.Content)
	}
	var got1 tools.ForgetResult
	if err := json.Unmarshal([]byte(res1.Content), &got1); err != nil {
		t.Fatalf("decode call #1: %v", err)
	}
	if got1.Status != tools.ForgetStatusForgotten {
		t.Errorf("call #1 Status = %q, want %q", got1.Status, tools.ForgetStatusForgotten)
	}

	// Call #2 — same id, model-retry simulated.
	res2 := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: memID})
	if res2.IsError {
		t.Fatalf("call #2 IsError = true: %s", res2.Content)
	}
	var got2 tools.ForgetResult
	if err := json.Unmarshal([]byte(res2.Content), &got2); err != nil {
		t.Fatalf("decode call #2: %v", err)
	}
	if got2.Status != tools.ForgetStatusAlreadyForgotten {
		t.Errorf("call #2 Status = %q, want %q", got2.Status, tools.ForgetStatusAlreadyForgotten)
	}
	if got2.MemoryID != memID {
		t.Errorf("call #2 MemoryID = %q, want %q", got2.MemoryID, memID)
	}

	// Service was hit exactly once — the short-circuit must not
	// re-enter ForgetMemory on the retry.
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if got, want := len(svc.requests), 1; got != want {
		t.Errorf("service requests = %d, want %d (second call must short-circuit before the service)", got, want)
	}
}

// Different memory_id after a successful forget must NOT
// short-circuit — the tracker is keyed by memory_id, not "any
// forget happened this turn". Critical security shape: an
// ID the user never approved should still go through the
// approval gate at the SDK layer and the scope check at the
// service layer.
func TestForgetTool_DifferentIDAfterSuccess_DoesNotShortCircuit(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{
		autoStatus: true,
		hubID:      hubA,
		title:      "x",
	}
	tracker := &tools.ForgottenTurnTracker{}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
		Tracker: tracker,
	})

	_ = invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "first-id"})
	_ = invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "second-id"})

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if got, want := len(svc.requests), 2; got != want {
		t.Fatalf("service requests = %d, want %d (different ids must each hit the service)", got, want)
	}
	if svc.requests[0].MemoryID == svc.requests[1].MemoryID {
		t.Error("both requests targeted the same memory_id — tracker must be id-keyed")
	}
}

// NotFound on the FIRST call (id never seen) keeps the existing
// surfaced error. The idempotency layer only kicks in for ids
// already forgotten earlier in this turn — a wrong id, or one
// belonging to another scope, must still surface NotFound so the
// agent (and the user) get an honest signal.
func TestForgetTool_NotFoundOnFirstCall_StillSurfaces(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{err: tools.ErrForgetMemoryNotFound}
	tracker := &tools.ForgottenTurnTracker{}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
		Tracker: tracker,
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "never-forgotten-here"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true (first-call NotFound must surface)")
	}
	if !strings.Contains(res.Content, "not found") {
		t.Errorf("err content = %q, want mention of not found", res.Content)
	}
}

// Nil tracker keeps the wrapper functional (legacy callers /
// tests that haven't been updated). Behavior reverts to "always
// call the service" with no idempotency optimization — but no
// crashes, no nil deref.
func TestForgetTool_NilTracker_StillWorks(t *testing.T) {
	t.Parallel()
	svc := &fakeForgetService{autoStatus: true, hubID: hubA, title: "x"}
	tool, _ := tools.NewForgetTool(tools.ForgetToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
		// Tracker omitted intentionally.
	})
	res := invokeForgetHandler(t, tool, tools.ForgetArgs{MemoryID: "id-1"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
}

// Quiet unused-import warning if model.MemoryKindSemantic is
// the only model symbol the file references; tests use the
// model package via SessionDescriptor indirectly.
var _ = model.MemoryKindSemantic
