package dreams

import (
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestParseOrganizeVerdict_AssignExisting(t *testing.T) {
	t.Parallel()
	body := `Looked at neighbors via recall_memories.

VERDICT_JSON: {"action":"assign_existing","topic_id":"00000000-0000-0000-0000-aaaaaaaaaaaa","reason":"clear topical fit","confidence":0.85}`
	v := parseOrganizeVerdict(body)
	if v == nil {
		t.Fatal("expected non-nil verdict")
	}
	if v.Action != organizeAgentActionAssignExisting {
		t.Errorf("Action = %q, want %q", v.Action, organizeAgentActionAssignExisting)
	}
	if v.TopicID != "00000000-0000-0000-0000-aaaaaaaaaaaa" {
		t.Errorf("TopicID = %q, unexpected", v.TopicID)
	}
	if v.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", v.Confidence)
	}
}

func TestParseOrganizeVerdict_NewTopic(t *testing.T) {
	t.Parallel()
	body := `VERDICT_JSON: {"action":"new_topic","new_name":"Daily Standups","new_icon":"calendar-clock","new_description":"Notes from morning standup meetings","reason":"distinct cadence from project notes"}`
	v := parseOrganizeVerdict(body)
	if v == nil {
		t.Fatal("expected non-nil verdict")
	}
	if v.Action != organizeAgentActionNewTopic {
		t.Errorf("Action = %q, want %q", v.Action, organizeAgentActionNewTopic)
	}
	if v.NewName != "Daily Standups" {
		t.Errorf("NewName = %q", v.NewName)
	}
	if v.NewIcon != "calendar-clock" {
		t.Errorf("NewIcon = %q", v.NewIcon)
	}
	if !strings.Contains(v.NewDesc, "standup") {
		t.Errorf("NewDesc = %q, want segment about standups", v.NewDesc)
	}
}

func TestParseOrganizeVerdict_Skip(t *testing.T) {
	t.Parallel()
	v := parseOrganizeVerdict(`VERDICT_JSON: {"action":"skip","reason":"too thin to commit yet"}`)
	if v == nil {
		t.Fatal("expected non-nil verdict")
	}
	if v.Action != organizeAgentActionSkip {
		t.Errorf("Action = %q, want %q", v.Action, organizeAgentActionSkip)
	}
}

// Trailing prose AFTER the verdict line is a contract violation.
// Same fail-safe shape as contradict's parser.
func TestParseOrganizeVerdict_RejectsTrailingProse(t *testing.T) {
	t.Parallel()
	body := `VERDICT_JSON: {"action":"assign_existing","topic_id":"abc"}

Sometimes the model adds a postscript here, against the contract.`
	if v := parseOrganizeVerdict(body); v != nil {
		t.Errorf("trailing prose should reject the verdict; got %+v", v)
	}
}

func TestParseOrganizeVerdict_InvalidJSONReturnsNil(t *testing.T) {
	t.Parallel()
	if v := parseOrganizeVerdict("VERDICT_JSON: {not valid json}"); v != nil {
		t.Errorf("invalid JSON should return nil; got %+v", v)
	}
}

func TestParseOrganizeVerdict_UnknownActionReturnsNil(t *testing.T) {
	t.Parallel()
	if v := parseOrganizeVerdict(`VERDICT_JSON: {"action":"merge","topic_id":"x"}`); v != nil {
		t.Errorf("unknown action should return nil; got %+v", v)
	}
}

func TestParseOrganizeVerdict_NoVerdictReturnsNil(t *testing.T) {
	t.Parallel()
	if v := parseOrganizeVerdict("I'm not sure how to assign this."); v != nil {
		t.Errorf("missing verdict should return nil; got %+v", v)
	}
}

func TestParseOrganizeVerdict_CaseInsensitivePrefix(t *testing.T) {
	t.Parallel()
	v := parseOrganizeVerdict(`verdict_json: {"action":"skip","reason":"x"}`)
	if v == nil || v.Action != organizeAgentActionSkip {
		t.Errorf("case-insensitive prefix should still parse; got %+v", v)
	}
}

func TestShouldRouteOrganizeAgent_KindFilter(t *testing.T) {
	t.Parallel()
	settings := map[string]any{"dreams_use_agent_runtime": true}
	mem := &model.Memory{ID: "m1"}

	cases := []struct {
		kind triggers.Kind
		want bool
	}{
		{triggers.KindTopicOverload, true},
		{triggers.KindUserFollowup, true},
		{triggers.KindContradiction, false}, // contradict-only signal
		{triggers.KindURLDrift, false},      // url-drift fires elsewhere
		{triggers.KindNone, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			decisions := map[string]triggers.Decision{
				"m1": {Fired: tc.kind != triggers.KindNone, Kind: tc.kind},
			}
			e := &Engine{agentRuntime: configuredFakeRuntime()}
			got := e.shouldRouteOrganizeAgent(settings, decisions, mem)
			if got != tc.want {
				t.Errorf("kind=%s → routed=%v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

func TestShouldRouteOrganizeAgent_NoFireKeepsBatch(t *testing.T) {
	t.Parallel()
	mem := &model.Memory{ID: "m1"}
	decisions := map[string]triggers.Decision{
		"m1": {Fired: false, Kind: triggers.KindNone},
	}
	e := &Engine{agentRuntime: configuredFakeRuntime()}
	if e.shouldRouteOrganizeAgent(map[string]any{"dreams_use_agent_runtime": true}, decisions, mem) {
		t.Errorf("no fire on memory should NOT route to agent")
	}
}

func TestShouldRouteOrganizeAgent_RuntimeNilShortCircuits(t *testing.T) {
	t.Parallel()
	mem := &model.Memory{ID: "m1"}
	decisions := map[string]triggers.Decision{
		"m1": {Fired: true, Kind: triggers.KindTopicOverload},
	}
	e := &Engine{} // runtime nil
	if e.shouldRouteOrganizeAgent(map[string]any{"dreams_use_agent_runtime": true}, decisions, mem) {
		t.Errorf("nil runtime should short-circuit before kind filter")
	}
}

func TestShouldRouteOrganizeAgent_FlagOffShortCircuits(t *testing.T) {
	t.Parallel()
	mem := &model.Memory{ID: "m1"}
	decisions := map[string]triggers.Decision{
		"m1": {Fired: true, Kind: triggers.KindTopicOverload},
	}
	e := &Engine{agentRuntime: configuredFakeRuntime()}
	if e.shouldRouteOrganizeAgent(map[string]any{"dreams_use_agent_runtime": false}, decisions, mem) {
		t.Errorf("flag off should short-circuit before kind filter")
	}
}

// TestOrganizeRouting_InlineBudgetGateCannotBeBypassed is the
// regression test for codex review v3's High finding: the
// previous partition-then-execute shape captured budget state
// upfront, so a hub at 99/100 model calls could route 50 fired
// memories before any of them updated the counter.
//
// The current shape consults the budget INLINE per-memory, AFTER
// the previous fired memory's run consumed. This test simulates
// the inline loop with a budget pre-loaded close to the hard
// cap and verifies that routing flips to "no" the moment a run
// pushes the budget over.
func TestOrganizeRouting_InlineBudgetGateCannotBeBypassed(t *testing.T) {
	t.Parallel()
	settings := map[string]any{"dreams_use_agent_runtime": true}
	e := &Engine{agentRuntime: configuredFakeRuntime()}
	decisions := map[string]triggers.Decision{
		"m1": {Fired: true, Kind: triggers.KindTopicOverload},
		"m2": {Fired: true, Kind: triggers.KindTopicOverload},
		"m3": {Fired: true, Kind: triggers.KindTopicOverload},
	}
	memos := []model.Memory{
		{ID: "m1"}, {ID: "m2"}, {ID: "m3"},
	}

	// Pre-load budget so the FIRST fired memory's simulated run
	// (consuming 5 model calls) pushes total over the hard cap.
	// Inline routing must then deny m2 and m3.
	budget := newDreamRunBudget()
	budget.modelCalls = dreamRunBudgetHard - 5

	var routed, batched []string
	for _, m := range memos {
		if e.shouldRouteOrganizeAgent(settings, decisions, &m) && budget.shouldRoute("run-1") {
			routed = append(routed, m.ID)
			// Simulate this run consuming 5 model calls.
			budget.consume(&agent.RunResult{ModelCalls: 5})
			continue
		}
		batched = append(batched, m.ID)
	}

	// m1 routed (budget at 95 < 100 → ok); after consuming 5 →
	// budget at 100 → m2 + m3 should batch.
	if len(routed) != 1 || routed[0] != "m1" {
		t.Errorf("expected only m1 routed, got %v", routed)
	}
	if len(batched) != 2 || batched[0] != "m2" || batched[1] != "m3" {
		t.Errorf("expected m2+m3 batched after budget exhaust, got %v", batched)
	}
}
