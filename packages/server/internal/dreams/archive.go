package dreams

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/decay"
)

func (e *Engine) phaseArchiveStale(ctx context.Context, hubID string, runID string) (int, []model.DreamAction, model.DreamPhaseMetrics) {
	memories, err := e.store.ListArchiveCandidates(hubID, 60, 500)
	if err != nil {
		// Load failure — record the error so the run demotes to
		// partial_failed instead of finalizing as a clean completed
		// with zero archives.
		return 0, nil, model.DreamPhaseMetrics{Errors: 1}
	}

	archived := 0
	var actions []model.DreamAction
	metrics := model.DreamPhaseMetrics{Candidates: len(memories)}
	started := time.Now()
	defer func() { metrics.DurationMs = time.Since(started).Milliseconds() }()

	for _, mem := range memories {
		metrics.Processed++
		// Skip no-decay stable memories.
		halfLife, ok := decay.StabilityHalfLife[mem.Stability]
		if ok && halfLife <= 0 {
			metrics.Skipped++
			continue
		}

		daysSinceAccess := time.Since(mem.AccessedAt).Hours() / 24.0
		decayMul := decay.Multiplier(daysSinceAccess, mem.AccessCount, mem.Stability)

		// Archive if at decay floor (SQL already ensures: not pinned, access_count=0, age>60 days)
		if decayMul <= StalenessMaxDecay {
			if err := e.store.ArchiveMemoryInHub(mem.ID, hubID); err != nil {
				// Archive mutation failed — don't silently swallow.
				// Count it so the run demotes to partial_failed.
				metrics.Errors++
				continue
			}

			daysSinceCreation := time.Since(mem.CreatedAt).Hours() / 24.0
			action := model.DreamAction{
				ID:              generateID(),
				RunID:           runID,
				ActionType:      "archive",
				SourceMemoryIDs: []string{mem.ID},
				Reason:          fmt.Sprintf("Archived stale memory \"%s\" (decay=%.2f, access_count=0, age=%d days)", mem.Title, decayMul, int(daysSinceCreation)),
				CreatedAt:       time.Now(),
			}
			if err := e.store.CreateDreamAction(&action); err != nil {
				slog.WarnContext(ctx, "dream: failed to record archive action", "error", err, "memory_id", mem.ID)
				metrics.Errors++
			}
			actions = append(actions, action)
			archived++
			metrics.Actions++
		} else {
			metrics.Skipped++
		}
	}

	return archived, actions, metrics
}

// phaseOrganize assigns unassigned memories to topics using the LLM.
// Batches are bounded by both item count and preview size so longer memories
// don't create oversized prompts that time out.
