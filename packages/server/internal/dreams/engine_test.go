package dreams

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestOrganizePreviewPrefersSummaryThenHintThenContent(t *testing.T) {
	t.Run("summary first", func(t *testing.T) {
		got := organizePreview(model.Memory{
			Summary: "Summary text",
			Hint:    "Hint text",
			Content: "Content text",
		})
		if got != "Summary text" {
			t.Fatalf("expected summary, got %q", got)
		}
	})

	t.Run("hint fallback", func(t *testing.T) {
		got := organizePreview(model.Memory{
			Hint:    "Hint text",
			Content: "Content text",
		})
		if got != "Hint text" {
			t.Fatalf("expected hint, got %q", got)
		}
	})

	t.Run("content fallback and truncation", func(t *testing.T) {
		got := organizePreview(model.Memory{
			Content: strings.Repeat("a", 260),
		})
		if len(got) != 223 {
			t.Fatalf("expected truncated preview length 223, got %d", len(got))
		}
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("expected truncated preview to end with ellipsis, got %q", got)
		}
	})
}

func TestNotificationSourceIDsAreStable(t *testing.T) {
	if contradictionNotificationSourceID("b", "a") != contradictionNotificationSourceID("a", "b") {
		t.Fatal("expected contradiction notification source id to be order-independent")
	}

	if topicMergeNotificationSourceID("target", []string{"b", "a"}) != topicMergeNotificationSourceID("target", []string{"a", "b"}) {
		t.Fatal("expected topic merge notification source id to be order-independent")
	}

	if got := topicRestructureNotificationSourceID("child", "Backend"); got != "topic_restructure:child:backend" {
		t.Fatalf("unexpected topic restructure notification source id: %s", got)
	}
}

func TestDefaultDreamPhaseBudgets(t *testing.T) {
	cfg := maintenanceDreamConfig()
	budgets := defaultDreamPhaseBudgets(cfg)
	if budgets["merge"].TimeoutMs == 0 {
		t.Fatal("expected merge timeout budget")
	}
	if budgets["organize"].BatchSize != organizeBatchMaxItems {
		t.Fatalf("expected organize batch size %d, got %d", organizeBatchMaxItems, budgets["organize"].BatchSize)
	}
	if budgets["organize"].PreviewCharBudget != organizeBatchMaxPreviewChars {
		t.Fatalf("expected organize preview char budget %d, got %d", organizeBatchMaxPreviewChars, budgets["organize"].PreviewCharBudget)
	}
	if budgets["organize"].TopicContextLimit != organizeTopicContextLimit {
		t.Fatalf("expected organize topic context limit %d, got %d", organizeTopicContextLimit, budgets["organize"].TopicContextLimit)
	}
	if budgets["organize"].TimeoutMs != cfg.organizeTimeout.Milliseconds() {
		t.Fatalf("expected organize timeout %d, got %d", cfg.organizeTimeout.Milliseconds(), budgets["organize"].TimeoutMs)
	}
	if budgets["restructure"].MaxLLMCalls != 1 {
		t.Fatalf("expected restructure max llm calls 1, got %d", budgets["restructure"].MaxLLMCalls)
	}
}

func TestBootstrapDreamPhaseBudgets(t *testing.T) {
	cfg := bootstrapDreamConfig()
	budgets := defaultDreamPhaseBudgets(cfg)
	if budgets["organize"].CandidateLimit != cfg.organizeCandidateLimit {
		t.Fatalf("expected bootstrap organize candidate limit %d, got %d", cfg.organizeCandidateLimit, budgets["organize"].CandidateLimit)
	}
	if budgets["organize"].MaxLLMCalls != cfg.organizeMaxLLMCalls {
		t.Fatalf("expected bootstrap organize max llm calls %d, got %d", cfg.organizeMaxLLMCalls, budgets["organize"].MaxLLMCalls)
	}
	if budgets["restructure"].TimeoutMs != cfg.restructureTimeout.Milliseconds() {
		t.Fatalf("expected bootstrap restructure timeout %d, got %d", cfg.restructureTimeout.Milliseconds(), budgets["restructure"].TimeoutMs)
	}
}

func TestBuildOrganizeBatches(t *testing.T) {
	t.Run("splits on max item count", func(t *testing.T) {
		memories := make([]model.Memory, organizeBatchMaxItems+1)
		for i := range memories {
			memories[i] = model.Memory{ID: fmt.Sprintf("m%d", i), Summary: "short"}
		}

		batches := buildOrganizeBatches(memories, organizeBatchMaxItems, organizeBatchMaxPreviewChars)
		if len(batches) != 2 {
			t.Fatalf("expected 2 batches, got %d", len(batches))
		}
		if len(batches[0]) != organizeBatchMaxItems {
			t.Fatalf("expected first batch size %d, got %d", organizeBatchMaxItems, len(batches[0]))
		}
		if len(batches[1]) != 1 {
			t.Fatalf("expected second batch size 1, got %d", len(batches[1]))
		}
	})

	t.Run("splits on preview char budget", func(t *testing.T) {
		long := strings.Repeat("x", 220)
		memories := []model.Memory{
			{ID: "m1", Summary: long},
			{ID: "m2", Summary: long},
			{ID: "m3", Summary: long},
			{ID: "m4", Summary: long},
			{ID: "m5", Summary: long},
			{ID: "m6", Summary: long},
			{ID: "m7", Summary: long},
			{ID: "m8", Summary: long},
			{ID: "m9", Summary: long},
		}

		batches := buildOrganizeBatches(memories, organizeBatchMaxItems, organizeBatchMaxPreviewChars)
		if len(batches) < 2 {
			t.Fatalf("expected char budget to split into multiple batches, got %d", len(batches))
		}
		for i, batch := range batches {
			if got := organizeBatchPreviewChars(batch); got > organizeBatchMaxPreviewChars {
				t.Fatalf("batch %d exceeded preview budget: %d > %d", i, got, organizeBatchMaxPreviewChars)
			}
		}
	})
}

func TestGenerateReportIncludesOrganizeTimeoutWarning(t *testing.T) {
	run := &model.DreamRun{
		MemoriesOrganized: 8,
		MemoriesScanned:   97,
		PhaseMetrics: map[string]model.DreamPhaseMetrics{
			"organize": {
				Candidates:       60,
				Batches:          8,
				LLMCalls:         8,
				LLMErrors:        7,
				LLMTimeouts:      7,
				TimedOutBatches:  7,
				CompletedBatches: 1,
			},
		},
	}

	report := (&Engine{}).generateReport(run, nil)
	if !strings.Contains(report, "Organize timed out on 7 of 8 batch(es); 52 inbox memory(ies) remain unassigned.") {
		t.Fatalf("expected timeout warning in report, got %q", report)
	}
	if strings.Contains(report, "your memory is clean") {
		t.Fatalf("expected report to avoid clean message on organize timeout, got %q", report)
	}
}

func TestGenerateReportIncludesBootstrapWarning(t *testing.T) {
	run := &model.DreamRun{
		Mode:            dreamModeBootstrap,
		MemoriesScanned: 42,
	}

	report := (&Engine{}).generateReport(run, nil)
	if !strings.Contains(report, "Bootstrap mode is active for this run") {
		t.Fatalf("expected bootstrap warning in report, got %q", report)
	}
}

func TestShouldSkipMergedPair(t *testing.T) {
	pair := model.SimilarMemoryPair{
		MemoryA: model.Memory{ID: "a"},
		MemoryB: model.Memory{ID: "b"},
	}

	if shouldSkipMergedPair(map[string]bool{}, pair) {
		t.Fatal("expected unprocessed pair to be allowed")
	}

	if !shouldSkipMergedPair(map[string]bool{"a": true}, pair) {
		t.Fatal("expected processed memory A to skip pair")
	}

	if !shouldSkipMergedPair(map[string]bool{"b": true}, pair) {
		t.Fatal("expected processed memory B to skip pair")
	}
}

func TestPartitionReparentTopics(t *testing.T) {
	rootA := &model.Topic{ID: "a", Name: "Alpha"}
	rootB := &model.Topic{ID: "b", Name: "Beta", UserModified: true}
	rootC := &model.Topic{ID: "c", Name: "Gamma", Pinned: true}
	childParent := "a"
	child := &model.Topic{ID: "d", Name: "Child", ParentID: &childParent}

	t.Run("splits movable and review topics", func(t *testing.T) {
		movable, review, valid := partitionReparentTopics(
			[]string{"a", "b", "c"},
			map[string]*model.Topic{
				"a": rootA,
				"b": rootB,
				"c": rootC,
			},
		)
		if !valid {
			t.Fatal("expected topic set to be valid")
		}
		if len(movable) != 1 || movable[0].ID != "a" {
			t.Fatalf("unexpected movable topics: %+v", movable)
		}
		if len(review) != 2 {
			t.Fatalf("expected 2 review topics, got %d", len(review))
		}
	})

	t.Run("rejects non-root topics", func(t *testing.T) {
		_, _, valid := partitionReparentTopics(
			[]string{"a", "d"},
			map[string]*model.Topic{
				"a": rootA,
				"d": child,
			},
		)
		if valid {
			t.Fatal("expected non-root topic set to be invalid")
		}
	})
}

func TestEvaluateDreamEligibility(t *testing.T) {
	now := time.Now()
	graceExpired := now.Add(-(model.HubGracePeriod + time.Hour))
	graceOK := now.Add(-1 * time.Hour)

	okLimits := model.UserLimits{PlanID: "personal_pro", PlanDisplayName: "Pro", DreamsEnabled: true}
	noDreamsLimits := model.UserLimits{PlanID: "personal_free", PlanDisplayName: "Free", DreamsEnabled: false}
	enabledSettings := map[string]any{"dreams_enabled": true}
	disabledSettings := map[string]any{"dreams_enabled": false}

	cases := []struct {
		name         string
		sub          *model.HubSubscription
		limits       model.UserLimits
		settings     map[string]any
		wantSkipped  bool
		wantContains string
	}{
		{
			name:        "no subscription, plan permits, user enabled → run",
			sub:         nil,
			limits:      okLimits,
			settings:    enabledSettings,
			wantSkipped: false,
		},
		{
			name:         "frozen by over-limit past grace → skip",
			sub:          &model.HubSubscription{Status: "active", OverLimitSince: &graceExpired},
			limits:       okLimits,
			settings:     enabledSettings,
			wantSkipped:  true,
			wantContains: "frozen",
		},
		{
			name:         "subscription not active → skip (frozen)",
			sub:          &model.HubSubscription{Status: "past_due"},
			limits:       okLimits,
			settings:     enabledSettings,
			wantSkipped:  true,
			wantContains: "frozen",
		},
		{
			name:        "within grace window → still runs",
			sub:         &model.HubSubscription{Status: "active", OverLimitSince: &graceOK},
			limits:      okLimits,
			settings:    enabledSettings,
			wantSkipped: false,
		},
		{
			name:         "plan lacks dreams → skip",
			sub:          nil,
			limits:       noDreamsLimits,
			settings:     enabledSettings,
			wantSkipped:  true,
			wantContains: "Free",
		},
		{
			name:         "user disabled dreams → skip",
			sub:          nil,
			limits:       okLimits,
			settings:     disabledSettings,
			wantSkipped:  true,
			wantContains: "user settings",
		},
		{
			name:         "freeze takes precedence over disabled plan",
			sub:          &model.HubSubscription{Status: "cancelled"},
			limits:       noDreamsLimits,
			settings:     disabledSettings,
			wantSkipped:  true,
			wantContains: "frozen",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, skipped := evaluateDreamEligibility(tc.sub, tc.limits, tc.settings, now)
			if skipped != tc.wantSkipped {
				t.Fatalf("skipped=%v want=%v (report=%q)", skipped, tc.wantSkipped, report)
			}
			if tc.wantContains != "" && !strings.Contains(report, tc.wantContains) {
				t.Fatalf("report %q does not contain %q", report, tc.wantContains)
			}
			if !skipped && report != "" {
				t.Fatalf("expected empty report on run path, got %q", report)
			}
		})
	}
}

// fakeMeter captures LogEvent calls so we can assert dream phases
// emit the right operations + metadata into usage_events.
type fakeMeter struct {
	events []meterCall
}

type meterCall struct {
	userID, op, hubID, source, agentName string
	metadata                             map[string]any
}

func (f *fakeMeter) LogEvent(userID, op, hubID, source, agentName string, metadata map[string]any) {
	f.events = append(f.events, meterCall{userID, op, hubID, source, agentName, metadata})
}

func TestMeterPhaseEmitsUsageEvent(t *testing.T) {
	m := &fakeMeter{}
	e := &Engine{meter: m}
	metrics := model.DreamPhaseMetrics{
		Candidates: 5, Attempted: 5, Processed: 4, Actions: 3,
		LLMCalls: 5, LLMErrors: 1, Errors: 0,
		TokensIn: 1250, TokensOut: 380, DurationMs: 1200,
	}
	e.meterPhase("user-1", "hub-1", "merge", metrics)

	if len(m.events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(m.events))
	}
	ev := m.events[0]
	if ev.op != "dream.merge" {
		t.Fatalf("expected op=dream.merge, got %q", ev.op)
	}
	if ev.userID != "user-1" || ev.hubID != "hub-1" {
		t.Fatalf("unexpected ids: user=%q hub=%q", ev.userID, ev.hubID)
	}
	// source=dreams, agent_name="" matches the HTTP meter's
	// convention — system-emitted events leave agent_name blank.
	if ev.source != "dreams" {
		t.Fatalf("expected source=dreams, got %q", ev.source)
	}
	if ev.agentName != "" {
		t.Fatalf("expected empty agent_name for system events, got %q", ev.agentName)
	}
	if got := ev.metadata["candidates"]; got != 5 {
		t.Fatalf("metadata.candidates = %v, want 5", got)
	}
	if got := ev.metadata["llm_errors"]; got != 1 {
		t.Fatalf("metadata.llm_errors = %v, want 1", got)
	}
	if got := ev.metadata["tokens_in"]; got != int64(1250) {
		t.Fatalf("metadata.tokens_in = %v, want 1250", got)
	}
	if got := ev.metadata["tokens_out"]; got != int64(380) {
		t.Fatalf("metadata.tokens_out = %v, want 380", got)
	}
	if got := ev.metadata["duration_ms"]; got != int64(1200) {
		t.Fatalf("metadata.duration_ms = %v, want 1200", got)
	}
}

func TestMeterPhaseSilentWhenMeterNil(t *testing.T) {
	e := &Engine{meter: nil}
	// Just verify no panic; there's nothing to assert on a nil meter.
	e.meterPhase("user-1", "hub-1", "merge", model.DreamPhaseMetrics{})
}

func TestComputeFinalStatus(t *testing.T) {
	cases := []struct {
		name    string
		metrics map[string]model.DreamPhaseMetrics
		want    string
	}{
		{
			name:    "nil metrics → completed",
			metrics: nil,
			want:    model.DreamRunStatusCompleted,
		},
		{
			name:    "empty metrics → completed",
			metrics: map[string]model.DreamPhaseMetrics{},
			want:    model.DreamRunStatusCompleted,
		},
		{
			name: "all phases clean → completed",
			metrics: map[string]model.DreamPhaseMetrics{
				"merge":       {LLMCalls: 3, LLMErrors: 0, LLMTimeouts: 0},
				"organize":    {LLMCalls: 5, LLMErrors: 0, LLMTimeouts: 0},
				"restructure": {LLMCalls: 1, LLMErrors: 0, LLMTimeouts: 0},
			},
			want: model.DreamRunStatusCompleted,
		},
		{
			name: "organize had LLM errors → partial_failed",
			metrics: map[string]model.DreamPhaseMetrics{
				"merge":    {LLMCalls: 3, LLMErrors: 0},
				"organize": {LLMCalls: 5, LLMErrors: 2},
			},
			want: model.DreamRunStatusPartialFailed,
		},
		{
			name: "any phase timeout → partial_failed",
			metrics: map[string]model.DreamPhaseMetrics{
				"merge":       {LLMCalls: 3, LLMTimeouts: 1},
				"restructure": {LLMCalls: 1},
			},
			want: model.DreamRunStatusPartialFailed,
		},
		{
			name: "counting-only phases (no LLM) don't demote",
			metrics: map[string]model.DreamPhaseMetrics{
				"archive": {Candidates: 10, Processed: 3, Actions: 3},
			},
			want: model.DreamRunStatusCompleted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeFinalStatus(tc.metrics)
			if got != tc.want {
				t.Fatalf("computeFinalStatus = %q, want %q", got, tc.want)
			}
		})
	}
}
