package v2api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// Search across spaces (plan 25 §5.11, epic 3.3): GET /v2/search runs the
// hybrid search memax_search and recall run (internal/v2recall: the
// lexical lanes, the query embedding with its 120 ms deadline, the exact
// space-scoped KNN above the recall floor, weighted RRF and the rerank
// within 150 ms), over every space the caller reads that is on the V2
// record. Rule 13: only the caller's own spaces, through the ledger's
// scope, so RLS holds as everywhere.
//
// People read what was kept and what waits in Review; an agent reads what
// was kept, and only where its connection is connected, as memax_search
// does. A person's search isn't a read (reads are agents', plan §5.3, and
// Ask records none); an agent's is recorded as memax_search's is, after
// the response.

// WithSearch serves GET /v2/search from the searcher recall and
// memax_search use. Without it (or with a nil one) the endpoint answers
// 503.
func WithSearch(s *v2recall.Searcher) Option { return func(h *Handler) { h.search = s } }

const (
	// searchDefaultLimit is a page's size when the request names none.
	searchDefaultLimit = 20
	// searchMaxQuery bounds the words searched for, as notes' search does.
	searchMaxQuery = ledger.MaxNoteQuery
	// searchMaxSpaces bounds the spaces one search names.
	searchMaxSpaces = 20
	// searchBudget is the search's internal deadline: memax_search's (twice
	// recall's 250 ms), so the plan's p95 under 500 ms holds.
	searchBudget = 500 * time.Millisecond
)

// searchWindow is how far a search ranks: its best 50 matches
// (v2recall.MaxLimit), paged through by cursor.
const searchWindow = v2recall.MaxLimit

type searchHit struct {
	Memory   ledger.Memory    `json:"memory"`
	Receipts []ledger.Receipt `json:"receipts"`
}

type searchPage struct {
	Items      []searchHit `json:"items"`
	HasMore    bool        `json:"has_more"`
	NextCursor string      `json:"next_cursor,omitempty"`
	// Notes come with the first page of a search that asks for them.
	Notes       []ledger.Note `json:"notes,omitempty"`
	LexicalOnly bool          `json:"lexical_only"`
}

// searchRequest is GET /v2/search's query, checked.
type searchRequest struct {
	text         string
	spaces       []string
	kind         ledger.Kind
	includeNotes bool
	limit        int
	offset       int
}

func parseSearch(r *http.Request) (searchRequest, *apiError) {
	q := r.URL.Query()
	req := searchRequest{text: strings.TrimSpace(q.Get("q")), spaces: q["space"], kind: ledger.Kind(q.Get("kind")),
		limit: searchDefaultLimit}
	switch n := utf8.RuneCountInString(req.text); {
	case n == 0:
		return req, invalidRequest("q", "Say what to search for in q.")
	case n > searchMaxQuery:
		return req, invalidRequest("q", "A search is at most 500 characters.")
	}
	if len(req.spaces) > searchMaxSpaces {
		return req, invalidRequest("space", "Name at most 20 spaces, or none to search them all.")
	}
	if req.kind != "" && !req.kind.Valid() {
		return req, invalidRequest("kind", "kind must be fact or decision.")
	}
	if raw := q.Get("include_notes"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return req, invalidRequest("include_notes", "include_notes must be true or false.")
		}
		req.includeNotes = b
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > searchWindow {
			return req, invalidRequest("limit", "limit must be a whole number from 1 to 50.")
		}
		req.limit = n
	}
	if raw := q.Get("cursor"); raw != "" {
		offset, ok := req.decodeCursor(raw)
		if !ok {
			return req, invalidRequest("cursor", "That cursor isn't this search's. Search again without it.")
		}
		req.offset = offset
	}
	return req, nil
}

// fingerprint ties a cursor to its search: the words, the spaces named,
// the kind and whether notes were asked for. Never the words themselves.
func (s searchRequest) fingerprint() string {
	spaces := slices.Clone(s.spaces)
	slices.Sort(spaces)
	sum := sha256.Sum256([]byte(s.text + "\x00" + strings.Join(spaces, "\x01") + "\x00" + string(s.kind) +
		"\x00" + strconv.FormatBool(s.includeNotes)))
	return hex.EncodeToString(sum[:6])
}

// A search's cursor is "s<offset>.<fingerprint>": the next page is the
// same search run again from offset.
func (s searchRequest) cursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("s" + strconv.Itoa(offset) + "." + s.fingerprint()))
}

func (s searchRequest) decodeCursor(raw string) (int, bool) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) < 2 || b[0] != 's' {
		return 0, false
	}
	n, fp, ok := strings.Cut(string(b[1:]), ".")
	if !ok || fp != s.fingerprint() {
		return 0, false
	}
	offset, err := strconv.Atoi(n)
	if err != nil || offset < 1 || offset >= searchWindow {
		return 0, false
	}
	return offset, true
}

// GET /v2/search
func (h *Handler) searchSpaces(w http.ResponseWriter, r *http.Request) {
	req, e := parseSearch(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if h.search == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Search isn't configured on this server."})
		return
	}
	// The query embedding needs only the words: it runs while the caller's
	// principal and spaces resolve (each round trip is about 24 ms).
	emb := h.search.Embed(r.Context(), req.text)
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	grants, e := h.searchGrants(r, p, req.spaces)
	if e != nil {
		writeError(w, e)
		return
	}
	out := searchPage{Items: []searchHit{}, LexicalOnly: true}
	if len(grants) == 0 {
		writeData(w, http.StatusOK, out)
		return
	}
	ids := make([]uuid.UUID, len(grants))
	for i, g := range grants {
		ids[i] = g.SpaceID
	}
	scope := p.scope.Narrow(ids...)

	// Notes don't depend on the memories: they're searched at the same time.
	var notes []ledger.Note
	var notesErr error
	var side sync.WaitGroup
	if req.includeNotes && req.offset == 0 {
		side.Add(1)
		go func() {
			defer side.Done()
			notes, notesErr = h.ledger.SearchNotes(r.Context(), scope, ledger.NoteQuery{SpaceIDs: ids, Text: req.text, Limit: req.limit})
		}()
	}

	lifecycles := []lifecycle.Lifecycle{lifecycle.Kept}
	if p.actor.Kind == policy.ActorPerson {
		lifecycles = append(lifecycles, lifecycle.Proposed)
	}
	// One past the page, so has_more is known, within the window.
	want := min(req.offset+req.limit+1, searchWindow)
	ctx, cancel := context.WithTimeout(r.Context(), searchBudget)
	found, err := h.search.Search(ctx, scope, v2recall.Query{Text: req.text, Limit: want, Embedding: emb,
		Filter: v2recall.Filter{Spaces: ids, Lifecycles: lifecycles, Kind: req.kind}})
	timedOut := ctx.Err() != nil && r.Context().Err() == nil
	cancel()
	if err == nil {
		out.LexicalOnly = found.LexicalOnly
		hits := found.Hits[min(req.offset, len(found.Hits)):]
		if len(hits) > req.limit {
			hits = hits[:req.limit]
			out.HasMore = true
			out.NextCursor = req.cursor(req.offset + req.limit)
		}
		out.Items, err = h.searchHits(r.Context(), scope, hits, lifecycles)
	}
	side.Wait()
	switch {
	case err != nil && (timedOut || v2recall.IsTimeout(err)):
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeBusy, retryAfter: 1,
			message: "Searching took too long. Search again in a moment.", details: &errorDetails{RetryAfter: 1}})
		return
	case err != nil:
		writeError(w, h.fromLedger(r, err))
		return
	case notesErr != nil:
		writeError(w, h.fromLedger(r, notesErr))
		return
	}
	if req.includeNotes && req.offset == 0 {
		out.Notes = nonNil(notes)
	}
	writeData(w, http.StatusOK, out)
	h.recordSearch(p, grants, out.Items)
}

// searchGrants are the spaces a search reads: the ones named (each must
// be the caller's, else 404 as anywhere), or all of them. Only spaces on
// the V2 record have memories to find (a space still on V1 keeps its
// memories in V1 until it switches), and an agent reads only where its
// connection is connected, as memax_search does.
func (h *Handler) searchGrants(r *http.Request, p *principal, named []string) ([]ledger.SpaceGrant, *apiError) {
	grants := p.scope.Spaces
	if len(named) > 0 {
		grants = nil
		for _, key := range named {
			g, e := h.space(r, p, key)
			if e != nil {
				return nil, e
			}
			if !slices.ContainsFunc(grants, func(x ledger.SpaceGrant) bool { return x.SpaceID == g.SpaceID }) {
				grants = append(grants, g)
			}
		}
	}
	out := make([]ledger.SpaceGrant, 0, len(grants))
	for _, g := range grants {
		if !g.OnV2 || !searchReads(p, g) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

// searchReads says whether the caller's search reads a space: a person
// reads every space of theirs; an agent the ones its connection is in
// (paused still reads), as memax_search does.
func searchReads(p *principal, g ledger.SpaceGrant) bool {
	if p.actor.Kind != policy.ActorAgent {
		return true
	}
	c := p.connection
	if c == nil || c.State == ledger.ConnectionDisconnected {
		return false
	}
	_, ok := c.AutonomyIn(g.SpaceID)
	return ok
}

// searchHits reads the hits' memories with their first and latest
// receipts, in rank order, in one round trip. A hit that left the
// lifecycles searched since the search ranked it (forgotten, rejected,
// folded into another) is left out.
func (h *Handler) searchHits(ctx context.Context, scope ledger.Scope, hits []v2recall.Hit, lifecycles []lifecycle.Lifecycle) ([]searchHit, error) {
	out := make([]searchHit, 0, len(hits))
	if len(hits) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ID
	}
	found, err := h.ledger.MemoriesWithReceipts(ctx, scope, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		m, ok := found[id]
		if !ok || !slices.Contains(lifecycles, m.Memory.Lifecycle) {
			continue
		}
		out = append(out, searchHit{Memory: m.Memory, Receipts: nonNil(m.Receipts)})
	}
	return out, nil
}

// recordSearch records an agent's search as a read of what it returned,
// per space, after the response (recordRead does nothing for people).
func (h *Handler) recordSearch(p *principal, grants []ledger.SpaceGrant, items []searchHit) {
	if p.actor.Kind != policy.ActorAgent || len(items) == 0 {
		return
	}
	bySpace := map[uuid.UUID][]uuid.UUID{}
	for _, it := range items {
		bySpace[it.Memory.SpaceID] = append(bySpace[it.Memory.SpaceID], it.Memory.ID)
	}
	for _, g := range grants {
		if ids := bySpace[g.SpaceID]; len(ids) > 0 {
			h.recordRead(p, g, ledger.ReadSearch, ids)
		}
	}
}
