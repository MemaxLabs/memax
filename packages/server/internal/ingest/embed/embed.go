package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	voyageEndpoint        = "https://api.voyageai.com/v1/embeddings"
	defaultRequestTimeout = 30 * time.Second
	defaultBatchSize      = 64
	maxResponseErrorBytes = 4096
	maxAttempts           = 3
)

// Embedder generates vector embeddings for text.
type Embedder interface {
	Embed(texts []string, inputType string) ([][]float64, error)
	EmbedContext(ctx context.Context, texts []string, inputType string) ([][]float64, error)
	Dimensions() int
}

// NewEmbedder returns a Voyage AI embedder if VOYAGE_API_KEY is set,
// otherwise returns nil (caller should fall back to keyword search).
func NewEmbedder() Embedder {
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		return nil
	}
	model := os.Getenv("VOYAGE_MODEL")
	if model == "" {
		model = "voyage-code-3"
	}
	return &VoyageEmbedder{
		apiKey: key,
		model:  model,
		client: &http.Client{Timeout: defaultRequestTimeout},
	}
}

// VoyageConfig is an explicit Voyage embedder: the key and model are
// read once at startup by the caller (the V2 index and query models,
// V2_EMBED_MODEL and V2_EMBED_QUERY_MODEL), never from the environment
// inside the package.
type VoyageConfig struct {
	APIKey string
	// Model is the Voyage model, e.g. voyage-4 (documents) or
	// voyage-4-lite (queries), which share one embedding space.
	Model string
	// OutputDimension asks for vectors of this width (voyage-4 supports
	// 256, 512, 1024 and 2048). 0 leaves the model's default.
	OutputDimension int
	// MaxAttempts bounds the tries of one batch, for 429 and 5xx (0 is 3).
	// The query path uses 1: a retry can't fit its deadline.
	MaxAttempts int
	// Timeout bounds one HTTP request (0 is 30 s); a context deadline
	// shorter than it wins.
	Timeout time.Duration
	// HTTPClient replaces the default client (tests).
	HTTPClient *http.Client
}

// NewVoyage returns a Voyage embedder for cfg, or nil when it has no key
// or no model (nil means disabled).
func NewVoyage(cfg VoyageConfig) Embedder {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultRequestTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	return &VoyageEmbedder{
		apiKey: cfg.APIKey, model: strings.TrimSpace(cfg.Model), client: client,
		outputDimension: cfg.OutputDimension, maxAttempts: cfg.MaxAttempts,
	}
}

// RateLimitError is Voyage answering 429 on the last attempt. RetryAfter
// is its Retry-After header, or 0 when it sent none.
type RateLimitError struct {
	RetryAfter time.Duration
	Err        error
}

func (e *RateLimitError) Error() string { return e.Err.Error() }
func (e *RateLimitError) Unwrap() error { return e.Err }

// IsRateLimited reports whether err is a RateLimitError, and its wait.
func IsRateLimited(err error) (time.Duration, bool) {
	var rl *RateLimitError
	if errors.As(err, &rl) {
		return rl.RetryAfter, true
	}
	return 0, false
}

// VoyageEmbedder calls the Voyage AI embeddings API.
type VoyageEmbedder struct {
	apiKey string
	model  string
	client *http.Client
	// outputDimension is sent as output_dimension when set.
	outputDimension int
	// maxAttempts overrides maxAttempts when set.
	maxAttempts int
}

func (e *VoyageEmbedder) Dimensions() int {
	if e.outputDimension > 0 {
		return e.outputDimension
	}
	// voyage-code-3 returns 1024-dim vectors
	return 1024
}

// Model is the Voyage model this embedder calls.
func (e *VoyageEmbedder) Model() string { return e.model }

type voyageRequest struct {
	Input           []string `json:"input"`
	Model           string   `json:"model"`
	InputType       string   `json:"input_type,omitempty"`
	OutputDimension int      `json:"output_dimension,omitempty"`
}

type voyageResponse struct {
	Data  []voyageData `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type voyageData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

func (e *VoyageEmbedder) Embed(texts []string, inputType string) ([][]float64, error) {
	return e.EmbedContext(context.Background(), texts, inputType)
}

func (e *VoyageEmbedder) EmbedContext(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if inputType == "" {
		inputType = "document"
	}

	embeddings := make([][]float64, len(texts))
	for start := 0; start < len(texts); start += defaultBatchSize {
		end := start + defaultBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batchEmbeddings, err := e.embedBatch(ctx, texts[start:end], inputType)
		if err != nil {
			return nil, err
		}
		for i, embedding := range batchEmbeddings {
			if start+i < len(embeddings) {
				embeddings[start+i] = embedding
			}
		}
	}
	return embeddings, nil
}

func (e *VoyageEmbedder) embedBatch(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	reqBody := voyageRequest{
		Input:           texts,
		Model:           e.model,
		InputType:       inputType,
		OutputDimension: e.outputDimension,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed marshal: %w", err)
	}

	attempts := maxAttempts
	if e.maxAttempts > 0 {
		attempts = e.maxAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := sleepBeforeRetry(ctx, attempt); err != nil {
				return nil, err
			}
		}

		embeddings, retryable, err := e.doEmbedBatch(ctx, body, len(texts))
		if err == nil {
			return embeddings, nil
		}
		lastErr = err
		if !retryable {
			break
		}
	}
	return nil, lastErr
}

func (e *VoyageEmbedder) doEmbedBatch(ctx context.Context, body []byte, count int) ([][]float64, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	client := e.client
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("embed call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseErrorBytes))
		err := fmt.Errorf("voyage API error %d: %s", resp.StatusCode, respBody)
		if resp.StatusCode == http.StatusTooManyRequests {
			err = &RateLimitError{RetryAfter: retryAfter(resp.Header.Get("Retry-After")), Err: err}
		}
		return nil, isRetryableStatus(resp.StatusCode), err
	}

	var voyageResp voyageResponse
	if err := json.NewDecoder(resp.Body).Decode(&voyageResp); err != nil {
		return nil, false, fmt.Errorf("embed decode: %w", err)
	}

	embeddings := make([][]float64, count)
	for _, d := range voyageResp.Data {
		if d.Index >= 0 && d.Index < len(embeddings) {
			embeddings[d.Index] = d.Embedding
		}
	}
	return embeddings, false, nil
}

// retryAfter parses a Retry-After header in seconds (the form Voyage
// sends); anything else is 0.
func retryAfter(h string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

func isRetryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt-1) * time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
