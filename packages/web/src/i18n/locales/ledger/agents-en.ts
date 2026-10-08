// Agents, Agent detail, Connect an agent and Settings › Agents and keys
// (epic 1.8), mounted as `t.ledger.agents`. The boards' copy is final
// (screens/source/Agents, AgentDetail, ConnectAgent, Keys), so strings
// that appear on a board are word for word. Placeholders are {name};
// `<key>One` is the singular of `<key>`. Ledger voice applies.
export const ledgerAgentsEn = {
  tableLabel: "Agents connected to {space}",
  noTarget: "Not compiling yet",
  ago: {
    justNow: "just now",
    minutes: "{n} min ago",
    hours: "{n} h ago",
    yesterday: "Yesterday",
    yesterdayLower: "yesterday",
    never: "never",
  },
  trust: {
    title: "Trust rules",
    meta: "Apply to every agent",
    external: {
      title: "Anything from external content goes to Review.",
      body: "Web pages, emails and issue comments can't be kept by an agent, even one set to Write.",
    },
    receipt: {
      title: "Every write carries a receipt.",
      body: "Agent, session, time and source, or the write is refused.",
    },
    forget: {
      title: "Forgetting reaches every copy.",
      body: "Compiled files are rewritten, and every agent gets a tombstone on its next read.",
    },
  },
  // What `memax-cli connect` prints, as the board draws it.
  terminal: {
    title: "connect from your terminal",
    command: "{cli} connect opencode",
    found: "Found OpenCode 0.9 in ~/.config/opencode",
    added: "MCP server added · autonomy: propose",
    compiled: "AGENTS.md compiled from {space} · in sync",
    receipts: "Its receipts will show as OC.",
  },
  // Why a level can't be chosen (the greyed option's tooltip).
  unavailable: {
    keyMaxPropose:
      "API keys propose at most. Connect {agent} over OAuth to let it write.",
    readOnlyCredential: "Its credential only reads.",
    revoked: "Its credential was revoked.",
    disconnected: "{agent} is disconnected.",
    notYoursRaise:
      "Only the person {agent} works for can raise it. Owners can lower it.",
    notYours: "Only the person {agent} works for can change it.",
    viewer: "Viewers can't raise what agents may do. Ask a member.",
  },
  // A command that didn't go through. The control rolls back.
  refused: {
    needsWeb:
      "Memax couldn't confirm this came from you on memax.app, so {agent} stays at {level}. Sign in again here, then raise it.",
    needsWebResume:
      "Memax couldn't confirm this came from you on memax.app, so {agent} stays paused. Sign in again here, then resume it.",
    needsWebDev:
      "In local development, set WEB_SURFACE_SECRET for the web app and the API.",
    signInAgain: "Sign in again",
    keyMaxPropose:
      "API keys can propose but never keep, so {agent} can't write. Connect it over OAuth to let it write.",
    notYours:
      "Only the person {agent} works for can raise what it may do. Owners of {space} can lower it.",
    notAllowed:
      "{agent} can propose at most in {space}: only people who keep there can let an agent write.",
    viewer: "Viewers can't raise what agents may do in {space}. Ask a member.",
    personMustManage: "Only a person can change what an agent may do.",
    notMember:
      "You're not a member of {space}, so its agents aren't yours to change.",
    surfaceUnverified:
      "The web app's signature didn't verify, so nothing changed. Reload the page and try again.",
    invalidTransition:
      "{agent} is disconnected, so nothing changed. Connect it again to use it.",
    notFound: "{agent} isn't here any more. Reload to see where it is.",
    failed: "That didn't go through, so {agent} stays at {level}. Try again.",
    failedCommand: "That didn't go through. Nothing changed for {agent}.",
  },
  changed: {
    read: "{agent} only reads {space} now.",
    propose: "{agent} proposes in {space} now. Its writes wait in Review.",
    write: "{agent} writes to {space} now, still with a receipt.",
    paused:
      "Paused {agent}. It still reads, and writes nothing until you resume it.",
    resumed: "Resumed {agent}.",
    disconnected: "Disconnected {agent}. Its credential is revoked.",
  },
  detail: {
    eyebrow: "Agents · {space}",
    surface: {
      cli: "Runs in your terminal.",
      ide: "Runs in your editor.",
      cloud: "Cloud tasks and the CLI.",
      chat: "A chat app, over remote MCP.",
    },
    lede: {
      read: "It reads and never writes, and its receipts show as {mono}.",
      propose:
        "It proposes; everything it writes waits for you, and its receipts show as {mono}.",
      write:
        "It writes; its writes are kept directly, and its receipts show as {mono}.",
      paused:
        "It's paused: it reads but writes nothing until it's resumed, and its receipts show as {mono}.",
      disconnected:
        "It's disconnected: its credential is revoked, and Memax refuses its requests.",
    },
    pause: "Pause",
    resume: "Resume",
    disconnect: "Disconnect",
    notMine: "Only the person {agent} works for can pause or disconnect it.",
    disconnectedAlready: "{agent} is already disconnected.",
    confirm: {
      title: "Disconnect {agent}?",
      oauth:
        "Its OAuth grant is revoked now, so its next request is refused. Its receipts stay, and you can connect it again later.",
      key: "Its API key is revoked now, so its next request is refused. Its receipts stay, and you can connect it again later.",
      cancel: "Cancel",
      confirm: "Disconnect {agent}",
    },
    revoked: {
      title: "Its credential was revoked.",
      body: "{agent}'s API key was revoked outside Agents, so Memax refuses its requests. Its receipts stay. Connect it again to use it, or disconnect it to clear it from this list.",
    },
    may: {
      title: "What {agent} may do",
      label: "Autonomy for {agent}",
      read: "Reads the Brief and kept memories. Never writes.",
      propose: "Its writes wait in Review until a person keeps them.",
      write: "Its writes are kept directly, still with a receipt.",
      external:
        "Whatever you choose, anything {agent} learns from a web page, an email or an issue waits for a person.",
      held: "It held {n} such writes this week.",
      heldOne: "It held 1 such write this week.",
      notConnected:
        "{agent} isn't connected to {space}. Choosing a level connects it here.",
    },
    writes: {
      title: "Recent writes",
      meta: "{writes} this week · {kept} kept · {rejected} rejected · {waiting} waiting",
      all: "All of {agent}'s writes",
      none: "Nothing written this week.",
      proposedIn: "Proposed by {agent} in session {session}",
      contradicts: "Contradicts {ref}",
      unread: "The words of {ref} didn't load.",
    },
    sessions: {
      title: "Sessions",
      meta: "Reads · writes · when",
      cloud: "Cloud task {ref}",
      cli: "CLI · session {ref}",
      plain: "Session {ref}",
      fromHandoff: "{session} · from handoff {ref}",
      running: "Running",
      none: "No sessions yet.",
      readsLabel: "Reads:",
      writesLabel: "Writes:",
    },
    connection: {
      title: "Connection",
      connected: "Connected",
      through: "Through",
      may: "May",
      space: "Space",
      spaces: "Spaces",
      client: "Client",
      lastUsed: "Last used",
      byYou: "{date}, by you",
      carriedOver: "{date}, carried over from V1",
      oauth: "Remote MCP · OAuth 2.1",
      key: "API key",
      only: "{space} only",
      none: "No space yet",
      never: "Never",
    },
    compiles: {
      title: "Compiles to",
      none: "Not compiling yet",
      mcpOnly: "Reads live over MCP, from no file",
      sharedWith: "shared with {agent}",
    },
    week: {
      title: "This week",
      reads: "Reads",
      proposals: "Proposals",
      questions: "Questions",
      questionsWaiting: "{n}, waiting on you",
      handoffs: "Handoffs",
      received: "{n} received",
      notRecorded: "Not recorded yet",
    },
    notFound: {
      title: "There's no agent here that you can open.",
      description:
        "It may have been disconnected, or it works in a space you're not part of.",
      back: "Open Agents",
    },
  },
  capabilities: {
    read: "read",
    propose: "propose",
    write: "write",
    ask: "ask",
  },
  connect: {
    title: "Connect an agent",
    to: "to {where}",
    close: "Close",
    which: "1 · Which agent",
    whichLabel: "Which agent",
    may: "2 · What it may do",
    mayLabel: "Autonomy for {agent}",
    reads: "3 · What it reads",
    readsLabel: "Spaces {agent} reads",
    runs: "4 · Run this where {agent} lives",
    runsChat: "4 · Add it in {agent}",
    connected: "Connected",
    paused: "Paused",
    notConnected: "{surface} · not connected",
    level: {
      read: "{agent} reads the Brief and kept memories, and never writes.",
      propose:
        "{agent}'s writes wait in Review until you keep them. Anything it learns from a web page, an email or an issue always waits, whatever you choose here.",
      write:
        "{agent}'s writes are kept directly, still with a receipt. Anything it learns from a web page, an email or an issue always waits, whatever you choose here.",
    },
    connectsAt:
      "It connects at {level}. Raise it to Write here in Agents once it shows up.",
    live: {
      read: "Live over MCP: recall, search and get",
      propose: "Live over MCP: recall, search and get, plus propose",
      write: "Live over MCP: recall, search and get, plus write",
    },
    oauth: "OAuth 2.1",
    oneSpace: "It reads at least one space.",
    copyCommand: "Copy command",
    copyUrl: "Copy the server address",
    byHand: "Or add the remote server by hand: {url}",
    chat: "Add it as a connector in {agent}. {agent} asks you to sign in to Memax and shows what it may do.",
    receiptsAs: "Its receipts will show as {stamp}",
    cancel: "Cancel",
    go: "Connect {agent}",
    copied:
      "Copied the command. Run it where {agent} lives, and it shows up here once it connects.",
    copiedUrl:
      "Copied the server address. Add it in {agent}, and it shows up here once you sign in.",
    copyFailed: "Couldn't copy. Select the text and copy it yourself.",
  },
  keys: {
    title: "Agents and keys",
    lede: "Everything that can read or write your spaces. Each one's writes carry its name in the receipt, and revoking one stops it at once.",
    connections: {
      title: "Agent connections",
      meta: "OAuth 2.1 · renew on their own",
      agent: "Agent",
      spaces: "Spaces",
      may: "May",
      lastUsed: "Last used",
      none: "No agents are connected yet.",
      label: "Your agent connections",
    },
    apiKeys: {
      title: "API keys",
      new: "New key",
      name: "Name",
      key: "Key",
      may: "May",
      lastUsed: "Last used",
      created: "{spaces} · created {date}",
      allSpaces: "All your spaces",
      read: "read",
      propose: "read, propose",
      none: "No API keys yet.",
      label: "Your API keys",
    },
    revoke: "Revoke",
    revokeLabel: "Revoke {name}",
    confirmAgent:
      "Revoke {name}? Its credential ends now, and its next request is refused.",
    confirmKey:
      "Revoke {name}? Anything using it is refused from its next request.",
    cancel: "Cancel",
    revoked: "Revoked {name}.",
    revokeFailed: "{name} wasn't revoked. Try again.",
    notice:
      "Keys can read and propose, never keep or forget. Only people, and agents you set to Write, can keep. Keys are shown once, then only their last four characters.",
    form: {
      title: "New key",
      name: "Name",
      nameHint: "Say what uses it, so its receipts make sense later.",
      may: "May",
      read: "Read",
      propose: "Read and propose",
      space: "It can reach {space} only.",
      create: "Create key",
      cancel: "Cancel",
      nameRequired: "Name the key first.",
      failed: "The key wasn't created. Try again.",
      createdTitle: "Copy {name} now",
      createdBody: "Memax shows a key once. Keep it somewhere safe.",
      copy: "Copy the key",
      copied: "Copied the key",
      done: "Done",
    },
  },
} as const;
