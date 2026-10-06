// Today (Main.png, TodayDark.png, MobileToday.png, EmptySpace.png),
// mounted as `t.ledger.today`. The page's title, lede and actions are
// in app-en.ts (`t.ledger.app.today`). Board copy is word for word.
// Placeholders are {name}; `<key>One` is the singular of `<key>`.
// Ledger voice applies (./voice.test.ts).
export const ledgerTodayEn = {
  dream: {
    seconds: "{n}s",
    folded: "{n} notes → {ref}",
    needsYou: "needs you",
    restorable: "restorable",
    none: "No edition yet",
    noneDetail:
      "Each morning Dream publishes an edition here: the notes it folded into facts, the conflicts it found and what faded. It isn't running for this space yet.",
    quiet: "No edition last night",
    quietDetail:
      "Dream had nothing new to read, so there's no edition. Empty nights cost nothing.",
  },
  waiting: {
    title: "Waiting on you",
    count: "{n} waiting on you",
    proposals: "{n} proposals",
    proposalsOne: "1 proposal",
    verify: "{n} to verify",
    more: "{n} more in Review",
    moreOne: "1 more in Review",
    open: "Open Review",
    nothing: "Nothing is waiting on you.",
    question: "a question",
    questions: "{n} questions",
  },
  inFlight: {
    title: "In flight",
    working: "{agent} is working",
    questions: "{n} questions for you",
    questionsOne: "1 question for you",
    from: "From {from} to {to}",
    none: "Nothing is in flight.",
    later:
      "Handoffs aren't available yet. When an agent passes a session on, it shows here while it's in flight.",
  },
  agents: {
    title: "Agents today",
    active: "{n} of {total} active",
    read: "{n} read",
    kept: "{n} kept",
    proposed: "{n} proposed",
    none: "none",
    nobody: "No agent has worked here today.",
    noAgents: "No agents are connected.",
    connect: "Connect an agent",
    unavailable: "The agents didn't load.",
  },
  compiled: {
    title: "Compiled context",
    recompile: "Recompile all files",
    none: "Nothing compiles yet.",
  },
  footer: {
    compiledAt: "Compiled at {time}",
    compiledOn: "Compiled {date} at {time}",
    dreamAt: "Dream runs nightly at {time}",
    kept: "{n} memories kept in {space}",
    keptOne: "1 memory kept in {space}",
  },
  empty: {
    title: "Nothing here yet",
    lede: "Connect an agent and Memax reads what it already knows about {where}. Everything arrives as a proposal.",
    steps: "Three steps to a first compile",
    connect: "Connect an agent in this repository",
    connectAnywhere: "Connect an agent",
    connectDetail:
      "Run this in {repository}. It finds Claude Code, Codex, Cursor and the rest.",
    connectDetailAnywhere:
      "Run this in your repository. It finds Claude Code, Codex, Cursor and the rest.",
    command: "npx memax-cli init --space {space}",
    copy: "Copy the command",
    connectHere: "Or connect one here",
    settle: "Settle what they disagree on",
    settleDetail:
      "The day-one cleanup shows where their files contradict each other.",
    compile: "Keep what's true, then compile",
    compileDetail:
      "Memax writes AGENTS.md, a CLAUDE.md that imports it and Cursor rules into the repository.",
    done: "Done",
    startFrom: "Or start from",
    conventionsFrom: "Conventions from {space}",
    conventionsLater: "Starting from another space isn't available yet.",
    drop: "Drop a CLAUDE.md or AGENTS.md",
    dropMeta: "Read as proposals, never kept on arrival",
    dropLater:
      "Reading a dropped file isn't available yet. `npx memax-cli init` reads them from the repository.",
    dreamTonight:
      "Dream runs tonight at {time} once there's something to read.",
  },
} as const;
