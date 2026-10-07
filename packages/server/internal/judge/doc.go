// Package judge decides how a proposal relates to the record before a
// person sees it (plan 25 §5.8, epic 1.3): a duplicate is folded into the
// memory it repeats, a repeat of a recent rejection is folded into that
// rejection, an update is linked so Review shows a diff, and a
// contradiction of a decision in force is flagged as a conflict (rule 11).
// Precision comes first: a false conflict costs a person's attention,
// while a missed one is still caught by Dream.
//
// It runs as the River job judge_proposal, which the ledger enqueues in
// the transaction of every command that writes a proposal (or a memory a
// Write-level agent kept at once):
//
//  1. Stage 0, no model. The exact content hash, then MinHash/LSH with an
//     exact Jaccard check (≥ 0.9) and the salient-token guard (numbers,
//     negations, qualifiers), against kept memories: fold. The same
//     against memories rejected in the last 90 days: fold into the
//     rejection, so it never reaches Review.
//  2. Candidates, two sets as Graphiti does: the top 10 kept memories by
//     hybrid similarity (full-text and trigram lanes fused by RRF, with a
//     seam for vectors once V2 memories are embedded), and every decision
//     in force with the same area key (or whose area the proposal names,
//     or which it overlaps enough to touch).
//  3. Stage 1, one structured model call over all candidates through
//     internal/anthropic: a relation per pair (duplicate, updates,
//     extends, contradicts, unrelated), a confidence, a one-line rationale
//     and an optional merged statement, under Graphiti's rule that numbers,
//     dates and qualifiers are never duplicates. The answer is validated
//     against a JSON Schema and retried once, then the strict-schema
//     fallback tier is tried. Verdicts that touch a decision in force are
//     confirmed by the strong tier.
//  4. The outcome goes through the ledger (RecordVerdict), as Memax, with
//     its receipt. Explicit changes of a decision in force with the same
//     key ("we moved from Railway to Fly") are linked as superseding it
//     (TEPA); implicit ones become conflicts for a person to settle.
//
// With no model configured the judge runs stage 0 alone and never blocks
// a proposal; a model that fails leaves the proposal unflagged, with the
// failure recorded on the verdict and logged as a metric.
package judge
