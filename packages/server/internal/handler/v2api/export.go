package v2api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/export"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Export (plan 25 §7.2, rule 14): a person takes a space's whole record as
// a zip archive in the export format (internal/export).
//
// Streamed, not a job. The archive is written as the ledger reads the
// record, from one REPEATABLE READ snapshot, in batches, so memory stays
// bounded whatever the space's size, and nothing is stored on the server:
// a stored export would be one more copy of the words that Forget would
// have to find and purge (an R2 object, a cache), and it would go stale
// the moment anything is kept. A read-only snapshot gets no transaction
// id, so even a long export never holds back the sealer's watermark. The
// plan's sizes (a memory is one statement; a large space is thousands of
// them and tens of thousands of receipts) stream in seconds; the deadline
// below bounds the slowest client. An async job would only pay off for
// spaces orders of magnitude larger, and would need its own Forget path.
const (
	// exportBurst exports at once per person, refilled one every
	// exportRefill: an export reads the whole record, so it is a cost
	// guard, in-process like the near-duplicate check's.
	exportBurst  = 4
	exportRefill = 20 * time.Second
	// exportsInFlight bounds the exports one process streams at a time:
	// each holds a database connection until its client has read it all.
	exportsInFlight = 4
	// exportDeadline bounds one export, slow client included.
	exportDeadline = 10 * time.Minute
)

// exportLimiter is a token bucket per person and a cap on exports in
// flight in the process.
type exportLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	inFlight int
}

// allow takes a token for key, or says how many seconds to wait.
func (l *exportLimiter) allow(key string, now time.Time) (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	if len(l.buckets) > 10000 {
		for k, b := range l.buckets {
			if now.Sub(b.at) > time.Duration(exportBurst)*exportRefill {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: exportBurst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(exportBurst, b.tokens+now.Sub(b.at).Seconds()/exportRefill.Seconds())
	b.at = now
	if b.tokens < 1 {
		wait := int((1-b.tokens)*exportRefill.Seconds()) + 1
		return wait, false
	}
	b.tokens--
	return 0, true
}

// begin takes a place among the exports in flight; done gives it back.
func (l *exportLimiter) begin() (done func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight >= exportsInFlight {
		return nil, false
	}
	l.inFlight++
	return func() {
		l.mu.Lock()
		l.inFlight--
		l.mu.Unlock()
	}, true
}

// POST /v2/spaces/{space}:export
func (h *Handler) exportSpace(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if wait, ok := h.exports.allow(p.actor.ID.String(), h.now()); !ok {
		writeError(w, &apiError{status: http.StatusTooManyRequests, code: codeRateLimited, retryAfter: wait,
			message: fmt.Sprintf("You've exported several times in a row. Export again in %d seconds.", wait),
			details: &errorDetails{RetryAfter: wait}})
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	done, ok := h.exports.begin()
	if !ok {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeBusy, retryAfter: 5,
			message: "Memax is writing other exports right now. Export again in a few seconds.",
			details: &errorDetails{RetryAfter: 5}})
		return
	}
	defer done()

	scope := p.scope.Narrow(sp.SpaceID)
	res, err := h.ledger.Apply(r.Context(), &ledger.Export{Meta: p.meta(scope, key, commandFields{}), SpaceID: sp.SpaceID})
	switch {
	case err != nil:
		writeError(w, h.fromLedger(r, err))
		return
	case res.Outcome == ledger.OutcomeRefused:
		writeError(w, refusal(res.Policy))
		return
	case len(res.Receipts) == 0:
		writeError(w, h.fromLedger(r, errors.New("v2: an export wrote no receipt")))
		return
	}
	receipt := res.Receipts[0].ID

	ctx, cancel := context.WithTimeout(r.Context(), exportDeadline)
	defer cancel()
	zipped := export.NewZipSink(w)
	started := false
	writer := export.NewWriter(zipped, export.Options{Receipt: receipt, Keys: h.exportKeys(),
		OnSpace: func(info ledger.ExportSpaceInfo) error {
			zipped.Start(info.Slug, info.AsOf)
			day := info.AsOf
			if day.IsZero() {
				day = h.now()
			}
			w.Header().Set("Content-Type", export.ZipMediaType)
			w.Header().Set("Content-Disposition",
				fmt.Sprintf(`attachment; filename="memax-%s-%s.zip"`, info.Slug, day.UTC().Format("2006-01-02")))
			w.Header().Set("X-Memax-Export-Receipt", receipt.String())
			w.Header().Set("Cache-Control", "no-store")
			if res.Replayed {
				w.Header().Set("Idempotent-Replayed", "true")
			}
			w.WriteHeader(http.StatusOK)
			started = true
			return nil
		}})
	err = h.ledger.ReadExport(ctx, scope, sp.SpaceID, writer)
	if err == nil {
		err = writer.Finish()
	}
	if err == nil {
		err = zipped.Close()
	}
	if err == nil {
		h.log.InfoContext(r.Context(), "v2: exported a space", "space_id", sp.SpaceID.String(), "receipt", receipt.String(),
			"replayed", res.Replayed)
		return
	}
	if !started {
		writeError(w, h.fromLedger(r, err))
		return
	}
	// The archive is half written: end the response early, so the client
	// sees a failed transfer rather than a zip that can't be opened.
	h.log.ErrorContext(r.Context(), "v2: an export failed partway", "space_id", sp.SpaceID.String(),
		"receipt", receipt.String(), "error", err)
	panic(http.ErrAbortHandler)
}

// exportKeys are the public keys checkpoints.json embeds, by id.
func (h *Handler) exportKeys() []export.SigningKey {
	out := make([]export.SigningKey, 0, len(h.receiptKeys))
	for id, pub := range h.receiptKeys {
		out = append(out, export.SigningKey{KeyID: id, Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KeyID < out[j].KeyID })
	return out
}
