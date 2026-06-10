package dreams

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// reviewMemoryRefTitle picks the best human-readable label for a
// memory inside a review_contradiction notification payload. Memory
// titles are optional at ingest time (quick captures, pipe-in dumps,
// source files without a first heading) so a raw Memory.Title can be
// empty. When it is, fall back to the LLM-generated Summary, then to
// a compact prefix of the raw Content. The client uses this string as
// both the inbox row headline and the link label in the expanded
// contradiction body, so leaving it empty lets the UI render raw
// UUIDs — which is what the "memory shows as id, not title" bug was.
func reviewMemoryRefTitle(m *model.Memory) string {
	if m == nil {
		return ""
	}
	if t := strings.TrimSpace(m.Title); t != "" {
		return t
	}
	if s := strings.TrimSpace(m.Summary); s != "" {
		return compactPreview(s, 80)
	}
	if c := strings.TrimSpace(m.Content); c != "" {
		return compactPreview(c, 80)
	}
	return ""
}

// compactPreview collapses whitespace and truncates to `max` chars,
// appending an ellipsis if truncation occurred. Matches the one-line
// preview convention used elsewhere in the dreams pipeline.
func compactPreview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// phaseDetectContradictions walks every contradiction-candidate
// pair and asks an LLM whether the pair contradicts. Phase 2b
// (plan 24) routes each pair through one of two paths:
//
//   - **single-call** (default): one Haiku-class LLM call decides.
//     The path that ships today and the only path for hubs without
//     `dreams_use_agent_runtime` enabled OR engines without an
//     AgentRuntime configured.
//
//   - **agentic** (plan 24 lucid_active): when the trigger
//     evaluator fired KindContradiction or KindUserFollowup on
//     EITHER memory of the pair AND the per-hub flag is on AND
//     the engine has an AgentRuntime configured, route to
//     agent.Run with the trigger framing as the prompt and the
//     recall_memories tool. The agent reasons across the hub's
//     full context and emits a structured verdict that the phase
//     code parses into the same (contradicts, reason) shape the
//     single-call path returns. The fan-out (DreamAction +
//     notification) is identical regardless of which path
//     produced the verdict — only the verdict's source changes.
//
// Why route per-pair (not per-cycle): one cycle can mix
// not-fired pairs (use single-call, lucid_passive) with fired
// pairs (use agent, lucid_active). Per plan 23's margin model,
// ≥80% of pairs land in lucid_passive even on a hub with the
// flag on; per-pair routing keeps the cost shape tight.
func (e *Engine) phaseDetectContradictions(
	ctx context.Context,
	hub *model.Hub,
	runID string,
	pairs []model.SimilarMemoryPair,
	mergedMemoryIDs map[string]bool,
	actorID string,
	settings map[string]any,
	triggerDecisions map[string]triggers.Decision,
	runBudget *dreamRunBudget,
) (int, []model.DreamAction, model.DreamPhaseMetrics) {
	found := 0
	var actions []model.DreamAction
	metrics := model.DreamPhaseMetrics{Candidates: len(pairs)}
	started := time.Now()
	defer func() { metrics.DurationMs = time.Since(started).Milliseconds() }()
	members, _ := e.store.ListHubMembers(hub.ID)

	for _, pair := range pairs {
		// Heartbeat at the top of every iteration so a slow LLM
		// path or a transient outage during a long contradiction
		// loop can't leave the stale-reclaim sweeper thinking the
		// run has abandoned.
		e.heartbeat(runID)

		if mergedMemoryIDs[pair.MemoryA.ID] || mergedMemoryIDs[pair.MemoryB.ID] {
			metrics.Skipped++
			continue
		}

		// Phase 2b routing: agentic path when the trigger evaluator
		// fired on either memory of the pair AND the per-hub flag
		// is on AND the engine has an AgentRuntime configured AND
		// the cycle's shared run budget hasn't reached its hard cap.
		// All four must hold; the per-pair gate keeps lucid_passive
		// at ≥80% even on opted-in hubs (plan 23 margin model), and
		// the cycle-level governor caps cumulative cost at ~100
		// model calls per run (plan 24).
		metrics.Processed++
		var (
			contradiction  bool
			reason         string
			agentResult    *agenticContradictionResult
			agentSessionID string
			lucidActive    bool
		)
		routeAgent := e.shouldRouteContradictionAgent(settings, triggerDecisions, &pair) &&
			runBudget.shouldRoute(runID)
		if routeAgent {
			lucidActive = true
			result, agentErr := e.agenticDetectContradiction(ctx, hub, &pair, triggerDecisions, actorID)
			// Always consume budget + metrics from the populated
			// result — agenticDetectContradiction returns a
			// non-nil result with real ModelCalls/ToolCalls/Usage
			// even on non-success terminals (budget exhaust, max
			// turns, provider error). The only nil-result case is
			// the pre-flight error (validation, registry build),
			// where no model calls happened and there's nothing
			// to count.
			runBudget.consume(toAgentRunResultForBudget(result))
			if result != nil {
				// Real model-call count from the SDK drain — NOT
				// "1 per agent.Run" (the agent can fan out across
				// up to 8 turns per plan 24). Without this, phase
				// metrics underreport cost pressure ~8x.
				metrics.LLMCalls += result.ModelCalls
				metrics.TokensIn += int64(result.Usage.InputTokens)
				metrics.TokensOut += int64(result.Usage.OutputTokens)
				agentSessionID = result.SessionID
				agentResult = result
				contradiction = result.Contradicts
				reason = result.Reason
			}
			if agentErr != nil {
				metrics.LLMErrors++
				slog.WarnContext(ctx, "dream: agentic contradiction detection failed",
					"hub_id", hub.ID, "memory_a", pair.MemoryA.ID, "memory_b", pair.MemoryB.ID, "error", agentErr,
				)
				continue
			}
		} else {
			metrics.LLMCalls++
			c, r, resp, err := e.llmDetectContradiction(ctx, &pair.MemoryA, &pair.MemoryB, actorID)
			// Accumulate tokens regardless of parse success — the
			// API call already cost tokens whether or not we
			// understood the response.
			addLLMUsage(&metrics, resp)
			if err != nil {
				metrics.LLMErrors++
				if isLLMTimeout(err) {
					metrics.LLMTimeouts++
				}
				slog.WarnContext(ctx, "dream: contradiction detection failed", "error", err)
				continue
			}
			contradiction = c
			reason = r
		}
		if !contradiction {
			metrics.Skipped++
			continue
		}

		now := time.Now()
		action := model.DreamAction{
			ID:              generateID(),
			RunID:           runID,
			ActionType:      "contradiction",
			SourceMemoryIDs: []string{pair.MemoryA.ID, pair.MemoryB.ID},
			Reason:          reason,
			Similarity:      pair.Similarity,
			CreatedAt:       now,
			AgentPath:       model.DreamActionAgentPathSingleCall,
		}
		// Phase 2b audit: when the agent produced this verdict,
		// stamp the dream_action with session_id + call counts so
		// admin / calibration queries can attribute lucid_active
		// rows to their underlying agent run. Plain `lucid_active`
		// path-tag also serves as the cheap discriminator for
		// "show me agent-derived contradictions" without needing
		// a JSON column scan.
		if lucidActive && agentResult != nil {
			action.AgentPath = model.DreamActionAgentPathLucidActive
			action.AgentSessionID = agentSessionID
			action.AgentModelCalls = agentResult.ModelCalls
			action.AgentToolCalls = len(agentResult.ToolCalls)
		}

		// Build the notification payload up front so the fan-out tx
		// is minimal. Self-contained memory refs so the inbox row
		// can render without a follow-up /v1/memories fetch. Memories
		// captured without an explicit title (e.g. quick dumps from
		// the CLI) have an empty Memory.Title, so fall back to the
		// LLM-produced summary, then a trimmed prefix of the raw
		// content. Without this the inbox row and expanded body would
		// render bare UUIDs, which the user can't visually parse.
		payloadBytes, payloadErr := json.Marshal(model.ReviewContradictionPayload{
			MemoryA: model.ReviewMemoryRef{
				ID:    pair.MemoryA.ID,
				Title: reviewMemoryRefTitle(&pair.MemoryA),
			},
			MemoryB: model.ReviewMemoryRef{
				ID:    pair.MemoryB.ID,
				Title: reviewMemoryRefTitle(&pair.MemoryB),
			},
			Similarity: pair.Similarity,
			Reason:     reason,
		})
		if payloadErr != nil {
			slog.WarnContext(ctx, "dream: failed to marshal contradiction payload", "error", payloadErr)
			payloadBytes = nil
		}
		// SourceID is the memory-pair dedup key, same shape the
		// legacy reviews path used. notifications_source_unique
		// (source_kind, source_id) makes re-processing the same
		// pair idempotent — upsert updates the payload rather
		// than duplicating rows.
		recipients := filterNotifiableRecipients(e.store, contradictionDecisionRecipients(hub, members, pair.MemoryA.OwnerID, pair.MemoryB.OwnerID))
		if len(recipients) == 0 {
			metrics.Skipped++
			continue
		}
		notifs := make([]*model.Notification, 0, len(recipients))
		runID := runID
		for _, recipientID := range recipients {
			notifs = append(notifs, &model.Notification{
				ID:              generateID(),
				Audience:        model.AudienceUser,
				RecipientUserID: recipientID,
				HubID:           hub.ID,
				Kind:            model.NotificationKindReviewContradiction,
				Status:          model.NotificationStatusPending,
				Priority:        0,
				SourceKind:      model.NotificationSourceContradiction,
				DreamRunID:      &runID,
				SourceID:        contradictionNotificationSourceID(pair.MemoryA.ID, pair.MemoryB.ID) + ":" + recipientID,
				Payload:         payloadBytes,
				CreatedAt:       now,
			})
		}

		// Fan out the dream action + notification in one transaction
		// so the inbox never shows a notification whose companion
		// dream action is missing. Phase 6: the legacy reviews row
		// is no longer written — the notification IS the inbox row.
		txErr := e.txRunner.WithTx(ctx, func(tx pgx.Tx) error {
			if err := e.txRunner.CreateDreamActionTx(ctx, tx, &action); err != nil {
				return fmt.Errorf("create dream action: %w", err)
			}
			for _, notif := range notifs {
				if err := e.txRunner.CreateNotificationTx(ctx, tx, notif); err != nil {
					return fmt.Errorf("create notification: %w", err)
				}
			}
			return nil
		})
		if txErr != nil {
			slog.WarnContext(ctx, "dream: contradiction fan-out tx failed", "error", txErr,
				"memory_a", pair.MemoryA.ID, "memory_b", pair.MemoryB.ID)
			metrics.Errors++
			continue
		}
		actions = append(actions, action)

		// Publish notification.created AFTER commit so the SSE
		// broadcast cannot arrive before the row is visible to a
		// reader. Best-effort: a publish failure is swallowed and
		// clients recover via reconnect or the polling safety net.
		for _, notif := range notifs {
			events.PublishNotificationCreated(ctx, e.events, notif)
		}

		found++
		metrics.Actions++

		slog.InfoContext(ctx, "dream: contradiction found, notification created",
			"memory_a", pair.MemoryA.ID, "memory_b", pair.MemoryB.ID,
			"recipients", len(notifs),
			"reason", reason,
		)
	}

	return found, actions, metrics
}

// phaseArchiveStale finds memories that have fully decayed and have never been recalled.
// Uses ListArchiveCandidates to pre-filter in SQL (active, not pinned, access_count=0, age>60d),
// then applies decay math in Go to decide final archival.
