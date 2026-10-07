package v2api

import (
	"encoding/base64"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// The sealed receipt chain (plan 25 §5.3): the worker's sealer writes it
// (internal/sealer); /v2 shows how far it reaches, so Activity and the
// export can say "sealed through receipt 1,284 at 14:02", and serves the
// public keys its checkpoints are signed with.

// WithReceiptKeys serves these public keys beside the checkpoints: the
// current signing key and retired ones (RECEIPT_VERIFY_KEYS).
func WithReceiptKeys(keys receiptchain.Keyring) Option {
	return func(h *Handler) { h.receiptKeys = keys }
}

type checkpointPage struct {
	Items      []ledger.Checkpoint `json:"items"`
	HasMore    bool                `json:"has_more"`
	NextCursor string              `json:"next_cursor,omitempty"`
	Seal       sealStatus          `json:"seal"`
	Keys       []signingKey        `json:"keys"`
}

type sealStatus struct {
	SealedReceipts         int64      `json:"sealed_receipts"`
	SealedThroughSeq       *int64     `json:"sealed_through_seq,omitempty"`
	SealedThroughReceiptID *uuid.UUID `json:"sealed_through_receipt_id,omitempty"`
	HeadSHA256             string     `json:"head_sha256,omitempty"`
	Checkpoints            int64      `json:"checkpoints"`
	SealedAt               *time.Time `json:"sealed_at,omitempty"`
	Unsealed               int        `json:"unsealed"`
	VerifiedAt             *time.Time `json:"verified_at,omitempty"`
	VerifiedReceipts       *int64     `json:"verified_receipts,omitempty"`
	VerifyProblems         *int       `json:"verify_problems,omitempty"`
}

type signingKey struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
}

// GET /v2/spaces/{space}/checkpoints
func (h *Handler) listCheckpoints(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.ListCheckpoints(r.Context(), p.scope, ledger.CheckpointQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := checkpointPage{Items: nonNil(res.Checkpoints), HasMore: res.HasMore, NextCursor: res.NextCursor,
		Seal: sealStatus{Unsealed: res.Unsealed}, Keys: []signingKey{}}
	if hd := res.Head; hd != nil {
		out.Seal.SealedReceipts, out.Seal.Checkpoints, out.Seal.SealedAt = hd.Position, hd.Checkpoints, hd.SealedAt
		out.Seal.VerifiedAt, out.Seal.VerifiedReceipts, out.Seal.VerifyProblems = hd.VerifiedAt, hd.VerifiedPosition, hd.VerifyProblems
		if hd.Position > 0 {
			seq := hd.LastSeq
			out.Seal.SealedThroughSeq, out.Seal.SealedThroughReceiptID, out.Seal.HeadSHA256 = &seq, hd.LastReceiptID, hd.Head
		}
	}
	for id, pub := range h.receiptKeys {
		out.Keys = append(out.Keys, signingKey{KeyID: id, Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	}
	sort.Slice(out.Keys, func(i, j int) bool { return out.Keys[i].KeyID < out.Keys[j].KeyID })
	writeData(w, http.StatusOK, out)
}
