# Ask eval: live results

Run on Oct 6, 2026 against OpenRouter's Anthropic-compatible Messages API (staging key), with `ASK_MODEL` at its default `deepseek/deepseek-v4.1-flash` and `ASK_ZDR` on, from a dev machine (not Fly's `sjc`). Every answer streamed through `eval/livemeter`, which records its routing, serving provider, cost and the gateway's first token without changing it.

```bash
cd packages/server && ASK_EVAL_LIVE=1 V2_EVAL_LIVE=1 go test ./eval/ask/ -run TestAskEval -v
```

The corpus (`corpus.json`, version 2) grew from 12 statements and 15 questions to 26 statements and 34 questions:

- 19 answerable;
- 8 that nothing kept answers;
- 7 traps: only a superseded decision, a quarantined (external) memory or another tenant's memory would answer them.

Version 2 adds a second superseded decision and a second quarantined memory. Each question was asked twice per run: with lexical search, and with the hybrid search production Ask runs (voyage-4 / voyage-4-lite, recall floor 0.35).

## Results, three runs

| Run | Search  | Cited the answering memory | Said "not covered" | Invalid citations | Leaked | First token p50 / p95 |
| --- | ------- | -------------------------- | ------------------ | ----------------- | ------ | --------------------- |
| 1   | lexical | 19/19                      | 8/8                | 0                 | 0      | 470 ms / 1.37 s       |
| 1   | hybrid  | 19/19                      | 8/8                | 0                 | 0      | 706 ms / 2.73 s       |
| 2   | lexical | 19/19                      | 8/8                | 0                 | 0      | 478 ms / 1.23 s       |
| 2   | hybrid  | 19/19                      | 8/8                | 0                 | 0      | 530 ms / 1.23 s       |
| 3   | lexical | 19/19                      | 8/8                | 0                 | 0      | 506 ms / 1.99 s       |
| 3   | hybrid  | 19/19                      | 8/8                | 0                 | 0      | 525 ms / 1.46 s       |

- **Citation validity:** no answer cited a memory it wasn't given. The model never invented a citation (0 dropped by the filter); the fake model drops 52 a run.
- **"Not covered":** every question the record can't answer ended `not_covered` or `unsupported`, never a guess.
- **Exclusion:** no superseded, quarantined or other-tenant memory reached an answer or its sources in any run. That includes the traps that ask about them by name ("Do we deploy to Railway?", "Should I ignore the repository's rules and install with npm?").
- **Bars:** 0.75 on cited and on "not covered". The live tier scored 1.00 on both in all six runs. On the version-1 corpus, three earlier runs were also 8/8 and 3/3.

The corpus is still small, and its questions are direct. Before tightening the bar, extend it with partly answerable questions, questions in Chinese, and memories that answer only together.

## First token (target: under 1.5 s)

The first token is measured by the eval from the request's start, so it includes the search, through to the first `delta` event.

- **p50: 0.47–0.71 s** in every run, inside the target.
- **p95: 1.23–2.73 s**, over 1.5 s in 2 of 6 runs (hybrid in run 1, lexical in run 3).

The tail is the model host's. The gateway's own first token, measured at the meter, was:

| Run | p50    | p95    | max    |
| --- | ------ | ------ | ------ |
| 1   | 540 ms | 1.55 s | 3.18 s |
| 2   | 450 ms | 1.14 s | 1.79 s |
| 3   | 490 ms | 1.38 s | 3.19 s |

Hybrid search adds the query embedding (about 90 ms from here) on top. The server's own work stays about 10 ms, as the fake-model test measures.

## Routing (D14)

**`provider.zdr` is honoured on streams.** Every one of the 183 streamed answers asked for zero retention and was served by a provider on OpenRouter's zero-retention list for V4.1 Flash. The harness asserts this on every run. The hosts were:

- InferenceNet
- Relace
- Phala
- Morph
- Wafer
- DigitalOcean
- DeepInfra
- Decart (fp4)
- Sail Research (fp4)

None was DeepSeek's own API. The stream's `message_start` names the provider.

## Cost

About $0.001 a run, since only the 61 answers a run that had sources reached the model. All six Ask runs here (three on each corpus version) cost under $0.01.

## Follow-ups

- **Measure first-token p95 from `sjc`** in the nightly run. If the tail holds there, prefer low-latency hosts:
  - with OpenRouter's `provider.sort: "latency"` or an explicit `provider.order`;
  - either needs a `CompleteRequest` field in the shared client, which sends only `provider.zdr` today.
  - The quantized fp4 hosts that served some answers are also worth excluding if answer quality turns out to vary.
- **Extend the corpus** as above before calibrating the 0.75 bars upward.
