//memax:lint-skip:tool-store
//
// This file is the production tools.RecallService implementation —
// it is the SINK the tool wrapper (recall.go) delegates to AFTER
// the wrapper has already done the ScopeResolver.Resolve and the
// hub validation. The wrapper passes a security-validated
// AgentRecallRequest in (HubIDs is the strict allowlist already
// intersected with the model's hub_id arg). Calling Resolve again
// here would be redundant. The lint's per-file rule is right for
// wrappers; this file is the sink it protects.
//
// Phase 3.4b1 — production strict-hub RecallService.
//
// The agent runtime's recall_memories tool wraps a RecallService;
// this file provides the production impl. The strict-hub semantics
// matter for security: when the model says "search hub_id=h-work
// specifically" or even "search across hubs A,B,C," the search
// MUST narrow to those hubs without falling back to "OR owner_id =
// caller". User-owned content in non-target hubs would otherwise
// leak — that's why the existing user-facing /v1/recall pipeline
// (which uses VisibilityScope's owner-OR-hub filter) is NOT what
// the agent path uses.
//
// The store layer already provides the strict primitive
// (SearchChunksInHubs); this service composes it with the embedder,
// chunk→memory aggregation, and a memory-title lookup so the model
// gets a structured response it can cite from.

package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// agentRecallStore is the narrow store surface the production
// RecallService uses. Defined as an interface so tests can stub
// it without pulling in PostgresStore. Keep this set minimal:
// every method here is a security-sensitive boundary, and adding
// new ones forces a code-review of the "is this strict-hub-safe?"
// question.
type agentRecallStore interface {
	// SearchChunksAgent runs the strict-hub chunk search the
	// agent path requires. The implementation MUST enforce
	// `m.hub_id = ANY(hubs)` exactly — no owner-OR-hub
	// fallback. Adapter in workerapp delegates to
	// PostgresStore.SearchChunksInHubs.
	//
	// Filters and search-options are intentionally absent from
	// this interface: the agent path uses default scoring lanes
	// and never needs the topic / temporal / no-rerank knobs
	// that the user-facing /v1/recall accepts. Adding them
	// later would be a contract change worth a separate review.
	SearchChunksAgent(ctx context.Context, query string, queryEmbedding []float64, hubIDs []string, limit int) ([]model.Chunk, error)

	// GetMemoryInHubs reads one memory by id, gated by the same
	// strict-hub allowlist. Used to populate Title + HubID on
	// the AgentRecallResponse without leaking memories the
	// agent has no business citing.
	GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error)
}

// AgentRecallService is the production tools.RecallService
// implementation. Constructed once at app startup; thread-safe
// (no mutable state).
type AgentRecallService struct {
	store    agentRecallStore
	embedder embed.Embedder
}

// NewAgentRecallService validates dependencies up-front so a
// misconfiguration (no embedder, no store) surfaces at app
// startup rather than at the first chat send.
func NewAgentRecallService(s agentRecallStore, e embed.Embedder) (*AgentRecallService, error) {
	if s == nil {
		return nil, errors.New("agent/tools: AgentRecallService requires a store")
	}
	if e == nil {
		return nil, errors.New("agent/tools: AgentRecallService requires an embedder")
	}
	return &AgentRecallService{store: s, embedder: e}, nil
}

// agentRecallChunkOversample is the multiplier between memory limit
// and chunk fetch limit. We over-fetch chunks because multiple
// chunks can belong to the same memory; aggregating to top-chunk-
// per-memory is the agent's expectation. 4× covers most realistic
// chunking density (5KB content → ~3 chunks) without paying for
// excessive fetch cost.
const agentRecallChunkOversample = 4

// AgentRecall is the tools.RecallService entry point. The contract:
//
//   - req.Query is non-empty (wrapper validated).
//   - req.HubIDs is non-empty AND already intersected against the
//     caller's authorized hub set (wrapper validated).
//   - req.Limit is in [1, maxRecallLimit] (wrapper validated).
//
// We embed the query, search chunks across the strict hub set,
// aggregate to one row per memory (highest-relevance chunk wins),
// and decorate each row with the memory title via GetMemoryInHubs.
// Title decoration uses the same strict allowlist so a chunk
// belonging to a memory the agent's scope doesn't cover (an
// impossible state given the search predicate, but defensive)
// gets dropped instead of leaking the title.
func (s *AgentRecallService) AgentRecall(ctx context.Context, req AgentRecallRequest) (AgentRecallResponse, error) {
	if len(req.HubIDs) == 0 {
		// Defensive — the wrapper rejects empty before getting
		// here, but a future direct caller (a CLI debug tool)
		// might miss it. The store would reject too, but this
		// gives the cleaner error.
		return AgentRecallResponse{}, errEmptyScope
	}
	if req.Limit <= 0 {
		req.Limit = defaultRecallLimit
	}
	if req.Limit > maxRecallLimit {
		req.Limit = maxRecallLimit
	}

	// Embed the query. Voyage's "query" inputType is the
	// retrieval-side counterpart of the "document" inputType
	// used at ingest time — they must match for the cosine
	// similarity to be meaningful.
	embeddings, err := s.embedder.EmbedContext(ctx, []string{req.Query}, "query")
	if err != nil {
		return AgentRecallResponse{}, fmt.Errorf("embed query: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		// Embedder returned nothing — degraded mode. Empty
		// response rather than a synthetic error; the model
		// will read "no results" and continue.
		return AgentRecallResponse{Memories: []RecallResult{}}, nil
	}

	// Over-fetch chunks; we'll aggregate to the requested memory
	// count below.
	chunkLimit := req.Limit * agentRecallChunkOversample
	chunks, err := s.store.SearchChunksAgent(ctx, req.Query, embeddings[0], req.HubIDs, chunkLimit)
	if err != nil {
		return AgentRecallResponse{}, fmt.Errorf("search chunks: %w", err)
	}
	if len(chunks) == 0 {
		return AgentRecallResponse{Memories: []RecallResult{}}, nil
	}

	// Aggregate: top chunk per memory, capped at req.Limit. Iter
	// preserves search order so the highest-ranked chunk for each
	// memory is the one we keep. HubID is left empty here and
	// populated from the memory fetch below — Chunk doesn't carry
	// the parent hub_id directly, and the per-result hub_id stamp
	// matters for plan 24's read-time redaction (Phase 3.4b2+).
	seen := make(map[string]struct{}, req.Limit)
	out := make([]RecallResult, 0, req.Limit)
	for _, c := range chunks {
		if _, dup := seen[c.MemoryID]; dup {
			continue
		}
		seen[c.MemoryID] = struct{}{}
		out = append(out, RecallResult{
			MemoryID: c.MemoryID,
			Snippet:  c.Content,
			Score:    c.RelevanceScore,
		})
		if len(out) >= req.Limit {
			break
		}
	}

	// Decorate with title + hub_id from the strict-hub memory
	// fetch. We distinguish three error shapes here — codex's
	// Phase 3.4b1 review caught the gap where any error was
	// silently dropping the row, masking transient DB failures
	// as "no results":
	//
	//   - pgx.ErrNoRows / wrapped: memory was deleted between
	//     the chunk search and this lookup, OR the chunk search
	//     returned a memory whose hub left the strict allowlist.
	//     Drop the row — the model can't follow up on a memory
	//     that's no longer there.
	//   - context cancellation: caller bailed (timeout / shutdown).
	//     Propagate so the worker's deferred guard can finalize
	//     the message as failed rather than persisting a partial
	//     result.
	//   - any other error: operational (DB down, network, etc).
	//     Propagate so the agent run sees the failure and finalizes
	//     accordingly. Without this, a transient outage would
	//     return "looks successful but empty" responses to the
	//     model, which is worse than failing loud.
	decorated := out[:0]
	for _, r := range out {
		mem, err := s.store.GetMemoryInHubs(ctx, r.MemoryID, req.HubIDs)
		if err != nil {
			if errors.Is(err, ctx.Err()) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return AgentRecallResponse{}, fmt.Errorf("memory decoration cancelled: %w", err)
			}
			if isMemoryMiss(err) {
				continue
			}
			return AgentRecallResponse{}, fmt.Errorf("decorate memory %s: %w", r.MemoryID, err)
		}
		r.Title = mem.Title
		r.HubID = mem.HubID
		decorated = append(decorated, r)
	}

	return AgentRecallResponse{Memories: decorated}, nil
}

// isMemoryMiss reports whether err indicates "memory not found
// in the strict-hub allowlist" rather than an operational
// failure. The store's scanMemoryWithAuthor wraps pgx.ErrNoRows
// with `fmt.Errorf("memory not found: %w", err)`, so errors.Is
// against pgx.ErrNoRows still matches.
func isMemoryMiss(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
