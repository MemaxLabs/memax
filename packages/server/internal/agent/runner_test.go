package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeModel emits a pre-scripted sequence of provider stream events
// per call to Stream. The runtime test uses this to drive Run
// through each terminal path without a real LLM.
//
// The shape mirrors the SDK's internal fakeModel
// (ref: .refs/memax-go-agent-sdk/agent_test.go); we duplicate it
// here so the test stays under our control as the SDK evolves.
type fakeModel struct {
	turns [][]sdkmodel.StreamEvent
	calls int
}

func (m *fakeModel) Stream(_ context.Context, _ sdkmodel.Request) (sdkmodel.Stream, error) {
	if m.calls >= len(m.turns) {
		return &fakeStream{}, nil
	}
	s := &fakeStream{events: m.turns[m.calls]}
	m.calls++
	return s, nil
}

type fakeStream struct {
	events []sdkmodel.StreamEvent
	index  int
}

func (s *fakeStream) Recv() (sdkmodel.StreamEvent, error) {
	if s.index >= len(s.events) {
		return sdkmodel.StreamEvent{}, sdkmodel.ErrEndOfStream
	}
	ev := s.events[s.index]
	s.index++
	return ev, nil
}

func (s *fakeStream) Close() error { return nil }

// echoTool is a minimal Tool implementation used by tests. It
// records the args it received for assertions. ConcurrencySafe so
// the SDK can dispatch parallel calls in one turn.
type echoTool struct {
	name      string
	failOnce  bool
	failed    bool
	lastInput string
}

func (t *echoTool) Spec() sdkmodel.ToolSpec {
	return sdkmodel.ToolSpec{
		Name:            t.name,
		Description:     "echo back input.value",
		ConcurrencySafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
			"required": []any{"value"},
		},
	}
}

func (t *echoTool) Execute(_ context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	if t.failOnce && !t.failed {
		t.failed = true
		return sdkmodel.ToolResult{
			ToolUseID: call.Use.ID,
			Name:      call.Use.Name,
			Content:   "transient failure",
			IsError:   true,
		}, nil
	}
	t.lastInput = string(call.Use.Input)
	args, _ := sdktool.DecodeInput[struct {
		Value string `json:"value"`
	}](call.Use)
	return sdkmodel.ToolResult{
		ToolUseID: call.Use.ID,
		Name:      call.Use.Name,
		Content:   "echo:" + args.Value,
	}, nil
}

func (t *echoTool) CanRunConcurrently(_ sdkmodel.ToolUse) bool { return true }

// stubMembership returns a fixed hub list for ScopeResolver.
// Every hub resolves to role="owner" so the runner tests
// (which exercise the agent loop, not membership semantics)
// don't need to think about WritableHubIDs.
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

// validSession constructs a SessionDescriptor that passes Validate.
func validSession() SessionDescriptor {
	return SessionDescriptor{
		OwnerID: "user-1",
		Type:    ScopeTypeSingleHub,
		HubIDs:  []string{"hub-a"},
	}
}

func TestRun_RejectsEmptyPrompt(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), RunInput{
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   &fakeModel{},
		Session: validSession(),
	})
	if !errors.Is(err, ErrEmptyPrompt) {
		t.Fatalf("expected ErrEmptyPrompt, got %v", err)
	}
}

func TestRun_RejectsInvalidProfile(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: Profile("bogus"),
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   &fakeModel{},
		Session: validSession(),
	})
	if !errors.Is(err, ErrInvalidPrf) {
		t.Fatalf("expected ErrInvalidPrf, got %v", err)
	}
}

func TestRun_RejectsTestProfileEscapingProd(t *testing.T) {
	t.Parallel()
	// profileTest is unexported, but a misuse via Profile("test")
	// should still be rejected — Valid() must say no.
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: profileTest,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   &fakeModel{},
		Session: validSession(),
	})
	if !errors.Is(err, ErrInvalidPrf) {
		t.Fatalf("expected ErrInvalidPrf for profileTest, got %v", err)
	}
}

func TestRun_RejectsMissingModel(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Session: validSession(),
	})
	if !errors.Is(err, ErrMissingModel) {
		t.Fatalf("expected ErrMissingModel, got %v", err)
	}
}

func TestRun_RejectsEmptyToolList(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: ProfileChat,
		Model:   &fakeModel{},
		Session: validSession(),
	})
	if !errors.Is(err, ErrEmptyToolList) {
		t.Fatalf("expected ErrEmptyToolList, got %v", err)
	}
}

func TestRun_RejectsInvalidScope(t *testing.T) {
	t.Parallel()
	bad := SessionDescriptor{Type: ScopeTypeSingleHub} // missing OwnerID + HubIDs
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   &fakeModel{},
		Session: bad,
	})
	if !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestRun_NaturalSuccess_FinalText(t *testing.T) {
	t.Parallel()
	// One turn: the model emits assistant text and ends the stream.
	// SDK derives EventResult from that. Usage event also lands.
	model := &fakeModel{turns: [][]sdkmodel.StreamEvent{
		{
			{Kind: sdkmodel.StreamText, Text: "hello there"},
			{Kind: sdkmodel.StreamUsage, Usage: &sdkmodel.Usage{
				Provider:     "fake",
				Model:        "fake-1",
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			}},
		},
	}}
	res, err := Run(context.Background(), RunInput{
		Prompt:  "say hi",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   model,
		Session: validSession(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != RunStatusSuccess {
		t.Errorf("Status = %q, want %q (err=%v)", res.Status, RunStatusSuccess, res.Err)
	}
	if res.Final != "hello there" {
		t.Errorf("Final = %q, want %q", res.Final, "hello there")
	}
	if res.Usage.TotalTokens != 30 {
		t.Errorf("Usage.TotalTokens = %d, want 30", res.Usage.TotalTokens)
	}
	if len(res.ToolCalls) != 0 {
		t.Errorf("ToolCalls = %d, want 0", len(res.ToolCalls))
	}
	if res.SessionID == "" {
		t.Errorf("SessionID should be set after a successful run")
	}
	if res.ModelCalls != 1 {
		t.Errorf("ModelCalls = %d, want 1 (single-turn run)", res.ModelCalls)
	}
}

// TestRun_ModelCallsCountsPerTurn verifies that a multi-turn run
// (model emits a tool_use, SDK executes the tool, model takes
// another turn to read the result and emit final text) reports
// ModelCalls = 2 — one per orchestration-loop iteration. This is
// the load-bearing accuracy for the cycle-level governor in
// internal/dreams (plan 24); without it, every multi-turn agent
// run would underreport as a single LLM call.
func TestRun_ModelCallsCountsPerTurn(t *testing.T) {
	t.Parallel()
	toolUseInput, _ := json.Marshal(map[string]any{"value": "x"})
	model := &fakeModel{turns: [][]sdkmodel.StreamEvent{
		{
			{Kind: sdkmodel.StreamToolUse, ToolUse: sdkmodel.ToolUse{
				ID: "toolu_1", Name: "echo", Input: toolUseInput,
			}},
		},
		{
			{Kind: sdkmodel.StreamText, Text: "done"},
		},
	}}
	res, err := Run(context.Background(), RunInput{
		Prompt:  "use the tool",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   model,
		Session: validSession(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != RunStatusSuccess {
		t.Fatalf("Status = %q, want success (err=%v)", res.Status, res.Err)
	}
	if res.ModelCalls != 2 {
		t.Errorf("ModelCalls = %d, want 2 (one per turn)", res.ModelCalls)
	}
}

func TestRun_ToolUseAndResult_Recorded(t *testing.T) {
	t.Parallel()
	// Turn 1: model emits a tool_use → the SDK runs the tool → next
	// turn the model gets the tool_result and emits final text.
	toolUseInput, _ := json.Marshal(map[string]any{"value": "world"})
	model := &fakeModel{turns: [][]sdkmodel.StreamEvent{
		{
			{
				Kind: sdkmodel.StreamToolUse,
				ToolUse: sdkmodel.ToolUse{
					ID:    "toolu_1",
					Name:  "echo",
					Input: toolUseInput,
				},
			},
		},
		{
			{Kind: sdkmodel.StreamText, Text: "done"},
		},
	}}

	res, err := Run(context.Background(), RunInput{
		Prompt:  "use the tool",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   model,
		Session: validSession(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != RunStatusSuccess {
		t.Fatalf("Status = %q, want %q (err=%v)", res.Status, RunStatusSuccess, res.Err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(res.ToolCalls))
	}
	got := res.ToolCalls[0]
	if got.Name != "echo" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", got.Name, "echo")
	}
	if got.IsError {
		t.Errorf("ToolCalls[0].IsError = true, want false")
	}
	if !strings.Contains(got.Result, "echo:world") {
		t.Errorf("ToolCalls[0].Result = %q, want contains %q", got.Result, "echo:world")
	}
	if !strings.Contains(got.ArgsJSON, `"value":"world"`) {
		t.Errorf("ToolCalls[0].ArgsJSON = %q, want contains value=world", got.ArgsJSON)
	}
}

func TestRun_ToolError_Recorded(t *testing.T) {
	t.Parallel()
	// Tool fails on first invocation; model gives up after.
	toolUseInput, _ := json.Marshal(map[string]any{"value": "x"})
	model := &fakeModel{turns: [][]sdkmodel.StreamEvent{
		{
			{
				Kind: sdkmodel.StreamToolUse,
				ToolUse: sdkmodel.ToolUse{
					ID:    "toolu_1",
					Name:  "echo",
					Input: toolUseInput,
				},
			},
		},
		{
			{Kind: sdkmodel.StreamText, Text: "I gave up"},
		},
	}}
	tool := &echoTool{name: "echo", failOnce: true}
	res, err := Run(context.Background(), RunInput{
		Prompt:  "use the tool",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{tool},
		Model:   model,
		Session: validSession(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != RunStatusSuccess {
		t.Fatalf("Status = %q, want %q (err=%v)", res.Status, RunStatusSuccess, res.Err)
	}
	if len(res.ToolCalls) != 1 || !res.ToolCalls[0].IsError {
		t.Fatalf("expected one IsError tool call, got %+v", res.ToolCalls)
	}
}

func TestRun_DuplicateToolName_FailsAtRegister(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: ProfileChat,
		Tools: []sdktool.Tool{
			&echoTool{name: "echo"},
			&echoTool{name: "echo"},
		},
		Model:   &fakeModel{},
		Session: validSession(),
	})
	if err == nil || !strings.Contains(err.Error(), "register tool") {
		t.Fatalf("expected register-tool error, got %v", err)
	}
}

func TestRun_NilToolInList_Rejected(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{nil},
		Model:   &fakeModel{},
		Session: validSession(),
	})
	if err == nil || !strings.Contains(err.Error(), "is nil") {
		t.Fatalf("expected nil-tool error, got %v", err)
	}
}

func TestRun_StreamErrorBeforeTerminal(t *testing.T) {
	t.Parallel()
	// errModel returns an error from Stream — Query reports an
	// EventError; runner classifies as RunStatusError.
	res, err := Run(context.Background(), RunInput{
		Prompt:  "hi",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{&echoTool{name: "echo"}},
		Model:   &errStreamModel{err: errors.New("provider blew up")},
		Session: validSession(),
	})
	if err != nil {
		t.Fatalf("unexpected pre-flight error: %v", err)
	}
	if res.Status != RunStatusError {
		t.Errorf("Status = %q, want %q", res.Status, RunStatusError)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "provider blew up") {
		t.Errorf("Err = %v, want wrapping provider error", res.Err)
	}
}

type errStreamModel struct {
	err error
}

func (m *errStreamModel) Stream(_ context.Context, _ sdkmodel.Request) (sdkmodel.Stream, error) {
	return nil, m.err
}

func TestClassifyTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want RunStatus
	}{
		{nil, RunStatusSuccess},
		{errors.New("budget exceeded: max model calls (8 > 7)"), RunStatusBudgetExceeded},
		{errors.New("max turns reached"), RunStatusMaxTurns},
		{errors.New("provider 500"), RunStatusError},
	}
	for _, tc := range cases {
		got := classifyTerminal(tc.err)
		if got != tc.want {
			t.Errorf("classifyTerminal(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestEnvelopeFor_KnownProfiles(t *testing.T) {
	t.Parallel()
	for _, p := range []Profile{ProfileDreamPassive, ProfileDreamActive, ProfileChat, profileTest} {
		env := envelopeFor(p)
		if env.maxTurns <= 0 || env.maxModelCalls <= 0 || env.maxToolCalls <= 0 {
			t.Errorf("profile %q has zero envelope: %+v", p, env)
		}
	}
}

func TestPolicyFor_TurnsBelowEnvelopeMaxTurns(t *testing.T) {
	t.Parallel()
	// Documented invariant: policy.MaxTurns < envelope.maxTurns so
	// budget exhaust beats the hard-loop bound on terminal stop reason.
	for _, p := range []Profile{ProfileDreamPassive, ProfileDreamActive, ProfileChat} {
		env := envelopeFor(p)
		policy := policyFor(p)
		if policy.MaxTurns >= env.maxTurns {
			t.Errorf("profile %q: policy.MaxTurns=%d should be < envelope.maxTurns=%d", p, policy.MaxTurns, env.maxTurns)
		}
		if policy.MaxModelCalls != env.maxModelCalls {
			t.Errorf("profile %q: policy.MaxModelCalls=%d != envelope.maxModelCalls=%d", p, policy.MaxModelCalls, env.maxModelCalls)
		}
	}
}

// TestPolicyFor_DurationBelowEnvelopeMaxDuration enforces the
// budget-wins-on-duration invariant. Without policy.MaxDuration <
// envelope.maxDuration, the SDK's run-context wrap fires
// context.DeadlineExceeded before the budget check evaluates
// MaxDuration (because budget.Policy.Check returns ctx.Err() first).
// This test guards the pre-mature regression where the gap shrinks
// or disappears.
func TestPolicyFor_DurationBelowEnvelopeMaxDuration(t *testing.T) {
	t.Parallel()
	for _, p := range []Profile{ProfileDreamPassive, ProfileDreamActive, ProfileChat} {
		env := envelopeFor(p)
		policy := policyFor(p)
		if policy.MaxDuration >= env.maxDuration {
			t.Errorf("profile %q: policy.MaxDuration=%v should be < envelope.maxDuration=%v", p, policy.MaxDuration, env.maxDuration)
		}
		if policy.MaxDuration <= 0 {
			t.Errorf("profile %q: policy.MaxDuration=%v must be positive (otherwise governor disables duration check)", p, policy.MaxDuration)
		}
	}
}
