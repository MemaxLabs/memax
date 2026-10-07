# Import cleanup eval: live results

Run on Oct 7, 2026, against OpenRouter's Anthropic-compatible Messages API (staging key), from a dev machine (not Fly's `sjc`). Every call went through `eval/livemeter`, which records each call's routing, serving host, tokens and cost without changing it. Raw records (statement refs, confidences, classes, timings; no statement or model words) are in `results-2026-10-07.json`.

**`holdout.json` was written first**, before `batches.json`, before any live run and before any change to the import check. Nothing was tuned on it.

```bash
cd packages/server
go test ./eval/imports/ -v                                                     # the sets, the scorer, the harness on a fake model
IMPORT_EVAL_LIVE=1 go test ./eval/imports/ -run Live -v                        # four times
IMPORT_EVAL_LIVE=1 IMPORT_EVAL_SET=holdout.json go test ./eval/imports/ -run Live -v
IMPORT_EVAL_LIVE=1 IMPORT_EVAL_TIERS=fallback go test ./eval/imports/ -run Live -v
IMPORT_EVAL_LIVE=1 IMPORT_EVAL_BATCH=120 IMPORT_EVAL_ONLY=atlas go test ./eval/imports/ -run Live -v   # the old cut
```

The live run reads the worker's configuration (`judge.ConfigFromEnv`): the import check uses the judge's primary tier, then its fallback. `IMPORT_EVAL_DEBUG=1` prints every answer the check couldn't use, and why.

## Summary

- **The §11 bar is met: every planted conflict found.** In four runs of `batches.json` the check found 54 of 54, and in four runs of `holdout.json` 17 of 17, at `ImportConflictBar` 0.8.
  - Precision was 0.98 in each main run (one wrong group in 55) and 0.94, 0.94, 1.00 and 0.94 held out.
  - A wrong group held 0.5% of the statements that agree out of bulk keep (2 of 425).
- **The eval found three faults in the check, now fixed:**
  1. **Correct answers were refused.** The primary leaves `suggestion` out when it has none, and the schema required it. At temperature 0 the retry repeated the omission, so 4 of 7 imports fell back to Haiku at 4–8 s a call. `suggestion` is now optional.
  2. **The check cut long imports apart.** An import over 120 proposals was cut by section, and the cut fell between files, so even a perfect model found only 41 of 54 conflicts. On a 215-proposal import, the real model found 2 of 10 when cut and 10 of 10 in one call. A whole import is now one call.
  3. **The prompt misread scope and personal facts.**
     - It read a narrower rule's exception as a disagreement.
     - It couldn't see headings, which are sometimes all that says where a statement applies.
     - It was unsure of disagreements between facts about the developer.

     The new prompt fixes the first and third, and the check now sends each statement's headings.
- **`ImportConflictBar` stays 0.8** (before: 0.8, after: 0.8), now on evidence. In eight final runs every planted conflict came back at 0.85 or more, so 0.8 keeps all 284 with a margin. Wrong groups ranged from 0.6 to 0.9; 0.8 keeps out 9 of 16, and 0.9 would lose 8 conflicts.
- **Proposed precision bar: 0.95 on `batches.json`**, asserted beside recall 1.00 (see "The bars").
- **Latency:** an import check takes p50 1.0–1.2 s and p95 1.9–2.2 s. The largest import (215 proposals) took 1.8–2.2 s, and 4.6 s once. Before the fixes, p95 was 10.8 s, because of fallbacks.
- **Cost:** about $0.005 for a run of `batches.json` and $0.002–0.003 for `holdout.json`. The whole eval spent **$0.32** by the meter.
  - The key's balance read $14.4714 before the first run and $14.1608 before the last two.
  - That $0.3106 matches the meter's sum for the same runs.
- **Hosts:** the 71 calls whose gateway summary was printed, the confirmation runs among them, were served by Together (primary) and Amazon Bedrock (fallback). Each asked for zero retention, its pinned hosts and the fp8 floor, at temperature 0. The harness fails a run if any call is served elsewhere.

## The sets

| Set            | Imports                 | Statements | Proposals (folded) | Largest | Planted conflicts (three-way) | Traps                                                                 |
| -------------- | ----------------------- | ---------- | ------------------ | ------- | ----------------------------- | --------------------------------------------------------------------- |
| `batches.json` | 7 project, 1 personal   | 551        | 549 (2)            | 215     | 54 (5)                        | 53: scope 12, complementary 10, repeat 14, number 7, date 4, version 6 |
| `holdout.json` | 2 project, 1 personal   | 119        | 119                | 48      | 17 (2)                        | 20: scope 3, complementary 4, repeat 5, number 3, date 3, version 2   |

Each batch is one import as `memax init` sends it:

- **Statements:** each is a list item or paragraph from a file, with its `file:line`, the headings above it (`under`) and the file's kind.
- **Order:** files are listed in the order init reads them.
- **Trust:** the file's kind sets it:
  - repository files are `repository`;
  - `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md` and `CLAUDE.local.md` are `person`;
  - agents' memory is `agent_own_work`.
- **Space:** init sends repository files to the project's space and this machine's files to Personal, so a batch is one or the other.
- **Proposals:** the harness builds them as the ledger does (`ledger.FoldImportItems` folds verbatim repeats into one proposal citing both files). It gives them to the check as `ImportCheckSnapshot` would, headings included.

The imports range from 22 to 215 proposals:

- **Project imports** (TypeScript, Go, Python, Rails, Rust, Kotlin and Terraform repositories) use `AGENTS.md`, `CLAUDE.md`, `.claude/rules`, Cursor rules with globs, `.cursorrules`, Copilot instructions (repository-wide and `applyTo`-scoped), `GEMINI.md`, and Windsurf and Devin rules.
- **Personal imports** use `CLAUDE.local.md`, `~/.claude/CLAUDE.md`, Claude Code's project memory, `~/.codex/AGENTS.md`, Codex's memories and `~/.gemini/GEMINI.md`.
- **The largest imports:** `orbit` (142) and `atlas` (215) have a long `AGENTS.md`. Their planted conflicts are spread between it and files read late (Copilot, `GEMINI.md`). `atlas` was added after the first live runs, to measure one call over an import well past 142 proposals.

**Planted conflicts:**

| Kind                                      | Main | Held out |
| ----------------------------------------- | ---- | -------- |
| Test commands, frameworks and coverage    | 7    | 1        |
| Build                                     | 3    | 1        |
| Lint and format                           | 5    | 1        |
| Package managers                          | 3    | 1        |
| Language and tool versions                | 8    | 3        |
| Deploy branches                           | 3    | 1        |
| Error formats                             | 3    | 1        |
| Naming and style                          | 8    | 3        |
| Preferences                               | 2    | 3        |
| Schedules and dates                       | 3    | 1        |
| Others (architecture, API, data)          | 9    | 1        |

Across both sets, 11 conflicts hinge on a number or a date and 10 on a version. Personal imports set the person's files against each other, against an agent's notes, and `CLAUDE.local.md` against the person's own global files.

**Labelling rules:**

- **Conflict:** statements an agent can't all follow, or that can't all be true, in the same place once kept. All its `members` must be in one group for it to count as found.
  - `also` lists statements that take a side in it (installing with pnpm, in a pnpm-or-yarn-or-npm conflict). A group may hold them without penalty, and needn't.
- **Scope trap:** a subpackage's own command, a rule for part of the places a general rule covers, or a rule scoped by its applies globs, its words or its heading.
  - An exception for part of a rule's places is not a disagreement: `as any` in tests against "never use `any`", or framework pages' default exports.
  - Two rules for the same files are a disagreement, whichever one names its files: a test-file rule saying Jest against "unit tests run on Vitest".
- **Other traps:** complementary statements on one subject, the same rule in other words, and numbers, dates and versions that only look different (Postgres 16 and `postgres:16-alpine`, macOS 15 and Sequoia).
  - No repeat trap folds before the check, so each one reaches the model.
- **`CLAUDE.local.md`:** init sends it to Personal without its repository's scope. Once kept, its statements sit beside the person's global files.
  - One that doesn't name its scope in its words disagrees with them (`kai-indent`, `zz-code-first`).
  - One that does ("in this repository") is a scope trap (`kai-scripts`, `zz-kotlin`).
- **One main-set label was rewritten after the first live runs.** `zz-code-first` said "Explain the plan before writing any code" against "Show the code first and explain it after". Both can be followed (plan, write, show, explain), so the pair was not a disagreement. It now reads "Explain the plan and wait for my go-ahead before writing any code" against "Don't wait for a go-ahead: make the change, then explain it." No held-out label changed.

## Scoring

A planted conflict is **found** when one group holds all its members. Each group gets a **credit**, and **precision** is the mean credit:

| Class    | Credit                                                    | When                                                                                                 |
| -------- | --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| exact    | 1                                                         | all of one conflict's members, and nothing outside its members and `also`                             |
| partial  | 1                                                         | two or more of one conflict's members and nothing else, not all of them (the miss counts in recall)   |
| merged   | share of its statements in the conflict it holds most of  | statements of two or more conflicts and nothing else: two conflicts a person must settle as one      |
| extra    | the same share                                            | a conflict plus statements that take no side in any                                                  |
| false    | 0                                                         | no two of its statements are members of one conflict                                                 |

So two two-way conflicts merged score 0.5, and a two-way conflict with one bystander scores 2/3.

- **held** is the cost of a wrong group to a person: the share of the statements init would offer to keep in bulk (repository and person trust, in no planted conflict) that some group flags.
- A trap is **sprung** when one group holds two of its members.

## Results

### Before and after

The three changes, in order. Recall and precision are at the bar of 0.8, and latency is the whole import check, per import.

| Stage                                   | Set                  | Recall              | Precision            | Wrong groups | Held                | Check p50 / p95         | Cost a run    |
| --------------------------------------- | -------------------- | ------------------- | -------------------- | ------------ | ------------------- | ----------------------- | ------------- |
| Production (`57ac9ec`)                  | main, before `atlas` | 0.86 (38/44)        | 0.91                 | 4            | 7/232 (3.0%)        | 2.0 / 10.8 s            | $0.030        |
| Production                              | held out             | 1.00 (17/17)        | 0.89                 | 2            | 4/78 (5.1%)         | 1.3 / 1.4 s             | $0.004        |
| `suggestion` optional, cut at 120       | main, before `atlas` | 0.84 (37/44)        | 0.90                 | 5            | 9/232 (3.9%)        | 1.1 / 1.9 s             | $0.009        |
| One call an import, the prompt before   | main                 | 0.98, 1.00          | 0.92, 0.96           | 5, 3         | 9, 5 of 425         | 0.9 / 2.0–2.2 s         | $0.005–0.006  |
| One call an import, the prompt before   | held out             | 1.00, 1.00          | 0.94, 0.94           | 1, 1         | 2/78                | 0.8–1.1 / 1.0–1.6 s     | $0.002–0.003  |
| **The Oct 7 prompt, with headings**     | **main** (×4)        | **1.00 (54/54)** ×4 | **0.98** ×4          | 1 each       | 2/425 (0.5%)        | 1.0–1.2 / 1.8–2.2 s     | $0.005        |
| **The Oct 7 prompt, with headings**     | **held out** (×4)    | **1.00 (17/17)** ×4 | 0.94, 0.94, 1.00, 0.94 | 1, 1, 0, 1 | 2, 2, 0, 2 of 78    | 0.8–1.0 / 1.0–1.1 s     | $0.002–0.003  |

The production runs' main-set misses were all in `orbit`, the only import over 120 then:

- five were conflicts the cut split;
- one, `orbit-test-framework`, the model missed inside one call.

**`atlas` alone (215 proposals):**

| Batch size                | Recall        | Precision | Time  |
| ------------------------- | ------------- | --------- | ----- |
| 120, by section (2 calls) | 0.20 (2/10)   | 1.00      | 1.2 s |
| The whole import (1 call) | 1.00 (10/10)  | 1.00      | 4.6 s |

Its first batch was `AGENTS.md` alone, so every conflict across files fell across the cut.

### What changed, and why

**1. `suggestion` is optional** (`importSchemaRaw`):

- `IMPORT_EVAL_DEBUG` showed the primary's answers were right but left `suggestion` out.
- The check refused them as not matching the schema, asked again, got the same answer (temperature 0), and fell back to Haiku. Before the change, `pyflow` and `ferrous` took 3 calls each and 5.7–10.8 s.
- After it, in every recorded run, each import took exactly one call: no answer was refused.

**2. One call per import** (`ImportBatch = ledger.MaxImportItems`):

- A disagreement is between files, and an import holds at most 500 proposals.
- `TestCuttingAnImportLosesConflicts` keeps the evidence: cut at 120, the labels themselves find only 41 of 54.
- One call reads up to about 15k tokens. The primary answered over 215 proposals in 1.8–4.6 s.
- The fallback took up to 11.4 s over 142 proposals, against the judge's 12 s call timeout. So an import call now gets 40 s (`ImportCallTimeout`, four of which fit in the worker's 3 minutes) and at least 6,000 answer tokens (12 groups took 1,241).

**3. The prompt** (`importSystem`, and `under` in `importPrompt`). Tuned on `batches.json` only, against classes of error rather than items; each example in the prompt is in neither set.

- **What it now says:**
  - Some statements are facts about the developer (preferences, setup, schedule), and they disagree when both can't be true.
  - An exception for part of a general rule's places isn't a disagreement, but two rules for the same files are ("Indent with 4 spaces" and "Makefiles indent with tabs").
  - Places come from the applies attribute, the headings or the words.
- **What it now sends:** each statement's headings (`under`), from its source's locator, which init already fills (`ImportCandidate.Under`, `ImportCheckSnapshot`).
- **The steps, on `batches.json` at 0.8:**

  | Step                                                      | Recall    | Precision | What happened                                                                                                                         |
  | --------------------------------------------------------- | --------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------- |
  | Before                                                    | 0.96      | 0.96      | Three scope traps sprung; `zz-leave` came back at 0.70, `zz-code-first` not at all                                                    |
  | + exceptions, + facts about the developer                 | 1.00      | 0.99      | Personal confidences rose (`zz-leave` 0.70 → 0.90); the Jest statement under "Mobile" still joined the test-runner group               |
  | + headings                                                | 0.98      | 1.00      | The Mobile statement was right; `zz-code-first`, then ambiguous, was lost to its "Notes for this repo" heading                        |
  | + "the developer's own files are kept together"           | 0.98      | 0.98      | No gain, and one new wrong group: **reverted**                                                                                        |
  | The `zz-code-first` label rewritten; the prompt of row 3  | 1.00 (×4) | 0.98 (×4) | Final                                                                                                                                 |

- **Held out, before and after:** recall 1.00 in both, and precision 0.94 before and 0.94, 0.94, 1.00 and 0.94 after. The wrong group before was the `kai-scripts` scope trap in one run and the VPC date trap in another; after, only the VPC date trap was left (`harbor-vpc`).

### The bar

Pooled over the eight final runs (four of each set), 284 right groups and 16 wrong ones:

| Bar     | Recall          | Precision | Wrong groups | Held           | Traps sprung |
| ------- | --------------- | --------- | ------------ | -------------- | ------------ |
| 0–0.6   | 1.000 (284/284) | 0.947     | 16           | 32/2012 (1.6%) | 16/292       |
| 0.7     | 1.000           | 0.973     | 8            | 16 (0.8%)      | 8            |
| 0.75    | 1.000           | 0.973     | 8            | 16 (0.8%)      | 8            |
| **0.8** | **1.000**       | **0.976** | 7            | 14 (0.7%)      | 7            |
| 0.85    | 1.000           | 0.976     | 7            | 14 (0.7%)      | 7            |
| 0.9     | 0.972 (276/284) | 0.996     | 1            | 2 (0.1%)       | 1            |
| 0.95    | 0.616           | 1.000     | 0            | 0              | 0            |

- **Right groups** came back at 0.85 (7), 0.88 (1) and 0.90 or more (276).
- **Wrong groups** came back at 0.90 (1), 0.85 (6), 0.75 (1) and 0.60 (8).
- **Why 0.8:** no bar separates the two classes. 0.8 is the highest bar that keeps every conflict with a margin, since 0.85 gains nothing and has none. Lowering it lets in wrong groups and finds nothing more.
- **Before the prompt change**, real conflicts came back as low as 0.70 (`zz-leave`), or not at all, so the old prompt had no margin at 0.8.
- `ImportConflictBar` stays **0.8**.

### The bars

`TestLiveModel` asserts both on the pipeline over the whole of `batches.json`; the confirmation run passes.

- **Recall 1.00:** every planted conflict found (plan 25 §11).
- **Precision ≥ 0.95:** about two wrong groups in a run's 55.
  - The final prompt scored 0.98 in each of four runs; the prompt before it, 0.92 and 0.96.
  - A wrong group holds two statements out of bulk keep, about 0.5% of a run's 425. At 0.95 that is at most about 1.4%.
  - 0.90, the judge's own bar, would pass the old prompt's worse run.
  - On `holdout.json` (18 groups) one wrong group is 0.06, so the bar isn't asserted there.

### What it still gets wrong

- **Wrong groups in the final runs (all seven of them):**
  - The `any` carve-out for tests, at 0.85, in three of four main runs. The inputs were the same each time: at temperature 0 the answer still changed between runs.
  - `zz-kotlin`, "This repo's services are Kotlin; stick to Kotlin here" against "Prefer Go for new services", at 0.90 in the other main run.
  - Held out: "Since February 2026 every resource lives in `vpc-main`" against "The legacy VPC was retired on 2026-02-01", at 0.85 in three of four runs. That is the same fact told twice.
- **Below the bar:**
  - HTTP client timeouts against gRPC deadlines (0.75);
  - at 0.6: framework pages' default exports, a repository's scripts language, the ML heading's pip-tools against `scripts/`' uv, `pandas` against the streaming jobs' `polars`, a Lambda limit against API Gateway's, and the `any` carve-out once more.

### The fallback tier alone

Claude Haiku 4.5, strict, with the final prompt (one run of each set):

| Set       | Recall       | Precision | Wrong groups | Check p50 / p95 | Cost   |
| --------- | ------------ | --------- | ------------ | --------------- | ------ |
| main      | 0.96 (52/54) | 0.88      | 7            | 5.0 / 6.8 s     | $0.063 |
| held out  | 0.94 (16/17) | 0.89      | 2            | 4.6 / 6.1 s     | $0.018 |

- **It misses three conflicts the primary finds:** `orbit-deploy`, `orbit-test-framework`, and the held-out freeze dates.
- **It springs traps the primary doesn't:** macOS 15 against Sequoia, a repeated comment rule, and the anyhow/thiserror split.
- **It is used only when the primary fails twice**, and no final run needed it. With the old prompt, one call per import and before `atlas`, it scored 0.93 / 0.87.

### Latency and cost

| Calls (final runs)           | Proposals | Time                              |
| ---------------------------- | --------- | --------------------------------- |
| Primary, other imports       | 22–48     | 0.7–1.5 s                         |
| Primary, `orbit`             | 142       | 1.5–1.8 s                         |
| Primary, `atlas`             | 215       | 1.8–2.2 s (4.6 s once)            |
| Fallback, any                | 22–215    | 4.0–6.8 s (11.4 s once, over 142) |

- **Tokens:** a run of `batches.json` sends about 30k tokens in and takes about 3.9k out.
- **Prompt cache:** Together cached repeated prompts, so later runs billed far fewer input tokens. A main run cost $0.005–0.014.
- **Whole eval:** $0.32 by the meter, for 20 recorded runs and the probes between them. 138 primary calls cost about $0.14 and 25 fallback calls $0.18.

### Hosts

| Tier                     | Slug                           | Calls | Served by (printed summaries) | Routing asked                                    |
| ------------------------ | ------------------------------ | ----- | ----------------------------- | ------------------------------------------------ |
| Primary (prompted JSON)  | `deepseek/deepseek-v4.1-flash` | 138   | Together (48 of 48)           | zero retention, its pinned hosts, fp8 floor, t=0 |
| Fallback (strict)        | `anthropic/claude-haiku-4.5`   | 25    | Amazon Bedrock (23 of 23)     | zero retention, its pinned hosts, fp8 floor, t=0 |

- **The pins** are the judge's defaults (`anthropic.DefaultProviders`; the reasons are in `eval/judge/RESULTS.md`).
- **The routing checks run on every call**, as the judge eval runs them: zero retention asked and served, a pinned host, the precision floor, and the tier's structured output and temperature.
- **What "printed summaries" covers:** the summaries of the filtered runs weren't kept. The table counts only the calls whose host was printed.

## Follow-ups

- **An import is at most 500 statements.** `memax init` sends a space's statements in imports of 500, and each is checked alone. A repository with more than 500 statements has disagreements across imports that nothing checks.
- **Plan 25's Phase 2 log** says "one model call per 120 statements". It is now one call per import, and the doc should say so.
- **The fallback tier is weaker here** than as the judge's fallback (precision 0.88–0.89). No run fell back once `suggestion` was optional, so there's no case for changing the tier yet. Re-run `IMPORT_EVAL_TIERS=fallback` before relying on it.
- **Temperature 0 doesn't make long answers repeat.** The same inputs gave one borderline group (0.85) in three of four runs. Score a change over several runs, as here.
- **`CLAUDE.local.md` lands in Personal without its repository's scope**, so its unscoped statements meet the person's global ones. Init could scope them to the repository.
- **A statement that says "here"** keeps its meaning only through its heading, which the check now reads but a compiled file doesn't keep.
- **Grow the held-out set** past 17 conflicts. It is too small to assert a precision bar on.
