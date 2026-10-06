package v2recall

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The vector side of V2 retrieval (plan 25 §5.11): the query embedding,
// the exact scoped KNN over memory embeddings (ledger.Nearest), Remember's
// draft embedding (§5.8) and the judge's vector candidates.
//
// The query is embedded while the lexical lanes run. If it misses
// QueryDeadline (120 ms) the answer is lexical only, and Result.Retrieval
// says so (MCP puts it in _meta). The LLM query distiller is never on
// this path.

// The defaults. The floors are uncalibrated until the retrieval and judge
// evals run on Voyage embeddings (no keys on the machine that wrote this).
const (
	// DefaultQueryDeadline is how long a recall waits for its query
	// embedding before answering lexically (§5.11).
	DefaultQueryDeadline = 120 * time.Millisecond
	// DefaultVectorFloor is the least cosine similarity the recall and
	// search vector lane keeps: a nonsense query must not bring back a
	// space's nearest memories as if they matched (precision over
	// recall).
	DefaultVectorFloor = 0.30
	// DefaultNearDuplicateFloor is the least similarity at which Remember
	// offers a memory as the same thing as the draft. It errs high: a
	// wrong offer costs the person a second look, a missed one only a
	// repeat the judge or Dream folds later.
	DefaultNearDuplicateFloor = 0.90
)

// VectorConfig configures the vector side.
type VectorConfig struct {
	// Model is the index model whose embeddings are searched
	// (V2_EMBED_MODEL). The query model must share its embedding space.
	Model string
	// QueryDeadline bounds the query embedding on recall and search.
	QueryDeadline time.Duration
	// Floor is the vector lane's least similarity (0 is DefaultVectorFloor,
	// a negative one keeps every neighbour).
	Floor float64
	// NearDuplicateFloor is the least similarity of a near repeat.
	NearDuplicateFloor float64
	// Log receives deadline misses and errors.
	Log *slog.Logger
}

// VectorConfigFromEnv reads the floors through lookup (os.LookupEnv),
// once, in a composition root; model is the index model.
//
//	V2_RECALL_VECTOR_FLOOR   least similarity the recall and search vector lane keeps (default 0.30)
//	V2_NEAR_DUPLICATE_FLOOR  least similarity Remember's check calls a near repeat (default 0.90)
func VectorConfigFromEnv(lookup func(string) (string, bool), model string) VectorConfig {
	c := VectorConfig{Model: model, QueryDeadline: DefaultQueryDeadline, Floor: DefaultVectorFloor,
		NearDuplicateFloor: DefaultNearDuplicateFloor}
	floor := func(key string, def float64) float64 {
		v, ok := lookup(key)
		if !ok {
			return def
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || f < 0 || f > 1 {
			return def
		}
		return f
	}
	c.Floor = floor("V2_RECALL_VECTOR_FLOOR", c.Floor)
	c.NearDuplicateFloor = floor("V2_NEAR_DUPLICATE_FLOOR", c.NearDuplicateFloor)
	return c
}

// Vectors embeds queries, drafts and statements, and finds memories near
// them.
type Vectors struct {
	ledger *ledger.Ledger
	// query embeds queries and drafts (voyage-4-lite); document embeds a
	// statement the judge compares (voyage-4), when its stored vector
	// isn't there yet.
	query, document embed.Embedder
	cfg             VectorConfig
	cache           *vectorCache
}

// NewVectors returns the vector side, or nil when the ledger, the query
// embedder or the model is missing (nil means lexical only). document may
// be nil: the query embedder then embeds statements too.
func NewVectors(l *ledger.Ledger, query, document embed.Embedder, cfg VectorConfig) *Vectors {
	if l == nil || query == nil || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	if cfg.QueryDeadline <= 0 {
		cfg.QueryDeadline = DefaultQueryDeadline
	}
	if cfg.Floor == 0 {
		cfg.Floor = DefaultVectorFloor // a negative floor keeps every neighbour
	}
	if cfg.NearDuplicateFloor <= 0 {
		cfg.NearDuplicateFloor = DefaultNearDuplicateFloor
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if document == nil {
		document = query
	}
	return &Vectors{ledger: l, query: query, document: document, cfg: cfg, cache: newVectorCache(1024, 10*time.Minute)}
}

// Model is the index model the vectors are searched in.
func (v *Vectors) Model() string { return v.cfg.Model }

// NearDuplicateFloor is the least similarity of a near repeat.
func (v *Vectors) NearDuplicateFloor() float64 { return v.cfg.NearDuplicateFloor }

// embed embeds one text with e, through the cache.
func (v *Vectors) embed(ctx context.Context, e embed.Embedder, text, inputType string) ([]float32, error) {
	key := cacheKey(v.cfg.Model, inputType, text)
	if vec, ok := v.cache.get(key); ok {
		return vec, nil
	}
	out, err := e.EmbedContext(ctx, []string{text}, inputType)
	if err != nil {
		return nil, err
	}
	if len(out) != 1 || len(out[0]) != ledger.EmbeddingDimensions {
		return nil, fmt.Errorf("v2recall: the embedder returned %d vector(s) of %d dimensions, want 1 of %d",
			len(out), firstLen(out), ledger.EmbeddingDimensions)
	}
	vec := make([]float32, len(out[0]))
	for i, x := range out[0] {
		vec[i] = float32(x)
	}
	v.cache.put(key, vec)
	return vec, nil
}

func firstLen(v [][]float64) int {
	if len(v) == 0 {
		return 0
	}
	return len(v[0])
}

// pendingQuery is a query embedding on its way.
type pendingQuery struct {
	done     chan struct{}
	vec      []float32
	err      error
	took     time.Duration
	start    time.Time
	deadline time.Time
	parent   context.Context
}

// startQuery embeds a query in the background, within QueryDeadline from
// now.
func (v *Vectors) startQuery(ctx context.Context, text string) *pendingQuery {
	p := &pendingQuery{done: make(chan struct{}), start: time.Now(), parent: ctx}
	p.deadline = p.start.Add(v.cfg.QueryDeadline)
	dctx, cancel := context.WithDeadline(ctx, p.deadline)
	go func() {
		defer close(p.done)
		defer cancel()
		p.vec, p.err = v.embed(dctx, v.query, text, "query")
		p.took = time.Since(p.start)
	}()
	return p
}

// wait returns the embedding, or why there is none: timeout when it missed
// its deadline (or the caller's ran out), error otherwise, with the
// embedder's error when it answered. It never waits past the deadline,
// even for an embedder that ignores its context; the goroutine's fields
// are read only once it is done.
func (p *pendingQuery) wait() ([]float32, string, time.Duration, error) {
	timer := time.NewTimer(time.Until(p.deadline))
	defer timer.Stop()
	select {
	case <-p.done:
	case <-timer.C:
		return nil, StageTimeout, time.Since(p.start), context.DeadlineExceeded
	case <-p.parent.Done():
		return nil, StageTimeout, time.Since(p.start), p.parent.Err()
	}
	switch {
	case p.err == nil:
		return p.vec, StageOK, p.took, nil
	case errors.Is(p.err, context.DeadlineExceeded) || errors.Is(p.err, context.Canceled):
		return nil, StageTimeout, p.took, p.err
	}
	return nil, StageError, p.took, p.err
}

// EmbedDraft embeds Remember's draft for the near-duplicate check, within
// QueryDeadline. A draft is compared with statements, document to
// document, so it is embedded as a document, with the query model.
func (v *Vectors) EmbedDraft(ctx context.Context, statement string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, v.cfg.QueryDeadline)
	defer cancel()
	return v.embed(ctx, v.query, strings.TrimSpace(statement), "document")
}

// Similar implements judge.Vectors: the k kept memories of the space
// nearest to the memory being judged, from its stored embedding (the
// index job usually wrote it with the proposal), or from the statement
// embedded now when it isn't there yet. The judge applies its floor.
func (v *Vectors) Similar(ctx context.Context, scope ledger.Scope, spaceID, memoryID uuid.UUID, statement string, k int) ([]ledger.JudgeCandidate, error) {
	vec, ok, err := v.ledger.StoredEmbedding(ctx, scope, memoryID, v.cfg.Model)
	if err != nil {
		return nil, err
	}
	if !ok {
		if vec, err = v.embed(ctx, v.document, statement, "document"); err != nil {
			return nil, fmt.Errorf("v2recall: embed the statement for the judge: %w", err)
		}
	}
	return v.ledger.VectorCandidates(ctx, scope, spaceID, memoryID, v.cfg.Model, vec, k)
}

// vectorCache keeps recent embeddings: an agent's repeated queries and a
// person's draft as they type and pause don't cost a request each.
type vectorCache struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	items map[[32]byte]cached
	order [][32]byte
}

type cached struct {
	vec []float32
	at  time.Time
}

func newVectorCache(max int, ttl time.Duration) *vectorCache {
	return &vectorCache{max: max, ttl: ttl, items: map[[32]byte]cached{}}
}

func cacheKey(model, inputType, text string) [32]byte {
	return sha256.Sum256([]byte(model + "\x00" + inputType + "\x00" + text))
}

func (c *vectorCache) get(k [32]byte) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[k]
	if !ok || time.Since(it.at) > c.ttl {
		return nil, false
	}
	return it.vec, true
}

func (c *vectorCache) put(k [32]byte, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[k]; !ok {
		c.order = append(c.order, k)
	}
	c.items[k] = cached{vec: vec, at: time.Now()}
	for len(c.order) > c.max {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
}
