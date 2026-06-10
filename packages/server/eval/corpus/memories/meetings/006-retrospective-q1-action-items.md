Q1 2026 Retrospective — March 28, 2026. Facilitated by Jiahao. Attendees: Ziyang, Sarah, Mira, Jiahao.

## What went well
- Shipped hybrid search (pgvector + pg_trgm + RRF) ahead of schedule. Retrieval accuracy improved ~15% on short keyword queries.
- OAuth MCP flow is live and working with Claude Code, Cursor, and Windsurf. Three external users connected successfully in the first week.
- CLI reached 200 npm downloads in Q1 without any marketing push. Organic growth from Claude Code integration.
- Config sync feature got positive feedback from beta testers. "Finally my CLAUDE.md follows me across machines" — direct quote from a user.
- Dream engine v1 shipped. Background knowledge synthesis is running on the worker without blocking the API server.

## What to improve
- **Deploy process is too manual.** Staging deploys require SSH into Fly machines to run migrations. Need to automate migration execution in the deploy pipeline. Owner: Mira.
- **PR review turnaround.** Average review time crept to 48 hours in March. Agreed on a 24-hour SLA going forward. Owner: everyone.
- **Test coverage gaps.** Retrieval pipeline has no integration tests — we only caught the boundary leak bug because of a manual spot check. Owner: Ziyang.
- **Documentation drift.** AGENTS.md and design docs have multiple stale sections. Need a monthly review cadence. Owner: Jiahao.
- **On-call rotation missing.** No one is formally on-call for production issues. Two outages in March were caught by chance. Owner: Jiahao to set up PagerDuty rotation.

## Action items
1. Ziyang: add retrieval integration tests using eval corpus by April 10
2. Mira: automate Fly migration in CI pipeline by April 7
3. Jiahao: set up PagerDuty on-call rotation by April 4
4. Sarah: schedule monthly docs review starting April 15
5. Everyone: 24-hour PR review SLA starts immediately
