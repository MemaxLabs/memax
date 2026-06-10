package dreams

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// phaseOrganize assigns each unassigned memory to a topic — either
// existing or newly created. Phase 2b (plan 24) routes individual
// memories whose trigger fired (KindTopicOverload or
// KindUserFollowup) through agent.Run so the agent can reason
// across the hub's topic structure with the recall_memories tool.
// Non-fired memories continue through the existing batched
// single-call LLM path.
//
// Routing follows the same gates contradict uses (settings flag +
// configured runtime + fired-on-this-memory + cycle budget). The
// agent runs ONE memory at a time (vs the batch model the
// single-call path uses) — per-memory framing matches plan 24's
// agent contract ("the agent reads 'this memory might belong to
// X or Y, decide what to do' and CHOOSES which tool to call").
func (e *Engine) phaseOrganize(
	ctx context.Context,
	hub *model.Hub,
	runID string,
	cfg dreamExecutionConfig,
	actorID string,
	settings map[string]any,
	triggerDecisions map[string]triggers.Decision,
	runBudget *dreamRunBudget,
) (int, []model.DreamAction, model.DreamPhaseMetrics) {
	// Load existing topics
	allTopics, err := e.store.ListTopics(hub.ID)
	if err != nil {
		slog.WarnContext(ctx, "dream: failed to load topics", "error", err)
		return 0, nil, model.DreamPhaseMetrics{Errors: 1}
	}
	topicCounts, _ := e.store.CountMemoriesByTopic(store.VisibilityScope{OwnerID: hub.OwnerID, HubIDs: []string{hub.ID}}, hub.ID)

	// Find unassigned memories
	unassigned, err := e.store.ListUnassignedMemoriesByHub(hub.ID, cfg.organizeCandidateLimit)
	if err != nil {
		slog.WarnContext(ctx, "dream: failed to list unassigned memories", "error", err)
		return 0, nil, model.DreamPhaseMetrics{Errors: 1}
	}
	if len(unassigned) == 0 {
		// Nothing to organize; this is a clean no-op, not a failure.
		return 0, nil, model.DreamPhaseMetrics{}
	}

	slog.InfoContext(ctx, "dream: phaseOrganize", "topics", len(allTopics), "unassigned", len(unassigned))

	organized := 0
	var actions []model.DreamAction
	newTopicsByName := make(map[string]string) // name → ID, tracks topics created this run
	metrics := model.DreamPhaseMetrics{Candidates: len(unassigned)}
	started := time.Now()
	defer func() { metrics.DurationMs = time.Since(started).Milliseconds() }()

	// Phase 2b agentic routing: route + execute inline.
	//
	// Codex review v3 caught the governor-bypass bug in the
	// previous partition-then-execute shape: partitioning all fired
	// memories upfront captured budget state at modelCalls=N before
	// any agent.Run consumed, so a hub at 99/100 model calls could
	// route 50 fired memories through the agent path before any of
	// them updated the counter. The cycle governor's hard cap was
	// effectively bypassed.
	//
	// Fix: decide path per-memory inline, AT THE MOMENT of the
	// routing decision. Each agentic run consumes the budget
	// before the next memory's `shouldRoute` is consulted, so a
	// run that pushes the cycle past the hard cap reliably
	// degrades the next fired memory to the batch path.
	//
	// Fired memories process FIRST so any new topics they create
	// are visible to the batch path's per-batch topic reload —
	// the existing batch path can adopt those topics for non-
	// fired siblings.
	var batchMems []model.Memory
	for _, m := range unassigned {
		if e.shouldRouteOrganizeAgent(settings, triggerDecisions, &m) && runBudget.shouldRoute(runID) {
			// Reload topic state before each agent run so the
			// agent sees the post-create world. Cheap query;
			// typical fired volume per cycle is small.
			topicCounts, _ = e.store.CountMemoriesByTopic(store.VisibilityScope{OwnerID: hub.OwnerID, HubIDs: []string{hub.ID}}, hub.ID)
			allTopics, _ = e.store.ListTopics(hub.ID)
			applied, action, ok := e.agenticOrganizeOne(ctx, hub, runID, &m, allTopics, topicCounts, triggerDecisions, newTopicsByName, &metrics, runBudget, actorID)
			if !ok {
				continue
			}
			if applied {
				organized++
				actions = append(actions, action)
			}
			continue
		}
		batchMems = append(batchMems, m)
	}

	batches := buildOrganizeBatches(batchMems, cfg.organizeBatchSize, cfg.organizePreviewChars)
	metrics.Batches = len(batches)

	// Process in batches
	for i, batch := range batches {
		// Heartbeat at the top of every batch — organize is the
		// slowest phase (Sonnet calls) and batch work is unbounded
		// by count, so per-batch heartbeats are the right cadence.
		e.heartbeat(runID)

		batchPreviewChars := organizeBatchPreviewChars(batch)

		// Reload topics (may have been created in previous batch)
		if i > 0 {
			allTopics, _ = e.store.ListTopics(hub.ID)
			topicCounts, _ = e.store.CountMemoriesByTopic(store.VisibilityScope{OwnerID: hub.OwnerID, HubIDs: []string{hub.ID}}, hub.ID)
		}

		// Select topics relevant to this specific batch (by embedding similarity)
		// plus a small amount of count-based coverage so the prompt stays focused.
		topics := e.selectTopicsForBatch(ctx, hub.ID, allTopics, topicCounts, batch, cfg.organizeTopicLimit)

		metrics.LLMCalls++
		metrics.Attempted += len(batch)
		assignments, organizeResp, err := e.llmOrganize(ctx, topics, batch, topicCounts, len(allTopics), cfg.organizeTimeout, actorID)
		addLLMUsage(&metrics, organizeResp)
		if err != nil {
			metrics.LLMErrors++
			if isLLMTimeout(err) {
				metrics.LLMTimeouts++
				metrics.TimedOutBatches++
			}
			slog.WarnContext(ctx, "dream: organize LLM call failed",
				"error", err,
				"batch_index", i,
				"batch_memories", len(batch),
				"batch_preview_chars", batchPreviewChars,
				"batch_topics", len(topics),
			)
			continue
		}
		metrics.CompletedBatches++
		metrics.Processed += len(batch)
		slog.InfoContext(ctx, "dream: organize batch completed",
			"batch_index", i,
			"batch_memories", len(batch),
			"batch_preview_chars", batchPreviewChars,
			"batch_topics", len(topics),
			"assignments", len(assignments),
		)

		// Track which memories were assigned
		assignedMemories := make(map[string]bool, len(batch))

		for _, a := range assignments {
			if a.Skip {
				continue // will be caught by fallback below
			}

			var topicID string
			var reason string

			if a.TopicID != "" {
				// Assign to existing topic
				topicID = a.TopicID
				// Verify the topic actually exists
				if _, err := e.store.GetTopic(topicID, hub.ID); err == nil {
					reason = a.Reason
				} else {
					slog.WarnContext(ctx, "dream: LLM suggested non-existent topic", "topic_id", topicID)
					continue
				}
			} else if a.NewTopic.Name != "" {
				// Create new topic (or reuse one created earlier in this run)
				if existingID, ok := newTopicsByName[a.NewTopic.Name]; ok {
					topicID = existingID
					reason = a.Reason
				} else {
					now := time.Now()
					icon := a.NewTopic.Icon
					if icon == "" {
						icon = "folder"
					}
					if a.NewTopic.Description == "" {
						slog.WarnContext(ctx, "dream: LLM created topic without description", "name", a.NewTopic.Name)
						a.NewTopic.Description = fmt.Sprintf("Memories related to %s", a.NewTopic.Name)
					}
					newTopic := &model.Topic{
						ID:          generateID(),
						OwnerID:     hub.OwnerID,
						HubID:       hub.ID,
						Name:        a.NewTopic.Name,
						Description: a.NewTopic.Description,
						Icon:        icon,
						ParentID:    a.NewTopic.ParentID,
						CreatedAt:   now,
						UpdatedAt:   now,
					}
					if err := e.store.CreateTopic(newTopic); err != nil {
						slog.WarnContext(ctx, "dream: failed to create topic", "name", a.NewTopic.Name, "error", err)
						metrics.Errors++
						continue
					}
					topicID = newTopic.ID
					newTopicsByName[a.NewTopic.Name] = topicID
					reason = a.Reason
					slog.InfoContext(ctx, "dream: created new topic", "id", topicID, "name", a.NewTopic.Name)
				}
			} else {
				continue
			}

			// Assign memory to topic with auto confidence
			confidence := model.ConfidenceAutoLow
			if a.Confidence > 0 {
				confidence = a.Confidence
				if confidence > model.ConfidenceAutoHigh {
					confidence = model.ConfidenceAutoHigh // cap at auto-high, never overwrite user
				}
			}

			if err := e.store.AssignMemoryToTopic(a.MemoryID, topicID, hub.ID, confidence); err != nil {
				slog.WarnContext(ctx, "dream: failed to assign memory to topic", "error", err)
				metrics.Errors++
				continue
			}

			assignedMemories[a.MemoryID] = true
			// Organize only processes unassigned memories (no prior topic),
			// so FromTopicID is intentionally empty. ToTopicID carries the
			// destination for the lifecycle resolver — ResultMemoryID keeps
			// the same value for backward compatibility with existing
			// readers. ToTopicID is the forward-facing field.
			action := model.DreamAction{
				ID:              generateID(),
				RunID:           runID,
				ActionType:      "organize",
				SourceMemoryIDs: []string{a.MemoryID},
				ResultMemoryID:  topicID,
				ToTopicID:       topicID,
				Reason:          reason,
				CreatedAt:       time.Now(),
				AgentPath:       model.DreamActionAgentPathSingleCall,
			}
			if err := e.store.CreateDreamAction(&action); err != nil {
				slog.WarnContext(ctx, "dream: failed to record organize action", "error", err, "memory_id", a.MemoryID)
				metrics.Errors++
			}
			actions = append(actions, action)
			organized++
			metrics.Actions++
		}

		var unhandled []string
		for _, m := range batch {
			if !assignedMemories[m.ID] {
				unhandled = append(unhandled, m.Title)
			}
		}
		if len(unhandled) > 0 {
			metrics.Skipped += len(unhandled)
			slog.InfoContext(ctx, "dream: leaving memories unassigned after organize batch",
				"count", len(unhandled),
				"titles", strings.Join(unhandled, " | "),
			)
		}
	}

	return organized, actions, metrics
}

func buildOrganizeBatches(memories []model.Memory, maxItems int, maxPreviewChars int) [][]model.Memory {
	if len(memories) == 0 {
		return nil
	}

	if maxItems <= 0 {
		maxItems = organizeBatchMaxItems
	}
	if maxPreviewChars <= 0 {
		maxPreviewChars = organizeBatchMaxPreviewChars
	}

	var batches [][]model.Memory
	current := make([]model.Memory, 0, maxItems)
	currentChars := 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		batch := make([]model.Memory, len(current))
		copy(batch, current)
		batches = append(batches, batch)
		current = make([]model.Memory, 0, maxItems)
		currentChars = 0
	}

	for _, memory := range memories {
		previewChars := len(organizePreview(memory))
		if len(current) > 0 && (len(current) >= maxItems || currentChars+previewChars > maxPreviewChars) {
			flush()
		}
		current = append(current, memory)
		currentChars += previewChars
	}
	flush()

	return batches
}

func organizeBatchPreviewChars(memories []model.Memory) int {
	total := 0
	for _, memory := range memories {
		total += len(organizePreview(memory))
	}
	return total
}

// organizeAssignment is one LLM-suggested assignment from the organize batch call.
//
// Reason is a short natural-language phrase (≤60 chars) explaining WHY
// this memory belongs to the chosen topic. Surfaces verbatim in the web
// DreamActionPopoverBody + detail-page dream history. The LLM is asked
// to emit something that reads as a reason from the user's perspective
// (e.g. "fits the eval harness cluster") — NOT a template like
// `Assigned "X" → topic "Y"`. Empty reason is tolerated; the UI just
// omits the reason line and shows the from → to pills alone.
type organizeAssignment struct {
	MemoryID    string             `json:"memory_id"`
	MemoryTitle string             // populated locally, not from LLM
	TopicID     string             `json:"topic_id,omitempty"`  // existing topic
	NewTopic    newTopicSuggestion `json:"new_topic,omitempty"` // create new
	Skip        bool               `json:"skip,omitempty"`
	Confidence  float64            `json:"confidence,omitempty"`
	Reason      string             `json:"reason,omitempty"`
}

type newTopicSuggestion struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Icon        string  `json:"icon,omitempty"`
	ParentID    *string `json:"parent_id,omitempty"`
}

// restructureSuggestion is one LLM-suggested restructure operation.
