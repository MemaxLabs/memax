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

// fakePushService records every CreateAndProcess call so tests
// can assert on the memory shape the tool wrote.
type fakePushService struct {
	mu       sync.Mutex
	memories []model.Memory
	requests []model.PushRequest
	err      error
	// status overrides the default "saved" outcome status —
	// tests covering saved_unprocessed pass that explicitly.
	status string
}

func (f *fakePushService) CreateAndProcess(_ context.Context, m *model.Memory, req model.PushRequest) (tools.PushOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return tools.PushOutcome{}, f.err
	}
	f.memories = append(f.memories, *m)
	f.requests = append(f.requests, req)
	status := f.status
	if status == "" {
		status = tools.PushStatusSaved
	}
	return tools.PushOutcome{Status: status}, nil
}

// stubMembership satisfies agent.MembershipReader for the
// scope resolver. Returns a constant hub list — every hub
// resolves to role="owner" so SessionScope.WritableHubIDs is
// the same as HubIDs (which is what most push/forget/propose
// tests want; tests targeting the viewer/non-writable branch
// use a custom membership reader inline).
type stubMembership struct {
	hubs []string
}

func (s stubMembership) HubMembershipsForUser(_ context.Context, _ string) ([]model.HubMembership, error) {
	out := make([]model.HubMembership, len(s.hubs))
	for i, h := range s.hubs {
		out[i] = model.HubMembership{HubID: h, Role: "owner"}
	}
	return out, nil
}

func newResolver(hubIDs ...string) *agent.ScopeResolver {
	return agent.NewScopeResolver(stubMembership{hubs: hubIDs})
}

// readOnlyMembership returns the same hubs as stubMembership
// but with role="viewer" so WritableHubIDs comes back empty.
// Tests targeting the IsHubWritable fast-fail use this fake.
type readOnlyMembership struct {
	hubs []string
}

func (s readOnlyMembership) HubMembershipsForUser(_ context.Context, _ string) ([]model.HubMembership, error) {
	out := make([]model.HubMembership, len(s.hubs))
	for i, h := range s.hubs {
		out[i] = model.HubMembership{HubID: h, Role: "viewer"}
	}
	return out, nil
}

func newReadOnlyResolver(hubIDs ...string) *agent.ScopeResolver {
	return agent.NewScopeResolver(readOnlyMembership{hubs: hubIDs})
}

const (
	testOwner = "11111111-1111-1111-1111-111111111111"
	hubA      = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	hubB      = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

// makeCall builds the SDK tool.Call shape the handler decodes.
func makeCall(t *testing.T, args tools.PushArgs) sdktool.Call {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return sdktool.Call{
		Use: sdkmodel.ToolUse{
			ID:    "call-1",
			Name:  "push_memory",
			Input: body,
		},
	}
}

// invokeHandler grabs the SDK Handler off the tool and calls
// it with the given args. Returns the ToolResult.
func invokeHandler(t *testing.T, tool sdktool.Tool, args tools.PushArgs) sdkmodel.ToolResult {
	t.Helper()
	def, ok := tool.(sdktool.Definition)
	if !ok {
		t.Fatalf("tool is not a Definition: %T", tool)
	}
	res, err := def.Handler(context.Background(), makeCall(t, args))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

// Happy path: single-hub session, no hub_id arg, push succeeds.
// Result is a JSON-encoded PushResult with status="saved".
func TestPushTool_HappyPath_SingleHub(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, err := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	if err != nil {
		t.Fatalf("NewPushTool: %v", err)
	}
	res := invokeHandler(t, tool, tools.PushArgs{
		Content: "remember this fact about the launch",
		Title:   "Launch fact",
		Tags:    []string{"launch", "ops"},
	})
	if res.IsError {
		t.Fatalf("IsError = true, content = %q", res.Content)
	}
	var got tools.PushResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode result: %v (raw=%s)", err, res.Content)
	}
	if got.Status != "saved" {
		t.Errorf("Status = %q, want saved", got.Status)
	}
	if got.HubID != hubA {
		t.Errorf("HubID = %q, want %q", got.HubID, hubA)
	}
	if got.MemoryID == "" {
		t.Errorf("MemoryID empty")
	}

	// Service saw the right memory.
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.memories) != 1 {
		t.Fatalf("service saw %d memories, want 1", len(svc.memories))
	}
	mem := svc.memories[0]
	if mem.OwnerID != testOwner {
		t.Errorf("memory.OwnerID = %q, want %q", mem.OwnerID, testOwner)
	}
	if mem.HubID != hubA {
		t.Errorf("memory.HubID = %q, want %q", mem.HubID, hubA)
	}
	if mem.Source != "chat_agent" {
		t.Errorf("memory.Source = %q, want chat_agent", mem.Source)
	}
	if mem.SourceAgent != "chat-agent" {
		t.Errorf("memory.SourceAgent = %q, want chat-agent", mem.SourceAgent)
	}
	if mem.ProvenanceCreatedByType != model.MemoryCreatedByAgent {
		t.Errorf("ProvenanceCreatedByType = %q, want %q", mem.ProvenanceCreatedByType, model.MemoryCreatedByAgent)
	}
	if mem.ProvenanceInitiationType != model.MemoryInitiationAgentProactive {
		t.Errorf("ProvenanceInitiationType = %q, want %q", mem.ProvenanceInitiationType, model.MemoryInitiationAgentProactive)
	}
	if mem.ContentHash == "" {
		t.Errorf("ContentHash empty")
	}
	if len(mem.Tags) != 2 {
		t.Errorf("Tags = %v, want 2 entries", mem.Tags)
	}
}

// Multi-hub session WITHOUT WriteHubID + no args.HubID → error.
// Pins the "no silent fallback" rule.
func TestPushTool_MultiHubNoDefault_RequiresHubID(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, err := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA, hubB),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeHubSubset,
			HubIDs:  []string{hubA, hubB},
		},
	})
	if err != nil {
		t.Fatalf("NewPushTool: %v", err)
	}
	res := invokeHandler(t, tool, tools.PushArgs{
		Content: "the multi-hub case",
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "hub_id is required") {
		t.Errorf("err content = %q, want mention of hub_id", res.Content)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.memories) != 0 {
		t.Errorf("service saw %d memories, want 0 (no write)", len(svc.memories))
	}
}

// Multi-hub session WITH WriteHubID → push uses the default.
func TestPushTool_MultiHubWithWriteHubID_UsesDefault(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA, hubB),
		Session: agent.SessionDescriptor{
			OwnerID:    testOwner,
			Type:       agent.ScopeTypeHubSubset,
			HubIDs:     []string{hubA, hubB},
			WriteHubID: hubB,
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{Content: "default-write hub"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.PushResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.HubID != hubB {
		t.Errorf("HubID = %q, want %q (WriteHubID)", got.HubID, hubB)
	}
}

// Explicit hub_id arg overrides the WriteHubID default.
func TestPushTool_ExplicitHubID_OverridesWriteDefault(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA, hubB),
		Session: agent.SessionDescriptor{
			OwnerID:    testOwner,
			Type:       agent.ScopeTypeHubSubset,
			HubIDs:     []string{hubA, hubB},
			WriteHubID: hubB,
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{Content: "explicit override", HubID: hubA})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.PushResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.HubID != hubA {
		t.Errorf("HubID = %q, want %q (explicit override)", got.HubID, hubA)
	}
}

// Explicit hub_id NOT in scope → error, no write.
func TestPushTool_ExplicitHubID_OutOfScope_Errors(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{
		Content: "out-of-scope attempt",
		HubID:   hubB, // not in this session's scope
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "not in this session") {
		t.Errorf("err content = %q, want scope-rejection message", res.Content)
	}
}

// Phase 3.7 step 1: a hub the user is a VIEWER on is in
// scope.HubIDs (read-eligible) but NOT in scope.WritableHubIDs.
// pickPushHub must reject with a clear "read-only" message
// distinct from the out-of-scope rejection above so the agent
// can phrase its response correctly.
func TestPushTool_ExplicitHubID_ReadOnlyHub_Errors(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newReadOnlyResolver(hubA), // user is viewer on hubA
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{
		Content: "viewer cannot push",
		HubID:   hubA,
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "read-only") {
		t.Errorf("err content = %q, want read-only message", res.Content)
	}
	// Service must NOT have been called — fast-fail at the
	// wrapper before any DB write.
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 0 {
		t.Errorf("service called %d times, want 0", len(svc.requests))
	}
}

// Single-hub fallback (hub_id omitted, scope.HubIDs has 1 entry)
// also requires the hub to be writable. If the user is a viewer
// on the only hub in scope, push fails with errPushNoHub —
// they need to either explicitly provide hub_id (which the
// "read-only" branch above also rejects) or use a different
// session.
func TestPushTool_SingleHubViewer_NoFallback(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newReadOnlyResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{
		Content: "no fallback for viewer-only single hub",
		// HubID omitted → fall through to single-hub default.
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// Empty content → validation error.
func TestPushTool_EmptyContent_Errors(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{Content: "   "})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "content") {
		t.Errorf("err content = %q, want mention of content", res.Content)
	}
}

// Service error propagates as a tool error result.
func TestPushTool_ServiceError_Propagates(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{err: errors.New("synthetic db failure")}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{Content: "anything"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(res.Content, "synthetic db failure") {
		t.Errorf("err content = %q, want propagated db error", res.Content)
	}
}

// Constructor validates dependencies.
func TestPushTool_NewPushTool_Validation(t *testing.T) {
	t.Parallel()
	cases := map[string]tools.PushToolConfig{
		"nil service": {
			Resolver: newResolver(hubA),
			Session:  agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}},
		},
		"nil resolver": {
			Service: &fakePushService{},
			Session: agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}},
		},
		"invalid session": {
			Service:  &fakePushService{},
			Resolver: newResolver(hubA),
			Session:  agent.SessionDescriptor{}, // missing OwnerID + Type
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tools.NewPushTool(cfg)
			if err == nil {
				t.Errorf("err = nil; want validation failure for %q", name)
			}
		})
	}
}

// Phase 3.6b3d v2: PushOutcome.Status flows through to the
// PushResult on the wire. saved_unprocessed surfaces a
// background-enqueue failure to the agent so it can tell the
// user "memory saved, but search indexing is delayed."
func TestPushTool_SavedUnprocessed_Surfaces(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{status: tools.PushStatusSavedUnprocessed}
	tool, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool, tools.PushArgs{Content: "delayed"})
	if res.IsError {
		t.Fatalf("IsError = true, content = %q", res.Content)
	}
	var got tools.PushResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.Status != tools.PushStatusSavedUnprocessed {
		t.Errorf("Status = %q, want %q", got.Status, tools.PushStatusSavedUnprocessed)
	}
}

// Defensive default: a service that returns an empty status
// (forgets to set the field) gets saved_unprocessed rather
// than implicitly claiming saved+processed.
func TestPushTool_EmptyStatusDefaultsToUnprocessed(t *testing.T) {
	t.Parallel()
	// Use a service that returns an explicit empty
	// PushOutcome{} so we exercise the tool's defensive
	// default. (fakePushService translates empty to
	// PushStatusSaved, which is the wrong fixture for this
	// test.)
	emptyStatusSvc := pushOutcomeReturning{outcome: tools.PushOutcome{}}
	tool2, _ := tools.NewPushTool(tools.PushToolConfig{
		Service:  emptyStatusSvc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeHandler(t, tool2, tools.PushArgs{Content: "anything"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.PushResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.Status != tools.PushStatusSavedUnprocessed {
		t.Errorf("Status = %q, want %q (tool's defensive default)", got.Status, tools.PushStatusSavedUnprocessed)
	}
}

// pushOutcomeReturning is a minimal PushService that returns
// a fixed outcome.
type pushOutcomeReturning struct {
	outcome tools.PushOutcome
}

func (p pushOutcomeReturning) CreateAndProcess(_ context.Context, _ *model.Memory, _ model.PushRequest) (tools.PushOutcome, error) {
	return p.outcome, nil
}

// Tool spec metadata.
func TestPushTool_SpecShape(t *testing.T) {
	t.Parallel()
	svc := &fakePushService{}
	tool, err := tools.NewPushTool(tools.PushToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	if err != nil {
		t.Fatalf("NewPushTool: %v", err)
	}
	spec := tool.Spec()
	if spec.Name != "push_memory" {
		t.Errorf("Name = %q, want push_memory", spec.Name)
	}
	if spec.ReadOnly {
		t.Errorf("ReadOnly = true, want false (mutating tool)")
	}
}
