package v2dream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Model makes one model call. It is judge.Model, so the classifier Dream
// reuses for duplicates and conflicts runs on the same seam; tests replace
// it with a fake.
type Model = judge.Model

// AnthropicModel calls models through the shared client (internal/anthropic,
// pointed at OpenRouter in production), with zero-data-retention routing,
// each tier on the hosts it pins at the precisions it admits and at its
// temperature (judge.Tier.Routing, Temperature), and counts tokens for the
// edition.
type AnthropicModel struct {
	Client            *anthropic.Client
	ZeroDataRetention bool
}

// NewAnthropicModel returns the model, or nil without a client.
func NewAnthropicModel(client *anthropic.Client, zdr bool) Model {
	if client == nil {
		return nil
	}
	return &AnthropicModel{Client: client, ZeroDataRetention: zdr}
}

// Complete implements Model.
func (m *AnthropicModel) Complete(ctx context.Context, c judge.Call) (string, error) {
	req := anthropic.CompleteRequest{
		Model: c.Tier.Model, MaxTokens: c.Tier.MaxTokens, System: c.System, Prompt: c.Prompt,
		Purpose: "dream." + purposeOf(ctx) + "." + c.Tier.Name, ZeroDataRetention: m.ZeroDataRetention,
		Providers: c.Tier.Routing.Providers, Quantizations: c.Tier.Routing.Quantizations, Temperature: c.Tier.Temperature,
	}
	if c.Tier.Strict {
		req.OutputSchema = c.Schema
	}
	resp, err := m.Client.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	if u := usageOf(ctx); u != nil {
		u.add(c.Tier.Name, resp.InputTokens, resp.OutputTokens)
	}
	if resp.Text == "" {
		return "", judge.ErrEmpty
	}
	return resp.Text, nil
}

// usage counts one run's model calls. A run puts it in its context.
type usage struct {
	mu sync.Mutex
	u  ledger.DreamModelUsage
}

func (u *usage) add(tier string, in, out int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.u.InputTokens += in
	u.u.OutputTokens += out
	if u.u.Tiers == nil {
		u.u.Tiers = map[string]int{}
	}
	u.u.Tiers[tier]++
}

func (u *usage) call(failed bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.u.Calls++
	if failed {
		u.u.Failures++
	}
}

func (u *usage) snapshot() ledger.DreamModelUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := u.u
	if u.u.Tiers != nil {
		out.Tiers = map[string]int{}
		for k, v := range u.u.Tiers {
			out.Tiers[k] = v
		}
	}
	return out
}

type ctxKey int

const (
	usageKey ctxKey = iota
	purposeKey
)

func withUsage(ctx context.Context, u *usage) context.Context {
	return context.WithValue(ctx, usageKey, u)
}

func usageOf(ctx context.Context) *usage {
	u, _ := ctx.Value(usageKey).(*usage)
	return u
}

func withPurpose(ctx context.Context, p string) context.Context {
	return context.WithValue(ctx, purposeKey, p)
}

func purposeOf(ctx context.Context) string {
	if p, _ := ctx.Value(purposeKey).(string); p != "" {
		return p
	}
	return "classify"
}

// countingModel counts every call a run makes, whoever makes it (Dream's
// own prompts or the judge's classifier), and refuses past the run's
// budget.
type countingModel struct {
	inner  Model
	budget int
	mu     sync.Mutex
	used   int
}

var errBudget = errors.New("dream: this run's model calls are used up")

func (m *countingModel) Complete(ctx context.Context, c judge.Call) (string, error) {
	m.mu.Lock()
	if m.used >= m.budget {
		m.mu.Unlock()
		return "", errBudget
	}
	m.used++
	m.mu.Unlock()
	text, err := m.inner.Complete(ctx, c)
	if u := usageOf(ctx); u != nil {
		u.call(err != nil)
	}
	return text, err
}

func (m *countingModel) spent() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used >= m.budget
}

// structured makes one of Dream's own calls: the tiers in order, each
// twice (the second time with what was wrong), the answer validated
// against the schema. It returns the decoded answer and the tier.
func (e *Engine) structured(ctx context.Context, m Model, purpose, system, prompt string, schema *compiledSchema, out any) (string, error) {
	ctx = withPurpose(ctx, purpose)
	var last error
	for _, t := range []judge.Tier{e.cfg.Primary, e.cfg.Fallback} {
		if !t.Enabled() {
			continue
		}
		p := prompt
		if !t.Strict {
			p = "Reply with only a JSON object that matches this JSON Schema, and no other text:\n" + string(schema.raw) + "\n\n" + prompt
		}
		for attempt := 0; attempt < 2; attempt++ {
			callCtx, cancel := context.WithTimeout(ctx, e.cfg.CallTimeout)
			text, err := m.Complete(callCtx, judge.Call{Tier: t, System: system, Prompt: p, Schema: schema.raw})
			cancel()
			if errors.Is(err, errBudget) {
				return "", err
			}
			if err == nil {
				if err = schema.decode(text, out); err == nil {
					return t.Name, nil
				}
			}
			last = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			p = prompt + "\n\nYour previous answer was not usable (" + firstLine(err.Error()) + "). Answer again with only the JSON object."
			if !t.Strict {
				p = "Reply with only a JSON object that matches this JSON Schema, and no other text:\n" + string(schema.raw) + "\n\n" + p
			}
		}
	}
	if last == nil {
		last = errors.New("no model tier configured")
	}
	return "", fmt.Errorf("dream: %s: no usable answer: %w", purpose, last)
}

// compiledSchema is a JSON Schema with its source.
type compiledSchema struct {
	raw    json.RawMessage
	schema *jsonschema.Schema
}

func mustSchema(name string, raw string) *compiledSchema {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		panic(err)
	}
	s, err := c.Compile(name)
	if err != nil {
		panic(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err != nil {
		panic(err)
	}
	return &compiledSchema{raw: compact.Bytes(), schema: s}
}

func (s *compiledSchema) decode(text string, out any) error {
	raw := anthropic.ExtractJSONObject(anthropic.StripMarkdownFences(text))
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	if err := s.schema.Validate(doc); err != nil {
		return fmt.Errorf("doesn't match the schema: %s", firstLine(err.Error()))
	}
	return json.Unmarshal([]byte(raw), out)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
