package queue

import (
	"context"

	sdkhook "github.com/MemaxLabs/memax-go-agent-sdk/hook"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"
	sdkapproval "github.com/MemaxLabs/memax-go-agent-sdk/toolkit/approvaltools"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/approver"
	"github.com/MemaxLabs/memax/packages/server/internal/chatstream"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Test-only constructors expose the queue package's
// unexported per-turn types so external tests in queue_test/
// can verify the emitter wiring without forcing the package's
// production code to expose internals.

// NewChatTurnSeqForTest returns a fresh turn-seq counter.
func NewChatTurnSeqForTest() *ChatTurnSeqForTest {
	return &ChatTurnSeqForTest{inner: &chatTurnSeq{}}
}

// ChatTurnSeqForTest wraps the unexported chatTurnSeq so test
// code can pre-advance the counter without exposing the type
// to non-test callers.
type ChatTurnSeqForTest struct{ inner *chatTurnSeq }

// AdvanceForTest pre-allocates n sequence numbers (simulating
// "observer already wrote n events"). Subsequent LockedNext
// calls return n+1, n+2, ...
func (c *ChatTurnSeqForTest) AdvanceForTest(n int) {
	for i := 0; i < n; i++ {
		_, release := c.inner.LockedNext()
		release()
	}
}

// NewChatApprovalEventEmitterForTest constructs the production
// emitter against the supplied dependencies. Used by integration
// tests to verify the emit + chat_message_events round-trip
// without standing up the full chat worker.
func NewChatApprovalEventEmitterForTest(s store.Store, sig *chatstream.Signaler, ownerID, messageID string, seq *ChatTurnSeqForTest) approver.ApprovalEventEmitter {
	return &chatApprovalEventEmitter{
		store:     s,
		signaler:  sig,
		ownerID:   ownerID,
		messageID: messageID,
		seq:       seq.inner,
	}
}

// RegisterFakeChatToolBuilderForTest installs a no-op tool
// builder under the given name so tests can drive
// buildSessionTools' ApprovalRequired branch without shipping
// a real mutating tool. The returned cleanup func removes the
// entry; tests should defer it.
//
// Concurrency: the chatToolBuilders map is modified at test
// time only. Production code reads it without locking. Tests
// that register fakes MUST run serially (no t.Parallel) with
// other tests touching this map.
func RegisterFakeChatToolBuilderForTest(name string, approvalRequired bool, build func() (sdktool.Tool, error)) func() {
	chatToolBuilders[name] = chatToolBuilderEntry{
		Build: func(_ *ChatAgentRuntime, _ agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return build()
		},
		ApprovalRequired: approvalRequired,
	}
	return func() {
		delete(chatToolBuilders, name)
	}
}

// BuildSessionToolsForTest wraps the unexported
// buildSessionTools so external tests can drive its branches
// without setting up a full Work() flow. Returns the tool
// slice + (nil-or-non-nil) hook runner.
func BuildSessionToolsForTest(runtime *ChatAgentRuntime, descriptor agent.SessionDescriptor, sessionTools []string, turnApprover sdkapproval.Approver) ([]sdktool.Tool, *sdkhook.Runner, error) {
	tools, hooks, err := buildSessionTools(runtime, descriptor, sessionTools, chatTurnContext{TurnApprover: turnApprover})
	return tools, hooks, err
}

// BuildSessionDescriptorForTest wraps the unexported
// buildSessionDescriptor so external tests can verify the
// WriteHubID propagation logic without running the full
// chat worker. Phase 3.6b3d v2.
func BuildSessionDescriptorForTest(ownerID string, scopeType agent.ScopeType, scopeHubIDs []string, sessionWriteHubID string) agent.SessionDescriptor {
	return buildSessionDescriptor(ownerID, scopeType, scopeHubIDs, sessionWriteHubID)
}

// ResolveTurnUserMessageForTest exposes the worker's parent-chain
// walk so external tests can pin its regenerate-depth behavior
// (repeated regenerate chains A→A→A→user, depth bound, cycle
// detection, cross-session safety) without spinning up the full
// Work() flow. Phase 3.7b v2.
func ResolveTurnUserMessageForTest(ctx context.Context, s store.Store, assistant *model.ChatMessage, ownerID string) (*model.ChatMessage, error) {
	w := &ChatMessageRunWorker{Store: s}
	return w.resolveTurnUserMessage(ctx, assistant, ownerID)
}
