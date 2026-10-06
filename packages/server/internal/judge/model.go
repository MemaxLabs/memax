package judge

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// Call is one model request.
type Call struct {
	Tier   Tier
	System string
	Prompt string
	// Schema is the answer's JSON Schema; strict tiers send it to the
	// provider, the rest get it in the prompt.
	Schema json.RawMessage
}

// Model makes one model call and returns the answer's text. It is the
// seam the tests replace with a deterministic fake.
type Model interface {
	Complete(ctx context.Context, call Call) (string, error)
}

// ErrEmpty is a model answer with no text: DeepSeek in JSON mode "may
// occasionally return empty content".
var ErrEmpty = errors.New("judge: the model returned an empty answer")

// AnthropicModel calls models through the shared client, which speaks the
// Anthropic Messages API (pointed at OpenRouter in production).
type AnthropicModel struct {
	Client *anthropic.Client
	// ZeroDataRetention routes every call to zero-retention providers.
	ZeroDataRetention bool
}

// NewAnthropicModel returns the model, or nil when client is nil (no API
// key: the judge runs stage 0 only).
func NewAnthropicModel(client *anthropic.Client, zdr bool) Model {
	if client == nil {
		return nil
	}
	return &AnthropicModel{Client: client, ZeroDataRetention: zdr}
}

// Complete implements Model.
func (m *AnthropicModel) Complete(ctx context.Context, c Call) (string, error) {
	req := anthropic.CompleteRequest{
		Model:             c.Tier.Model,
		MaxTokens:         c.Tier.MaxTokens,
		System:            c.System,
		Prompt:            c.Prompt,
		Purpose:           "judge.classify." + c.Tier.Name,
		ZeroDataRetention: m.ZeroDataRetention,
	}
	if c.Tier.Strict {
		req.OutputSchema = c.Schema
	}
	resp, err := m.Client.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	if resp.Text == "" {
		return "", ErrEmpty
	}
	return resp.Text, nil
}
