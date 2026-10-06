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
// A query runs on lanes, each returning memory IDs in rank order, and the
// lanes are fused with reciprocal rank fusion. Today the lanes are lexical:
// full-text search over memories.search, and trigram word similarity over
// the statement, so typos and partial words still match. Embeddings for
// V2 memories aren't indexed yet; a vector lane plugs in as one more Lane
// (exact, space-filtered KNN over memories.embedding), and a Reranker runs
// after fusion when one is configured. The LLM query distiller is never on
// this path.
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

// Reranker reorders fused hits, within its own deadline. Nil means none.
type Reranker interface {
	Rerank(ctx context.Context, query string, hits []Hit) ([]Hit, error)
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
}

// Searcher runs queries on the ledger.
type Searcher struct {
	ledger *ledger.Ledger
	lanes  []Lane
	rerank Reranker
}

// New returns a Searcher with the lexical lanes, or nil without a ledger.
func New(l *ledger.Ledger) *Searcher {
	if l == nil {
		return nil
	}
	return &Searcher{ledger: l, lanes: []Lane{FullText{}, Trigram{}}}
}

// WithLane adds a lane (the vector lane, when V2 embeddings are indexed).
func (s *Searcher) WithLane(l Lane) *Searcher { s.lanes = append(s.lanes, l); return s }

// WithReranker sets the reranker.
func (s *Searcher) WithReranker(r Reranker) *Searcher { s.rerank = r; return s }

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
	var res Result
	res.LexicalOnly = true
	err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
		scores := map[uuid.UUID]float64{}
		for _, lane := range s.lanes {
			// A fallback lane (trigrams, for typos) runs only when the lanes
			// before it found fewer than the results asked for: scoring
			// every statement's trigrams is the expensive part of a recall.
			if fb, ok := lane.(interface{ Fallback() bool }); ok && fb.Fallback() && len(scores) >= q.Limit {
				continue
			}
			ids, err := lane.Rank(ctx, tx, q, pool)
			if err != nil {
				return fmt.Errorf("v2recall: %s lane: %w", lane.Name(), err)
			}
			if lane.Name() == "vector" && len(ids) > 0 {
				res.LexicalOnly = false
			}
			for i, id := range ids {
				scores[id] += 1 / float64(rrfK+i+1)
			}
		}
		if len(scores) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(scores))
		for id := range scores {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if scores[ids[i]] != scores[ids[j]] {
				return scores[ids[i]] > scores[ids[j]]
			}
			return ids[i].String() > ids[j].String() // uuidv7: newer first on ties
		})
		if len(ids) > q.Limit {
			ids = ids[:q.Limit]
		}
		hits, err := loadHits(ctx, tx, q.Filter.Spaces, ids)
		if err != nil {
			return err
		}
		for i := range hits {
			hits[i].Score = scores[hits[i].ID]
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
		res.Hits = hits
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if s.rerank != nil && len(res.Hits) > 8 {
		if reranked, err := s.rerank.Rerank(ctx, q.Text, res.Hits); err == nil {
			res.Hits = reranked
		}
	}
	return res, nil
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

// filterSQL is the WHERE clause every lane shares, from $2 on: spaces,
// lifecycle, kind, proposer, session.
const filterSQL = `m.space_id = ANY($2) AND m.lifecycle = $3 AND ($4 = '' OR m.kind = $4)
	AND ($5::uuid IS NULL OR EXISTS (
	      SELECT 1 FROM v2.receipts r
	       WHERE r.id = m.created_receipt_id AND r.space_id = m.space_id
	         AND r.actor_kind = 'agent' AND r.actor_id = $5 AND r.session_ref = $6))`

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

// Rank implements Lane.
func (FullText) Rank(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]uuid.UUID, error) {
	tsq := tsQuery(q.Text)
	if tsq == "" {
		return nil, nil
	}
	args := append([]any{tsq}, filterArgs(q.Filter)...)
	args = append(args, limit)
	rows, err := tx.Query(ctx, `
		SELECT m.id FROM v2.memories m, to_tsquery('simple', public.immutable_unaccent($1)) tq
		 WHERE m.search @@ tq AND `+filterSQL+`
		 ORDER BY ts_rank_cd(m.search, tq) DESC, m.seq DESC
		 LIMIT $7`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
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

// Rank implements Lane.
func (Trigram) Rank(ctx context.Context, tx pgx.Tx, q Query, limit int) ([]uuid.UUID, error) {
	text := normalize(q.Text)
	if utf8.RuneCountInString(text) < 3 {
		return nil, nil
	}
	// The <% operator is word similarity at the transaction's threshold,
	// and memory_versions_trgm_idx (migration 033) serves it, so only
	// statements that share trigrams with the query are scored.
	if _, err := tx.Exec(ctx, `SELECT set_config('pg_trgm.word_similarity_threshold', $1, true)`,
		fmt.Sprintf("%g", trigramThreshold)); err != nil {
		return nil, err
	}
	args := append([]any{text}, filterArgs(q.Filter)...)
	args = append(args, limit)
	rows, err := tx.Query(ctx, `
		WITH q AS (SELECT public.immutable_unaccent($1) AS t)
		SELECT m.id
		  FROM q, v2.memory_versions v
		  JOIN v2.memories m ON m.id = v.memory_id AND v.version = m.current_version
		 WHERE q.t <% public.immutable_unaccent(lower(v.statement))
		   AND v.space_id = ANY($2) AND `+filterSQL+`
		 ORDER BY word_similarity(q.t, public.immutable_unaccent(lower(v.statement))) DESC, m.seq DESC
		 LIMIT $7`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
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

// SessionProposals returns one connection's pending proposals from one
// session, newest first: read-after-write for the proposer (plan 25
// §5.6). A session's own proposals are few, so they all come back rather
// than being ranked against the query.
func (s *Searcher) SessionProposals(ctx context.Context, scope ledger.Scope, spaces []uuid.UUID, proposer uuid.UUID, sessionRef string, limit int) ([]Hit, error) {
	if s == nil || proposer == uuid.Nil || sessionRef == "" || len(spaces) == 0 {
		return nil, nil
	}
	f := Filter{Spaces: spaces, Lifecycle: "proposed", Proposer: proposer, SessionRef: sessionRef}
	var hits []Hit
	err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
		args := append([]any{nil}, filterArgs(f)...)
		args = append(args, limit)
		rows, err := tx.Query(ctx, hitSelect+` WHERE ($1::text IS NULL) AND `+filterSQL+` ORDER BY m.seq DESC LIMIT $7`, args...)
		if err != nil {
			return err
		}
		hits, err = pgx.CollectRows(rows, scanHit)
		return err
	})
	return hits, err
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
	err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, seq, space_id, statement, section, kind, state, lifecycle, trust, current_version, updated_at
			  FROM (SELECT m.id, m.seq, m.space_id, COALESCE(v.statement, '') AS statement, m.section, m.kind,
			               m.state, m.lifecycle, m.trust, m.current_version, m.updated_at,
			               row_number() OVER (PARTITION BY m.space_id, m.section ORDER BY m.updated_at DESC, m.seq DESC) AS n
			          FROM v2.memories m
			          LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
			         WHERE m.space_id = ANY($1) AND m.lifecycle = 'kept') ranked
			 WHERE n <= $2
			 ORDER BY space_id, section, n`, spaces, perSection)
		if err != nil {
			return err
		}
		hits, err := pgx.CollectRows(rows, scanHit)
		if err != nil {
			return err
		}
		for _, h := range hits {
			d := byID[h.SpaceID]
			d.Sections[h.Section] = append(d.Sections[h.Section], h)
		}
		rows, err = tx.Query(ctx, `
			SELECT space_id, count(*) FROM v2.memories m
			 WHERE m.space_id = ANY($1)
			   AND (m.lifecycle = 'proposed' OR (m.lifecycle = 'kept' AND cardinality(m.flags) > 0))
			 GROUP BY space_id`, spaces)
		if err != nil {
			return err
		}
		if err := collectCounts(rows, func(id uuid.UUID, n int) { byID[id].Waiting = n }); err != nil {
			return err
		}
		if since == nil {
			return nil
		}
		rows, err = tx.Query(ctx, `
			SELECT space_id, count(DISTINCT object_id) FROM v2.receipts
			 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action IN ('kept', 'edited') AND recorded_at > $2
			 GROUP BY space_id`, spaces, *since)
		if err != nil {
			return err
		}
		if err := collectCounts(rows, func(id uuid.UUID, n int) { byID[id].Changed = n }); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT space_id, object_ref FROM v2.receipts
			 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action = 'forgot' AND recorded_at > $2
			 ORDER BY seq`, spaces, *since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var ref string
			if err := rows.Scan(&id, &ref); err != nil {
				return err
			}
			byID[id].Forgotten = append(byID[id].Forgotten, ref)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ForgottenSince lists memories forgotten in the spaces after since, as
// space id → refs. Forget notices for recalls with a query.
func (s *Searcher) ForgottenSince(ctx context.Context, scope ledger.Scope, spaces []uuid.UUID, since time.Time) (map[uuid.UUID][]string, error) {
	out := map[uuid.UUID][]string{}
	if s == nil || len(spaces) == 0 {
		return out, nil
	}
	err := s.ledger.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT space_id, object_ref FROM v2.receipts
			 WHERE space_id = ANY($1) AND object_kind = 'memory' AND action = 'forgot' AND recorded_at > $2
			 ORDER BY seq LIMIT 50`, spaces, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var ref string
			if err := rows.Scan(&id, &ref); err != nil {
				return err
			}
			out[id] = append(out[id], ref)
		}
		return rows.Err()
	})
	return out, err
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
