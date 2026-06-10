package quota

import (
	"context"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
)

// DreamResolver is the planresolver surface this package depends on.
// Implemented by *planresolver.Resolver. We declare a focused
// interface here so tests can stub it without the full resolver
// stack (Redis cache, plan registry, store).
type DreamResolver interface {
	ResolveDreamLimits(ctx context.Context, userID string) planresolver.DreamLimits
	GetHubDreamLimits(ctx context.Context, hubID string) planresolver.DreamLimits
}

// DreamUsageStore reads current-period dream usage for a scope.
// Implemented by *store.PostgresStore (and in-memory test doubles).
//
// Lazy-creation contract: when no row exists for the (scope,
// period_start) pair, returns zero counts and nil error — never
// surfaces sql.ErrNoRows. The first call writes nothing; the row
// is created lazily by ConsumeDreamRun on first increment.
type DreamUsageStore interface {
	// GetUserDreamUsage returns (basic, lucid) counts for the user
	// in the given billing period.
	GetUserDreamUsage(ctx context.Context, userID string, periodStart time.Time) (basic, lucid int, err error)

	// GetHubDreamUsage returns the lucid count for the hub in the
	// given billing period. Hubs only track Lucid; Basic is a
	// personal-scope concept.
	GetHubDreamUsage(ctx context.Context, hubID string, periodStart time.Time) (lucid int, err error)
}

// DreamConsumeStore performs the atomic-consume transaction at run
// completion. Implemented by *store.PostgresStore.
//
// The interface uses model.DreamConsumeParams (primitive fields) to
// avoid a circular import between quota and store. Conversion from
// the quota package's typed ConsumeParams is handled by Convert.
type DreamConsumeStore interface {
	ConsumeDreamRun(ctx context.Context, params model.DreamConsumeParams) (model.DreamConsumeResult, error)
}

// ConsumeParams is the typed input to ConsumeDreamRun in the quota
// package. Mirrors model.DreamConsumeParams with typed enum values
// the package's call sites care about.
type ConsumeParams struct {
	DreamRunID    string
	Scope         Scope
	Tier          Tier
	TerminalState TerminalState
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Mode          Mode
	ResolvedLimit int
	QuotaSource   string
}

// ConsumeResult is the typed result of ConsumeDreamRun.
type ConsumeResult struct {
	Counted       bool
	QuotaRaceLost bool
	UsedAfter     int
	TerminalState TerminalState
}

// toModel translates typed quota.ConsumeParams to the primitive
// model.DreamConsumeParams the store interface expects.
func (p ConsumeParams) toModel() model.DreamConsumeParams {
	return model.DreamConsumeParams{
		DreamRunID:    p.DreamRunID,
		ScopeType:     string(p.Scope.Type),
		ScopeID:       p.Scope.ID(),
		Tier:          string(p.Tier),
		TerminalState: string(p.TerminalState),
		PeriodStart:   p.PeriodStart,
		PeriodEnd:     p.PeriodEnd,
		Mode:          string(p.Mode),
		ResolvedLimit: p.ResolvedLimit,
		QuotaSource:   p.QuotaSource,
	}
}

// fromModel translates the store's primitive result back to typed
// quota.ConsumeResult.
func resultFromModel(r model.DreamConsumeResult) ConsumeResult {
	return ConsumeResult{
		Counted:       r.Counted,
		QuotaRaceLost: r.QuotaRaceLost,
		UsedAfter:     r.UsedAfter,
		TerminalState: TerminalState(r.TerminalState),
	}
}
