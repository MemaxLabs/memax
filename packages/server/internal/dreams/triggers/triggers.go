// Package triggers is the Phase 2a / plan-24 dream-trigger
// evaluator. The dream engine's scan loop calls Evaluate per
// memory considered; the result tells the engine whether a
// "lucid_active" agent run is justified for that memory.
//
// Phase 2a runs the evaluator in soft-mode: decisions are LOGGED
// (via dream_trigger_decisions) but the engine still uses the
// existing single-call LLM path. Phase 2b reads the soft-mode
// log to calibrate thresholds, then flips the behavior gate so
// fired triggers route through agent.Run.
//
// Why a pure evaluator: the four signals (contradiction count,
// URL drift, topic-sibling count, user-followup marker) are all
// pre-fetched by the engine's scan loop. Keeping the evaluator
// pure (same Inputs → same Decision) makes it trivially testable
// without store mocks AND keeps the data-fetching policy in one
// place at the engine layer where the per-hub batching can
// optimize round-trips.
package triggers

import (
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Kind is the dream-trigger discriminator. Strings match the SQL
// CHECK constraint on dream_trigger_decisions.trigger_kind — the
// constants on this side are the canonical source; the migration
// just mirrors them so the database refuses bad writes from a
// future direct-SQL caller.
type Kind string

const (
	// KindNone — no trigger fired. The decision is still logged
	// (it's evidence that "this memory was considered and rejected"),
	// but trigger_fired is false.
	KindNone Kind = "none"

	// KindUserFollowup — the user explicitly flagged the memory
	// with @TODO/@FIXME/@QUESTION. Highest priority because it's
	// the only signal that's an explicit user request — every
	// other trigger is system-derived.
	KindUserFollowup Kind = "user_followup"

	// KindContradiction — the memory is in N or more contradiction
	// pairs from recent dream runs. A factual conflict the user
	// has surfaced (or the system has detected) and not yet
	// resolved; the agent should propose a merge or archival.
	KindContradiction Kind = "contradiction"

	// KindURLDrift — the memory's source URL was re-fetched and
	// the body hash differs from the stored content_hash. The
	// canonical content has changed; the agent should re-ingest
	// or at least surface the drift to the user.
	KindURLDrift Kind = "url_drift"

	// KindTopicOverload — the memory's topic has accumulated more
	// than the threshold of sibling memories. The agent should
	// propose a topic split (organize phase). Lowest priority
	// because it's the most diffuse signal — a topic with 30
	// memories isn't broken, it's just dense.
	KindTopicOverload Kind = "topic_overload"
)

// Inputs bundles the per-memory context the evaluator needs to
// decide. The engine's scan loop pre-fetches all four signal
// inputs in one batch per hub, then calls Evaluate per memory —
// this keeps the evaluator pure and the database round-trip
// count constant per scan rather than O(memory × signal).
type Inputs struct {
	// Memory is the memory under consideration. SourceFetchHash,
	// ContentHash, and UserFollowupMarker are read from the row;
	// the engine MUST pass a fully-populated *model.Memory (i.e.
	// scanned via the standard memoryCols path, not a partial
	// projection that drops the new Phase 2a fields).
	Memory *model.Memory

	// UserFollowupScanned reports whether the ingest distill stage
	// has run a marker scan on Memory. The trigger doesn't need
	// this for fire/no-fire (an empty Memory.UserFollowupMarker
	// short-circuits regardless), but the calibration log uses
	// it to distinguish "scan ran, no marker found" (Scanned=true,
	// Marker="") from "scan never ran" (Scanned=false, Marker="").
	// Without the bool, pre-Phase-2a memories that never got the
	// distill-stage update look identical to scanned-empty memories
	// in the no-fire denominator and dilute the fire-rate metric.
	UserFollowupScanned bool

	// ContradictionCount is the number of contradiction pairs
	// involving Memory.ID across recent dream runs. The engine
	// computes this once per scan from dream_actions or
	// review_contradictions; the trigger only reads the count.
	ContradictionCount int

	// TopicSiblingCount is the number of memories in the same
	// topic as Memory (including Memory itself, if assigned).
	// A non-topic memory has count=0 and never fires
	// topic-overload. The engine fetches per-topic counts in a
	// single query per hub.
	TopicSiblingCount int
}

// Config holds the trigger thresholds. The defaults come from plan
// 24 §"Phase 2a — soft-mode trigger evaluator" — they're starting
// points for soft-mode calibration, not load-bearing constants.
// Phase 2b reads dream_trigger_decisions to retune.
//
// All Threshold fields are ">=" comparisons (>= threshold fires).
type Config struct {
	// ContradictionThreshold — the minimum contradiction count
	// that fires the contradiction trigger. Default: 3.
	ContradictionThreshold int

	// TopicOverloadThreshold — the minimum sibling count that
	// fires the topic-overload trigger. A topic with exactly
	// this many memories (counting the focal one) fires.
	// Default: 12.
	TopicOverloadThreshold int
}

// DefaultConfig returns the plan-24 starting thresholds. Always
// safe to use; tests that want tighter or looser values build
// their own Config literal.
func DefaultConfig() Config {
	return Config{
		ContradictionThreshold: 3,
		TopicOverloadThreshold: 12,
	}
}

// Signals captures the raw signal values for all four triggers,
// independent of which one (if any) fired. Persisted as the
// `signals` JSONB column on dream_trigger_decisions. Always
// fully populated so calibration queries don't need joins —
// "how often does contradiction_count >= 3 fire alongside a
// non-empty user_followup_marker?" is a single jsonb query.
//
// UserFollowupScanned is the three-state denominator helper:
// true means an ingest scan touched this memory (whether it
// found a marker or not); false means the scan never ran. The
// calibration query "fire rate among scanned memories" filters
// on UserFollowupScanned=true to exclude pre-Phase-2a memories
// that haven't been re-ingested.
type Signals struct {
	ContradictionCount  int    `json:"contradiction_count"`
	URLDrift            bool   `json:"url_drift"`
	TopicSiblingCount   int    `json:"topic_sibling_count"`
	UserFollowupScanned bool   `json:"user_followup_scanned"`
	UserFollowupMarker  string `json:"user_followup_marker,omitempty"`
}

// Decision is the per-memory result of one Evaluate call. The
// engine writes one row to dream_trigger_decisions per Decision.
//
// Even when Fired is false, the row IS written (with Kind=KindNone)
// — the absence of a fire is calibration evidence too. Without
// the negative cases logged, we can't compute a fire rate.
type Decision struct {
	Fired   bool
	Kind    Kind
	Signals Signals
}

// Evaluate runs all four triggers and returns the highest-priority
// fire (or KindNone if none fires). Signals are always populated
// regardless of which trigger wins — see Signals docs.
//
// **Priority order (highest first):**
//
//  1. user_followup — explicit user signal; if the user wrote
//     @TODO they want action.
//  2. contradiction — factual conflict needing resolution.
//  3. url_drift — canonical content has changed.
//  4. topic_overload — organizational drift, least urgent.
//
// Picking ONE kind (not all that fire) is a deliberate choice:
// the agent run that follows in Phase 2b takes the kind as its
// task framing, and we don't want to dilute the framing with
// multiple unrelated tasks ("the user TODO'd this AND it
// contradicts X AND it's drifted"). Highest priority is the
// most actionable; the others stay visible in `signals` for
// calibration and for the agent to read as secondary context.
//
// Concurrency: pure function; safe to call concurrently across
// hubs / memories.
func (c Config) Evaluate(in Inputs) Decision {
	signals := buildSignals(in)

	// Cascade in priority order. First-fire wins.
	if userFollowupFires(in) {
		return Decision{Fired: true, Kind: KindUserFollowup, Signals: signals}
	}
	if contradictionFires(in, c.ContradictionThreshold) {
		return Decision{Fired: true, Kind: KindContradiction, Signals: signals}
	}
	if urlDriftFires(in) {
		return Decision{Fired: true, Kind: KindURLDrift, Signals: signals}
	}
	if topicOverloadFires(in, c.TopicOverloadThreshold) {
		return Decision{Fired: true, Kind: KindTopicOverload, Signals: signals}
	}
	return Decision{Fired: false, Kind: KindNone, Signals: signals}
}

func buildSignals(in Inputs) Signals {
	var marker string
	var sourceHashDiffers bool
	if in.Memory != nil {
		marker = in.Memory.UserFollowupMarker
		// URL-drift signal: the source was re-fetched (non-empty
		// SourceFetchHash) AND the re-fetched hash differs from
		// the stored content_hash. Pre-Phase-2a memories have
		// empty SourceFetchHash and never fire — by design.
		if in.Memory.SourceFetchHash != "" && in.Memory.SourceFetchHash != in.Memory.ContentHash {
			sourceHashDiffers = true
		}
	}
	return Signals{
		ContradictionCount:  in.ContradictionCount,
		URLDrift:            sourceHashDiffers,
		TopicSiblingCount:   in.TopicSiblingCount,
		UserFollowupScanned: in.UserFollowupScanned,
		UserFollowupMarker:  marker,
	}
}

func userFollowupFires(in Inputs) bool {
	if in.Memory == nil {
		return false
	}
	return in.Memory.UserFollowupMarker != ""
}

func contradictionFires(in Inputs, threshold int) bool {
	if threshold <= 0 {
		// Disabled by config — never fire. Useful for tests that
		// want to exercise other triggers in isolation.
		return false
	}
	return in.ContradictionCount >= threshold
}

func urlDriftFires(in Inputs) bool {
	if in.Memory == nil {
		return false
	}
	if in.Memory.SourceFetchHash == "" {
		return false
	}
	return in.Memory.SourceFetchHash != in.Memory.ContentHash
}

func topicOverloadFires(in Inputs, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	return in.TopicSiblingCount >= threshold
}
