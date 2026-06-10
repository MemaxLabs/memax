package dreams

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (e *Engine) llmMerge(ctx context.Context, keeper *model.Memory, absorbed *model.Memory, actorID string) (*anthropic.CompleteResponse, error) {
	prompt := fmt.Sprintf(`You are memax's Memory Dreams engine. Two memories are semantically very similar and should be merged into one authoritative version.

Rules:
1. Keep ALL unique information from both memories — do not lose facts.
2. Remove redundant/duplicate sentences.
3. Use the structure of the primary memory as the base.
4. If the memories have different levels of detail, keep the more detailed version.
5. Output ONLY the merged content in markdown. No preamble, no explanation.

Primary memory (higher quality):
Title: %s
Classification: %s/%s
---
%s
---

Secondary memory (to be absorbed):
Title: %s
Classification: %s/%s
---
%s
---

Merged content:`, keeper.Title, keeper.Kind, keeper.Stability, truncateForLLM(keeper.Content),
		absorbed.Title, absorbed.Kind, absorbed.Stability, truncateForLLM(absorbed.Content))

	// trackingContextForMemory layers PostHog tracking metadata ON
	// TOP of the caller's ctx — it's a WithValue ladder, so the
	// slog attrs stashed by the River middleware survive. Job_id
	// propagates into this LLM call's logs and the ctx also carries
	// cancellation from the worker deadline.
	trackCtx := e.trackingContextForMemory(ctx, keeper, actorID, "dreams")
	return e.callLLM(trackCtx, prompt, 2000, "dreams.merge")
}

// llmDetectContradiction asks Claude if two topically related memories contain conflicting information.
// Returns the contradiction verdict, reason, the raw CompleteResponse
// (for token accounting), and any error.
func (e *Engine) llmDetectContradiction(ctx context.Context, memA *model.Memory, memB *model.Memory, actorID string) (bool, string, *anthropic.CompleteResponse, error) {
	prompt := fmt.Sprintf(`You are memax's Memory Dreams engine. Two memories are topically related. Determine if they contain CONTRADICTORY information — facts that cannot both be true.

Rules:
1. Only flag genuine contradictions, not merely different aspects of the same topic.
2. Different opinions or perspectives are NOT contradictions.
3. Outdated information vs newer information IS a contradiction worth flagging.
4. Respond with a JSON object: {"contradiction": true/false, "reason": "explanation"}

Memory A:
Title: %s
---
%s
---

Memory B:
Title: %s
---
%s
---

JSON:`, memA.Title, truncateForLLM(memA.Content), memB.Title, truncateForLLM(memB.Content))

	trackCtx := e.trackingContextForMemory(ctx, memA, actorID, "dreams")
	resp, err := e.callLLM(trackCtx, prompt, 300, "dreams.contradiction_detect")
	if err != nil {
		return false, "", nil, err
	}

	// Parse JSON response
	var result struct {
		Contradiction bool   `json:"contradiction"`
		Reason        string `json:"reason"`
	}

	// Try to extract JSON from response (LLM might wrap it in markdown)
	jsonStr := resp.Text
	if idx := strings.Index(jsonStr, "{"); idx >= 0 {
		if end := strings.LastIndex(jsonStr, "}"); end >= idx {
			jsonStr = jsonStr[idx : end+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return false, "", resp, fmt.Errorf("parse contradiction response: %w", err)
	}

	return result.Contradiction, result.Reason, resp, nil
}

// generateReport creates a human-readable summary of the dream cycle.
