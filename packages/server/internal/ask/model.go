package ask

import (
	"context"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// Call is one answer request.
type Call struct {
	Model     string
	System    string
	Prompt    string
	MaxTokens int
}

// Usage is what one answer used.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Model streams one answer. onText gets the text as it arrives, on the
// calling goroutine; cancelling ctx must end the call promptly (the person
// closed Ask, or the answer ran out of time). It is the seam tests replace
// with a fake that streams on a clock.
type Model interface {
	Stream(ctx context.Context, c Call, onText func(string)) (Usage, error)
}

// AnthropicModel answers through the shared client, which speaks the
// Anthropic Messages API (pointed at OpenRouter in production).
type AnthropicModel struct {
	Client *anthropic.Client
	// ZeroDataRetention routes every call to zero-retention providers.
	ZeroDataRetention bool
}

// NewAnthropicModel returns the model, or nil when client is nil (no API
// key: Ask answers with memories only).
func NewAnthropicModel(client *anthropic.Client, zdr bool) Model {
	if client == nil {
		return nil
	}
	return &AnthropicModel{Client: client, ZeroDataRetention: zdr}
}

// Stream implements Model.
func (m *AnthropicModel) Stream(ctx context.Context, c Call, onText func(string)) (Usage, error) {
	u, err := m.Client.CompleteStreamUsage(ctx, anthropic.CompleteRequest{
		Model:             c.Model,
		MaxTokens:         c.MaxTokens,
		System:            c.System,
		Prompt:            c.Prompt,
		Purpose:           "ask.v2.answer",
		ZeroDataRetention: m.ZeroDataRetention,
	}, onText)
	return Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}, err
}
