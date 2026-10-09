package ledger

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// Embeddings of memory versions (plan 25 §5.11, migration 037) are derived
// index data, like the search vector: writing them needs no receipt, but
// they are written here, in the ledger, so every write to schema v2 still
// has one home.
//
//   - Every command that writes a statement version a person or agent may
//     look up (a proposal, a kept memory, an edit, a resolution's new
//     words, an Undo that restores a version) enqueues index_memory for it
//     in the command's own transaction (WithIndexJobs), with the compile
//     and judge jobs: a rolled-back command enqueues nothing.
//   - Proposals are embedded too, not only kept memories: Remember's
//     near-duplicate check offers to keep an agent's pending proposal
//     instead of writing the same thing twice, and the judge reuses a
//     proposal's stored vector instead of embedding it again.
//   - The index job (internal/v2index) reads IndexWork, embeds the batch
//     and writes it with StoreEmbeddings. The sweep (UnindexedVersions)
//     finds what a job never covered: memories written while embeddings
//     were off, a model switch, a job that ran out of attempts.
//   - Lanes search with Nearest, exactly and within the scope's spaces.
//   - Forget purges the vectors with the words, by trigger (037).

// QueueIndex is the River queue index jobs run on.
const QueueIndex = "index"

// EmbeddingDimensions is the width of a stored embedding: halfvec(1024).
const EmbeddingDimensions = 1024

// IndexArgs is the River job that embeds a memory's current version.
//
// It is unique by args over unfinished states only, so the sweep can queue
// a version again after its job completed (a model switch) or was
// discarded. The job is idempotent: a version that has its embedding is
// skipped, and one that is no longer current is left to the job of the
// version that replaced it.
type IndexArgs struct {
	MemoryID uuid.UUID `json:"memory_id"`
	SpaceID  uuid.UUID `json:"space_id"`
	Version  int       `json:"version"`
}

// Kind implements river.JobArgs.
func (IndexArgs) Kind() string { return "index_memory" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (IndexArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueIndex,
		// River's backoff covers a few hours of outage; rate limits snooze
		// without spending attempts, and the sweep takes over after that.
		MaxAttempts: 10,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: slices.Clone(CompileUniqueStates)},
	}
}

// WithIndexJobs makes commands enqueue index_memory jobs: set it when V2
// embeddings are configured (an embedder and an index model). Without it
// nothing is enqueued, and the sweep indexes what was written once they
// are.
func WithIndexJobs() Option { return func(l *Ledger) { l.indexJobs = true } }

// indexVersion queues the index job for a memory version the command
// wrote; flush inserts it with the command's other jobs.
func (w *writer) indexVersion(spaceID, memoryID uuid.UUID, version int) {
	if !w.indexJobs {
		return
	}
	args := IndexArgs{MemoryID: memoryID, SpaceID: spaceID, Version: version}
	if !slices.ContainsFunc(w.jobs, func(p river.InsertManyParams) bool { return p.Args == args }) {
		w.jobs = append(w.jobs, river.InsertManyParams{Args: args})
	}
}

// indexedLifecycles are the lifecycles whose current version is embedded.
var indexedLifecycles = []string{string(lifecycle.Proposed), string(lifecycle.Kept)}

// EmbedTask is a memory version waiting for an embedding.
type EmbedTask struct {
	MemoryID  uuid.UUID
	SpaceID   uuid.UUID
	Version   int
	Statement string
}

// IndexWork reads what one index job embeds in a space: the memory it was
// queued for first, when its current version still needs an embedding of
// model, then more of the space's versions that need one, newest change
// first, up to limit. One embedder request then covers a burst of writes,
// and the other jobs of the burst find their version done.
func (l *Ledger) IndexWork(ctx context.Context, scope Scope, spaceID uuid.UUID, model string, first uuid.UUID, limit int) ([]EmbedTask, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []EmbedTask
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT m.id, m.space_id, m.current_version, v.statement
			  FROM v2.memories m
			  JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
			 WHERE m.space_id = $1 AND m.lifecycle = ANY ($2) AND v.statement IS NOT NULL
			   AND NOT EXISTS (SELECT 1 FROM v2.memory_embeddings e
			                    WHERE e.memory_id = m.id AND e.version = m.current_version AND e.model = $3)
			 ORDER BY (m.id = $4) DESC, m.updated_at DESC, m.id
			 LIMIT $5`, spaceID, indexedLifecycles, model, first, max(limit, 1))
		if err != nil {
			return fmt.Errorf("ledger: index work: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (EmbedTask, error) {
			var t EmbedTask
			err := r.Scan(&t.MemoryID, &t.SpaceID, &t.Version, &t.Statement)
			return t, err
		})
		return err
	})
	return out, err
}

// Embedding is one vector to store for a memory version.
type Embedding struct {
	MemoryID uuid.UUID
	SpaceID  uuid.UUID
	Version  int
	Vector   []float32
}

// StoreEmbeddings writes embeddings of model in one transaction and
// returns how many it stored. Each is stored only while its version is
// still the memory's current one, the memory is proposed or kept, and its
// words are there; the memory row is locked FOR SHARE, so a Forget either
// waits for this transaction (and then purges what it wrote) or commits
// first (and this one stores nothing). An embedding that exists already
// is left as it is.
func (l *Ledger) StoreEmbeddings(ctx context.Context, scope Scope, model string, rows []Embedding) (int, error) {
	if l == nil {
		return 0, ErrDisabled
	}
	if strings.TrimSpace(model) == "" {
		return 0, invalid("model", "say which model the embeddings come from")
	}
	if len(rows) == 0 {
		return 0, nil
	}
	b := &pgx.Batch{}
	for _, r := range rows {
		if _, ok := scope.Grant(r.SpaceID); !ok {
			return 0, ErrNotFound
		}
		lit, err := VectorLiteral(r.Vector)
		if err != nil {
			return 0, err
		}
		b.Queue(`
			INSERT INTO v2.memory_embeddings (memory_id, version, model, space_id, embedding)
			SELECT m.id, m.current_version, $3, m.space_id, $4::halfvec(1024)
			  FROM v2.memories m
			  JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
			 WHERE m.id = $1 AND m.space_id = $5 AND m.current_version = $2
			   AND m.lifecycle = ANY ($6) AND v.statement IS NOT NULL
			   FOR SHARE OF m
			ON CONFLICT DO NOTHING`, r.MemoryID, r.Version, model, lit, r.SpaceID, indexedLifecycles)
	}
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	results := tx.SendBatch(ctx, b)
	stored := 0
	for range rows {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return 0, mapDBError(fmt.Errorf("ledger: store embedding: %w", err))
		}
		stored += int(tag.RowsAffected())
	}
	if err := results.Close(); err != nil {
		return 0, mapDBError(fmt.Errorf("ledger: store embeddings: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, mapDBError(fmt.Errorf("ledger: store embeddings: %w", err))
	}
	return stored, nil
}

// StoredEmbedding reads the embedding of model of a memory's current
// version, when the index job has written it.
func (l *Ledger) StoredEmbedding(ctx context.Context, scope Scope, memoryID uuid.UUID, model string) ([]float32, bool, error) {
	if l == nil {
		return nil, false, ErrDisabled
	}
	var vec []float32
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT e.embedding::vector::real[]
			  FROM v2.memory_embeddings e
			  JOIN v2.memories m ON m.id = e.memory_id AND m.current_version = e.version
			 WHERE e.memory_id = $1 AND e.model = $2`, memoryID, model).Scan(&vec)
		if errNoRows(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return vec, vec != nil, nil
}

// Neighbor is a memory near a vector.
type Neighbor struct {
	ID uuid.UUID
	// Similarity is the cosine similarity, 1 for the same direction.
	Similarity float64
}

// NearestQuery is an exact, scoped nearest-neighbour search over memory
// embeddings.
type NearestQuery struct {
	// Spaces to search; required (and in the transaction's scope: row-level
	// security holds the rest out anyway).
	Spaces []uuid.UUID
	// Model is the index model whose embeddings are searched.
	Model  string
	Vector []float32
	// Lifecycles defaults to kept.
	Lifecycles []lifecycle.Lifecycle
	// Kind narrows to facts or decisions.
	Kind Kind
	// SkipSuperseded leaves out decisions a newer decision superseded, as
	// recall and search do.
	SkipSuperseded bool
	// Except leaves one memory out (the one being judged).
	Except uuid.UUID
	// K is how many to return at most (default 10).
	K int
	// Floor is the least similarity returned; 0 or less keeps all K.
	Floor float64
}

// Nearest runs q in tx, a transaction of Read (memax_v2, scoped): an
// exact ORDER BY embedding <=> q LIMIT k over the spaces' rows of the
// model, inside a materialized CTE, joined to each memory's current
// version (plan §5.11). There is no approximate index to cut the pool.
func Nearest(ctx context.Context, tx pgx.Tx, q NearestQuery) ([]Neighbor, error) {
	sql, args, err := NearestSQL(q)
	if err != nil || sql == "" {
		return nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: nearest: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Neighbor, error) {
		var n Neighbor
		err := r.Scan(&n.ID, &n.Similarity)
		return n, err
	})
}

// NearestSQL is Nearest's statement and arguments, for a caller that
// reads more with it in the same statement: rows of (memory id,
// similarity), nearest first. It is empty when q can't match anything.
func NearestSQL(q NearestQuery) (string, []any, error) {
	if len(q.Spaces) == 0 || len(q.Vector) == 0 || q.Model == "" {
		return "", nil, nil
	}
	lit, err := VectorLiteral(q.Vector)
	if err != nil {
		return "", nil, err
	}
	lifecycles := indexedLifecycles[1:] // kept
	if len(q.Lifecycles) > 0 {
		lifecycles = make([]string, len(q.Lifecycles))
		for i, l := range q.Lifecycles {
			lifecycles[i] = string(l)
		}
	}
	k := q.K
	if k <= 0 {
		k = 10
	}
	var except *uuid.UUID
	if q.Except != uuid.Nil {
		except = &q.Except
	}
	maxDistance := 2.0 // cosine distance is in [0, 2]
	if q.Floor > 0 {
		maxDistance = 1 - q.Floor
	}
	return `
		WITH knn AS MATERIALIZED (
		    SELECT e.memory_id, e.embedding <=> $1::halfvec(1024) AS distance
		      FROM v2.memory_embeddings e
		      JOIN v2.memories m ON m.id = e.memory_id AND m.current_version = e.version
		     WHERE e.space_id = ANY ($2) AND e.model = $3
		       AND m.space_id = ANY ($2) AND m.lifecycle = ANY ($4) AND ($5 = '' OR m.kind = $5)
		       AND (NOT $6 OR NOT (m.kind = 'decision' AND COALESCE(m.decision ->> 'status', '') = 'superseded'))
		       AND ($7::uuid IS NULL OR m.id <> $7)
		     ORDER BY distance, e.memory_id
		     LIMIT $8)
		SELECT memory_id, 1 - distance FROM knn WHERE distance <= $9 ORDER BY distance, memory_id`,
		[]any{lit, q.Spaces, q.Model, lifecycles, string(q.Kind), q.SkipSuperseded, except, k, maxDistance}, nil
}

// VectorCandidates is the judge's vector set (§5.8): the k kept memories
// of the space nearest to vec among model's embeddings, other than the
// memory being judged, as judge candidates, best first, with Score set to
// the cosine similarity. The judge applies its own floor.
func (l *Ledger) VectorCandidates(ctx context.Context, scope Scope, spaceID, except uuid.UUID, model string, vec []float32, k int) ([]JudgeCandidate, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []JudgeCandidate
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		near, err := Nearest(ctx, tx, NearestQuery{Spaces: []uuid.UUID{spaceID}, Model: model, Vector: vec,
			Lifecycles: []lifecycle.Lifecycle{lifecycle.Kept}, Except: except, K: k})
		if err != nil || len(near) == 0 {
			return err
		}
		ids := make([]uuid.UUID, len(near))
		for i, n := range near {
			ids[i] = n.ID
		}
		cands, err := judgeCandidates(ctx, tx, ` WHERE m.id = ANY ($1) AND m.space_id = $2`, ids, spaceID)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]JudgeCandidate, len(cands))
		for _, c := range cands {
			byID[c.ID] = c
		}
		for _, n := range near {
			if c, ok := byID[n.ID]; ok {
				c.Score = n.Similarity
				out = append(out, c)
			}
		}
		return nil
	})
	return out, err
}

// DuplicateMatch is how a near-duplicate repeats a draft.
type DuplicateMatch string

// The matches.
const (
	// MatchExact: the same words (the content hash the judge's stage 0
	// uses).
	MatchExact DuplicateMatch = "exact"
	// MatchNear: close enough by embedding to be the same thing.
	MatchNear DuplicateMatch = "near"
)

// NearDuplicate is a memory a draft repeats.
type NearDuplicate struct {
	Memory *Memory
	// Similarity is the cosine similarity (1 for an exact repeat).
	Similarity float64
	Match      DuplicateMatch
	// Created is the receipt that wrote the memory: who proposed or kept
	// it, through which agent, and when.
	Created Receipt
}

// NearDuplicateQuery asks which kept memories and pending proposals of a
// space a draft statement repeats.
type NearDuplicateQuery struct {
	SpaceID   uuid.UUID
	Statement string
	// Model and Vector are the draft's embedding; without them only exact
	// repeats are found.
	Model  string
	Vector []float32
	// Floor is the least similarity a near repeat needs.
	Floor float64
	// Limit bounds the matches (default 3).
	Limit int
}

// NearDuplicates finds the kept memories and pending proposals of the
// space that a draft repeats (Remember's check, §5.8): exact repeats by
// content hash first, then near ones by embedding, at least Floor
// similar, best first. Superseded decisions are left out, as everywhere a
// reader looks for what is in force. It only reads.
func (l *Ledger) NearDuplicates(ctx context.Context, scope Scope, q NearDuplicateQuery) ([]NearDuplicate, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return nil, ErrNotFound
	}
	statement := strings.TrimSpace(q.Statement)
	if statement == "" {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 3
	}
	var out []NearDuplicate
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		var exact []uuid.UUID
		rows, err := tx.Query(ctx, `
			SELECT m.id FROM v2.memories m
			 WHERE m.space_id = $1 AND m.content_sha256 = $2 AND m.lifecycle = ANY ($3)
			   AND NOT (m.kind = 'decision' AND COALESCE(m.decision ->> 'status', '') = 'superseded')
			 ORDER BY m.lifecycle = 'kept' DESC, m.seq
			 LIMIT $4`, q.SpaceID, textsig.ContentSHA256(statement), indexedLifecycles, limit)
		if err != nil {
			return fmt.Errorf("ledger: exact repeats: %w", err)
		}
		if exact, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return fmt.Errorf("ledger: exact repeats: %w", err)
		}
		type hit struct {
			id    uuid.UUID
			sim   float64
			match DuplicateMatch
		}
		hits := make([]hit, 0, limit)
		for _, id := range exact {
			hits = append(hits, hit{id, 1, MatchExact})
		}
		if len(q.Vector) > 0 && q.Model != "" && len(hits) < limit {
			near, err := Nearest(ctx, tx, NearestQuery{Spaces: []uuid.UUID{q.SpaceID}, Model: q.Model, Vector: q.Vector,
				Lifecycles: []lifecycle.Lifecycle{lifecycle.Kept, lifecycle.Proposed}, SkipSuperseded: true,
				K: limit + len(exact), Floor: q.Floor})
			if err != nil {
				return err
			}
			for _, n := range near {
				if len(hits) == limit {
					break
				}
				if !slices.Contains(exact, n.ID) {
					hits = append(hits, hit{n.ID, min(n.Similarity, 1), MatchNear})
				}
			}
		}
		for _, h := range hits {
			m, err := loadMemory(ctx, tx, scope, h.id, false)
			if err != nil {
				return err
			}
			rcs, err := tx.Query(ctx, receiptSelect+` WHERE id = $1 AND space_id = $2`, m.CreatedReceiptID, m.SpaceID)
			if err != nil {
				return fmt.Errorf("ledger: near duplicate receipt: %w", err)
			}
			created, err := pgx.CollectExactlyOneRow(rcs, scanReceipt)
			if err != nil {
				return fmt.Errorf("ledger: near duplicate receipt: %w", err)
			}
			out = append(out, NearDuplicate{Memory: m, Similarity: h.sim, Match: h.match, Created: created})
		}
		return nil
	})
	return out, err
}

// UnindexedVersions lists memory versions that need an embedding of model,
// across spaces, for the index sweep: ids only, at most limit. It reads
// the spaces from public.hubs as the login role (as SpaceScope does), then
// each batch of spaces inside its own scope, as Memax, so row-level
// security holds for the sweep too: no cross-space policy is needed.
func (l *Ledger) UnindexedVersions(ctx context.Context, model string, limit int) ([]IndexArgs, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	rows, err := l.pool.Query(ctx, `
		SELECT id, tenant_id, COALESCE(space_kind, '') FROM public.hubs WHERE tenant_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("ledger: unindexed: spaces: %w", err)
	}
	grants, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SpaceGrant, error) {
		var g SpaceGrant
		var kind string
		err := r.Scan(&g.SpaceID, &g.TenantID, &kind)
		g.Kind = policy.SpaceKind(kind)
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: unindexed: spaces: %w", err)
	}
	const perScope = 500
	var out []IndexArgs
	for start := 0; start < len(grants) && len(out) < limit; start += perScope {
		chunk := grants[start:min(start+perScope, len(grants))]
		scope := Scope{Spaces: chunk}
		err := l.Read(ctx, scope, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT m.id, m.space_id, m.current_version
				  FROM v2.memories m
				 WHERE m.space_id = ANY ($1) AND m.lifecycle = ANY ($2)
				   AND NOT EXISTS (SELECT 1 FROM v2.memory_embeddings e
				                    WHERE e.memory_id = m.id AND e.version = m.current_version AND e.model = $3)
				 ORDER BY m.updated_at DESC, m.id
				 LIMIT $4`, scope.SpaceIDs(), indexedLifecycles, model, limit-len(out))
			if err != nil {
				return fmt.Errorf("ledger: unindexed: %w", err)
			}
			found, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (IndexArgs, error) {
				var a IndexArgs
				err := r.Scan(&a.MemoryID, &a.SpaceID, &a.Version)
				return a, err
			})
			out = append(out, found...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// VectorLiteral formats a vector as pgvector's text form, "[0.1,0.2,…]",
// for a $n::halfvec(1024) parameter. It refuses the wrong width and
// values that aren't finite (halfvec can't hold them).
func VectorLiteral(v []float32) (string, error) {
	if len(v) != EmbeddingDimensions {
		return "", invalid("embedding", "has %d dimensions; V2 stores %d", len(v), EmbeddingDimensions)
	}
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) || math.Abs(float64(x)) > 65504 {
			return "", invalid("embedding", "value %d is %v, which halfvec can't hold", i, x)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', 7, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}
