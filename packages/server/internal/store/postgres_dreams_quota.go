package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Dream-quota primitives. Schema lives in migration 002. Design
// rationale + SQL spec: docs/plans/23-dream-tiers.md.

// GetUserDreamUsage reads the user's basic and lucid dream counters
// for the given billing period. Returns zeros (not an error) when
// no row exists — the row is created lazily by ConsumeDreamRun on
// first increment.
func (s *PostgresStore) GetUserDreamUsage(
	ctx context.Context, userID string, periodStart time.Time,
) (basic, lucid int, err error) {
	row := s.pool.QueryRow(ctx,
		`SELECT basic_dream_count, lucid_dream_count
		 FROM usage
		 WHERE user_id = $1::uuid AND period_start = $2`,
		userID, periodStart,
	)
	if err := row.Scan(&basic, &lucid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("get user dream usage: %w", err)
	}
	return basic, lucid, nil
}

// GetHubDreamUsage reads the hub's lucid dream counter for the
// given billing period. Lazy-creation contract identical to
// GetUserDreamUsage.
func (s *PostgresStore) GetHubDreamUsage(
	ctx context.Context, hubID string, periodStart time.Time,
) (lucid int, err error) {
	row := s.pool.QueryRow(ctx,
		`SELECT lucid_dream_count
		 FROM hub_usage
		 WHERE hub_id = $1::uuid AND period_start = $2`,
		hubID, periodStart,
	)
	if err := row.Scan(&lucid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get hub dream usage: %w", err)
	}
	return lucid, nil
}

// ConsumeDreamRun is the atomic-consume primitive. Single
// transaction; idempotent on params.DreamRunID.
//
// Flow:
//
//  1. INSERT into dream_run_completions ... ON CONFLICT
//     (dream_run_id) DO NOTHING. If no row inserted, the run was
//     already counted; return the prior state and exit.
//  2. If terminal state is countable AND limit != 0:
//     a. Hard mode: guarded UPSERT into usage/hub_usage with
//        WHERE limit < 0 OR count < limit. If no row returned,
//        race lost — UPDATE the completion's terminal_state to
//        'quota_race_lost'.
//     b. Soft mode: unguarded UPSERT, plus INSERT into
//        dream_quota_signals snapshotting the resolved state.
//  3. On successful counter increment, UPDATE the completion's
//     counted=true.
//
// All steps in one tx; if any step fails, all roll back.
func (s *PostgresStore) ConsumeDreamRun(
	ctx context.Context, params model.DreamConsumeParams,
) (model.DreamConsumeResult, error) {
	if err := validateConsumeParams(params); err != nil {
		return model.DreamConsumeResult{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return model.DreamConsumeResult{}, fmt.Errorf("consume dream run: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // safe no-op after Commit

	// Step 1: idempotency ledger insert.
	var insertedRunID string
	err = tx.QueryRow(ctx,
		`INSERT INTO dream_run_completions
		   (dream_run_id, scope_type, scope_id, period_start, tier, terminal_state)
		 VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6)
		 ON CONFLICT (dream_run_id) DO NOTHING
		 RETURNING dream_run_id::text`,
		params.DreamRunID, params.ScopeType, params.ScopeID,
		params.PeriodStart, params.Tier, params.TerminalState,
	).Scan(&insertedRunID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Run was already counted in a prior call. Read the prior
		// state and return it without touching the counter.
		return readPriorCompletion(ctx, tx, params.DreamRunID)
	}
	if err != nil {
		return model.DreamConsumeResult{}, fmt.Errorf("consume dream run: insert completion: %w", err)
	}

	result := model.DreamConsumeResult{
		Counted:       false,
		QuotaRaceLost: false,
		UsedAfter:     0,
		TerminalState: params.TerminalState,
	}

	// Step 2: skip the counter increment if this run isn't
	// countable, OR the tier is disabled (limit=0).
	if !isCountableTerminalState(params.TerminalState) || params.ResolvedLimit == 0 {
		// Mirror the terminal state to dream_runs for query convenience.
		if err := mirrorTerminalToDreamRuns(ctx, tx, params); err != nil {
			return model.DreamConsumeResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return model.DreamConsumeResult{}, fmt.Errorf("consume dream run: commit (no-charge): %w", err)
		}
		return result, nil
	}

	// Step 2: increment the counter. Branch on hard vs soft mode.
	if params.Mode == "hard" {
		usedAfter, raceLost, err := consumeHard(ctx, tx, params)
		if err != nil {
			return model.DreamConsumeResult{}, err
		}
		if raceLost {
			// Hard-mode guard refused — transition the just-inserted
			// completion row to terminal_state=quota_race_lost.
			if err := transitionToRaceLost(ctx, tx, params.DreamRunID); err != nil {
				return model.DreamConsumeResult{}, err
			}
			result.QuotaRaceLost = true
			result.TerminalState = string(QuotaRaceLost)
			// Mirror the transitioned terminal state to dream_runs.
			mirroredParams := params
			mirroredParams.TerminalState = string(QuotaRaceLost)
			if err := mirrorTerminalToDreamRuns(ctx, tx, mirroredParams); err != nil {
				return model.DreamConsumeResult{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return model.DreamConsumeResult{}, fmt.Errorf("consume dream run: commit (race-lost): %w", err)
			}
			return result, nil
		}
		result.Counted = true
		result.UsedAfter = usedAfter
	} else {
		// Soft mode: unguarded UPSERT + signal row.
		usedAfter, err := consumeSoft(ctx, tx, params)
		if err != nil {
			return model.DreamConsumeResult{}, err
		}
		result.Counted = true
		result.UsedAfter = usedAfter
	}

	// Step 3: mark the completion as counted.
	if _, err := tx.Exec(ctx,
		`UPDATE dream_run_completions SET counted = true WHERE dream_run_id = $1::uuid`,
		params.DreamRunID,
	); err != nil {
		return model.DreamConsumeResult{}, fmt.Errorf("consume dream run: mark counted: %w", err)
	}

	// Mirror terminal state + tier to dream_runs.
	if err := mirrorTerminalToDreamRuns(ctx, tx, params); err != nil {
		return model.DreamConsumeResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.DreamConsumeResult{}, fmt.Errorf("consume dream run: commit: %w", err)
	}
	return result, nil
}

// QuotaRaceLost is the canonical string value for the race-loss
// terminal state. Mirrors the CHECK constraint in migration 002.
const QuotaRaceLost = "quota_race_lost"

// validateConsumeParams checks invariants the SQL CHECK constraints
// would also enforce, but failing earlier with a clearer error.
func validateConsumeParams(p model.DreamConsumeParams) error {
	if p.DreamRunID == "" {
		return fmt.Errorf("consume dream run: dream_run_id is required")
	}
	if p.ScopeType != "user" && p.ScopeType != "hub" {
		return fmt.Errorf("consume dream run: invalid scope_type %q", p.ScopeType)
	}
	if p.ScopeID == "" {
		return fmt.Errorf("consume dream run: scope_id is required")
	}
	switch p.Tier {
	case "basic", "lucid_passive", "lucid_active":
	default:
		return fmt.Errorf("consume dream run: invalid tier %q", p.Tier)
	}
	if p.ScopeType == "hub" && p.Tier == "basic" {
		// Defense in depth on the store boundary. Hub consume path
		// always increments lucid_dream_count, so a basic-tier hub
		// run would silently drain the hub's Lucid pool. The quota
		// package's validator rejects this too; we re-check here
		// in case the store is called via a different code path
		// (e.g., admin tooling, reconciliation).
		return fmt.Errorf("consume dream run: hub scope cannot use tier=basic (hubs only have Lucid)")
	}
	if p.TerminalState == QuotaRaceLost {
		return fmt.Errorf("consume dream run: caller must not pass quota_race_lost (internal-only)")
	}
	if p.Mode != "hard" && p.Mode != "soft" {
		return fmt.Errorf("consume dream run: invalid mode %q", p.Mode)
	}
	if p.PeriodStart.IsZero() || p.PeriodEnd.IsZero() {
		return fmt.Errorf("consume dream run: period_start and period_end are required")
	}
	return nil
}

// isCountableTerminalState mirrors the truth table in
// docs/plans/23-dream-tiers.md.
func isCountableTerminalState(state string) bool {
	switch state {
	case "completed", "partial_failed", "loop_cap_hit":
		return true
	}
	return false
}

// readPriorCompletion looks up a previously-inserted completion row
// and returns its state. Used when the idempotency gate detects a
// duplicate ConsumeDreamRun call.
func readPriorCompletion(ctx context.Context, tx pgx.Tx, dreamRunID string) (model.DreamConsumeResult, error) {
	var counted bool
	var terminalState, scopeType, scopeID string
	var periodStart time.Time
	var tier string
	err := tx.QueryRow(ctx,
		`SELECT counted, terminal_state, scope_type, scope_id::text, period_start, tier
		 FROM dream_run_completions
		 WHERE dream_run_id = $1::uuid`,
		dreamRunID,
	).Scan(&counted, &terminalState, &scopeType, &scopeID, &periodStart, &tier)
	if err != nil {
		return model.DreamConsumeResult{}, fmt.Errorf("read prior completion: %w", err)
	}

	result := model.DreamConsumeResult{
		Counted:       counted,
		QuotaRaceLost: terminalState == QuotaRaceLost,
		TerminalState: terminalState,
	}
	if counted {
		// Look up the current counter value so the caller can render
		// quota state without a second query. Best-effort — if the
		// counter row is gone (shouldn't happen in practice), return
		// 0 rather than failing the idempotent path.
		switch scopeType {
		case "user":
			var basic, lucid int
			_ = tx.QueryRow(ctx,
				`SELECT basic_dream_count, lucid_dream_count FROM usage
				 WHERE user_id = $1::uuid AND period_start = $2`,
				scopeID, periodStart,
			).Scan(&basic, &lucid)
			if tier == "basic" {
				result.UsedAfter = basic
			} else {
				result.UsedAfter = lucid
			}
		case "hub":
			_ = tx.QueryRow(ctx,
				`SELECT lucid_dream_count FROM hub_usage
				 WHERE hub_id = $1::uuid AND period_start = $2`,
				scopeID, periodStart,
			).Scan(&result.UsedAfter)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.DreamConsumeResult{}, fmt.Errorf("commit idempotent path: %w", err)
	}
	return result, nil
}

// consumeHard runs the guarded UPSERT for hard-mode increments.
// Returns (usedAfter, raceLost, err). raceLost=true when the cap
// has already been reached by a parallel completion; the caller
// must NOT count the run and should reclassify the terminal state.
func consumeHard(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) (int, bool, error) {
	switch p.ScopeType {
	case "user":
		return consumeUserHard(ctx, tx, p)
	case "hub":
		return consumeHubHard(ctx, tx, p)
	}
	return 0, false, fmt.Errorf("consume hard: invalid scope_type %q", p.ScopeType)
}

func consumeUserHard(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) (int, bool, error) {
	// Build the bumps for the right counter based on tier.
	basicBump, lucidBump := tierBumps(p.Tier)

	// The WHERE clause on ON CONFLICT DO UPDATE enforces the cap
	// inside the same SQL statement that performs the increment.
	// For first INSERT (no prior row), the unique-key constraint
	// serializes concurrent INSERTs; the loser falls into ON
	// CONFLICT and goes through the guarded UPDATE path.
	const sql = `
		INSERT INTO usage (user_id, period_start, period_end,
		                   basic_dream_count, lucid_dream_count, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, now())
		ON CONFLICT (user_id, period_start) DO UPDATE
		   SET basic_dream_count = usage.basic_dream_count + $4,
		       lucid_dream_count = usage.lucid_dream_count + $5,
		       updated_at = now()
		   WHERE $6::int < 0
		      OR (CASE WHEN $7::text = 'basic'
		               THEN usage.basic_dream_count
		               ELSE usage.lucid_dream_count END) < $6::int
		RETURNING (CASE WHEN $7::text = 'basic'
		                 THEN basic_dream_count
		                 ELSE lucid_dream_count END) AS used_after
	`
	var usedAfter int
	err := tx.QueryRow(ctx, sql,
		p.ScopeID, p.PeriodStart, p.PeriodEnd,
		basicBump, lucidBump,
		p.ResolvedLimit, p.Tier,
	).Scan(&usedAfter)

	if errors.Is(err, pgx.ErrNoRows) {
		// Guard refused: cap was just consumed by a parallel run.
		return 0, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("consume user hard: %w", err)
	}
	return usedAfter, false, nil
}

func consumeHubHard(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) (int, bool, error) {
	// Hubs only track lucid; tier value is one of lucid_passive /
	// lucid_active and both go to lucid_dream_count.
	const sql = `
		INSERT INTO hub_usage (hub_id, period_start, period_end, lucid_dream_count, updated_at)
		VALUES ($1::uuid, $2, $3, 1, now())
		ON CONFLICT (hub_id, period_start) DO UPDATE
		   SET lucid_dream_count = hub_usage.lucid_dream_count + 1,
		       updated_at = now()
		   WHERE $4::int < 0 OR hub_usage.lucid_dream_count < $4::int
		RETURNING lucid_dream_count
	`
	var usedAfter int
	err := tx.QueryRow(ctx, sql,
		p.ScopeID, p.PeriodStart, p.PeriodEnd, p.ResolvedLimit,
	).Scan(&usedAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("consume hub hard: %w", err)
	}
	return usedAfter, false, nil
}

// consumeSoft runs the unguarded UPSERT plus the signal-row insert.
// Always succeeds (no race-loss path in soft mode).
func consumeSoft(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) (int, error) {
	usedAfter, err := func() (int, error) {
		switch p.ScopeType {
		case "user":
			return consumeUserSoft(ctx, tx, p)
		case "hub":
			return consumeHubSoft(ctx, tx, p)
		}
		return 0, fmt.Errorf("consume soft: invalid scope_type %q", p.ScopeType)
	}()
	if err != nil {
		return 0, err
	}

	// Step: signal row for capacity-planning telemetry. Captures
	// the snapshot at completion time so dashboards stay valid
	// after plan changes.
	wouldHaveBlocked := p.ResolvedLimit > 0 && usedAfter > p.ResolvedLimit
	if _, err := tx.Exec(ctx,
		`INSERT INTO dream_quota_signals
		   (dream_run_id, scope_type, scope_id, period_start, tier, mode,
		    resolved_limit, used_after, quota_source, would_have_blocked)
		 VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10)`,
		p.DreamRunID, p.ScopeType, p.ScopeID, p.PeriodStart, p.Tier, p.Mode,
		p.ResolvedLimit, usedAfter, p.QuotaSource, wouldHaveBlocked,
	); err != nil {
		return 0, fmt.Errorf("consume soft: insert signal: %w", err)
	}

	return usedAfter, nil
}

func consumeUserSoft(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) (int, error) {
	basicBump, lucidBump := tierBumps(p.Tier)
	const sql = `
		INSERT INTO usage (user_id, period_start, period_end,
		                   basic_dream_count, lucid_dream_count, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, now())
		ON CONFLICT (user_id, period_start) DO UPDATE
		   SET basic_dream_count = usage.basic_dream_count + $4,
		       lucid_dream_count = usage.lucid_dream_count + $5,
		       updated_at = now()
		RETURNING (CASE WHEN $6::text = 'basic'
		                 THEN basic_dream_count
		                 ELSE lucid_dream_count END) AS used_after
	`
	var usedAfter int
	if err := tx.QueryRow(ctx, sql,
		p.ScopeID, p.PeriodStart, p.PeriodEnd,
		basicBump, lucidBump, p.Tier,
	).Scan(&usedAfter); err != nil {
		return 0, fmt.Errorf("consume user soft: %w", err)
	}
	return usedAfter, nil
}

func consumeHubSoft(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) (int, error) {
	const sql = `
		INSERT INTO hub_usage (hub_id, period_start, period_end, lucid_dream_count, updated_at)
		VALUES ($1::uuid, $2, $3, 1, now())
		ON CONFLICT (hub_id, period_start) DO UPDATE
		   SET lucid_dream_count = hub_usage.lucid_dream_count + 1,
		       updated_at = now()
		RETURNING lucid_dream_count
	`
	var usedAfter int
	if err := tx.QueryRow(ctx, sql, p.ScopeID, p.PeriodStart, p.PeriodEnd).Scan(&usedAfter); err != nil {
		return 0, fmt.Errorf("consume hub soft: %w", err)
	}
	return usedAfter, nil
}

// tierBumps returns (basicBump, lucidBump) — exactly one is 1, the
// other 0, depending on tier. lucid_passive and lucid_active both
// increment lucid_dream_count.
func tierBumps(tier string) (int, int) {
	if tier == "basic" {
		return 1, 0
	}
	return 0, 1
}

// transitionToRaceLost flips a completion row's terminal_state to
// quota_race_lost. Called when hard-mode guard refused.
func transitionToRaceLost(ctx context.Context, tx pgx.Tx, dreamRunID string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE dream_run_completions
		    SET terminal_state = $1, counted = false
		  WHERE dream_run_id = $2::uuid`,
		QuotaRaceLost, dreamRunID,
	); err != nil {
		return fmt.Errorf("transition to race lost: %w", err)
	}
	return nil
}

// mirrorTerminalToDreamRuns writes the terminal state + tier +
// triggered_by snapshot to dream_runs for query convenience. The
// canonical source of truth is dream_run_completions; this mirror
// avoids a join when listing recent runs.
func mirrorTerminalToDreamRuns(ctx context.Context, tx pgx.Tx, p model.DreamConsumeParams) error {
	// triggered_by is a separate column populated by the engine
	// when the run starts; we don't overwrite it here. tier and
	// terminal_state are populated at completion only.
	_, err := tx.Exec(ctx,
		`UPDATE dream_runs
		    SET tier = $1, terminal_state = $2
		  WHERE id = $3::uuid`,
		p.Tier, p.TerminalState, p.DreamRunID,
	)
	if err != nil {
		return fmt.Errorf("mirror terminal to dream_runs: %w", err)
	}
	return nil
}
