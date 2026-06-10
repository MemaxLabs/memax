package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (e *Engine) selectTopicsForBatch(ctx context.Context, hubID string, allTopics []model.Topic, counts map[string]int, batch []model.Memory, maxTopics int) []model.Topic {
	if maxTopics <= 0 {
		maxTopics = organizeTopicContextLimit
	}
	// If few topics, just remove dead ones and return all.
	// Always hide "Miscellaneous" from the LLM — it's a fallback bucket, not a real topic.
	// If the LLM sees it, it will pile memories into it instead of creating proper topics.
	isMiscellaneous := func(t model.Topic) bool {
		return t.Name == "Miscellaneous" || t.Name == "Other" || t.Name == "Uncategorized"
	}

	if len(allTopics) <= maxTopics {
		var active []model.Topic
		for _, t := range allTopics {
			if isMiscellaneous(t) {
				continue
			}
			if counts[t.ID] > 0 || t.UserModified {
				active = append(active, t)
			}
		}
		return active
	}

	included := make(map[string]bool, maxTopics)

	// 1. Find topics relevant to this batch by embedding similarity
	memoryIDs := make([]string, len(batch))
	for i, m := range batch {
		memoryIDs[i] = m.ID
	}
	relevantIDs, err := e.store.FindRelevantTopicIDsByHub(hubID, memoryIDs, maxTopics)
	if err != nil {
		slog.WarnContext(ctx, "dream: FindRelevantTopicIDs failed, falling back to top-by-count", "error", err)
	}
	// Build Miscellaneous ID lookup so we can exclude it everywhere
	miscIDs := make(map[string]bool)
	for _, t := range allTopics {
		if isMiscellaneous(t) {
			miscIDs[t.ID] = true
		}
	}

	for _, id := range relevantIDs {
		if !miscIDs[id] {
			included[id] = true
		}
	}

	// 2. Fill remaining slots with top topics by memory count for general coverage.
	// At least 10, but more if embedding relevance didn't fill 50.
	coverageSlots := maxTopics - len(included)
	if coverageSlots < organizeTopicCoverageFloor {
		coverageSlots = organizeTopicCoverageFloor
	}
	sorted := make([]model.Topic, len(allTopics))
	copy(sorted, allTopics)
	sort.Slice(sorted, func(i, j int) bool {
		return counts[sorted[i].ID] > counts[sorted[j].ID]
	})
	added := 0
	for _, t := range sorted {
		if added >= coverageSlots {
			break
		}
		if miscIDs[t.ID] {
			continue
		}
		if !included[t.ID] && (counts[t.ID] > 0 || t.UserModified) {
			included[t.ID] = true
			added++
		}
	}

	// 3. Build result from allTopics (preserving original order) + ensure parents
	topicByID := make(map[string]model.Topic, len(allTopics))
	for _, t := range allTopics {
		topicByID[t.ID] = t
	}

	var filtered []model.Topic
	for _, t := range allTopics {
		if included[t.ID] {
			filtered = append(filtered, t)
		}
	}

	// Cap at topic-context budget.
	if len(filtered) > maxTopics {
		filtered = filtered[:maxTopics]
	}

	// Ensure parent topics are included for tree context
	for _, t := range filtered {
		if t.ParentID != nil && !included[*t.ParentID] {
			if pt, ok := topicByID[*t.ParentID]; ok {
				filtered = append(filtered, pt)
				included[pt.ID] = true
			}
		}
	}

	return filtered
}

// organizeSystemPrompt is the persistent system prompt for the organize phase.
// Emphasizes hierarchy, required descriptions, and concrete JSON examples with parent_id.
const organizeSystemPrompt = `You are memax's Dream Engine — the intelligence that organizes a user's personal knowledge into a browsable topic tree.

## Your role
You receive batches of unassigned memories and a snapshot of the existing topic tree. For each memory, you decide where it belongs. Your goal is to build a topic tree that a human would find intuitive and useful for browsing their knowledge.

## Core principles
1. **EVERY memory gets a proper topic.** Assign every memory to a meaningful topic — either existing or new. Even a single memory can justify creating a new topic if it represents a distinct knowledge area. NEVER use "Miscellaneous", "Other", "General", or catch-all buckets. If you can't find a good fit, create a specific topic that describes what the memory is actually about.
2. **BUILD HIERARCHY.** A flat list of many root topics is a failure state. When you create a new topic, nest it under an existing parent if one makes sense. Think: would a human browsing this tree expect to find this topic under a parent? Group related topics together.
3. **DESCRIPTIONS ARE REQUIRED.** Every new topic MUST have a description (1-2 sentences explaining what belongs there). Descriptions help future batches make better assignments.
4. **RESPECT USER OWNERSHIP.** Topics marked [user-named] were customized by the human. NEVER rename them. You CAN assign memories to them and create subtopics under them.
5. **READ THE CONTENT.** Pay close attention to each memory's title AND content preview. The title often reveals the subject clearly. Don't lump different subjects together just because they share a retrieval kind.

## Output format
Respond with a JSON array. Each element is one of:

Assign to existing topic:
{"memory_id": "abc-123", "topic_id": "existing-uuid", "confidence": 0.6, "reason": "fits the retrieval eval cluster"}

Create new topic and assign (description is REQUIRED):
{"memory_id": "abc-123", "new_topic": {"name": "Deploy Runbooks", "description": "Step-by-step deployment procedures and rollback guides", "icon": "rocket", "parent_id": "parent-uuid"}, "confidence": 0.5, "reason": "first runbook we have, deserves its own home"}

## Rules
- **EVERY memory MUST be assigned.** You MUST return exactly one entry per memory in the batch. Missing entries = bug.
- **NEVER create or assign to "Miscellaneous", "Other", "Uncategorized", or similar catch-all topics.** Every memory deserves a specific, descriptive topic.
- confidence: 0.3 = weak match, 0.5 = decent, 0.7 = strong. Never exceed 0.7.
- **reason: REQUIRED.** One short phrase (≤60 chars) explaining WHY this memory belongs to the chosen topic, from the user's perspective. The UI shows this verbatim as "why dream moved this" on the memory row.
  - Good: "fits the retrieval eval cluster", "core deployment runbook", "same API surface as other auth notes"
  - Bad: "Assigned to topic" (tautological), "memory_id abc belongs here" (mentions IDs), "This memory is about X" (rephrases the title), long sentences (>60 chars). **NEVER include memory titles or topic names in the reason — the UI already shows both.**
- Topic names: 2-4 words, title case, human-readable. Be specific — "API Security" not "Technical Stuff".
- Icon: a lucide icon name (folder, code, book, rocket, globe, wrench, heart, star, lightbulb, database, terminal, server, shield, zap, layers, git-branch, etc.)
- Max tree depth is 5. Use parent_id to nest under existing topics when it creates a logical grouping.
- If you create a new topic, reuse it for other memories in the same batch that fit.
- When multiple memories relate to a broad area with no parent topic, create the parent and nest under it.

## Example
Given topics: "Backend" (id: t1), "Frontend" (id: t2)
Given memories about Redis caching, Postgres indexing, React hooks:

[
  {"memory_id": "m1", "new_topic": {"name": "Caching Strategy", "description": "Redis caching patterns, cache invalidation, and TTL policies", "icon": "database", "parent_id": "t1"}, "confidence": 0.6, "reason": "first caching note, deserves its own cluster"},
  {"memory_id": "m2", "new_topic": {"name": "Database Optimization", "description": "PostgreSQL indexing, query planning, and performance tuning", "icon": "server", "parent_id": "t1"}, "confidence": 0.6, "reason": "query perf, fits under Backend"},
  {"memory_id": "m3", "topic_id": "t2", "confidence": 0.5, "reason": "React hook pattern, core frontend concern"}
]

Notice: Caching Strategy and Database Optimization are nested under Backend (parent_id: "t1"), not created as root topics. Each assignment carries a short user-facing reason.`

// llmOrganize sends a batch of unassigned memories + filtered topic tree to the LLM
// and receives assignment suggestions. Uses Sonnet model with system prompt for better reasoning.
func (e *Engine) llmOrganize(ctx context.Context, topics []model.Topic, memories []model.Memory, topicCounts map[string]int, totalTopicCount int, timeout time.Duration, actorID string) ([]organizeAssignment, *anthropic.CompleteResponse, error) {
	// Build topic tree description (already filtered/capped by caller)
	var topicDesc strings.Builder
	if len(topics) == 0 {
		topicDesc.WriteString("No topics exist yet. Create new topics as needed.\n")
	} else {
		nameByID := make(map[string]string, len(topics))
		for _, t := range topics {
			nameByID[t.ID] = t.Name
		}
		for _, t := range topics {
			indent := ""
			path := ""
			if t.ParentID != nil {
				indent = "  "
				if pName, ok := nameByID[*t.ParentID]; ok {
					path = pName + " > "
				}
			}
			userTag := ""
			if t.UserModified {
				userTag = " [user-named]"
			}
			count := topicCounts[t.ID]
			desc := ""
			if t.Description != "" {
				desc = " — " + t.Description
			}
			topicDesc.WriteString(fmt.Sprintf("%s- [%s] %s%s%s%s [%d memories]\n", indent, t.ID, path, t.Name, desc, userTag, count))
		}
		if totalTopicCount > len(topics) {
			topicDesc.WriteString(fmt.Sprintf("\n(%d additional topics not shown — assign to listed topics or create new ones)\n", totalTopicCount-len(topics)))
		}
	}

	// Build memory list from summary/hint/content, keeping previews compact to control token cost.
	var memDesc strings.Builder
	for _, m := range memories {
		preview := organizePreview(m)
		memDesc.WriteString(fmt.Sprintf("- [%s] \"%s\" (%s/%s)\n  %s\n", m.ID, m.Title, m.Kind, m.Stability, preview))
	}

	prompt := fmt.Sprintf("## Existing Topics (%d total, %d shown)\n%s\n## Unassigned Memories (batch of %d)\n%s\nRespond with a JSON array only.",
		totalTopicCount, len(topics), topicDesc.String(), len(memories), memDesc.String())

	trackCtx := e.trackingContextForMemories(ctx, memories, actorID, "dreams")
	resp, err := e.callLLMWithModelTimeout(trackCtx, e.organizeModel, organizeSystemPrompt, prompt, 4000, timeout, "dreams.organize")
	if err != nil {
		return nil, nil, err
	}

	// Parse JSON array from response
	jsonStr := anthropic.ExtractJSONArray(resp.Text)
	var assignments []organizeAssignment
	if err := json.Unmarshal([]byte(jsonStr), &assignments); err != nil {
		return nil, resp, fmt.Errorf("parse organize response: %w (raw: %.200s)", err, resp.Text)
	}

	// Attach memory titles for logging
	titleByID := make(map[string]string, len(memories))
	for _, m := range memories {
		titleByID[m.ID] = m.Title
	}
	for i := range assignments {
		assignments[i].MemoryTitle = titleByID[assignments[i].MemoryID]
	}

	return assignments, resp, nil
}

func organizePreview(m model.Memory) string {
	preview := strings.TrimSpace(m.Summary)
	if preview == "" {
		preview = strings.TrimSpace(m.Hint)
	}
	if preview == "" {
		preview = strings.TrimSpace(m.Content)
	}
	if len(preview) > 220 {
		preview = preview[:220] + "..."
	}
	return strings.ReplaceAll(preview, "\n", " ")
}

func dreamReportWarnings(run *model.DreamRun) []string {
	if run == nil {
		return nil
	}

	var warnings []string
	if run.Mode == dreamModeBootstrap {
		warnings = append(warnings, "Bootstrap mode is active for this run to process a larger inbox backlog.")
	}
	if metrics, ok := run.PhaseMetrics["organize"]; ok {
		remaining := metrics.Candidates - run.MemoriesOrganized
		if remaining < 0 {
			remaining = 0
		}
		totalBatches := metrics.Batches
		if totalBatches == 0 {
			totalBatches = metrics.LLMCalls
		}
		switch {
		case metrics.LLMTimeouts > 0:
			warnings = append(warnings,
				fmt.Sprintf("Organize timed out on %d of %d batch(es); %d inbox memory(ies) remain unassigned.",
					max(metrics.TimedOutBatches, metrics.LLMTimeouts), totalBatches, remaining))
		case metrics.LLMErrors > 0:
			warnings = append(warnings,
				fmt.Sprintf("Organize failed on %d of %d batch(es); %d inbox memory(ies) remain unassigned.",
					metrics.LLMErrors, totalBatches, remaining))
		case metrics.Candidates > 0 && run.MemoriesOrganized == 0:
			warnings = append(warnings,
				fmt.Sprintf("Organize reviewed %d inbox memory(ies) but did not assign any topics.",
					metrics.Candidates))
		}
	}
	return warnings
}

func shouldSkipMergedPair(processed map[string]bool, pair model.SimilarMemoryPair) bool {
	return processed[pair.MemoryA.ID] || processed[pair.MemoryB.ID]
}

func partitionReparentTopics(topicIDs []string, topicByID map[string]*model.Topic) (movable []*model.Topic, review []*model.Topic, valid bool) {
	for _, id := range topicIDs {
		t, ok := topicByID[id]
		if !ok || t.ParentID != nil {
			return nil, nil, false
		}
		if t.UserModified || t.Pinned {
			review = append(review, t)
			continue
		}
		movable = append(movable, t)
	}
	return movable, review, true
}

// contradictionNotificationSourceID produces the deterministic
// dedup key used as the notification source_id for a contradiction
// between two memories. The key is order-independent so re-processing
// (A, B) and (B, A) collapses to the same notification row via the
// notifications_source_unique constraint.
func contradictionNotificationSourceID(idA, idB string) string {
	ids := []string{idA, idB}
	sort.Strings(ids)
	return "contradiction:" + strings.Join(ids, ":")
}

// topicRestructureNotificationSourceID is the stable dedup key for a
// topic-restructure review notification. Keyed on (child, parent)
// so a repeated suggestion for the same reparenting upserts in place.
func topicRestructureNotificationSourceID(childID, parentName string) string {
	return "topic_restructure:" + childID + ":" + strings.ToLower(parentName)
}

// topicMergeNotificationSourceID is a thin shim over
// model.BuildTopicMergeNotificationSourceID kept for test
// callers in this package that pre-date the chat-share. It
// passes the empty string for recipient so the order-
// independence + no-mutation invariants the tests assert can
// continue to hold without binding to a recipient identity.
// Production dream-engine code uses
// model.BuildTopicMergeNotificationSourceID directly with the
// real recipient (see restructure.go).
func topicMergeNotificationSourceID(targetID string, topicIDs []string) string {
	return model.BuildTopicMergeNotificationSourceID(targetID, topicIDs, "")
}

func isLLMTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// rechunkAndEmbed deletes old chunks and creates new ones for a merged memory.
