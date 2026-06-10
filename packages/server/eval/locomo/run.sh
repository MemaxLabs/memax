#!/usr/bin/env bash
#
# LoCoMo benchmark runner for Memax.
#
# Usage:
#   ./run.sh                 # Quick: 10 questions, retrieval only
#   ./run.sh --full          # Full: categories 1-4, retrieval only
#   ./run.sh --full --qa     # Full + answer generation
#   ./run.sh --full --qa --qa-cutoffs=10,20 --judge
#   ./run.sh --full --qa --no-distill
#
# Required environment variables:
#   DATABASE_URL     - PostgreSQL connection string
#   VOYAGE_API_KEY   - Voyage AI API key for embeddings
#
# Optional environment variables:
#   ANTHROPIC_API_KEY - enables query distillation and --qa
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
DATA_DIR="$SCRIPT_DIR/data"
OUTPUT_DIR="$SCRIPT_DIR/results"

MAX_QUESTIONS=10
CORPUS_MODE="dialog"
CATEGORIES="1,2,3,4"
CUTOFFS="10,20,50,200"
LIMIT=200
ENABLE_QA=""
DISTILL="-distill=true"
ENABLE_JUDGE=""
JUDGE_EVIDENCE=""
QA_CUTOFFS="10"
QA_MODEL="claude-haiku-4-5-20251001"
JUDGE_MODEL="claude-haiku-4-5-20251001"
QA_MAX_TOKENS=64
SAMPLES=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --full)          MAX_QUESTIONS=0 ;;
        --qa)            ENABLE_QA="--qa" ;;
        --no-distill)    DISTILL="-distill=false" ;;
        --judge)         ENABLE_JUDGE="--judge" ;;
        --judge-evidence) JUDGE_EVIDENCE="--judge-evidence" ;;
        --mode=*)        CORPUS_MODE="${1#--mode=}" ;;
        --categories=*)  CATEGORIES="${1#--categories=}" ;;
        --cutoffs=*)     CUTOFFS="${1#--cutoffs=}" ;;
        --qa-cutoffs=*)  QA_CUTOFFS="${1#--qa-cutoffs=}" ;;
        --qa-model=*)    QA_MODEL="${1#--qa-model=}" ;;
        --judge-model=*) JUDGE_MODEL="${1#--judge-model=}" ;;
        --qa-max-tokens=*) QA_MAX_TOKENS="${1#--qa-max-tokens=}" ;;
        --limit=*)       LIMIT="${1#--limit=}" ;;
        --max=*)         MAX_QUESTIONS="${1#--max=}" ;;
        --samples=*)     SAMPLES="${1#--samples=}" ;;
        -h|--help)
            echo "Usage: $0 [flags]"
            echo ""
            echo "  --full             Run all questions in scope"
            echo "  --qa               Generate answers from retrieved memories"
            echo "  --no-distill       Disable query distillation while keeping QA/Judge LLM calls"
            echo "  --qa-cutoffs=10    QA cutoffs; use 10,20 for production+Mem0 OSS default comparison"
            echo "  --qa-model=...     Anthropic model for QA generation"
            echo "  --qa-max-tokens=64 Max generated answer tokens"
            echo "  --judge            Run LLM-as-judge for generated answers"
            echo "  --judge-model=...  Anthropic model for judge scoring"
            echo "  --judge-evidence   Include gold evidence in judge prompts"
            echo "  --mode=dialog      Corpus mode: dialog, observation, summary"
            echo "  --categories=1,2   LoCoMo categories"
            echo "  --cutoffs=10,50    Evidence recall cutoffs"
            echo "  --limit=200        Max recall results"
            echo "  --max=N            Evaluate only first N questions"
            echo "  --samples=id,...   Filter to sample IDs"
            exit 0
            ;;
        *)
            echo "Unknown flag: $1"
            exit 1
            ;;
    esac
    shift
done

if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "ERROR: DATABASE_URL is required"
    exit 1
fi
if [[ -z "${VOYAGE_API_KEY:-}" ]]; then
    echo "ERROR: VOYAGE_API_KEY is required"
    exit 1
fi

mkdir -p "$DATA_DIR"
DATASET_FILE="$DATA_DIR/locomo10.json"
if [[ ! -f "$DATASET_FILE" ]]; then
    echo "Downloading LoCoMo dataset..."
    curl -fSL "https://raw.githubusercontent.com/snap-research/locomo/main/data/locomo10.json" -o "$DATASET_FILE"
    echo "Downloaded to $DATASET_FILE ($(du -h "$DATASET_FILE" | cut -f1))"
fi

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RUN_DIR="$OUTPUT_DIR/${CORPUS_MODE}_${TIMESTAMP}"
mkdir -p "$RUN_DIR"

echo ""
echo "=== LoCoMo Benchmark ==="
echo "Dataset:     $DATASET_FILE"
echo "Corpus mode: $CORPUS_MODE"
echo "Categories:  $CATEGORIES"
echo "Limit:       $LIMIT"
echo "Cutoffs:     $CUTOFFS"
echo "Max Q:       $MAX_QUESTIONS"
echo "Distill:     $DISTILL"
echo "QA:          ${ENABLE_QA:-disabled}"
if [[ -n "$ENABLE_QA" ]]; then
    echo "QA cutoffs:  $QA_CUTOFFS"
    echo "QA model:    $QA_MODEL"
    echo "Judge:       ${ENABLE_JUDGE:-disabled}"
    if [[ -n "$ENABLE_JUDGE" ]]; then
        echo "Judge model: $JUDGE_MODEL"
    fi
fi
echo "Output:      $RUN_DIR"
echo ""

cd "$SERVER_DIR"

ARGS=(
    -dataset "$DATASET_FILE"
    -corpus-mode "$CORPUS_MODE"
    -categories "$CATEGORIES"
    -cutoffs "$CUTOFFS"
    -limit "$LIMIT"
    -output "$RUN_DIR"
    "$DISTILL"
)
if [[ $MAX_QUESTIONS -gt 0 ]]; then
    ARGS+=(-max-questions "$MAX_QUESTIONS")
fi
if [[ -n "$ENABLE_QA" ]]; then
    ARGS+=(-qa -qa-cutoffs "$QA_CUTOFFS" -qa-model "$QA_MODEL" -qa-max-tokens "$QA_MAX_TOKENS")
fi
if [[ -n "$ENABLE_JUDGE" ]]; then
    ARGS+=(-judge -judge-model "$JUDGE_MODEL")
fi
if [[ -n "$JUDGE_EVIDENCE" ]]; then
    ARGS+=(-judge-evidence)
fi
if [[ -n "$SAMPLES" ]]; then
    ARGS+=(-samples "$SAMPLES")
fi

go run ./cmd/locomo/ "${ARGS[@]}" 2>&1 | tee "$RUN_DIR/benchmark.log"

echo ""
echo "Results saved to $RUN_DIR/"
ls -la "$RUN_DIR/"
