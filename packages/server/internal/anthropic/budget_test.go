package anthropic

import (
	"strings"
	"testing"
)

func TestFitCompleteRequestTrimsOversizedPrompt(t *testing.T) {
	req := CompleteRequest{
		Model:     DefaultModel,
		MaxTokens: 1024,
		System:    strings.Repeat("system ", 2000),
		Prompt:    strings.Repeat("payload ", 150000),
	}
	fitted, budget := FitCompleteRequest(req)
	if !budget.TrimmedPrompt {
		t.Fatal("expected prompt to be trimmed")
	}
	if EstimateTokens(fitted.System)+EstimateTokens(fitted.Prompt) > budget.InputBudgetTokens {
		t.Fatalf("expected fitted request to stay within budget, got input=%d budget=%d", EstimateTokens(fitted.System)+EstimateTokens(fitted.Prompt), budget.InputBudgetTokens)
	}
}

func TestFitCompleteRequestKeepsSmallPromptUnchanged(t *testing.T) {
	req := CompleteRequest{
		Model:     DefaultModel,
		MaxTokens: 300,
		System:    "short system",
		Prompt:    "short prompt",
	}
	fitted, budget := FitCompleteRequest(req)
	if fitted.Prompt != req.Prompt || fitted.System != req.System {
		t.Fatal("expected small prompt to remain unchanged")
	}
	if budget.TrimmedPrompt || budget.TrimmedSystem {
		t.Fatal("did not expect trimming for small prompt")
	}
}

func TestResolveModelSpecUsesLargerSonnet46Window(t *testing.T) {
	haiku := ResolveModelSpec("claude-haiku-4-5-20251001")
	if haiku.ContextWindow != defaultContextWindow {
		t.Fatalf("expected Haiku window %d, got %d", defaultContextWindow, haiku.ContextWindow)
	}

	sonnet := ResolveModelSpec("claude-sonnet-4-6-20250725")
	if sonnet.ContextWindow != sonnetContextWindow {
		t.Fatalf("expected Sonnet 4.6 window %d, got %d", sonnetContextWindow, sonnet.ContextWindow)
	}
}

func TestTruncateToTokensAppendsMarker(t *testing.T) {
	text := strings.Repeat("abcdef ", 2000)
	truncated := truncateToTokens(text, 200)
	if !strings.Contains(truncated, "[truncated]") {
		t.Fatalf("expected truncation marker, got %q", truncated)
	}
	if EstimateTokens(truncated) > 220 {
		t.Fatalf("expected truncated text near target budget, got %d tokens", EstimateTokens(truncated))
	}
}

func TestTruncateMiddleToTokensKeepsHeadAndTail(t *testing.T) {
	text := "HEAD " + strings.Repeat("middle ", 4000) + " TAIL"
	truncated := TruncateMiddleToTokens(text, 300)
	if !strings.Contains(truncated, "HEAD") {
		t.Fatalf("expected head to be preserved, got %q", truncated)
	}
	if !strings.Contains(truncated, "TAIL") {
		t.Fatalf("expected tail to be preserved, got %q", truncated)
	}
	if !strings.Contains(truncated, "[truncated middle]") {
		t.Fatalf("expected middle truncation marker, got %q", truncated)
	}
}

func TestHasTruncationMarker(t *testing.T) {
	if HasTruncationMarker("hello world") {
		t.Fatalf("unexpected truncation marker in plain text")
	}
	if !HasTruncationMarker("alpha\n\n[truncated]\n\nomega") {
		t.Fatalf("expected truncation marker")
	}
	if !HasTruncationMarker("alpha\n\n[truncated middle]\n\nomega") {
		t.Fatalf("expected middle truncation marker")
	}
}

func TestFitMessagesTrimsOversizedTextBlocks(t *testing.T) {
	messages := []map[string]any{{
		"role": "user",
		"content": []any{
			map[string]any{
				"type": "document",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "application/pdf",
					"data":       strings.Repeat("A", 4096),
				},
			},
			map[string]any{
				"type": "text",
				"text": strings.Repeat("instruction ", 80000),
			},
		},
	}}

	fitted, budget := FitMessages(DefaultModel, 4096, messages)
	if budget.DocumentBlockCount != 1 {
		t.Fatalf("expected one document block, got %d", budget.DocumentBlockCount)
	}
	if budget.TrimmedTextBlocks == 0 {
		t.Fatal("expected multimodal text to be trimmed")
	}
	if budget.TextTokens > budget.TextBudgetTokens {
		t.Fatalf("expected text tokens within budget, got text=%d budget=%d", budget.TextTokens, budget.TextBudgetTokens)
	}

	content := fitted[0]["content"].([]any)
	textBlock := content[1].(map[string]any)
	text := textBlock["text"].(string)
	if !strings.Contains(text, "[truncated middle]") {
		t.Fatalf("expected trimmed text marker, got %q", text)
	}
}

func TestFitMessagesTracksImagePayloadMetrics(t *testing.T) {
	messages := []map[string]any{{
		"role": "user",
		"content": []map[string]any{
			{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/png",
					"data":       strings.Repeat("A", 800),
				},
			},
			{
				"type": "text",
				"text": "Extract visible text only.",
			},
		},
	}}

	_, budget := FitMessages(DefaultModel, 1024, messages)
	if budget.ImageBlockCount != 1 {
		t.Fatalf("expected one image block, got %d", budget.ImageBlockCount)
	}
	if budget.ReservedNonTextTokens < imageTokenReserve {
		t.Fatalf("expected image reserve to be applied, got %d", budget.ReservedNonTextTokens)
	}
	if budget.ApproxPayloadBytes == 0 {
		t.Fatal("expected approximate payload bytes to be tracked")
	}
}

func TestLLMContentCaptureConfigDefaultsToDevAndStaging(t *testing.T) {
	t.Setenv("POSTHOG_LLM_CAPTURE_CONTENT", "")
	t.Setenv("POSTHOG_LLM_CAPTURE_CONTENT_MAX_CHARS", "")

	t.Setenv("MEMAX_ENV", "dev")
	enabled, maxChars := llmContentCaptureConfig()
	if !enabled {
		t.Fatal("expected content capture enabled in dev")
	}
	if maxChars != defaultCaptureContentMaxChars {
		t.Fatalf("expected default max chars, got %d", maxChars)
	}

	t.Setenv("MEMAX_ENV", "staging")
	enabled, _ = llmContentCaptureConfig()
	if !enabled {
		t.Fatal("expected content capture enabled in staging")
	}

	t.Setenv("MEMAX_ENV", "production")
	enabled, _ = llmContentCaptureConfig()
	if enabled {
		t.Fatal("expected content capture disabled in production")
	}
}

func TestLLMContentCaptureConfigEnvOverride(t *testing.T) {
	t.Setenv("MEMAX_ENV", "production")
	t.Setenv("POSTHOG_LLM_CAPTURE_CONTENT", "true")
	t.Setenv("POSTHOG_LLM_CAPTURE_CONTENT_MAX_CHARS", "123")

	enabled, maxChars := llmContentCaptureConfig()
	if !enabled {
		t.Fatal("expected explicit override to enable content capture")
	}
	if maxChars != 123 {
		t.Fatalf("expected max chars override, got %d", maxChars)
	}

	t.Setenv("MEMAX_ENV", "staging")
	t.Setenv("POSTHOG_LLM_CAPTURE_CONTENT", "false")
	enabled, _ = llmContentCaptureConfig()
	if enabled {
		t.Fatal("expected explicit override to disable content capture")
	}
}

func TestCaptureMessagesInputRedactsBinaryPayloadAndSecrets(t *testing.T) {
	c := &Client{captureContent: true, captureContentMaxChars: 200}
	input := c.captureMessagesInput([]map[string]any{{
		"role": "user",
		"content": []map[string]any{
			{
				"type": "document",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "application/pdf",
					"data":       strings.Repeat("A", 100),
				},
			},
			{
				"type": "text",
				"text": "token=secret-value sk-1234567890abcdef",
			},
		},
	}})

	messages := input.([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	source := content[0]["source"].(map[string]any)
	if source["data"] != "[redacted binary payload]" {
		t.Fatalf("expected binary data redacted, got %#v", source["data"])
	}
	text := content[1]["text"].(string)
	if strings.Contains(text, "secret-value") || strings.Contains(text, "sk-1234567890abcdef") {
		t.Fatalf("expected secrets redacted, got %q", text)
	}
}

func TestCaptureOutputDisabledOutsideConfiguredEnvs(t *testing.T) {
	t.Setenv("MEMAX_ENV", "production")
	t.Setenv("POSTHOG_LLM_CAPTURE_CONTENT", "")

	capture, _ := llmContentCaptureConfig()
	c := &Client{captureContent: capture}
	if output := c.captureOutput("private answer"); output != nil {
		t.Fatalf("expected no captured output in production, got %#v", output)
	}
}
