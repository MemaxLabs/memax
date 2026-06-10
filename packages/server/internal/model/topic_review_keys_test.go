package model

import "testing"

// Format pin: dreams + chat MUST produce identical source_ids
// for the same (target, sources, recipient) so the global
// UNIQUE constraint on (source_kind, source_id) dedupes them
// onto the same inbox row.
func TestBuildTopicMergeNotificationSourceID_Format(t *testing.T) {
	t.Parallel()
	got := BuildTopicMergeNotificationSourceID("t-target", []string{"s-a", "s-b"}, "u-1")
	want := "topic_merge:t-target:s-a:s-b:u-1"
	if got != want {
		t.Errorf("source_id = %q, want %q", got, want)
	}
}

// Order independence: same set in different orders → same id.
func TestBuildTopicMergeNotificationSourceID_OrderIndependent(t *testing.T) {
	t.Parallel()
	a := BuildTopicMergeNotificationSourceID("t", []string{"s-c", "s-a", "s-b"}, "u")
	b := BuildTopicMergeNotificationSourceID("t", []string{"s-b", "s-c", "s-a"}, "u")
	c := BuildTopicMergeNotificationSourceID("t", []string{"s-a", "s-b", "s-c"}, "u")
	if a != b || b != c {
		t.Errorf("source_ids differed: %q / %q / %q", a, b, c)
	}
}

// Target filtering: a caller that accidentally includes the
// target in the source list produces the SAME key as a
// caller that omits it. Codex Phase 3.6b3f v2 [Medium]
// flagged that dreams previously included the target in the
// key while chat filtered it out, producing two distinct
// inbox rows for the same merge. The model-level helper now
// strips the target either way.
func TestBuildTopicMergeNotificationSourceID_TargetStripped(t *testing.T) {
	t.Parallel()
	with := BuildTopicMergeNotificationSourceID("t", []string{"t", "s-a", "s-b"}, "u")
	without := BuildTopicMergeNotificationSourceID("t", []string{"s-a", "s-b"}, "u")
	if with != without {
		t.Errorf("target-included = %q, target-excluded = %q (must match)", with, without)
	}
}

// Whitespace + duplicates collapse to the canonical set.
func TestBuildTopicMergeNotificationSourceID_DedupAndTrim(t *testing.T) {
	t.Parallel()
	got := BuildTopicMergeNotificationSourceID("t", []string{"  s-a  ", "s-b", "s-a", "", "s-b"}, "u")
	want := "topic_merge:t:s-a:s-b:u"
	if got != want {
		t.Errorf("source_id = %q, want %q", got, want)
	}
}

// Per-recipient distinct: same merge → distinct keys per
// recipient. Pins the per-user inbox semantics.
func TestBuildTopicMergeNotificationSourceID_RecipientDistinct(t *testing.T) {
	t.Parallel()
	a := BuildTopicMergeNotificationSourceID("t", []string{"s-a"}, "user-1")
	b := BuildTopicMergeNotificationSourceID("t", []string{"s-a"}, "user-2")
	if a == b {
		t.Errorf("same recipient suffix produced identical keys: %q", a)
	}
}
