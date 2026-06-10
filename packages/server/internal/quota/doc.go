// Package quota implements the dream-tier quota policy and the
// atomic-consume primitives that enforce it.
//
// Two entry points:
//
//   - CheckDreamQuota — read-only resolution of "is this scope under
//     its monthly cap right now?". Returns a Decision with the
//     current state, the quota source plan ID, and a Mode flag the
//     caller threads through to ConsumeDreamRun at completion.
//   - ConsumeDreamRun — atomic-consume at run completion, gated by
//     the limit. Hard mode rejects the increment past cap and
//     reclassifies the run's terminal_state to quota_race_lost. Soft
//     mode records actual usage past cap (for Phase 1 telemetry) and
//     never rejects.
//
// Design rationale lives in docs/plans/23-dream-tiers.md (5 review
// rounds, codex-clean). The plan's "Atomic-consume" SQL is the
// reference; this package wraps it via the store interface so the
// SQL stays close to the schema.
//
// # Worker-safety
//
// Both entry points are pure functions over the resolver and store
// interfaces — they don't reach into HTTP middleware or request
// context. The same code path runs from:
//
//   - Manual trigger (POST /v1/dreams/trigger handler)
//   - Worker job execution (dreams_worker)
//   - Nightly engine (engine.go scheduling loop)
//
// All three call CheckDreamQuota for the gate and ConsumeDreamRun
// at completion. The Decision struct's Mode field flows through the
// run context so the start-time mode is what gets recorded at
// completion — even if rollout phase config changes mid-run.
//
// # Idempotency contract
//
// dream_run_completions is the canonical "did we count this run?"
// ledger. PRIMARY KEY on dream_run_id ensures exactly-once counting
// under retries. The counter increment and the ledger insert run in
// the same transaction; if either fails, both roll back.
//
// # See also
//
//   - docs/plans/23-dream-tiers.md — full design
//   - migrations/002_dream_tiers_add.up.sql — schema
//   - planresolver.ResolveDreamLimits / GetHubDreamLimits — limit lookup
package quota
