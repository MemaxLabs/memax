// Settings › Notifications (Notifications.png) and Settings › Security,
// mounted as `t.ledger.settings`. Board copy is word for word; the rows
// and panels no board draws say plainly what the server says.
// ./settings-zh.ts matches it key for key.
export const ledgerSettingsEn = {
  notifications: {
    title: "Notifications",
    lede: "Memax only interrupts you for things waiting on you. Everything else waits in Today.",
    label: "When each event reaches you",
    columns: {
      when: "When",
      inApp: "In app",
      email: "Email",
      phone: "Phone",
      slack: "Slack",
    },
    // A checkbox's name: the row, then the column.
    cell: "{event}: {channel}",
    inAppOn: "In app, always on",
    notYet: "not available yet",
    events: {
      decision_gate: {
        title: "An agent asks you to decide",
        meta: "Decision gates, and questions in a handoff",
        short: "decision gates",
      },
      morning_edition: {
        title: "The morning edition",
        meta: "What Dream changed overnight, at {time}",
        metaReady: "What Dream changed overnight, as soon as it's done",
        short: "the morning edition",
      },
      review_waiting: {
        title: "Proposals waiting a day",
        titleDays: "Proposals waiting {n} days",
        meta: "Once a day, never per proposal",
        short: "proposals waiting",
      },
      drift: {
        title: "A compiled file drifted",
        meta: "Someone edited a file Memax writes",
        short: "drift",
      },
      stale: {
        title: "Something you kept went stale",
        meta: "Its source changed",
        short: "stale memories",
      },
      write_held: {
        title: "A write was refused or held",
        meta: "No receipt, or content from the web",
        short: "refused or held writes",
      },
      weekly_summary: {
        title: "Weekly summary",
        meta: "Mondays: kept, rejected, forgotten, read",
        short: "the weekly summary",
      },
      forget_done: {
        title: "A Forget finished",
        meta: "Every file rewritten and every agent told",
        short: "finished Forgets",
      },
      agent_changed: {
        title: "An agent's connection changed",
        meta: "Connected, paused, disconnected, or allowed more or less",
        short: "agent connections",
      },
    },
    // Under the table: what email goes out today.
    sentNow:
      "Memax emails {list} today. It keeps your other choices and follows them as each email arrives.",
    sentNone:
      "This server sends no email yet. Memax keeps your choices for when it does.",
    channelsLater: "Phone and Slack aren't available yet.",
    quiet: {
      title: "Quiet hours",
      zone: "In {zone}",
      zoneDefault: "In UTC, until Memax learns your time zone",
      changeZone: "Change the time zone",
      from: "From",
      until: "Until",
      gates: "Decision gates still reach me",
      off: "Off. Set both times to hold email overnight.",
      invalid: "Use a 24-hour time, such as 08:00",
      same: "End at a different time than you start",
    },
    clash:
      "Your settings changed elsewhere, perhaps through an unsubscribe link. They're reloaded, so make your change again.",
    failed: "That didn't go through. Check your connection and try again.",
    loading: "Loading your notification settings",
    loadFailed: "Your notification settings didn't load.",
    retry: "Try again",
  },
  security: {
    title: "Security",
    lede: "What Memax keeps for you, where it keeps it, and what it can't reach. This page reads it from the server's own settings.",
    loading: "Loading what this server says",
    loadFailed: "The security details didn't load.",
    retry: "Try again",
    receipts: {
      title: "Receipts",
      meta: "Each space's chain",
      label: "Each space's seal",
      signedWith: "Signed with key {key}.",
      nothing: "Nothing sealed yet.",
      noSpaces:
        "None of your spaces is on the V2 record yet, so there are no receipts to seal.",
      loading: "Reading the seal",
    },
    export: {
      title: "Export and verify",
      body: "Take a space's whole record, with its receipts and signed checkpoints, and check it on your own machine without trusting Memax.",
      note: "verify-export checks every file, recomputes the chain from the first receipt and checks each signature. It trusts the keys you pass with --key, else the server's published keys.",
    },
    forget: {
      title: "What Forget reaches",
      reaches: "It reaches",
      out: "Out of Memax's reach",
      reach: {
        words:
          "Every copy of the words in Memax: each version, its sources, the receipts' reasons, its search entries and Memax's checks",
        files: "Every compiled file, rewritten within a minute",
        agents:
          "Every agent that read it or is connected to the space, told on its next read",
        tombstone:
          "A tombstone stays, so you can see it happened. It holds no words.",
      },
      unreachable: {
        backups:
          "Database backups, for {days} days. Memax applies every Forget again after any restore.",
        backupsOne:
          "Database backups, for 1 day. Memax applies every Forget again after any restore.",
        git: "Earlier commits of a compiled file, in your repository's history",
        agentMemory: "What agents wrote to their own memory",
        handEdits: "Files with a hand edit Memax won't write over",
        copies: "Copies pasted elsewhere, such as a ChatGPT project",
        providers:
          "What {names} kept of what they were sent, under their terms (below)",
      },
    },
    residency: {
      title: "Where your data lives",
      what: "What",
      where: "Where",
      holds: {
        database: "Memories, receipts, sources and your settings",
        compute: "The API and the worker, which process them",
        objects: "Receipt checkpoints, compiled files and the forget ledger",
        edge: "The web app and the compile service, which keep nothing between requests",
      },
      at: "{provider}, {region}",
      encryption:
        "Encrypted in transit, and at rest with each provider's defaults. Per-space keys aren't offered yet.",
    },
    processors: {
      title: "Who else sees a memory's words",
      meta: "Only while the feature is on",
      service: "Service",
      gets: "What it gets",
      keeps: "What it keeps",
      uses: {
        judge: "The judge's first pass",
        judge_fallback: "The judge's fallback",
        judge_strong: "The judge's check on decisions in force",
        ask: "Ask's answers",
        dream: "Dream",
        dream_fallback: "Dream's fallback",
        dream_strong: "Dream's check on decisions in force",
        embeddings: "Embeddings of what you keep and propose",
        queries: "Embeddings of searches",
        rerank: "Reranking search results",
        email: "The morning email, to you",
      },
      model: "{use}: {model}",
      hosts: "on {hosts}",
      precision: "{precision} or better",
      retention: {
        zero: "Nothing. Every call goes to zero-retention endpoints only.",
        unconfirmed:
          "Not confirmed. {name} keeps API inputs for training unless the account opts out, and Memax hasn't confirmed its opt-out yet. Treat what it was sent as held under its terms.",
        provider_terms: "What {name}'s terms allow. Memax doesn't control it.",
      },
      none: "This server sends no memory's words to outside services.",
    },
    agents: {
      title: "Agents and autonomy",
      summary: "{agents} connected across {spaces}.",
      agents: "{n} agents",
      agentsOne: "1 agent",
      spaces: "{n} spaces",
      spacesOne: "1 space",
      levels: "At most: {list}.",
      level: {
        read: "{n} read",
        propose: "{n} propose",
        write: "{n} write",
      },
      paused: "{n} paused.",
      none: "No agents are connected to your spaces.",
      rule: "Agents propose and people keep. An agent at Write keeps its own writes, each one receipted, and only you, on the web, can let an agent do more.",
      link: "Agents and keys",
    },
    assurance: {
      title: "What your Keep counts as",
      body: "A Keep counts as human_web only when you're signed in on the Memax web app and its server signs your request. From the CLI, an agent or any other client it counts as client_attested, because an agent could drive those with your login.",
      needs:
        "Keeping a decision in a team space, or anything that came from outside, needs human_web.",
      session: "This session",
      verifiedLabel: "With your passkey",
      verified:
        "You have a passkey, so those keeps, raising what an agent may do and Forget ask for it, and count as human_web_verified: a copied browser session or an agent driving your browser can't make them.",
      nudge:
        "Add a passkey in Account, and those keeps will ask for it and count as human_web_verified, which a copied browser session can't reach.",
      account: "Account",
    },
  },
} as const;
