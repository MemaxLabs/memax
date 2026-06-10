package longmemeval

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunker"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// ---------------------------------------------------------------------------
// Benchmark configuration
// ---------------------------------------------------------------------------

// Config controls the benchmark execution.
type Config struct {
	// DatasetPath is the path to the LongMemEval JSON file.
	DatasetPath string

	// Granularity controls corpus document granularity.
	Granularity Granularity

	// CorpusMode controls which turns are indexed.
	// UserOnly = LongMemEval standard (user turns only).
	// AllTurns = include assistant turns (matches other top systems).
	CorpusMode CorpusMode

	// RecallLimit is the max number of results from the recall pipeline.
	// LongMemEval evaluates at k up to 50, so we need at least 50.
	RecallLimit int

	// EnableQA runs the /ask pipeline for QA hypothesis generation.
	EnableQA bool

	// OutputDir for writing JSONL result files.
	OutputDir string

	// MaxQuestions limits evaluation to the first N questions (0 = all).
	// Useful for development/testing.
	MaxQuestions int

	// QuestionTypes filters to specific question types (empty = all).
	QuestionTypes []string

	// SkipAbstention excludes abstention questions from the run.
	SkipAbstention bool

	// OnlyQuestionIDs filters to specific question IDs (empty = all).
	// Useful for retrying failed questions without re-running everything.
	OnlyQuestionIDs []string

	// AppendOutput appends to existing result files instead of overwriting.
	AppendOutput bool
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig(datasetPath string) Config {
	return Config{
		DatasetPath: datasetPath,
		Granularity: SessionGranularity,
		RecallLimit: 50,
		OutputDir:   ".",
	}
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

const (
	benchOwnerID = "00000000-0000-0000-0000-000000000098"
	benchHubID   = "00000000-0000-0000-0000-0000000000a0"
)

// Runner executes the LongMemEval benchmark.
type Runner struct {
	cfg      Config
	pool     *pgxpool.Pool
	store    store.Store
	embedder embed.Embedder
	recallH  *handler.RecallHandler
	llm      *anthropic.Client // for QA synthesis (may be nil)
}

// NewRunner creates a benchmark runner.
// llm may be nil if QA evaluation is not needed.
func NewRunner(cfg Config, pool *pgxpool.Pool, s store.Store, embedder embed.Embedder, recallH *handler.RecallHandler, llm *anthropic.Client) *Runner {
	return &Runner{
		cfg:      cfg,
		pool:     pool,
		store:    s,
		embedder: embedder,
		recallH:  recallH,
		llm:      llm,
	}
}

// Run executes the full benchmark and returns results.
func (r *Runner) Run(ctx context.Context) (*BenchmarkResult, error) {
	log.Printf("[longmemeval] loading dataset from %s", r.cfg.DatasetPath)
	entries, err := LoadDataset(r.cfg.DatasetPath)
	if err != nil {
		return nil, fmt.Errorf("load dataset: %w", err)
	}

	// Filter entries.
	entries = r.filterEntries(entries)
	log.Printf("[longmemeval] evaluating %d questions (granularity=%s, limit=%d)",
		len(entries), r.cfg.Granularity, r.cfg.RecallLimit)

	// Ensure benchmark entities exist (user + hub).
	if err := r.ensureBenchEntities(ctx); err != nil {
		return nil, fmt.Errorf("ensure bench entities: %w", err)
	}

	result := &BenchmarkResult{
		Config:    r.cfg,
		StartTime: time.Now(),
	}

	for i, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		log.Printf("[longmemeval] [%d/%d] %s (%s)", i+1, len(entries), entry.QuestionID, entry.QuestionType)

		qr, err := r.evaluateQuestion(ctx, &entry)
		if err != nil {
			log.Printf("[longmemeval] ERROR %s: %v", entry.QuestionID, err)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.QuestionID, err))
			continue
		}

		result.Questions = append(result.Questions, *qr)

		// Log progress metrics periodically.
		if (i+1)%50 == 0 || i+1 == len(entries) {
			r.logProgressMetrics(result)
		}
	}

	result.EndTime = time.Now()
	return result, nil
}

// evaluateQuestion runs the full evaluation for a single question:
// 1. Build corpus from haystack sessions
// 2. Ingest corpus as memories (chunk + embed + insert)
// 3. Run recall pipeline
// 4. Map results back to corpus IDs
// 5. Compute metrics
// 6. Clean up
func (r *Runner) evaluateQuestion(ctx context.Context, entry *Entry) (*QuestionResult, error) {
	start := time.Now()

	// Step 1: Build corpus.
	mode := r.cfg.CorpusMode
	if mode == "" {
		mode = UserOnly
	}
	corpus := BuildCorpusWithMode(entry, r.cfg.Granularity, mode)
	correctDocs := CorrectDocs(corpus)

	// Step 2: Ingest corpus as memories.
	corpusIDToMemoryID, err := r.ingestCorpus(ctx, entry, corpus)
	if err != nil {
		return nil, fmt.Errorf("ingest corpus: %w", err)
	}

	// Step 3: Run recall.
	scope := handler.NewRecallScope([]string{benchHubID}, "")
	results, _, err := r.recallH.RunPipeline(ctx, entry.Question, "longmemeval", "", nil, r.cfg.RecallLimit, benchOwnerID, nil, scope)
	if err != nil {
		r.cleanupCorpus(ctx, corpusIDToMemoryID)
		return nil, fmt.Errorf("recall pipeline: %w", err)
	}

	// Step 4: Map recalled memories back to corpus IDs and build rankings.
	memoryIDToCorpusIdx := make(map[string]int)
	corpusIDs := make([]string, len(corpus))
	for i, doc := range corpus {
		corpusIDs[i] = doc.ID
		memoryIDToCorpusIdx[corpusIDToMemoryID[doc.ID]] = i
	}

	// Build ranking: for each recalled memory, find its corpus index.
	// Memories not in corpus (shouldn't happen but be safe) are skipped.
	var rankings []int
	rankedItems := make([]RankedItem, 0, len(results))
	for _, mem := range results {
		if idx, ok := memoryIDToCorpusIdx[mem.ID]; ok {
			rankings = append(rankings, idx)
			rankedItems = append(rankedItems, RankedItem{
				CorpusID:  corpusIDs[idx],
				Text:      corpus[idx].Text,
				Timestamp: corpus[idx].Timestamp,
			})
		}
	}

	// Append un-recalled documents to complete the ranking (needed for NDCG).
	recalled := make(map[int]bool)
	for _, idx := range rankings {
		recalled[idx] = true
	}
	for i := range corpus {
		if !recalled[i] {
			rankings = append(rankings, i)
		}
	}

	// Step 5: Compute metrics.
	nativeMetrics, sessionMetrics := ComputeAllMetrics(rankings, correctDocs, corpusIDs, r.cfg.Granularity)

	// Step 6: Build QA hypothesis if enabled.
	var hypothesis string
	if r.cfg.EnableQA && r.llm != nil {
		hypothesis, err = r.generateAnswer(ctx, entry, results)
		if err != nil {
			log.Printf("[longmemeval] QA error for %s: %v", entry.QuestionID, err)
			hypothesis = "Error generating answer: " + err.Error()
		}
	}

	// Step 7: Clean up.
	r.cleanupCorpus(ctx, corpusIDToMemoryID)

	latencyMs := time.Since(start).Milliseconds()

	qr := &QuestionResult{
		QuestionID:     entry.QuestionID,
		QuestionType:   entry.QuestionType,
		Question:       entry.Question,
		Answer:         entry.Answer,
		CorpusSize:     len(corpus),
		CorrectDocs:    len(correctDocs),
		RecalledCount:  len(results),
		NativeMetrics:  nativeMetrics,
		SessionMetrics: sessionMetrics,
		Hypothesis:     hypothesis,
		LatencyMs:      latencyMs,
		IsAbstention:   entry.IsAbstention(),
	}

	// Build retrieval result entry for JSONL output.
	metrics := map[string]MetricsAtK{}
	if r.cfg.Granularity == SessionGranularity {
		metrics["session"] = nativeMetrics
	} else {
		metrics["turn"] = nativeMetrics
		if len(sessionMetrics) > 0 {
			metrics["session"] = sessionMetrics
		}
	}
	qr.OutputEntry = RetrievalResultEntry{
		QuestionID:         entry.QuestionID,
		QuestionType:       entry.QuestionType,
		Question:           entry.Question,
		Answer:             entry.Answer,
		QuestionDate:       entry.QuestionDate,
		HaystackDates:      entry.HaystackDates,
		HaystackSessions:   entry.HaystackSessions,
		HaystackSessionIDs: entry.HaystackSessionIDs,
		AnswerSessionIDs:   entry.AnswerSessionIDs,
		RetrievalResults: &RetrievalResults{
			Query:       entry.Question,
			RankedItems: rankedItems,
			Metrics:     metrics,
		},
	}

	// Log per-question result.
	log.Printf("[longmemeval]   corpus=%d correct=%d recalled=%d R_any@5=%.2f R_all@5=%.2f ndcg@5=%.2f %dms",
		len(corpus), len(correctDocs), len(results),
		nativeMetrics["recall_any@5"], nativeMetrics["recall_all@5"], nativeMetrics["ndcg_any@5"], latencyMs)

	return qr, nil
}

// ingestCorpus inserts corpus documents as memories with chunks and embeddings.
// Returns a mapping from corpus_id -> memory_id for result mapping.
func (r *Runner) ingestCorpus(ctx context.Context, entry *Entry, corpus []CorpusDoc) (map[string]string, error) {
	corpusIDToMemoryID := make(map[string]string)

	// Prepare all chunks and embeddings in batch.
	type memoryBundle struct {
		memory model.Memory
		chunks []model.Chunk
		texts  []string // for batch embedding
	}

	bundles := make([]memoryBundle, 0, len(corpus))
	var allEmbedTexts []string
	chunkOffsets := make([]int, 0, len(corpus)) // track offset into allEmbedTexts per bundle

	for _, doc := range corpus {
		memID := corpusMemoryUUID(entry.QuestionID, doc.ID)
		corpusIDToMemoryID[doc.ID] = memID

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(doc.Text)))

		mem := model.Memory{
			ID:              memID,
			HubID:           benchHubID,
			OwnerID:         benchOwnerID,
			Title:           truncateTitle(doc.ID, 200),
			Content:         doc.Text,
			ContentType:     "markdown",
			ContentHash:     hash,
			Kind:            "episodic",
			Stability:       "stable",
			Boundary:        "private",
			State:           "active",
			Source:          "longmemeval",
			Tags:            []string{}, // NOT NULL in DB
			CreatedAt:       parseTimestamp(doc.Timestamp),
			UpdatedAt:       time.Now(),
			AccessedAt:      time.Now(),
			Version:         1,
			RetrievalWeight: 1.0,
		}

		chunkResults := chunker.ChunkMarkdown(doc.Text)
		chunks := make([]model.Chunk, 0, len(chunkResults))
		var texts []string
		for ci, cr := range chunkResults {
			c := model.Chunk{
				ID:              corpusChunkUUID(entry.QuestionID, doc.ID, ci),
				MemoryID:        memID,
				Content:         cr.Content,
				HeadingChain:    cr.HeadingChain,
				ChunkIndex:      cr.Index,
				TokenCount:      cr.TokenCount,
				Kind:            "episodic",
				Stability:       "stable",
				RetrievalWeight: 1.0,
				CreatedAt:       mem.CreatedAt,
			}
			chunks = append(chunks, c)
			text := cr.HeadingChain + "\n" + cr.Content
			texts = append(texts, text)
		}

		chunkOffsets = append(chunkOffsets, len(allEmbedTexts))
		allEmbedTexts = append(allEmbedTexts, texts...)

		bundles = append(bundles, memoryBundle{
			memory: mem,
			chunks: chunks,
			texts:  texts,
		})
	}

	// Batch embed all chunks at once for efficiency.
	var allEmbeddings [][]float64
	if len(allEmbedTexts) > 0 {
		var err error
		allEmbeddings, err = r.embedder.Embed(allEmbedTexts, "document")
		if err != nil {
			return nil, fmt.Errorf("batch embed: %w", err)
		}
		if len(allEmbeddings) != len(allEmbedTexts) {
			return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(allEmbeddings), len(allEmbedTexts))
		}
	}

	// Insert memories and chunks. Delete any stale data first — previous
	// runs that were interrupted (ctrl+C) may have left orphaned rows.
	for bi, bundle := range bundles {
		_ = r.store.DeleteChunksByMemory(bundle.memory.ID, benchOwnerID)
		_ = r.store.DeleteMemory(bundle.memory.ID, benchOwnerID)

		if err := r.store.CreateMemory(&bundle.memory); err != nil {
			return nil, fmt.Errorf("create memory %s: %w", bundle.memory.ID, err)
		}

		// Assign embeddings to chunks.
		offset := chunkOffsets[bi]
		for ci := range bundle.chunks {
			if offset+ci < len(allEmbeddings) {
				bundle.chunks[ci].Embedding = allEmbeddings[offset+ci]
			}
		}

		if err := r.store.CreateChunks(bundle.chunks); err != nil {
			return nil, fmt.Errorf("create chunks for %s: %w", bundle.memory.ID, err)
		}
	}

	return corpusIDToMemoryID, nil
}

// cleanupCorpus removes all memories and chunks created for a question.
func (r *Runner) cleanupCorpus(ctx context.Context, corpusIDToMemoryID map[string]string) {
	for _, memID := range corpusIDToMemoryID {
		_ = r.store.DeleteChunksByMemory(memID, benchOwnerID)
		_ = r.store.DeleteMemory(memID, benchOwnerID)
	}
}

// generateAnswer uses the Anthropic client to synthesize a QA answer from
// recalled memories. Uses the same RAG prompt template as LongMemEval's
// run_generation.py for a fair comparison.
func (r *Runner) generateAnswer(ctx context.Context, entry *Entry, recallResults []model.RecalledMemory) (string, error) {
	if r.llm == nil {
		return "", fmt.Errorf("anthropic client not configured")
	}

	// Build history context from top-10 recalled memories (matching LongMemEval's
	// default context window of top-k retrieved sessions).
	var historyParts []string
	limit := 10
	if len(recallResults) < limit {
		limit = len(recallResults)
	}
	for i := 0; i < limit; i++ {
		mem := recallResults[i]
		historyParts = append(historyParts,
			fmt.Sprintf("[Session from %s]\n%s", mem.CreatedAt, mem.ChunkContent))
	}
	history := strings.Join(historyParts, "\n\n---\n\n")

	// Use LongMemEval's RAG prompt template (with CoT).
	prompt := fmt.Sprintf(
		"I will give you several history chats between you and a user. "+
			"Please answer the question based on the relevant chat history. "+
			"Answer the question step by step: first extract all the relevant information, "+
			"and then reason over the information to get the answer.\n\n\n"+
			"History Chats:\n\n%s\n\n"+
			"Current Date: %s\n"+
			"Question: %s\n"+
			"Answer (step by step):",
		history, entry.QuestionDate, entry.Question)

	resp, err := r.llm.Complete(ctx, anthropic.CompleteRequest{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 512,
		Prompt:    prompt,
		Purpose:   "longmemeval.qa_synthesis",
	})
	if err != nil {
		return "", fmt.Errorf("LLM completion: %w", err)
	}

	return resp.Text, nil
}

// ensureBenchEntities creates the benchmark user and hub if they don't exist.
func (r *Runner) ensureBenchEntities(ctx context.Context) error {
	now := time.Now()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, name, display_name, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $5)
		ON CONFLICT (id) DO NOTHING`,
		benchOwnerID, "longmemeval@memax.local", "LongMemEval", "LongMemEval Benchmark", now)
	if err != nil {
		return fmt.Errorf("create bench user: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO hubs (id, name, accent, slug, hub_type, owner_id, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7, $7)
		ON CONFLICT (id) DO NOTHING`,
		benchHubID, "LongMemEval", "#818cf8", "longmemeval", "personal", benchOwnerID, now)
	if err != nil {
		return fmt.Errorf("create bench hub: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (hub_id, user_id) DO NOTHING`,
		benchHubID, benchOwnerID, "owner")
	if err != nil {
		return fmt.Errorf("create bench hub member: %w", err)
	}

	return nil
}

// filterEntries applies MaxQuestions, QuestionTypes, and SkipAbstention filters.
func (r *Runner) filterEntries(entries []Entry) []Entry {
	var filtered []Entry

	typeFilter := make(map[string]bool)
	for _, qt := range r.cfg.QuestionTypes {
		typeFilter[qt] = true
	}

	idFilter := make(map[string]bool)
	for _, id := range r.cfg.OnlyQuestionIDs {
		idFilter[id] = true
	}

	for _, e := range entries {
		if len(idFilter) > 0 && !idFilter[e.QuestionID] {
			continue
		}
		if r.cfg.SkipAbstention && e.IsAbstention() {
			continue
		}
		if len(typeFilter) > 0 && !typeFilter[e.QuestionType] {
			continue
		}
		filtered = append(filtered, e)
		if r.cfg.MaxQuestions > 0 && len(filtered) >= r.cfg.MaxQuestions {
			break
		}
	}

	return filtered
}

func (r *Runner) logProgressMetrics(result *BenchmarkResult) {
	var nativeMetrics []MetricsAtK
	for _, q := range result.Questions {
		if !q.IsAbstention {
			nativeMetrics = append(nativeMetrics, q.NativeMetrics)
		}
	}
	agg := AggregateMetricsMap(nativeMetrics)

	log.Printf("[longmemeval] progress: %d/%d questions | R_any@5=%.3f R_all@5=%.3f ndcg@5=%.3f R_any@10=%.3f R_all@10=%.3f ndcg@10=%.3f",
		len(result.Questions), len(result.Questions)+len(result.Errors),
		agg["recall_any@5"], agg["recall_all@5"], agg["ndcg_any@5"],
		agg["recall_any@10"], agg["recall_all@10"], agg["ndcg_any@10"])
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// BenchmarkResult holds the complete benchmark output.
type BenchmarkResult struct {
	Config    Config           `json:"config"`
	StartTime time.Time        `json:"start_time"`
	EndTime   time.Time        `json:"end_time"`
	Questions []QuestionResult `json:"questions"`
	Errors    []string         `json:"errors,omitempty"`
}

// QuestionResult holds the evaluation result for a single question.
type QuestionResult struct {
	QuestionID     string               `json:"question_id"`
	QuestionType   string               `json:"question_type"`
	Question       string               `json:"question"`
	Answer         FlexString           `json:"answer"`
	CorpusSize     int                  `json:"corpus_size"`
	CorrectDocs    int                  `json:"correct_docs"`
	RecalledCount  int                  `json:"recalled_count"`
	NativeMetrics  MetricsAtK           `json:"native_metrics"`
	SessionMetrics MetricsAtK           `json:"session_metrics,omitempty"`
	Hypothesis     string               `json:"hypothesis,omitempty"`
	LatencyMs      int64                `json:"latency_ms"`
	IsAbstention   bool                 `json:"is_abstention"`
	OutputEntry    RetrievalResultEntry `json:"-"` // for JSONL output, not included in JSON report
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// corpusMemoryUUID generates a deterministic UUID from question ID + corpus ID.
func corpusMemoryUUID(questionID, corpusID string) string {
	h := sha256.Sum256([]byte("longmemeval:mem:" + questionID + ":" + corpusID))
	h[6] = (h[6] & 0x0f) | 0x40
	h[8] = (h[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func corpusChunkUUID(questionID, corpusID string, chunkIdx int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("longmemeval:chunk:%s:%s:%d", questionID, corpusID, chunkIdx)))
	h[6] = (h[6] & 0x0f) | 0x40
	h[8] = (h[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// parseTimestamp parses a LongMemEval timestamp: "2023/04/10 (Mon) 23:07"
func parseTimestamp(ts string) time.Time {
	// Try full format first: "2023/04/10 (Mon) 23:07"
	t, err := time.Parse("2006/01/02 (Mon) 15:04", ts)
	if err == nil {
		return t
	}
	// Try without day name: "2023/04/10 23:07"
	t, err = time.Parse("2006/01/02 15:04", ts)
	if err == nil {
		return t
	}
	// Fallback to now.
	return time.Now()
}

func truncateTitle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// ExportResults writes the benchmark results to files in the output directory.
func (r *Runner) ExportResults(result *BenchmarkResult) error {
	if r.cfg.OutputDir == "" {
		return nil
	}

	if err := os.MkdirAll(r.cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Write retrieval log (JSONL, compatible with LongMemEval's print_retrieval_metrics.py).
	var retrievalEntries []RetrievalResultEntry
	for _, q := range result.Questions {
		retrievalEntries = append(retrievalEntries, q.OutputEntry)
	}
	retrievalPath := r.cfg.OutputDir + "/memax_retrievallog_" + string(r.cfg.Granularity) + ".jsonl"
	if err := WriteRetrievalLog(retrievalPath, retrievalEntries, r.cfg.AppendOutput); err != nil {
		return fmt.Errorf("write retrieval log: %w", err)
	}
	log.Printf("[longmemeval] wrote retrieval log to %s", retrievalPath)

	// Write QA hypotheses (JSONL, compatible with LongMemEval's evaluate_qa.py).
	if r.cfg.EnableQA {
		var hypotheses []QAHypothesis
		for _, q := range result.Questions {
			if q.Hypothesis != "" {
				hypotheses = append(hypotheses, QAHypothesis{
					QuestionID: q.QuestionID,
					Hypothesis: q.Hypothesis,
				})
			}
		}
		qaPath := r.cfg.OutputDir + "/memax_qa_hypotheses.jsonl"
		if err := WriteQAHypotheses(qaPath, hypotheses, r.cfg.AppendOutput); err != nil {
			return fmt.Errorf("write QA hypotheses: %w", err)
		}
		log.Printf("[longmemeval] wrote QA hypotheses to %s", qaPath)
	}

	// Print summary report.
	PrintRetrievalReport(retrievalEntries)

	return nil
}

// PrintSummary prints a final summary of the benchmark results.
func PrintSummary(result *BenchmarkResult) {
	duration := result.EndTime.Sub(result.StartTime)

	fmt.Printf("\n=== LongMemEval Benchmark Summary ===\n\n")
	fmt.Printf("Dataset: %s\n", result.Config.DatasetPath)
	fmt.Printf("Granularity: %s\n", result.Config.Granularity)
	fmt.Printf("Questions: %d evaluated, %d errors\n", len(result.Questions), len(result.Errors))
	fmt.Printf("Duration: %s\n", duration.Round(time.Second))

	if len(result.Questions) == 0 {
		return
	}

	// Aggregate by question type.
	typeMetrics := make(map[string][]MetricsAtK)
	var allMetrics []MetricsAtK

	for _, q := range result.Questions {
		if q.IsAbstention {
			continue
		}
		if !q.hasAnswerTurns() {
			continue
		}
		typeMetrics[q.QuestionType] = append(typeMetrics[q.QuestionType], q.NativeMetrics)
		allMetrics = append(allMetrics, q.NativeMetrics)
	}

	fmt.Printf("\n--- Retrieval Metrics (abstention excluded, %d questions) ---\n\n", len(allMetrics))

	// Per-type breakdown.
	keyMetrics := []string{"recall_any@5", "recall_all@5", "ndcg_any@5", "recall_any@10", "recall_all@10", "ndcg_any@10"}
	fmt.Printf("%-30s", "Type")
	for _, k := range keyMetrics {
		fmt.Printf("  %-14s", k)
	}
	fmt.Printf("  %-5s\n", "N")
	fmt.Println(strings.Repeat("-", 124))

	for _, qt := range AllQuestionTypes {
		ms, ok := typeMetrics[qt]
		if !ok {
			continue
		}
		agg := AggregateMetricsMap(ms)
		fmt.Printf("%-30s", qt)
		for _, k := range keyMetrics {
			fmt.Printf("  %-14.4f", agg[k])
		}
		fmt.Printf("  %-5d\n", len(ms))
	}

	// Overall.
	fmt.Println(strings.Repeat("-", 124))
	agg := AggregateMetricsMap(allMetrics)
	fmt.Printf("%-30s", "OVERALL")
	for _, k := range keyMetrics {
		fmt.Printf("  %-14.4f", agg[k])
	}
	fmt.Printf("  %-5d\n", len(allMetrics))

	// Avg latency.
	var totalLatency int64
	for _, q := range result.Questions {
		totalLatency += q.LatencyMs
	}
	avgLatency := totalLatency / int64(len(result.Questions))
	fmt.Printf("\nAvg latency: %dms\n", avgLatency)
}

// hasAnswerTurns is a QuestionResult-level check (mirrors Entry.HasAnswerTurns
// but we don't store the full sessions in the result). We track this via
// the correct doc count instead — if no correct docs, the question has
// no answer turns.
func (q *QuestionResult) hasAnswerTurns() bool {
	return q.CorrectDocs > 0
}
