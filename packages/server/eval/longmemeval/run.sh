#!/usr/bin/env bash
#
# LongMemEval benchmark runner for Memax.
#
# Downloads the dataset (if needed), runs the benchmark, and optionally
# evaluates QA answers using LongMemEval's GPT-4o judge.
#
# Usage:
#   ./run.sh                     # Quick: oracle dataset, 10 questions
#   ./run.sh --full              # Full: s_cleaned dataset, all 500 questions
#   ./run.sh --full --qa         # Full + QA hypothesis generation
#   ./run.sh --full --qa --eval  # Full + QA + GPT-4o evaluation
#
# Required environment variables:
#   DATABASE_URL     — PostgreSQL connection string
#   VOYAGE_API_KEY   — Voyage AI API key for embeddings
#
# Optional environment variables:
#   ANTHROPIC_API_KEY — enables query distillation + QA generation
#   OPENAI_API_KEY    — enables GPT-4o QA evaluation (--eval flag)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
DATA_DIR="$SCRIPT_DIR/data"
OUTPUT_DIR="$SCRIPT_DIR/results"

# Parse flags.
DATASET_SIZE="oracle"
MAX_QUESTIONS=10
GRANULARITY="session"
ENABLE_QA=""
ENABLE_EVAL=""
QUESTION_TYPES=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --full)       DATASET_SIZE="s_cleaned"; MAX_QUESTIONS=0 ;;
        --medium)     DATASET_SIZE="m_cleaned"; MAX_QUESTIONS=0 ;;
        --oracle)     DATASET_SIZE="oracle" ;;
        --qa)         ENABLE_QA="--qa" ;;
        --eval)       ENABLE_EVAL="1" ;;
        --turn)       GRANULARITY="turn" ;;
        --session)    GRANULARITY="session" ;;
        --max=*)      MAX_QUESTIONS="${1#--max=}" ;;
        --types=*)    QUESTION_TYPES="${1#--types=}" ;;
        -h|--help)
            echo "Usage: $0 [flags]"
            echo ""
            echo "Dataset flags:"
            echo "  --oracle          Use oracle dataset (evidence only, fast) [default]"
            echo "  --full            Use s_cleaned dataset (~115k tokens/question)"
            echo "  --medium          Use m_cleaned dataset (~500 sessions/question)"
            echo ""
            echo "Scope flags:"
            echo "  --max=N           Evaluate only first N questions (0=all)"
            echo "  --types=a,b       Filter to specific question types"
            echo "  --session         Session-level granularity [default]"
            echo "  --turn            Turn-level granularity"
            echo ""
            echo "Evaluation flags:"
            echo "  --qa              Enable QA hypothesis generation (needs ANTHROPIC_API_KEY)"
            echo "  --eval            Run GPT-4o QA evaluation (needs OPENAI_API_KEY)"
            exit 0
            ;;
        *)
            echo "Unknown flag: $1"
            exit 1
            ;;
    esac
    shift
done

# Check required env vars.
if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "ERROR: DATABASE_URL is required"
    exit 1
fi
if [[ -z "${VOYAGE_API_KEY:-}" ]]; then
    echo "ERROR: VOYAGE_API_KEY is required"
    exit 1
fi

# Download dataset if needed.
mkdir -p "$DATA_DIR"
DATASET_FILE="$DATA_DIR/longmemeval_${DATASET_SIZE}.json"

if [[ ! -f "$DATASET_FILE" ]]; then
    echo "Downloading LongMemEval ${DATASET_SIZE} dataset..."
    REPO_URL="https://raw.githubusercontent.com/xiaowu0162/LongMemEval/main/data"
    curl -fSL "${REPO_URL}/longmemeval_${DATASET_SIZE}.json" -o "$DATASET_FILE"
    echo "Downloaded to $DATASET_FILE ($(du -h "$DATASET_FILE" | cut -f1))"
fi

# Prepare output directory.
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RUN_DIR="$OUTPUT_DIR/${DATASET_SIZE}_${GRANULARITY}_${TIMESTAMP}"
mkdir -p "$RUN_DIR"

echo ""
echo "=== LongMemEval Benchmark ==="
echo "Dataset:     $DATASET_SIZE ($DATASET_FILE)"
echo "Granularity: $GRANULARITY"
echo "Max Q:       ${MAX_QUESTIONS:-all}"
echo "QA:          ${ENABLE_QA:-disabled}"
echo "Output:      $RUN_DIR"
echo ""

# Build and run.
cd "$SERVER_DIR"

ARGS=(
    -dataset "$DATASET_FILE"
    -granularity "$GRANULARITY"
    -output "$RUN_DIR"
    -limit 50
)
if [[ $MAX_QUESTIONS -gt 0 ]]; then
    ARGS+=(-max-questions "$MAX_QUESTIONS")
fi
if [[ -n "$QUESTION_TYPES" ]]; then
    ARGS+=(-types "$QUESTION_TYPES")
fi
if [[ -n "$ENABLE_QA" ]]; then
    ARGS+=(-qa)
fi

go run ./cmd/longmemeval/ "${ARGS[@]}" 2>&1 | tee "$RUN_DIR/benchmark.log"

echo ""
echo "Results saved to $RUN_DIR/"
ls -la "$RUN_DIR/"

# Optional: run LongMemEval's Python QA evaluation.
if [[ -n "$ENABLE_EVAL" ]]; then
    if [[ -z "${OPENAI_API_KEY:-}" ]]; then
        echo ""
        echo "WARNING: --eval requires OPENAI_API_KEY for GPT-4o judge. Skipping."
        exit 0
    fi

    HYP_FILE="$RUN_DIR/memax_qa_hypotheses.jsonl"
    if [[ ! -f "$HYP_FILE" ]]; then
        echo ""
        echo "WARNING: No QA hypotheses file found. Run with --qa first."
        exit 0
    fi

    # Clone LongMemEval repo for eval scripts if needed.
    LONGMEMEVAL_DIR="$DATA_DIR/LongMemEval"
    if [[ ! -d "$LONGMEMEVAL_DIR" ]]; then
        echo ""
        echo "Cloning LongMemEval repo for evaluation scripts..."
        git clone --depth 1 https://github.com/xiaowu0162/LongMemEval.git "$LONGMEMEVAL_DIR"
    fi

    # Install Python dependencies.
    pip install -q openai tqdm backoff numpy nltk 2>/dev/null || true

    echo ""
    echo "Running GPT-4o QA evaluation..."
    python3 "$LONGMEMEVAL_DIR/src/evaluation/evaluate_qa.py" \
        gpt-4o \
        "$HYP_FILE" \
        "$DATASET_FILE"

    EVAL_RESULTS="${HYP_FILE}.eval-results-gpt-4o"
    if [[ -f "$EVAL_RESULTS" ]]; then
        echo ""
        echo "Computing final QA metrics..."
        python3 "$LONGMEMEVAL_DIR/src/evaluation/print_qa_metrics.py" \
            "$EVAL_RESULTS" \
            "$DATASET_FILE"
    fi
fi
