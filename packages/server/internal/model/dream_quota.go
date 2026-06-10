package model

import "time"

// DreamConsumeParams is the input to a single dream-run quota
// consumption transaction. Defined in model (rather than quota or
// store) so both packages can use it without a circular import.
//
// Field semantics match docs/plans/23-dream-tiers.md. String-typed
// fields (ScopeType, Tier, TerminalState, Mode) carry the same
// enum values the quota package's typed constants do; the SQL
// CHECK constraints in migration 002 enforce the valid set.
type DreamConsumeParams struct {
	// DreamRunID is the canonical identifier of the run, used as
	// the idempotency key (PRIMARY KEY on dream_run_completions).
	DreamRunID string

	// ScopeType: "user" or "hub".
	ScopeType string

	// ScopeID: user_id when ScopeType=user, hub_id when scope=hub.
	ScopeID string

	// Tier: "basic" | "lucid_passive" | "lucid_active". The
	// umbrella "lucid" is a check-time-only convenience; consume
	// always records the resolved sub-tier.
	Tier string

	// TerminalState: one of the canonical states from the truth
	// table. Caller MUST NOT pass "quota_race_lost" — that's set
	// internally by the consume primitive on hard-mode race
	// detection.
	TerminalState string

	// PeriodStart, PeriodEnd anchor the billing period.
	PeriodStart time.Time
	PeriodEnd   time.Time

	// Mode: "hard" | "soft". Pinned from start-of-run so a
	// rollout-phase config change between start and completion
	// doesn't mutate the recorded snapshot.
	Mode string

	// ResolvedLimit at start of run. -1 unlimited, 0 disabled,
	// >0 finite cap. Snapshotted into dream_quota_signals so
	// dashboards stay stable across plan changes.
	ResolvedLimit int

	// QuotaSource is the plan ID that contributed the limit.
	// Frozen in dream_quota_signals.
	QuotaSource string
}

// DreamConsumeResult reports the outcome of an atomic consume.
// Mirrors the quota package's typed ConsumeResult with primitive
// strings instead of typed enums; conversion happens at the quota
// package boundary.
type DreamConsumeResult struct {
	// Counted is true when this run was added to the quota
	// counter. False for terminal states that record-but-don't-charge.
	Counted bool

	// QuotaRaceLost is true when the hard-mode guarded UPDATE
	// refused the increment because the cap was just consumed by
	// a parallel completion. The consume primitive transitions
	// the run's terminal_state to "quota_race_lost" in this case.
	// Mutually exclusive with Counted.
	QuotaRaceLost bool

	// UsedAfter is the counter value the caller can show alongside
	// the quota state.
	//
	// Semantics:
	//
	//   - Fresh consume (first call for a run_id): the counter
	//     value AFTER this run's increment (or 0 when no
	//     increment fires, e.g. limit=0 short-circuit, race-lost,
	//     non-counting terminal state).
	//   - Idempotent re-read (call N>1 for the same run_id):
	//     best-effort current counter value. May reflect later
	//     runs' increments since the original. Intentionally NOT
	//     persisted per-run — the source of truth for this run's
	//     contribution is dream_run_completions.counted; if you
	//     need the exact post-this-run value, query the run's
	//     dream_quota_signals row in soft mode (it snapshots
	//     used_after at insert time).
	//
	// Callers that need a stable per-run number for display should
	// snapshot it from the first call's response; subsequent
	// idempotent re-reads are for state recovery, not authoritative
	// reporting.
	UsedAfter int

	// TerminalState is what was recorded in the ledger. Possibly
	// transitioned to "quota_race_lost" from the caller's input.
	TerminalState string
}

// DreamUsage is the read-only quota snapshot returned by
// GET /v1/usage/dreams. Mirrors quota.Decision but with primitive
// strings (so model stays free of the quota package import) and
// adds explicit Scope/HubID at the top level so clients don't have
// to infer them.
//
// Scope values match the public API contract used by DenialDetails
// in 402 responses: "personal" for user-scoped quota,  "hub" for
// hub-scoped. The internal quota.ScopeUser enum is mapped to
// "personal" at the handler boundary so clients see one
// vocabulary across both endpoints.
//
// Tier reporting follows quota.CheckDreamQuota's tier-disambiguation
// rules: a Free user gets Tier="basic" with the basic counter; any
// user with Lucid quota gets Tier="lucid" with the lucid counter;
// hubs always report Tier="lucid". Only ONE tier is reported per
// scope — the active one for that scope — so the UI doesn't have to
// hide irrelevant counters.
//
// Allowed reflects whether a manual trigger right now would succeed
// under the current Mode. The web "Dream now" button reads this to
// gate its enabled state. Soft mode never reports Allowed=false for
// finite-cap exhaustion (it would still allow under Phase 1
// rollout); only Limit=0 (tier disabled) flips Allowed in soft mode.
//
// Remaining is *int so the JSON distinguishes "unlimited" (field
// absent) from "exhausted finite cap" (field present, value 0). A
// plain int with omitempty would erase that distinction — exhausted
// would look identical to unlimited to clients.
type DreamUsage struct {
	Scope       string    `json:"scope"`            // "personal" | "hub"
	HubID       string    `json:"hub_id,omitempty"` // populated only when Scope="hub"
	Tier        string    `json:"tier"`             // "basic" | "lucid"
	Mode        string    `json:"mode"`             // "soft" | "hard"
	Limit       int       `json:"limit"`            // -1 unlimited, 0 disabled, >0 finite
	Used        int       `json:"used"`             // count consumed in current period
	Remaining   *int      `json:"remaining,omitempty"` // nil ONLY when Limit=-1 (omitted from JSON); explicit 0 when exhausted
	Allowed     bool      `json:"allowed"`          // whether a trigger right now would succeed
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	QuotaSource string    `json:"quota_source"` // plan ID that contributed the limit
}
