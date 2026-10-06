package v2recall

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
)

// Reciprocal rank fusion, weighted per lane, with V1's constants
// (internal/store/postgres_chunks.go: rrfK and the rrf*W weights), which
// the V1 retrieval eval tuned: the vector lane is the best semantic
// signal, full text the lexical baseline, trigrams the noisiest. V2's
// statements are V1's chunks without the chunking, so the same balance
// applies; retune only against the eval.
const (
	rrfVectorWeight  = 1.5
	rrfFTSWeight     = 1.0
	rrfTrigramWeight = 0.6
)

// laneWeight is a lane's weight in the fusion; an unknown lane counts 1.
func laneWeight(lane string) float64 {
	switch lane {
	case laneVector:
		return rrfVectorWeight
	case "fts":
		return rrfFTSWeight
	case "trigram":
		return rrfTrigramWeight
	}
	return 1
}

// laneVector is the vector lane's name.
const laneVector = "vector"

// ranking is one lane's answer: memory ids, best first.
type ranking struct {
	lane string
	ids  []uuid.UUID
}

// fuse merges lanes by weighted reciprocal rank: score(m) = Σ w_lane /
// (k + rank + 1). The order is total and deterministic: score, then the
// newer memory (uuidv7) first.
func fuse(rankings []ranking) ([]uuid.UUID, map[uuid.UUID]float64) {
	scores := map[uuid.UUID]float64{}
	for _, r := range rankings {
		w := laneWeight(r.lane)
		for i, id := range r.ids {
			scores[id] += w / float64(rrfK+i+1)
		}
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
	return ids, scores
}

// Rerank (plan 25 §5.11): only on more than rerankMin candidates, within
// RerankTimeout (and never past the caller's deadline); on a timeout or an
// error the RRF order stands.
const (
	rerankMin = 8
	// RerankTimeout bounds the reranker on the query path.
	RerankTimeout = 150 * time.Millisecond
	// rerankReserve is left of the caller's deadline for what follows
	// the rerank (formatting the answer).
	rerankReserve = 10 * time.Millisecond
	// rerankFloor is the least time worth calling the reranker with.
	rerankFloor = 20 * time.Millisecond
)

// What happened to a stage, for _meta and logs.
const (
	StageOK      = "ok"
	StageOff     = "off"
	StageTimeout = "timeout"
	StageError   = "error"
	// StageSkipped: the reranker wasn't needed (8 or fewer candidates) or
	// there was no time left for it.
	StageSkipped = "skipped"
)

// rerankHits reorders hits with the reranker: the documents it returns,
// best first, then the rest in their fused order. It reports what
// happened; on anything but ok, hits come back unchanged.
func rerankHits(ctx context.Context, r rerank.Reranker, query string, hits []Hit) ([]Hit, string) {
	if r == nil {
		return hits, StageOff
	}
	if len(hits) <= rerankMin {
		return hits, StageSkipped
	}
	timeout := RerankTimeout
	if dl, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(dl)-rerankReserve)
	}
	if timeout < rerankFloor {
		return hits, StageSkipped
	}
	n := len(hits)
	if top := r.TopN(); top > 0 {
		n = min(n, top)
	}
	docs := make([]rerank.Document, n)
	for i, h := range hits[:n] {
		docs[i] = rerank.Document{ID: h.ID.String(), Content: h.Statement}
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results, err := r.Rerank(rctx, query, docs)
	switch {
	case err != nil && (errors.Is(err, context.DeadlineExceeded) || rctx.Err() != nil):
		return hits, StageTimeout
	case err != nil:
		return hits, StageError
	case len(results) == 0:
		return hits, StageError
	}
	byID := make(map[string]Hit, len(hits))
	for _, h := range hits {
		byID[h.ID.String()] = h
	}
	out := make([]Hit, 0, len(hits))
	taken := map[string]bool{}
	for _, res := range results {
		if h, ok := byID[res.ID]; ok && !taken[res.ID] {
			out = append(out, h)
			taken[res.ID] = true
		}
	}
	for _, h := range hits {
		if !taken[h.ID.String()] {
			out = append(out, h)
		}
	}
	return out, StageOK
}
