package triggers

import (
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// memWith builds a minimal Memory carrying just the Phase 2a
// fields the evaluator inspects. Tests that need the full row
// shape don't go through this helper.
func memWith(content, fetchHash, marker string) *model.Memory {
	return &model.Memory{
		ContentHash:        content,
		SourceFetchHash:    fetchHash,
		UserFollowupMarker: marker,
	}
}

func TestEvaluate_NilMemoryReturnsNone(t *testing.T) {
	t.Parallel()
	d := DefaultConfig().Evaluate(Inputs{})
	if d.Fired || d.Kind != KindNone {
		t.Fatalf("nil memory should return None, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}
}

func TestEvaluate_NoSignalsReturnsNone(t *testing.T) {
	t.Parallel()
	d := DefaultConfig().Evaluate(Inputs{Memory: memWith("h1", "", "")})
	if d.Fired || d.Kind != KindNone {
		t.Fatalf("zero signals should return None, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}
}

func TestEvaluate_UserFollowupFiresOnNonEmptyMarker(t *testing.T) {
	t.Parallel()
	d := DefaultConfig().Evaluate(Inputs{
		Memory: memWith("h1", "", "@TODO verify this"),
	})
	if !d.Fired || d.Kind != KindUserFollowup {
		t.Fatalf("expected user_followup fire, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}
	if d.Signals.UserFollowupMarker != "@TODO verify this" {
		t.Errorf("Signals.UserFollowupMarker = %q, want %q", d.Signals.UserFollowupMarker, "@TODO verify this")
	}
}

// User-followup is highest priority — when a marker is present
// AND contradiction count > threshold, user_followup wins.
func TestEvaluate_UserFollowupBeatsContradiction(t *testing.T) {
	t.Parallel()
	d := DefaultConfig().Evaluate(Inputs{
		Memory:             memWith("h1", "", "@TODO foo"),
		ContradictionCount: 5,
	})
	if d.Kind != KindUserFollowup {
		t.Errorf("expected user_followup to win over contradiction, got %q", d.Kind)
	}
	// But the contradiction signal is still recorded.
	if d.Signals.ContradictionCount != 5 {
		t.Errorf("Signals.ContradictionCount = %d, want 5", d.Signals.ContradictionCount)
	}
}

func TestEvaluate_ContradictionFiresAtThreshold(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig() // threshold = 3
	cases := []struct {
		count int
		want  bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{4, true},
		{99, true},
	}
	for _, tc := range cases {
		d := cfg.Evaluate(Inputs{
			Memory:             memWith("h1", "", ""),
			ContradictionCount: tc.count,
		})
		fired := d.Fired && d.Kind == KindContradiction
		if fired != tc.want {
			t.Errorf("count=%d → fired=%v, want %v (kind=%q)", tc.count, fired, tc.want, d.Kind)
		}
	}
}

func TestEvaluate_URLDriftRequiresFetchHashAndMismatch(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	// Same hash → no drift.
	d := cfg.Evaluate(Inputs{Memory: memWith("hash-a", "hash-a", "")})
	if d.Fired {
		t.Error("identical hash should not fire url_drift")
	}

	// Different hash → drift fires.
	d = cfg.Evaluate(Inputs{Memory: memWith("hash-a", "hash-b", "")})
	if !d.Fired || d.Kind != KindURLDrift {
		t.Errorf("hash mismatch should fire url_drift, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}
	if !d.Signals.URLDrift {
		t.Error("Signals.URLDrift should be true on mismatch")
	}

	// Empty fetch hash (pre-Phase-2a or never re-fetched) →
	// must NOT fire even if the stored content_hash is non-empty.
	// Plan 24 §"source_fetch_hash" is explicit: NULL/empty
	// means "scan didn't run" and the trigger short-circuits.
	d = cfg.Evaluate(Inputs{Memory: memWith("hash-a", "", "")})
	if d.Fired {
		t.Error("empty fetch hash must not fire url_drift (pre-Phase-2a memories never fire)")
	}
	if d.Signals.URLDrift {
		t.Error("Signals.URLDrift should be false when fetch hash is empty")
	}
}

// Priority: contradiction beats url_drift.
func TestEvaluate_ContradictionBeatsURLDrift(t *testing.T) {
	t.Parallel()
	d := DefaultConfig().Evaluate(Inputs{
		Memory:             memWith("hash-a", "hash-b", ""),
		ContradictionCount: 3,
	})
	if d.Kind != KindContradiction {
		t.Errorf("expected contradiction to beat url_drift, got %q", d.Kind)
	}
	// Both signals still recorded.
	if !d.Signals.URLDrift {
		t.Error("Signals.URLDrift should still be true even though contradiction won")
	}
}

func TestEvaluate_TopicOverloadFiresAtThreshold(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig() // topic threshold = 12
	d := cfg.Evaluate(Inputs{
		Memory:            memWith("h1", "", ""),
		TopicSiblingCount: 12,
	})
	if !d.Fired || d.Kind != KindTopicOverload {
		t.Errorf("count=12 should fire topic_overload, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}

	d = cfg.Evaluate(Inputs{
		Memory:            memWith("h1", "", ""),
		TopicSiblingCount: 11,
	})
	if d.Fired {
		t.Errorf("count=11 should NOT fire topic_overload, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}
}

// Topic-overload is lowest priority — even when it fires,
// any other fire wins.
func TestEvaluate_TopicOverloadLosesToOthers(t *testing.T) {
	t.Parallel()

	// vs user_followup
	d := DefaultConfig().Evaluate(Inputs{
		Memory:            memWith("h1", "", "@TODO x"),
		TopicSiblingCount: 50,
	})
	if d.Kind != KindUserFollowup {
		t.Errorf("user_followup should win over topic_overload, got %q", d.Kind)
	}

	// vs contradiction
	d = DefaultConfig().Evaluate(Inputs{
		Memory:             memWith("h1", "", ""),
		ContradictionCount: 5,
		TopicSiblingCount:  50,
	})
	if d.Kind != KindContradiction {
		t.Errorf("contradiction should win over topic_overload, got %q", d.Kind)
	}

	// vs url_drift
	d = DefaultConfig().Evaluate(Inputs{
		Memory:            memWith("hash-a", "hash-b", ""),
		TopicSiblingCount: 50,
	})
	if d.Kind != KindURLDrift {
		t.Errorf("url_drift should win over topic_overload, got %q", d.Kind)
	}
}

// Disabled threshold (zero) means the corresponding trigger
// never fires. Useful for testing other triggers in isolation,
// and as a future kill-switch if calibration shows a trigger
// is too noisy to ship.
func TestEvaluate_ZeroThresholdDisablesTrigger(t *testing.T) {
	t.Parallel()
	cfg := Config{ContradictionThreshold: 0, TopicOverloadThreshold: 0}

	d := cfg.Evaluate(Inputs{
		Memory:             memWith("h1", "", ""),
		ContradictionCount: 100,
		TopicSiblingCount:  100,
	})
	if d.Fired {
		t.Errorf("zero thresholds should disable contradiction+topic_overload, got Fired=%v Kind=%q", d.Fired, d.Kind)
	}
	// Signals still populated for calibration.
	if d.Signals.ContradictionCount != 100 || d.Signals.TopicSiblingCount != 100 {
		t.Error("Signals should record raw values even when triggers are disabled")
	}
}

// Signals always populated, even for None.
func TestEvaluate_SignalsPopulatedOnNone(t *testing.T) {
	t.Parallel()
	d := DefaultConfig().Evaluate(Inputs{
		Memory:             memWith("h1", "", ""),
		ContradictionCount: 1,
		TopicSiblingCount:  5,
	})
	if d.Fired {
		t.Fatalf("expected None")
	}
	if d.Signals.ContradictionCount != 1 || d.Signals.TopicSiblingCount != 5 {
		t.Errorf("Signals not populated on None: %+v", d.Signals)
	}
}

// Kind constants must match the migration's CHECK constraint.
// If this assertion fails, either the const list drifted or the
// migration did. Keep them in lockstep.
func TestKindConstantsMatchSchema(t *testing.T) {
	t.Parallel()
	// Mirror of the CHECK constraint in Part A of
	// migrations/006_dreams_phase2_signals_and_audit.up.sql.
	wantKinds := []Kind{
		KindNone,
		KindUserFollowup,
		KindContradiction,
		KindURLDrift,
		KindTopicOverload,
	}
	wantStrings := map[Kind]string{
		KindNone:          "none",
		KindUserFollowup:  "user_followup",
		KindContradiction: "contradiction",
		KindURLDrift:      "url_drift",
		KindTopicOverload: "topic_overload",
	}
	for _, k := range wantKinds {
		if string(k) != wantStrings[k] {
			t.Errorf("Kind %v string = %q, want %q (migration check constraint will reject)", k, string(k), wantStrings[k])
		}
	}
}
