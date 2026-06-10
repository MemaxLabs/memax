package dreams

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type restructureSuggestion struct {
	Type     string              `json:"type"`                // "group" or "merge"
	TopicIDs []string            `json:"topic_ids"`           // topics to group or merge
	Parent   *newTopicSuggestion `json:"parent,omitempty"`    // for "group": the new parent topic
	TargetID string              `json:"target_id,omitempty"` // for "merge": keep this topic
	Reason   string              `json:"reason"`
}

// restructureSystemPrompt is the persistent system prompt for the restructure phase.
const restructureSystemPrompt = `You are memax's Dream Engine analyzing a user's knowledge topic tree for structural improvements.

## Your role
You receive the full list of root-level topics with their descriptions, memory counts, and any immediate children. Your job is to identify opportunities to create a better hierarchy by grouping related root topics under new parent topics.

## Core principles
1. **REDUCE ROOT CLUTTER.** A flat list of 10+ root topics is overwhelming. Group related topics under meaningful parents to create a browsable tree.
2. **BE CONSERVATIVE.** Only suggest groupings that are clearly better. When in doubt, leave topics alone. Bad reorganization is worse than no reorganization.
3. **RESPECT SCALE.** A topic with 50 memories is more established than one with 2. Don't casually reparent large topics unless the grouping is clearly right.
4. **NEVER TOUCH user-named or pinned topics.** You can suggest including them in a group, but they will only be moved if the user confirms.

## Output format
Respond with a JSON array of actions:

Group related root topics under a new parent:
{"type": "group", "topic_ids": ["id1", "id2", "id3"], "parent": {"name": "Software Development", "description": "Programming practices, tools, and technical decisions", "icon": "code"}, "reason": "These 3 topics all relate to software development practices"}

Suggest merging near-duplicate topics (creates a user review — NOT auto-applied):
{"type": "merge", "topic_ids": ["id1", "id2"], "target_id": "id1", "reason": "Both topics cover deployment procedures with overlapping memories"}

If no restructuring is needed, return an empty array: []

## Rules
- Only suggest groupings of 2+ topics that share a clear theme.
- New parent topic names: 2-4 words, broad enough to encompass all children.
- New parent descriptions are REQUIRED (1-2 sentences).
- Keep max tree depth at 5 — don't create parents for topics that already have children nested deep.
- Prefer fewer, larger groups over many small ones.
- Icon: a lucide icon name (folder, code, book, layers, etc.)`

const minRootTopicsForRestructure = 8
const restructureCooldownRuns = 7

// phaseRestructure reviews the existing topic tree and reorganizes flat root topics into a hierarchy.
// Runs periodically (not every cycle) when there are enough root topics to warrant restructuring.
func (e *Engine) phaseRestructure(ctx context.Context, hub *model.Hub, runID string, cfg dreamExecutionConfig, actorID string) (int, []model.DreamAction, model.DreamPhaseMetrics) {
	// Load all topics and memory counts
	topics, err := e.store.ListTopics(hub.ID)
	if err != nil {
		slog.WarnContext(ctx, "dream: phaseRestructure failed to load topics", "error", err)
		return 0, nil, model.DreamPhaseMetrics{Errors: 1}
	}
	topicCounts, _ := e.store.CountMemoriesByTopic(store.VisibilityScope{OwnerID: hub.OwnerID, HubIDs: []string{hub.ID}}, hub.ID)

	// Filter to root-level topics
	var roots []model.Topic
	for _, t := range topics {
		if t.ParentID == nil {
			roots = append(roots, t)
		}
	}

	if len(roots) < minRootTopicsForRestructure {
		return 0, nil, model.DreamPhaseMetrics{Candidates: len(roots), Skipped: len(roots)}
	}
	metrics := model.DreamPhaseMetrics{Candidates: len(roots)}
	started := time.Now()
	defer func() { metrics.DurationMs = time.Since(started).Milliseconds() }()
	members, _ := e.store.ListHubMembers(hub.ID)
	recipients := filterNotifiableRecipients(e.store, topicDecisionRecipients(hub, members))

	// Check cooldown: skip if any recent run had restructures
	recentRuns, _ := e.store.ListDreamRunsByHub(hub.ID, restructureCooldownRuns)
	for _, r := range recentRuns {
		if r.ID == runID {
			continue // skip current run
		}
		if r.TopicsRestructured > 0 {
			slog.InfoContext(ctx, "dream: skipping restructure, recent run already restructured", "recent_run", r.ID)
			metrics.Skipped = len(roots)
			return 0, nil, metrics
		}
	}

	// Build children map for context
	childrenOf := make(map[string][]model.Topic)
	for _, t := range topics {
		if t.ParentID != nil {
			childrenOf[*t.ParentID] = append(childrenOf[*t.ParentID], t)
		}
	}

	// Build prompt showing root topics + their immediate children
	var rootDesc strings.Builder
	for _, t := range roots {
		tags := ""
		if t.UserModified {
			tags += " [user-named]"
		}
		if t.Pinned {
			tags += " [pinned]"
		}
		desc := ""
		if t.Description != "" {
			desc = " — " + t.Description
		}
		count := topicCounts[t.ID]
		rootDesc.WriteString(fmt.Sprintf("- [%s] \"%s\"%s%s [%d memories]\n", t.ID, t.Name, desc, tags, count))
		if children, ok := childrenOf[t.ID]; ok {
			for _, child := range children {
				childCount := topicCounts[child.ID]
				rootDesc.WriteString(fmt.Sprintf("  - \"%s\" [%d memories]\n", child.Name, childCount))
			}
		}
	}

	prompt := fmt.Sprintf("## Root-Level Topics (%d total)\n%s\nAnalyze these root-level topics and suggest structural improvements. Return a JSON array of actions.",
		len(roots), rootDesc.String())

	slog.InfoContext(ctx, "dream: phaseRestructure", "root_topics", len(roots))

	metrics.LLMCalls++
	// Branch tracking ctx keeps PostHog flow=dreams + actor_id, while
	// the outer ctx retains the job-level slog attrs (job_id/kind).
	// Pass trackCtx to callLLM so analytics stay intact; logging in
	// this function continues to use the outer ctx for job enrichment.
	trackCtx := e.trackingContextForHub(ctx, hub.ID, hub.OwnerID, actorID, "dreams")
	resp, err := e.callLLMWithModelTimeout(trackCtx, e.organizeModel, restructureSystemPrompt, prompt, 3000, cfg.restructureTimeout, "dreams.restructure")
	if err != nil {
		metrics.LLMErrors++
		if isLLMTimeout(err) {
			metrics.LLMTimeouts++
		}
		slog.WarnContext(ctx, "dream: restructure LLM call failed", "error", err)
		return 0, nil, metrics
	}
	addLLMUsage(&metrics, resp)

	// Parse response
	jsonStr := anthropic.ExtractJSONArray(resp.Text)
	var suggestions []restructureSuggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestions); err != nil {
		slog.WarnContext(ctx, "dream: failed to parse restructure response", "error", err, "raw", resp.Text[:min(len(resp.Text), 200)])
		metrics.LLMErrors++
		return 0, nil, metrics
	}

	restructured := 0
	var actions []model.DreamAction

	// Build topic lookup
	topicByID := make(map[string]*model.Topic, len(topics))
	for i := range topics {
		topicByID[topics[i].ID] = &topics[i]
	}

	for _, s := range suggestions {
		// Heartbeat per suggestion — each one does topic CRUD plus
		// notification fan-out, and the suggestion list comes from
		// an LLM so the total loop can be slow.
		e.heartbeat(runID)

		switch s.Type {
		case "group":
			if s.Parent == nil || s.Parent.Name == "" || len(s.TopicIDs) < 2 {
				continue
			}

			movableTopics, reviewTopics, valid := partitionReparentTopics(s.TopicIDs, topicByID)
			if !valid || len(movableTopics) == 0 {
				continue
			}

			// Ensure description
			if s.Parent.Description == "" {
				s.Parent.Description = fmt.Sprintf("Topics related to %s", s.Parent.Name)
			}
			icon := s.Parent.Icon
			if icon == "" {
				icon = "folder"
			}

			// Create parent topic
			now := time.Now()
			parentTopic := &model.Topic{
				ID:          generateID(),
				OwnerID:     hub.OwnerID,
				HubID:       hub.ID,
				Name:        s.Parent.Name,
				Description: s.Parent.Description,
				Icon:        icon,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := e.store.CreateTopic(parentTopic); err != nil {
				slog.WarnContext(ctx, "dream: failed to create parent topic", "name", s.Parent.Name, "error", err)
				metrics.Errors++
				continue
			}

			// Reparent child topics
			moved := 0
			for _, child := range movableTopics {
				// Check depth won't exceed 5
				depth, _ := e.store.GetTopicDepth(child.ID, hub.ID)
				if depth+1 > 5 {
					continue
				}

				child.ParentID = &parentTopic.ID
				child.UpdatedAt = now
				if err := e.store.UpdateTopic(child); err != nil {
					slog.WarnContext(ctx, "dream: failed to reparent topic", "child", child.ID, "parent", parentTopic.ID, "error", err)
					metrics.Errors++
					continue
				}
				moved++
			}

			if moved == 0 {
				if err := e.store.DeleteTopic(parentTopic.ID, hub.ID); err != nil {
					// Orphan cleanup failure leaves an unintended
					// empty topic in the tree. Count it so the run
					// demotes to partial_failed rather than
					// reporting "completed" with stale clutter.
					slog.WarnContext(ctx, "dream: failed to remove empty restructure parent topic", "topic_id", parentTopic.ID, "error", err)
					metrics.Errors++
				}
				continue
			}

			for _, child := range reviewTopics {
				payload, payloadErr := json.Marshal(model.ReviewTopicRestructurePayload{
					ParentTopic: model.ReviewTopicRef{
						ID:          parentTopic.ID,
						Name:        parentTopic.Name,
						MemoryCount: topicCounts[parentTopic.ID],
					},
					ChildTopic: model.ReviewTopicRef{
						ID:          child.ID,
						Name:        child.Name,
						MemoryCount: topicCounts[child.ID],
					},
					Reason: s.Reason,
				})
				if payloadErr != nil {
					slog.WarnContext(ctx, "dream: failed to marshal topic_restructure payload", "child", child.ID, "parent", parentTopic.ID, "error", payloadErr)
					payload = nil
				}
				// Phase 6: no reviews row. The notification is the
				// inbox row. source_id is the deterministic dedup
				// key from the dream engine so a re-run upserts in
				// place via notifications_source_unique.
				runIDRestructure := runID
				for _, recipientID := range recipients {
					notif := &model.Notification{
						ID:              generateID(),
						Audience:        model.AudienceUser,
						RecipientUserID: recipientID,
						HubID:           hub.ID,
						Kind:            model.NotificationKindReviewTopicRestructure,
						Status:          model.NotificationStatusPending,
						Priority:        0,
						SourceKind:      model.NotificationSourceTopicReview,
						SourceID:        topicRestructureNotificationSourceID(child.ID, parentTopic.Name) + ":" + recipientID,
						DreamRunID:      &runIDRestructure,
						Payload:         payload,
						CreatedAt:       now,
					}
					if err := e.store.CreateNotification(notif); err != nil {
						slog.WarnContext(ctx, "dream: topic_restructure notification create failed", "error", err, "child", child.ID, "parent", parentTopic.ID, "recipient", recipientID)
						metrics.Errors++
						continue
					}
					events.PublishNotificationCreated(ctx, e.events, notif)
				}
			}

			action := model.DreamAction{
				ID:         generateID(),
				RunID:      runID,
				ActionType: "restructure",
				Reason:     fmt.Sprintf("Created \"%s\" and grouped %d topics: %s", parentTopic.Name, moved, s.Reason),
				CreatedAt:  now,
			}
			if err := e.store.CreateDreamAction(&action); err != nil {
				slog.WarnContext(ctx, "dream: failed to record restructure action", "error", err, "parent", parentTopic.ID)
				metrics.Errors++
			}
			actions = append(actions, action)
			restructured += moved
			metrics.Actions += moved

			slog.InfoContext(ctx, "dream: restructured topics", "parent", parentTopic.Name, "moved", moved, "reason", s.Reason)

		case "merge":
			if len(s.TopicIDs) < 2 || s.TargetID == "" {
				continue
			}
			// Create a review for user to confirm (don't auto-merge topics)
			now := time.Now()
			targetTopic, ok := topicByID[s.TargetID]
			if !ok {
				continue
			}
			// Build the source topic list — every TopicIDs entry that
			// is not the target. Skip ids we cannot resolve so the
			// payload does not carry stale references.
			sourceRefs := make([]model.ReviewTopicRef, 0, len(s.TopicIDs))
			for _, id := range s.TopicIDs {
				if id == s.TargetID {
					continue
				}
				src, ok := topicByID[id]
				if !ok {
					continue
				}
				sourceRefs = append(sourceRefs, model.ReviewTopicRef{
					ID:          src.ID,
					Name:        src.Name,
					MemoryCount: topicCounts[src.ID],
				})
			}
			if len(sourceRefs) == 0 {
				// Nothing left to merge after filtering — skip.
				continue
			}
			payload, payloadErr := json.Marshal(model.ReviewTopicMergePayload{
				TargetTopic: model.ReviewTopicRef{
					ID:          targetTopic.ID,
					Name:        targetTopic.Name,
					MemoryCount: topicCounts[targetTopic.ID],
				},
				SourceTopics: sourceRefs,
				Reason:       s.Reason,
			})
			if payloadErr != nil {
				slog.WarnContext(ctx, "dream: failed to marshal topic_merge payload", "target", s.TargetID, "error", payloadErr)
				payload = nil
			}
			// Phase 6: the notification is the inbox row. Dedup
			// key is the stable topic-merge source_id so a repeated
			// suggestion upserts in place.
			runIDMerge := runID
			for _, recipientID := range recipients {
				notif := &model.Notification{
					ID:              generateID(),
					Audience:        model.AudienceUser,
					RecipientUserID: recipientID,
					HubID:           hub.ID,
					Kind:            model.NotificationKindReviewTopicMerge,
					Status:          model.NotificationStatusPending,
					Priority:        0,
					SourceKind:      model.NotificationSourceTopicReview,
					// Phase 3.6b3f v2 — switched to the shared
					// model.BuildTopicMergeNotificationSourceID
					// so chat-proposed merges and dream-proposed
					// merges with the same (target, sources,
					// recipient) collapse to one inbox row. The
					// helper filters the target out of TopicIDs
					// (which on this path can include it,
					// because the LLM emits target inside the
					// source list).
					SourceID:        model.BuildTopicMergeNotificationSourceID(s.TargetID, s.TopicIDs, recipientID),
					DreamRunID:      &runIDMerge,
					Payload:         payload,
					CreatedAt:       now,
				}
				if err := e.store.CreateNotification(notif); err != nil {
					slog.WarnContext(ctx, "dream: topic_merge notification create failed", "error", err, "target", s.TargetID, "recipient", recipientID)
					metrics.Errors++
					continue
				}
				events.PublishNotificationCreated(ctx, e.events, notif)
			}

			action := model.DreamAction{
				ID:         generateID(),
				RunID:      runID,
				ActionType: "restructure",
				Reason:     fmt.Sprintf("Suggested merging %d topics into \"%s\": %s (pending review)", len(s.TopicIDs), targetTopic.Name, s.Reason),
				CreatedAt:  now,
			}
			if err := e.store.CreateDreamAction(&action); err != nil {
				slog.WarnContext(ctx, "dream: failed to record restructure-merge action", "error", err, "target", s.TargetID)
				metrics.Errors++
			}
			actions = append(actions, action)
		}
	}

	return restructured, actions, metrics
}

// selectTopicsForBatch picks topics most relevant to a specific batch of memories.
// Combines embedding-based relevance (via FindRelevantTopicIDs) with top topics by
// memory count for general coverage. Caps at the topic-context budget and
// ensures parent context.
