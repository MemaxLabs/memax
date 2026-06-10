package dreams

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestParseContradictVerdict_Contradiction(t *testing.T) {
	t.Parallel()
	got, reason := parseContradictVerdict(`I reviewed both memories carefully.
Memory A says coffee, Memory B says tea.

VERDICT: CONTRADICTION — A and B specify different favorite drinks for the same person.`)
	if !got {
		t.Errorf("expected contradicts=true")
	}
	if !strings.Contains(reason, "different favorite drinks") {
		t.Errorf("reason = %q, want segment about different drinks", reason)
	}
}

func TestParseContradictVerdict_NoContradiction(t *testing.T) {
	t.Parallel()
	got, reason := parseContradictVerdict(`Both memories complement each other.

VERDICT: NO_CONTRADICTION — A is the morning preference, B is the afternoon preference.`)
	if got {
		t.Errorf("expected contradicts=false")
	}
	if !strings.Contains(reason, "morning") {
		t.Errorf("reason = %q, want morning segment", reason)
	}
}

// Tolerance for "NO CONTRADICTION" with a space (model
// substitution). The contract specifies "NO_CONTRADICTION" but
// the parser accepts both for robustness.
func TestParseContradictVerdict_AcceptsSpaceVariant(t *testing.T) {
	t.Parallel()
	got, _ := parseContradictVerdict(`VERDICT: NO CONTRADICTION — fine.`)
	if got {
		t.Errorf("space variant should still parse as no_contradiction")
	}
}

// Verdict on a line that isn't the LAST line — parser treats the
// last non-empty line as the verdict candidate. If that line
// isn't a verdict, the whole reply is unparsed (returns false).
// This is intentional: the system prompt promises "verdict line
// is the LAST line" so trailing prose AFTER it should be a
// programmer-noticeable bug, not silently parse-good.
func TestParseContradictVerdict_RejectsTrailingProse(t *testing.T) {
	t.Parallel()
	got, _ := parseContradictVerdict(`VERDICT: CONTRADICTION — fine.

Sometimes the model adds a postscript here, against the contract.`)
	if got {
		t.Errorf("verdict NOT on the last line should NOT parse as contradiction (model violated the contract)")
	}
}

func TestParseContradictVerdict_NoVerdictReturnsFalse(t *testing.T) {
	t.Parallel()
	got, reason := parseContradictVerdict("I'm not sure either way; can you give me more context?")
	if got || reason != "" {
		t.Errorf("missing verdict should return (false, empty), got (%v, %q)", got, reason)
	}
}

func TestParseContradictVerdict_EmptyReturnsFalse(t *testing.T) {
	t.Parallel()
	got, reason := parseContradictVerdict("")
	if got || reason != "" {
		t.Errorf("empty input should return (false, empty)")
	}
}

func TestParseContradictVerdict_DashVariants(t *testing.T) {
	t.Parallel()
	for _, sep := range []string{"—", "-", ":", "–"} {
		body := "VERDICT: CONTRADICTION " + sep + " reason text"
		if sep == "-" {
			body = "VERDICT: CONTRADICTION - reason text"
		}
		got, reason := parseContradictVerdict(body)
		if !got {
			t.Errorf("sep=%q failed to parse contradicts=true (body=%q)", sep, body)
		}
		if !strings.Contains(reason, "reason text") {
			t.Errorf("sep=%q reason = %q, want contains 'reason text'", sep, reason)
		}
	}
}

func TestShouldRouteContradictionAgent_GatesAreAllRequired(t *testing.T) {
	t.Parallel()
	pair := &model.SimilarMemoryPair{
		MemoryA: model.Memory{ID: "a"},
		MemoryB: model.Memory{ID: "b"},
	}
	firedDecisions := map[string]triggers.Decision{
		"a": {Fired: true, Kind: triggers.KindContradiction},
	}

	// Engine without a runtime → never routes, even with flag and fire.
	e := &Engine{}
	if e.shouldRouteContradictionAgent(map[string]any{"dreams_use_agent_runtime": true}, firedDecisions, pair) {
		t.Errorf("nil agent runtime should short-circuit to single-call")
	}

	// Engine with runtime but flag off.
	e = &Engine{agentRuntime: &AgentRuntime{}}
	// The runtime needs IsConfigured() — set required fields via
	// a nil-equivalent that still passes the check. We can't
	// build a real one without network, so just verify the
	// flag-off branch short-circuits FIRST.
	if e.shouldRouteContradictionAgent(map[string]any{"dreams_use_agent_runtime": false}, firedDecisions, pair) {
		t.Errorf("flag off should short-circuit to single-call")
	}
}

// fakeAgentMembership satisfies agent.MembershipReader so we
// can build a real *agent.ScopeResolver in tests without
// hitting the store. The hub list is whatever the test
// fixture passes — the scope resolver intersects against this
// list per-call. Every hub resolves to role="owner" so dreams
// tests (which only care about hub membership for scope
// resolution) don't need to think about WritableHubIDs.
type fakeAgentMembership struct{ hubs []string }

func (f fakeAgentMembership) HubMembershipsForUser(_ context.Context, _ string) ([]model.HubMembership, error) {
	out := make([]model.HubMembership, len(f.hubs))
	for i, h := range f.hubs {
		out[i] = model.HubMembership{HubID: h, Role: "owner"}
	}
	return out, nil
}

// fakeRecallService is a no-op tools.RecallService — the gate
// tests never actually invoke recall, but the AgentRuntime
// requires a non-nil service to satisfy IsConfigured.
type fakeRecallService struct{}

func (fakeRecallService) AgentRecall(_ context.Context, _ tools.AgentRecallRequest) (tools.AgentRecallResponse, error) {
	return tools.AgentRecallResponse{}, nil
}

// fakeAgentModelClient satisfies sdkmodel.Client. Returns
// ErrEndOfStream so any accidental Run call terminates without
// network. Tests that actually want to drive a Run loop should
// build their own model with scripted turns.
type fakeAgentModelClient struct{}

func (fakeAgentModelClient) Stream(_ context.Context, _ sdkmodel.Request) (sdkmodel.Stream, error) {
	return nil, sdkmodel.ErrEndOfStream
}

// configuredFakeRuntime returns an AgentRuntime whose
// IsConfigured() is true. Used for routing-gate tests that need
// to clear the runtime gate so the trigger-kind gate is the
// thing actually under test.
func configuredFakeRuntime() *AgentRuntime {
	return &AgentRuntime{
		Model:         fakeAgentModelClient{},
		RecallService: fakeRecallService{},
		Resolver:      agent.NewScopeResolver(fakeAgentMembership{hubs: []string{"hub-1"}}),
	}
}

// TestShouldRouteContradictionAgent_PositiveRoutesWhenAllGatesHold
// pins the positive route path with a configured runtime + flag
// on + contradiction trigger fired. Without this test, the routing
// implementation could regress (e.g. the kind filter could
// silently drop KindUserFollowup) and the suite would still pass
// because every other test only exercises the negative cases.
func TestShouldRouteContradictionAgent_PositiveRoutesWhenAllGatesHold(t *testing.T) {
	t.Parallel()
	pair := &model.SimilarMemoryPair{
		MemoryA: model.Memory{ID: "a"},
		MemoryB: model.Memory{ID: "b"},
	}
	settings := map[string]any{"dreams_use_agent_runtime": true}

	for _, kind := range []triggers.Kind{triggers.KindContradiction, triggers.KindUserFollowup} {
		t.Run(string(kind), func(t *testing.T) {
			decisions := map[string]triggers.Decision{
				"a": {Fired: true, Kind: kind},
			}
			e := &Engine{agentRuntime: configuredFakeRuntime()}
			if !e.shouldRouteContradictionAgent(settings, decisions, pair) {
				t.Errorf("kind=%s on memory A should route to agent (all three gates hold)", kind)
			}
		})
	}

	// Either side firing routes the pair — the contract says
	// "EITHER memory of the pair", not "both".
	t.Run("fire on B alone routes", func(t *testing.T) {
		decisions := map[string]triggers.Decision{
			"b": {Fired: true, Kind: triggers.KindContradiction},
		}
		e := &Engine{agentRuntime: configuredFakeRuntime()}
		if !e.shouldRouteContradictionAgent(settings, decisions, pair) {
			t.Errorf("kind=contradiction on memory B alone should still route (either side fires)")
		}
	})
}

// Counter to TestShouldRouteContradictionAgent_PositiveRoutes:
// verify that with a CONFIGURED runtime + flag on, but no fired
// trigger, routing still short-circuits to single-call. This pins
// the third gate (trigger fire) independently of the first two.
func TestShouldRouteContradictionAgent_NoFireKeepsSingleCall(t *testing.T) {
	t.Parallel()
	pair := &model.SimilarMemoryPair{
		MemoryA: model.Memory{ID: "a"},
		MemoryB: model.Memory{ID: "b"},
	}
	settings := map[string]any{"dreams_use_agent_runtime": true}
	decisions := map[string]triggers.Decision{
		"a": {Fired: false, Kind: triggers.KindNone},
		"b": {Fired: false, Kind: triggers.KindNone},
	}
	e := &Engine{agentRuntime: configuredFakeRuntime()}
	if e.shouldRouteContradictionAgent(settings, decisions, pair) {
		t.Errorf("no fire on either memory should NOT route to agent even when runtime+flag are on")
	}
}

func TestShouldRouteContradictionAgent_TopicOverloadDoesNotRouteContradict(t *testing.T) {
	t.Parallel()
	// A pair where the only fired trigger is topic-overload —
	// that's not a contradiction-relevant signal, so the
	// contradict phase stays on single-call. The organize phase
	// (separately) will pick up topic-overload routing.
	pair := &model.SimilarMemoryPair{
		MemoryA: model.Memory{ID: "a"},
		MemoryB: model.Memory{ID: "b"},
	}
	decisions := map[string]triggers.Decision{
		"a": {Fired: true, Kind: triggers.KindTopicOverload},
		"b": {Fired: true, Kind: triggers.KindURLDrift},
	}
	// Use a CONFIGURED runtime so the kind-filter is the actual
	// gate under test (without it, runtime-nil short-circuits
	// before the kind filter is consulted).
	e := &Engine{agentRuntime: configuredFakeRuntime()}
	if e.shouldRouteContradictionAgent(map[string]any{"dreams_use_agent_runtime": true}, decisions, pair) {
		t.Errorf("topic-overload + url-drift fires should NOT route contradict to agent (kind filter)")
	}
}

// scriptedAgentModelClient lets a test drive `agent.Run` to a
// chosen terminal status. Returns a stream that emits the scripted
// events then ErrEndOfStream — same pattern the SDK's own internal
// fakes use.
type scriptedAgentModelClient struct {
	turns [][]sdkmodel.StreamEvent
	calls int
}

func (m *scriptedAgentModelClient) Stream(_ context.Context, _ sdkmodel.Request) (sdkmodel.Stream, error) {
	if m.calls >= len(m.turns) {
		return &scriptedAgentStream{}, nil
	}
	s := &scriptedAgentStream{events: m.turns[m.calls]}
	m.calls++
	return s, nil
}

type scriptedAgentStream struct {
	events []sdkmodel.StreamEvent
	index  int
}

func (s *scriptedAgentStream) Recv() (sdkmodel.StreamEvent, error) {
	if s.index >= len(s.events) {
		return sdkmodel.StreamEvent{}, sdkmodel.ErrEndOfStream
	}
	ev := s.events[s.index]
	s.index++
	return ev, nil
}

func (s *scriptedAgentStream) Close() error { return nil }

// TestAgenticDetectContradiction_SuccessProjectsRunResult pins
// the success path: the result carries SessionID + ModelCalls +
// ToolCalls + Usage from agent.Run AND parses the verdict. Missing
// before would mean a regression that drops audit fields couldn't
// be caught without a real provider.
func TestAgenticDetectContradiction_SuccessProjectsRunResult(t *testing.T) {
	t.Parallel()
	mc := &scriptedAgentModelClient{turns: [][]sdkmodel.StreamEvent{
		{
			{Kind: sdkmodel.StreamText, Text: "Reviewed both memories.\n\nVERDICT: CONTRADICTION — A says coffee, B says tea."},
			{Kind: sdkmodel.StreamUsage, Usage: &sdkmodel.Usage{
				InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
			}},
		},
	}}
	rt := &AgentRuntime{
		Model:         mc,
		RecallService: fakeRecallService{},
		Resolver:      agent.NewScopeResolver(fakeAgentMembership{hubs: []string{"hub-1"}}),
	}
	e := &Engine{agentRuntime: rt}
	hub := &model.Hub{ID: "hub-1", OwnerID: "owner-1"}
	pair := &model.SimilarMemoryPair{
		MemoryA:    model.Memory{ID: "mem-a", HubID: "hub-1", Title: "Coffee", Content: "I drink coffee."},
		MemoryB:    model.Memory{ID: "mem-b", HubID: "hub-1", Title: "Tea", Content: "I drink tea."},
		Similarity: 0.78,
	}
	decisions := map[string]triggers.Decision{
		"mem-a": {Fired: true, Kind: triggers.KindContradiction},
	}

	res, err := e.agenticDetectContradiction(context.Background(), hub, pair, decisions, "actor-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if !res.Contradicts {
		t.Errorf("expected Contradicts=true, got false")
	}
	if !strings.Contains(res.Reason, "coffee") {
		t.Errorf("Reason = %q, want contains 'coffee'", res.Reason)
	}
	if res.ModelCalls != 1 {
		t.Errorf("ModelCalls = %d, want 1 (single-turn scripted run)", res.ModelCalls)
	}
	if res.SessionID == "" {
		t.Errorf("SessionID should be populated")
	}
	if res.Usage.TotalTokens != 150 {
		t.Errorf("Usage.TotalTokens = %d, want 150", res.Usage.TotalTokens)
	}
}

// erroringAgentModelClient returns a Stream error on every call,
// which makes agent.Run terminate as RunStatusError (the SDK
// surfaces the Stream error as EventError). Used to drive the
// non-success-still-accounts test below without a real timeout.
type erroringAgentModelClient struct {
	err error
}

func (m *erroringAgentModelClient) Stream(_ context.Context, _ sdkmodel.Request) (sdkmodel.Stream, error) {
	return nil, m.err
}

// TestAgenticDetectContradiction_NonSuccessStillProjectsRunResult
// is the regression test for the codex-flagged Medium: when
// agent.Run terminates with a non-success status, the error
// returned to the caller is non-nil BUT the result still carries
// the populated SessionID + run state so the budget governor and
// phase metrics can account for the work that actually happened.
//
// Without this, a budget-exhausted or provider-errored agent run
// would consume real model calls without counting them against
// the cycle's hard cap — defeating the governor on the exact
// failure mode it was designed for.
func TestAgenticDetectContradiction_NonSuccessStillProjectsRunResult(t *testing.T) {
	t.Parallel()
	rt := &AgentRuntime{
		Model:         &erroringAgentModelClient{err: errors.New("simulated provider 503")},
		RecallService: fakeRecallService{},
		Resolver:      agent.NewScopeResolver(fakeAgentMembership{hubs: []string{"hub-1"}}),
	}
	e := &Engine{agentRuntime: rt}
	hub := &model.Hub{ID: "hub-1", OwnerID: "owner-1"}
	pair := &model.SimilarMemoryPair{
		MemoryA: model.Memory{ID: "mem-a", HubID: "hub-1"},
		MemoryB: model.Memory{ID: "mem-b", HubID: "hub-1"},
	}
	decisions := map[string]triggers.Decision{
		"mem-a": {Fired: true, Kind: triggers.KindContradiction},
	}

	res, err := e.agenticDetectContradiction(context.Background(), hub, pair, decisions, "actor-1")
	if err == nil {
		t.Fatal("expected non-success terminal to produce an error")
	}
	if res == nil {
		t.Fatal("non-success agent run MUST still return a populated result so the budget can consume the real ModelCalls — this is the codex-flagged regression. Got nil.")
	}
	// SessionID should be populated from the SDK session even on
	// non-success (the session was created before the Stream
	// error terminated the loop).
	if res.SessionID == "" {
		t.Errorf("SessionID should be populated even on non-success terminal; got empty")
	}
	// Contradicts must be false on non-success (no parsable verdict).
	if res.Contradicts {
		t.Errorf("Contradicts must be false on non-success terminal; got true")
	}
}

func TestBuildContradictPrompt_IncludesTriggerContext(t *testing.T) {
	t.Parallel()
	pair := &model.SimilarMemoryPair{
		MemoryA: model.Memory{
			ID:      "a-1",
			Title:   "Coffee preference",
			Summary: "I drink coffee in the morning.",
			Content: "Detailed notes about my coffee routine.",
		},
		MemoryB: model.Memory{
			ID:      "b-1",
			Title:   "Tea preference",
			Summary: "I drink tea in the morning.",
		},
		Similarity: 0.82,
	}
	decisions := map[string]triggers.Decision{
		"a-1": {
			Fired: true,
			Kind:  triggers.KindUserFollowup,
			Signals: triggers.Signals{
				UserFollowupMarker: "@TODO reconcile coffee vs tea entry",
			},
		},
		"b-1": {
			Fired: true,
			Kind:  triggers.KindContradiction,
			Signals: triggers.Signals{
				ContradictionCount: 4,
			},
		},
	}
	prompt := buildContradictPrompt(pair, decisions)

	for _, want := range []string{
		"Coffee preference",
		"Tea preference",
		"Similarity: 0.820",
		"@TODO reconcile coffee vs tea entry",
		"contradiction_count=4",
		"VERDICT", // implicitly checked via system prompt; shouldn't be in user prompt
	} {
		if want == "VERDICT" {
			if strings.Contains(prompt, want) {
				t.Errorf("user prompt should NOT contain VERDICT instructions (those live in the system prompt), got: %q", prompt)
			}
			continue
		}
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q, got:\n%s", want, prompt)
		}
	}
}
