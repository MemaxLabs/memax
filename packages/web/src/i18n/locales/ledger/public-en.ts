// The public site (plan 25 epic 3.4): Landing, Pricing, Security (the
// Trust page) and Docs, from Landing.png, Pricing.png, Security.png and
// Docs.png. Mounted as `t.ledger.public`; ./public-zh.ts matches it key
// for key.
//
// Every claim here is true of the product today. What isn't built yet
// says so (pricing.coming), and the Security page's facts come from the
// server's own defaults (lib/v2/cloud-posture.json) in Settings ›
// Security's words. Company and model names live in settings-copy.ts
// NAMES, not here. The init transcript is what the CLI prints, and the
// CLI prints English, so its lines read the same in every language.

export const ledgerPublicEn = {
  meta: {
    landing: {
      title: "Memax: the context layer you own",
      description:
        "Memax keeps a living, cited record of what your agents learn and compiles it into Claude Code, Codex, Cursor and ChatGPT. Nothing is kept without a receipt.",
    },
    pricing: {
      title: "Pricing",
      description:
        "Free for one project. Every plan puts a receipt on every line, exports everything as Markdown and forgets for real.",
    },
    security: {
      title: "Trust and security",
      description:
        "What Memax keeps, where it keeps it, who else sees a memory's words, and how Forget lets go.",
    },
    docs: {
      title: "Quickstart",
      description:
        "Five minutes from the files your agents already keep to one Brief that all of them read.",
    },
    imageAlt: "Memax: one context, every agent",
  },
  site: {
    skip: "Skip to content",
    home: "Memax home",
    nav: {
      label: "Site",
      product: "Product",
      docs: "Docs",
      pricing: "Pricing",
      trust: "Trust",
      changelog: "Changelog",
    },
    signIn: "Sign in",
    getStarted: "Get started",
    footer: {
      label: "Footer",
      docs: "Docs",
      changelog: "Changelog",
      github: "GitHub",
      privacy: "Privacy",
    },
  },
  landing: {
    eyebrow: "Memax 2 · the context layer you own",
    titleLine1: "One context.",
    titleLine2: "Every agent.",
    lede: "Memax keeps a living, cited record of what your agents learn and compiles it into Claude Code, Codex, Cursor and ChatGPT. Nothing is kept without a receipt.",
    command: "The command that starts Memax",
    copy: "Copy command",
    copied: "Copied",
    copyFailed: "Couldn't copy. Select the command and copy it yourself.",
    howItWorks: "How it works",
    worksWith:
      "Free for one project. Works with Claude Code, Codex, Cursor, ChatGPT, Gemini CLI, Copilot and OpenCode.",
    example: {
      label: "An example space",
      counts: "{kept} kept · {waiting} waiting",
      river: "Background jobs run on River, not Temporal.",
      errors: "API errors are RFC 9457 problem+json.",
      confirm: "MCP write tools must ask for confirmation with input_required.",
      keptDate: "Oct 5",
      oct2: "Oct 2",
      sep12: "Sep 12",
      hourAgo: "1 h ago",
    },
    dayOne: {
      eyebrow: "Day one",
      title: "Your agents already disagree. Fix it once.",
      body: "One command reads every CLAUDE.md, AGENTS.md, Cursor rule and agent memory in the repo. It folds what repeats, shows you where they contradict each other, and writes one Brief back into every tool.",
      personal:
        "What your agents learned about you on this laptop goes to your Personal space, which follows you to every machine you link.",
      stats: {
        label: "What that run found",
        files: "files found",
        conflicts: "conflicts to settle",
        brief: "Brief, every tool",
      },
      transcript: {
        title: "~/code/memax · zsh",
        command: "npx memax-cli init",
        reading: "Reading what your agents already know",
        claude: "  CLAUDE.md                42 lines  →  18",
        agents: "  AGENTS.md                27 lines  →   9",
        cursor: "  .cursor/rules/*.mdc       3 files  →   6",
        codex: "  ~/.codex/memories         9 notes  →   7",
        folded: "  7 duplicates folded · 2 secrets skipped",
        testCommand: "Test command: pnpm test ≠ npm run test ≠ pnpm vitest run",
        packageManager: "Package manager: pnpm ≠ npm in /tools",
        deploy: "API deploy: Fly.io ≠ Railway",
        summary:
          "6 files, 3 conflicts, 1 Brief. Nothing kept until you say so.",
        personal: "Personal: 37 things your agents learned on this laptop.",
      },
    },
    how: {
      eyebrow: "How it works",
      title: "Kept once. Read natively, everywhere.",
      compiled: "Compiled into each tool's own file",
      live: "Live over MCP",
      inSync: "In sync",
      overConnector: "Live over connector",
      projectInstructions: "Project instructions",
      readsAndProposes: "reads and proposes",
    },
    principles: {
      label: "Principles",
      write: {
        title: "Write once. Every agent knows.",
        body: "A decision made in one tool shows up in the next, in that tool's own format. No more re-explaining your stack on Monday.",
      },
      review: {
        title: "Review before it remembers.",
        body: "Agents propose; you keep. Every memory shows who wrote it, when and from what, and anything from the open web waits for you.",
      },
      forget: {
        title: "Forget for real.",
        body: "Forgetting rewrites every compiled file and tells every agent. The redaction stays, so you can see it happened.",
      },
    },
    cta: {
      title: "Start with the files you already have.",
      start: "Get started free",
      pricing: "See pricing",
    },
  },
  pricing: {
    eyebrow: "Pricing",
    titleLine1: "Free for one project.",
    titleLine2: "Fair when it grows.",
    lede: "Every plan puts a receipt on every line, exports everything as Markdown and forgets for real. You pay when Memax spans more projects, your cloud agents or a team.",
    alpha:
      "Memax is free during the alpha, and billing isn't open yet. These are the plans it will offer.",
    period: {
      label: "Billing period",
      monthly: "Monthly",
      yearly: "Yearly",
    },
    plans: {
      free: { name: "Free", audience: "Trying it on one project" },
      pro: { name: "Pro", audience: "Developers who run several agents" },
      team: { name: "Team", audience: "Teams of 3 to 30" },
      enterprise: {
        name: "Enterprise",
        audience: "When the team plan is not enough",
      },
    },
    price: {
      custom: "Custom",
      month: "a month",
      year: "a year",
      seatMonth: "a seat, a month",
      seatYear: "a seat, a year",
      monthsFree: "{price} a month, billed yearly: {n} months free",
      discount: "{price} a seat a month, billed yearly: {n}% off",
      seats:
        "Members who keep are seats. Viewers are free, with no seat minimum.",
    },
    features: {
      adapters: "Compiles into every tool's own file",
      spaces: "{personal} personal and {project} project space",
      agentKinds: "Up to {n} agents, counted by kind",
      review: "Review, receipts and every autonomy level",
      dreamWeekly: "Dream, every week",
      askMonthly: "Ask, {n} a month",
      activityDays: "{n} days of activity",
      unlimited: "Unlimited spaces and agents",
      cloudAgents: "Cloud and background agents",
      dreamNightly: "Dream, nightly on your {spaces} busiest spaces",
      staleWatched: "Stale detection that watches your repository",
      handoffsGates: "Handoffs and gates between your own agents",
      viewers: "{n} viewers per space, free",
      everythingInPro: "Everything in Pro",
      teamSpaces: "Team spaces and roles",
      decisionLedger: "The decision ledger, with ADR export",
      routing: "Gates and handoffs routed to teammates",
      integrations: "Slack, Linear and webhooks",
      auditYear: "A year of audit history",
      sso: "SSO and SCIM",
      euResidency: "Data residency in the EU",
      selfHosting: "Supported self-hosting",
    },
    coming: "Coming",
    actions: {
      sign_in: "Start free",
      coming: "Coming soon",
      contact: "Talk to us",
    },
    closed: {
      pro: "Billing isn't open yet.",
      team: "Team spaces and billing aren't open yet.",
    },
    every: {
      label: "On every plan",
      receipts: {
        title: "A receipt on every line",
        body: "Who wrote it, when and from what, on every plan.",
      },
      export: {
        title: "Everything exports as Markdown",
        body: "With receipts and sources in frontmatter. Leave whenever you like.",
      },
      forget: {
        title: "Forget reaches every copy Memax holds",
        body: "Every compiled file is rewritten and every agent is told.",
      },
    },
    faq: {
      title: "Questions",
      replace: {
        q: "Does Memax replace Claude's or ChatGPT's memory?",
        a: "No. It feeds them, through their own files and connectors. Built-in memory stays for chat; Memax holds what every agent should know.",
      },
      agent: {
        q: "What counts as an agent?",
        a: "Any tool connected to a space, counted by kind: Claude Code, Codex, Cursor, ChatGPT, Gemini CLI, Copilot, OpenCode or any other MCP client. The same agent on two machines counts once, and a tool that only reads the compiled files doesn't count.",
      },
      store: {
        q: "What does Memax store?",
        a: "The statements you keep and the proposals that wait for you, with their receipts and links to their sources. Secrets are refused before they're stored.",
      },
      leave: {
        q: "Can I leave?",
        a: "Yes. Export everything as Markdown with its receipts at any time, on every plan, and check the export on your own machine.",
      },
    },
  },
  security: {
    eyebrow: "Trust",
    titleLine1: "What Memax keeps,",
    titleLine2: "and how it lets go.",
    lede: "A memory that agents can write to is something an attacker can write to. These are the rules Memax runs by, on every plan.",
    rules: {
      label: "The rules",
      receipts: {
        title: "Every write has a receipt.",
        body: "Agent, session, time and source, or the write is refused. Any line an agent reads traces back to who wrote it.",
        where: "In the product: the receipt rail on every memory",
      },
      propose: {
        title: "Agents propose. People keep.",
        body: "Each agent reads, proposes or writes, as you decide. Nothing an agent proposes reaches another agent until a person keeps it.",
        where: "In the product: Agents, and Review",
      },
      outside: {
        title: "What comes from outside waits.",
        body: "Anything an agent read on a web page, in an email or in an issue arrives quarantined, whatever that agent may otherwise do. This is the defence against planted memories, which the OWASP Top 10 for agentic applications calls memory and context poisoning (ASI06).",
        where: "In the product: the quarantine notice in Review",
      },
      forget: {
        title: "Forget reaches every copy Memax holds.",
        body: "Forgetting rewrites every compiled file within a minute and tells every agent on its next read. A tombstone stays, so you can see it happened.",
        where: "In the product: the forgotten memory's page",
      },
      secrets: {
        title: "Secrets never land.",
        body: "Imports and writes are scanned, and keys or tokens are refused before they're stored. The day-one cleanup shows you what it skipped.",
        where: "In the product: “2 secrets skipped”",
      },
      leave: {
        title: "Everything leaves with you.",
        body: "Export a space's whole record as Markdown, each memory with its receipts and sources in frontmatter, at any time and on every plan.",
        where:
          "In the product: Memories › Export as Markdown, and memax export",
      },
    },
    data: {
      title: "Your data",
      what: "What",
      where: "Where",
      howLong: "For how long",
      cloud: "Memax cloud: {place}",
      memories: {
        what: "Memories, receipts and links to sources",
        howLong: "Until you forget them. Receipts stay, without the words.",
      },
      notes: {
        what: "Raw notes from agents",
        where: "Memax cloud",
        howLong:
          "Until you forget them. Dream folds them into proposals for you to review.",
      },
      compiled: {
        what: "Compiled files",
        where: "Your repository or machine, and each compile in {place}",
        howLong: "They're yours. A Forget rewrites them and Memax's copies.",
      },
      reads: {
        what: "Agents' reads",
        where: "Memax cloud",
        howLong:
          "13 months, as counts. A read never holds a memory's or a query's words.",
      },
      code: {
        what: "Your code",
        where: "Not stored",
        howLong:
          "Memax keeps file paths and commits as sources, never your source code.",
      },
      backups: {
        what: "Database backups",
        howLong:
          "{days} days. Memax applies every Forget again after any restore.",
        howLongOne:
          "1 day. Memax applies every Forget again after any restore.",
      },
      runs: "Where Memax runs",
      certifications:
        "Memax holds no third-party certification, such as SOC 2, yet.",
    },
    processors: {
      lede: "Only these services, and only while the feature they serve is on. Every model call goes through OpenRouter's zero-data-retention routing, pinned to the hosts listed at fp8 precision or better, and never to DeepSeek's own API.",
      source:
        "These are the server's defaults, which Memax cloud runs on. Signed in, Settings › Security reads them from the running server.",
      original:
        "Spaces still on the original record go through its own pipeline, which calls OpenRouter without this routing and may rerank with Cohere. Switching a space to the V2 record moves it onto the routing above.",
    },
    forget: {
      lede: "Forget takes the words out of everything Memax holds. Some copies are out of its reach, and every tombstone names them.",
    },
    check: {
      title: "Check it yourself",
      receipts: {
        title: "Sealed receipts",
        body: "Each space's receipts form a hash chain. Every 10 to 60 seconds Memax seals the newest into a checkpoint signed with Ed25519, and each night it recomputes every chain from its first receipt.",
      },
      export: {
        title: "Export and verify",
        body: "Take a space's whole record, with its receipts and signed checkpoints, and check it on your own machine without trusting Memax.",
      },
      passkeys: {
        title: "Passkeys",
        body: "Add a passkey, and the decisions that need a person ask for it again: keeping what came from outside, letting an agent do more, and every Forget. A copied browser session can't make them.",
      },
      sessions: {
        title: "Sessions",
        body: "Refresh tokens are stored only as a hash and change on every use. An old one used again signs its session out.",
      },
    },
    report: {
      title: "Found a problem?",
      body: "Write to {email}. We reply within two business days.",
      docs: "Read how Memax works",
    },
  },
  docs: {
    header: {
      label: "Docs",
      search: "Search the docs",
      matches: "{n} pages match",
      matchesOne: "1 page matches",
      noMatch: "No page matches. The full docs are at {site}.",
      open: "Open Memax",
    },
    tree: {
      label: "Docs",
      start: "Get started",
      agents: "Agents",
      concepts: "Concepts",
      reference: "Reference",
      quickstart: "Quickstart",
      install: "Install the CLI",
      personal: "Your Personal space",
      claudeCode: "Claude Code",
      plugin: "Claude Code plugin",
      codex: "Codex",
      cursor: "Cursor",
      cloud: "Cloud agents",
      otherAgents: "Any MCP client",
      memories: "Memories",
      dream: "Dream",
      security: "Security",
      exportFormat: "Export format",
      cli: "CLI",
      init: "memax init",
      compile: "memax compile",
      connect: "memax connect",
      mcp: "MCP server",
    },
    eyebrow: "Get started",
    title: "Quickstart",
    lede: "Five minutes from the files your agents already keep to one Brief that all of them read.",
    init: {
      title: "1. Read what your agents already know",
      body: "Run this at the root of a repository. It finds your agents and their files, and imports everything as proposals. Nothing is kept yet.",
    },
    settle: {
      title: "2. Settle conflicts, then review",
      body: "Where two files disagree, init asks you once, right in the terminal. Then keep what's true: all at once there, or one at a time in Review on the web.",
    },
    compile: {
      title: "3. Compile into every tool's own file",
      body: "Memax writes AGENTS.md, a CLAUDE.md that imports it, and any other file you turn on, from what you kept. It compiles again after every change, and memax compile asks for one now. Commit the files, so every clone and cloud agent reads them too.",
    },
    cloud: {
      title: "4. Connect cloud agents",
      body: "Agents that run away from your machine read the same context live over MCP. They sign in with OAuth and ask you which space to use.",
      start:
        "Claude Code's cloud sessions and Copilot's cloud agent can also start with what changed since the files they loaded: memax connect --cloud adds a start key and a hook.",
    },
    tools: {
      title: "MCP tools",
      tool: "Tool",
      does: "What it does",
      memax_recall:
        "What this agent should know: the kept memories for a query, or each space's digest without one",
      memax_search: "Search kept memories and decisions, and notes when asked",
      memax_get: "One memory with its receipts and sources",
      memax_list: "Kept memories, newest first, a page at a time",
      memax_push:
        "Propose a memory. A person keeps it, in the agent or in Review",
      memax_capture: "Session notes, which Dream folds into proposals",
      memax_request_decision: "Ask a person to decide, and get the answer back",
      memax_forget: "Ask to forget a memory. A person confirms.",
      memax_hubs: "The spaces this agent can read, with its autonomy in each",
      memax_hub_members: "A space's members and their roles",
      memax_topics: "A space's sections and their kept memories",
    },
    note: "An agent's writes are proposals unless you set it to Write. Content it read on the web is always a proposal.",
    next: "Next",
    toc: {
      label: "On this page",
      init: "Read what agents know",
      settle: "Settle and review",
      compile: "Compile",
      cloud: "Cloud agents",
      tools: "MCP tools",
    },
  },
};
