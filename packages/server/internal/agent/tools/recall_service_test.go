package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeAgentRecallStore implements agentRecallStore for tests.
// Captures inputs for assertions and serves canned outputs.
type fakeAgentRecallStore struct {
	chunks       []model.Chunk
	chunksErr    error
	memories     map[string]*model.Memory
	memoriesErr  error
	lastQuery    string
	lastEmbed    []float64
	lastHubs     []string
	lastLimit    int
	getCallCount int
}

func (f *fakeAgentRecallStore) SearchChunksAgent(_ context.Context, query string, queryEmbedding []float64, hubIDs []string, limit int) ([]model.Chunk, error) {
	f.lastQuery = query
	f.lastEmbed = queryEmbedding
	f.lastHubs = hubIDs
	f.lastLimit = limit
	if f.chunksErr != nil {
		return nil, f.chunksErr
	}
	return f.chunks, nil
}

func (f *fakeAgentRecallStore) GetMemoryInHubs(_ context.Context, id string, _ []string) (*model.Memory, error) {
	f.getCallCount++
	if f.memoriesErr != nil {
		return nil, f.memoriesErr
	}
	mem, ok := f.memories[id]
	if !ok {
		// Not-in-allowlist or deleted — wrap pgx.ErrNoRows the
		// same way the production scanMemoryWithAuthor does so
		// the service's isMemoryMiss check matches.
		return nil, fmt.Errorf("memory not found: %w", pgx.ErrNoRows)
	}
	return mem, nil
}

// fakeEmbedder serves a canned embedding (or error). Implements
// embed.Embedder.
type fakeEmbedder struct {
	out [][]float64
	err error
}

func (f *fakeEmbedder) Embed(texts []string, _ string) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	// Default: a synthetic embedding per input.
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{0.1, 0.2, 0.3}
	}
	return out, nil
}

func (f *fakeEmbedder) EmbedContext(_ context.Context, texts []string, inputType string) ([][]float64, error) {
	return f.Embed(texts, inputType)
}

func (f *fakeEmbedder) Dimensions() int { return 3 }

func TestNewAgentRecallService_RequiresStore(t *testing.T) {
	if _, err := NewAgentRecallService(nil, &fakeEmbedder{}); err == nil {
		t.Error("expected error for nil store")
	}
}

func TestNewAgentRecallService_RequiresEmbedder(t *testing.T) {
	if _, err := NewAgentRecallService(&fakeAgentRecallStore{}, nil); err == nil {
		t.Error("expected error for nil embedder")
	}
}

// Empty hub list rejected upfront — strict-hub semantics demand
// an explicit allowlist; the wrapper's empty-scope check is the
// first line of defense, this is the second.
func TestAgentRecall_RejectsEmptyHubs(t *testing.T) {
	svc, _ := NewAgentRecallService(&fakeAgentRecallStore{}, &fakeEmbedder{})
	_, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: nil, Limit: 5,
	})
	if err != errEmptyScope {
		t.Errorf("err = %v, want errEmptyScope", err)
	}
}

// Happy path: chunks → memories aggregation, title decoration,
// snippet preserved, score preserved.
func TestAgentRecall_HappyPath(t *testing.T) {
	store := &fakeAgentRecallStore{
		chunks: []model.Chunk{
			{ID: "c1", MemoryID: "m1", Content: "snippet one", RelevanceScore: 0.9},
			{ID: "c2", MemoryID: "m1", Content: "snippet one alt", RelevanceScore: 0.8}, // dup memory; should be skipped
			{ID: "c3", MemoryID: "m2", Content: "snippet two", RelevanceScore: 0.7},
		},
		memories: map[string]*model.Memory{
			"m1": {ID: "m1", Title: "Memory One", HubID: "h1"},
			"m2": {ID: "m2", Title: "Memory Two", HubID: "h1"},
		},
	}
	svc, _ := NewAgentRecallService(store, &fakeEmbedder{})
	resp, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "test query", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 5,
	})
	if err != nil {
		t.Fatalf("AgentRecall: %v", err)
	}
	if len(resp.Memories) != 2 {
		t.Fatalf("got %d memories, want 2 (deduped by memory_id)", len(resp.Memories))
	}
	if resp.Memories[0].MemoryID != "m1" || resp.Memories[0].Title != "Memory One" {
		t.Errorf("first = %+v", resp.Memories[0])
	}
	if resp.Memories[0].Snippet != "snippet one" {
		t.Errorf("first.Snippet = %q (top-ranked chunk wins)", resp.Memories[0].Snippet)
	}
	if resp.Memories[0].HubID != "h1" {
		t.Errorf("first.HubID = %q, want h1 (from memory fetch)", resp.Memories[0].HubID)
	}
	if resp.Memories[1].MemoryID != "m2" {
		t.Errorf("second.MemoryID = %q, want m2", resp.Memories[1].MemoryID)
	}

	// Verify the strict-hub plumbing: HubIDs passed through, query
	// + embedding propagated, oversample applied to chunk limit.
	if len(store.lastHubs) != 1 || store.lastHubs[0] != "h1" {
		t.Errorf("lastHubs = %v, want [h1]", store.lastHubs)
	}
	if store.lastLimit != 5*agentRecallChunkOversample {
		t.Errorf("lastLimit = %d, want %d (oversample)", store.lastLimit, 5*agentRecallChunkOversample)
	}
	if store.lastQuery != "test query" {
		t.Errorf("lastQuery = %q", store.lastQuery)
	}
}

// Empty embedding → empty response, no chunk search attempted.
func TestAgentRecall_EmptyEmbeddingReturnsEmpty(t *testing.T) {
	store := &fakeAgentRecallStore{
		chunks: []model.Chunk{
			{ID: "c1", MemoryID: "m1", Content: "should not be returned"},
		},
	}
	svc, _ := NewAgentRecallService(store, &fakeEmbedder{out: [][]float64{{}}})
	resp, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 5,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(resp.Memories) != 0 {
		t.Errorf("got %d memories, want 0 (degraded embedder)", len(resp.Memories))
	}
	if store.lastQuery != "" {
		t.Errorf("chunk search was attempted with degraded embedder")
	}
}

// Memory deleted between chunk-search and decoration — the row
// gets dropped from results so we never return a memory_id the
// model can't query.
func TestAgentRecall_DroppedWhenMemoryNotInScope(t *testing.T) {
	store := &fakeAgentRecallStore{
		chunks: []model.Chunk{
			{ID: "c1", MemoryID: "m1", Content: "snippet one", RelevanceScore: 0.9},
			{ID: "c2", MemoryID: "m2", Content: "snippet two", RelevanceScore: 0.8},
		},
		memories: map[string]*model.Memory{
			"m1": {ID: "m1", Title: "Memory One", HubID: "h1"},
			// m2 deliberately missing — simulates "deleted between
			// search and decoration" or "wrongly returned by a
			// buggy search filter (would never happen in production
			// but defensive)".
		},
	}
	svc, _ := NewAgentRecallService(store, &fakeEmbedder{})
	resp, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 5,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(resp.Memories) != 1 || resp.Memories[0].MemoryID != "m1" {
		t.Errorf("got %+v, want only m1", resp.Memories)
	}
}

// Operational errors during decoration (DB outage, etc) MUST
// propagate — silently dropping rows on transient failures would
// mask DB issues as "no results" which is worse UX than failing
// loud. Codex's Phase 3.4b1 v1 review caught the gap.
func TestAgentRecall_DecorationOperationalErrorPropagates(t *testing.T) {
	store := &fakeAgentRecallStore{
		chunks: []model.Chunk{
			{ID: "c1", MemoryID: "m1", Content: "snippet", RelevanceScore: 0.9},
		},
		// memoriesErr fires regardless of id → simulates the DB
		// down on the decoration call.
		memoriesErr: errors.New("connection refused"),
	}
	svc, _ := NewAgentRecallService(store, &fakeEmbedder{})
	_, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 5,
	})
	if err == nil {
		t.Fatalf("expected operational error to propagate, got nil")
	}
	if !errors.Is(err, store.memoriesErr) {
		t.Errorf("err = %v, want wrapped %v", err, store.memoriesErr)
	}
}

// Context cancellation propagates — same reasoning as the
// operational-error path.
func TestAgentRecall_DecorationContextCancelPropagates(t *testing.T) {
	store := &fakeAgentRecallStore{
		chunks: []model.Chunk{
			{ID: "c1", MemoryID: "m1", Content: "snippet", RelevanceScore: 0.9},
		},
		memoriesErr: context.Canceled,
	}
	svc, _ := NewAgentRecallService(store, &fakeEmbedder{})
	_, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 5,
	})
	if err == nil {
		t.Fatalf("expected context.Canceled to propagate, got nil")
	}
}

// Embedder error propagates as an error — we don't silently
// degrade because the model won't know it's getting bad results.
func TestAgentRecall_EmbedderErrorPropagates(t *testing.T) {
	svc, _ := NewAgentRecallService(&fakeAgentRecallStore{}, &fakeEmbedder{err: errors.New("voyage 503")})
	_, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 5,
	})
	if err == nil {
		t.Error("expected error from embedder failure")
	}
}

// Limit>maxRecallLimit clamps to maxRecallLimit.
func TestAgentRecall_LimitClamped(t *testing.T) {
	store := &fakeAgentRecallStore{}
	svc, _ := NewAgentRecallService(store, &fakeEmbedder{})
	_, err := svc.AgentRecall(context.Background(), AgentRecallRequest{
		Query: "hi", OwnerID: "u1", HubIDs: []string{"h1"}, Limit: 9999,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if store.lastLimit != maxRecallLimit*agentRecallChunkOversample {
		t.Errorf("lastLimit = %d, want clamped to maxRecallLimit*oversample = %d",
			store.lastLimit, maxRecallLimit*agentRecallChunkOversample)
	}
}
