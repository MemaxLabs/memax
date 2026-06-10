package dreams

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (e *Engine) phaseMergeDuplicates(ctx context.Context, hubID string, runID string, pairs []model.SimilarMemoryPair, actorID string) (int, []model.DreamAction, map[string]bool, model.DreamPhaseMetrics) {
	merged := 0
	var actions []model.DreamAction
	processed := make(map[string]bool)
	metrics := model.DreamPhaseMetrics{Candidates: len(pairs)}
	started := time.Now()
	defer func() { metrics.DurationMs = time.Since(started).Milliseconds() }()

	for _, pair := range pairs {
		// Heartbeat at the top of every iteration. Each pair can
		// take tens of seconds (merge LLM + metadata LLM + embed
		// retries), and with up to MaxPairsPerRun (30) pairs the
		// whole loop can exceed the 30-min stale threshold. A
		// per-iteration heartbeat keeps the max quiet window bounded
		// by a single pair's runtime, not the loop's total.
		e.heartbeat(runID)

		if shouldSkipMergedPair(processed, pair) {
			metrics.Skipped++
			continue
		}

		// Skip if either memory is pinned
		if pair.MemoryA.Pinned || pair.MemoryB.Pinned {
			metrics.Skipped++
			continue
		}
		metrics.Processed++
		metrics.LLMCalls++

		// Determine which memory to keep (higher access count = more proven)
		keeper, absorbed := &pair.MemoryA, &pair.MemoryB
		if pair.MemoryB.AccessCount > pair.MemoryA.AccessCount {
			keeper, absorbed = &pair.MemoryB, &pair.MemoryA
		}
		// If equal access, keep the newer one
		if pair.MemoryB.AccessCount == pair.MemoryA.AccessCount && pair.MemoryB.CreatedAt.After(pair.MemoryA.CreatedAt) {
			keeper, absorbed = &pair.MemoryB, &pair.MemoryA
		}

		// LLM merge: combine content intelligently
		mergeResp, err := e.llmMerge(ctx, keeper, absorbed, actorID)
		if err != nil {
			metrics.LLMErrors++
			if isLLMTimeout(err) {
				metrics.LLMTimeouts++
			}
			slog.WarnContext(ctx, "dream: merge failed", "error", err, "keeper", keeper.ID, "absorbed", absorbed.ID)
			continue
		}
		addLLMUsage(&metrics, mergeResp)
		mergedContent := mergeResp.Text

		// Update the keeper memory with merged content
		keeper.Content = mergedContent
		keeper.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(mergedContent)))
		keeper.Version++
		keeper.UpdatedAt = time.Now()
		keeper.AccessCount += absorbed.AccessCount // combine access history

		// Re-synthesize metadata before chunking so denormalized chunk search
		// fields reflect the merged memory's final tags and hint. A failed
		// LLM/parse leaves the keeper's prior metadata in place; we count
		// it so the run demotes to partial_failed instead of reporting
		// stale post-merge metadata as "clean completed". Passing the
		// metrics pointer also closes the merge LLMCalls undercount —
		// the metadata call now increments LLMCalls / tokens attributes
		// to the merge phase.
		if !e.resynthesizeMetadata(ctx, keeper, actorID, &metrics) {
			metrics.Errors++
		}

		// Re-chunk and re-embed the merged memory. embedFailed means
		// chunks are valid but lack vectors — the merge still proceeds
		// (text search stays correct) but we count it so operators see
		// that vector retrieval on this memory is briefly stale until
		// the next embedding pass.
		chunks, embedFailed := e.rechunkAndEmbed(ctx, keeper)
		if embedFailed {
			metrics.Errors++
		}

		if err := e.store.ExecuteDreamMergeInHub(keeper, absorbed.ID, hubID, chunks); err != nil {
			slog.WarnContext(ctx, "dream: failed to execute transactional merge", "error", err)
			metrics.Errors++
			continue
		}

		action := model.DreamAction{
			ID:              generateID(),
			RunID:           runID,
			ActionType:      "merge",
			SourceMemoryIDs: []string{keeper.ID, absorbed.ID},
			ResultMemoryID:  keeper.ID,
			Reason:          fmt.Sprintf("Merged %.0f%% similar memories: \"%s\" absorbed into \"%s\"", pair.Similarity*100, absorbed.Title, keeper.Title),
			Similarity:      pair.Similarity,
			CreatedAt:       time.Now(),
		}
		if err := e.store.CreateDreamAction(&action); err != nil {
			slog.WarnContext(ctx, "dream: failed to record merge action", "error", err, "keeper", keeper.ID)
			metrics.Errors++
		}
		actions = append(actions, action)
		merged++
		metrics.Actions++
		processed[keeper.ID] = true
		processed[absorbed.ID] = true

		slog.InfoContext(ctx, "dream: merged memories",
			"keeper", keeper.ID, "absorbed", absorbed.ID,
			"similarity", fmt.Sprintf("%.2f", pair.Similarity),
			"keeper_title", keeper.Title,
		)
	}

	return merged, actions, processed, metrics
}

// phaseDetectContradictions finds topically related memories with conflicting information.
// Pairs are pre-filtered to the contradiction range (0.70–0.85 similarity).
