package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/cache"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type stubReranker struct {
	topN    int
	results []rerank.Result
	calls   int
	docs    []rerank.Document
}

func (s *stubReranker) TopN() int { return s.topN }

func (s *stubReranker) Rerank(_ context.Context, _ string, docs []rerank.Document) ([]rerank.Result, error) {
	s.calls++
	s.docs = append([]rerank.Document(nil), docs...)
	return s.results, nil
}

type stubQueryEmbedder struct {
	calls int
}

func (s *stubQueryEmbedder) Embed(texts []string, inputType string) ([][]float64, error) {
	return s.EmbedContext(context.Background(), texts, inputType)
}

func (s *stubQueryEmbedder) EmbedContext(_ context.Context, texts []string, _ string) ([][]float64, error) {
	s.calls++
	embeddings := make([][]float64, 0, len(texts))
	for range texts {
		embeddings = append(embeddings, []float64{0.25, 0.75})
	}
	return embeddings, nil
}

func (s *stubQueryEmbedder) Dimensions() int { return 2 }

type stubRecallCache struct {
	values map[string]string
}

var _ cache.Cache = (*stubRecallCache)(nil)

func (s *stubRecallCache) Get(_ context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", io.EOF
}

func TestAppendStructuredFilterTermsIncludesAuthors(t *testing.T) {
	filters := &model.SearchFilters{
		Authors: []string{"Jiahao", "Jiahao"},
		People:  []string{"Ziyang"},
		Source:  "claude-code",
		Hub:     "memax-test",
	}

	got := appendStructuredFilterTerms("push yesterday", filters)
	for _, want := range []string{"push yesterday", "Jiahao", "Ziyang", "claude-code", "memax-test"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
	if strings.Count(got, "Jiahao") != 1 {
		t.Fatalf("expected duplicate authors to be deduplicated, got %q", got)
	}
}

func TestShouldEmbedDistilledQuery(t *testing.T) {
	tests := []struct {
		name       string
		original   string
		distilled  string
		confidence float64
		want       bool
	}{
		{
			name:       "long noisy query re-embeds",
			original:   strings.Repeat("agent context ", 70),
			distilled:  "deployment process",
			confidence: 0.8,
			want:       true,
		},
		{
			name:       "large character count re-embeds",
			original:   strings.Repeat("a", 401),
			distilled:  "api timeout",
			confidence: 0.8,
			want:       true,
		},
		{
			name:       "strong compression re-embeds",
			original:   "one two three four five six seven eight nine ten eleven twelve",
			distilled:  "deploy notes",
			confidence: 0.8,
			want:       true,
		},
		{
			name:       "low confidence keeps raw embedding",
			original:   strings.Repeat("agent context ", 70),
			distilled:  "deployment process",
			confidence: 0.4,
			want:       false,
		},
		{
			name:       "same query keeps raw embedding",
			original:   "deploy process",
			distilled:  "deploy process",
			confidence: 0.9,
			want:       false,
		},
		{
			name:       "short similar query keeps raw embedding",
			original:   "deploy process today",
			distilled:  "deploy process",
			confidence: 0.9,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldEmbedDistilledQuery(tt.original, tt.distilled, tt.confidence); got != tt.want {
				t.Fatalf("shouldEmbedDistilledQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func (s *stubRecallCache) Set(_ context.Context, key string, value string, _ time.Duration) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *stubRecallCache) SetNX(_ context.Context, key string, value string, _ time.Duration) (bool, error) {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	if _, exists := s.values[key]; exists {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}

func (s *stubRecallCache) Del(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func (s *stubRecallCache) Close() error { return nil }

func TestRecallHandlerCachesQueryEmbeddings(t *testing.T) {
	embedder := &stubQueryEmbedder{}
	h := NewRecallHandler(store.NewInMemoryStore(), embedder, nil, nil, &stubRecallCache{values: make(map[string]string)})

	first, err := h.embedQuery(context.Background(), "deploy process")
	if err != nil {
		t.Fatalf("embedQuery first call: %v", err)
	}
	second, err := h.embedQuery(context.Background(), "deploy process")
	if err != nil {
		t.Fatalf("embedQuery second call: %v", err)
	}
	if embedder.calls != 1 {
		t.Fatalf("expected one embedder call, got %d", embedder.calls)
	}
	if len(first) != 1 || len(second) != 1 || first[0][0] != second[0][0] || first[0][1] != second[0][1] {
		t.Fatalf("expected cached embedding to match first embedding, got %#v and %#v", first, second)
	}
}

func TestRecallRunPipelineAllowsAccessibleHubMemories(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:              "shared-memory",
		HubID:           "shared-hub",
		OwnerID:         "owner-b",
		Title:           "Deploy Notes",
		Content:         "deploy runbook",
		ContentType:     "markdown",
		ContentHash:     "hash",
		Kind:            model.MemoryKindProcedural,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Boundary:        "private",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	if err := s.CreateChunks([]model.Chunk{{
		ID:              "chunk-1",
		MemoryID:        memory.ID,
		Content:         "deploy runbook",
		HeadingChain:    "Deploy",
		ChunkIndex:      0,
		TokenCount:      2,
		Kind:            memory.Kind,
		Stability:       memory.Stability,
		RetrievalWeight: memory.RetrievalWeight,
		CreatedAt:       now,
	}}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	h := NewRecallHandler(s, nil, nil, nil, nil)

	resultsWithoutHub, _, err := h.RunPipeline(context.Background(), "deploy", "api", "", nil, 5, "owner-a", nil, RecallScope{})
	if err != nil {
		t.Fatalf("RunPipeline without hub: %v", err)
	}
	if len(resultsWithoutHub) != 0 {
		t.Fatalf("expected no results without hub access, got %d", len(resultsWithoutHub))
	}

	resultsWithHub, _, err := h.RunPipeline(context.Background(), "deploy", "api", "", nil, 5, "owner-a", nil, NewRecallScope([]string{"shared-hub"}, ""))
	if err != nil {
		t.Fatalf("RunPipeline with hub: %v", err)
	}
	if len(resultsWithHub) != 1 {
		t.Fatalf("expected one accessible result, got %d", len(resultsWithHub))
	}
	if resultsWithHub[0].ID != memory.ID {
		t.Fatalf("expected memory %q, got %q", memory.ID, resultsWithHub[0].ID)
	}
}

func TestRecallHandlerUsesAccessibleHubScope(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:              "shared-memory",
		HubID:           "shared-hub",
		OwnerID:         "owner-b",
		Title:           "Shared UX Notes",
		Content:         "forget inbox ux review batch operations",
		ContentType:     "markdown",
		ContentHash:     "hash",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Boundary:        "private",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	if err := s.CreateChunks([]model.Chunk{{
		ID:              "chunk-1",
		MemoryID:        memory.ID,
		Content:         "forget inbox ux review batch operations",
		HeadingChain:    "UX",
		ChunkIndex:      0,
		TokenCount:      6,
		Kind:            memory.Kind,
		Stability:       memory.Stability,
		RetrievalWeight: memory.RetrievalWeight,
		CreatedAt:       now,
	}}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	h := NewRecallHandler(s, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", strings.NewReader(`{"query":"forget inbox ux"}`))
	ctx := context.WithValue(req.Context(), userIDKey, "owner-a")
	ctx = context.WithValue(ctx, hubIDKey, "personal-hub")
	ctx = context.WithValue(ctx, hubIDsKey, []string{"personal-hub", "shared-hub"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Recall(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data  model.RecallResult `json:"data"`
		Error *model.Error       `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload.Error != nil {
		t.Fatalf("unexpected error: %#v", payload.Error)
	}
	if len(payload.Data.Memories) != 1 {
		t.Fatalf("expected one shared memory, got %d: %#v", len(payload.Data.Memories), payload.Data.Memories)
	}
	if payload.Data.Memories[0].ID != memory.ID {
		t.Fatalf("expected memory %q, got %q", memory.ID, payload.Data.Memories[0].ID)
	}
}

func TestRecallRunPipelineBoostsActiveHubWithinAccessibleScope(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memories := []model.Memory{
		{
			ID:              "personal-memory",
			HubID:           "personal-hub",
			OwnerID:         "owner-a",
			Title:           "UX Notes",
			Content:         "forget inbox ux review batch operations",
			ContentType:     "markdown",
			ContentHash:     "hash-personal",
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "team-memory",
			HubID:           "team-hub",
			OwnerID:         "owner-b",
			Title:           "UX Notes",
			Content:         "forget inbox ux review batch operations",
			ContentType:     "markdown",
			ContentHash:     "hash-team",
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
	}
	for _, memory := range memories {
		mem := memory
		if err := s.CreateMemory(&mem); err != nil {
			t.Fatalf("CreateMemory(%s): %v", mem.ID, err)
		}
		if err := s.CreateChunks([]model.Chunk{{
			ID:              mem.ID + "-chunk",
			MemoryID:        mem.ID,
			Content:         mem.Content,
			HeadingChain:    "UX",
			ChunkIndex:      0,
			TokenCount:      6,
			Kind:            mem.Kind,
			Stability:       mem.Stability,
			RetrievalWeight: mem.RetrievalWeight,
			CreatedAt:       now,
		}}); err != nil {
			t.Fatalf("CreateChunks(%s): %v", mem.ID, err)
		}
	}

	h := NewRecallHandler(s, nil, nil, nil, nil)
	results, _, err := h.RunPipeline(context.Background(), "forget inbox ux", "api", "", nil, 2, "owner-a", nil, NewRecallScope([]string{"personal-hub", "team-hub"}, "team-hub"))
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected two results, got %d: %#v", len(results), results)
	}
	if results[0].ID != "team-memory" {
		t.Fatalf("expected active hub memory first, got %#v", results)
	}
}

func TestRecallRunPipelineAppliesSelectiveRerank(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:        "team-hub",
		Name:      "Team Knowledge",
		Slug:      "team-knowledge",
		HubType:   "team",
		OwnerID:   "owner-a",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	memories := []model.Memory{
		{
			ID:              "mem-1",
			OwnerID:         "owner-a",
			HubID:           "team-hub",
			Title:           "Deploy Runbook",
			Content:         "deploy staging runbook",
			ContentType:     "markdown",
			ContentHash:     "hash-1",
			Kind:            model.MemoryKindProcedural,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "mem-2",
			OwnerID:         "owner-a",
			HubID:           "team-hub",
			Title:           "OAuth Redirect Fix",
			Content:         "deploy oauth redirect fix",
			ContentType:     "markdown",
			ContentHash:     "hash-2",
			Kind:            model.MemoryKindEpisodic,
			Stability:       model.MemoryStabilityVolatile,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "mem-3",
			OwnerID:         "owner-a",
			HubID:           "team-hub",
			Title:           "Staging Checklist",
			Content:         "deploy staging checklist",
			ContentType:     "markdown",
			ContentHash:     "hash-3",
			Kind:            model.MemoryKindProcedural,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
	}
	for _, memory := range memories {
		mem := memory
		if err := s.CreateMemory(&mem); err != nil {
			t.Fatalf("CreateMemory(%s): %v", mem.ID, err)
		}
		if err := s.CreateChunks([]model.Chunk{{
			ID:              mem.ID + "-chunk",
			MemoryID:        mem.ID,
			Content:         mem.Content,
			HeadingChain:    mem.Title,
			ChunkIndex:      0,
			TokenCount:      3,
			Kind:            mem.Kind,
			Stability:       mem.Stability,
			RetrievalWeight: mem.RetrievalWeight,
			CreatedAt:       now,
		}}); err != nil {
			t.Fatalf("CreateChunks(%s): %v", mem.ID, err)
		}
	}

	reranker := &stubReranker{
		topN: 3,
		results: []rerank.Result{
			{ID: "mem-2", Score: 0.98},
			{ID: "mem-3", Score: 0.52},
			{ID: "mem-1", Score: 0.41},
		},
	}
	h := NewRecallHandler(s, nil, nil, reranker, &stubRecallCache{values: make(map[string]string)})

	results, metadata, err := h.RunPipeline(context.Background(), "deploy", "api", "", nil, 3, "owner-a", nil, RecallScope{})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if !metadata.Reranked {
		t.Fatalf("expected rerank metadata to be true")
	}
	if metadata.RerankReason == "" {
		t.Fatalf("expected rerank reason to be populated")
	}
	if len(results) == 0 || results[0].ID != "mem-2" {
		t.Fatalf("expected mem-2 to rank first after rerank, got %#v", results)
	}
	if len(reranker.docs) == 0 {
		t.Fatalf("expected reranker documents to be captured")
	}
	for _, doc := range reranker.docs {
		if strings.Contains(doc.Content, "Hub ID:") {
			t.Fatalf("expected rerank document to omit hub UUID, got %q", doc.Content)
		}
	}
	if !strings.Contains(reranker.docs[0].Content, "Hub: Team Knowledge") {
		t.Fatalf("expected rerank document to include hub name, got %q", reranker.docs[0].Content)
	}

	resultsCached, metadataCached, err := h.RunPipeline(context.Background(), "deploy", "ask", "", nil, 3, "owner-a", nil, RecallScope{})
	if err != nil {
		t.Fatalf("RunPipeline cached: %v", err)
	}
	if len(resultsCached) == 0 || resultsCached[0].ID != "mem-2" {
		t.Fatalf("expected cached reranked results, got %#v", resultsCached)
	}
	if !metadataCached.Reranked {
		t.Fatalf("expected cached metadata to preserve reranked state")
	}
	if reranker.calls != 1 {
		t.Fatalf("expected reranker to be called once due to recall cache, got %d", reranker.calls)
	}
}

func TestRecallRunPipelineExpandsSiblingChunksForHowTo(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:              "deploy-memory",
		OwnerID:         "owner-a",
		Title:           "Staging Deploy Runbook",
		Content:         "install prerequisites\n\ndeploy staging with fly deploy\n\nverify smoke tests",
		ContentType:     "markdown",
		ContentHash:     "hash-deploy",
		Kind:            model.MemoryKindProcedural,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Boundary:        "private",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	if err := s.CreateChunks([]model.Chunk{
		{
			ID:              "deploy-chunk-0",
			MemoryID:        memory.ID,
			Content:         "install prerequisites",
			HeadingChain:    "Prepare",
			ChunkIndex:      0,
			TokenCount:      2,
			Kind:            memory.Kind,
			Stability:       memory.Stability,
			RetrievalWeight: memory.RetrievalWeight,
			CreatedAt:       now,
		},
		{
			ID:              "deploy-chunk-1",
			MemoryID:        memory.ID,
			Content:         "deploy staging with fly deploy",
			HeadingChain:    "Deploy",
			ChunkIndex:      1,
			TokenCount:      5,
			Kind:            memory.Kind,
			Stability:       memory.Stability,
			RetrievalWeight: memory.RetrievalWeight,
			CreatedAt:       now,
		},
		{
			ID:              "deploy-chunk-2",
			MemoryID:        memory.ID,
			Content:         "verify smoke tests",
			HeadingChain:    "Verify",
			ChunkIndex:      2,
			TokenCount:      3,
			Kind:            memory.Kind,
			Stability:       memory.Stability,
			RetrievalWeight: memory.RetrievalWeight,
			CreatedAt:       now,
		},
	}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	h := NewRecallHandler(s, nil, nil, nil, nil)
	results, _, err := h.RunPipeline(context.Background(), "how to deploy staging", "api", "", nil, 1, "owner-a", nil, RecallScope{})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	for _, want := range []string{"install prerequisites", "deploy staging with fly deploy", "verify smoke tests", "[Section: Deploy | matched]"} {
		if !strings.Contains(results[0].ChunkContent, want) {
			t.Fatalf("expected expanded content to contain %q, got %q", want, results[0].ChunkContent)
		}
	}
}

func TestRecallRunPipelinePrefersHeadingMatches(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memories := []model.Memory{
		{
			ID:              "mem-heading",
			OwnerID:         "owner-a",
			Title:           "Staging Notes",
			Content:         "oauth redirect callback mismatch during staging deploy",
			ContentType:     "markdown",
			ContentHash:     "hash-heading",
			Kind:            model.MemoryKindEpisodic,
			Stability:       model.MemoryStabilityVolatile,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "mem-body",
			OwnerID:         "owner-a",
			Title:           "Redirect Issue",
			Content:         "oauth redirect callback mismatch during staging deploy",
			ContentType:     "markdown",
			ContentHash:     "hash-body",
			Kind:            model.MemoryKindEpisodic,
			Stability:       model.MemoryStabilityVolatile,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
	}
	chunks := []model.Chunk{
		{
			ID:              "chunk-heading",
			MemoryID:        "mem-heading",
			Content:         memories[0].Content,
			HeadingChain:    "OAuth Redirect",
			ChunkIndex:      0,
			TokenCount:      6,
			Kind:            memories[0].Kind,
			Stability:       memories[0].Stability,
			RetrievalWeight: memories[0].RetrievalWeight,
			CreatedAt:       now,
		},
		{
			ID:              "chunk-body",
			MemoryID:        "mem-body",
			Content:         memories[1].Content,
			HeadingChain:    "Deployment Notes",
			ChunkIndex:      0,
			TokenCount:      6,
			Kind:            memories[1].Kind,
			Stability:       memories[1].Stability,
			RetrievalWeight: memories[1].RetrievalWeight,
			CreatedAt:       now,
		},
	}
	for _, memory := range memories {
		mem := memory
		if err := s.CreateMemory(&mem); err != nil {
			t.Fatalf("CreateMemory(%s): %v", mem.ID, err)
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	h := NewRecallHandler(s, nil, nil, nil, nil)
	results, _, err := h.RunPipeline(context.Background(), "oauth redirect", "api", "", nil, 2, "owner-a", nil, RecallScope{})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least two results, got %d", len(results))
	}
	if results[0].ID != "mem-heading" {
		t.Fatalf("expected heading match to rank first, got %q", results[0].ID)
	}
}

func TestRecallRunPipelinePrefersIdentifierMatches(t *testing.T) {
	s := store.NewInMemoryStore()
	now := time.Now()
	memories := []model.Memory{
		{
			ID:              "mem-identifier",
			OwnerID:         "owner-a",
			Title:           "OAUTH_GITHUB_REDIRECT_URL staging fix",
			Content:         "oauth redirect callback mismatch for github login staging deploy",
			ContentType:     "markdown",
			ContentHash:     "hash-identifier",
			Kind:            model.MemoryKindEpisodic,
			Stability:       model.MemoryStabilityVolatile,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			SourcePath:      "docs/env/OAUTH_GITHUB_REDIRECT_URL.md",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "mem-generic",
			OwnerID:         "owner-a",
			Title:           "OAuth redirect staging notes",
			Content:         "oauth redirect callback mismatch for github login staging deploy",
			ContentType:     "markdown",
			ContentHash:     "hash-generic",
			Kind:            model.MemoryKindEpisodic,
			Stability:       model.MemoryStabilityVolatile,
			RetrievalWeight: 1.0,
			Boundary:        "private",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
	}
	chunks := []model.Chunk{
		{
			ID:              "chunk-identifier",
			MemoryID:        "mem-identifier",
			Content:         memories[0].Content,
			HeadingChain:    "Set OAUTH_GITHUB_REDIRECT_URL",
			ChunkIndex:      0,
			TokenCount:      8,
			Kind:            memories[0].Kind,
			Stability:       memories[0].Stability,
			RetrievalWeight: memories[0].RetrievalWeight,
			CreatedAt:       now,
		},
		{
			ID:              "chunk-generic",
			MemoryID:        "mem-generic",
			Content:         memories[1].Content,
			HeadingChain:    "OAuth redirect fix",
			ChunkIndex:      0,
			TokenCount:      8,
			Kind:            memories[1].Kind,
			Stability:       memories[1].Stability,
			RetrievalWeight: memories[1].RetrievalWeight,
			CreatedAt:       now,
		},
	}
	for _, memory := range memories {
		mem := memory
		if err := s.CreateMemory(&mem); err != nil {
			t.Fatalf("CreateMemory(%s): %v", mem.ID, err)
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	h := NewRecallHandler(s, nil, nil, nil, nil)
	results, _, err := h.RunPipeline(context.Background(), "oauth redirect OAUTH_GITHUB_REDIRECT_URL", "api", "", nil, 2, "owner-a", nil, RecallScope{})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least two results, got %d", len(results))
	}
	if results[0].ID != "mem-identifier" {
		t.Fatalf("expected identifier match to rank first, got %q", results[0].ID)
	}
}

func TestExtractIdentifierTerms(t *testing.T) {
	terms := extractIdentifierTerms("fix OAUTH_GITHUB_REDIRECT_URL in api/auth.ts for callbackV2")
	expected := []string{"oauth_github_redirect_url", "api/auth.ts", "callbackv2"}
	if len(terms) != len(expected) {
		t.Fatalf("expected %d identifier terms, got %v", len(expected), terms)
	}
	for i, term := range expected {
		if terms[i] != term {
			t.Fatalf("expected identifier term %q at index %d, got %q", term, i, terms[i])
		}
	}
}

func TestNormalizeAndFilterResults(t *testing.T) {
	tests := []struct {
		name      string
		scores    []float64
		wantCount int
		wantTop   float64 // expected normalized score for rank 1
	}{
		{
			name:      "empty input",
			scores:    nil,
			wantCount: 0,
		},
		{
			name:      "strong top score uses normal threshold 0.15",
			scores:    []float64{0.20, 0.10, 0.05, 0.02, 0.01},
			wantCount: 3, // normalized: 1.0, 0.50, 0.25, 0.10, 0.05 → 0.10 < 0.15
			wantTop:   1.0,
		},
		{
			name:      "weak top score uses tight threshold 0.40",
			scores:    []float64{0.03, 0.025, 0.015, 0.010},
			wantCount: 3, // normalized: 1.0, 0.833, 0.50, 0.333 → 0.333 < 0.40
			wantTop:   1.0,
		},
		{
			name:      "all scores equal — all survive",
			scores:    []float64{0.10, 0.10, 0.10},
			wantCount: 3,
			wantTop:   1.0,
		},
		{
			name:      "single result",
			scores:    []float64{0.05},
			wantCount: 1,
			wantTop:   1.0,
		},
		{
			name:      "top below absolute gate returns empty via pipeline minAbsoluteRecallScore",
			scores:    []float64{0.005},
			wantCount: 1, // normalizeAndFilterResults keeps it; the pipeline gate is separate
			wantTop:   1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := make([]model.RecalledMemory, len(tt.scores))
			for i, s := range tt.scores {
				results[i] = model.RecalledMemory{
					ID:             strings.Repeat("a", i+1),
					RelevanceScore: s,
				}
			}
			got := normalizeAndFilterResults(results)
			if len(got) != tt.wantCount {
				scores := make([]float64, len(got))
				for i, r := range got {
					scores[i] = r.RelevanceScore
				}
				t.Fatalf("count: got %d, want %d (scores=%v)", len(got), tt.wantCount, scores)
			}
			if tt.wantCount > 0 && got[0].RelevanceScore != tt.wantTop {
				t.Fatalf("top score: got %.4f, want %.4f", got[0].RelevanceScore, tt.wantTop)
			}
		})
	}
}

func TestPreserveRawEntityTerms(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		distilled string
		want      []string
	}{
		{
			name:      "hub slug preserved when dropped",
			raw:       "memax-test sprint review roadmap",
			distilled: "sprint review roadmap",
			want:      []string{"memax-test"},
		},
		{
			name:      "repo path preserved",
			raw:       "auth failures in github.com/MemaxLabs/memax-internal",
			distilled: "auth failures",
			want:      []string{"github.com/memaxlabs/memax-internal"},
		},
		{
			name:      "source agent preserved",
			raw:       "what did claude-code capture about auth",
			distilled: "auth capture",
			want:      []string{"claude-code"},
		},
		{
			name:      "colon-prefixed metadata preserved",
			raw:       "hub:memax-test auth decisions",
			distilled: "auth decisions",
			want:      []string{"hub:memax-test"},
		},
		{
			name:      "ordinary words NOT preserved",
			raw:       "what did we discuss about the roadmap notes",
			distilled: "roadmap",
			want:      nil,
		},
		{
			name:      "term already in distilled not duplicated",
			raw:       "memax-test sprint review",
			distilled: "memax-test sprint review",
			want:      nil,
		},
		{
			name:      "multiple entity terms preserved",
			raw:       "platform-private claude-code auth in github.com/MemaxLabs/memax-internal",
			distilled: "auth",
			want:      []string{"platform-private", "claude-code", "github.com/memaxlabs/memax-internal"},
		},
		{
			name:      "capped at 5 terms",
			raw:       "a-b c-d e-f g-h i-j k-l m-n",
			distilled: "",
			want:      []string{"a-b", "c-d", "e-f", "g-h", "i-j"},
		},
		{
			name:      "CamelCase identifier preserved",
			raw:       "MemaxLabs pricing strategy",
			distilled: "pricing strategy",
			want:      []string{"memaxlabs"},
		},
		{
			name:      "underscore identifier preserved",
			raw:       "memory_kind field documentation",
			distilled: "field documentation",
			want:      []string{"memory_kind"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preserveRawEntityTerms(tt.raw, tt.distilled)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("term %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMetadataEntityBoost(t *testing.T) {
	tests := []struct {
		name     string
		metaText string
		terms    []string
		want     float64
	}{
		{"no identifier terms", "hub:memax-test", nil, 1.0},
		{"empty metadata", "", []string{"memax-test"}, 1.0},
		{"hub slug match", "hub:memax-test hub memax test", []string{"memax-test"}, 1.4},
		{"repo path match", "repo:github.com/memaxlabs/memax", []string{"github.com/memaxlabs/memax"}, 1.4},
		{"two entity matches", "hub:memax-test agent:claude-code", []string{"memax-test", "claude-code"}, 1.5},
		{"no match", "hub:memax-test", []string{"platform-private"}, 1.0},
		{"capped at 1.8", "a-b c-d e-f g-h i-j", []string{"a-b", "c-d", "e-f", "g-h", "i-j"}, 1.8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk := model.Chunk{MetadataText: tt.metaText}
			got := metadataEntityBoost(chunk, tt.terms)
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			if diff > 0.001 {
				t.Fatalf("boost = %.4f, want %.4f", got, tt.want)
			}
		})
	}
}

func TestShouldApplyHubFilter(t *testing.T) {
	tests := []struct {
		name  string
		query string
		slug  string
		want  bool
	}{
		// Explicit English hub intent — should scope
		{"explicit: in X hub", "what did Jiahao push in the memax hub?", "memax", true},
		{"explicit: in X hub no article", "search in memax hub", "memax", true},
		{"explicit: from X hub", "from memax hub get auth notes", "memax", true},
		{"explicit: X team", "memax team decisions", "memax", true},
		{"explicit: X workspace", "memax workspace notes", "memax", true},
		{"explicit: hub:X", "hub:memax-test auth", "memax-test", true},

		// Explicit CJK hub intent — should scope
		{"explicit CJK: X里", "memax里的auth笔记", "memax", true},
		{"explicit CJK: X 里的", "memax 里的auth笔记", "memax", true},
		{"explicit CJK: X 团队", "memax 团队的决定", "memax", true},
		{"explicit CJK: X 工作区", "memax 工作区", "memax", true},
		{"explicit CJK: 在X hub", "在memax hub查找", "memax", true},
		{"explicit CJK: X hub 里", "memax hub 里的记录", "memax", true},

		// Product/project name — should NOT scope
		{"product name: memax email", "memax 用什么 email 选型？", "memax", false},
		{"product name: english", "what do we use for email for memax?", "memax", false},
		{"product name: memax pricing", "memax pricing model v2", "memax", false},
		{"product name: memax auth", "memax auth architecture", "memax", false},
		{"product name: memax design", "memax design system", "memax", false},

		// Slug doesn't appear in query at all — should NOT scope
		{"no slug in query", "email provider selection", "memax", false},
		{"unrelated query", "how to deploy to production", "memax-test", false},

		// Case insensitive
		{"case insensitive", "in the MEMAX hub auth notes", "memax", true},
		{"case insensitive slug", "what's in the Memax Hub?", "memax", true},

		// Hyphenated slugs
		{"hyphenated: in X hub", "in the memax-test hub", "memax-test", true},
		{"hyphenated: product name", "memax-test sprint results", "memax-test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldApplyHubFilter(tt.query, tt.slug)
			if got != tt.want {
				t.Errorf("shouldApplyHubFilter(%q, %q) = %v, want %v", tt.query, tt.slug, got, tt.want)
			}
		})
	}
}

func TestDiscriminatingFieldTerms(t *testing.T) {
	// Set up store with hubs that have known slugs/names
	s := store.NewInMemoryStore()
	_ = s.CreateHub(&model.Hub{ID: "hub-memax", Name: "memax", Slug: "memax"})
	_ = s.CreateHub(&model.Hub{ID: "hub-test", Name: "memax-test", Slug: "memax-test"})

	h := NewRecallHandler(s, nil, nil, nil, nil)

	tests := []struct {
		name   string
		query  string
		hubIDs []string
		want   []string
	}{
		{
			name:   "filters out hub slug",
			query:  "memax email provider selection",
			hubIDs: []string{"hub-memax"},
			want:   []string{"email", "provider", "selection"},
		},
		{
			name:   "filters out hyphenated hub slug parts",
			query:  "memax test email provider",
			hubIDs: []string{"hub-test"},
			want:   []string{"email", "provider"},
		},
		{
			name:   "filters short Latin terms (< 3 chars)",
			query:  "is email ok to use",
			hubIDs: nil,
			want:   []string{"email", "use"},
		},
		{
			name:   "preserves CJK bigrams, filters unigrams",
			query:  "email 选型 选",
			hubIDs: nil,
			want:   []string{"email", "选型"},
		},
		{
			name:   "empty query returns nil",
			query:  "",
			hubIDs: nil,
			want:   nil,
		},
		{
			name:   "deduplicates terms",
			query:  "email email provider email",
			hubIDs: nil,
			want:   []string{"email", "provider"},
		},
		{
			name:   "strips punctuation",
			query:  "email? provider! selection.",
			hubIDs: nil,
			want:   []string{"email", "provider", "selection"},
		},
		{
			name:   "no hub IDs — no filtering by hub name",
			query:  "memax email provider",
			hubIDs: nil,
			want:   []string{"memax", "email", "provider"},
		},
		{
			name:   "mixed CJK and Latin",
			query:  "email 选型 Resend infrastructure",
			hubIDs: []string{"hub-memax"},
			want:   []string{"email", "选型", "resend", "infrastructure"},
		},
		{
			name:   "CJK stopword-only token filtered",
			query:  "email 什么 选型",
			hubIDs: nil,
			want:   []string{"email", "选型"},
		},
		{
			name:   "CJK with stopword splits into bigrams",
			query:  "email 用什么 选型",
			hubIDs: nil,
			// 什/么 are stopwords → splits into [用] (single, skipped in field lane) and gap
			want: []string{"email", "选型"},
		},
		{
			name:   "CJK adjacent to Latin without space, stopword filtered",
			query:  "memax 的邮件方案",
			hubIDs: nil,
			// 的 is stopword → [邮,件,方,案] → bigrams
			want: []string{"memax", "邮件", "件方", "方案"},
		},
		{
			name:   "mixed CJK+Latin without space splits correctly",
			query:  "memax的邮件方案",
			hubIDs: nil,
			// memax (Latin), 的 (stopword), 邮件方案 (CJK run → bigrams)
			want: []string{"memax", "邮件", "件方", "方案"},
		},
		{
			name:   "pure CJK compound emits bigrams",
			query:  "向量数据库选型",
			hubIDs: nil,
			want:   []string{"向量", "量数", "数据", "据库", "库选", "选型"},
		},
		{
			name:   "CJK negation preserved in bigrams",
			query:  "不做 email",
			hubIDs: nil,
			want:   []string{"不做", "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.discriminatingFieldTerms(tt.query, tt.hubIDs)
			if len(got) != len(tt.want) {
				t.Fatalf("discriminatingFieldTerms(%q) = %v (len %d), want %v (len %d)",
					tt.query, got, len(got), tt.want, len(tt.want))
			}
			for i, term := range got {
				if term != tt.want[i] {
					t.Errorf("term[%d] = %q, want %q", i, term, tt.want[i])
				}
			}
		})
	}
}

func TestTokenizeForMatching(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		// --- Pure English (unchanged behavior) ---
		{
			name:  "english: filters stopwords and short tokens",
			query: "what is the auth system?",
			want:  []string{"auth", "system"},
		},
		{
			name:  "english: preserves multi-word terms",
			query: "email provider selection for memax",
			want:  []string{"email", "provider", "selection", "memax"},
		},
		{
			name:  "english: empty query",
			query: "",
			want:  nil,
		},
		{
			name:  "english: only stopwords",
			query: "what is the",
			want:  nil,
		},
		{
			name:  "english: single char Latin ignored",
			query: "a b c email",
			want:  []string{"email"},
		},

		// --- CJK bigrams ---
		{
			name:  "cjk: 2-char compound emits one bigram",
			query: "选型",
			want:  []string{"选型"},
		},
		{
			name:  "cjk: 4-char run emits overlapping bigrams",
			query: "选型文档",
			want:  []string{"选型", "型文", "文档"},
		},
		{
			name:  "cjk: single non-stopword char preserved",
			query: "选",
			want:  []string{"选"},
		},

		// --- CJK stopword filtering ---
		{
			name:  "cjk: stopwords removed, remaining emitted as bigrams",
			query: "为什么选择？",
			// 什/么 are stopwords → buffer splits into [为] and [选, 择]
			want: []string{"为", "选择"},
		},
		{
			name:  "cjk: pure stopword run produces nothing",
			query: "的了是",
			want:  nil,
		},
		{
			name:  "cjk: single stopword char produces nothing",
			query: "的",
			want:  nil,
		},
		{
			name:  "cjk: stopword between content chars splits runs",
			query: "技术的选型",
			// 的 is a stopword → splits into [技, 术] and [选, 型]
			want: []string{"技术", "选型"},
		},

		// --- Mixed English + CJK ---
		{
			name:  "mixed: email 选型 → bigram",
			query: "email 选型",
			want:  []string{"email", "选型"},
		},
		{
			name:  "mixed: memax email 选型 Resend",
			query: "memax email 选型 Resend",
			want:  []string{"memax", "email", "选型", "resend"},
		},
		{
			name:  "mixed: Chinese with English technical term",
			query: "pgvector 向量检索 技术选型",
			// 向量检索 → ["向量", "量检", "检索"]
			// 技术选型 → ["技术", "术选", "选型"]
			want: []string{"pgvector", "向量", "量检", "检索", "技术", "术选", "选型"},
		},
		{
			name:  "mixed: CJK adjacent to Latin without space, stopword filtered",
			query: "memax的邮件方案",
			// 的 is a stopword → splits CJK into [] and [邮, 件, 方, 案]
			want: []string{"memax", "邮件", "件方", "方案"},
		},
		{
			name:  "mixed: eval query dh-q1",
			query: "memax 用什么 email 选型？",
			// 用 is not a stopword, 什/么 are → [用] then gap
			want: []string{"memax", "用", "email", "选型"},
		},
		{
			name:  "mixed: eval query dh-q7",
			query: "memax 的邮件方案",
			// 的 is a stopword, then [邮, 件, 方, 案] as one run
			want: []string{"memax", "邮件", "件方", "方案"},
		},
		{
			name:  "mixed: decision query with stopwords",
			query: "memax 用什么做缓存",
			// 什/么 are stopwords → [用] gap [做, 缓, 存]
			want: []string{"memax", "用", "做缓", "缓存"},
		},

		// --- Edge cases ---
		{
			name:  "cjk: 3-char run emits 2 overlapping bigrams",
			query: "数据库",
			want:  []string{"数据", "据库"},
		},
		{
			name:  "cjk: all stopwords between content chars",
			query: "邮的件",
			// 的 splits into [邮] and [件]
			want: []string{"邮", "件"},
		},
		{
			name:  "cjk: negation preserved (不 is not a stopword)",
			query: "email marketing 不做",
			want:  []string{"email", "marketing", "不做"},
		},
		{
			name:  "cjk: 没 preserved in negation context",
			query: "没做完",
			want:  []string{"没做", "做完"},
		},
		{
			name:  "whitespace only",
			query: "   ",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenizeForMatching(tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("tokenizeForMatching(%q) = %v (len %d), want %v (len %d)",
					tt.query, got, len(got), tt.want, len(tt.want))
			}
			for i, term := range got {
				if term != tt.want[i] {
					t.Errorf("term[%d] = %q, want %q", i, term, tt.want[i])
				}
			}
		})
	}
}
