// Command locomo runs the LoCoMo benchmark against Memax's retrieval pipeline.
//
// Required environment variables:
//
//	DATABASE_URL   - PostgreSQL connection string
//	VOYAGE_API_KEY - Voyage AI API key for embeddings
//
// Optional:
//
//	ANTHROPIC_API_KEY - enables query distillation and --qa answer generation
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/eval/locomo"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	dbmigrate "github.com/MemaxLabs/memax/packages/server/internal/migrate"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/distill"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func main() {
	dataset := flag.String("dataset", "", "Path to LoCoMo locomo10.json dataset (required)")
	corpusMode := flag.String("corpus-mode", "dialog", "Corpus mode: dialog, observation, or summary")
	limit := flag.Int("limit", 200, "Max recall results per query")
	cutoffs := flag.String("cutoffs", "10,20,50,200", "Comma-separated cutoffs for evidence recall metrics")
	maxQuestions := flag.Int("max-questions", 0, "Limit to first N questions (0 = all)")
	categories := flag.String("categories", "1,2,3,4", "Comma-separated LoCoMo categories (default excludes adversarial category 5)")
	sampleIDs := flag.String("samples", "", "Comma-separated sample IDs to evaluate")
	appendOutput := flag.Bool("append", false, "Append to existing result file instead of overwriting")
	enableDistillation := flag.Bool("distill", true, "Enable Anthropic query distillation when ANTHROPIC_API_KEY is configured")
	enableQA := flag.Bool("qa", false, "Enable QA hypothesis generation (requires ANTHROPIC_API_KEY)")
	qaCutoffs := flag.String("qa-cutoffs", "10", "Comma-separated retrieval cutoffs used for QA generation when -qa is enabled")
	qaModel := flag.String("qa-model", "claude-haiku-4-5-20251001", "Anthropic model for QA generation")
	qaMaxTokens := flag.Int("qa-max-tokens", 64, "Max output tokens for QA generation")
	enableJudge := flag.Bool("judge", false, "Enable LLM-as-judge scoring for generated answers (requires -qa and ANTHROPIC_API_KEY)")
	judgeModel := flag.String("judge-model", "claude-haiku-4-5-20251001", "Anthropic model for LLM-as-judge scoring")
	judgeEvidence := flag.Bool("judge-evidence", false, "Include gold evidence context in LLM-as-judge prompts")
	outputDir := flag.String("output", ".", "Output directory for result files")
	flag.Parse()

	if *dataset == "" {
		fmt.Fprintln(os.Stderr, "Usage: locomo -dataset <path-to-locomo10.json>")
		fmt.Fprintln(os.Stderr, "\nRequired: DATABASE_URL, VOYAGE_API_KEY")
		fmt.Fprintln(os.Stderr, "Optional: ANTHROPIC_API_KEY (for distillation and --qa)")
		flag.PrintDefaults()
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	if err := dbmigrate.Run(dbURL, "migrations"); err != nil {
		log.Printf("WARNING: migrations: %v (continuing; DB may already be migrated)", err)
	}

	embedder := embed.NewEmbedder()
	if embedder == nil {
		log.Fatal("VOYAGE_API_KEY is required for embeddings")
	}

	llm := anthropic.NewFromEnv()
	if llm != nil {
		if *enableDistillation {
			log.Println("Anthropic LLM enabled; distillation active")
		} else {
			log.Println("Anthropic LLM enabled; distillation disabled by -distill=false")
		}
	} else {
		log.Println("No ANTHROPIC_API_KEY; distillation disabled, raw query path")
		if *enableQA {
			log.Fatal("--qa requires ANTHROPIC_API_KEY")
		}
		if *enableJudge {
			log.Fatal("--judge requires ANTHROPIC_API_KEY")
		}
	}
	if *enableJudge && !*enableQA {
		log.Fatal("--judge requires --qa")
	}
	parsedCutoffs := parseInts(*cutoffs)
	parsedQACutoffs := parseInts(*qaCutoffs)
	if maxCutoff := maxInt(parsedCutoffs); maxCutoff > *limit {
		log.Fatalf("-limit must be >= max -cutoffs value: limit=%d max_cutoff=%d", *limit, maxCutoff)
	}
	if *enableQA {
		if maxCutoff := maxInt(parsedQACutoffs); maxCutoff > *limit {
			log.Fatalf("-limit must be >= max -qa-cutoffs value: limit=%d max_qa_cutoff=%d", *limit, maxCutoff)
		}
	}

	s := store.NewPostgresStore(pool)
	var evalDistiller *distill.Distiller
	if *enableDistillation {
		evalDistiller = distill.New(llm)
	}
	recallH := handler.NewRecallHandler(s, embedder, evalDistiller, nil, nil)

	cfg := locomo.Config{
		DatasetPath:   *dataset,
		CorpusMode:    locomo.CorpusMode(*corpusMode),
		RecallLimit:   *limit,
		Cutoffs:       parsedCutoffs,
		Distillation:  *enableDistillation && llm != nil,
		EnableQA:      *enableQA,
		QACutoffs:     parsedQACutoffs,
		QAModel:       *qaModel,
		QAMaxTokens:   *qaMaxTokens,
		EnableJudge:   *enableJudge,
		JudgeModel:    *judgeModel,
		JudgeEvidence: *judgeEvidence,
		OutputDir:     *outputDir,
		MaxQuestions:  *maxQuestions,
		Categories:    parseInts(*categories),
		AppendOutput:  *appendOutput,
	}
	if *sampleIDs != "" {
		cfg.OnlySampleIDs = strings.Split(*sampleIDs, ",")
	}

	runner := locomo.NewRunner(cfg, pool, s, embedder, recallH, llm)
	result, err := runner.Run(ctx)
	if err != nil {
		log.Fatalf("benchmark failed: %v", err)
	}
	if err := runner.ExportResults(result); err != nil {
		log.Fatalf("export results: %v", err)
	}
	if len(result.Errors) > 0 {
		fmt.Printf("\n%d errors occurred:\n", len(result.Errors))
		for _, errText := range result.Errors {
			fmt.Printf("  - %s\n", errText)
		}
	}
}

func parseInts(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			log.Fatalf("invalid integer %q", part)
		}
		out = append(out, n)
	}
	return out
}

func maxInt(vals []int) int {
	max := 0
	for _, val := range vals {
		if val > max {
			max = val
		}
	}
	return max
}
