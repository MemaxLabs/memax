package onboarding

import (
	"context"
	"strconv"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// memaxBuiltInAgentSlug mirrors the store-package constant. We
// duplicate it here to avoid an import cycle (store imports nothing
// from onboarding; onboarding imports store).
const memaxBuiltInAgentSlug = "memax"

// ChecklistTick describes a single materialization-driven change to a
// checklist item. The handler turns these into store writes by calling
// existing primitives (CompleteNotificationItem /
// UpdateChecklistItemProgress) — the materializer never mutates the
// payload itself, only describes what the SOT says should happen.
//
// Either ShouldComplete is true (count crossed the trigger threshold)
// OR ProgressCurrent is non-nil (count changed but threshold not yet
// met). Both can be true for a single tick when the item has progress
// AND just crossed the threshold; the handler applies completion in
// that case (which subsumes progress).
type ChecklistTick struct {
	ItemID          string
	ShouldComplete  bool
	ProgressCurrent *int
	ProgressTarget  *int
}

// MaterializeChecklist inspects the user's persistent state
// (memories, connected_agents, team-hub memberships) and returns
// the list of checklist item changes the SOT implies. It does NOT
// mutate the payload or write to the database — the caller composes
// existing store primitives to apply the changes atomically and to
// fire the matching SSE events.
//
// Triggers handled:
//
//	memory_count_gte:N → memories table (excluding seeds)
//	configs_sync       → connected_agents (excluding memax built-in) >= 1
//	hub_event          → ListUserHubs filtered to hub_type=team >= 1
//
// Triggers NOT handled (no queryable backing data — these still use
// payload.completed_at written by /complete or by the slim ask
// recorder hook):
//
//	ask_count_gte:N    → /ask is stateless, no persistent row
//	user_click         → first_dream is a UI gesture
//	(empty)            → welcome is a UI gesture
//
// Returns empty slice when the user has no items needing
// materialization (past-onboarding users hit zero query cost — the
// fast path checks the payload alone before issuing any count
// queries).
func MaterializeChecklist(
	ctx context.Context,
	s store.Store,
	userID string,
	payload model.ChecklistPayload,
) []ChecklistTick {
	if s == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	// Fast-path: if no queryable triggers are still incomplete, skip
	// the count queries entirely.
	needsLookup := false
	for _, item := range payload.Items {
		if item.CompletedAt != nil {
			continue
		}
		if _, _, ok := parseQueryableTrigger(item.Trigger); ok {
			needsLookup = true
			break
		}
	}
	if !needsLookup {
		return nil
	}
	counts := loadOnboardingCounts(ctx, s, userID, payload)
	var ticks []ChecklistTick
	for i := range payload.Items {
		item := payload.Items[i]
		if item.CompletedAt != nil {
			continue
		}
		bucket, threshold, ok := parseQueryableTrigger(item.Trigger)
		if !ok {
			continue
		}
		observed, found := counts[bucket]
		if !found {
			continue
		}
		// Threshold met → mark for completion. Subsumes any progress
		// update (the completed_at write is the authoritative signal;
		// progress fields become irrelevant once an item is done).
		if observed >= threshold {
			ticks = append(ticks, ChecklistTick{ItemID: item.ID, ShouldComplete: true})
			continue
		}
		// Below threshold but the item HAS a progress.target → write
		// progress.current so the UI shows live progress (3/5, etc.)
		// instead of the stale 0/N the emitter seeded. Skip when
		// observed equals the stored current — pointless write.
		if item.Progress == nil || item.Progress.Target <= 0 {
			continue
		}
		if observed == item.Progress.Current {
			continue
		}
		clamped := observed
		if clamped > item.Progress.Target {
			clamped = item.Progress.Target
		}
		c := clamped
		target := item.Progress.Target
		ticks = append(ticks, ChecklistTick{
			ItemID:          item.ID,
			ProgressCurrent: &c,
			ProgressTarget:  &target,
		})
	}
	return ticks
}

// parseQueryableTrigger returns (bucket, threshold, ok) for triggers
// whose condition can be computed from a count query against
// persistent state. Returns ok=false for triggers that require a
// gesture or stateless event (welcome / first_dream / first_ask).
func parseQueryableTrigger(trigger string) (string, int, bool) {
	if trigger == "" {
		return "", 0, false
	}
	switch trigger {
	case "configs_sync":
		return "agents", 1, true
	case "hub_event":
		return "team_hubs", 1, true
	}
	if idx := strings.LastIndex(trigger, "_gte:"); idx > 0 {
		prefix := trigger[:idx]
		n, err := strconv.Atoi(trigger[idx+len("_gte:"):])
		if err != nil || n < 1 {
			return "", 0, false
		}
		if prefix == "memory_count" {
			return "memories", n, true
		}
		// ask_count_gte intentionally NOT queryable — see file doc.
	}
	return "", 0, false
}

// loadOnboardingCounts runs the count queries the materializer
// needs, gated on which triggers actually appear in this payload.
// Skipping unused queries keeps the per-read cost proportional to
// active items rather than a fixed 3-query overhead. The returned
// map only contains buckets that were actually queried.
func loadOnboardingCounts(
	ctx context.Context,
	s store.Store,
	userID string,
	payload model.ChecklistPayload,
) map[string]int {
	wants := map[string]bool{}
	for _, item := range payload.Items {
		if item.CompletedAt != nil {
			continue
		}
		bucket, _, ok := parseQueryableTrigger(item.Trigger)
		if !ok {
			continue
		}
		wants[bucket] = true
	}
	out := make(map[string]int, len(wants))
	if wants["memories"] {
		if n, err := s.CountMemoriesByOwnerExcludingSeeds(ctx, userID); err == nil {
			if n < 0 {
				n = 0
			}
			out["memories"] = n
		}
	}
	if wants["agents"] {
		if list, err := s.ListConnectedAgentsWithStats(userID); err == nil {
			n := 0
			for _, a := range list {
				if a.AgentName == "" || a.AgentName == memaxBuiltInAgentSlug {
					continue
				}
				n++
			}
			out["agents"] = n
		}
	}
	if wants["team_hubs"] {
		if list, err := s.ListUserHubs(userID); err == nil {
			n := 0
			for _, h := range list {
				if h.Hub.HubType == "team" {
					n++
				}
			}
			out["team_hubs"] = n
		}
	}
	return out
}
