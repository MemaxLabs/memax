# Judge eval: live results

Run on Oct 6, 2026, against OpenRouter's Anthropic-compatible Messages API (staging key) and Voyage, from a dev machine (not Fly's `sjc`). Every call went through `eval/livemeter`, which records each call's routing, serving provider, tokens and cost without changing it. Raw verdicts (pair ids, labels, relations, confidences, tiers, timings; no memory text) are in `results-2026-10-06.json`.

```bash
cd packages/server
JUDGE_EVAL_LIVE=1 JUDGE_EVAL_TIERS=pipeline,primary,fallback,strong go test ./eval/judge/ -run Live -v -timeout 30m
JUDGE_EVAL_LIVE=1 JUDGE_EVAL_SET=holdout.json JUDGE_EVAL_TIERS=pipeline,primary go test ./eval/judge/ -run Live -v
V2_EVAL_LIVE=1 go test ./eval/judge/ -run Candidates -v
```

## Summary

- **The bar passes, after one prompt change.**
  - With the Phase 1 prompt, the pipeline's `contradicts` precision was 0.85, 0.93 and 0.90 over three runs (bar 0.90).
  - With the clarified updates/contradicts definitions (`internal/judge/prompt.go`), it was **1.00 in all three runs, with recall 0.96** (bar 0.75).
  - On a held-out set written before the change, it was 1.00/1.00 twice.
- **The judge's own action never misfired.** Across all ten pipeline runs (five before the prompt change and five after, on both sets), it flagged nothing that wasn't a conflict on a decision in force, at any threshold. That is the flag rule 11 depends on.
- **Contradicts threshold: 0.8 → 0.6** (`DefaultThresholds`, `internal/judge/config.go`).
  - Sonnet 5.5 gives three of the 28 planted conflicts a confidence of 0.55–0.72, so 0.8 flagged only 25 of 28.
  - At 0.6 it flags 27–28 of 28, still with no false flag.
- **Tiers: no change.**
  - DeepSeek V4.1 Flash stays the primary (prompted JSON), Claude Haiku 4.5 the strict fallback, and Claude Sonnet 5.5 the strong tier.
  - GPT-6 Luna is a viable, much cheaper fallback, but the fallback was never needed: none of 874 pipeline verdicts fell back, and 2 retried the primary once.
- **Zero data retention holds on every slug** (D14). Each of the 2,877 metered judge calls asked for `provider.zdr` and was served by a provider on OpenRouter's zero-retention list for that model, never DeepSeek's own API. V4.1 Flash has many zero-retention hosts, so D14 needs no tier change.
- **Strict structured output (`output_config.format`) is honoured** on all four slugs, V4.1 Flash included.
- **Latency is inside 5 s.**
  - On `pairs.json`, verdict p95 is 2.4–2.9 s with the new prompt (1.9–3.1 s before), and 1.9–3.7 s across all ten pipeline runs.
  - Verdicts the strong tier confirms take p95 2.8–3.5 s.
  - Sonnet answers in p50 1.3 s with no thinking.
  - In three of the ten runs the slowest verdict passed 5 s (5.5, 5.7 and 7.1 s).
- **Vector candidates:** voyage-4 lifts the related pairs' candidate recall from 0.87 to 0.91. `JUDGE_VECTOR_FLOOR` stays 0.65: between 0.45 and 0.75 the floor moves recall by one pair at most.
- **Remember's near-duplicate floor: 0.90 → 0.85** (`internal/v2recall/vector.go`). The draft is embedded with voyage-4-lite and compared with voyage-4, which reads about 0.05 lower.
- **Cost:** $2.47 for every live judge and Ask run here. A full pipeline run over the 124 pairs costs about $0.20, almost all of it Sonnet.

## Models and prices

| Tier                     | Slug                           | OpenRouter list price, per 1M in / out | Served by (zero retention)                                                                                            |
| ------------------------ | ------------------------------ | -------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Primary                  | `deepseek/deepseek-v4.1-flash` | $0.30 / $1.20 (hosts charge less)      | Together, InferenceNet (fp8), CoreWeave (fp8), DigitalOcean, BaseTen, Morph, DeepInfra, Venice, Wafer, Decart (fp4) |
| Strict fallback          | `anthropic/claude-haiku-4.5`   | $1 / $5                                | Amazon Bedrock                                                                                                        |
| Strong (decisions)       | `anthropic/claude-sonnet-5.5`  | $2 / $10                               | Google Vertex                                                                                                         |
| Tried as primary/fallback | `openai/gpt-6-luna`            | $0.10 / $0.50                          | Azure                                                                                                                 |

The primary's routing varies by the hour: one run went 121 of 125 calls to Together, and later runs 123 of 124 to InferenceNet. DeepSeek V4.1 Flash's zero-retention hosts include fp8 and fp4 quantizations, and OpenRouter chooses between them. The shared client sends only `provider.zdr`, so it can't pin a quantization (see the follow-ups).

## The bars

The label-level measure is what the harness asserts: the relation `contradicts` over all 124 pairs. The flag is what the judge does with a verdict on a decision in force: it flags a contradiction, or an implicit update, at confidence ≥ the Contradicts threshold. All 28 planted contradictions are against decisions in force.

### The pipeline (primary → strong on decisions in force)

| Prompt         | Run | `contradicts` P / R | Flag P / R at 0.8 | Flag P / R at 0.6 | duplicate P / R | updates P / R | Fact updates linked |
| -------------- | --- | ------------------- | ----------------- | ----------------- | --------------- | ------------- | ------------------- |
| Phase 1        | 1   | 0.85 / 1.00         | 1.00 / 0.93       | 1.00 / 1.00       | 1.00 / 1.00     | 1.00 / 0.83   | 14/18               |
| Phase 1        | 2   | 0.93 / 1.00         | 1.00 / 0.93       | 1.00 / 1.00       | 1.00 / 1.00     | 1.00 / 0.92   | 16/18               |
| Phase 1        | 3   | 0.90 / 1.00         | 1.00 / 0.93       | 1.00 / 1.00       | 1.00 / 1.00     | 1.00 / 0.88   | 15/18               |
| **Oct 6**      | 1   | **1.00 / 0.96**     | 1.00 / 0.89       | 1.00 / 1.00       | 1.00 / 1.00     | 0.96 / 1.00   | 18/18               |
| **Oct 6**      | 2   | **1.00 / 0.96**     | 1.00 / 0.89       | 1.00 / 0.96       | 1.00 / 0.96     | 0.96 / 1.00   | 18/18               |
| **Oct 6**      | 3   | **1.00 / 0.96**     | 1.00 / 0.89       | 1.00 / 1.00       | 1.00 / 0.92     | 0.96 / 1.00   | 18/18               |

Every fold of a duplicate was right (precision 1.00, recall 0.92–0.96), and so was every supersede of an explicitly changed decision (6/6 in every run).

**Held out.** These are 30 pairs written before the prompt change, with the same labelling rules (`holdout.json`). Contradicts P / R:

| Prompt  | Pipeline              | Primary alone         |
| ------- | --------------------- | --------------------- |
| Phase 1 | 0.86 / 1.00, 1.00 / 1.00 | 0.83 / 0.83, 1.00 / 1.00 |
| Oct 6   | 1.00 / 1.00, 1.00 / 1.00 | 0.86 / 1.00, 1.00 / 0.83 |

The pipeline flagged exactly the 6 planted conflicts every time, before and after. The held-out set is small: it shows the change carries over and doesn't regress, not a measured gain.

### What changed in the prompt, and why

The Phase 1 definitions overlapped:

- `contradicts` read "both can't be true at once", which is also true of a fact's new value ("Access tokens expire after 30 minutes" against "after 1 hour").
- The in-force rule said "contradicts (or updates)".

The primary labelled those pairs at random between runs. Over three runs, 14 pairs changed relation in the pipeline and 19 with the primary alone. The pairs moving into and out of `contradicts` were almost all updates: 5 of 6 in the pipeline, 11 of 12 alone. The label set and plan §5.8 follow one rule, and the prompt now says it:

- a different value for a detail of a fact **updates** it;
- a different choice against a decision in force **contradicts** it, unless the proposal itself says the decision changed, which **updates** it (and supersedes when explicit).

After the change, the pipeline runs disagreed on 9 pairs, all of them extends ↔ unrelated or duplicate ↔ extends, which change no outcome. The outcome-level gains came with it:

- fact updates are now linked ("Updates M-…") 18/18 times, against 14–16 before;
- the primary alone went from `contradicts` precision 0.74–0.85 to 0.93–1.00.

The extends ↔ unrelated confusion is left alone. The judge calls 10–15 of the 22 extensions unrelated, both before and after, but `Decide` acts on neither, and the bars don't cover extends.

### The contradicts threshold

Every verdict on a decision in force is confirmed by the strong tier, so the threshold is in effect Sonnet's confidence bar. In all ten pipeline runs (six on `pairs.json` and four on `holdout.json`, before and after the prompt change), no false flag appeared at any threshold from 0 up. Three real conflicts sit low:

| Pair   | Proposal against the decision in force                                               | Sonnet's confidence over the runs                                                         |
| ------ | ------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| con-04 | "API keys may keep memories in personal spaces" against "An API key can never keep or forget" | 0.70–0.72                                                                          |
| con-25 | "npx memax init" against "npx memax-cli init"                                        | 0.60–0.72                                                                                 |
| con-28 | "Retrieval goes through Mem0 instead of Postgres"                                    | 0.55–0.60, labelled updates with `explicit_change` (the proposal is a fact, so it can't supersede; it is a conflict) |

Flag recall by threshold, Oct 6 prompt, three runs on `pairs.json`:

| Threshold | Recall                                          |
| --------- | ----------------------------------------------- |
| 0.8       | 0.89, 0.89, 0.89                                |
| 0.7       | 0.93, 0.93, 0.96                                |
| 0.6       | 1.00, 0.96, 1.00                                |
| 0.5       | 1.00, 1.00, 1.00, with precision 1.00 throughout |

0.6 was chosen over 0.5 to keep a margin, since the evidence is 28 conflicts and about 27 in-force non-conflicts per run. Without the strong tier (`JUDGE_STRONG_MODEL=off`), the primary's false flags came at confidence ≥ 0.8, so the bar wouldn't guard them at 0.8 either. On the held-out set, though, one primary-alone false flag sat at 0.7–0.8.

### Each tier alone

Oct 6 prompt. The strong tier is scored on the 55 pairs whose candidate is a decision in force, the only verdicts it gives in the pipeline.

| Tier                        | Pairs | `contradicts` P / R          | duplicate R | updates P / R | Verdict p50 / p95  | Cost   |
| --------------------------- | ----- | ---------------------------- | ----------- | ------------- | ------------------ | ------ |
| V4.1 Flash, prompted (×3)   | 124   | 1.00/1.00, 0.93/1.00, 1.00/1.00 | 1.00     | 1.00 / 1.00   | 0.55–1.19 s / 1.06–2.67 s | $0.006 |
| V4.1 Flash, strict          | 124   | 0.96 / 0.96                  | 1.00        | 0.96 / 0.96   | 0.76 s / 1.56 s    | $0.016 |
| Haiku 4.5, strict           | 124   | 0.90 / 0.96                  | 1.00        | 0.96 / 0.96   | 1.77 s / 2.42 s    | $0.22  |
| GPT-6 Luna, prompted        | 124   | 1.00 / 1.00                  | 0.77        | 1.00 / 1.00   | 1.39 s / 3.08 s    | $0.019 |
| GPT-6 Luna, strict          | 124   | 0.93 / 1.00                  | 0.88        | 1.00 / 1.00   | 1.45 s / 2.74 s    | $0.015 |
| Sonnet 5.5, strict          | 55    | 1.00 / 0.96                  | 1.00        | 0.86 / 1.00   | 1.30 s / 1.77 s    | $0.26  |

With the Phase 1 prompt the scores were:

| Tier                  | `contradicts` P / R              |
| --------------------- | -------------------------------- |
| V4.1 Flash, three runs | 0.85/1.00, 0.78/1.00, 0.74/1.00 |
| Haiku                 | 0.82 / 0.96                      |
| Sonnet                | 1.00 / 1.00                      |

- **Keep V4.1 Flash prompted as the primary.** Strict mode scored no better and spread calls over more hosts, fp4 included, at the same cost.
- **GPT-6 Luna** answers 2–3× slower than V4.1 Flash and misses more duplicates (0.77–0.88), so it doesn't replace the primary. As the fallback it beats Haiku on `contradicts` precision (0.93 against 0.90) at 1/14 the cost. But none of the 874 pipeline verdicts fell back, and 2 retried the primary once, so there's no evidence that switching matters. Haiku stays.

## Routing and structured output, per slug

**`provider.zdr` is honoured** on `/v1/messages`.

- A request pinned to non-zero-retention providers (`provider.only: ["deepseek"]` or `["alibaba"]` with `zdr: true`) was refused with 404 "No endpoints found matching your data policy (Zero data retention)".
- Every metered call in these runs asked for zero retention and was served by a provider on OpenRouter's zero-retention list for its slug (`/api/v1/endpoints/zdr`). The harness asserts both on every run.
- The response names the provider (`provider`) and the cost (`usage.cost`), and so do streams (`message_start`). That is how the meter checks them.

**`output_config.format` is honoured.** A probe schema with a marker enum the prompt never mentioned came back conforming on every slug:

| Slug        | Probes      | Host                 |
| ----------- | ----------- | -------------------- |
| V4.1 Flash  | 10 of 10    | CoreWeave, InferenceNet |
| Haiku 4.5   | 2 of 2      | Bedrock              |
| Sonnet 5.5  | 2 of 2      | Vertex               |
| GPT-6 Luna  | 2 of 2      | Azure                |

In the eval, none of the 1,013 strict calls needed a retry. Plan §4.3 says V4.1 Flash has JSON mode but no schema enforcement. On OpenRouter's zero-retention hosts it does enforce the schema: most of them list `structured_outputs`.

**Thinking.**

- The client sends `thinking: disabled` to non-Claude models: V4.1 Flash and Luna used 0 thinking tokens.
- Sonnet 5.5, with `thinking` omitted, used no thinking tokens in its 283 pipeline calls, and 271 tokens on one of its 110 standalone calls.
- The strong tier's latency therefore needs no effort setting today.

## Latency (target: a verdict within 5 s)

| Prompt  | Run | Verdict p50 / p95 / max | Confirmed by Sonnet p50 / p95 | Sonnet call p50 / p95 | Primary call p50 / p95 |
| ------- | --- | ----------------------- | ----------------------------- | --------------------- | ---------------------- |
| Phase 1 | 1   | 0.43 / 3.13 / 5.67 s    | 1.74 / 4.65 s                 | 1.32 / 4.07 s         | 0.39 / 0.88 s          |
| Phase 1 | 2   | 0.48 / 1.93 / 3.08 s    | 1.79 / 2.18 s                 | 1.35 / 1.63 s         | 0.41 / 0.66 s          |
| Phase 1 | 3   | 0.50 / 1.94 / 3.05 s    | 1.74 / 2.50 s                 | 1.35 / 1.68 s         | 0.40 / 0.85 s          |
| Oct 6   | 1   | 0.65 / 2.36 / 2.96 s    | 1.90 / 2.78 s                 | 1.35 / 1.65 s         | 0.58 / 1.48 s          |
| Oct 6   | 2   | 0.77 / 2.91 / 7.05 s    | 2.09 / 3.54 s                 | 1.33 / 1.81 s         | 0.72 / 1.63 s          |
| Oct 6   | 3   | 0.97 / 2.77 / 3.00 s    | 2.28 / 2.90 s                 | 1.39 / 1.72 s         | 0.86 / 1.73 s          |

- These are four verdicts in flight, measured from a dev machine. Add stage 0 and candidates (about 8 ms, Phase 1 log) for the whole judgement.
- The primary's speed follows its host: Together answered in about 0.4 s, InferenceNet in 0.6–0.9 s.

## The judge's vector candidates (voyage-4)

Each of the 124 proposals is judged in one space that holds all 99 distinct candidate statements.

| Class       | Pairs | Labelled candidate reached the model, lexical | with vectors |
| ----------- | ----- | --------------------------------------------- | ------------ |
| duplicate   | 26    | 25                                            | 25           |
| updates     | 24    | 22                                            | 22           |
| extends     | 22    | 16                                            | 18           |
| contradicts | 28    | 24                                            | 26           |

Related-pair candidate recall: **0.87 lexical → 0.91 with vectors**. The fake embedder showed no gain.

Of the 13 related pairs the lexical lanes miss:

- 7 aren't misses. Their proposal is word for word another pair's candidate in the shared space, so stage 0 folds it there, correctly, before any model call.
- Of the 6 left, the vectors win 4 (similarity 0.68–0.82, ranks 1–3). One sits at 0.60, under the floor; one isn't among the 10 nearest.

**`JUDGE_VECTOR_FLOOR` stays 0.65.**

- End to end, related candidate recall is 0.91 at every floor from 0.45 to 0.65, and 0.92 at 0.55, 0.90 at 0.70, 0.89 at 0.75.
- The number of unrelated candidates that reach the model doesn't change (6 of 24, all of them from the lexical lanes).
- Pairwise cosine on voyage-4 (min / p10 / median):
  - duplicates 0.72 / 0.87 / 0.98
  - updates 0.74 / 0.75 / 0.96
  - extends 0.37 / 0.52 / 0.65
  - contradicts 0.49 / 0.64 / 0.76
  - unrelated 0.28 / 0.35 / 0.42, max 0.69
- At 0.65, 86 of 100 related pairs and 3 of 24 unrelated pairs pass. The floor isn't what limits recall here, so there is no case for moving it.

**Remember's near-duplicate floor (`V2_NEAR_DUPLICATE_FLOOR`): 0.90 → 0.85.**

- Production embeds the draft with the query model (voyage-4-lite) and compares it with stored voyage-4 vectors.
- That pairing reads lower than voyage-4 on both sides: duplicates have a median of 0.93 against 0.98.

| Floor (draft on lite) | Duplicates offered | Updates offered | Extensions, contradictions, unrelated offered |
| --------------------- | ------------------ | --------------- | --------------------------------------------- |
| 0.90                  | 18/26              | 14/24           | 1/74                                          |
| 0.88                  | 19/26              | 17/24           | 2/74                                          |
| **0.85**              | **21/26**          | **17/24**       | **3/74** (none unrelated)                     |
| 0.80                  | 23/26              | 18/24           | 10/74                                         |

0.85 with voyage-4-lite drafts is exactly the operating point 0.90 has with voyage-4 on both sides (21, 17, 3). The extra offers are a new value of the same fact, or a contradiction of it, which a person keeping a memory should see anyway. No cosine floor separates duplicates from updates: their distributions overlap from 0.87 up. Telling them apart is the judge's job.

## Cost

OpenRouter spend for everything in this report and the Ask eval was **$2.47**, read from the key's balance before and after ($19.509 → $17.036 remaining) and matching the meter's sum.

| Run                                                              | Cost  |
| ---------------------------------------------------------------- | ----- |
| One full set, all four runs (pipeline, primary, fallback, strong) | $0.68 |
| A pipeline run                                                   | $0.19–0.20 |
| A primary-only run                                               | $0.006–0.017 |
| The held-out set                                                 | about $0.05 |
| Ask, all six runs                                                | under $0.01 |

Voyage usage for the candidates runs was a few thousand short texts.

## Follow-ups

- **Pin or exclude quantizations for the primary**, if quality varies by host. The shared client sends only `provider.zdr`. OpenRouter's `provider.quantizations` (for example excluding `fp4`) or `provider.sort` would need a `CompleteRequest` field. Nothing measured here shows a host-dependent quality difference, so this is a watch item.
- **Temperature.** The client sets none, so each provider's default applies. The run-to-run flips with the Phase 1 prompt were sampling. A judge at temperature 0 would be steadier, but it needs a `CompleteRequest` field and a re-run.
- **Write the eval set out further** past 28 conflicts:
  - contradictions of kept facts (none today; every planted contradiction is against a decision in force);
  - multi-candidate prompts (every live call here judges one pair; production sends up to 10 candidates, and false flags on longer lists are untested).
- **Re-run this eval** once the branch `v2-rule11` lands, since a flag will then move a Write agent's kept memory back to Review. The bar's stakes go up, and the evidence above should be re-checked against that code.
