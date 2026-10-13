// A Personal space's machines and home files (personal-layer design §8),
// mounted as `t.ledger.machines`. No board draws them yet, so the words
// follow the design doc and the Ledger voice (./voice.test.ts).
// Placeholders are {name}; `<key>One` is the singular of `<key>`.
export const ledgerMachinesEn = {
  // What a home file is for, where no agent's name says it.
  canonical: "Canonical copy",
  platform: {
    darwin: "macOS",
    linux: "Linux",
    win32: "Windows",
    windows: "Windows",
  },
  // A home file across its machines (a list row).
  status: {
    inSync: "In sync on {n} machines",
    inSyncOn: "In sync on {machine}",
    drifted: "Hand edit on {machine}",
    driftedMany: "Hand edits on {n} machines",
    held: "Held for Review on {machine}",
    heldMany: "Held for Review on {n} machines",
    compiling: "Compiling",
    behind: "On {reached} of {of} machines",
    waitingFor: "Waiting for {machine}",
    // {seen} is one of `seen` below: "home-linux last online 3 days ago".
    machineSeen: "{machine} {seen}",
    off: "Off",
  },
  // One machine's copy of a file.
  state: {
    in_sync: "In sync",
    compiling: "Compiling",
    waiting: "Waiting for its daemon",
    offline: "Offline",
    drifted: "Hand edit",
    held: "Held for Review",
    stopped: "Stopped",
    unlinked: "Unlinked",
  },
  // When a machine's daemon last read its files.
  seen: {
    now: "online now",
    minutes: "last online {n} min ago",
    hours: "last online {n} h ago",
    yesterday: "last online yesterday",
    days: "last online {n} days ago",
    on: "last online {date}",
    never: "not online yet",
  },
  panel: {
    title: "Machines",
    titleAccount: "Where Personal is written",
    linked: "{n} linked",
    linkedOne: "1 linked",
    none: "Your Personal space isn't on any machine yet. Run this on each computer you work on:",
    another: "Run this on another computer to write Personal there too:",
    copy: "Copy the command",
    noFiles: "No agent's file",
    loadFailed: "Your machines didn't load.",
    retry: "Try again",
    rename: "Rename",
    renameAria: "Rename {machine}",
    renameLabel: "Name of {machine}",
    renameHint: "What you call it in Memax. Receipts call it {label}.",
    renameError: "Give it a name of 1 to 80 characters.",
    renameDo: "Rename",
    cancel: "Cancel",
    stop: "Stop compiling here",
    stopLabel: "Stop compiling on {machine}",
    confirm:
      "Memax takes your Personal space off {machine} the next time it's online. Files you edited by hand stay as they are.",
    confirmDo: "Stop compiling on {machine}",
    unlinked: "Unlinked {date}",
    unlinkedDetail:
      "Run {command} on it to write your Personal space there again.",
  },
  toast: {
    renamed: "Renamed {from} to {to}",
    renameClash:
      "{machine} was renamed somewhere else first. Here's its name now; rename it again if you still want to.",
    renameFailed: "{machine} wasn't renamed. {reason}",
    stopped:
      "Stopped compiling on {machine}. Memax takes its files off the next time it's online.",
    stopFailed: "Memax still writes your Personal space to {machine}. {reason}",
    stoppedFile:
      "Stopped compiling {file} on {machine}. Memax takes it off there the next time it's online.",
    stopFileFailed: "Memax still writes {file} on {machine}. {reason}",
  },
  // A home target's page (TargetPreview, one machine's copy).
  target: {
    switcher: "Machines",
    switcherItem: "{machine}, {state}",
    on: "On {machine}",
    lede: {
      claude_user:
        "What Claude Code reads at the start of every session, in every project, on {machine}. Memax writes it from your Personal space after every Keep and Forget.",
      codex_user:
        "Memax's block in {file} on {machine}, which Codex reads in every project. Your own lines in that file stay on your machine; Memax doesn't read them.",
      copilot_user:
        "Memax's block in {file} on {machine}, which Copilot CLI and VS Code's Copilot agent read in every project. Your own lines in that file stay on your machine; Memax doesn't read them.",
      windsurf_user:
        "Memax's block in {file} on {machine}, which Windsurf reads in every workspace. Windsurf reads at most 6,000 characters of it, your own rules included, so Memax writes nothing there past that. Your own lines stay on your machine; Memax doesn't read them.",
      gemini_user:
        "Memax's block in {file} on {machine}, which Antigravity reads in every project, and Gemini CLI once it reads AGENTS.md. Your own lines in that file stay on your machine; Memax doesn't read them.",
      agents_md:
        "Your Personal space in one file on {machine}, for you to read and for agents that have no file of their own.",
      agents_md_opencode:
        "Your Personal space in one file on {machine}, for you to read and for agents that have no file of their own. OpenCode reads it in every project there: its settings name this file.",
    },
    fileHead: "{machine} · {file}",
    block:
      "Only Memax's block is shown. Your own lines in this file stay on {machine}; Memax doesn't read them.",
    delivery: "Written by the memax daemon on {machine}.",
    budgetHint:
      "It loads in every session of every agent, so it stays short. The rest stay live over MCP.",
    budgetError: "Between 1 and 16 KB.",
    stop: "Stop compiling here",
    off: "Memax stopped compiling this file on {machine}. Agents there keep reading your Personal space over MCP.",
    unlinked:
      "{machine} is unlinked, so Memax writes nothing there. Run {command} on it to write your Personal space there again.",
  },
  // A hand edit to one machine's copy (DriftResolve).
  drift: {
    eyebrow: "Brief · compiled to · {file} on {machine}",
    title: "{file} has a hand edit on {machine}",
    lede: "Someone changed it on {machine} {when}, outside Memax. Memax won't overwrite it without you.",
    observed: "On {machine} now",
    stopDetail:
      "{tool} keeps reading your Personal space live over MCP, and the edited file stays on {machine} as it is.",
    notice: "{agent}'s copy on {machine} has one local edit.",
    noticeMany: "{agent}'s copy on {machine} has {n} local edits.",
    noticeFile: "{file} on {machine} has one local edit.",
    noticeFileMany: "{file} on {machine} has {n} local edits.",
    noticeBody: "Someone changed {file} by hand on {machine} {when}.",
  },
  // How far Personal reaches (D8).
  reach: {
    keep: "Keeping it reaches every agent on every machine you've linked.",
    write:
      "In Personal, what it keeps reaches every agent on every machine you've linked.",
    writeNow:
      "{agent} writes to {space} now, still with a receipt. What it keeps reaches every agent on every machine you've linked.",
  },
  // Settings › Account's "Signed in on": the CLI sessions whose daemon writes Personal.
  session: {
    writes: "writes Personal",
    signedOut:
      "{client} is signed out. Its daemon takes your Personal space off {machine} the next time it runs.",
  },
  // The first run and Connect an agent.
  setup: {
    written: "Personal: written for {agents} on {machine}.",
    writtenNone: "Personal: written on {machine}.",
    notYet:
      "Personal isn't written into your agents' own files yet. Run this on each computer you work on:",
    reads: "Reads your Personal space from {file} on each machine you link.",
    readsHint: "On a machine without it, run {command} first.",
  },
} as const;
