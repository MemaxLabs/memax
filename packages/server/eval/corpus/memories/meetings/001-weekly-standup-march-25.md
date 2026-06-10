Weekly standup March 25, 2026. Attendees: Jiahao, Ziyang, Sarah, Mira.

## Ziyang
- Finished the retrieval eval corpus scaffolding. Harness can now load fixture memories and run nDCG scoring against live retrieval.
- Started working on hub-scoped recall — the query planner now threads read_hub_ids through the embedding search.
- Blocker: Voyage AI rate limits hitting us on the eval runner. Need to add request batching or switch to a local embedding cache for tests.

## Sarah
- Shipped the consent screen redesign. OAuth approval page now matches the glass design language.
- Working on the settings page — hub management section is next.
- No blockers.

## Mira
- Deployed the River worker autoscaling config on Fly. Worker now scales 1-3 instances based on queue depth.
- Investigating memory leak in the dream engine — RSS grows ~50MB/hour under sustained load.
- Blocker: Need access to the production Fly dashboard metrics to debug the leak.

## Jiahao
- Reviewed and merged the config sync PR. Device-aware sync is live on staging.
- Writing the team hub invitation flow — backend handlers done, need frontend.
- No blockers, but flagged that we should do a retro before end of Q1.

## Action items
- Ziyang: batch Voyage API calls in eval runner by EOW
- Mira: file Fly support ticket for metrics access
- Jiahao: schedule Q1 retro for Friday March 28
