## Cursor Session: Fix Search Ranking Bug

Session captured: April 8, 2026
Agent: Cursor (0.47.x)
User: Ziyang
Duration: ~1 hour
Branch: fix-scoring

### Problem

Users reported that recall results were poorly ranked -- relevant memories were appearing at positions 5-8 instead of 1-3. The issue was most visible when querying memories that had both high semantic similarity AND high keyword match, but were being outranked by memories with only moderate semantic scores.

### Debugging Process

**Step 1: Reproduced the issue**

Queried "deployment process for staging" and got:
1. React Component Patterns (score: 0.82) -- irrelevant
2. API Rate Limiting Design (score: 0.79) -- tangentially relevant
3. Deployment Process (score: 0.76) -- should be #1

The scores looked wrong. The deployment memory should have the highest combined score.

**Step 2: Traced the scoring pipeline in recall.go**

Cursor helped me trace through the scoring code. Found the bug in the score combination logic:

```go
// BUG: BM25 score was being normalized incorrectly
// BM25 scores range from 0 to ~25, but we were treating them as 0-1
combinedScore := 0.7*vectorScore + 0.3*bm25Score

// vectorScore for "Deployment Process": 0.85
// bm25Score for "Deployment Process": 12.4 (high keyword match!)
// combined: 0.7*0.85 + 0.3*12.4 = 0.595 + 3.72 = 4.315

// vectorScore for "React Patterns": 0.82
// bm25Score for "React Patterns": 0.3 (low keyword match)
// combined: 0.7*0.82 + 0.3*0.3 = 0.574 + 0.09 = 0.664

// React Patterns wins because its combined score (0.664) is less than
// Deployment's (4.315), BUT the sort was ascending instead of descending!
// Wait no -- the real issue is that the scores aren't comparable.
```

Actually, the bug was more subtle than that. Cursor helped me realize there were TWO bugs:

**Bug 1: BM25 scores not normalized.** The BM25 scores from pg_trgm weren't being normalized to 0-1 range before combining with vector scores.

**Bug 2: Recency boost applied before rerank.** The recency decay multiplier was being applied to the pre-rerank scores, which meant Cohere rerank was working with already-skewed scores.

### Fix

```go
// Fix 1: Normalize BM25 scores to 0-1
maxBM25 := 0.0
for _, r := range results {
    if r.BM25Score > maxBM25 {
        maxBM25 = r.BM25Score
    }
}
for i := range results {
    if maxBM25 > 0 {
        results[i].BM25ScoreNorm = results[i].BM25Score / maxBM25
    }
}

// Fix 2: Apply recency boost AFTER rerank, not before
// Moved the recencyBoost() call from preRerank() to postRerank()
```

### Verification

After the fix, same query "deployment process for staging":
1. Deployment Process (score: 0.91) -- correct!
2. Team Meeting Notes - Sprint Review (score: 0.64) -- mentions deployment
3. API Rate Limiting Design (score: 0.52) -- tangential

nDCG@5 on the smoke eval corpus went from 0.72 to 0.89 after this fix.

### Files Changed

- `packages/server/internal/retrieval/recall.go` -- score normalization + reorder recency boost
- `packages/server/internal/retrieval/recall_test.go` -- added test cases for BM25 normalization
- `packages/server/internal/retrieval/scoring.go` -- extracted scoring helpers

### Lessons Learned

- Always normalize scores to the same range before combining them
- Score combination order matters: normalize -> combine -> rerank -> boost
- The Cursor agent was really helpful for tracing through the code path, but I had to guide it to look at the right variables
