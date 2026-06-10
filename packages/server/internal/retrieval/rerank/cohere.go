package rerank

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/cache"
)

const cohereBaseURL = "https://api.cohere.com/v2/rerank"

type Cohere struct {
	apiKey     string
	model      string
	topN       int
	cacheTTL   time.Duration
	cache      cache.Cache
	httpClient *http.Client
}

func (c *Cohere) TopN() int { return c.topN }

func (c *Cohere) Rerank(ctx context.Context, query string, docs []Document) ([]Result, error) {
	if c == nil || len(docs) == 0 {
		return nil, nil
	}
	if cached, ok := c.getCached(ctx, query, docs); ok {
		return cached, nil
	}

	payload := map[string]any{
		"model":            c.model,
		"query":            query,
		"top_n":            min(c.topN, len(docs)),
		"documents":        documentsContent(docs),
		"return_documents": false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cohereBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cohere rerank call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rerank response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cohere rerank error %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	results := make([]Result, 0, len(parsed.Results))
	for rank, item := range parsed.Results {
		if item.Index < 0 || item.Index >= len(docs) {
			continue
		}
		results = append(results, Result{ID: docs[item.Index].ID, Score: item.RelevanceScore, Rank: rank})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	c.setCached(ctx, query, docs, results)
	return results, nil
}

func (c *Cohere) getCached(ctx context.Context, query string, docs []Document) ([]Result, bool) {
	if c.cache == nil {
		return nil, false
	}
	key := c.cacheKey(query, docs)
	payload, err := c.cache.Get(ctx, key)
	if err != nil {
		if err == redis.Nil || ctx.Err() != nil {
			slog.Debug("rerank cache miss", "key", key, "query", query, "doc_count", len(docs))
			return nil, false
		}
		slog.Warn("rerank cache read failed", "key", key, "query", query, "doc_count", len(docs), "error", err)
		return nil, false
	}
	var results []Result
	if err := json.Unmarshal([]byte(payload), &results); err != nil {
		slog.Warn("rerank cache decode failed", "key", key, "error", err)
		return nil, false
	}
	slog.Info("rerank cache hit", "key", key, "query", query, "doc_count", len(docs), "result_count", len(results))
	return results, true
}

func (c *Cohere) setCached(ctx context.Context, query string, docs []Document, results []Result) {
	if c.cache == nil || c.cacheTTL <= 0 || ctx.Err() != nil {
		return
	}
	body, err := json.Marshal(results)
	if err != nil {
		return
	}
	key := c.cacheKey(query, docs)
	if err := c.cache.Set(ctx, key, string(body), c.cacheTTL); err != nil {
		slog.Warn("rerank cache store failed", "key", key, "query", query, "doc_count", len(docs), "error", err)
		return
	}
	slog.Info("rerank cache stored", "key", key, "query", query, "doc_count", len(docs), "ttl_seconds", int(c.cacheTTL.Seconds()), "result_count", len(results))
}

func (c *Cohere) cacheKey(query string, docs []Document) string {
	var b strings.Builder
	b.WriteString(c.model)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%d\n", c.topN))
	b.WriteString(query)
	for _, doc := range docs {
		b.WriteString("\n")
		b.WriteString(doc.ID)
		b.WriteString("\n")
		b.WriteString(doc.Content)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("memax:rerank:%x", sum)
}

func documentsContent(docs []Document) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Content
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
