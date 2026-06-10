package dreams

import (
	"context"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Test hooks. The build-tagged _test.go suffix keeps these out of
// production binaries and external API surface. Tests in package
// dreams_test reach them through the standard "internal export
// pattern" — exported symbols visible only when compiled under
// `go test`.
//
// Codex review (commit ec08f418) flagged the previous public
// NewForTesting / FinalizeForTesting helpers as widening production
// API for test plumbing. They're now confined here.

// NewForTesting builds a minimal Engine suitable for tests that
// exercise wiring (quota completion, scope selection, finalize)
// without driving the full LLM/embedder pipeline.
func NewForTesting(s store.Store, plansResolver planResolver) *Engine {
	txRunner, _ := s.(store.TxRunner)
	return &Engine{
		store:    s,
		txRunner: txRunner,
		plans:    plansResolver,
	}
}

// FinalizeForTesting exposes the engine's completion path
// (UpdateDreamRun + recordQuotaConsumption + writeDreamReceipt) as
// a helper for integration tests. The dream_runs row at run.ID
// must already exist when this is called.
func (e *Engine) FinalizeForTesting(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	periodStart, periodEnd time.Time,
) error {
	snap := e.captureQuotaSnapshot(ctx, hub)
	// Override the period bounds to whatever the test passes; the
	// resolver-driven default (calendar month) may not match what
	// the test seeded.
	if snap.captured {
		snap.periodStart = periodStart
		snap.periodEnd = periodEnd
	}
	return e.finalizeDreamRun(ctx, hub, run, model.DreamRunCounts{}, snap)
}

// FinalizeWithQuotaTerminalForTesting is FinalizeForTesting with
// the explicit quota-terminal-state override exposed. Lets tests
// drive the no_memories / skipped_concurrent paths without
// reproducing the no-memories or worker-skip branches in
// RunForActor.
func (e *Engine) FinalizeWithQuotaTerminalForTesting(
	ctx context.Context,
	hub *model.Hub,
	run *model.DreamRun,
	periodStart, periodEnd time.Time,
	quotaTerminal quota.TerminalState,
) error {
	snap := e.captureQuotaSnapshot(ctx, hub)
	if snap.captured {
		snap.periodStart = periodStart
		snap.periodEnd = periodEnd
	}
	return e.finalizeDreamRunWithQuota(ctx, hub, run, model.DreamRunCounts{}, snap, quotaTerminal)
}
