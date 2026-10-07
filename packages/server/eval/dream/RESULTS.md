# Dream eval results

`go test ./eval/dream/ -v` runs Dream (internal/v2dream) twice on the fixture space in
`fixture.json`: run A the night after the record was written, run B sixty-one days later
after one more note. Every phase has a known answer; the fake model (internal/v2dream/dreamtest)
answers from the fixture's oracle, and `DREAM_EVAL_LIVE=1` adds the real `DREAM_*` tiers.

## Oct 7, 2026: fake model and live tiers

Tiers: `DREAM_MODEL` deepseek/deepseek-v4.1-flash (primary), anthropic/claude-haiku-4.5
(fallback, never needed), anthropic/claude-sonnet-5.5 (strong, confirming the verdict on the
decision in force). Zero-data-retention routing on every call (`provider.zdr`), checked
against OpenRouter's list by eval/livemeter.

| Phase                                   | Fake (oracle) | Live, 4 runs                       |
| --------------------------------------- | ------------- | ---------------------------------- |
| Fold (note → kept memory)               | P 1.00, R 1.00 (3/3) | P 1.00, R 1.00 every run    |
| New facts (from notes, not chatter)     | P 1.00, R 1.00 (2/2) | P 1.00, R 1.00; 0 from chatter |
| Dedupe (proposal repeats)               | P 1.00, R 1.00 (2/2) | P 1.00, R 1.00              |
| Conflict (fact vs fact, fact vs decision) | P 1.00, R 1.00 (2/2) | P 1.00, R 1.00; the decision verdict confirmed by the strong tier |
| Stale (stale_after passed)              | 1/1           | 1/1                                |
| Fade (60 days unread; exemptions held)  | 2/2           | every unplaced one; the live Brief placed some, which then don't fade |
| Brief (small, cited)                    | 1 change of 2 ops, the uncited line dropped | 1 change, every line cited |
| Undo of run A's actions, after run B   | 9 of 10; one refused as behind a later change | the same |

- Run A on the live tiers: 10 model calls (9 primary, 1 strong), 6.9–8.8 s for the whole run,
  gateway p50 410–510 ms and p95 1.5–2.1 s per call.
- Spend: $0.0037, $0.0079, $0.0067 and $0.0068 by the meter: **$0.025 for the four live runs**
  (the OpenRouter balance moved $0.0085 while they ran; its usage lags).
- No call was served by a provider that isn't zero-retention for its model.

The fixture is small on purpose: it checks that the prompts, validation and the ledger's
re-checks do what each phase promises, not how Dream does on a large, messy record. Grow it from
alpha editions (with their undos as labels) before tuning the bars.
