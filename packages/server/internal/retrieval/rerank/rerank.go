package rerank

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/cache"
)

const (
	defaultModel     = "rerank-v4.0-fast"
	defaultTopN      = 15
	defaultTimeoutMs = 1000
	defaultCacheTTL  = 15 * time.Minute
)

type Document struct {
	ID      string
	Content string
}

type Result struct {
	ID    string
	Score float64
	Rank  int
}

type Reranker interface {
	TopN() int
	Rerank(ctx context.Context, query string, docs []Document) ([]Result, error)
}

type Config struct {
	Model    string
	TopN     int
	Timeout  time.Duration
	CacheTTL time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		Model:    envString("RERANK_MODEL", defaultModel),
		TopN:     envInt("RERANK_TOP_N", defaultTopN),
		Timeout:  time.Duration(envInt("RERANK_TIMEOUT_MS", defaultTimeoutMs)) * time.Millisecond,
		CacheTTL: time.Duration(envInt("RERANK_CACHE_TTL_SECONDS", int(defaultCacheTTL.Seconds()))) * time.Second,
	}
}

func NewCohereFromEnv(httpClient *http.Client, cacheClient cache.Cache) *Cohere {
	apiKey := os.Getenv("COHERE_API_KEY")
	if apiKey == "" {
		return nil
	}
	cfg := ConfigFromEnv()
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &Cohere{
		apiKey:     apiKey,
		model:      cfg.Model,
		topN:       cfg.TopN,
		cacheTTL:   cfg.CacheTTL,
		cache:      cacheClient,
		httpClient: httpClient,
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
