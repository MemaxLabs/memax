// Package v2index embeds V2 memory versions (plan 25 §5.11): the
// index_memory River job the ledger enqueues in the transaction of every
// command that writes a searchable version, and the sweep that finds
// versions no job covered.
//
// # What is embedded
//
// The current version of every proposed and kept memory, whole (a
// statement is short), with the index model (voyage-4, input type
// document). Proposals are embedded as well as kept memories: Remember's
// near-duplicate check offers an agent's pending proposal instead of a
// second copy, and the judge reuses a proposal's vector.
//
// # Batching, idempotence, backoff
//
//   - One job embeds its own version and up to Batch−1 more of its space
//     that wait (ledger.IndexWork), in one embedder request, so a burst of
//     proposals costs one request; the burst's other jobs then find their
//     version done. Jobs of one space take turns in a process, so two
//     never send the same batch.
//   - A version is embedded once per model: storing is ON CONFLICT DO
//     NOTHING, and only while the version is current and its words exist
//     (ledger.StoreEmbeddings), so a job racing an edit or a Forget writes
//     nothing stale.
//   - A rate limit (429) snoozes the job for Voyage's Retry-After, which
//     spends no attempt; other errors retry with River's backoff (10
//     attempts, a few hours), and the sweep takes over after that.
//
// # The sweep, not a backfill command
//
// Every SweepInterval the sweep queues an index job for each proposed or
// kept version with no embedding of the index model, across spaces
// (ledger.UnindexedVersions, ids only). It covers, with no one having to
// remember a command: memories written before this existed or while
// embeddings were off (no key), a model switch (V2_EMBED_MODEL changes:
// the new model's rows fill in while queries keep the old one until the
// switch), and jobs that ran out of attempts. index_memory's uniqueness
// keeps a version from getting a second job while one waits.
//
// # Nil means disabled
//
// Without VOYAGE_API_KEY or with V2_EMBED_MODEL=off, New returns nil, the
// ledger enqueues no index jobs, jobs that exist cancel with the reason,
// the sweep doesn't run, and V2 recall and search stay lexical.
package v2index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The defaults (plan 25 §5.11: voyage-4 to index, voyage-4-lite to query;
// they share one embedding space). Both are eval-gated: the retrieval
// eval must not regress before they ship.
const (
	DefaultIndexModel    = "voyage-4"
	DefaultQueryModel    = "voyage-4-lite"
	DefaultBatch         = 64
	DefaultSweepInterval = 5 * time.Minute
	// MaxWorkers is the index queue's concurrency in the worker: the jobs
	// wait on Voyage, and batching makes few of them do the work.
	MaxWorkers = 4
	// sweepBatch bounds the versions one sweep queues.
	sweepBatch = 1000
)

// Config is the V2 embedding configuration, read once at startup.
type Config struct {
	// APIKey is Voyage's (VOYAGE_API_KEY). Empty disables embeddings.
	APIKey string
	// IndexModel embeds memory versions; empty disables embeddings.
	IndexModel string
	// QueryModel embeds queries and Remember's drafts. It must share the
	// index model's embedding space (voyage-4-lite does voyage-4's).
	QueryModel string
	// Batch is the most versions one embedder request carries.
	Batch int
	// SweepInterval is how often the sweep runs.
	SweepInterval time.Duration
}

// ConfigFromEnv reads the configuration through lookup (os.LookupEnv),
// once, in a composition root.
//
//	VOYAGE_API_KEY          Voyage's key; without it V2 retrieval stays lexical
//	V2_EMBED_MODEL          index model (default voyage-4; "off" disables V2 embeddings)
//	V2_EMBED_QUERY_MODEL    query model (default voyage-4-lite)
//	V2_EMBED_BATCH          versions per embedder request (default 64)
//	V2_INDEX_SWEEP_SECONDS  how often the sweep looks for versions without embeddings (default 300)
func ConfigFromEnv(lookup func(string) (string, bool)) Config {
	get := func(key, def string) string {
		v, ok := lookup(key)
		if !ok || strings.TrimSpace(v) == "" {
			return def
		}
		v = strings.TrimSpace(v)
		if strings.EqualFold(v, "off") || strings.EqualFold(v, "none") {
			return ""
		}
		return v
	}
	key, _ := lookup("VOYAGE_API_KEY")
	c := Config{
		APIKey:        strings.TrimSpace(key),
		IndexModel:    get("V2_EMBED_MODEL", DefaultIndexModel),
		QueryModel:    get("V2_EMBED_QUERY_MODEL", DefaultQueryModel),
		Batch:         DefaultBatch,
		SweepInterval: DefaultSweepInterval,
	}
	if n, err := strconv.Atoi(get("V2_EMBED_BATCH", "")); err == nil && n > 0 {
		c.Batch = min(n, 512)
	}
	if n, err := strconv.Atoi(get("V2_INDEX_SWEEP_SECONDS", "")); err == nil && n > 0 {
		c.SweepInterval = time.Duration(n) * time.Second
	}
	return c
}

// Enabled reports whether V2 memories are embedded.
func (c Config) Enabled() bool { return c.APIKey != "" && c.IndexModel != "" }

// IndexEmbedder is the embedder of memory versions, or nil when disabled.
func (c Config) IndexEmbedder() embed.Embedder {
	if !c.Enabled() {
		return nil
	}
	return embed.NewVoyage(embed.VoyageConfig{APIKey: c.APIKey, Model: c.IndexModel,
		OutputDimension: ledger.EmbeddingDimensions, MaxAttempts: 2, Timeout: 30 * time.Second})
}

// QueryEmbedder is the embedder of queries and drafts, or nil when
// disabled. It never retries: the query path's deadline is 120 ms.
func (c Config) QueryEmbedder() embed.Embedder {
	if !c.Enabled() {
		return nil
	}
	model := c.QueryModel
	if model == "" {
		model = c.IndexModel
	}
	return embed.NewVoyage(embed.VoyageConfig{APIKey: c.APIKey, Model: model,
		OutputDimension: ledger.EmbeddingDimensions, MaxAttempts: 1, Timeout: 2 * time.Second})
}

// Indexer embeds memory versions and stores them through the ledger.
type Indexer struct {
	ledger   *ledger.Ledger
	embedder embed.Embedder
	model    string
	batch    int
	log      *slog.Logger
	spaces   sync.Map // space id → *sync.Mutex: one batch per space at a time

	// Metrics count what the indexer did, for logs and tests.
	Metrics Metrics
}

// Metrics are the indexer's counters.
type Metrics struct {
	// Requests are embedder calls; Embedded are versions stored.
	Requests, Embedded atomic.Int64
}

// New returns an indexer, or nil when the ledger, the embedder or the
// model is missing (nil means disabled).
func New(l *ledger.Ledger, e embed.Embedder, model string, batch int, log *slog.Logger) *Indexer {
	if l == nil || e == nil || strings.TrimSpace(model) == "" {
		return nil
	}
	if batch <= 0 {
		batch = DefaultBatch
	}
	if log == nil {
		log = slog.Default()
	}
	return &Indexer{ledger: l, embedder: e, model: model, batch: batch, log: log}
}

// Model is the index model.
func (ix *Indexer) Model() string { return ix.model }

// Index embeds the job's version, and what else waits in its space, in
// one embedder request, and returns how many embeddings it stored.
func (ix *Indexer) Index(ctx context.Context, args ledger.IndexArgs) (int, error) {
	mu, _ := ix.spaces.LoadOrStore(args.SpaceID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	scope, err := ix.ledger.SpaceScope(ctx, args.SpaceID)
	if errors.Is(err, ledger.ErrNotFound) {
		return 0, nil // the space is gone
	}
	if err != nil {
		return 0, err
	}
	work, err := ix.ledger.IndexWork(ctx, scope, args.SpaceID, ix.model, args.MemoryID, ix.batch)
	if err != nil || len(work) == 0 {
		return 0, err
	}
	texts := make([]string, len(work))
	for i, w := range work {
		texts[i] = w.Statement
	}
	start := time.Now()
	ix.Metrics.Requests.Add(1)
	vecs, err := ix.embedder.EmbedContext(ctx, texts, "document")
	if err != nil {
		return 0, fmt.Errorf("v2index: embed %d version(s) with %s: %w", len(texts), ix.model, err)
	}
	if len(vecs) != len(work) {
		return 0, fmt.Errorf("v2index: %s returned %d embeddings for %d statements", ix.model, len(vecs), len(work))
	}
	rows := make([]ledger.Embedding, len(work))
	for i, w := range work {
		if len(vecs[i]) != ledger.EmbeddingDimensions {
			return 0, fmt.Errorf("v2index: %s returned %d dimensions; V2 stores %d (check V2_EMBED_MODEL)",
				ix.model, len(vecs[i]), ledger.EmbeddingDimensions)
		}
		rows[i] = ledger.Embedding{MemoryID: w.MemoryID, SpaceID: w.SpaceID, Version: w.Version, Vector: ToFloat32(vecs[i])}
	}
	stored, err := ix.ledger.StoreEmbeddings(ctx, scope, ix.model, rows)
	if err != nil {
		return 0, err
	}
	ix.Metrics.Embedded.Add(int64(stored))
	ix.log.InfoContext(ctx, "v2index: embedded", "metric", "v2_index_embedded", "space_id", args.SpaceID.String(),
		"model", ix.model, "versions", len(work), "stored", stored, "embed_ms", time.Since(start).Milliseconds())
	return stored, nil
}

// ToFloat32 narrows an embedder's vector to the stored precision.
func ToFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x)
	}
	return out
}

// Worker runs index_memory jobs (ledger.IndexArgs).
type Worker struct {
	river.WorkerDefaults[ledger.IndexArgs]
	Indexer *Indexer
}

// Timeout bounds one attempt: one embedder request and one write.
func (w *Worker) Timeout(*river.Job[ledger.IndexArgs]) time.Duration { return time.Minute }

// Work embeds the version (and its space's batch). A rate limit snoozes
// the job for as long as Voyage asks, without spending an attempt.
func (w *Worker) Work(ctx context.Context, job *river.Job[ledger.IndexArgs]) error {
	if w.Indexer == nil {
		return river.JobCancel(errors.New("v2index: V2 embeddings are off (set VOYAGE_API_KEY and V2_EMBED_MODEL); " +
			"the sweep indexes this version once they are on"))
	}
	_, err := w.Indexer.Index(ctx, job.Args)
	if wait, limited := embed.IsRateLimited(err); limited {
		w.Indexer.log.WarnContext(ctx, "v2index: rate limited", "metric", "v2_index_rate_limited", "retry_after", wait.String())
		return river.JobSnooze(snoozeFor(wait))
	}
	return err
}

// snoozeFor is how long a rate-limited job waits: Voyage's Retry-After (at
// least 20 s), plus up to 10 s of jitter so a burst doesn't return at once.
func snoozeFor(retryAfter time.Duration) time.Duration {
	return max(retryAfter, 20*time.Second) + time.Duration(rand.Int64N(int64(10*time.Second)))
}

// SweepArgs is the periodic index sweep.
type SweepArgs struct{}

// Kind implements river.JobArgs.
func (SweepArgs) Kind() string { return "index_sweep" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ledger.QueueIndex, MaxAttempts: 1}
}

// SweepWorker queues an index job for every proposed or kept version
// without an embedding of the index model.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	Ledger *ledger.Ledger
	// Model is the index model; empty does nothing (embeddings are off).
	Model string
	// Jobs inserts the jobs; nil uses the River client working the sweep.
	Jobs Inserter
}

// Inserter inserts River jobs: (*river.Client[pgx.Tx]).InsertMany.
type Inserter interface {
	InsertMany(ctx context.Context, params []river.InsertManyParams) ([]*rivertype.JobInsertResult, error)
}

// Work runs one sweep.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	if w.Ledger == nil || w.Model == "" {
		return nil
	}
	missing, err := w.Ledger.UnindexedVersions(ctx, w.Model, sweepBatch)
	if err != nil || len(missing) == 0 {
		return err
	}
	var client Inserter = w.Jobs
	if client == nil {
		c, err := river.ClientFromContextSafely[pgx.Tx](ctx)
		if err != nil {
			return fmt.Errorf("v2index sweep: %w", err)
		}
		client = c
	}
	params := make([]river.InsertManyParams, len(missing))
	spaces := map[uuid.UUID]bool{}
	for i, a := range missing {
		params[i] = river.InsertManyParams{Args: a}
		spaces[a.SpaceID] = true
	}
	if _, err := client.InsertMany(ctx, params); err != nil {
		return fmt.Errorf("v2index sweep: queue %d job(s): %w", len(params), err)
	}
	slog.InfoContext(ctx, "v2index: sweep queued versions without embeddings", "metric", "v2_index_swept",
		"model", w.Model, "versions", len(missing), "spaces", len(spaces))
	return nil
}

// PeriodicJobs are the index path's periodic jobs: the sweep, when
// embeddings are on.
func PeriodicJobs(c Config) []*river.PeriodicJob {
	if !c.Enabled() {
		return nil
	}
	interval := c.SweepInterval
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)}
}

// AddWorkers registers the index workers. ix may be nil (embeddings are
// off, or an insert-only client that never works jobs): index jobs then
// cancel with the reason.
func AddWorkers(workers *river.Workers, ix *Indexer, l *ledger.Ledger) {
	river.AddWorker(workers, &Worker{Indexer: ix})
	model := ""
	if ix != nil {
		model = ix.model
	}
	river.AddWorker(workers, &SweepWorker{Ledger: l, Model: model})
}
