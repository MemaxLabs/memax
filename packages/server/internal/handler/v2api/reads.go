package v2api

import (
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// GET /v2/spaces
func (h *Handler) listSpaces(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	spaces, err := h.ledger.ListSpaces(r.Context(), p.scope)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, spaceList{Items: nonNil(spaces)})
}

// GET /v2/spaces/{space}/memories
func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	q := ledger.MemoryQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit}
	for _, s := range r.URL.Query()["state"] {
		q.States = append(q.States, lifecycle.Mark(s))
	}
	for _, s := range r.URL.Query()["section"] {
		q.Sections = append(q.Sections, ledger.Section(s))
	}
	res, err := h.ledger.ListMemories(r.Context(), p.scope, q)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, memoryPage{Items: nonNil(res.Memories), HasMore: res.HasMore, NextCursor: res.NextCursor})
}

// GET /v2/spaces/{space}/review
func (h *Handler) listReview(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.ReviewQueue(r.Context(), p.scope, ledger.ReviewQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, reviewPage{
		Items: nonNil(res.Memories), HasMore: res.HasMore, NextCursor: res.NextCursor, Total: res.Total,
	})
}

// GET /v2/spaces/{space}/receipts
func (h *Handler) listReceipts(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	q := ledger.ReceiptQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit}
	if ref := r.URL.Query().Get("memory"); ref != "" {
		// Resolve within this space, so a memory elsewhere is not found
		// rather than an empty page.
		m, err := h.ledger.GetMemory(r.Context(), p.scope.Narrow(sp.SpaceID), ref)
		if err != nil {
			writeError(w, h.fromLedger(r, err))
			return
		}
		q.ObjectID = m.ID
	}
	res, err := h.ledger.ListReceipts(r.Context(), p.scope, q)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, toReceiptPage(res))
}

// spaceList is the common start of the /v2/spaces/{space}/… lists.
func (h *Handler) spaceList(r *http.Request) (*principal, ledger.SpaceGrant, string, int, *apiError) {
	p, e := h.principalFor(r)
	if e != nil {
		return nil, ledger.SpaceGrant{}, "", 0, e
	}
	cursor, limit, e := page(r)
	if e != nil {
		return nil, ledger.SpaceGrant{}, "", 0, e
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		return nil, ledger.SpaceGrant{}, "", 0, e
	}
	return p, sp, cursor, limit, nil
}

// GET /v2/memories/{ref}
func (h *Handler) getMemory(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	ref, scope, e := h.target(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	hist, err := h.ledger.GetMemoryHistory(r.Context(), scope, ref)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	setETag(w, hist.Memory)
	writeData(w, http.StatusOK, memoryDetail{
		Memory: hist.Memory, Versions: nonNil(hist.Versions), Receipts: toReceiptPage(hist.Receipts),
	})
}
