// Package anthropic provides a shared client for calling the Anthropic Messages API.
// All LLM-dependent modules should use this client instead of making direct HTTP calls.
//
// Usage:
//
//	client := anthropic.NewFromEnv()
//	if client == nil { /* API key not set, LLM features disabled */ }
//
//	resp, err := client.Complete(ctx, anthropic.CompleteRequest{
//	    Model:     "claude-haiku-4-5",
//	    MaxTokens: 200,
//	    Prompt:    "Summarize this: ...",
//	})
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/analytics"
)

const (
	DefaultBaseURL = "https://api.anthropic.com"
	DefaultModel   = "claude-haiku-4-5"
	SonnetModel    = "claude-sonnet-4-6"
	apiVersion     = "2023-06-01"

	defaultCaptureContentMaxChars = 4000
)

// Client is a shared Anthropic API client.
// Create one instance and inject it into all modules that need LLM access.
type Client struct {
	apiKey                 string
	baseURL                string
	httpClient             *http.Client
	analytics              *analytics.Client
	captureContent         bool
	captureContentMaxChars int
}

type trackingContextKey struct{}

// Tracking carries non-sensitive attribution for LLM analytics.
type Tracking struct {
	DistinctID string
	TraceID    string
	SessionID  string
	ParentID   string
	Metadata   map[string]any
}

// NewFromEnv creates a Client from environment variables.
// Returns nil if ANTHROPIC_API_KEY is not set (LLM features gracefully disabled).
func NewFromEnv() *Client {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil
	}
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return newClient(key, baseURL)
}

// New constructs a Client with an explicit key + base URL. Primarily
// used by the mockllm harness so tests can run with t.Parallel()
// without fighting the process-wide env vars NewFromEnv reads.
// Production code should prefer NewFromEnv.
func New(apiKey, baseURL string) *Client {
	if apiKey == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return newClient(apiKey, baseURL)
}

func newClient(apiKey, baseURL string) *Client {
	captureContent, captureContentMaxChars := llmContentCaptureConfig()
	return &Client{
		apiKey:                 apiKey,
		baseURL:                baseURL,
		captureContent:         captureContent,
		captureContentMaxChars: captureContentMaxChars,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetAnalytics enables PostHog LLM analytics for all Anthropic calls made by this client.
func (c *Client) SetAnalytics(client *analytics.Client) {
	if c == nil {
		return
	}
	c.analytics = client
}

// WithTracking attaches LLM analytics attribution to a context.
func WithTracking(ctx context.Context, tracking Tracking) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	existing, _ := ctx.Value(trackingContextKey{}).(Tracking)
	if tracking.DistinctID == "" {
		tracking.DistinctID = existing.DistinctID
	}
	if tracking.TraceID == "" {
		tracking.TraceID = existing.TraceID
	}
	if tracking.SessionID == "" {
		tracking.SessionID = existing.SessionID
	}
	if tracking.ParentID == "" {
		tracking.ParentID = existing.ParentID
	}
	tracking.Metadata = mergeMetadata(existing.Metadata, tracking.Metadata)
	return context.WithValue(ctx, trackingContextKey{}, tracking)
}

func trackingFromContext(ctx context.Context) Tracking {
	if ctx == nil {
		return Tracking{}
	}
	tracking, _ := ctx.Value(trackingContextKey{}).(Tracking)
	return tracking
}

// CompleteRequest is the input to a single LLM completion call.
type CompleteRequest struct {
	Model      string         // required — e.g. "claude-haiku-4-5"
	MaxTokens  int            // required — max output tokens
	Prompt     string         // required — user message content
	System     string         // optional — system prompt
	Purpose    string         // optional — stable span name, e.g. "retrieval.query_distill"
	TraceID    string         // optional — groups related AI events
	SessionID  string         // optional — groups related traces
	ParentID   string         // optional — parent span ID for tree views
	DistinctID string         // optional — PostHog distinct ID, defaults to "server"
	Metadata   map[string]any // optional — extra non-sensitive properties
}

// CompleteResponse is the output of a single LLM completion call.
type CompleteResponse struct {
	Text         string // trimmed Content[0].Text
	InputTokens  int    // from usage.input_tokens
	OutputTokens int    // from usage.output_tokens
	StopReason   string // raw stop_reason from API (e.g. "max_tokens")
	RawBody      string // full JSON response body (for debugging)
}

// Complete sends a single-turn completion request to the Anthropic Messages API.
func (c *Client) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	req, budget := FitCompleteRequest(req)
	input := c.captureCompleteInput(req)
	ctxTracking := trackingFromContext(ctx)
	start := time.Now()
	tracking := llmTracking{
		purpose:     req.Purpose,
		traceID:     firstNonEmpty(req.TraceID, ctxTracking.TraceID),
		sessionID:   firstNonEmpty(req.SessionID, ctxTracking.SessionID),
		parentID:    firstNonEmpty(req.ParentID, ctxTracking.ParentID),
		distinctID:  firstNonEmpty(req.DistinctID, ctxTracking.DistinctID),
		model:       req.Model,
		baseURL:     c.baseURL,
		maxTokens:   req.MaxTokens,
		inputTokens: budget.InputTokens,
		input:       input,
		metadata:    mergeMetadata(ctxTracking.Metadata, req.Metadata),
	}

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": req.Prompt},
		},
	}
	if req.System != "" {
		body["system"] = req.System
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("marshal request: %w", err)))
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("create request: %w", err)))
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("anthropic API call: %w", err)))
		return nil, fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("read response: %w", err)))
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("anthropic API error %d", resp.StatusCode)))
		return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, respBody)
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("decode response: %w", err)))
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(apiResp.Content) == 0 {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("empty response from Anthropic")))
		return nil, fmt.Errorf("empty response from Anthropic")
	}
	if apiResp.Usage.InputTokens > 0 {
		tracking.inputTokens = apiResp.Usage.InputTokens
	}
	tracking.outputTokens = apiResp.Usage.OutputTokens
	tracking.output = c.captureOutput(apiResp.Content[0].Text)
	c.trackLLMGeneration(tracking.withSuccess(start, resp.StatusCode))

	return &CompleteResponse{
		Text:         strings.TrimSpace(apiResp.Content[0].Text),
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
		StopReason:   apiResp.StopReason,
		RawBody:      string(respBody),
	}, nil
}

// CompleteMessages sends a request with arbitrary messages (for multimodal content like images/PDFs).
// Use this when you need image or document content blocks instead of a plain text prompt.
func (c *Client) CompleteMessages(ctx context.Context, model string, maxTokens int, messages []map[string]any, purpose ...string) (*CompleteResponse, error) {
	if model == "" {
		model = DefaultModel
	}
	messages, budget := FitMessages(model, maxTokens, messages)
	input := c.captureMessagesInput(messages)
	ctxTracking := trackingFromContext(ctx)
	start := time.Now()
	tracking := llmTracking{
		purpose:     firstString(purpose),
		traceID:     ctxTracking.TraceID,
		sessionID:   ctxTracking.SessionID,
		parentID:    ctxTracking.ParentID,
		distinctID:  ctxTracking.DistinctID,
		model:       model,
		baseURL:     c.baseURL,
		maxTokens:   maxTokens,
		inputTokens: budget.TextTokens + budget.ReservedNonTextTokens,
		input:       input,
		metadata: mergeMetadata(ctxTracking.Metadata, map[string]any{
			"message_count":            budget.MessageCount,
			"text_block_count":         budget.TextBlockCount,
			"document_block_count":     budget.DocumentBlockCount,
			"image_block_count":        budget.ImageBlockCount,
			"other_block_count":        budget.OtherBlockCount,
			"trimmed_text_blocks":      budget.TrimmedTextBlocks,
			"approx_payload_bytes":     budget.ApproxPayloadBytes,
			"reserved_non_text_tokens": budget.ReservedNonTextTokens,
		}),
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   messages,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("marshal request: %w", err)))
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("create request: %w", err)))
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("anthropic API call: %w", err)))
		return nil, fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("read response: %w", err)))
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("anthropic API error %d", resp.StatusCode)))
		return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, respBody)
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("decode response: %w", err)))
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(apiResp.Content) == 0 {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("empty response from Anthropic")))
		return nil, fmt.Errorf("empty response from Anthropic")
	}
	if apiResp.Usage.InputTokens > 0 {
		tracking.inputTokens = apiResp.Usage.InputTokens
	}
	tracking.outputTokens = apiResp.Usage.OutputTokens
	tracking.output = c.captureOutput(apiResp.Content[0].Text)
	c.trackLLMGeneration(tracking.withSuccess(start, resp.StatusCode))

	return &CompleteResponse{
		Text:         strings.TrimSpace(apiResp.Content[0].Text),
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
		StopReason:   apiResp.StopReason,
		RawBody:      string(respBody),
	}, nil
}

// CompleteStream sends a streaming completion request. It calls onDelta for each text chunk
// as it arrives from the Anthropic API. Returns the total output token count.
func (c *Client) CompleteStream(ctx context.Context, req CompleteRequest, onDelta func(text string)) (int, error) {
	req, budget := FitCompleteRequest(req)
	input := c.captureCompleteInput(req)
	ctxTracking := trackingFromContext(ctx)
	start := time.Now()
	tracking := llmTracking{
		purpose:     req.Purpose,
		traceID:     firstNonEmpty(req.TraceID, ctxTracking.TraceID),
		sessionID:   firstNonEmpty(req.SessionID, ctxTracking.SessionID),
		parentID:    firstNonEmpty(req.ParentID, ctxTracking.ParentID),
		distinctID:  firstNonEmpty(req.DistinctID, ctxTracking.DistinctID),
		model:       req.Model,
		baseURL:     c.baseURL,
		maxTokens:   req.MaxTokens,
		inputTokens: budget.InputTokens,
		input:       input,
		stream:      true,
		metadata:    mergeMetadata(ctxTracking.Metadata, req.Metadata),
	}

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": req.Prompt},
		},
	}
	if req.System != "" {
		body["system"] = req.System
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("marshal request: %w", err)))
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("create request: %w", err)))
		return 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.trackLLMGeneration(tracking.withError(start, 0, fmt.Errorf("anthropic API call: %w", err)))
		return 0, fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, fmt.Errorf("anthropic API error %d", resp.StatusCode)))
		return 0, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, respBody)
	}

	// Parse SSE stream from Anthropic
	scanner := bufio.NewScanner(resp.Body)
	outputTokens := 0
	var firstTokenAt time.Time
	var capturedOutput strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Text != "" {
				if firstTokenAt.IsZero() {
					firstTokenAt = time.Now()
				}
				c.captureStreamDelta(&capturedOutput, event.Delta.Text)
				onDelta(event.Delta.Text)
			}
		case "message_delta":
			if event.Usage.OutputTokens > 0 {
				outputTokens = event.Usage.OutputTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		c.trackLLMGeneration(tracking.withError(start, resp.StatusCode, err))
		return outputTokens, err
	}
	tracking.outputTokens = outputTokens
	if !firstTokenAt.IsZero() {
		tracking.timeToFirstToken = firstTokenAt.Sub(start).Seconds()
	}
	tracking.output = c.captureOutput(capturedOutput.String())
	c.trackLLMGeneration(tracking.withSuccess(start, resp.StatusCode))
	return outputTokens, nil
}

type llmTracking struct {
	purpose          string
	traceID          string
	sessionID        string
	parentID         string
	distinctID       string
	model            string
	baseURL          string
	maxTokens        int
	inputTokens      int
	outputTokens     int
	input            any
	output           any
	stream           bool
	timeToFirstToken float64
	metadata         map[string]any
}

func (t llmTracking) withSuccess(start time.Time, status int) map[string]any {
	return t.properties(start, status, nil)
}

func (t llmTracking) withError(start time.Time, status int, err error) map[string]any {
	return t.properties(start, status, err)
}

func (t llmTracking) properties(start time.Time, status int, err error) map[string]any {
	purpose := strings.TrimSpace(t.purpose)
	if purpose == "" {
		purpose = "anthropic.unknown"
	}
	traceID := strings.TrimSpace(t.traceID)
	if traceID == "" {
		traceID = newTraceID()
	}
	distinctID := strings.TrimSpace(t.distinctID)
	if distinctID == "" {
		distinctID = "server"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(t.baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	props := map[string]any{
		"$ai_trace_id":      traceID,
		"$ai_span_id":       newTraceID(),
		"$ai_span_name":     purpose,
		"$ai_model":         t.model,
		"$ai_provider":      "anthropic",
		"$ai_input_tokens":  t.inputTokens,
		"$ai_output_tokens": t.outputTokens,
		"$ai_latency":       time.Since(start).Seconds(),
		"$ai_http_status":   status,
		"$ai_base_url":      baseURL,
		"$ai_request_url":   baseURL + "/v1/messages",
		"$ai_max_tokens":    t.maxTokens,
		"$ai_stream":        t.stream,
		"llm_purpose":       purpose,
		"distinct_id":       distinctID,
	}
	if t.timeToFirstToken > 0 {
		props["$ai_time_to_first_token"] = t.timeToFirstToken
	}
	if t.input != nil {
		props["$ai_input"] = t.input
	}
	if t.output != nil {
		props["$ai_output_choices"] = t.output
	}
	if t.sessionID != "" {
		props["$ai_session_id"] = t.sessionID
	}
	if t.parentID != "" {
		props["$ai_parent_id"] = t.parentID
	}
	if err != nil {
		props["$ai_is_error"] = true
		props["$ai_error"] = err.Error()
	} else {
		props["$ai_is_error"] = false
	}
	for k, v := range t.metadata {
		props[k] = v
	}
	return props
}

func (c *Client) captureCompleteInput(req CompleteRequest) any {
	if !c.shouldCaptureContent() {
		return nil
	}
	messages := make([]map[string]any, 0, 2)
	if req.System != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": c.truncateAndRedact(req.System),
		})
	}
	messages = append(messages, map[string]any{
		"role":    "user",
		"content": c.truncateAndRedact(req.Prompt),
	})
	return messages
}

func (c *Client) captureMessagesInput(messages []map[string]any) any {
	if !c.shouldCaptureContent() {
		return nil
	}
	return sanitizeForAnalytics(messages, c.captureContentMaxChars)
}

func (c *Client) captureOutput(text string) any {
	if !c.shouldCaptureContent() || text == "" {
		return nil
	}
	return []map[string]any{{
		"role":    "assistant",
		"content": c.truncateAndRedact(text),
	}}
}

func (c *Client) captureStreamDelta(sb *strings.Builder, text string) {
	if !c.shouldCaptureContent() || text == "" {
		return
	}
	limit := c.captureContentMaxChars
	if limit <= 0 {
		limit = defaultCaptureContentMaxChars
	}
	if sb.Len() >= limit {
		return
	}
	remaining := limit - sb.Len()
	if len(text) > remaining {
		sb.WriteString(text[:remaining])
		sb.WriteString("\n\n[truncated]")
		return
	}
	sb.WriteString(text)
}

func (c *Client) shouldCaptureContent() bool {
	return c != nil && c.captureContent
}

func (c *Client) truncateAndRedact(text string) string {
	return truncateAndRedact(text, c.captureContentMaxChars)
}

func llmContentCaptureConfig() (bool, int) {
	maxChars := defaultCaptureContentMaxChars
	if raw := strings.TrimSpace(os.Getenv("POSTHOG_LLM_CAPTURE_CONTENT_MAX_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxChars = parsed
		}
	}

	if raw := strings.TrimSpace(os.Getenv("POSTHOG_LLM_CAPTURE_CONTENT")); raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			return true, maxChars
		case "0", "false", "no", "off":
			return false, maxChars
		}
	}

	env := strings.ToLower(strings.TrimSpace(os.Getenv("MEMAX_ENV")))
	return env == "" || env == "dev" || env == "development" || env == "staging", maxChars
}

func sanitizeForAnalytics(value any, maxChars int) any {
	switch typed := value.(type) {
	case string:
		return truncateAndRedact(typed, maxChars)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeMapForAnalytics(item, maxChars))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeForAnalytics(item, maxChars))
		}
		return out
	case map[string]any:
		return sanitizeMapForAnalytics(typed, maxChars)
	default:
		return typed
	}
}

func sanitizeMapForAnalytics(input map[string]any, maxChars int) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		key := strings.ToLower(k)
		if key == "data" {
			out[k] = "[redacted binary payload]"
			continue
		}
		if key == "source" {
			if source, ok := v.(map[string]any); ok {
				out[k] = sanitizeSourceForAnalytics(source, maxChars)
				continue
			}
			if source, ok := v.(map[string]string); ok {
				generic := make(map[string]any, len(source))
				for sk, sv := range source {
					generic[sk] = sv
				}
				out[k] = sanitizeSourceForAnalytics(generic, maxChars)
				continue
			}
		}
		out[k] = sanitizeForAnalytics(v, maxChars)
	}
	return out
}

func sanitizeSourceForAnalytics(source map[string]any, maxChars int) map[string]any {
	out := make(map[string]any, len(source))
	for k, v := range source {
		if strings.EqualFold(k, "data") {
			out[k] = "[redacted binary payload]"
			continue
		}
		out[k] = sanitizeForAnalytics(v, maxChars)
	}
	return out
}

func truncateAndRedact(text string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = defaultCaptureContentMaxChars
	}
	text = redactSensitiveText(text)
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "\n\n[truncated]"
}

func redactSensitiveText(text string) string {
	for _, re := range sensitiveValueRegexps {
		text = re.ReplaceAllString(text, "$1[redacted]")
	}
	for _, re := range standaloneSecretRegexps {
		text = re.ReplaceAllString(text, "[redacted]")
	}
	return text
}

var sensitiveValueRegexps = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|authorization)\b\s*[:=]\s*([^\s,"']+)`),
	regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]+`),
}

var standaloneSecretRegexps = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}`),
}

func (c *Client) trackLLMGeneration(props map[string]any) {
	if c == nil || c.analytics == nil {
		return
	}
	distinctID, _ := props["distinct_id"].(string)
	if distinctID == "" {
		distinctID = "server"
	}
	delete(props, "distinct_id")
	c.analytics.Track(distinctID, "$ai_generation", props)
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mergeMetadata(base map[string]any, overrides map[string]any) map[string]any {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := make(map[string]any, len(base)+len(overrides))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

func newTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ModelFromEnv reads a model name from an environment variable, falling back to the default.
func ModelFromEnv(envVar string) string {
	if m := os.Getenv(envVar); m != "" {
		return m
	}
	return DefaultModel
}

// ModelFromEnvWithDefault reads a model name from an environment variable with a custom fallback.
func ModelFromEnvWithDefault(envVar, fallback string) string {
	if m := os.Getenv(envVar); m != "" {
		return m
	}
	return fallback
}

// --- Text extraction utilities ---
// These help parse LLM responses that may be wrapped in markdown code fences.

var jsonObjectRe = regexp.MustCompile(`(?s)` + "```" + `(?:json)?\s*(\{.*?\})\s*` + "```")
var jsonArrayRe = regexp.MustCompile(`(?s)` + "```" + `(?:json)?\s*(\[.*?\])\s*` + "```")

// ExtractJSONObject extracts a JSON object from text that may be wrapped in markdown fences.
func ExtractJSONObject(text string) string {
	if m := jsonObjectRe.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	// Try raw: find first { to last }
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

// ExtractJSONArray extracts a JSON array from text that may be wrapped in markdown fences.
func ExtractJSONArray(text string) string {
	if m := jsonArrayRe.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	// Try raw: find first [ to last ]
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

// StripMarkdownFences removes ```json ... ``` wrapping from text.
func StripMarkdownFences(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.SplitN(text, "\n", 2)
		if len(lines) == 2 {
			text = lines[1]
		}
	}
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
