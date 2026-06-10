---
name: business
description: "Use when writing, revising, or reviewing business documents — pricing, GTM, competitive analysis, fundraising, cost analysis, growth strategy. ALWAYS trigger when the task involves internal-docs business files, pricing decisions, competitor analysis, growth projections, or investor-facing content. Ensures rigorous research, accurate numbers, cross-document consistency, and investor-grade quality. Numbers must have sources; projections must have assumptions."
---

# Memax Business Planning — Skill

Business documents live in a separate private repository at [`MemaxLabs/internal-docs`](https://github.com/MemaxLabs/internal-docs) (7 files). The convention is to clone it as a sibling of this monorepo so paths resolve as `../internal-docs/NN-*.md` from the monorepo root. Previously these files lived at `docs/business/` inside this monorepo; they were moved to keep business planning private and separate from the product codebase.

They must be accurate, internally consistent, investor-compelling, and grounded in real data — not aspirational fiction.

## Document Map

All paths below are relative to the sibling-clone location (`../internal-docs/`). If your working tree has a different layout, adjust accordingly — the filenames themselves are stable.

| File                          | Purpose                                               | Key Numbers                                 |
| ----------------------------- | ----------------------------------------------------- | ------------------------------------------- |
| `01-business-model.md`        | Pricing tiers, unit economics, revenue projections    | Free/Pro $9/Pro+ $19/Team $15/seat          |
| `02-go-to-market.md`          | Launch strategy, channels, phase targets              | User growth by phase                        |
| `03-competitive-landscape.md` | Every competitor, positioning, response playbook      | Funding, pricing, features per competitor   |
| `04-growth-engine.md`         | PLG loops, activation, conversion triggers, retention | Free tier math, conversion rates, K-factors |
| `05-partnerships.md`          | Platform partnerships, ecosystem strategy             | Tier 1-3 partners                           |
| `06-fundraising.md`           | Seed ask, pitch, milestone triggers, investor list    | $1.5-2.5M seed, $500K ARR target            |
| `07-cost-analysis.md`         | Per-operation costs, infra at scale, OpEx, margins    | COGS + OpEx per user per tier               |

## Before Writing or Revising

### 1. Research First, Write Second

Never write business claims without verifying them. For every number, ask: "Where does this come from?"

**Competitor data:**

- Check competitor websites for current pricing (pricing pages change quarterly)
- Check Crunchbase/PitchBook for funding amounts and dates
- Check GitHub for star counts and recent activity
- Check their docs/changelog for new features since last review
- Search for recent blog posts, tweets, or announcements

**Market data:**

- Cite sources for TAM/SAM numbers (CB Insights, Gartner, Stack Overflow surveys)
- Include the year and source for every market size claim
- If a number is an estimate, say so explicitly: "~$412M (estimated: 30M devs x 55% adoption x $25/yr)"

**Our own numbers:**

- Per-operation costs must match actual API pricing pages (Anthropic, Voyage AI, Fly.io, Neon, etc.)
- If API pricing changed, update ALL references across ALL docs (not just one file)
- Cross-check MRR calculations: (Pro count x $9) + (Pro+ count x $19) + (Team seats x $15) = stated MRR
- Verify ARR = MRR x 12

### 2. Internal Consistency Check

Before committing changes to ANY business doc, verify consistency across ALL 7 files:

**Pricing numbers must match everywhere:**

- Free tier limits: 300 memories, 200 pushes/mo, 500 recalls/mo, 10 asks/mo, unlimited agents
- Pro: $9/mo, Pro+: $19/mo, Team: $15/seat/mo
- These appear in: 01 (tiers), 03 (comparison table), 04 (conversion triggers), 07 (recommended tiers)
- If you change a number in one file, grep for it across all 7 and update every occurrence

**Growth projections must be consistent:**

- The same Month 12/18/24 numbers should appear in: 01 (revenue projections), 02 (phase targets), 06 (fundraising pitch)
- Conversion rates assumed in 01 must match those stated in 04
- MRR at each milestone in 06 must match the projection table in 01

**Cost numbers must flow correctly:**

- Per-operation costs in 07 feed into per-user costs in 01
- Per-user costs in 01 determine margins stated in 07
- OpEx in 07 determines burn rate in 06
- If ANY cost changes (e.g., Anthropic raises prices), cascade through 07 -> 01 -> 06

**Run this consistency check:**

```bash
# Grep for key numbers across all business docs
grep -n "300 memor\|500 recall\|200 push\|\$9/\|\$19/\|\$15/seat\|0\.65\|68%\|74%" ../internal-docs/*.md
```

### 3. Avoid These Mistakes

**Unenforceable limits:** Don't propose limits that can't be technically enforced. Example: "agent integrations: 2 agents" is unenforceable when we expose MCP and REST API — any agent can connect with any API key. Gate on measurable operations (recalls, pushes, asks), not on identity.

**Stale competitor data:** Competitors ship fast. Mem0's pricing, Zep's features, QMD's star count — all change. Date-stamp competitor data: "Mem0: $24M raised (Oct 2025)" so reviewers know when it was verified.

**Aspirational projections presented as plans:** "$1.2M ARR by Month 24" is a projection, not a commitment. Always label projections as such and state the assumptions underneath (conversion rate, growth rate, viral coefficient). Never present projections without assumptions.

**Inconsistent terminology:** Use the same terms everywhere. "Memories" not "notes" (we renamed). "Hubs" not "workspaces" in business docs (internal term). "Push" not "save" (API term). "Recall" not "search" (product term).

## When Writing New Content

### Competitive Analysis Standards

For each competitor, document:

1. **Funding** — amount, round, date, lead investors
2. **Team size** — approximate headcount
3. **Traction** — GitHub stars, stated user count, any public metrics
4. **Pricing** — full tier breakdown with limits
5. **Architecture** — how it works (local vs cloud, what DB, what models)
6. **Strengths** — be honest, not dismissive
7. **Weaknesses** — specific, not generic ("no team features" not "bad product")
8. **Our advantage** — concrete, not hand-wavy

Search for new competitors quarterly. The AI memory space is young — new entrants appear fast.

### Pricing Change Protocol

If changing ANY pricing (tier limits, prices, features per tier):

1. Model the cost impact: what does this cost us per user per month?
2. Model the revenue impact: how does this affect conversion and MRR?
3. Update ALL 7 docs (use grep to find every reference)
4. Update `.env.example` if the change affects rate limiting env vars
5. Update the server code if limits are enforced server-side

### Fundraising Content Standards

Investor-facing content must be:

- **Specific:** "$500K ARR in 18 months" not "significant revenue"
- **Grounded:** Show the math (user count x conversion rate x ARPU = MRR)
- **Honest about risks:** Include a "what could go wrong" section
- **Benchmarked:** Compare to similar-stage companies (Mem0, Supermemory, PostHog seed stage)
- **Capital-efficient:** Show burn rate and runway, not just the ask amount

### Growth Projection Standards

Every projection table must include:

1. **Assumptions section** — conversion rate, growth rate, viral coefficient, churn
2. **MRR calculation** — show the arithmetic for at least 3 rows
3. **Sensitivity analysis** — what if conversion is 2% instead of 3%?
4. **Inflection points** — call out when team virality kicks in, when enterprise starts

## After Writing

### Prettier Format

Always run prettier after editing business docs:

```bash
pnpm prettier --write "../internal-docs/*.md"
```

### Cross-Reference Audit

After any change to business docs, check that plan docs still reference correct information:

- `docs/plans/01-vision-and-strategy.md` references competitive positioning
- `docs/plans/07-team-hubs.md` references pricing tiers for hub conversion
- `AGENTS.md` lists all business doc descriptions

### Decision Logging

When a significant business decision is made (pricing change, new tier, revised projections):

1. Save to Memax: `memax_push` with category `decisions/business` and clear title
2. Include the rationale (why the change) and the data (what numbers drove it)
3. This ensures future sessions have context on business decisions

## Quality Bar

Before any business doc is committed, it must pass:

- [ ] Every number has a source or is clearly labeled as an estimate/projection
- [ ] MRR calculations are arithmetically correct
- [ ] Pricing is consistent across all 7 docs
- [ ] Growth projections have stated assumptions
- [ ] Competitor data includes date of last verification
- [ ] No "old vs new" comparison language (just state the current state)
- [ ] Prettier formatted
- [ ] No conflicts with information in other business docs or plan docs
