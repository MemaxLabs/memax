package locomo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	lme "github.com/MemaxLabs/memax/packages/server/eval/longmemeval"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunker"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Config controls LoCoMo benchmark execution.
type Config struct {
	DatasetPath   string
	CorpusMode    CorpusMode
	RecallLimit   int
	Cutoffs       []int
	Distillation  bool
	EnableQA      bool
	QACutoffs     []int
	QAModel       string
	QAMaxTokens   int
	EnableJudge   bool
	JudgeModel    string
	JudgeEvidence bool
	OutputDir     string
	MaxQuestions  int
	Categories    []int
	OnlySampleIDs []string
	AppendOutput  bool
}

// DefaultConfig returns a config with production-comparable defaults.
func DefaultConfig(datasetPath string) Config {
	return Config{
		DatasetPath:  datasetPath,
		CorpusMode:   DialogMode,
		RecallLimit:  200,
		Cutoffs:      []int{10, 20, 50, 200},
		Distillation: true,
		QACutoffs:    []int{10},
		QAModel:      "claude-haiku-4-5-20251001",
		JudgeModel:   "claude-haiku-4-5-20251001",
		QAMaxTokens:  64,
		OutputDir:    ".",
	}
}

const (
	locomoBenchOwnerID = "00000000-0000-0000-0000-000000000097"
	locomoBenchHubID   = "00000000-0000-0000-0000-0000000000b0"
)

// Runner executes LoCoMo against Memax's retrieval pipeline.
type Runner struct {
	cfg      Config
	pool     *pgxpool.Pool
	store    store.Store
	embedder embed.Embedder
	recallH  *handler.RecallHandler
	llm      *anthropic.Client
}

// NewRunner creates a LoCoMo runner. llm may be nil when QA synthesis is off.
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

// Run executes the benchmark.
func (r *Runner) Run(ctx context.Context) (*BenchmarkResult, error) {
	log.Printf("[locomo] loading dataset from %s", r.cfg.DatasetPath)
	samples, err := LoadDataset(r.cfg.DatasetPath)
	if err != nil {
		return nil, fmt.Errorf("load dataset: %w", err)
	}

	questions := r.filterQuestions(samples)
	if len(r.cfg.Cutoffs) == 0 {
		r.cfg.Cutoffs = []int{10, 20, 50, 200}
	}
	log.Printf("[locomo] evaluating %d questions (mode=%s, limit=%d, cutoffs=%v)", len(questions), r.cfg.CorpusMode, r.cfg.RecallLimit, r.cfg.Cutoffs)

	if err := r.ensureBenchEntities(ctx); err != nil {
		return nil, fmt.Errorf("ensure bench entities: %w", err)
	}

	result := &BenchmarkResult{
		Config:    r.cfg,
		StartTime: time.Now(),
	}
	for i, question := range questions {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		log.Printf("[locomo] [%d/%d] %s q%d category=%d", i+1, len(questions), question.Sample.SampleID, question.Index+1, question.QA.Category)
		qr, err := r.evaluateQuestion(ctx, question)
		if err != nil {
			errText := fmt.Sprintf("%s q%d: %v", question.Sample.SampleID, question.Index+1, err)
			log.Printf("[locomo] ERROR %s", errText)
			result.Errors = append(result.Errors, errText)
			continue
		}
		result.Questions = append(result.Questions, *qr)

		if (i+1)%50 == 0 || i+1 == len(questions) {
			r.logProgressMetrics(result)
		}
	}
	result.EndTime = time.Now()
	return result, nil
}

type questionRef struct {
	Sample Sample
	QA     QuestionAnswer
	Index  int
}

func (r *Runner) evaluateQuestion(ctx context.Context, question questionRef) (*QuestionResult, error) {
	start := time.Now()
	corpus := BuildCorpus(&question.Sample, r.cfg.CorpusMode)
	if len(corpus) == 0 {
		return nil, fmt.Errorf("empty corpus")
	}

	correctDocs := CorrectDocs(corpus, question.QA.Evidence)
	corpusIDToMemoryID, err := r.ingestCorpus(ctx, question, corpus)
	if err != nil {
		return nil, fmt.Errorf("ingest corpus: %w", err)
	}
	defer r.cleanupCorpus(ctx, corpusIDToMemoryID)

	query := question.QA.Question
	if question.QA.Category == 2 {
		query += " Use the dates of the conversations to answer with an approximate date."
	}

	scope := handler.NewRecallScope([]string{locomoBenchHubID}, "")
	searchStart := time.Now()
	results, _, err := r.recallH.RunPipeline(ctx, query, "locomo", "", nil, r.cfg.RecallLimit, locomoBenchOwnerID, nil, scope)
	searchLatencyMs := time.Since(searchStart).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("recall pipeline: %w", err)
	}

	corpusIDs := corpusIDs(corpus)
	corpusIndex := corpusIndexByID(corpus)
	memoryIDToCorpusID := make(map[string]string, len(corpusIDToMemoryID))
	for corpusID, memoryID := range corpusIDToMemoryID {
		memoryIDToCorpusID[memoryID] = corpusID
	}

	var rankings []int
	var rankedDocs []CorpusDoc
	var rankedItems []RankedItem
	for _, mem := range results {
		corpusID, ok := memoryIDToCorpusID[mem.ID]
		if !ok {
			continue
		}
		idx, ok := corpusIndex[corpusID]
		if !ok {
			continue
		}
		rankings = append(rankings, idx)
		doc := corpus[idx]
		rankedDocs = append(rankedDocs, doc)
		rankedItems = append(rankedItems, RankedItem{
			CorpusID:  doc.ID,
			SourceIDs: doc.SourceIDs,
			Text:      doc.Text,
			Timestamp: doc.Timestamp,
		})
	}

	recalled := make(map[int]bool)
	for _, idx := range rankings {
		recalled[idx] = true
	}
	for i := range corpus {
		if !recalled[i] {
			rankings = append(rankings, i)
		}
	}

	metrics := computeRetrievalMetrics(rankings, correctDocs, corpusIDs, rankedDocs, question.QA.Evidence, r.cfg.Cutoffs)

	var hypothesis string
	var qaMetrics *QAMetrics
	qaResults := make(map[string]QAResult)
	if r.cfg.EnableQA && r.llm != nil {
		evidenceContext := ""
		if r.cfg.JudgeEvidence {
			evidenceContext = buildEvidenceContext(corpus, question.QA.Evidence)
		}
		for _, cutoff := range r.qaCutoffs() {
			qaStart := time.Now()
			answer, qaErr := r.generateAnswer(ctx, question.QA, rankedItems, cutoff)
			if qaErr != nil {
				log.Printf("[locomo] QA error for %s q%d @%d: %v", question.Sample.SampleID, question.Index+1, cutoff, qaErr)
				answer = "Error generating answer: " + qaErr.Error()
			}
			m := EvaluateAnswer(answer, question.QA.Answer.String(), question.QA.Category)
			var judge *JudgeResult
			if r.cfg.EnableJudge && qaErr == nil {
				judge, qaErr = r.judgeAnswer(ctx, question.QA, answer, evidenceContext)
				if qaErr != nil {
					log.Printf("[locomo] judge error for %s q%d @%d: %v", question.Sample.SampleID, question.Index+1, cutoff, qaErr)
				} else if judge != nil {
					m.Judge = judge.Score
				}
			}
			result := QAResult{
				Cutoff:     cutoff,
				Hypothesis: answer,
				Metrics:    m,
				Judge:      judge,
				LatencyMs:  time.Since(qaStart).Milliseconds(),
			}
			qaResults[cutoffLabel(cutoff)] = result
			if hypothesis == "" {
				hypothesis = answer
				metricsCopy := m
				qaMetrics = &metricsCopy
			}
		}
	}
	if len(qaResults) == 0 {
		qaResults = nil
	}

	latencyMs := time.Since(start).Milliseconds()
	qr := &QuestionResult{
		SampleID:         question.Sample.SampleID,
		QuestionIndex:    question.Index,
		QuestionID:       fmt.Sprintf("%s:q%d", question.Sample.SampleID, question.Index+1),
		Category:         question.QA.Category,
		CategoryName:     CategoryName(question.QA.Category),
		Question:         question.QA.Question,
		Answer:           question.QA.Answer,
		Evidence:         SplitEvidenceIDs(question.QA.Evidence),
		CorpusSize:       len(corpus),
		CorrectDocs:      len(correctDocs),
		RecalledCount:    len(results),
		RetrievalMetrics: metrics,
		Hypothesis:       hypothesis,
		QAMetrics:        qaMetrics,
		QAResults:        qaResults,
		SearchLatencyMs:  searchLatencyMs,
		LatencyMs:        latencyMs,
		OutputEntry: RetrievalResultEntry{
			SampleID:        question.Sample.SampleID,
			QuestionIndex:   question.Index,
			Question:        question.QA.Question,
			Answer:          question.QA.Answer,
			Category:        question.QA.Category,
			Evidence:        SplitEvidenceIDs(question.QA.Evidence),
			CorpusMode:      r.cfg.CorpusMode,
			RankedItems:     rankedItems,
			Metrics:         metrics,
			Hypothesis:      hypothesis,
			QAMetrics:       qaMetrics,
			QAResults:       qaResults,
			SearchLatencyMs: searchLatencyMs,
			LatencyMs:       latencyMs,
		},
	}

	log.Printf("[locomo]   corpus=%d evidence=%d recalled=%d evidence_recall@10=%.2f ndcg@10=%.2f search=%dms total=%dms",
		len(corpus), len(qr.Evidence), len(results), metrics["evidence_recall@10"], metrics["ndcg_any@10"], searchLatencyMs, latencyMs)

	return qr, nil
}

func (r *Runner) ingestCorpus(ctx context.Context, question questionRef, corpus []CorpusDoc) (map[string]string, error) {
	corpusIDToMemoryID := make(map[string]string, len(corpus))

	type memoryBundle struct {
		memory model.Memory
		chunks []model.Chunk
	}

	bundles := make([]memoryBundle, 0, len(corpus))
	var allEmbedTexts []string
	chunkOffsets := make([]int, 0, len(corpus))

	for _, doc := range corpus {
		memID := corpusMemoryUUID(question.Sample.SampleID, question.Index, doc.ID)
		corpusIDToMemoryID[doc.ID] = memID

		content := fmt.Sprintf("Date: %s\nSource IDs: %s\n\n%s", doc.Timestamp, sourceIDsString(doc.SourceIDs), doc.Text)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		mem := model.Memory{
			ID:              memID,
			HubID:           locomoBenchHubID,
			OwnerID:         locomoBenchOwnerID,
			Title:           truncateTitle(doc.ID, 200),
			Content:         content,
			ContentType:     "markdown",
			ContentHash:     hash,
			Kind:            "episodic",
			Stability:       "stable",
			Boundary:        "private",
			State:           "active",
			Source:          "locomo",
			Tags:            []string{"locomo", string(r.cfg.CorpusMode)},
			CreatedAt:       parseTimestamp(doc.Timestamp),
			UpdatedAt:       time.Now(),
			AccessedAt:      time.Now(),
			Version:         1,
			RetrievalWeight: 1.0,
		}

		chunkResults := chunker.ChunkMarkdown(content)
		chunks := make([]model.Chunk, 0, len(chunkResults))
		for ci, cr := range chunkResults {
			chunks = append(chunks, model.Chunk{
				ID:              corpusChunkUUID(question.Sample.SampleID, question.Index, doc.ID, ci),
				MemoryID:        memID,
				Content:         cr.Content,
				HeadingChain:    cr.HeadingChain,
				ChunkIndex:      cr.Index,
				TokenCount:      cr.TokenCount,
				Kind:            "episodic",
				Stability:       "stable",
				RetrievalWeight: 1.0,
				CreatedAt:       mem.CreatedAt,
			})
			allEmbedTexts = append(allEmbedTexts, cr.HeadingChain+"\n"+cr.Content)
		}
		chunkOffsets = append(chunkOffsets, len(allEmbedTexts)-len(chunks))
		bundles = append(bundles, memoryBundle{memory: mem, chunks: chunks})
	}

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

	for bi, bundle := range bundles {
		_ = r.store.DeleteChunksByMemory(bundle.memory.ID, locomoBenchOwnerID)
		_ = r.store.DeleteMemory(bundle.memory.ID, locomoBenchOwnerID)
		if err := r.store.CreateMemory(&bundle.memory); err != nil {
			return nil, fmt.Errorf("create memory %s: %w", bundle.memory.ID, err)
		}
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

func (r *Runner) cleanupCorpus(ctx context.Context, corpusIDToMemoryID map[string]string) {
	for _, memID := range corpusIDToMemoryID {
		_ = r.store.DeleteChunksByMemory(memID, locomoBenchOwnerID)
		_ = r.store.DeleteMemory(memID, locomoBenchOwnerID)
	}
}

func (r *Runner) generateAnswer(ctx context.Context, qa QuestionAnswer, rankedItems []RankedItem, cutoff int) (string, error) {
	if r.llm == nil {
		return "", fmt.Errorf("anthropic client not configured")
	}

	limit := cutoff
	if limit <= 0 {
		limit = 10
	}
	if len(rankedItems) < limit {
		limit = len(rankedItems)
	}
	var contextParts []string
	for i := 0; i < limit; i++ {
		item := rankedItems[i]
		contextParts = append(contextParts, fmt.Sprintf("Date: %s\n%s", item.Timestamp, item.Text))
	}

	question := qa.Question
	if qa.Category == 2 {
		question += " Use the dates of the conversations to answer with an approximate date."
	}

	prompt := fmt.Sprintf(
		"Based on the conversation context below, write an answer in the form of a short phrase. "+
			"Answer with exact words from the context whenever possible.\n\n"+
			"Context:\n%s\n\nQuestion: %s\nShort answer:",
		strings.Join(contextParts, "\n\n---\n\n"), question)

	resp, err := r.llm.Complete(ctx, anthropic.CompleteRequest{
		Model:     r.qaModel(),
		MaxTokens: r.qaMaxTokens(),
		Prompt:    prompt,
		Purpose:   "locomo.qa_synthesis",
	})
	if err != nil {
		return "", fmt.Errorf("LLM completion: %w", err)
	}
	return strings.TrimSpace(resp.Text), nil
}

func (r *Runner) judgeAnswer(ctx context.Context, qa QuestionAnswer, hypothesis string, evidenceContext string) (*JudgeResult, error) {
	if r.llm == nil {
		return nil, fmt.Errorf("anthropic client not configured")
	}

	prompt := fmt.Sprintf(`You are grading a long-term memory benchmark answer.

Question: %s
Ground truth answer: %s
Predicted answer: %s
Category: %d
`, qa.Question, qa.Answer.String(), hypothesis, qa.Category)
	if strings.TrimSpace(evidenceContext) != "" {
		prompt += "\nGold evidence context:\n" + evidenceContext + "\n"
	}
	prompt += `
Return JSON only:
{"label":"CORRECT"|"WRONG","score":1|0,"reason":"short reason"}

Mark CORRECT only if the predicted answer contains the same essential information as the ground truth. Allow minor wording differences, but do not give credit for unsupported or contradictory answers.`

	resp, err := r.llm.Complete(ctx, anthropic.CompleteRequest{
		Model:     r.judgeModel(),
		MaxTokens: 200,
		Prompt:    prompt,
		Purpose:   "locomo.qa_judge",
	})
	if err != nil {
		return nil, fmt.Errorf("LLM judge: %w", err)
	}
	var parsed struct {
		Label  string  `json:"label"`
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}
	text := anthropic.ExtractJSONObject(resp.Text)
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("parse judge JSON: %w", err)
	}
	label := strings.ToUpper(strings.TrimSpace(parsed.Label))
	score := parsed.Score
	if score == 0 && label == "CORRECT" {
		score = 1
	}
	if score != 0 {
		score = 1
	}
	return &JudgeResult{
		Label:  label,
		Score:  score,
		Reason: parsed.Reason,
		Raw:    resp.Text,
	}, nil
}

func (r *Runner) ensureBenchEntities(ctx context.Context) error {
	now := time.Now()
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, name, display_name, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $5)
		ON CONFLICT (id) DO NOTHING`,
		locomoBenchOwnerID, "locomo@memax.local", "LoCoMo", "LoCoMo Benchmark", now)
	if err != nil {
		return fmt.Errorf("create bench user: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO hubs (id, name, accent, slug, hub_type, owner_id, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7, $7)
		ON CONFLICT (id) DO NOTHING`,
		locomoBenchHubID, "LoCoMo", "#22c55e", "locomo", "personal", locomoBenchOwnerID, now)
	if err != nil {
		return fmt.Errorf("create bench hub: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (hub_id, user_id) DO NOTHING`,
		locomoBenchHubID, locomoBenchOwnerID, "owner")
	if err != nil {
		return fmt.Errorf("create bench hub member: %w", err)
	}
	return nil
}

func (r *Runner) filterQuestions(samples []Sample) []questionRef {
	categoryFilter := make(map[int]bool)
	for _, category := range r.cfg.Categories {
		categoryFilter[category] = true
	}
	sampleFilter := make(map[string]bool)
	for _, id := range r.cfg.OnlySampleIDs {
		sampleFilter[id] = true
	}

	var out []questionRef
	for _, sample := range samples {
		if len(sampleFilter) > 0 && !sampleFilter[sample.SampleID] {
			continue
		}
		for i, qa := range sample.QA {
			if len(categoryFilter) > 0 && !categoryFilter[qa.Category] {
				continue
			}
			out = append(out, questionRef{Sample: sample, QA: qa, Index: i})
			if r.cfg.MaxQuestions > 0 && len(out) >= r.cfg.MaxQuestions {
				return out
			}
		}
	}
	return out
}

func (r *Runner) qaCutoffs() []int {
	if len(r.cfg.QACutoffs) == 0 {
		return []int{10}
	}
	seen := make(map[int]bool, len(r.cfg.QACutoffs))
	out := make([]int, 0, len(r.cfg.QACutoffs))
	for _, cutoff := range r.cfg.QACutoffs {
		if cutoff <= 0 || seen[cutoff] {
			continue
		}
		seen[cutoff] = true
		out = append(out, cutoff)
	}
	if len(out) == 0 {
		return []int{10}
	}
	sort.Ints(out)
	return out
}

func (r *Runner) qaModel() string {
	if strings.TrimSpace(r.cfg.QAModel) != "" {
		return strings.TrimSpace(r.cfg.QAModel)
	}
	return "claude-haiku-4-5-20251001"
}

func (r *Runner) judgeModel() string {
	if strings.TrimSpace(r.cfg.JudgeModel) != "" {
		return strings.TrimSpace(r.cfg.JudgeModel)
	}
	return r.qaModel()
}

func (r *Runner) qaMaxTokens() int {
	if r.cfg.QAMaxTokens > 0 {
		return r.cfg.QAMaxTokens
	}
	return 64
}

func cutoffLabel(cutoff int) string {
	return fmt.Sprintf("%d", cutoff)
}

func buildEvidenceContext(corpus []CorpusDoc, evidence []string) string {
	if len(evidence) == 0 {
		return ""
	}
	evidenceSet := make(map[string]bool, len(evidence))
	for _, id := range SplitEvidenceIDs(evidence) {
		evidenceSet[id] = true
	}
	var parts []string
	for _, doc := range corpus {
		for _, sourceID := range doc.SourceIDs {
			if evidenceSet[sourceID] {
				parts = append(parts, fmt.Sprintf("[%s] Date: %s\n%s", sourceID, doc.Timestamp, doc.Text))
				break
			}
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func computeRetrievalMetrics(rankings []int, correctDocs map[string]bool, corpusIDs []string, rankedDocs []CorpusDoc, evidence []string, cutoffs []int) lme.MetricsAtK {
	metrics := make(lme.MetricsAtK)
	for _, k := range cutoffs {
		m := lme.EvaluateRetrieval(rankings, correctDocs, corpusIDs, k)
		metrics[metricKey("recall_any", k)] = m.RecallAny
		metrics[metricKey("recall_all", k)] = m.RecallAll
		metrics[metricKey("ndcg_any", k)] = m.NDCGAny
		metrics[metricKey("evidence_recall", k)] = EvidenceRecallAtK(rankedDocs, evidence, k)
	}
	return metrics
}

func (r *Runner) logProgressMetrics(result *BenchmarkResult) {
	var metrics []lme.MetricsAtK
	for _, q := range result.Questions {
		if len(q.Evidence) == 0 || q.Category == 5 {
			continue
		}
		metrics = append(metrics, q.RetrievalMetrics)
	}
	agg := lme.AggregateMetricsMap(metrics)
	log.Printf("[locomo] progress: %d/%d questions | evidence_recall@10=%.3f evidence_recall@50=%.3f ndcg@10=%.3f",
		len(result.Questions), len(result.Questions)+len(result.Errors), agg["evidence_recall@10"], agg["evidence_recall@50"], agg["ndcg_any@10"])
}

// BenchmarkResult holds the complete LoCoMo output.
type BenchmarkResult struct {
	Config    Config           `json:"config"`
	StartTime time.Time        `json:"start_time"`
	EndTime   time.Time        `json:"end_time"`
	Questions []QuestionResult `json:"questions"`
	Errors    []string         `json:"errors,omitempty"`
}

// QuestionResult holds one question's benchmark result.
type QuestionResult struct {
	SampleID         string               `json:"sample_id"`
	QuestionIndex    int                  `json:"question_index"`
	QuestionID       string               `json:"question_id"`
	Category         int                  `json:"category"`
	CategoryName     string               `json:"category_name"`
	Question         string               `json:"question"`
	Answer           FlexString           `json:"answer"`
	Evidence         []string             `json:"evidence"`
	CorpusSize       int                  `json:"corpus_size"`
	CorrectDocs      int                  `json:"correct_docs"`
	RecalledCount    int                  `json:"recalled_count"`
	RetrievalMetrics lme.MetricsAtK       `json:"retrieval_metrics"`
	Hypothesis       string               `json:"hypothesis,omitempty"`
	QAMetrics        *QAMetrics           `json:"qa_metrics,omitempty"`
	QAResults        map[string]QAResult  `json:"qa_results,omitempty"`
	SearchLatencyMs  int64                `json:"search_latency_ms"`
	LatencyMs        int64                `json:"latency_ms"`
	OutputEntry      RetrievalResultEntry `json:"-"`
}

// ExportResults writes JSONL outputs and prints summary tables.
func (r *Runner) ExportResults(result *BenchmarkResult) error {
	if r.cfg.OutputDir == "" {
		return nil
	}
	if err := os.MkdirAll(r.cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	var entries []RetrievalResultEntry
	for _, q := range result.Questions {
		entries = append(entries, q.OutputEntry)
	}
	path := r.cfg.OutputDir + "/memax_locomo_results.jsonl"
	if err := WriteResults(path, entries, r.cfg.AppendOutput); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	log.Printf("[locomo] wrote results to %s", path)
	PrintSummary(result)
	return nil
}

// PrintSummary prints comparison-ready retrieval, QA, and latency metrics.
func PrintSummary(result *BenchmarkResult) {
	duration := result.EndTime.Sub(result.StartTime)
	fmt.Printf("\n=== LoCoMo Benchmark Summary ===\n\n")
	fmt.Printf("Dataset: %s\n", result.Config.DatasetPath)
	fmt.Printf("Corpus mode: %s\n", result.Config.CorpusMode)
	fmt.Printf("Distillation: %t\n", result.Config.Distillation)
	if result.Config.EnableQA {
		fmt.Printf("QA cutoffs: %v\n", result.Config.QACutoffs)
		fmt.Printf("QA model: %s\n", result.Config.QAModel)
		if result.Config.EnableJudge {
			fmt.Printf("Judge model: %s (evidence=%t)\n", result.Config.JudgeModel, result.Config.JudgeEvidence)
		}
	}
	fmt.Printf("Questions: %d evaluated, %d errors\n", len(result.Questions), len(result.Errors))
	fmt.Printf("Duration: %s\n", duration.Round(time.Second))

	typeMetrics := make(map[string][]lme.MetricsAtK)
	typeQAByCutoff := make(map[int]map[string][]QAMetrics)
	judgeByCutoff := make(map[int]bool)
	var allMetrics []lme.MetricsAtK
	allQAByCutoff := make(map[int][]QAMetrics)
	var searchLatencies []int64
	var totalLatencies []int64

	for _, q := range result.Questions {
		if q.Category == 5 {
			continue
		}
		if len(q.Evidence) > 0 {
			typeMetrics[q.CategoryName] = append(typeMetrics[q.CategoryName], q.RetrievalMetrics)
			allMetrics = append(allMetrics, q.RetrievalMetrics)
		}
		for _, qaResult := range q.QAResults {
			if typeQAByCutoff[qaResult.Cutoff] == nil {
				typeQAByCutoff[qaResult.Cutoff] = make(map[string][]QAMetrics)
			}
			typeQAByCutoff[qaResult.Cutoff][q.CategoryName] = append(typeQAByCutoff[qaResult.Cutoff][q.CategoryName], qaResult.Metrics)
			allQAByCutoff[qaResult.Cutoff] = append(allQAByCutoff[qaResult.Cutoff], qaResult.Metrics)
			if qaResult.Judge != nil {
				judgeByCutoff[qaResult.Cutoff] = true
			}
		}
		searchLatencies = append(searchLatencies, q.SearchLatencyMs)
		totalLatencies = append(totalLatencies, q.LatencyMs)
	}

	cutoffs := result.Config.Cutoffs
	if len(cutoffs) == 0 {
		cutoffs = []int{10, 20, 50, 200}
	}

	fmt.Printf("\n--- Retrieval Metrics (category 5 excluded, evidence-only) ---\n\n")
	fmt.Printf("%-14s", "Category")
	for _, cutoff := range cutoffs {
		fmt.Printf("  %-20s", metricKey("evidence_recall", cutoff))
	}
	fmt.Printf("  %-5s\n", "N")
	fmt.Println(strings.Repeat("-", 20+22*len(cutoffs)))
	for _, category := range comparisonCategories() {
		ms := typeMetrics[category]
		if len(ms) == 0 {
			continue
		}
		agg := lme.AggregateMetricsMap(ms)
		fmt.Printf("%-14s", category)
		for _, cutoff := range cutoffs {
			fmt.Printf("  %-20.4f", agg[metricKey("evidence_recall", cutoff)])
		}
		fmt.Printf("  %-5d\n", len(ms))
	}
	if len(allMetrics) > 0 {
		agg := lme.AggregateMetricsMap(allMetrics)
		fmt.Println(strings.Repeat("-", 20+22*len(cutoffs)))
		fmt.Printf("%-14s", "OVERALL")
		for _, cutoff := range cutoffs {
			fmt.Printf("  %-20.4f", agg[metricKey("evidence_recall", cutoff)])
		}
		fmt.Printf("  %-5d\n", len(allMetrics))
	}

	if len(allQAByCutoff) > 0 {
		printQAComparisonReady(typeQAByCutoff, allQAByCutoff, judgeByCutoff)
	}
	printLatencySummary(searchLatencies, totalLatencies)
}

func printQAComparisonReady(typeQAByCutoff map[int]map[string][]QAMetrics, allQAByCutoff map[int][]QAMetrics, judgeByCutoff map[int]bool) {
	fmt.Printf("\n--- QA Metrics (LoCoMo/Mem0-paper compatible scale) ---\n\n")
	var cutoffs []int
	for cutoff := range allQAByCutoff {
		cutoffs = append(cutoffs, cutoff)
	}
	sort.Ints(cutoffs)
	for _, cutoff := range cutoffs {
		typeQA := typeQAByCutoff[cutoff]
		allQA := allQAByCutoff[cutoff]
		fmt.Printf("Cutoff @%d\n", cutoff)
		if judgeByCutoff[cutoff] {
			fmt.Printf("%-14s  %-8s  %-8s  %-8s  %-5s\n", "Category", "F1", "BLEU1", "Judge", "N")
			fmt.Println(strings.Repeat("-", 55))
		} else {
			fmt.Printf("%-14s  %-8s  %-8s  %-5s\n", "Category", "F1", "BLEU1", "N")
			fmt.Println(strings.Repeat("-", 44))
		}
		for _, category := range comparisonCategories() {
			ms := typeQA[category]
			if len(ms) == 0 {
				continue
			}
			agg := aggregateQA(ms)
			if judgeByCutoff[cutoff] {
				fmt.Printf("%-14s  %-8.2f  %-8.2f  %-8.2f  %-5d\n", category, agg.F1*100, agg.BLEU1*100, agg.Judge*100, len(ms))
			} else {
				fmt.Printf("%-14s  %-8.2f  %-8.2f  %-5d\n", category, agg.F1*100, agg.BLEU1*100, len(ms))
			}
		}
		agg := aggregateQA(allQA)
		if judgeByCutoff[cutoff] {
			fmt.Println(strings.Repeat("-", 55))
			fmt.Printf("%-14s  %-8.2f  %-8.2f  %-8.2f  %-5d\n\n", "OVERALL", agg.F1*100, agg.BLEU1*100, agg.Judge*100, len(allQA))
		} else {
			fmt.Println(strings.Repeat("-", 44))
			fmt.Printf("%-14s  %-8.2f  %-8.2f  %-5d\n\n", "OVERALL", agg.F1*100, agg.BLEU1*100, len(allQA))
		}
	}
	fmt.Println("For Mem0-paper comparability, run category 1-4 only, use the same answerer and judge model, the same QA cutoffs, and the same judge evidence setting.")
}

func printLatencySummary(searchLatencies, totalLatencies []int64) {
	if len(totalLatencies) == 0 {
		return
	}
	fmt.Printf("\n--- Latency Metrics (seconds) ---\n\n")
	fmt.Printf("%-14s  %-10s  %-10s  %-10s  %-10s\n", "Method", "search_p50", "search_p95", "total_p50", "total_p95")
	fmt.Println(strings.Repeat("-", 64))
	fmt.Printf("%-14s  %-10.3f  %-10.3f  %-10.3f  %-10.3f\n",
		"Memax", percentileSeconds(searchLatencies, 50), percentileSeconds(searchLatencies, 95), percentileSeconds(totalLatencies, 50), percentileSeconds(totalLatencies, 95))
}

func aggregateQA(metrics []QAMetrics) QAMetrics {
	var out QAMetrics
	if len(metrics) == 0 {
		return out
	}
	for _, m := range metrics {
		out.F1 += m.F1
		out.BLEU1 += m.BLEU1
		out.Judge += m.Judge
	}
	out.F1 /= float64(len(metrics))
	out.BLEU1 /= float64(len(metrics))
	out.Judge /= float64(len(metrics))
	return out
}

func percentileSeconds(vals []int64, pct int) float64 {
	if len(vals) == 0 {
		return 0
	}
	sortedVals := append([]int64(nil), vals...)
	sort.Slice(sortedVals, func(i, j int) bool { return sortedVals[i] < sortedVals[j] })
	idx := (pct*len(sortedVals) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sortedVals) {
		idx = len(sortedVals)
	}
	return float64(sortedVals[idx-1]) / 1000
}

func comparisonCategories() []string {
	return []string{"single-hop", "multi-hop", "open-domain", "temporal"}
}

func corpusMemoryUUID(sampleID string, questionIdx int, corpusID string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("locomo:mem:%s:%d:%s", sampleID, questionIdx, corpusID)))
	h[6] = (h[6] & 0x0f) | 0x40
	h[8] = (h[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func corpusChunkUUID(sampleID string, questionIdx int, corpusID string, chunkIdx int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("locomo:chunk:%s:%d:%s:%d", sampleID, questionIdx, corpusID, chunkIdx)))
	h[6] = (h[6] & 0x0f) | 0x40
	h[8] = (h[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func parseTimestamp(ts string) time.Time {
	for _, layout := range []string{
		"3:04 pm on 2 January, 2006",
		"3:04 PM on 2 January, 2006",
		"3:04 pm on 2 Jan, 2006",
		"3:04 PM on 2 Jan, 2006",
		"January 2, 2006, 03:04 PM",
		"January 2, 2006 03:04 PM",
		"2 January 2006, 03:04 PM",
		"2006/01/02 (Mon) 15:04",
		"2006/01/02 15:04",
	} {
		t, err := time.Parse(layout, ts)
		if err == nil {
			return t
		}
	}
	return time.Now()
}

func metricKey(name string, k int) string {
	return fmt.Sprintf("%s@%d", name, k)
}

func truncateTitle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
