package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/providers"
)

// Integration test: a real Anthropic round-trip through agent.Run.
//
// Self-skips when ANTHROPIC_API_KEY is unset so `go test ./...` stays
// green on CI runners and dev machines without a live key. When a
// key IS present, this test exercises the full runtime end-to-end:
// provider construction, tool registration, the SDK's Query loop,
// event drain, and result rollup. It's the only test in the agent
// package that touches the real provider — every other test uses a
// fakeModel.
//
// Run via:
//
//	cd packages/server
//	ANTHROPIC_API_KEY=sk-... go test ./internal/agent/ -run Integration -v
//
// Cost: one Haiku-class round-trip per test. Roughly fractions of a
// cent. Don't run in tight CI loops; the suite that runs every push
// excludes this via the env-var gate.

const integrationTestModel = "claude-haiku-4-5-20251001"

// integrationTimeout is the wall-clock cap on the round-trip. The
// test deliberately caps below the ProfileChat envelope (120s) so a
// hung round-trip fails the test in under 2 minutes rather than
// blocking on the profile's full budget. The trade-off is that
// when the timeout fires it lands as `context deadline exceeded`
// (RunStatusError), not RunStatusBudgetExceeded — but a smoke test
// hitting this path is a real fault, not a budget characterization,
// so the classification difference doesn't matter here.
const integrationTimeout = 90 * time.Second

// TestRun_Integration_AnthropicRoundTrip is the Phase 1 smoke test
// the plan calls out — "an integration test that runs a single
// agent turn against a real Anthropic call (gated on env var)".
//
// What it verifies:
//   - providers.NewAnthropicFromEnv returns a usable model.Client
//     when the env var is set (negative case is in
//     providers/anthropic_test.go).
//   - agent.Run wires the SDK Options correctly: tool registration
//     succeeds, the budget envelope is respected, the run reaches
//     a terminal state without exceptions.
//   - The drain loop captures Final, SessionID, and a non-zero
//     Usage from the provider's usage event.
//   - Status maps to RunStatusSuccess for a normal model reply
//     (no budget exhaust, no provider error).
//
// What it deliberately does NOT verify:
//   - Tool invocation. Models are non-deterministic about tool
//     selection; asserting "the model called the echo tool" would
//     be flaky. The presence of a registered tool exercises the
//     registration path; whether the model uses it is a separate
//     concern handled by the per-feature evals.
//   - Specific text content. We assert non-empty, not the exact
//     reply, so a cheaper-model swap or prompt-style change
//     doesn't break the smoke test.
func TestRun_Integration_AnthropicRoundTrip(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
		t.Skip("ANTHROPIC_API_KEY unset — skipping Anthropic integration test")
	}

	client := providers.NewAnthropicFromEnv(integrationTestModel)
	if client == nil {
		// Defensive: NewAnthropicFromEnv only returns nil for
		// missing/whitespace key, which we just guarded. A nil here
		// means a deeper config problem (e.g. SDK constructor
		// changed) and the test should fail loudly, not skip.
		t.Fatalf("NewAnthropicFromEnv returned nil despite ANTHROPIC_API_KEY being set — provider wiring regression")
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()

	// Echo tool. The integration test reuses the same fake the
	// unit tests use so the smoke test exercises the same tool
	// registration path. Whether the model calls it is non-
	// deterministic and not asserted (see test doc above).
	tool := &echoTool{name: "echo"}

	res, err := Run(ctx, RunInput{
		// Short prompt that doesn't need tools — the model
		// reliably produces a final response. Using a tool-
		// dependent prompt would couple the smoke test to
		// model tool-selection behavior, which is a moving
		// target across model versions.
		Prompt:  "Reply with the single word 'pong'. No other text.",
		Profile: ProfileChat,
		Tools:   []sdktool.Tool{tool},
		Model:   client,
		Session: SessionDescriptor{
			OwnerID: "u-integration-test",
			Type:    ScopeTypeSingleHub,
			HubIDs:  []string{"hub-integration-test"},
		},
	})

	if err != nil {
		t.Fatalf("Run returned pre-flight error: %v", err)
	}

	if res.Status != RunStatusSuccess {
		t.Fatalf("Status = %q, want %q (err=%v, final=%q)", res.Status, RunStatusSuccess, res.Err, res.Final)
	}

	if strings.TrimSpace(res.Final) == "" {
		t.Errorf("Final is empty; want a non-empty model response")
	}

	if res.SessionID == "" {
		t.Errorf("SessionID is empty; want a populated SDK session id")
	}

	if !hasNonZeroUsage(res.Usage) {
		t.Errorf("Usage shows zero tokens (%+v); a real provider should report at least input+output usage", res.Usage)
	}

	t.Logf("Integration round-trip ok: status=%s session=%s tokens=in:%d out:%d total:%d final=%q",
		res.Status, res.SessionID,
		res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.TotalTokens,
		strings.TrimSpace(res.Final))
}

// hasNonZeroUsage reports whether the provider returned any token
// counts. We accept any non-zero field — provider-specific token
// accounting may report all three or only some, depending on the
// stream events the SDK aggregates.
func hasNonZeroUsage(u sdkmodel.Usage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0
}
