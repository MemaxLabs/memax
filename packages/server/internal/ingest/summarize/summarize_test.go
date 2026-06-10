package summarize

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

func TestParseResultReadsSummaryAndTitleJSON(t *testing.T) {
	result := parseResult(`{"title":"OAuth Redirect Fix","summary":"**OAuth** callback URLs were corrected for staging."}`)

	if result.Title != "OAuth Redirect Fix" {
		t.Fatalf("expected title, got %q", result.Title)
	}
	if result.Summary != "**OAuth** callback URLs were corrected for staging." {
		t.Fatalf("expected summary, got %q", result.Summary)
	}
}

func TestParseResultFallsBackToSummaryText(t *testing.T) {
	result := parseResult(`A plain summary from an older model response.`)

	if result.Title != "" {
		t.Fatalf("expected no title for non-json response, got %q", result.Title)
	}
	if result.Summary != "A plain summary from an older model response." {
		t.Fatalf("expected fallback summary, got %q", result.Summary)
	}
}

func TestParseResultDropsMalformedJSON(t *testing.T) {
	result := parseResult(`{ "title": "memax 技术架构与差异化诊断：新定位为共享大脑", "summary": "memax 是独特的新物种，结合 active dump + passive organization`)

	if result.Title != "" {
		t.Fatalf("expected malformed JSON title to be dropped, got %q", result.Title)
	}
	if result.Summary != "" {
		t.Fatalf("expected malformed JSON summary to be dropped, got %q", result.Summary)
	}
}

func TestParseResultDropsPreamblePlusTruncatedJSON(t *testing.T) {
	// LLM violates "JSON only" with preamble, then gets truncated
	result := parseResult(`Here is the JSON:
{ "title": "Good title", "summary": "This is a summary that gets cut off...`)

	if result.Title != "" {
		t.Fatalf("expected preamble+truncated JSON title to be dropped, got %q", result.Title)
	}
	if result.Summary != "" {
		t.Fatalf("expected preamble+truncated JSON summary to be dropped, got %q", result.Summary)
	}
}

func TestParseResultDropsPreambleWithWhitespaceAroundColon(t *testing.T) {
	// LLM uses "title" : with space around colon
	result := parseResult(`Here is the JSON:
{ "title" : "Good title", "summary" : "This gets cut off...`)

	if result.Title != "" {
		t.Fatalf("expected preamble+spaced-colon JSON to be dropped, got title %q", result.Title)
	}
	if result.Summary != "" {
		t.Fatalf("expected preamble+spaced-colon JSON to be dropped, got summary %q", result.Summary)
	}
}

func TestCleanTitleCandidateRejectsOverlong(t *testing.T) {
	// 81+ rune title should be rejected, not truncated
	longTitle := strings.Repeat("あ", 81) // 81 CJK characters
	result := validateGeneratedTitle(longTitle)
	if result != "" {
		t.Fatalf("expected overlong generated title to be rejected, got %q (len=%d)", result, len([]rune(result)))
	}

	// 80 rune title should be accepted
	okTitle := strings.Repeat("あ", 80)
	result = validateGeneratedTitle(okTitle)
	if result != okTitle {
		t.Fatalf("expected 80-rune title to be accepted, got %q", result)
	}
}

func TestValidateGeneratedTitle(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"generic", "Document", ""},
		{"json artifact", `{"title":"Bad"}`, ""},
		{"field artifact", `"summary": "Bad"`, ""},
		{"markdown fence", "```json", ""},
		{"heading", "# Heading", ""},
		{"prompt leakage", "Here is the title", ""},
		{"timestamp", "2026-04-17 10:30", ""},
		{"source label", "Body: Hi Mobility Contact", ""},
		{"email greeting", "Hi Mobility Contact", ""},
		{"valid chinese", "memax 技术架构诊断", "memax 技术架构诊断"},
		{"valid code title", "Memax CLI: personal hub alias support", "Memax CLI: personal hub alias support"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateGeneratedTitle(tt.raw); got != tt.want {
				t.Fatalf("validateGeneratedTitle(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestValidateGeneratedSummary(t *testing.T) {
	validMarkdown := "**OAuth** callback URLs were corrected for [staging](https://example.com)."
	validChinese := "memax 的总结逻辑现在会拒绝损坏的 JSON，并保留已有元数据。"
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"overlong", strings.Repeat("a", 1001), ""},
		{"json", `{"summary":"Bad"}`, ""},
		{"xml", `<summary>Bad</summary>`, ""},
		{"field artifact", `"title": "Bad"`, ""},
		{"prompt leakage", "The summary is this.", ""},
		{"label prefix summary:", "Summary: This is the actual content.", ""},
		{"label prefix title:", "Title: Some title\nSummary: Some summary.", ""},
		{"subject line is valid", "Subject: export job failed after token rotation. The fix was to regenerate credentials.", "Subject: export job failed after token rotation. The fix was to regenerate credentials."},
		{"this memory", "This memory captures the decision.", ""},
		{"dangling slash", `partial\`, ""},
		{"dangling quote", `partial\"`, ""},
		{"valid markdown", validMarkdown, validMarkdown},
		{"valid chinese", validChinese, validChinese},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateGeneratedSummary(tt.raw); got != tt.want {
				t.Fatalf("validateGeneratedSummary(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseResultPartialSuccess(t *testing.T) {
	result := parseResult(`{"title":"Document","summary":"**OAuth** callback URLs were corrected."}`)
	if result.Title != "" {
		t.Fatalf("expected generic title to be dropped, got %q", result.Title)
	}
	if result.Summary != "**OAuth** callback URLs were corrected." {
		t.Fatalf("expected valid summary to survive, got %q", result.Summary)
	}
}

func TestSummarizeRetriesMalformedJSON(t *testing.T) {
	var calls atomic.Int32
	fakeAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		req := decodeAnthropicRequest(t, r)
		if req.MaxTokens != summarizeMaxTokens {
			t.Fatalf("expected max_tokens %d, got %d", summarizeMaxTokens, req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			fmt.Fprint(w, `{"content":[{"text":"Here is the JSON:\n{ \"title\" : \"Good title\", \"summary\" : \"cut off"}],"stop_reason":"max_tokens","usage":{"input_tokens":25,"output_tokens":512}}`)
			return
		}
		if !strings.Contains(req.Messages[0].Content, "Your previous response was invalid") {
			t.Fatalf("expected retry prompt, got %q", req.Messages[0].Content)
		}
		fmt.Fprint(w, `{"content":[{"text":"{\"title\":\"OAuth Redirect Fix\",\"summary\":\"**OAuth** callback URLs were corrected.\"}"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":30}}`)
	}))
	defer fakeAnthropic.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", fakeAnthropic.URL)
	s := New(anthropic.NewFromEnv())
	if s == nil {
		t.Fatal("expected summarizer")
	}

	result := s.SummarizeWithTitleContext(t.Context(), "Document", strings.Repeat("memory content ", 20), "semantic/stable", "")
	if calls.Load() != 2 {
		t.Fatalf("expected two LLM calls, got %d", calls.Load())
	}
	if result.Title != "OAuth Redirect Fix" {
		t.Fatalf("expected retry title, got %q", result.Title)
	}
	if result.Summary != "**OAuth** callback URLs were corrected." {
		t.Fatalf("expected retry summary, got %q", result.Summary)
	}
}

func TestSummarizeDropsFailedRetry(t *testing.T) {
	var calls atomic.Int32
	fakeAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"text":"{ \"title\": \"Still cut off\", \"summary\": \"broken"}],"stop_reason":"max_tokens","usage":{"input_tokens":25,"output_tokens":512}}`)
	}))
	defer fakeAnthropic.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", fakeAnthropic.URL)
	s := New(anthropic.NewFromEnv())
	if s == nil {
		t.Fatal("expected summarizer")
	}

	result := s.SummarizeWithTitleContext(t.Context(), "Document", strings.Repeat("memory content ", 20), "semantic/stable", "")
	if calls.Load() != 2 {
		t.Fatalf("expected two LLM calls, got %d", calls.Load())
	}
	if result.Title != "" || result.Summary != "" {
		t.Fatalf("expected failed retry to return empty result, got %+v", result)
	}
}

func decodeAnthropicRequest(t *testing.T, r *http.Request) struct {
	MaxTokens int `json:"max_tokens"`
	Messages  []struct {
		Content string `json:"content"`
	} `json:"messages"`
} {
	t.Helper()
	var req struct {
		MaxTokens int `json:"max_tokens"`
		Messages  []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(req.Messages))
	}
	return req
}

func TestShouldSummarize(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"short", "hello world", false},
		{"199 chars", strings.Repeat("a", 199), false},
		{"200 chars", strings.Repeat("a", 200), true},
		{"long", strings.Repeat("a", 500), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSummarize(tt.content); got != tt.want {
				t.Fatalf("ShouldSummarize(%d chars) = %v, want %v", len(tt.content), got, tt.want)
			}
		})
	}
}

func TestShouldEnrich(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		content string
		want    bool
	}{
		{"empty both", "", "", false},
		{"title only short", "Hi", "", false},
		{"title only 19 chars", "Q2 Pricing Decision", "", false}, // 19 chars < 20
		{"title only 20 chars", "Q2 Pricing Decisions", "", true}, // 20 chars >= 20
		{"content provides signal", "", "Decided to go with $9/$19 tiers", true},
		{"motivating example", "Pricing", "Decided to go with $9/$19 tiers", true}, // 39 chars combined
		{"whitespace only", "   ", "   ", false},
		{"18 chars combined", "ab", strings.Repeat("c", 15), false}, // "ab ccc...c" = 18 chars
		{"20 chars combined", "ab", strings.Repeat("c", 17), true},  // "ab ccc...c" = 20 chars
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldEnrich(tt.title, tt.content); got != tt.want {
				t.Fatalf("ShouldEnrich(%q, %q) = %v, want %v", tt.title, tt.content, got, tt.want)
			}
		})
	}
}
