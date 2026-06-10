package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// VoyageEmbedder calls the Voyage AI embeddings API.
type VoyageEmbedder struct {
	apiKey string
	model  string
	client *http.Client
}

func (e *VoyageEmbedder) Dimensions() int {
	// voyage-code-3 returns 1024-dim vectors
	return 1024
}

type voyageRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
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
		Input:     texts,
		Model:     e.model,
		InputType: inputType,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed marshal: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
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
		return nil, isRetryableStatus(resp.StatusCode), fmt.Errorf("voyage API error %d: %s", resp.StatusCode, respBody)
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
