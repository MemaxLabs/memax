package quota

import "time"

// Mode is the rollout-phase enforcement mode. Hard mode rejects
// over-cap consumption; soft mode allows it through and records
// would_have_blocked for telemetry. Both modes still write the
// completion ledger; the difference is whether the counter is
// guarded by the limit.
type Mode string

const (
	ModeHard Mode = "hard"
	ModeSoft Mode = "soft"
)

func (m Mode) Valid() bool {
	return m == ModeHard || m == ModeSoft
}

// Tier identifies the dream tier of a single run. lucid_passive and
// lucid_active are accounted under the same Lucid quota counter;
// the distinction drives margin telemetry only (passive runs cost
// roughly the same as Basic, active runs fire the tool-use loop).
//
// At quota check time (before the run executes), the engine doesn't
// yet know whether it'll be passive or active — use TierBasic for
// Free users, TierLucid (the umbrella) for paid. At consume time
// (after completion), the engine writes the resolved per-run tier.
type Tier string

const (
	// TierBasic — Free user; single LLM call, no tool loop.
	TierBasic Tier = "basic"
	// TierLucid — used at check-time for paid users when the run
	// hasn't executed yet so we don't know passive vs active.
	TierLucid Tier = "lucid"
	// TierLucidPassive — paid run that didn't fire the tool loop;
	// cost equivalent to Basic. Counts against Lucid quota.
	TierLucidPassive Tier = "lucid_passive"
	// TierLucidActive — paid run that fired the tool-use loop;
	// ~10× cost. Counts against Lucid quota.
	TierLucidActive Tier = "lucid_active"
)

// IsLucid returns true for any Lucid sub-tier (umbrella, passive,
// active). All three count against the lucid_dream_count counter.
func (t Tier) IsLucid() bool {
	return t == TierLucid || t == TierLucidPassive || t == TierLucidActive
}

// IsBasic returns true for the Basic tier.
func (t Tier) IsBasic() bool {
	return t == TierBasic
}

// TerminalState is the canonical "how did this run end" enum.
// Mirrors the CHECK constraint on dream_run_completions.terminal_state
// and on dream_runs.terminal_state.
type TerminalState string

const (
	StateCompleted         TerminalState = "completed"
	StatePartialFailed     TerminalState = "partial_failed"
	StateFailed            TerminalState = "failed"
	StateSkippedConcurrent TerminalState = "skipped_concurrent"
	StateNoMemories        TerminalState = "no_memories"
	StateLoopCapHit        TerminalState = "loop_cap_hit"
	// StateQuotaRaceLost is set by ConsumeDreamRun when a hard-mode
	// guarded UPDATE refuses the increment because the cap was just
	// consumed by a parallel completion. Caller should NOT pass this
	// in to ConsumeDreamRun directly; the function transitions a
	// run's terminal_state to it on race detection.
	StateQuotaRaceLost TerminalState = "quota_race_lost"
)

// Counted reports whether this terminal state should charge the
// run against the user's quota. Mirrors the truth table in
// docs/plans/23-dream-tiers.md.
func (s TerminalState) Counted() bool {
	switch s {
	case StateCompleted, StatePartialFailed, StateLoopCapHit:
		return true
	}
	// failed, skipped_concurrent, no_memories, quota_race_lost — not counted.
	return false
}

// ScopeType identifies which quota counter a run charges against.
type ScopeType string

const (
	ScopeUser ScopeType = "user"
	ScopeHub  ScopeType = "hub"
)

// Scope is a triple identifying a quota target. Exactly one of
// UserID/HubID is set, matching Type.
type Scope struct {
	Type   ScopeType
	UserID string // set when Type == ScopeUser
	HubID  string // set when Type == ScopeHub
}

// IsUser returns true for user-scoped quotas (personal-hub triggers
// resolved to max(personal, best_hub)).
func (s Scope) IsUser() bool { return s.Type == ScopeUser }

// IsHub returns true for hub-scoped quotas (team-hub triggers
// against the hub's fixed pool).
func (s Scope) IsHub() bool { return s.Type == ScopeHub }

// ID returns the user_id or hub_id depending on scope type.
// Empty string if Type is unset.
func (s Scope) ID() string {
	switch s.Type {
	case ScopeUser:
		return s.UserID
	case ScopeHub:
		return s.HubID
	}
	return ""
}

// Decision is the result of CheckDreamQuota. It is also the input
// to ConsumeDreamRun's params (most fields are passed through
// unchanged), so callers can carry it from start-of-run gate to
// end-of-run completion as a single value in the run's context.
//
// The Mode field is load-bearing: it pins the rollout phase the
// run was checked under, so a config change between start and
// completion doesn't change what gets recorded.
type Decision struct {
	Allowed     bool           `json:"allowed"`
	Mode        Mode           `json:"mode"`
	Tier        Tier           `json:"tier"`
	Limit       int            `json:"limit"`               // -1 unlimited, 0 disabled, >0 finite
	Used        int            `json:"used"`                // count BEFORE this run
	Remaining   int            `json:"remaining,omitempty"` // omitted when Limit=-1
	PeriodStart time.Time      `json:"period_start"`
	PeriodEnd   time.Time      `json:"period_end"`
	QuotaSource string         `json:"quota_source"`             // plan ID
	Details     *DenialDetails `json:"denial_details,omitempty"` // populated when !Allowed; nil on Allowed=true
}

// DenialDetails is the body of the 402 response when a quota gate
// rejects a request. Matches the existing model.Error.Details
// envelope shape (which is `any`); we use a typed shape here for
// callsite clarity and JSON-marshal it into the error envelope.
type DenialDetails struct {
	Code        string    `json:"code"`  // "basic_quota_exhausted" | "lucid_quota_exhausted"
	Scope       string    `json:"scope"` // "personal" | "hub" — distinct from internal ScopeType for client clarity
	HubID       string    `json:"hub_id,omitempty"`
	Limit       int       `json:"limit"`
	Used        int       `json:"used"`
	ResetsAt    time.Time `json:"resets_at"`
	UpgradeURL  string    `json:"upgrade_url,omitempty"`
	DocsURL     string    `json:"docs_url"`
	QuotaSource string    `json:"quota_source"`
}
