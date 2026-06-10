// Package providers wires the SDK's provider clients into our
// agent runtime. Each provider is a thin constructor that pulls
// credentials and HTTP config from the same environment variables
// the rest of the codebase already uses, so there's exactly one
// source of truth for "how does memax authenticate to Anthropic."
package providers

import (
	"net/http"
	"os"
	"strings"
	"time"

	sdkanthropic "github.com/MemaxLabs/memax-go-agent-sdk/providers/anthropic"
)

// Anthropic env vars. We deliberately reuse the same names that
// internal/anthropic.NewFromEnv reads so a deployment configures
// exactly one credential per provider, regardless of which subsystem
// (existing direct-completion call sites OR new agent runtime)
// makes the request.
const (
	anthropicAPIKeyEnv  = "ANTHROPIC_API_KEY"
	anthropicBaseURLEnv = "ANTHROPIC_BASE_URL"
)

// Default request timeout for an SDK Anthropic stream. The SDK's
// per-request timeout composes with our existing run-level budgets
// (MaxModelCalls, MaxRunDuration on memaxagent.Options); this value
// is the per-stream ceiling and matches the timeout our existing
// internal/anthropic.Client uses for streaming completions.
const defaultStreamTimeout = 5 * time.Minute

// AnthropicConfig is the resolved provider config. NewAnthropicFromEnv
// produces it; tests and call sites that override the model or key
// build it directly.
type AnthropicConfig struct {
	// APIKey is the Anthropic credential. Required — providers
	// constructed with an empty key would silently fail at request
	// time; we fail fast at construction instead.
	APIKey string

	// Model is the model id passed to the SDK's New() constructor.
	// Per-run model selection (e.g. lucid_passive=Haiku,
	// lucid_active=Sonnet) is implemented above this layer by
	// constructing one provider per profile and routing.
	Model string

	// BaseURL is optional; empty falls through to the SDK's default
	// ("https://api.anthropic.com"). Override for staging/proxy.
	BaseURL string

	// HTTPClient is optional; nil falls through to the SDK's default
	// http.Client. We deliberately don't share our existing
	// internal/anthropic.Client's HTTP client by default — that
	// client wraps requests with telemetry/retries tuned to the
	// non-tool-use Complete API, and the SDK's provider already
	// has its own backoff + error mapping for the tool-use streaming
	// path. Sharing the client could double-wrap. Pass an explicit
	// http.Client only if you have a specific reason (custom proxy,
	// shared connection pool experimentation).
	HTTPClient *http.Client

	// Timeout is the per-stream request ceiling. Zero falls through
	// to defaultStreamTimeout.
	Timeout time.Duration
}

// NewAnthropicFromEnv builds a model.Client backed by the SDK's
// Anthropic provider, reading credentials from the same env vars
// the existing internal/anthropic client uses.
//
// Returns nil when ANTHROPIC_API_KEY is unset — matches our existing
// "LLM features gracefully disabled" pattern. Callers (chat handler,
// dream engine) must check for nil and fail closed: agent runs are
// never silently no-op'd when the provider isn't configured.
//
// modelName is required. Per-profile model selection
// (Haiku/Sonnet/Opus) is the caller's responsibility — typical
// production wiring constructs one provider per (provider, model)
// pair at startup so the hot path doesn't allocate.
func NewAnthropicFromEnv(modelName string) *sdkanthropic.Client {
	key := strings.TrimSpace(os.Getenv(anthropicAPIKeyEnv))
	if key == "" {
		return nil
	}
	cfg := AnthropicConfig{
		APIKey:  key,
		Model:   modelName,
		BaseURL: strings.TrimSpace(os.Getenv(anthropicBaseURLEnv)),
		Timeout: defaultStreamTimeout,
	}
	return NewAnthropic(cfg)
}

// NewAnthropic builds a model.Client from an explicit config.
// Returns nil when cfg.APIKey is empty-or-whitespace (matches
// NewAnthropicFromEnv's fail-closed contract); panics on cfg.Model
// unset because that's a programmer error, not a deployment-config
// issue.
//
// Strings are trimmed before use — explicit configs that pass
// "  key  " or "  https://api  " (e.g. from a per-tenant config
// file with stray whitespace) get the same fail-closed and-or-correct
// treatment as the env path. One source of truth for "what counts
// as configured."
//
// We deliberately keep this helper minimal — it's a pure constructor.
// Per-tenant overrides (different keys per workspace, etc.) go above
// this layer where the deployment knows the policy.
func NewAnthropic(cfg AnthropicConfig) *sdkanthropic.Client {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil
	}
	if cfg.Model == "" {
		panic("agent/providers: AnthropicConfig.Model is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultStreamTimeout
	}

	opts := []sdkanthropic.Option{
		sdkanthropic.WithTimeout(timeout),
	}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, sdkanthropic.WithBaseURL(baseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, sdkanthropic.WithHTTPClient(cfg.HTTPClient))
	}
	return sdkanthropic.New(apiKey, cfg.Model, opts...)
}
