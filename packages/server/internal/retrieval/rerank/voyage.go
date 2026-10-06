package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const voyageRerankURL = "https://api.voyageai.com/v1/rerank"

// DefaultVoyageModel is the V2 reranker (plan 25 §5.11: Voyage
// rerank-3-lite instead of Cohere, about 10× cheaper; eval-gated).
const DefaultVoyageModel = "rerank-3-lite"

// VoyageConfig configures the Voyage reranker. The key and model are read
// once at startup by the caller.
type VoyageConfig struct {
	APIKey string
	// Model defaults to DefaultVoyageModel.
	Model string
	// TopN is how many documents one call reranks (default 20).
	TopN int
	// Timeout bounds one HTTP request (default 1 s); the query path's own
	// deadline (150 ms) is shorter and wins.
	Timeout time.Duration
	// HTTPClient and URL replace the defaults (tests).
	HTTPClient *http.Client
	URL        string
}

// Voyage reranks with Voyage's rerank API. It implements Reranker.
type Voyage struct {
	apiKey string
	model  string
	topN   int
	url    string
	client *http.Client
}

// NewVoyage returns the Voyage reranker, or nil without a key (nil means
// disabled).
func NewVoyage(cfg VoyageConfig) *Voyage {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil
	}
	v := &Voyage{apiKey: cfg.APIKey, model: strings.TrimSpace(cfg.Model), topN: cfg.TopN, url: cfg.URL, client: cfg.HTTPClient}
	if v.model == "" {
		v.model = DefaultVoyageModel
	}
	if v.topN <= 0 {
		v.topN = 20
	}
	if v.url == "" {
		v.url = voyageRerankURL
	}
	if v.client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = time.Second
		}
		v.client = &http.Client{Timeout: timeout}
	}
	return v
}

// TopN implements Reranker.
func (v *Voyage) TopN() int { return v.topN }

// Model is the rerank model.
func (v *Voyage) Model() string { return v.model }

// Rerank implements Reranker: the documents by relevance, best first.
func (v *Voyage) Rerank(ctx context.Context, query string, docs []Document) ([]Result, error) {
	if v == nil || len(docs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"model":      v.model,
		"query":      query,
		"documents":  documentsContent(docs),
		"top_k":      min(v.topN, len(docs)),
		"truncation": true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal voyage rerank request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create voyage rerank request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage rerank call: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read voyage rerank response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage rerank error %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Data []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode voyage rerank response: %w", err)
	}
	out := make([]Result, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(docs) {
			continue
		}
		out = append(out, Result{ID: docs[d.Index].ID, Score: d.RelevanceScore})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	for i := range out {
		out[i].Rank = i
	}
	return out, nil
}
