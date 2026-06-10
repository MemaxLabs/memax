#!/usr/bin/env python3
"""
Regenerate QA hypotheses from existing retrieval results using a different model.

Reads the retrieval log (which contains the ranked items per question) and generates
new QA answers using the specified Anthropic model. This avoids re-running the expensive
retrieval pipeline.

Usage:
    ANTHROPIC_API_KEY=... python3 regen_qa.py \
        --model claude-sonnet-4-6 \
        --retrieval-log results/memax_retrievallog_session.jsonl \
        --output results/memax_qa_hypotheses_sonnet.jsonl

    # Then evaluate:
    OPENAI_API_KEY=... python3 data/LongMemEval/src/evaluation/evaluate_qa.py \
        gpt-4o results/memax_qa_hypotheses_sonnet.jsonl data/longmemeval_s_cleaned.json
"""

import argparse
import json
import os
import sys
import time

import anthropic
from tqdm import tqdm


# Same RAG prompt as LongMemEval's run_generation.py and our runner.go
RAG_PROMPT = (
    "I will give you several history chats between you and a user. "
    "Please answer the question based on the relevant chat history. "
    "Answer the question step by step: first extract all the relevant information, "
    "and then reason over the information to get the answer.\n\n\n"
    "History Chats:\n\n{context}\n\n"
    "Current Date: {date}\n"
    "Question: {question}\n"
    "Answer (step by step):"
)


def build_context(ranked_items, top_k=10):
    """Build context from top-k ranked retrieval items."""
    parts = []
    for item in ranked_items[:top_k]:
        ts = item.get("timestamp", "unknown")
        text = item.get("text", "")
        parts.append(f"[Session from {ts}]\n{text}")
    return "\n\n---\n\n".join(parts)


def generate_answer(client, model, question, context, date, max_tokens=512):
    """Generate a QA hypothesis using the Anthropic API."""
    prompt = RAG_PROMPT.format(context=context, date=date, question=question)

    response = client.messages.create(
        model=model,
        max_tokens=max_tokens,
        messages=[{"role": "user", "content": prompt}],
    )
    return response.content[0].text


def main():
    parser = argparse.ArgumentParser(description="Regenerate QA hypotheses with a different model")
    parser.add_argument("--model", required=True, help="Anthropic model ID (e.g., claude-sonnet-4-6)")
    parser.add_argument("--retrieval-log", required=True, help="Path to retrieval log JSONL")
    parser.add_argument("--output", required=True, help="Output path for hypotheses JSONL")
    parser.add_argument("--top-k", type=int, default=10, help="Number of retrieved items to use as context")
    parser.add_argument("--max-tokens", type=int, default=512, help="Max output tokens per answer")
    parser.add_argument("--resume", action="store_true", help="Resume from existing output file")
    args = parser.parse_args()

    api_key = os.environ.get("ANTHROPIC_API_KEY")
    if not api_key:
        print("ERROR: ANTHROPIC_API_KEY is required", file=sys.stderr)
        sys.exit(1)

    base_url = os.environ.get("ANTHROPIC_BASE_URL") or None
    client = anthropic.Anthropic(api_key=api_key, base_url=base_url)

    # Load retrieval log
    print(f"Loading retrieval log from {args.retrieval_log}")
    entries = [json.loads(line) for line in open(args.retrieval_log)]
    print(f"Loaded {len(entries)} entries")

    # Resume support: skip already-processed questions
    done_ids = set()
    if args.resume and os.path.exists(args.output):
        with open(args.output) as f:
            for line in f:
                entry = json.loads(line)
                done_ids.add(entry["question_id"])
        print(f"Resuming: {len(done_ids)} already done, {len(entries) - len(done_ids)} remaining")

    mode = "a" if args.resume and done_ids else "w"
    out_f = open(args.output, mode)

    errors = []
    for entry in tqdm(entries, desc=f"Generating with {args.model}"):
        qid = entry["question_id"]
        if qid in done_ids:
            continue

        ranked_items = entry.get("retrieval_results", {}).get("ranked_items", [])
        context = build_context(ranked_items, args.top_k)
        question = entry["question"]
        date = entry.get("question_date", "")

        try:
            hypothesis = generate_answer(
                client, args.model, question, context, date, args.max_tokens
            )
        except Exception as e:
            print(f"\nERROR {qid}: {e}", file=sys.stderr)
            errors.append(f"{qid}: {e}")
            hypothesis = f"Error: {e}"
            time.sleep(2)  # back off on errors

        result = {"question_id": qid, "hypothesis": hypothesis}
        print(json.dumps(result, ensure_ascii=False), file=out_f)
        out_f.flush()

    out_f.close()

    total = len(entries) - len(done_ids)
    print(f"\nDone: {total} hypotheses generated, {len(errors)} errors")
    if errors:
        print("Errors:")
        for e in errors[:10]:
            print(f"  - {e}")

    print(f"Output: {args.output}")


if __name__ == "__main__":
    main()
