---
name: business
description: "Use when writing, revising, or reviewing business documents — pricing, GTM, competitive analysis, fundraising, cost analysis, growth strategy. ALWAYS trigger when the task involves internal-docs business files, pricing decisions, competitor analysis, growth projections, or investor-facing content. Ensures rigorous research, accurate numbers, cross-document consistency, and investor-grade quality. Numbers must have sources; projections must have assumptions."
---

# Memax Business Planning — Skill

Business documents live in a separate private repository at [`MemaxLabs/internal-docs`](https://github.com/MemaxLabs/internal-docs) (7 files). The convention is to clone it as a sibling of this monorepo so paths resolve as `../internal-docs/NN-*.md` from the monorepo root. Previously these files lived at `docs/business/` inside this monorepo; they were moved to keep business planning private and separate from the product codebase.

They must be accurate, internally consistent, investor-compelling, and grounded in real data — not aspirational fiction.

## Document Map

All paths below are relative to the sibling-clone location (`../internal-docs/`). If your working tree has a different layout, adjust accordingly — the filenames themselves are stable.

| File                          | Purpose                                                                                   | Key Numbers                                                                                                                  |
| ----------------------------- | ----------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `01-business-model.md`        | Scope and sizing, plans, value metric, unit economics, projection                         | Free $0 / Pro $12 or $120/yr / Team $20/seat or $192/yr / Enterprise custom; founding $96/yr; Month 24 base case $37,210 MRR |
| `02-go-to-market.md`          | `npx memax-cli init` wedge, launch phases and gates, distribution order                   | Phases 0–4 and their gates; active free users by month (projection)                                                          |
| `03-competitive-landscape.md` | Platform memory, memory APIs, cross-tool MCP memory, translators; positioning             | Dated funding, pricing and capabilities per competitor (Oct 2026)                                                            |
| `04-growth-engine.md`         | North star, funnel targets, loops, upgrade triggers, retention                            | Activation 60%, week-4 retention 40%, team pull 25% in 60 days, conversion 5% in 90 days                                     |
| `05-partnerships.md`          | Directories and registries, agent platforms, AAIF record schema                           | The 8-step distribution order (master plan §4.5)                                                                             |
| `06-fundraising.md`           | Stage, seed ask, raise triggers, burn, pitch, risks                                       | $1.5–2.5M seed; ~$730/mo pre-seed burn; ~$39–45K/mo post-seed gross burn                                                     |
| `07-cost-analysis.md`         | Unit prices, per-operation costs, COGS per tier, fixed infra, payments, OpEx, sensitivity | Pro $1.14 variable / $2.03 total / 83%; Team seat 84–86%; Free $0.10–0.21 variable; ~$650 fixed at 10k MAU                   |

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
- If a number is an estimate, say so explicitly: "~$263M/yr (estimate: ~2.19M developers likely to buy a second, self-paid AI tool x $120/yr)"

**Our own numbers:**

- Per-operation costs must match actual API pricing pages (OpenRouter and the zero-data-retention hosts, Anthropic, OpenAI, Voyage AI, Fly.io, Neon, Upstash, R2, Stripe, etc.)
- If API pricing changed, update ALL references across ALL docs (not just one file)
- Cross-check MRR calculations: (founding Pro x $8) + (list Pro x $11.20 blended ARPU) + (discounted Team seats x their price) + (Team seats x $18 blended ARPU) = stated MRR. The blends are 60% monthly / 40% annual for Pro ($12 / $10) and 50/50 for Team ($20 / $16)
- Verify ARR = MRR x 12

### 2. Internal Consistency Check

Before committing changes to ANY business doc, verify consistency across ALL 7 files:

**Pricing numbers must match everywhere** (master plan D9, accepted Oct 6, 2026):

- Free $0: every compile adapter, 1 personal + 1 project Space, 3 agent kinds, Write autonomy, Dream weekly, Ask 50/month, 30-day activity
- Pro $12/mo or $120/yr: unlimited Spaces and agents, cloud agents, Dream nightly on the 5 busiest Spaces, source-watched stale detection, handoffs and gates between your own agents, 3 free viewers, 90-day activity, Ask 1,000/month
- Team $20/seat/mo or $192/seat/yr: members billed, viewers free, no seat minimum, 14-day trial, decision ledger + ADR export, routing to teammates, Slack/Linear/webhooks, 1-year audit history
- Enterprise custom: SSO/SCIM, EU residency, supported self-hosting (indicative floor $30/seat, 20 seats, an estimate)
- Launch: alpha free; founding $96/yr, held while subscribed, until GA or the first 1,000 subscribers; V1 `early_access` gets Pro free until 3 months after GA, then $96/yr
- Programmes: students Pro free; Team free for public OSS repos; startups 50% off Team for year one
- COGS (07): Free $0.10 / $0.21 variable; typical Pro $1.14 variable, $2.03 total, 83%; heavy Pro $5.51; Team seat $1.69 variable, 84–86%; fixed infra ~$650 at 10k MAU
- These appear in: 01 (plans, unit economics, projection), 02 (launch phases), 03 (pricing comparison), 04 (free tier, upgrade triggers), 06 (projection, pitch), 07 (COGS, margins)
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
# 1. Key numbers: each should appear in the files listed above, with the same value
grep -n '\$12\b\|\$120\|\$20/seat\|\$192\|\$96\|3 agent kinds\|1 personal + 1 project\|Ask 50\|\$1\.14\|\$2\.03\|83%\|\$5\.51\|\$1\.69\|\$37,210' ../internal-docs/*.md

# 2. Retired numbers: every hit must be a competitor's price or a dated market fact, never a Memax price or limit
grep -n '\$9/\|Pro+\|\$15/seat\|300 memor\|200 push\|500 recall\|10 asks\|unlimited agents\|0\.65\|68%\|74%\|Haiku only\|seat minimum of 3\|minimum 3 seats' ../internal-docs/*.md
```

Expected hits in grep 2 today: Basic Memory's "$15/seat" (03) and the stride.page "74%" repo statistic (02, 03, 04).

### 3. Avoid These Mistakes

**Soft limits and load-bearing gates:** A soft limit is acceptable as an upgrade prompt when circumventing it costs us nothing and degrades the circumventer's own product. Example: the Free plan's 3-agent cap counts distinct agent kinds (claude-code, codex, cursor, chatgpt, …); the same agent on two laptops counts once, API keys count, and tools that only read compiled files don't. Sharing one key across Cursor and Codex dodges the cap, but it mislabels every receipt, collapses per-agent autonomy and breaks cross-agent read counts, while a read costs us about $0.00002. So the agent cap is a fence, not a wall. **Load-bearing gates** (the ones revenue depends on: Spaces, nightly Dream, source-watched stale detection, member seats, Ask caps) must be enforced server-side, through the plan rows and `policy.Decide`. Never make a soft limit load-bearing (for example an agent ladder of 3 / 10 / unlimited), and never gate reads, compiles, export or Forget.

**Stale competitor data:** Competitors ship fast. Mem0's pricing, Zep's features, QMD's star count — all change. Date-stamp competitor data: "Mem0: $24M raised (Oct 2025)" so reviewers know when it was verified.

**Aspirational projections presented as plans:** "$446.5K ARR by Month 24" is a projection, not a commitment. Always label projections as such and state the assumptions underneath (conversion rate, growth rate, viral coefficient). Never present projections without assumptions.

**Inconsistent terminology:** Use the V2 nouns everywhere, the same ones the product, CLI and MCP use:

- **Space** (personal, project or team), not "hub" or "workspace". "Hub" appears only when naming a V1 table or plan.
- **Memory**: one reviewed statement with a state, a receipt and sources. A **note** is raw captured text (V1 memories become notes).
- **Propose** (what agents do) and **Keep** (what a person does), not "push", "save" or "approve". `memax_push` is a V1 tool name that now proposes.
- **Review** (the queue of proposals), **Brief** (one per Space, the readable face of its kept memories), **Dream** (the weekly or nightly edition), **Receipt**, **Handoff**, **Decision gate**, **Compile** and **target**, **Verify**, **Forget** (never "delete").
- **Ask** is the cited answer; recall and search are MCP reads.
- **Member** (billed, can keep) vs **viewer** (free); **agent kind** for the agent count.
- When quoting product copy, follow the V2 copy rules in `AGENTS.md`: sentence case, and no "AI", "magic", "smart" or "delete".

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
5. Update the server plan rows and `policy.Decide` if limits are enforced server-side

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

- `docs/plans/25-memax-v2.md` (branch `v2` of `memax-internal`): D9 pricing, §4 positioning, §5.17 cost model and §12 phase gates
- `docs/v2/research/pricing-2026-10.md`: the source for plans, COGS and sensitivity
- `docs/plans/01-vision-and-strategy.md` references competitive positioning (V1)
- `docs/plans/07-team-hubs.md` references pricing tiers for hub conversion (V1)
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
