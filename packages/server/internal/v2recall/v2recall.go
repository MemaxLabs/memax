// Package v2recall reads the V2 record for recall and search (plan 25
// §5.11): kept memories matched to a query, a session's own proposals,
// the digest of a space, and what changed since an agent's last read.
//
// Every read runs through ledger.Read, as memax_v2 inside the caller's
// scope, so row-level security holds; the explicit space filters stay as
// defence in depth.
//
// # Lanes and fusion
//
// A query runs on lanes, each returning memory IDs in rank order, fused
// with weighted reciprocal rank fusion (V1's constants, fuse.go). The
// lexical lanes are full-text search over memories.search, and trigram
// word similarity over the statement, so typos and partial words still
// match. With vectors (WithVectors), the query is embedded while the
// lexical lanes run, and the vector lane is an exact, space-filtered KNN
// over the memories' embeddings (ledger.Nearest); a query embedding that
// misses its 120 ms deadline leaves the answer lexical, flagged in
// Result.Retrieval. A reranker (WithReranker) reorders more than 8
// candidates within 150 ms, else the RRF order stands. The LLM query
// distiller is never on this path.
package v2recall

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
)

// Hit is one memory a read returns.
type Hit struct {
	ID        uuid.UUID
	Ref       string
	SpaceID   uuid.UUID
	Statement string
	Section   ledger.Section
	Kind      ledger.Kind
	State     string
	Lifecycle string
	Trust     string
	Version   int
	UpdatedAt time.Time
	// Score is the fused score (higher is better); 0 outside a query.
	Score float64
}

// Filter narrows a query.
type Filter struct {
	// Spaces to read; required. They must be in the scope.
	Spaces []uuid.UUID
	// Lifecycle is "kept" (the default) or "proposed".
	Lifecycle string
	// Kind narrows to facts or decisions.
	Kind ledger.Kind
	// Proposer and SessionRef narrow proposals to the ones one agent
	// connection made in one session (read-after-write for the proposer).
	Proposer   uuid.UUID
	SessionRef string
}

// Lane ranks candidate memories for a query inside a read transaction.
type Lane interface {
	Name() string
	Rank(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]uuid.UUID, error)
}

// Query is a search.
type Query struct {
	Text   string
	Filter Filter
	Limit  int
}

// Result is a search's answer.
type Result struct {
	Hits []Hit
	// LexicalOnly: no lane matched by meaning.
	LexicalOnly bool
	// Retrieval says how the query path went (MCP's _meta).
	Retrieval Retrieval
}

// Retrieval is what the with-query path did: whether the vector lane ran
// (StageOK), was off, or fell back to lexical only because the query
// embedding missed its deadline (StageTimeout) or failed (StageError); and
// whether the reranker reordered, was skipped, off, or timed out and left
// the RRF order.
type Retrieval struct {
	Vector string
	Rerank string
	// EmbedMS is how long the query embedding took (or waited).
	EmbedMS int64
}

// Searcher runs queries on the ledger.
type Searcher struct {
	ledger  *ledger.Ledger
	lanes   []Lane
	vectors *Vectors
	rerank  rerank.Reranker
}

// New returns a Searcher with the lexical lanes, or nil without a ledger.
func New(l *ledger.Ledger) *Searcher {
	if l == nil {
		return nil
	}
	return &Searcher{ledger: l, lanes: []Lane{FullText{}, Trigram{}}}
}

// WithLane adds a lane.
func (s *Searcher) WithLane(l Lane) *Searcher { s.lanes = append(s.lanes, l); return s }

// WithVectors adds the vector lane (nil: lexical only).
func (s *Searcher) WithVectors(v *Vectors) *Searcher { s.vectors = v; return s }

// WithReranker sets the reranker, V1's interface (nil: none).
func (s *Searcher) WithReranker(r rerank.Reranker) *Searcher {
	if isNilReranker(r) {
		r = nil
	}
	s.rerank = r
	return s
}

// isNilReranker catches a typed nil (a *rerank.Voyage that is nil because
// it has no key) inside the interface.
func isNilReranker(r rerank.Reranker) bool {
	if r == nil {
		return true
	}
	switch x := r.(type) {
	case *rerank.Voyage:
		return x == nil
	case *rerank.Cohere:
		return x == nil
	}
	return false
}

// MaxLimit bounds a query's results.
const MaxLimit = 50

// rrfK is the reciprocal rank fusion constant.
const rrfK = 60

// Search returns the memories matching q, best first.
func (s *Searcher) Search(ctx context.Context, scope ledger.Scope, q Query) (Result, error) {
	if s == nil {
		return Result{}, ledger.ErrDisabled
	}
	if len(q.Filter.Spaces) == 0 || strings.TrimSpace(q.Text) == "" {
		return Result{LexicalOnly: true}, nil
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	q.Limit = min(q.Limit, MaxLimit)
	pool := max(q.Limit*4, 40)
	res := Result{LexicalOnly: true, Retrieval: Retrieval{Vector: StageOff, Rerank: StageOff}}
	if s.rerank != nil {
		res.Retrieval.Rerank = StageSkipped
	}
	// The query embedding runs while the lexical lanes do (§5.11).
	var pending *pendingQuery
	if s.vectors != nil && q.Filter.Proposer == uuid.Nil {
		pending = s.vectors.startQuery(ctx, q.Text)
	}
	var rankings []ranking
	// loaded holds the hits the lanes returned with their ranking (the
	// built-in lexical lanes do, so their hits cost no second statement).
	loaded := map[uuid.UUID]Hit{}
	lexical := func(tx pgx.Tx) error {
		found := map[uuid.UUID]bool{}
		for _, lane := range s.lanes {
			// A fallback lane (trigrams, for typos) runs only when the lanes
			// before it found fewer than the results asked for: scoring
			// every statement's trigrams is the expensive part of a recall.
			if fb, ok := lane.(interface{ Fallback() bool }); ok && fb.Fallback() && len(found) >= q.Limit {
				continue
			}
			ids, err := rankLane(ctx, tx, lane, q, pool, loaded)
			if err != nil {
				return fmt.Errorf("v2recall: %s lane: %w", lane.Name(), err)
			}
			for _, id := range ids {
				found[id] = true
			}
			rankings = append(rankings, ranking{lane: lane.Name(), ids: ids})
		}
		return nil
	}
	// load fuses the lanes and reads the best hits: q.Limit of them, or
	// the reranker's pool when it may rerank. Only hits no lane returned
	// are read (the vector lane's), so a lexical answer reads nothing more.
	load := func(tx pgx.Tx) error {
		ids, scores := fuse(rankings)
		if len(ids) == 0 {
			return nil
		}
		n := q.Limit
		if s.rerank != nil && len(ids) > rerankMin {
			n = max(n, s.rerank.TopN())
		}
		ids = ids[:min(n, len(ids))]
		hits, err := hitsOf(ctx, tx, q.Filter.Spaces, ids, loaded)
		if err != nil {
			return err
		}
		for i := range hits {
			hits[i].Score = scores[hits[i].ID]
		}
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].Score != hits[j].Score {
				return hits[i].Score > hits[j].Score
			}
			return hits[i].ID.String() > hits[j].ID.String()
		})
		res.Hits = hits
		return nil
	}

	if pending == nil {
		// Lexical only: one transaction, as before vectors existed.
		if err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
			if err := lexical(tx); err != nil {
				return err
			}
			return load(tx)
		}); err != nil {
			return Result{}, err
		}
	} else {
		// Two transactions, so no pooled connection sits idle in one while
		// the embedding is on its way.
		if err := s.ledger.Read(ctx, scope, lexical); err != nil {
			return Result{}, err
		}
		vec, status, took, embedErr := pending.wait()
		res.Retrieval.Vector, res.Retrieval.EmbedMS = status, took.Milliseconds()
		if status != StageOK {
			s.vectors.cfg.Log.WarnContext(ctx, "v2recall: answering lexically", "metric", "v2_recall_lexical_fallback",
				"reason", status, "embed_ms", took.Milliseconds(), "error", embedErr)
		}
		if err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
			if vec != nil {
				// The neighbours come with their hits, so the answer needs no
				// further statement.
				near, err := nearestHits(ctx, tx, ledger.NearestQuery{Spaces: q.Filter.Spaces, Model: s.vectors.cfg.Model,
					Vector: vec, Lifecycles: filterLifecycles(q.Filter), Kind: q.Filter.Kind, SkipSuperseded: true,
					K: pool, Floor: s.vectors.cfg.Floor})
				if err != nil {
					return fmt.Errorf("v2recall: vector lane: %w", err)
				}
				ids := make([]uuid.UUID, len(near))
				for i, h := range near {
					ids[i] = h.ID
					loaded[h.ID] = h
				}
				rankings = append(rankings, ranking{lane: laneVector, ids: ids})
				res.LexicalOnly = len(ids) == 0
			}
			return load(tx)
		}); err != nil {
			return Result{}, err
		}
	}
	if s.rerank != nil {
		res.Hits, res.Retrieval.Rerank = rerankHits(ctx, s.rerank, q.Text, res.Hits)
	}
	if len(res.Hits) > q.Limit {
		res.Hits = res.Hits[:q.Limit]
	}
	return res, nil
}

// filterLifecycles is the lifecycle a filter reads, for the vector lane.
func filterLifecycles(f Filter) []lifecycle.Lifecycle {
	if f.Lifecycle == "" {
		return []lifecycle.Lifecycle{lifecycle.Kept}
	}
	return []lifecycle.Lifecycle{lifecycle.Lifecycle(f.Lifecycle)}
}

const hitSelect = `
	SELECT m.id, m.seq, m.space_id, COALESCE(v.statement, ''), m.section, m.kind, m.state, m.lifecycle,
	       m.trust, m.current_version, m.updated_at
	  FROM v2.memories m
	  LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version`

func scanHit(r pgx.CollectableRow) (Hit, error) {
	var h Hit
	var seq int64
	err := r.Scan(&h.ID, &seq, &h.SpaceID, &h.Statement, &h.Section, &h.Kind, &h.State, &h.Lifecycle,
		&h.Trust, &h.Version, &h.UpdatedAt)
	h.Ref = ledger.FormatRef(ledger.PrefixMemory, seq)
	return h, err
}

func loadHits(ctx context.Context, tx pgx.Tx, spaces, ids []uuid.UUID) ([]Hit, error) {
	rows, err := tx.Query(ctx, hitSelect+` WHERE m.id = ANY($1) AND m.space_id = ANY($2)`, ids, spaces)
	if err != nil {
		return nil, fmt.Errorf("v2recall: load: %w", err)
	}
	hits, err := pgx.CollectRows(rows, scanHit)
	if err != nil {
		return nil, fmt.Errorf("v2recall: load: %w", err)
	}
	return hits, nil
}

// hitsOf returns the hits of ids: those in loaded as they are, the rest
// read in one statement. The order is the caller's to set.
func hitsOf(ctx context.Context, tx pgx.Tx, spaces, ids []uuid.UUID, loaded map[uuid.UUID]Hit) ([]Hit, error) {
	hits := make([]Hit, 0, len(ids))
	var missing []uuid.UUID
	for _, id := range ids {
		if h, ok := loaded[id]; ok {
			hits = append(hits, h)
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return hits, nil
	}
	more, err := loadHits(ctx, tx, spaces, missing)
	if err != nil {
		return nil, err
	}
	return append(hits, more...), nil
}

// nearestHits is ledger.Nearest returning the neighbours' hits, nearest
// first, in one statement.
func nearestHits(ctx context.Context, tx pgx.Tx, q ledger.NearestQuery) ([]Hit, error) {
	sql, args, err := ledger.NearestSQL(q)
	if err != nil || sql == "" {
		return nil, err
	}
	rows, err := tx.Query(ctx, `WITH near (id, similarity) AS (`+sql+`)`+hitSelect+`
	  JOIN near n ON n.id = m.id
	 ORDER BY n.similarity DESC, n.id`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanHit)
}

// hitLane is a lane that returns its hits with its ranking, in the same
// statement.
type hitLane interface {
	rankHits(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]Hit, error)
}

// rankLane ranks with lane, keeping the hits it returns in loaded.
func rankLane(ctx context.Context, tx pgx.Tx, lane Lane, q Query, limit int, loaded map[uuid.UUID]Hit) ([]uuid.UUID, error) {
	hl, ok := lane.(hitLane)
	if !ok {
		return lane.Rank(ctx, tx, q, limit)
	}
	hits, err := hl.rankHits(ctx, tx, q, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
		loaded[h.ID] = h
	}
	return ids, nil
}

// withHits wraps a lane's ranking (rows of id, score, seq, best first) so
// the statement returns the ranked hits in the same order.
func withHits(ranking string) string {
	return `WITH ranked AS (` + ranking + `)` + hitSelect + `
	  JOIN ranked r ON r.id = m.id
	 ORDER BY r.score DESC, r.seq DESC`
}

// filterSQL is the WHERE clause every lane shares, from $2 on: spaces,
// lifecycle, kind, proposer, session. A superseded decision stays kept,
// with its history, but is no longer in force, so recall and search leave
// it out, as the compiled files do (see notSuperseded).
const filterSQL = `m.space_id = ANY($2) AND m.lifecycle = $3 AND ($4 = '' OR m.kind = $4) AND ` + notSuperseded + `
	AND ($5::uuid IS NULL OR EXISTS (
	      SELECT 1 FROM v2.receipts r
	       WHERE r.id = m.created_receipt_id AND r.space_id = m.space_id
	         AND r.actor_kind = 'agent' AND r.actor_id = $5 AND r.session_ref = $6))`

// notSuperseded excludes kept decisions that a newer decision superseded
// (the judge's explicit changes, a settled conflict).
const notSuperseded = `NOT (m.kind = 'decision' AND COALESCE(m.decision ->> 'status', '') = 'superseded')`

func filterArgs(f Filter) []any {
	lifecycle := f.Lifecycle
	if lifecycle == "" {
		lifecycle = "kept"
	}
	var proposer *uuid.UUID
	if f.Proposer != uuid.Nil {
		p := f.Proposer
		proposer = &p
	}
	return []any{f.Spaces, lifecycle, string(f.Kind), proposer, f.SessionRef}
}

// FullText ranks by full-text search over the statement (the search
// vector the ledger writes), any query word counting, prefixes included.
type FullText struct{}

// Name implements Lane.
func (FullText) Name() string { return "fts" }

// ranking is the lane's query: rows of (id, score, seq), best first; ok
// is false when the text has nothing to search for.
func (FullText) ranking(q Query, limit int) (string, []any, bool) {
	tsq := tsQuery(q.Text)
	if tsq == "" {
		return "", nil, false
	}
	args := append([]any{tsq}, filterArgs(q.Filter)...)
	args = append(args, limit)
	return `
		SELECT m.id, ts_rank_cd(m.search, tq) AS score, m.seq
		  FROM v2.memories m, to_tsquery('simple', public.immutable_unaccent($1)) tq
		 WHERE m.search @@ tq AND ` + filterSQL + `
		 ORDER BY ts_rank_cd(m.search, tq) DESC, m.seq DESC
		 LIMIT $7`, args, true
}

// Rank implements Lane.
func (l FullText) Rank(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]uuid.UUID, error) {
	sql, args, ok := l.ranking(q, limit)
	if !ok {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM (`+sql+`) ranked ORDER BY score DESC, seq DESC`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// rankHits is Rank returning the hits, in one statement.
func (l FullText) rankHits(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]Hit, error) {
	sql, args, ok := l.ranking(q, limit)
	if !ok {
		return nil, nil
	}
	rows, err := tx.Query(ctx, withHits(sql), args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanHit)
}

// Trigram ranks by how well the query matches part of the statement
// (pg_trgm word similarity), for typos and partial words.
type Trigram struct{}

// Name implements Lane.
func (Trigram) Name() string { return "trigram" }

// Fallback marks the lane as one that runs only when the full-text lane
// found too little: a query whose words match needs no typo tolerance.
func (Trigram) Fallback() bool { return true }

// trigramThreshold is the least word similarity that counts as a match.
const trigramThreshold = 0.3

// ranking is the lane's query: rows of (id, score, seq), best first; ok
// is false when the text is too short to match by trigrams.
func (Trigram) ranking(q Query, limit int) (string, []any, bool) {
	text := normalize(q.Text)
	if utf8.RuneCountInString(text) < 3 {
		return "", nil, false
	}
	args := append([]any{text}, filterArgs(q.Filter)...)
	args = append(args, limit)
	return `
		SELECT m.id, word_similarity(q.t, public.immutable_unaccent(lower(v.statement))) AS score, m.seq
		  FROM (SELECT public.immutable_unaccent($1) AS t) q, v2.memory_versions v
		  JOIN v2.memories m ON m.id = v.memory_id AND v.version = m.current_version
		 WHERE q.t <% public.immutable_unaccent(lower(v.statement))
		   AND v.space_id = ANY($2) AND ` + filterSQL + `
		 ORDER BY word_similarity(q.t, public.immutable_unaccent(lower(v.statement))) DESC, m.seq DESC
		 LIMIT $7`, args, true
}

// threshold sets the word-similarity threshold the <% operator uses for
// the rest of the transaction. The <% operator is word similarity at the
// transaction's threshold, and memory_versions_trgm_idx (migration 034)
// serves it, so only statements that share trigrams with the query are
// scored.
func (Trigram) threshold(b *pgx.Batch) {
	b.Queue(`SELECT set_config('pg_trgm.word_similarity_threshold', $1, true)`, fmt.Sprintf("%g", trigramThreshold))
}

// Rank implements Lane.
func (l Trigram) Rank(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]uuid.UUID, error) {
	sql, args, ok := l.ranking(q, limit)
	if !ok {
		return nil, nil
	}
	var ids []uuid.UUID
	err := l.send(ctx, tx, `SELECT id FROM (`+sql+`) ranked ORDER BY score DESC, seq DESC`, args, func(rows pgx.Rows) error {
		var err error
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	return ids, err
}

// rankHits is Rank returning the hits, in one statement.
func (l Trigram) rankHits(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]Hit, error) {
	sql, args, ok := l.ranking(q, limit)
	if !ok {
		return nil, nil
	}
	var hits []Hit
	err := l.send(ctx, tx, withHits(sql), args, func(rows pgx.Rows) error {
		var err error
		hits, err = pgx.CollectRows(rows, scanHit)
		return err
	})
	return hits, err
}

// send runs the threshold and the query in one round trip.
func (l Trigram) send(ctx context.Context, tx pgx.Tx, sql string, args []any, read func(pgx.Rows) error) error {
	b := &pgx.Batch{}
	l.threshold(b)
	b.Queue(sql, args...).Query(read)
	return tx.SendBatch(ctx, b).Close()
}

var wordRE = regexp.MustCompile(`[\p{L}\p{N}]+`)

// stopwords are words too common to rank by.
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true, "do": true,
	"does": true, "for": true, "from": true, "how": true, "i": true, "in": true, "is": true, "it": true, "of": true,
	"on": true, "or": true, "should": true, "that": true, "the": true, "this": true, "to": true, "we": true,
	"what": true, "when": true, "where": true, "which": true, "who": true, "why": true, "with": true, "use": true,
}

// tsQuery turns free text into an OR of prefix terms for to_tsquery:
// "deploy targets?" → "deploy:* | targets:*".
func tsQuery(text string) string {
	var terms []string
	for _, w := range wordRE.FindAllString(normalize(text), -1) {
		if stopwords[w] || slices.Contains(terms, w+":*") {
			continue
		}
		terms = append(terms, w+":*")
		if len(terms) == 16 {
			break
		}
	}
	return strings.Join(terms, " | ")
}

// normalize lower-cases and strips accents' most common forms the way the
// search vector does (immutable_unaccent runs in SQL; this keeps Go's
// tokens comparable).
func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ExtrasQuery is what a recall reads beside its search or its digest
// (Extras), all in one round trip.
type ExtrasQuery struct {
	Spaces []uuid.UUID
	// Proposer and SessionRef ask for one connection's pending proposals
	// from one session, newest first, up to ProposalLimit: read-after-write
	// for the proposer (plan 25 §5.6). A session's own proposals are few,
	// so they all come back rather than being ranked against the query.
	Proposer      uuid.UUID
	SessionRef    string
	ProposalLimit int
	// Since asks for what changed after it: the writes the judge returned
	// to Review, and, with Forgotten, the memories forgotten (a digest
	// reads its own).
	Since     *time.Time
	Forgotten bool
}

// Extras is what an ExtrasQuery found.
type Extras struct {
	Proposals []Hit
	// Forgotten lists memories forgotten since, as space id → refs (the
	// words are gone): forget notices.
	Forgotten map[uuid.UUID][]string
	// Returned lists the writes the judge returned to Review since, as space
	// id → returns: the notices a connection gets on its next recall, since
	// it may have read them while they were kept. One that has since been
	// kept again, or settled, is still listed: what the agent read before
	// was not what stands.
	Returned map[uuid.UUID][]Returned
}

// Returned is a Write-level agent's write the judge put back in Review
// (rule 11): its ref, and the decision in force it contradicts.
type Returned struct {
	Ref      string
	Decision string
}

// Extras reads what q asks for in one round trip (ledger.ReadBatch): its
// statements don't depend on each other.
func (s *Searcher) Extras(ctx context.Context, scope ledger.Scope, q ExtrasQuery) (Extras, error) {
	out := Extras{Forgotten: map[uuid.UUID][]string{}, Returned: map[uuid.UUID][]Returned{}}
	if s == nil || len(q.Spaces) == 0 {
		return out, nil
	}
	b := &pgx.Batch{}
	if q.Proposer != uuid.Nil && q.SessionRef != "" {
		f := Filter{Spaces: q.Spaces, Lifecycle: "proposed", Proposer: q.Proposer, SessionRef: q.SessionRef}
		args := append([]any{nil}, filterArgs(f)...)
		args = append(args, q.ProposalLimit)
		b.Queue(hitSelect+` WHERE ($1::text IS NULL) AND `+filterSQL+` ORDER BY m.seq DESC LIMIT $7`, args...).
			Query(func(rows pgx.Rows) error {
				var err error
				out.Proposals, err = pgx.CollectRows(rows, scanHit)
				return err
			})
	}
	if q.Since != nil {
		if q.Forgotten {
			b.Queue(`
				SELECT space_id, object_ref FROM v2.receipts
				 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action = 'forgot' AND recorded_at > $2
				 ORDER BY seq LIMIT 50`, q.Spaces, *q.Since).
				Query(func(rows pgx.Rows) error { return collectRefs(rows, out.Forgotten) })
		}
		b.Queue(`
			SELECT space_id, object_ref, COALESCE(source->>'ref', '') FROM v2.receipts
			 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action = 'returned' AND recorded_at > $2
			 ORDER BY seq LIMIT 50`, q.Spaces, *q.Since).
			Query(func(rows pgx.Rows) error {
				for rows.Next() {
					var id uuid.UUID
					var r Returned
					if err := rows.Scan(&id, &r.Ref, &r.Decision); err != nil {
						return err
					}
					out.Returned[id] = append(out.Returned[id], r)
				}
				return rows.Err()
			})
	}
	if b.Len() == 0 {
		return out, nil
	}
	return out, s.ledger.ReadBatch(ctx, scope, b)
}

// collectRefs reads rows of (space id, ref) into out.
func collectRefs(rows pgx.Rows, out map[uuid.UUID][]string) error {
	for rows.Next() {
		var id uuid.UUID
		var ref string
		if err := rows.Scan(&id, &ref); err != nil {
			return err
		}
		out[id] = append(out[id], ref)
	}
	return rows.Err()
}

// SpaceDigest is the lexical stand-in for a space's compiled digest: its
// top kept memories in each section, and what changed.
type SpaceDigest struct {
	SpaceID  uuid.UUID
	Sections map[ledger.Section][]Hit
	// Changed counts memories kept or edited since the given time.
	Changed int
	// Forgotten lists the memories forgotten since then (refs only: the
	// words are gone).
	Forgotten []string
	// Waiting counts what waits in Review.
	Waiting int
}

// Digest reads each space's digest: up to perSection newest kept
// memories per section, and, when since isn't nil, what changed after it.
// Its statements go out together, in one round trip (ledger.ReadBatch).
func (s *Searcher) Digest(ctx context.Context, scope ledger.Scope, spaces []uuid.UUID, perSection int, since *time.Time) ([]SpaceDigest, error) {
	if s == nil {
		return nil, ledger.ErrDisabled
	}
	if len(spaces) == 0 {
		return nil, nil
	}
	byID := map[uuid.UUID]*SpaceDigest{}
	out := make([]SpaceDigest, len(spaces))
	for i, id := range spaces {
		out[i] = SpaceDigest{SpaceID: id, Sections: map[ledger.Section][]Hit{}}
		byID[id] = &out[i]
	}
	b := &pgx.Batch{}
	b.Queue(`
		SELECT id, seq, space_id, statement, section, kind, state, lifecycle, trust, current_version, updated_at
		  FROM (SELECT m.id, m.seq, m.space_id, COALESCE(v.statement, '') AS statement, m.section, m.kind,
		               m.state, m.lifecycle, m.trust, m.current_version, m.updated_at,
		               row_number() OVER (PARTITION BY m.space_id, m.section ORDER BY m.updated_at DESC, m.seq DESC) AS n
		          FROM v2.memories m
		          LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
		         WHERE m.space_id = ANY($1) AND m.lifecycle = 'kept' AND `+notSuperseded+`) ranked
		 WHERE n <= $2
		 ORDER BY space_id, section, n`, spaces, perSection).
		Query(func(rows pgx.Rows) error {
			hits, err := pgx.CollectRows(rows, scanHit)
			if err != nil {
				return err
			}
			for _, h := range hits {
				d := byID[h.SpaceID]
				d.Sections[h.Section] = append(d.Sections[h.Section], h)
			}
			return nil
		})
	b.Queue(`
		SELECT space_id, count(*) FROM v2.memories m
		 WHERE m.space_id = ANY($1)
		   AND (m.lifecycle = 'proposed' OR (m.lifecycle = 'kept' AND cardinality(m.flags) > 0))
		 GROUP BY space_id`, spaces).
		Query(func(rows pgx.Rows) error {
			return collectCounts(rows, func(id uuid.UUID, n int) { byID[id].Waiting = n })
		})
	if since != nil {
		b.Queue(`
			SELECT space_id, count(DISTINCT object_id) FROM v2.receipts
			 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action IN ('kept', 'edited') AND recorded_at > $2
			 GROUP BY space_id`, spaces, *since).
			Query(func(rows pgx.Rows) error {
				return collectCounts(rows, func(id uuid.UUID, n int) { byID[id].Changed = n })
			})
		b.Queue(`
			SELECT space_id, object_ref FROM v2.receipts
			 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action = 'forgot' AND recorded_at > $2
			 ORDER BY seq`, spaces, *since).
			Query(func(rows pgx.Rows) error {
				forgotten := map[uuid.UUID][]string{}
				if err := collectRefs(rows, forgotten); err != nil {
					return err
				}
				for id, refs := range forgotten {
					byID[id].Forgotten = refs
				}
				return nil
			})
	}
	if err := s.ledger.ReadBatch(ctx, scope, b); err != nil {
		return nil, err
	}
	return out, nil
}

// KeptCounts counts each space's kept memories.
func (s *Searcher) KeptCounts(ctx context.Context, scope ledger.Scope, spaces []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if s == nil || len(spaces) == 0 {
		return out, nil
	}
	err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT space_id, count(*) FROM v2.memories
			 WHERE space_id = ANY($1) AND lifecycle = 'kept' GROUP BY space_id`, spaces)
		if err != nil {
			return err
		}
		return collectCounts(rows, func(id uuid.UUID, n int) { out[id] = n })
	})
	return out, err
}

func collectCounts(rows pgx.Rows, set func(uuid.UUID, int)) error {
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		set(id, n)
	}
	return rows.Err()
}

// IsTimeout reports whether err is a deadline (the recall budget ran out).
func IsTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
