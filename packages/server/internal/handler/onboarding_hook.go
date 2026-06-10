package handler

import "context"

// OnboardingHook is the handler-side facade for the slimmed-down
// onboarding Recorder. Plan 18's earlier design had Recorder cover
// five hot paths (memory.Create, ask, configs.Sync, hubs.Create +
// AcceptInvite); the materializer at internal/onboarding/materialize.go
// replaced four of those with read-time lazy compute over the
// underlying tables. Ask remains because /ask is stateless — no
// `asks` row gets written per call — so we can't derive completion
// from a count query the way the materializer does for the rest.
//
// Nil-tolerant: when this hook is unset the caller just passes
// through. The Recorder implementation's nil check is the inner
// fence; this interface is the outer one.
type OnboardingHook interface {
	RecordAsk(ctx context.Context, userID string)
}
