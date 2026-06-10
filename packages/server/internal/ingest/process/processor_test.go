package process

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/extract"
	ingestformat "github.com/MemaxLabs/memax/packages/server/internal/ingest/format"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/summarize"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// fakeEmbedder returns zero vectors of fixed dimensions. Used to make
// searchRelatedContext proceed past the embedding step.
type fakeEmbedder struct{ dims int }

func (f *fakeEmbedder) Embed(texts []string, _ string) ([][]float64, error) {
	return f.EmbedContext(context.Background(), texts, "")
}
func (f *fakeEmbedder) EmbedContext(_ context.Context, texts []string, _ string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = make([]float64, f.dims)
	}
	return out, nil
}
func (f *fakeEmbedder) Dimensions() int { return f.dims }

var _ embed.Embedder = (*fakeEmbedder)(nil)

// enrichmentStore wraps InMemoryStore to return fake enrichment candidates.
type enrichmentStore struct {
	*store.InMemoryStore
	candidates []model.EnrichmentCandidate
}

func (s *enrichmentStore) FindRelatedForEnrichment(_ context.Context, _ []float64, _ string, _ []string, _ string, _ int) ([]model.EnrichmentCandidate, error) {
	return s.candidates, nil
}

func TestProcessorProcessSetsProjectRepoAndChunks(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:          "mem-1",
		HubID:       "hub-1",
		OwnerID:     "user-1",
		Title:       "Draft",
		Content:     "# Draft",
		ContentType: "markdown",
		ContentHash: "hash",
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
		Boundary:    "private",
		State:       "processing",
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	p := New(s, nil, nil, nil, nil, nil, nil, nil, ingestformat.New(nil), nil, nil)
	req := model.PushRequest{
		Content:        "# Architecture\n\nLine one.\n\nLine two.",
		Title:          "Architecture",
		Hint:           "repo memory",
		Kind:           model.MemoryKindSemantic,
		Stability:      model.MemoryStabilityStable,
		Tags:           []string{"person:Ziyang", "memax_cli"},
		ContentType:    "markdown",
		ProjectContext: map[string]string{"repo": "github.com/acme/repo"},
	}
	if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	updated, err := s.GetMemory(memory.ID, memory.OwnerID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if updated.State != "active" {
		t.Fatalf("expected state active, got %q", updated.State)
	}
	if updated.Kind != model.MemoryKindSemantic || updated.Stability != model.MemoryStabilityStable {
		t.Fatalf("expected updated classification, got %s/%s", updated.Kind, updated.Stability)
	}

	chunks, err := s.GetChunksByMemory(memory.ID, memory.OwnerID)
	if err != nil {
		t.Fatalf("GetChunksByMemory: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks to be created")
	}
	for _, chunk := range chunks {
		if chunk.ProjectRepo != "github.com/acme/repo" {
			t.Fatalf("expected chunk project repo to be set, got %q", chunk.ProjectRepo)
		}
		if chunk.ChunkIndex == 0 {
			// Chunk 0 gets enriched with title prepended + tags appended to hint
			if !strings.Contains(chunk.Hint, "repo memory") {
				t.Fatalf("expected chunk 0 hint to contain original hint, got %q", chunk.Hint)
			}
			if !strings.Contains(chunk.Hint, "Architecture") {
				t.Fatalf("expected chunk 0 hint to contain title enrichment, got %q", chunk.Hint)
			}
			if !strings.Contains(chunk.Hint, "Tags: ") {
				t.Fatalf("expected chunk 0 hint to contain tag enrichment, got %q", chunk.Hint)
			}
			if !strings.Contains(chunk.Hint, "memax_cli") {
				t.Fatalf("expected chunk 0 hint tags to include memax_cli, got %q", chunk.Hint)
			}
		} else {
			if chunk.Hint != "repo memory" {
				t.Fatalf("expected non-zero chunk hint to be preserved, got %q", chunk.Hint)
			}
		}
		if !strings.Contains(chunk.TagsText, "person ziyang") || !strings.Contains(chunk.TagsText, "memax cli") {
			t.Fatalf("expected chunk tags text to include normalized tags, got %q", chunk.TagsText)
		}
	}
}

// TestProcessorScansUserFollowupMarker verifies the Phase 2a /
// plan-24 marker scan: when a memory body contains a @TODO/@FIXME/
// @QUESTION marker, the processor records the matched text on the
// memory's user_followup_marker column. Bodies without a marker
// get the empty string written (not NULL) so the trigger evaluator
// can distinguish "scan ran, no marker" from "scan never ran".
func TestProcessorScansUserFollowupMarker(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantMarker string
	}{
		{
			name:       "marker present",
			body:       "Auth notes.\n\n@TODO verify the API key rotation cadence",
			wantMarker: "@TODO verify the API key rotation cadence",
		},
		{
			name:       "no marker writes empty",
			body:       "Just some prose, nothing flagged.",
			wantMarker: "",
		},
		{
			name:       "fixme marker",
			body:       "Bug log:\n@FIXME refresh races under load",
			wantMarker: "@FIXME refresh races under load",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := store.NewInMemoryStore()
			now := time.Now()
			memID := "mem-marker-" + tc.name
			memory := &model.Memory{
				ID:          memID,
				HubID:       "hub-marker",
				OwnerID:     "user-marker",
				Title:       "x",
				Content:     "x",
				ContentType: "markdown",
				ContentHash: "h-" + tc.name,
				Kind:        model.MemoryKindSemantic,
				Stability:   model.MemoryStabilityEvolving,
				State:       "processing",
				CreatedAt:   now,
				UpdatedAt:   now,
				AccessedAt:  now,
			}
			if err := s.CreateMemory(memory); err != nil {
				t.Fatalf("CreateMemory: %v", err)
			}

			p := New(s, nil, nil, nil, nil, nil, nil, nil, ingestformat.New(nil), nil, nil)
			req := model.PushRequest{
				Content:     tc.body,
				ContentType: "markdown",
			}
			if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
				t.Fatalf("Process: %v", err)
			}

			updated, err := s.GetMemory(memory.ID, memory.OwnerID)
			if err != nil {
				t.Fatalf("GetMemory: %v", err)
			}
			if updated.UserFollowupMarker != tc.wantMarker {
				t.Errorf("UserFollowupMarker = %q, want %q", updated.UserFollowupMarker, tc.wantMarker)
			}
		})
	}
}

func TestProcessorProcessRenamesJunkFilenameFromContent(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:          "mem-2",
		HubID:       "hub-1",
		OwnerID:     "user-1",
		Title:       "Screenshot",
		Content:     "",
		ContentType: "image",
		ContentHash: "hash",
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
		Boundary:    "private",
		State:       "processing",
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	p := New(s, nil, nil, nil, nil, nil, nil, nil, ingestformat.New(nil), nil, nil)
	req := model.PushRequest{
		Content:     "# OAuth Redirect Fix\n\nUse the staging callback URL.",
		Title:       "Screenshot 2026-04-07 at 10.31.22 AM",
		SourcePath:  "Screenshot 2026-04-07 at 10.31.22 AM.png",
		ContentType: "image",
		Kind:        model.MemoryKindEpisodic,
		Stability:   model.MemoryStabilityVolatile,
	}
	if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	updated, err := s.GetMemory(memory.ID, memory.OwnerID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if updated.Title != "OAuth Redirect Fix" {
		t.Fatalf("expected content-derived title, got %q", updated.Title)
	}
}

func TestProcessorProcessNormalizesHTMLContent(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:          "mem-3",
		HubID:       "hub-1",
		OwnerID:     "user-1",
		Title:       "HTML Import",
		Content:     "",
		ContentType: "markdown",
		ContentHash: "hash",
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
		Boundary:    "private",
		State:       "processing",
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	p := New(s, nil, nil, nil, nil, nil, nil, nil, ingestformat.New(nil), nil, nil)
	req := model.PushRequest{
		Content:     `<!doctype html><html><body><h1>Deploy Guide</h1><p>Use the <strong>staging</strong> URL.</p></body></html>`,
		Title:       "deploy-guide",
		SourcePath:  "deploy-guide.html",
		ContentType: "markdown",
		Kind:        model.MemoryKindProcedural,
		Stability:   model.MemoryStabilityEvolving,
	}
	if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	updated, err := s.GetMemory(memory.ID, memory.OwnerID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if updated.Content == req.Content {
		t.Fatalf("expected content to be normalized, got raw html %q", updated.Content)
	}
	if updated.Content != "# Deploy Guide\n\nUse the **staging** URL." {
		t.Fatalf("unexpected normalized content: %q", updated.Content)
	}
}

func TestProcessorProcessUsesSummaryCallForTitleCandidate(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:          "mem-4",
		HubID:       "hub-1",
		OwnerID:     "user-1",
		Title:       "Document",
		Content:     "",
		ContentType: "pdf",
		ContentHash: "hash",
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
		Boundary:    "private",
		State:       "processing",
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	var llmCalls atomic.Int32
	fakeAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"text":"{\"title\":\"Quarterly Planning Notes\",\"summary\":\"**Quarterly planning** covered launch goals and staffing risks.\"}"}],"usage":{"input_tokens":25,"output_tokens":20}}`)
	}))
	defer fakeAnthropic.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", fakeAnthropic.URL)
	client := anthropic.NewFromEnv()
	if client == nil {
		t.Fatal("expected anthropic client")
	}

	p := New(s, nil, nil, summarize.New(client), nil, nil, nil, nil, ingestformat.New(nil), ingesttitle.New(client), nil)
	req := model.PushRequest{
		Content:     strings.Repeat("1234567890 ", 30),
		Title:       "Document",
		SourcePath:  "scan-2026-04-11.pdf",
		ContentType: "pdf",
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
	}
	if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	updated, err := s.GetMemory(memory.ID, memory.OwnerID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if updated.Title != "Quarterly Planning Notes" {
		t.Fatalf("expected title from summary call, got %q", updated.Title)
	}
	if updated.Summary != "**Quarterly planning** covered launch goals and staffing risks." {
		t.Fatalf("expected summary from same call, got %q", updated.Summary)
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected one LLM call for summary and title, got %d", llmCalls.Load())
	}
}

func TestHasMinSearchSignal(t *testing.T) {
	tests := []struct {
		name string
		text string
		tags []string
		want bool
	}{
		{"empty", "", nil, false},
		{"single word", "hello", nil, false},
		{"two meaningful words", "pricing decision", nil, true},
		{"stop words only", "what the how", nil, false},
		{"short words only", "go to it", nil, false},
		{"title with signal", "Q2 Pricing Decision", nil, true},
		{"tags compensate", "hey", []string{"pricing", "decisions"}, true},
		{"single tag insufficient", "hi", []string{"pricing"}, false},
		{"real short note", "Decided to go with $9/$19 tiers", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasMinSearchSignal(tt.text, tt.tags)
			if got != tt.want {
				t.Fatalf("hasMinSearchSignal(%q, %v) = %v, want %v", tt.text, tt.tags, got, tt.want)
			}
		})
	}
}

func TestRunesTruncate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		max     int
		wantLen int
	}{
		{"ascii within limit", "hello world", 20, 11},
		{"ascii at limit", "hello", 5, 5},
		{"ascii over limit", "hello world", 5, 5},
		{"cjk truncation safe", "你好世界测试文本", 4, 4},
		{"empty", "", 10, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runesTruncate(tt.input, tt.max)
			if len([]rune(got)) != tt.wantLen {
				t.Fatalf("runesTruncate(%q, %d) has %d runes, want %d", tt.input, tt.max, len([]rune(got)), tt.wantLen)
			}
		})
	}
}

func TestProcessorEnrichmentSkipsWhenNotAllowed(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID: "mem-enrich-1", HubID: "hub-1", OwnerID: "user-1",
		Title: "Draft", Content: "# Draft", ContentType: "markdown",
		ContentHash: "hash", Kind: model.MemoryKindSemantic,
		Stability: model.MemoryStabilityEvolving, Boundary: "private",
		State: "processing", CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	var llmCalls atomic.Int32
	fakeAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"text":"{\"title\":\"Test\",\"summary\":\"Test summary.\"}"}],"usage":{"input_tokens":10,"output_tokens":10}}`)
	}))
	defer fakeAnthropic.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", fakeAnthropic.URL)
	client := anthropic.NewFromEnv()

	p := New(s, nil, nil, summarize.New(client), nil, nil, nil, nil, ingestformat.New(nil), ingesttitle.New(client), nil)

	// AllowRelatedContext = false → enrichment search should not run.
	// The processor still runs summarization normally.
	req := model.PushRequest{
		Content:             "# Architecture\n\nMemax is a universal context and memory hub for AI agents. It provides persistent, shared, secure access to user and team knowledge. The system uses PostgreSQL with pgvector for embeddings, Redis for caching, and Voyage AI for embedding generation. The retrieval pipeline combines vector, BM25, and trigram search with RRF fusion.",
		Title:               "Architecture",
		ContentType:         "markdown",
		AllowRelatedContext: false,
	}
	if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	updated, _ := s.GetMemory(memory.ID, memory.OwnerID)
	if updated.State != "active" {
		t.Fatalf("expected state active, got %q", updated.State)
	}
	// Summarizer should still be called (1 call for title+summary)
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call (summary only, no enrichment), got %d", llmCalls.Load())
	}
}

func TestProcessorShadowModeDoesNotPersistEnrichedOutput(t *testing.T) {
	inner := store.NewInMemoryStore()
	s := &enrichmentStore{
		InMemoryStore: inner,
		candidates: []model.EnrichmentCandidate{
			{MemoryID: "related-1", Title: "Related Memory", Summary: "Related summary", Similarity: 0.85},
		},
	}
	now := time.Now()
	memory := &model.Memory{
		ID: "mem-shadow-1", HubID: "hub-1", OwnerID: "user-1",
		Title: "Draft", Content: "# Draft", ContentType: "markdown",
		ContentHash: "hash", Kind: model.MemoryKindSemantic,
		Stability: model.MemoryStabilityEvolving, Boundary: "private",
		State: "processing", CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	}
	if err := inner.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	var llmCalls atomic.Int32
	fakeAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := llmCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First call: baseline summary
			fmt.Fprint(w, `{"content":[{"text":"{\"title\":\"Baseline Title\",\"summary\":\"Baseline summary.\"}"}],"usage":{"input_tokens":10,"output_tokens":10}}`)
		} else {
			// Second call: enriched summary (should NOT be persisted)
			fmt.Fprint(w, `{"content":[{"text":"{\"title\":\"Enriched Title\",\"summary\":\"Enriched summary with related context.\"}"}],"usage":{"input_tokens":10,"output_tokens":10}}`)
		}
	}))
	defer fakeAnthropic.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", fakeAnthropic.URL)
	t.Setenv("ENRICHMENT_SHADOW_MODE", "true")
	client := anthropic.NewFromEnv()

	emb := &fakeEmbedder{dims: 8}
	p := New(s, nil, emb, summarize.New(client), nil, nil, nil, nil, ingestformat.New(nil), ingesttitle.New(client), nil)

	req := model.PushRequest{
		Content:             "# Architecture\n\nMemax is a universal context and memory hub for AI agents. It provides persistent, shared, secure access to user and team knowledge. The system uses PostgreSQL with pgvector for embeddings, Redis for caching, and Voyage AI for embedding generation. The retrieval pipeline combines vector, BM25, and trigram search with RRF fusion.",
		Title:               "Architecture",
		ContentType:         "markdown",
		AllowRelatedContext: true,
	}
	if err := p.Process(t.Context(), memory.ID, memory.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	updated, _ := inner.GetMemory(memory.ID, memory.OwnerID)
	if updated.State != "active" {
		t.Fatalf("expected state active, got %q", updated.State)
	}
	// Shadow mode: 2 LLM calls (baseline + enriched), NOT 3.
	// The baseline result is reused for persistence.
	if llmCalls.Load() != 2 {
		t.Fatalf("expected 2 LLM calls (baseline + enriched in shadow), got %d", llmCalls.Load())
	}
	// Persisted summary must be the baseline, not the enriched version.
	if updated.Summary != "Baseline summary." {
		t.Fatalf("expected baseline summary persisted, got %q", updated.Summary)
	}
}

func TestProcessorExtractionInheritsParentProvenance(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	parent := &model.Memory{
		ID:                             "mem-extract-parent",
		HubID:                          "hub-1",
		OwnerID:                        "user-1",
		Title:                          "Session capture",
		Content:                        "",
		ContentType:                    "transcript",
		ContentHash:                    "hash",
		Kind:                           model.MemoryKindEpisodic,
		Stability:                      model.MemoryStabilityVolatile,
		Boundary:                       "private",
		State:                          "processing",
		Source:                         "hook",
		SourceAgent:                    "claude-code",
		ProvenanceCreatedByType:        model.MemoryCreatedByAgent,
		ProvenanceCreatedBySlug:        "claude-code",
		ProvenanceCreatedByDisplayName: "Claude Code",
		ProvenanceCreatedVia:           "hook",
		ProvenanceAssistedByAgent:      "codex",
		ProvenanceInitiationType:       model.MemoryInitiationAgentAutomatic,
		ProvenanceAttributionSource:    model.MemoryAttributionSourceAuth,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
	}
	if err := s.CreateMemory(parent); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	fakeAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"text":"[{\"fact\":\"Claude Code decided to keep auth-derived attribution and separate assisted_by_agent for collaboration.\",\"kind_hint\":\"rationale\",\"confidence\":0.92}]"}],"usage":{"input_tokens":20,"output_tokens":20}}`)
	}))
	defer fakeAnthropic.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", fakeAnthropic.URL)
	client := anthropic.NewFromEnv()

	p := New(s, nil, nil, nil, extract.New(client), nil, nil, nil, ingestformat.New(nil), nil, nil)
	req := model.PushRequest{
		Content: `Claude Code: We should keep auth-derived attribution.
Human: Agreed, and collaboration should use assisted_by_agent.
Claude Code: Let's make child extraction inherit provenance from the parent.
Human: Also keep source_agent only for compatibility.
Claude Code: That avoids agent identity drift in extracted facts.
Human: Good, ship it.`,
		Title:       "Session capture",
		ContentType: "transcript",
	}
	if err := p.Process(t.Context(), parent.ID, parent.OwnerID, req); err != nil {
		t.Fatalf("Process: %v", err)
	}

	memories, err := s.ListMemories(parent.OwnerID, 10)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	var child *model.Memory
	for i := range memories {
		if memories[i].Source == "extraction" && memories[i].SourcePath == parent.ID {
			child = &memories[i]
			break
		}
	}
	if child == nil {
		t.Fatal("expected extracted child memory")
	}
	if child.Provenance == nil {
		t.Fatal("expected child provenance")
	}
	if child.Provenance.CreatedBySlug != "claude-code" {
		t.Fatalf("created_by_slug = %q, want %q", child.Provenance.CreatedBySlug, "claude-code")
	}
	if child.Provenance.CreatedByType != model.MemoryCreatedByAgent {
		t.Fatalf("created_by_type = %q, want %q", child.Provenance.CreatedByType, model.MemoryCreatedByAgent)
	}
	if child.Provenance.AttributionSource != model.MemoryAttributionSourceInherited {
		t.Fatalf("attribution_source = %q, want %q", child.Provenance.AttributionSource, model.MemoryAttributionSourceInherited)
	}
	if child.Provenance.InitiationType != model.MemoryInitiationAgentAutomatic {
		t.Fatalf("initiation_type = %q, want %q", child.Provenance.InitiationType, model.MemoryInitiationAgentAutomatic)
	}
	if child.Provenance.AssistedByAgent != "codex" {
		t.Fatalf("assisted_by_agent = %q, want %q", child.Provenance.AssistedByAgent, "codex")
	}
}

func TestInheritProvenanceUsesImmediateParentForTransitiveChains(t *testing.T) {
	parent := &model.Memory{
		ProvenanceCreatedByType:        model.MemoryCreatedByAgent,
		ProvenanceCreatedBySlug:        "claude-code",
		ProvenanceCreatedByDisplayName: "Claude Code",
		ProvenanceCreatedVia:           "hook",
		ProvenanceAssistedByAgent:      "codex",
		ProvenanceInitiationType:       model.MemoryInitiationAgentAutomatic,
		ProvenanceAttributionSource:    model.MemoryAttributionSourceAuth,
	}
	model.NormalizeMemoryProvenanceFields(parent)

	childProv := inheritProvenance(parent)
	child := &model.Memory{
		ProvenanceCreatedByType:        childProv.CreatedByType,
		ProvenanceCreatedBySlug:        childProv.CreatedBySlug,
		ProvenanceCreatedByDisplayName: childProv.CreatedByDisplayName,
		ProvenanceCreatedVia:           childProv.CreatedVia,
		ProvenanceAssistedByAgent:      childProv.AssistedByAgent,
		ProvenanceInitiationType:       childProv.InitiationType,
		ProvenanceAttributionSource:    childProv.AttributionSource,
	}
	model.NormalizeMemoryProvenanceFields(child)

	grandchildProv := inheritProvenance(child)
	if grandchildProv.CreatedBySlug != childProv.CreatedBySlug {
		t.Fatalf("grandchild created_by_slug = %q, want %q", grandchildProv.CreatedBySlug, childProv.CreatedBySlug)
	}
	if grandchildProv.AssistedByAgent != childProv.AssistedByAgent {
		t.Fatalf("grandchild assisted_by_agent = %q, want %q", grandchildProv.AssistedByAgent, childProv.AssistedByAgent)
	}
	if grandchildProv.AttributionSource != model.MemoryAttributionSourceInherited {
		t.Fatalf("grandchild attribution_source = %q, want %q", grandchildProv.AttributionSource, model.MemoryAttributionSourceInherited)
	}
	if grandchildProv.InitiationType != model.MemoryInitiationAgentAutomatic {
		t.Fatalf("grandchild initiation_type = %q, want %q", grandchildProv.InitiationType, model.MemoryInitiationAgentAutomatic)
	}
}
