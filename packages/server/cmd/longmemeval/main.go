// Command longmemeval runs the LongMemEval benchmark (ICLR 2025) against
// Memax's retrieval pipeline.
//
// Usage:
//
//	go run ./cmd/longmemeval/ -dataset data/longmemeval_s_cleaned.json
//
// Required environment variables:
//
//	DATABASE_URL     — PostgreSQL connection string
//	VOYAGE_API_KEY   — Voyage AI API key for embeddings
//
// Optional environment variables:
//
//	ANTHROPIC_API_KEY — enables QA hypothesis generation via Claude
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/eval/longmemeval"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	dbmigrate "github.com/MemaxLabs/memax/packages/server/internal/migrate"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/distill"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func main() {
	// Flags.
	dataset := flag.String("dataset", "", "Path to LongMemEval JSON dataset (required)")
	granularity := flag.String("granularity", "session", "Corpus granularity: session or turn")
	corpusMode := flag.String("corpus-mode", "user-only", "Corpus mode: user-only (standard) or all-turns (include assistant)")
	limit := flag.Int("limit", 50, "Max recall results per query (need ≥50 for all k values)")
	maxQuestions := flag.Int("max-questions", 0, "Limit to first N questions (0 = all)")
	questionTypes := flag.String("types", "", "Comma-separated question types to evaluate (empty = all)")
	skipAbstention := flag.Bool("skip-abstention", false, "Skip abstention questions")
	onlyIDs := flag.String("ids", "", "Comma-separated question IDs to evaluate (retry failed questions)")
	appendOutput := flag.Bool("append", false, "Append to existing result files instead of overwriting")
	enableQA := flag.Bool("qa", false, "Enable QA hypothesis generation (requires ANTHROPIC_API_KEY)")
	outputDir := flag.String("output", ".", "Output directory for result files")
	flag.Parse()

	if *dataset == "" {
		fmt.Fprintln(os.Stderr, "Usage: longmemeval -dataset <path-to-dataset.json>")
		fmt.Fprintln(os.Stderr, "\nRequired: DATABASE_URL, VOYAGE_API_KEY")
		fmt.Fprintln(os.Stderr, "Optional: ANTHROPIC_API_KEY (for QA and distillation)")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Connect to database.
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

	// Run migrations — try the standard path first (go run from packages/server/).
	// Migration failure is non-fatal: the database is likely already migrated in
	// local dev. We just log a warning and continue.
	if err := dbmigrate.Run(dbURL, "migrations"); err != nil {
		log.Printf("WARNING: migrations: %v (continuing — DB may already be migrated)", err)
	}

	// Initialize embedder.
	embedder := embed.NewEmbedder()
	if embedder == nil {
		log.Fatal("VOYAGE_API_KEY is required for embeddings")
	}

	// Initialize LLM client (optional — enables distillation + QA).
	llm := anthropic.NewFromEnv()
	if llm != nil {
		log.Println("Anthropic LLM enabled — distillation and QA synthesis active")
	} else {
		log.Println("No ANTHROPIC_API_KEY — distillation disabled, raw query path")
		if *enableQA {
			log.Fatal("--qa requires ANTHROPIC_API_KEY")
		}
	}

	// Wire up components.
	s := store.NewPostgresStore(pool)
	evalDistiller := distill.New(llm)
	recallH := handler.NewRecallHandler(s, embedder, evalDistiller, nil, nil)

	// Build config.
	cfg := longmemeval.Config{
		DatasetPath:    *dataset,
		Granularity:    longmemeval.Granularity(*granularity),
		CorpusMode:     longmemeval.CorpusMode(*corpusMode),
		RecallLimit:    *limit,
		EnableQA:       *enableQA,
		OutputDir:      *outputDir,
		MaxQuestions:   *maxQuestions,
		SkipAbstention: *skipAbstention,
		AppendOutput:   *appendOutput,
	}
	if *questionTypes != "" {
		cfg.QuestionTypes = strings.Split(*questionTypes, ",")
	}
	if *onlyIDs != "" {
		cfg.OnlyQuestionIDs = strings.Split(*onlyIDs, ",")
	}

	// Run benchmark.
	runner := longmemeval.NewRunner(cfg, pool, s, embedder, recallH, llm)
	result, err := runner.Run(ctx)
	if err != nil {
		log.Fatalf("benchmark failed: %v", err)
	}

	// Export results.
	if err := runner.ExportResults(result); err != nil {
		log.Fatalf("export results: %v", err)
	}

	// Print summary.
	longmemeval.PrintSummary(result)

	if len(result.Errors) > 0 {
		fmt.Printf("\n%d errors occurred:\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}
}
