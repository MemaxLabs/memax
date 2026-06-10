package providers

import (
	"net/http"
	"testing"
	"time"
)

// These tests pin the constructor's contract WITHOUT making real
// HTTP calls. The SDK's Anthropic provider has its own coverage of
// the wire protocol; what we own here is the env-reading + nil-on-
// missing-key + panic-on-missing-model behavior, plus the option
// pass-through.

func TestNewAnthropicFromEnvReturnsNilWhenKeyMissing(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	got := NewAnthropicFromEnv("claude-sonnet-4-6")
	if got != nil {
		t.Errorf("expected nil when ANTHROPIC_API_KEY is unset, got %T", got)
	}
}

func TestNewAnthropicFromEnvReturnsNilOnWhitespaceKey(t *testing.T) {
	// Defense against a deployment that exports the env var but
	// leaves it blank or whitespace — the existing
	// internal/anthropic.NewFromEnv treats this as "not configured."
	// We mirror that semantic.
	t.Setenv("ANTHROPIC_API_KEY", "   \t  ")

	if got := NewAnthropicFromEnv("claude-sonnet-4-6"); got != nil {
		t.Errorf("expected nil for whitespace key, got %T", got)
	}
}

func TestNewAnthropicFromEnvHonorsCredentialsAndModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-abc")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	got := NewAnthropicFromEnv("claude-haiku-4-5-20251001")
	if got == nil {
		t.Fatal("expected non-nil client when key is set")
	}
	if got.APIKey != "test-key-abc" {
		t.Errorf("APIKey = %q, want test-key-abc", got.APIKey)
	}
	if got.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model = %q, want claude-haiku-4-5-20251001", got.Model)
	}
}

func TestNewAnthropicFromEnvForwardsBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_BASE_URL", "https://staging-anthropic.test")

	got := NewAnthropicFromEnv("claude-sonnet-4-6")
	if got.BaseURL != "https://staging-anthropic.test" {
		t.Errorf("BaseURL = %q, want staging override", got.BaseURL)
	}
}

func TestNewAnthropicReturnsNilForEmptyKey(t *testing.T) {
	got := NewAnthropic(AnthropicConfig{Model: "claude-sonnet-4-6"})
	if got != nil {
		t.Errorf("expected nil when APIKey is empty, got %T", got)
	}
}

func TestNewAnthropicTrimsExplicitConfig(t *testing.T) {
	// Per-tenant configs that pass whitespace-padded values get the
	// same trim treatment as the env path. Pin the contract so a
	// stray space in a tenant config doesn't produce a subtly
	// broken client.
	got := NewAnthropic(AnthropicConfig{
		APIKey:  "  k  ",
		Model:   "claude-sonnet-4-6",
		BaseURL: "  https://staging.test  ",
	})
	if got == nil {
		t.Fatal("expected non-nil after trim")
	}
	if got.APIKey != "k" {
		t.Errorf("APIKey = %q, want trimmed %q", got.APIKey, "k")
	}
	if got.BaseURL != "https://staging.test" {
		t.Errorf("BaseURL = %q, want trimmed %q", got.BaseURL, "https://staging.test")
	}
}

func TestNewAnthropicReturnsNilOnWhitespaceOnlyKey(t *testing.T) {
	got := NewAnthropic(AnthropicConfig{
		APIKey: "   \t\n  ", Model: "claude-sonnet-4-6",
	})
	if got != nil {
		t.Errorf("expected nil for whitespace-only APIKey, got %T", got)
	}
}

func TestNewAnthropicPanicsForEmptyModel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when Model is empty")
		}
	}()
	NewAnthropic(AnthropicConfig{APIKey: "k"})
}

func TestNewAnthropicAppliesTimeout(t *testing.T) {
	got := NewAnthropic(AnthropicConfig{
		APIKey: "k", Model: "claude-sonnet-4-6", Timeout: 7 * time.Second,
	})
	if got == nil {
		t.Fatal("nil client")
	}
	if got.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v, want 7s", got.Timeout)
	}
}

func TestNewAnthropicDefaultsTimeoutWhenZero(t *testing.T) {
	got := NewAnthropic(AnthropicConfig{APIKey: "k", Model: "claude-sonnet-4-6"})
	if got.Timeout != defaultStreamTimeout {
		t.Errorf("Timeout = %v, want default %v", got.Timeout, defaultStreamTimeout)
	}
}

func TestNewAnthropicForwardsCustomHTTPClient(t *testing.T) {
	// Sentinel client — the test asserts the SDK provider receives
	// our specific instance, not a fresh one. This guards against
	// the option-builder dropping our HTTPClient on the floor.
	custom := &http.Client{Timeout: 99 * time.Second}
	got := NewAnthropic(AnthropicConfig{
		APIKey: "k", Model: "claude-sonnet-4-6", HTTPClient: custom,
	})
	if got.HTTPClient != custom {
		t.Errorf("HTTPClient = %p, want sentinel %p", got.HTTPClient, custom)
	}
}

func TestNewAnthropicLeavesHTTPClientNilByDefault(t *testing.T) {
	// We don't share our existing internal/anthropic.Client's HTTP
	// client by default — see the doc comment in
	// AnthropicConfig.HTTPClient for why. Pin the contract: when
	// caller supplies nil, the SDK provider gets nil (and falls
	// through to its own default), not our existing client.
	got := NewAnthropic(AnthropicConfig{APIKey: "k", Model: "claude-sonnet-4-6"})
	if got.HTTPClient != nil {
		t.Errorf("HTTPClient = %p, want nil (SDK default)", got.HTTPClient)
	}
}
