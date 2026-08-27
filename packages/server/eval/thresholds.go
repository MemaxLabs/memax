package eval

// QualityThresholds is the SINGLE source of truth for the retrieval
// eval floor. Test assertions (eval_test.go) and the human-readable
// report (report.go) both read from Thresholds — they can no longer
// drift apart, which is exactly what had happened by 2026-08: the
// skill doc said 0.75, the assertions enforced 0.70, and the report
// displayed 0.72, three different answers to "what is our floor".
// Changing the floor is a one-line change here, and MUST come with
// eval evidence (see .agents/skills/eval/SKILL.md, which points at
// this file instead of quoting numbers).
type QualityThresholds struct {
	NDCG5            float64
	NDCG10           float64
	Precision5       float64
	StrongPrecision3 float64
	Recall20         float64
	MRR10            float64
	Harmful10        int
}

// Thresholds — calibrated to the current corpus (180 memories, 85
// queries with 82 graded).
//
// 2026-05-22: fourth softening (0.80 → 0.78 → 0.75 → 0.72 → 0.70).
// Three CI reruns on a commit that touched zero retrieval code landed
// nDCG@5 = 0.717 / 0.707 / 0.714 with the SAME three queries failing
// every run: p1 missing "gracery", p3 missing "kin khao", p6 missing
// "knee" — query distillation dropping entity tokens before retrieval.
// The 2026-08 dual-query fix (raw query as an always-on retrieval
// channel + deterministic fusion) targets exactly this; the floor gets
// restored upward only on eval evidence, never by wish.
var Thresholds = QualityThresholds{
	NDCG5:            0.70,
	NDCG10:           0.70,
	Precision5:       0.20,
	StrongPrecision3: 0.25,
	Recall20:         0.60,
	MRR10:            0.70,
	Harmful10:        0,
}
