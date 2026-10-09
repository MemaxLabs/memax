package v2api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// DraftEmbedder embeds Remember's draft for the near-duplicate check
// (plan 25 §5.8): internal/v2recall.Vectors, with the query model, within
// its 120 ms deadline. Nil means exact repeats only.
type DraftEmbedder interface {
	EmbedDraft(ctx context.Context, statement string) ([]float32, error)
	// Model is the index model the space's embeddings are stored under.
	Model() string
	// NearDuplicateFloor is the least similarity of a near repeat.
	NearDuplicateFloor() float64
}

// WithDrafts serves the near-duplicate check by meaning, not only by
// exact words. Nil (the default) checks exact repeats only.
func WithDrafts(d DraftEmbedder) Option { return func(h *Handler) { h.drafts = d } }

// The near-duplicate check's rate limit, per caller: a person typing, with
// the client debouncing (150 ms), stays well inside it; a script doesn't.
// In-process, so each API machine counts its own (a cost guard, not a
// quota), and no Redis round trip lands on a 150 ms path.
const (
	nearBurst    = 30
	nearPerSec   = 5
	nearMaxLimit = 5
	nearDefault  = 3
	// defaultNearFloor is the floor reported when there is no embedder.
	defaultNearFloor = 0.90
)

type nearDuplicatesRequest struct {
	Statement string `json:"statement"`
	Limit     *int   `json:"limit"`
}

type nearDuplicates struct {
	Items    []nearDuplicate `json:"items"`
	Semantic bool            `json:"semantic"`
	Floor    float64         `json:"floor"`
}

type nearDuplicate struct {
	Memory     *ledger.Memory        `json:"memory"`
	Similarity float64               `json:"similarity"`
	Match      ledger.DuplicateMatch `json:"match"`
	Created    ledger.Receipt        `json:"created"`
}

// POST /v2/spaces/{space}/memories:near-duplicates
func (h *Handler) findNearDuplicates(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if wait, ok := h.near.allow(string(p.actor.Kind)+":"+p.actor.ID.String(), h.now()); !ok {
		writeError(w, &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, retryAfter: wait,
			message: "Too many near-duplicate checks. Check again when the person pauses typing.",
			details: &errorDetails{RetryAfter: wait}})
		return
	}
	var req nearDuplicatesRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	statement := strings.TrimSpace(req.Statement)
	switch n := utf8.RuneCountInString(statement); {
	case n == 0:
		writeError(w, invalidRequest("statement", "Send the draft statement to check."))
		return
	case n > ledger.MaxStatementRunes:
		writeError(w, invalidRequest("statement", "A statement is at most 2000 characters."))
		return
	}
	limit := nearDefault
	if req.Limit != nil {
		if *req.Limit < 1 || *req.Limit > nearMaxLimit {
			writeError(w, invalidRequest("limit", "limit is 1 to 5."))
			return
		}
		limit = *req.Limit
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	q := ledger.NearDuplicateQuery{SpaceID: sp.SpaceID, Statement: statement, Floor: defaultNearFloor, Limit: limit}
	semantic := false
	if h.drafts != nil {
		q.Floor, q.Model = h.drafts.NearDuplicateFloor(), h.drafts.Model()
		vec, err := h.drafts.EmbedDraft(r.Context(), statement)
		if err != nil {
			// Exact repeats still answer; the client hears it wasn't by meaning.
			h.log.WarnContext(r.Context(), "v2: near-duplicate check without its embedding", "metric", "v2_near_duplicate_lexical",
				"error", err)
		} else {
			q.Vector, semantic = vec, true
		}
	}
	found, err := h.ledger.NearDuplicates(r.Context(), p.scope.Narrow(sp.SpaceID), q)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := nearDuplicates{Items: make([]nearDuplicate, len(found)), Semantic: semantic, Floor: q.Floor}
	for i, d := range found {
		out.Items[i] = nearDuplicate{Memory: d.Memory, Similarity: d.Similarity, Match: d.Match, Created: d.Created}
	}
	writeData(w, http.StatusOK, out)
}

// nearLimiter is a token bucket per caller: nearBurst checks at once,
// refilled at nearPerSec.
type nearLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

// allow takes a token for the caller, or says how many seconds to wait.
func (l *nearLimiter) allow(key string, now time.Time) (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	if len(l.buckets) > 10000 {
		for k, b := range l.buckets {
			if now.Sub(b.at) > time.Minute {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: nearBurst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(nearBurst, b.tokens+now.Sub(b.at).Seconds()*nearPerSec)
	b.at = now
	if b.tokens < 1 {
		return 1, false
	}
	b.tokens--
	return 0, true
}
