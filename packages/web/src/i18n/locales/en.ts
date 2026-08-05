// English — source of truth. All keys must exist in every locale.
export const en = {
  common: {
    retry: "Retry",
    undo: "Undo",
  },
  billing: {
    upgrade: "Upgrade",
  },
  // Shell-v2 chrome navigation copy. Top-level so it's reachable from
  // both desktop LeftRail and mobile drawer (plan 22) without locale
  // namespace gymnastics.
  nav: {
    primary: "Primary",
    expandRail: "Expand navigation rail",
    collapseRail: "Collapse navigation rail",
    showSecondaryPanel: "Show topic panel",
    hideSecondaryPanel: "Hide topic panel",
    openBar: "Open bar (⌘K)",
    openSettings: "Open settings",
    topicTreeRegion: "Topics",
    overview: "Overview",
    tabs: {
      brain: "Ask memax",
      memories: "Memories",
      agents: "Agents",
      inbox: "Inbox",
    },
  },
  // Bar
  bar: {
    placeholder: {
      default: "dump or ask…",
      brain: "remember or ask...",
      memory: "Search or remember...",
      searchIn: "Search in {title}...",
      recall: "ask a question to search your knowledge",
    },
    cta: {
      recall: "✦ Recall",
      remember: "Remember",
      rememberDown: "Remember ↓",
      tab: "Tab",
      copy: "copy",
      copied: "✓ copied",
    },
    dismiss: "Dismiss",
    dropToRemember: "Drop files here",
    loadMore: "+ {count} more",
    noResults: "No memories match this query yet.",
    noResultsHint: "Save it above to start remembering.",
    recallError: "Recall failed.",
    recallErrorHint: "Network error — your query is still here.",
    captureUrl: "Capture page",
    mobileClose: "Close",
    mobileBack: "Back",
    mobileDumping: "dumping",
    mobileAsking: "asking",
    mobileExpand: "Expand to fullscreen",
    mobileAddAttachment: "Add attachment",
    removeAttachment: "Remove attachment",
    mode: {
      remember: "remember",
      recall: "recall",
    },
    ai: {
      placeholderBefore: "AI answer will appear here after",
      placeholderAfter: "",
      thinking: 'Thinking about "{query}"…',
      freeStub: "AI answers are a Pro feature — recall stays free.",
      error: "AI answer failed — sources below are still available.",
      errorNoSources: "AI answer failed.",
      quotaExhausted:
        "You've used all your asks this month — sources below are still available.",
    },
    section: {
      memaxSays: "memax says",
      memaxSaysTopic: "memax says · {topic}",
      related: "Related",
      relatedInTopic: "Related in {topic}",
      matches: "Quick matches",
      matchesInTopic: "Quick matches in {topic}",
      recent: "Recent",
      recentInTopic: "Recent in {topic}",
      jumpTo: "Jump to",
      jumpToInTopic: "Jump to · within {topic}",
      filterTopic: "Filter by topic",
    },
    remember: {
      action: "Dump to memax",
      actionFiles: "Dump {count} files to memax",
      sending: "Sending…",
      error: "Couldn't send.",
      errorNetwork: "Network error",
    },
    hint: {
      recall: "recall",
      remember: "remember",
      open: "open",
      forget: "forget",
      navigate: "navigate",
      close: "close",
      commands: "commands",
      run: "run",
      drill: "drill",
    },
    command: {
      hub: {
        label: "hub",
        desc: "Switch active workspace",
      },
      forget: {
        label: "forget",
        descMounted: 'Forget "{title}"',
      },
    },
    mention: {
      topicDesc: "Jump to topic",
      hint: "Type to filter topics, ↑↓ to navigate, Enter to jump.",
      noMatches: "No topics match.",
    },
    scope: {
      clear: "Click to clear {label} scope",
    },
  },

  // Brain view — below bar
  brainView: {
    empty: {
      hint: "Type a thought · **⌘↵** to remember",
      hintMobile: "Dump a thought — memax remembers",
      tagline: "memax will organize it into topics as you build.",
      setupHint: "Set up agents →",
    },
    shortcuts: {
      recall: "recall",
      remember: "remember",
      drop: "drop files",
      commands: "commands",
      toggle: "summon",
    },
    loading: "loading…",
  },

  // Memory count
  memory: {
    zero: "No memories yet",
    one: "1 memory",
    other: "{n} memories",
  },

  // Memory detail surface — shared between the route variant
  // (/h/<slug>/memories/<id>) and the right-side panel variant
  // (intercepted from grid clicks). Panel-specific affordances:
  // close button, "open full page" hard-nav escape, panel aria.
  memoryDetail: {
    panelAria: "Memory detail",
    openFullPage: "Open full page",
    closePanel: "Close memory detail",
  },

  // Chat (plan 24 — Agent Chat)
  chat: {
    title: "Chat",
    newChat: "New chat",
    // Empty state copy redesigned 2026-05-20. Earlier title "Ask
    // memax anything" mirrored the generic ChatGPT / Claude pattern
    // and didn't communicate memax's specific value (the *memory*
    // angle). Personalized greeting + USP-forward subtitle reads
    // closer to Linear / Notion AI / Cursor on the empty surface:
    // warm, concise, opinionated about what this product is FOR.
    emptyGreeting: "Hey {name}, what's on your mind?",
    emptyGreetingFallback: "What's on your mind?",
    emptyTitle: "Ask memax anything",
    emptySubtitle:
      "memax has read your notes, your team's, and your agents' work. Ask it anything.",
    sidebar: {
      heading: "Conversations",
      close: "Close conversations",
      groupToday: "Today",
      groupYesterday: "Yesterday",
      groupThisWeek: "This week",
      groupEarlier: "Earlier",
      groupPinned: "Pinned",
      groupArchived: "Archived",
      noSessions: "No conversations yet.",
      untitled: "Untitled chat",
      pinned: "Pinned",
      archived: "Archived",
      showArchived: "Show archived",
      hideArchived: "Hide archived",
      noArchived: "No archived conversations.",
    },
    // Two suggestion sets, picked at render time based on the
    // user's actual state. New users (empty corpus or no
    // non-memax agent connected yet) see seed-aware discovery
    // prompts that naturally recall the plan-23 onboarding seeds
    // (e001..e004) — the agent answers using their OWN seed
    // content, which doubles as the organic onboarding moment.
    // Once the user crosses the activation threshold (≥5 user
    // memories AND ≥1 connected agent), they switch to the
    // veteran-flavored set focused on recall over their actual
    // working corpus.
    suggestions: {
      // Veteran set — for users who've moved past activation.
      catchUp: "What's new since I last checked?",
      findConflicts: "Find conflicts in my notes",
      focus: "What should I focus on this week?",
      planWeek: "Plan my week from my memory",
      // Onboarding set — surfaces while the corpus is empty
      // or no non-memax agent is connected. Each prompt recalls
      // one of the plan-23 seeds:
      //   onboardCorpus    → recall over the seed corpus
      //   onboardSetup     → e002 (Claude Code / Cursor setup)
      //   onboardSave      → e001 (⌘K bar)
      //   onboardOrganize  → e003 (dreams + topics)
      onboardCorpus: "Catch me up on what's already here",
      onboardSetup: "Connect my AI tools to memax",
      onboardSave: "How do I save something fast?",
      onboardOrganize: "How does memax organize my notes?",
    },
    composer: {
      placeholder: "Ask anything…",
      send: "Send",
      sendShortcut: "Enter",
      cancel: "Stop",
    },
    status: {
      thinking: "Thinking…",
      streaming: "Replying…",
      canceling: "Stopping…",
      canceled: "Stopped",
      failed: "Failed",
      partial: "Got cut off",
      completed: "Completed",
    },
    toolCall: {
      running: "{name} running…",
      done: "{name}",
      error: "{name} failed",
    },
    thinking: {
      // Readable reasoning blocks (model.thinking wire events).
      reasoningLive: "Thinking…",
      reasoningLabel: "Thought",
      reasoningFor: "Thought for {s}s",
      recallNoHits: "No memories matched",
      // Per-tool in-flight labels (running…).
      recallMemoriesRunning: "Searching memories…",
      listMemoriesRunning: "Listing memories…",
      getMemoryRunning: "Reading memory…",
      listHubsRunning: "Looking at your hubs…",
      getHubRunning: "Reading hub…",
      listTopicsRunning: "Reading topics…",
      pushMemoryRunning: "Saving memory…",
      forgetMemoryRunning: "Forgetting memory…",
      proposeTopicMergeRunning: "Proposing topic merge…",
      fetchUrlRunning: "Fetching link…",
      genericRunning: "Working…",
      // Per-tool done labels (no ellipsis — work is finished).
      recallMemoriesDoneOne: "Searched 1 memory",
      recallMemoriesDoneN: "Searched {count} memories",
      recallMemoriesDoneZero: "No memories found",
      listMemoriesDone: "Listed memories",
      getMemoryDone: "Read memory",
      listHubsDone: "Read your hubs",
      getHubDone: "Read hub",
      listTopicsDone: "Read topics",
      pushMemoryDone: "Saved memory",
      forgetMemoryDone: "Forgot memory",
      proposeTopicMergeDone: "Proposed topic merge",
      fetchUrlDone: "Fetched link",
      genericDone: "Done",
      // Per-tool error label.
      genericError: "{name} failed",
      // Auto-fold summary pill (terminal + N tools ≥ threshold).
      // The user can chevron-expand to see the full step chain.
      autoFoldUsing: "Using {count} tools…",
      autoFoldUsed: "Used {count} tools",
      autoFoldExpand: "Show steps",
      autoFoldCollapse: "Hide steps",
      // Per-step expand panel labels (args + tool result preview).
      detailArgsLabel: "args:",
      detailResultLabel: "result:",
    },
    sources: {
      heading: "Sources",
      pillTitle: "{title}",
      pillUntitled: "Untitled memory",
      empty: "",
      // Compact-row pattern: show 3 chips, "+{n}" pill for the
      // rest, click to expand. Industry: Perplexity / ChatGPT
      // search / Phind all default to a few visible + expand.
      showMore: "+{n} more",
      showLess: "Show less",
    },
    capabilities: {
      label: "Capabilities",
      approval: "asks first",
      empty: "No tools enabled",
      tools: {
        recall_memories: "Recall",
        list_memories: "List",
        get_memory: "Read",
        list_hubs: "Hubs",
        get_hub: "Hub info",
        list_topics: "Topics",
        push_memory: "Save",
        forget_memory: "Forget",
        propose_topic_merge: "Merge topics",
        fetch_url: "Web",
      },
    },
    approval: {
      title: "Approve {tool}?",
      description:
        "memax wants to use {tool}. Review the arguments below before approving.",
      approve: "Approve",
      deny: "Deny",
      expiresIn: "Expires in {seconds}s",
      expired: "Approval timed out",
    },
    citations: {
      heading: "Cited memories",
    },
    settings: {
      title: "Conversation settings",
      modelLabel: "Model",
      toolsLabel: "Tools",
      scopeLabel: "Hub scope",
      titleLabel: "Title",
      pin: "Pin",
      unpin: "Unpin",
      archive: "Archive",
      unarchive: "Unarchive",
      delete: "Delete",
    },
    rowMenu: {
      open: "Conversation actions",
      rename: "Rename",
      renameTitle: "Rename conversation",
      renameConfirm: "Rename",
      renameCancel: "Cancel",
      renamePending: "Saving…",
      pin: "Pin",
      unpin: "Unpin",
      archive: "Archive",
      unarchive: "Unarchive",
      delete: "Delete",
      deletePrompt: "Delete this conversation?",
      deleteConfirm: "Delete",
      deleteCancel: "Keep",
      deletePending: "Deleting…",
    },
    errors: {
      createSession: "Couldn't start a new chat.",
      patchSession: "Couldn't update the conversation.",
      deleteSession: "Couldn't delete the conversation.",
      sendMessage: "Couldn't send your message.",
      cancelMessage: "Couldn't stop the reply.",
      regenerateMessage: "Couldn't regenerate.",
      decideApproval: "Couldn't record your approval.",
      sessionLocked:
        "Another reply is in flight on this conversation. Wait for it to finish or stop it first.",
      regenerateNotEligible: "This message can't be regenerated.",
      replayExpired: "The replay window for this turn has expired.",
    },
  },

  // Recall flow
  recall: {
    recalling: "Recalling...",
    noResults: "No matching memories found.",
    noResultsHint: "Try different words or rephrase your question.",
    errorTitle: "memax couldn't reach the memory server",
    retry: "Retry recall",
    aiPro: "AI answers available on",
    showMore: "+ {n} more",
    showLess: "Show less",
    done: "Done",
    backToResults: "Back to results",
    sources: "Sources",
    copy: "Copy",
    copyForAI: "Copy for AI",
    copiedContext: "Copied",
    synthesizing: "memax is thinking",
    errorGenerate: "Could not generate a summary.",
    errorFailed: "Failed to generate summary.",
    messages: [
      "memax recalling",
      "connecting the dots",
      "surfacing memories",
      "piecing it together",
    ] as readonly string[],
    aiMessages: [
      "memax is thinking",
      "reading your memories",
      "connecting the dots",
      "composing an answer",
      "almost there",
    ] as readonly string[],
  },

  // Remember flow
  remember: {
    remembering: "Remembering...",
    remembered: "Remembered.",
    hintPlaceholder: "What is this?",
  },

  // Forget
  forget: {
    button: "Forget",
    confirm: "Forget?",
    keep: "Keep",
    everything: "Forget everything",
    allConfirm: "Forget all {n} memories?",
    allConfirmOne: "Forget your 1 memory?",
    allForgetting: "Forgetting {n} memories...",
    allForgettingOne: "Forgetting 1 memory...",
    thisMemory: "Forget this memory?",
    forgettingQuote: "Forgetting “{title}”",
  },

  // Note detail
  note: {
    summary: "memax summary",
    classifiedAs: "memax classified this as",
    attachments: "Original files",
    originalFile: "Original file",
    actions: {
      menu: "More actions",
      copyMarkdown: "Copy markdown",
      rename: "Rename",
      download: "Download .md",
      showDetails: "Show details",
      hideDetails: "Hide details",
      editBody: "Edit body",
      saveEdit: "Save",
      cancelEdit: "Cancel",
    },
    editSaveFailed:
      "Couldn't save just now. Try again — your edit is preserved.",
    editEmptyContent:
      "Memory content can't be empty. Add something or cancel to keep what's there.",
    download: "Download",
    downloading: "Downloading...",
    openPreview: "Open preview",
    closePreview: "Close preview",
    previousImage: "Previous image",
    nextImage: "Next image",
    classification: {
      recentActivity: "recent activity and short-lived context",
      pastContext: "past context that may still help",
      howTo: "a guide, workflow, or how-to memory",
      decisionContext: "a decision, tradeoff, or rationale",
      durableReference: "durable reference knowledge",
      reference: "reference knowledge that can evolve",
    },
    stability: {
      volatile: "fades over ~2 weeks unless referenced",
      evolving: "updates as things change",
      stable: "kept at full weight forever",
    },
    find: "Find...",
    tagPlaceholder: "tag...",
    addTag: "+ add",
    removeTag: "Remove tag",
    origin: {
      // Transport-only labels. Not consumed by ProvenanceStrip anymore —
      // the detail-page actor pattern shows identity (author / agent /
      // glyph) instead of transport. Kept for potential debug affordances
      // that want to surface "captured via web/CLI/MCP/API" explicitly.
      web: "web",
      cli: "CLI",
      mcp: "AI agent",
      api: "API",
      inProject: "{project}",
      fromFile: "{file}",
    },
    stillRemembering: "Still remembering...",
    remembering: "Remembering",
    forgetting: "Forgetting...",
    originalContent: "Original content",
    words: "{n} words",
    recalledN: "recalled {n}×",
    captured: "captured",
    capturedAt: "Captured {time}",
    updatedAt: "Updated {time}",
    alsoIn: "Also in {topic}",
    siblingsEmpty: "No other memories in this topic yet",
    related: "Related",
    relatedEmptyTitle: "Nothing related yet",
    relatedEmptyHint: "memax will link similar memories as they arrive",
    relatedProcessing: "Still reading — related memories will appear soon",
    source: {
      claudeCode: "Claude Code",
      cursor: "Cursor",
      chatgpt: "ChatGPT",
      you: "You",
    },
  },

  // Settings
  settings: {
    connectAgents: "Connect your AI agents",
    setupLabel: "One command, all agents",
    setupDesc:
      "Auto-detects your installed agents, creates per-agent API keys, and configures MCP + hooks. Tomorrow, your AI will remember today.",
    promptLabel: "Or paste this into any agent chat",
    promptTemplate: "Connect to memax MCP: {url}",
    altMethods: "Other setup methods",
    manualConfig: "Manual MCP config",
    pasteInEditor: "Paste in your editor's MCP settings",
    valueRecall: "Instant recall",
    valuePush: "Auto-capture",
    valueShared: "Shared across agents",
    documentation: "Documentation",
    signOut: "Sign out",
  },

  agentConfigs: {
    title: "Your Agents",
    subtitle: "AI assistants connected to your memory.",
    empty: "No agents connected yet",
    emptyHint:
      "Connect your first agent to give it persistent memory across sessions.",
    emptyTitle: "Connect your first agent",
    emptyDesc: "One command — tomorrow, your AI will remember today.",
    files: "{n} files",
    file: "1 file",
    version: "v{n}",
    lastSynced: "Synced {time}",
    synced: "Synced",
    neverSynced: "Never synced",
    forgetConfig: "Forget",
    forgetConfigConfirm: "Forget this file?",
    forgetAll: "Forget all files",
    forgetAllConfirm: "Forget all {n} files for {agent}?",
    forgetting: "Forgetting...",
    forgotConfig: "Forgot file",
    forgotConfigs: "Forgot {n} files",
    forgetFailed: "Couldn't forget — retry",
    forgetPartial: "Forgot {deleted} · {failed} failed — retry",
    addMore: "Connect another agent",
    connectNew: "Connect agent",
    connectNewAria: "Connect a new AI agent",
    connectPageTitle: "Connect an agent",
    connectPageSubtitle:
      "Run one command and your AI assistants gain persistent memory.",
    gridEmptyHint: "No agents connected yet — connect your first below.",
    viewDetails: "View details",
    notFoundTitle: "Agent not found",
    notFoundHint: "It may have been disconnected. Pick another from your list.",
    backToList: "Back to all agents",
    viewDetail: "Open detail page",
    distilled: "{n} memories distilled",
    distilledOne: "1 memory distilled",
    preview: "Preview",
    hidePreview: "Hide",
    apiKeys: "API Keys",
    allHubs: "All hubs",
    revokeConfirm: "Revoke this key? Agent will lose access.",
    revoke: "Revoke",
    revoking: "Revoking...",
    revoked: "Key revoked",
    revokedAlready: "Key was already revoked",
    revokeFailed: "Couldn't revoke key — retry",
    syncedConfigs: "Synced Configs",
    memoriesPushed: "{n} pushed",
    memoriesPushedLong: "{n} memories pushed",
    memoriesPushedLongOne: "1 memory pushed",
    lastActive: "Last active {time}",
    lastActivity: "Last activity {time}",
    lastActionAsk: 'asked "{summary}"',
    lastActionRecall: 'looked up "{summary}"',
    lastActionPush: 'saved "{summary}"',
    lastActionGeneric: "Worked with {summary}",
    rename: "Rename",
    editName: "Edit name",
    displayNameLabel: "Display name",
    agentSlugLabel: "Agent ID",
    changeIcon: "Change icon",
    disconnect: "Disconnect",
    disconnecting: "Disconnecting...",
    disconnectedWithCounts:
      "Disconnected {agent} — revoked {keys} keys, forgot {configs} configs",
    disconnectedAlready: "{agent} was already disconnected",
    disconnectFailed: "Couldn't disconnect — retry",
    disconnectConfirm:
      "Disconnect {agent}? All keys and configs will be removed. Memories are preserved.",
    disconnectMemoriesStay: "Memories from this agent stay in place.",
    statusConfigured: "Connected",
    statusJustNow: "Just active",
    // statusToday replaces a 7-day "recent" window that was too loose
    // — an agent idle for 5 days shouldn't read as "active recently."
    // New split: <24h = active today, older = idle (still visible,
    // just not freshly used). statusRecent stays as a legacy alias
    // for any consumer that hasn't migrated.
    statusToday: "Active today",
    statusIdle: "Idle",
    statusRecent: "Active recently",
    noKeys: "No API keys",
    keysWithoutAgentIdentity: "Keys without agent identity",
    keysWithoutAgentIdentityHint:
      "These authenticate as you. Memories pushed with them appear as your own.",
    keysWithoutAgentIdentityInfoLabel: "About keys without agent identity",
    keysWithoutAgentIdentityInfoTitle: "Why these show as you",
    keysWithoutAgentIdentityInfoBody:
      "Re-issue as an agent key to attribute memories correctly. Or revoke if no longer needed.",
    reissueAsAgentKey: "Reissue as agent key",
    reissueDialogTitle: "Reissue as agent key",
    reissueDialogDescription:
      "Create a new key mapped to an agent slug. The old key stays active until you revoke it.",
    agentSlugInputLabel: "Agent slug",
    agentSlugInputPlaceholder: "e.g. hatch",
    reissueAgentSlugHelp:
      "Use a known agent slug or invent a new one. The agent will appear in the Agents tab after its first push.",
    reissueSlugRequired: "Enter an agent slug.",
    reissueCreate: "Create key",
    reissueCreating: "Creating...",
    reissueFailed: "Couldn't create agent key",
    reissueUpdateNote:
      "Update your agent to use this key, then revoke the old one.",
    // Config file classes + profile scopes (personal agents)
    classIdentity: "identity",
    profileTag: "profile",
    credentialClassApiKey: "via API key",
    credentialClassOAuth: "via OAuth",
    credentialClassSession: "via session",
    unnamedAgent: "Unnamed agent",
    reconnect: "Reconnect",
    reconnectExplanation:
      "This connection was created without a stable agent identity. Disconnect the agent and have it reconnect through MCP OAuth — its registration will be validated.",
    disconnectCredential: "Disconnect agent",
  },

  // Batch operations
  batch: {
    select: "Select",
    selectAll: "Select all",
    done: "Done",
    move: "Move",
    pickerBack: "Back to actions",
    moving: "Moving {n} memories...",
    movingOne: "Moving 1 memory...",
    copy: "Copy",
    copied: "Copied",
    export: "Export",
    forget: "Forget",
    forgetConfirm: "Forget {n} memories?",
    forgetConfirmOne: "Forget 1 memory?",
    forgetting: "Forgetting {n} memories...",
    forgettingOne: "Forgetting 1 memory...",
    moved: "Moved {n} memories.",
    movedOne: "Moved 1 memory.",
    movedToDestination: "Moved {n} memories to {name}.",
    movedToDestinationOne: "Moved 1 memory to {name}.",
    partialMove: "{success} · {skipped} skipped",
    partialForget: "{success} · {skipped} skipped",
    forgot: "{n} memories forgotten",
    forgetFailed: "Couldn't forget memories",
    forgetDenied: "You don't have permission to forget those memories",
    forgetNotReady: "This memory is still syncing. Try again in a moment.",
    moveFailed: "Couldn't move memories",
    moveNotReady: "This memory is still syncing. Try again in a moment.",
    moveSourceDenied:
      "Couldn't move memories out of the source hub — its policy forbids it",
    targetNotFound: "Destination topic is no longer available.",
    noWriteAccess: "You don't have write access to that destination.",
  },

  // Status (real server state, not decorative)
  status: {
    dreaming: "dreaming",
    recalling: "recalling",
    dreamed: "dreamed",
    idle: "listening",
  },

  // Compose
  compose: {
    placeholder: "Write your memory...",
    escHint: "Esc to collapse",
    rememberDown: "Remember ↓",
  },

  composeCard: {
    title: "New memory",
    subtitle: "For notes that need more space",
    aria: "Compose a new memory",
    resumeTitle: "Resume draft",
    resumeSubtitle: "Pick up where you left off",
    resumeAria: "Resume your compose draft",
  },

  composeCell: {
    title: "Dump a new memory",
    hint: "Drop · type · paste",
    aria: "Dump a new memory",
    resumeTitle: "Resume draft",
    resumeHint: "Pick up where you left off",
    resumeAria: "Resume your compose draft",
  },

  composeModal: {
    title: "New memory",
    closeAria: "Close compose",
    titlePlaceholder: "Title (optional)",
    titleAria: "Memory title",
    bodyPlaceholder:
      "Write something worth remembering. Markdown shortcuts work — # for heading, - for list, ** for bold.",
    save: "Save",
    hotkeyHint: "⌘↵ to save · Esc to close",
    saveFailed: "Couldn't save just now. Try again — your draft is preserved.",
    tagsPlaceholder: "Add tags (Enter to commit)",
    tagsPlaceholderSubsequent: "Add another…",
    tagsAria: "Add tags to this memory",
    removeTagLabel: "Remove tag {tag}",
    targetHubAria: "Save to {hub} (click to change)",
    attachLabel: "Attach file",
    attachmentUploading: "Uploading…",
    attachmentError: "Upload failed",
    attachmentRemoveLabel: "Remove attachment {file}",
    toolbarBoldAria: "Bold",
    toolbarItalicAria: "Italic",
    toolbarStrikeAria: "Strikethrough",
    toolbarCodeAria: "Inline code",
    toolbarLinkAria: "Link",
    linkPromptLabel: "Enter URL",
  },

  // Sort & view
  sort: {
    recent: "Sorted by recent",
    recalled: "Sorted by most recalled",
  },

  // Tabs
  tabs: {
    all: "All",
  },

  // Mobile dock navigation
  dock: {
    brain: "Ask",
    topics: "Topics",
    inbox: "Inbox",
    recentTitle: "Recent",
  },

  settingsOnboarding: {
    // Settings → Getting started row (plan 18 §3.3 + §5.4)
    title: "Getting started",
    descriptionInitial:
      "Take the first-week tour. memax pins it to your home page until you're set up.",
    descriptionPending:
      "Your first-week checklist is still pinned to /memories. Open it from there to continue.",
    descriptionDone:
      "You're all set up. Restart the tour any time to revisit the basics.",
    startCta: "Start onboarding",
    restartCta: "Restart onboarding",
    rateLimited:
      "You can only restart onboarding a few times per day. Try again later.",
    errorUnauthorized: "Sign in to restart onboarding.",
    errorGeneric: "Couldn't restart onboarding. Try again.",
  },

  onboarding: {
    welcome: {
      // Founder note v3 — founder-authored on 2026-05-18. Four short
      // paragraphs, dual signature (Ziyang & Jiahao). Body translates
      // per locale; "memax" stays English. Server-side fallback in
      // packages/server/internal/onboarding/emitter.go kept in sync.
      title: "Hey, welcome to memax.",
      paragraph1:
        "We're so excited you're here! We're a two-person team building the shared brain for you, your AI, and your team.",
      paragraph2:
        "memax is early, but it does an awesome job memorizing and organizing everything dumped here — no matter whether it's through your agent, your teammate, or you.",
      paragraph3:
        "Start by connecting your agents to sync all the setups and memories, or simply start dumping something here yourself — a wiki, a .md file, a note, a thought, a link. Don't organize it. That's memax's job.",
      paragraph4:
        "Things might break, but it'll get smarter as you use it, and we'll work very hard to make your experience smoother. We hope you enjoy using memax.",
      signature: "— Ziyang & Jiahao",
      ctaLabel: "Start dumping & remembering",
      dismissAria: "Dismiss welcome note",
    },
    checklist: {
      title: "Your first week",
      progressFormat: "{done} of {total} done",
      celebrateTitle: "You're all set up",
      celebrateSubtitle: "memax dreams nightly now. See you tomorrow.",
      stripOpen: "Open",
      collapseAria: "Collapse checklist",
      dismissAria: "Dismiss checklist",
      lockedHint: "Unlocks after you dump 5 memories.",
      items: {
        welcome: {
          title: "Read the welcome note",
          description: "90 seconds. Worth it.",
          completedTitle: "Read the welcome note",
          ctaLabel: "Read it",
        },
        connect_agent: {
          title: "Connect your first agent",
          description:
            "Claude Code, Cursor, Codex — one command, memory flows both ways.",
          completedTitle: "Connected your first agent",
          ctaLabel: "Set up",
        },
        first_memory: {
          title: "Dump your first memory",
          description:
            "Type anything in the bar, hit ⌘↵. A wiki, a link, a half-thought.",
          completedTitle: "Dumped your first memory",
          ctaLabel: "Try the bar",
        },
        first_ask: {
          title: "Ask memax a question",
          description:
            "Press ↵ instead of ⌘↵ — memax answers from everything you've dumped.",
          completedTitle: "Asked your first question",
          ctaLabel: "Try asking",
        },
        five_memories: {
          title: "Dump 5 memories",
          description:
            "memax needs a critical mass to start seeing patterns. Unlocks dreams.",
          completedTitle: "Dumped 5 memories",
        },
        first_hub_invite: {
          title: "Join or start a team hub",
          description:
            "Shared brain with people you work with. Jump in or start your own.",
          completedTitle: "Joined a team hub",
          ctaLabel: "Start a hub",
        },
        first_dream: {
          title: "Let memax dream",
          description:
            "Dreams stitch your memories together. Tap to run your first one.",
          completedTitle: "Ran your first dream",
          // Single-use: row hides the button on optimistic complete
          // after a successful trigger, so a user can't spam. Also,
          // dreams no longer auto-tick this item from other surfaces
          // — only this CTA completes it (per user direction).
          ctaLabel: "Run a dream",
          // Shown beneath the row title after the user clicks the
          // CTA. The trigger fires fast (HTTP returns in <1s) but the
          // actual dream takes 30–90s; without this caption the user
          // sees the row check off instantly with no signal the work
          // is still happening. Persists for the session — no timer,
          // no SSE handshake needed.
          dreamingHint: "memax is dreaming — check back in a minute.",
        },
      },
    },
    // Visual tag for the four onboarding-seed memory cards (plan 23
    // tutorial curriculum). Without this chip the seeds read as
    // "did I sync this from somewhere?" — they're memax-authored
    // tutorial content, not user notes. Rendered on
    // MemoryCardLayout when memory.source_kind === "onboarding-seed".
    seedTag: {
      label: "tutorial",
      tooltip: "Sample memory from memax to show you the ropes.",
    },
  },

  board: {
    actionAck: "Got it",
    actionDismiss: "Not interested",
    receiptAcked: "Noted",
    receiptDismissed: "Dismissed",
    kindTrace: "Traces · last {n} hours",
    kindPulse: "Topic pulse · last {n} days",
    kindCapsule: "One year ago today",
    kindWeek: "This week",
    traceCountOne: "1 memory",
    traceCount: "{n} memories",
    traceManual: "Captured by hand",
    traceLatest: "Latest: “{title}”",
    traceAck: "All correct · got it",
    capsuleAck: "I remember",
    pulseRecentOne: "1 new memory",
    pulseRecent: "{n} new memories",
    pulseContributors: "{n} people",
    weekLineOne: "1 memory this week",
    weekLine: "{n} memories this week",
    weekCompare: "Last week: {n}",
  },
  inbox: {
    title: "Inbox",
    open: "Open Inbox",
    pageSubtitle: "Things memax needs you to decide",
    pendingHint: "{n} things are waiting for your review",
    expandReview: "Expand review",
    collapseReview: "Collapse review",
    viewAll: "{n} waiting · open full inbox",
    emptyTitle: "Inbox is quiet",
    emptySubtitle: "Dreams are out wandering",
    lastRun: "Last run {time} · {n} processed",
    loadFailed: "Couldn't open Inbox right now.",
    loadFailedHint: "Reviews are temporarily unavailable. Try again.",
    kindHubInvite: "HUB INVITE",
    kindHubInviteAccepted: "INVITE ACCEPTED",
    kindHubInviteDeclined: "INVITE DECLINED",
    kindHubInviteDeclinedByYou: "INVITE DECLINED",
    kindHubMemberJoined: "NEW MEMBER",
    kindHubOwnershipTransfer: "OWNERSHIP TRANSFER",
    kindHubOwnershipTransferred: "OWNERSHIP CHANGED",
    kindSystemNotice: "NOTICE",
    kindGiftInvite: "GIFT",
    kindHubOverLimit: "HUB OVER LIMIT",
    kindHubFrozen: "HUB FROZEN",
    kindHubRestored: "HUB RESTORED",
    topicMergeOneTitle: "Merge {source} into {target}?",
    topicMergeManyTitle: "Merge {sources} into {target}?",
    topicMergeFallbackTitle: "Merge topics into {target}?",
    topicRestructureTitle: "Move {child} under {parent}?",
    hubInviteBody: "invited you to",
    hubInviteRoleLabel: "role",
    hubInviteAnonymous: "Someone",
    hubMemberJoinedBody: "joined",
    hubInviteAcceptedBody: "You joined",
    hubInviteDeclinedBody: "declined your invite to",
    hubInviteDeclinedByYouBody: "You declined the invite to",
    hubOwnershipTransferBody: "wants to transfer ownership of",
    hubOwnershipTransferHint:
      "Accepting makes you the new owner; the current owner becomes an admin.",
    hubOwnershipTransferredSelfTitle: "You are now the owner of {hub}",
    hubOwnershipTransferredOtherTitle: "{owner} is now the owner of {hub}",
    hubOwnershipTransferredBody: "is now the owner of",
    hubOwnershipTransferredSelfBody: "You are now the owner of",
    giftInviteBody: "sent you a memax invite",
    giftInviteOpenLink: "Open invite link",
    reviewStaleNote: "Stale memory review — renderer ready, no producer yet.",
    reviewLowConfidenceNote:
      "Low-confidence review — renderer ready, no producer yet.",
  },

  // Time formatting
  time: {
    now: "now",
    m: "{n}m",
    h: "{n}h",
    d: "{n}d",
    w: "{n}w",
    justNow: "just now",
    mAgo: "{n}m ago",
    hAgo: "{n}h ago",
    dAgo: "{n}d ago",
    wAgo: "{n}w ago",
    moAgo: "{n}mo ago",
  },

  // Hubs
  hubs: {
    personal: "Personal",
    defaultPersonalHubName: "My memax",
    team: "Team",
    general: "General",
    membersTab: "Members",
    all: "All",
    switchTo: "Switch to {name}",
    switchTitle: "Switch hub",
    switchedTo: "Switched to {name}",
    switchingTo: "Switching to {name}…",
    showRecentHint: "Show {name}'s recent",
    pushTo: "→ {name}",
    joinedHub: "Welcome to {name}",
    active: "Active",
    create: "Create hub",
    createTeam: "Create team hub",
    createDesc:
      "Your team will share a knowledge base. Invite members after creation.",
    createName: "Hub name",
    changeIcon: "Change hub icon",
    iconPlaceholder: "Emoji",
    accentLabel: "Background color",
    accentViolet: "Violet",
    accentBlue: "Blue",
    accentGreen: "Green",
    accentAmber: "Amber",
    accentRose: "Rose",
    accentSlate: "Slate",
    members: "{n} members",
    memberOne: "1 member",
    noMembers: "No members yet",
    owner: "Owner",
    admin: "Admin",
    member: "Contributor",
    reader: "Viewer",
    noTeams: "No team hubs yet",
    leaveHub: "Leave hub",
    leaveConfirm: "Leave this hub?",
    leaveConfirmYes: "Leave",
    deleteHub: "Delete hub",
    deleteConfirm: "Delete this hub? All shared memories will be lost.",
    deleteConfirmYes: "Delete",
    inviteExpired: "This invite has expired",
    inviteUsed: "This invite has been used",
    inviteNotFound: "Invite not found",
    joinHub: "Join {name}",
    joinDesc: "Recall and contribute to your team's shared brain.",
    joinButton: "Join {name}",
    invitedBy: "Invited by {name}",
    loginToJoin: "Sign in to join",
    joined: "You're in.",
    redirecting: "Redirecting to your team's memory hub...",
    inviteLink: "Invite link",
    inviteHeaderTrigger: "Invite",
    invitePopoverTitle: "Invite to {name}",
    inviteSentInline: "Sent to {email}",
    manageInvites: "Manage invites",
    generateLink: "Generate link",
    inviteError: "Could not generate invite link.",
    inviteByEmail: "Invite by email",
    inviteByEmailTitle: "Invite by email",
    inviteByEmailHint: "They'll receive an email invitation to join this hub.",
    inviteExistingUser: "Invite existing user",
    inviteExistingUserTitle: "Invite an existing memax user",
    inviteExistingUserHint:
      "Find them by email. Works only for people who already have a memax account.",
    inviteEmailPlaceholder: "email address",
    inviteEmailRequired: "Enter an email address.",
    inviteEmailInvalid: "That doesn't look like a valid email.",
    inviteSend: "Send invite",
    inviteSending: "Sending...",
    inviteAlreadyMember: "That user is already a member of this hub.",
    inviteeNotFound:
      "No memax account found for that email. Use a link invite instead — they can sign up when they open it.",
    inviteLimit:
      "Invite limit reached — revoke unused invites or wait for them to expire.",
    inviteSentExisting: "Invite sent to {email}",
    inviteSentNew: "Email invite sent to {email}",
    inviteUseLinkInstead: "Link instead",
    inviteUseEmailInstead: "Email instead",
    emailQueued: "Email queued",
    emailQueuedAt: "Invited {date}",
    anyoneWithLink: "Anyone with link",
    resend: "Resend",
    resending: "Resending...",
    resent: "Resent",
    copyLink: "Copy",
    copied: "Copied!",
    inviteFirst: "Invite your first teammate",
    inviteFirstDesc: "Share a link to start building shared knowledge.",
    hubNameTaken: "Hub name already taken.",
    // New: hub management
    hubMemories: "{n} memories",
    hubMemoryOne: "1 memory",
    you: "(you)",
    contributor: "Contributor",
    viewer: "Viewer",
    removeMemberConfirm: "Remove {name}?",
    remove: "Remove",
    removing: "Removing...",
    inviteAs: "Invite as",
    generating: "Generating...",
    creating: "Creating...",
    revokeInviteConfirm: "Revoke this invite link?",
    revoke: "Revoke",
    revoking: "Revoking...",
    regenerate: "Regenerate",
    expiresOn: "Expires {date}",
    deleteConfirmFull:
      "Permanently delete {name} and all {memories} memories. {members} members will lose access.",
    deletePersonalSafe: "Personal memories are not affected.",
    deleteForever: "Delete forever",
    leaving: "Leaving...",
    deleting: "Deleting...",
    loadInviteFailed: "Could not load invite.",
    acceptInviteFailed: "Could not accept invite.",
    leaveFailedTransfer: "Transfer ownership before leaving this hub.",
    leaveFailedDelete: "Delete the hub instead if you are the last member.",
    transferTitle: "Ownership",
    transferDescription:
      "The owner can transfer the hub to an existing member.",
    transferTo: "Transfer to {name}",
    transferPending: "Transfer pending",
    transferPendingDesc:
      "{name} needs to accept ownership before the role changes.",
    transferAccept: "Accept ownership",
    transferDecline: "Decline",
    transferCancel: "Cancel transfer",
    transferInitiate: "Start transfer",
    transferEmpty: "Choose a member to transfer ownership.",
    transferSuccess: "Ownership transferred to {name}",
    transferRequested: "Ownership transfer sent to {name}",
    transferCanceled: "Ownership transfer canceled",
    roleLabel: "Role",
    deleteHubDesc:
      "Deletes this team hub and all shared memories in it. Personal memories are not affected.",
    lastMemberDelete: "You are the last member. Leaving would delete this hub.",
    noTransferTargets: "No eligible members to transfer ownership to.",
    overviewTitle: "Team hubs",
    hubsListLabel: "Hubs",
    overviewDescription:
      "Open a hub to manage members, roles, invites, permissions, and ownership.",
    summaryRole: "Your role",
    summaryMembers: "Members",
    summaryMemories: "Shared memories",
    // Contributor permissions
    permissions: "Contributor permissions",
    deletePolicy: "Memory deletion",
    deletePolicyNone: "No deletion",
    deletePolicyNoneDesc: "Contributors cannot delete any memories",
    deletePolicyOwn: "Own memories only",
    deletePolicyOwnDesc: "Contributors can delete memories they contributed",
    deletePolicyAny: "All memories",
    deletePolicyAnyDesc: "Contributors can delete any memory in this hub",
    allowTopics: "Manage topics",
    allowTopicsDesc: "Contributors can create and organize topics",
    allowDreams: "Trigger dreams",
    allowDreamsDesc: "Contributors can run memory consolidation",
  },

  hubRoute: {
    notFound: {
      title: "Hub not found",
      description:
        "This hub doesn't exist, or you don't have access. Check the link or pick a hub you belong to.",
      backToPersonal: "Go to Personal",
    },
  },

  hubHeader: {
    previewName: "You",
    status: {
      unavailable: "Couldn't load this hub summary.",
      unavailableHint:
        "The page can still load, but the header needs the new summary endpoint.",
    },
    // Substituted for {name} when the account has no display name —
    // must read naturally inside every greeting template below.
    greetingNameFallback: "there",
    greeting: {
      reviewNeededA:
        "Good evening, {name}. {n} merge conflicts need your call.",
      reviewNeededB:
        "Good evening, {name}. {n} merge conflicts are waiting on you.",
      dreamDeltasA: "{name}, memax reorganized {n} topics overnight.",
      dreamDeltasB: "{name}, {n} topics were reorganized overnight.",
      inboxOverflowA:
        "Good afternoon, {name}. {n} items waiting in your inbox.",
      inboxOverflowB:
        "Good afternoon, {name}. Your inbox still has {n} to sort.",
      morningDreamA:
        "Good morning, {name}. Dreams reorganized {n} topics overnight.",
      morningDreamB:
        "Good morning, {name}. {n} topics shifted while you slept.",
      morningCleanA: "Good morning, {name}. Everything's in order.",
      morningCleanB: "Good morning, {name}. Everything feels settled.",
      afternoonCleanA: "Good afternoon, {name}. Everything's in order.",
      afternoonCleanB: "Good afternoon, {name}. Everything feels settled.",
      afternoonInboxA:
        "Good afternoon, {name}. {n} items waiting in your inbox.",
      afternoonInboxB:
        "Good afternoon, {name}. Your inbox still has {n} to sort.",
      eveningReviewA:
        "Good evening, {name}. {n} merge conflicts need your call.",
      eveningReviewB:
        "Good evening, {name}. {n} merge conflicts are waiting on you.",
      deepNightCleanA: "Late night, {name}. Quiet thoughts travel far.",
      deepNightCleanB: "Late night, {name}. Quiet thoughts feel sharp.",
      returnAfterAbsenceA:
        "Welcome back, {name}. What have you been thinking about?",
      returnAfterAbsenceB:
        "Welcome back, {name}. What's been on your mind lately?",
      firstTimeA: "Welcome to memax, {name}.",
      firstTimeB: "Welcome to memax, {name}. Let's begin.",
      teamMorningA: "Good morning, {name}. Team added {n} memories today.",
      teamMorningB:
        "Good morning, {name}. The team added {n} new memories today.",
      teamReviewA: "{name}, {n} merge conflicts need your call.",
      teamReviewB: "{name}, {n} conflicts are waiting for your decision.",
      timeAfternoonA: "Good afternoon, {name}.",
      timeAfternoonB: "Afternoon, {name}.",
      timeEveningA: "Good evening, {name}.",
      timeEveningB: "Evening, {name}.",
      timeNoonA: "Good afternoon, {name}.",
      timeNoonB: "Midday, {name}.",
    },
    stats: {
      unassigned: "{n} unassigned",
      pendingReview: "{n} to review",
      cta: "Start with your first memory",
    },
  },

  // Dreams
  dreams: {
    title: "Memory Dreams",
    noRuns: "Dreams haven't run yet. They run automatically each night.",
    merged: "{n} duplicates merged",
    contradictions: "{n} similar memories got tangled",
    archived: "{n} stale notes archived",
    organized: "{n} memories organized",
    restructured: "{n} topics restructured",
    clean: "Your memory is clean — nothing to consolidate.",
    organize: "Organize",
    organizing: "organizing...",
    lastRun: "Last run {time}",
    running: "Dreaming...",
    // Notification (bar banner)
    dreamingTitle: "Dreaming...",
    barDreamingTitle: "memax is dreaming",
    barDreamingDetail: "scanning {n} memories",
    scanningCount: "scanning {n} memories",
    notificationTitle: "memax dreamed",
    notificationCleanTitle: "memax dreamed — everything held together",
    notificationPartialTitle: "memax dreamed — finished with some issues",
    notificationCleanBody:
      "Nothing needed merging, restructuring, or review this time.",
    notificationMerged: "{n} merged",
    notificationContradictions: "{n} tangled",
    notificationArchived: "{n} archived",
    notificationOrganized: "{n} organized",
    barCompleteTitle: "memax dreamed — {n} changes surfaced",
    barCleanTitle: "dreams are clean — nothing to review",
    barPartialTitle: "memax dreamed — finished with some issues",
    barDetailLastNight: "last night · {n} memories",
    barView: "View",
    viewReport: "View",
    // Dream inbox
    report: "Dream report",
    needsInput: "Needs your input",
    whatMemaxDid: "What memax did",
    narrative: "Dream narrative",
    scanned: "{n} memories scanned",
    mergeAction: "Merged {n} notes about {topic}",
    archiveAction: "Archived stale note",
    contradictionAction: "Two similar memories got tangled",
    organizeAction: "Organized into {topic}",
    organizeReceipt: "Organized {n} memories into {topics} topics",
    topicCreated: "Created new topic",
    conflictTitle: "Conflict · {topic}",
    similarity: "{n}% similar",
    kept: "kept",
    mergedLabel: "merged",
    reviewNeeded: "Review →",
    noteA: "Note A",
    noteB: "Note B",
    dismissed: "Dismissed",
    resolved: "Resolved",
    // Dream history view (settings → You → Intelligence → history).
    // Cursor-paginated log of past dream runs, scoped to the caller's
    // hubs. Kept under dreams.history.* so all dream copy lives in
    // one namespace.
    history: {
      entry: "View dream history →",
      title: "Dream history",
      back: "← Back to Intelligence",
      hubScopeAll: "All hubs",
      hubScopeLabel: "Filter",
      emptyTitle: "No dream runs yet",
      emptyBody:
        "memax will dream tonight at ~3am UTC if your plan supports it.",
      errorTitle: "Couldn't load dream history",
      errorRetry: "Retry",
      loadMore: "Load more",
      loadingMore: "Loading more runs…",
      expandAriaLabel: "Show dream from {when}",
      collapseAriaLabel: "Hide dream from {when}",
      runTimeLabel: "Ran {when}",
      summaryNoActions: "No changes needed",
      summaryMerged: "{n} merged",
      summaryArchived: "{n} archived",
      summaryOrganized: "{n} organized",
      summaryRestructured: "{n} topics restructured",
      summaryContradictions: "{n} tangled",
      partialFailedHint: "Finished with some issues",
      skippedPrefix: "Skipped",
      statusCompleted: "Completed",
      statusPartialFailed: "Partial",
      statusSkipped: "Skipped",
      statusFailed: "Failed",
      statusRunning: "Running",
      statusStale: "Stale",
      phasesHeading: "Phase breakdown",
      phaseMemoriesScanned: "Memories scanned",
      phaseMerge: "Merge",
      phaseArchive: "Archive",
      phaseOrganize: "Organize",
      phaseRestructure: "Restructure",
      phaseContradictions: "Contradictions found",
    },
  },

  // Topics — knowledge organization
  topics: {
    title: "Your Topics",
    titlePersonal: "Your Topics",
    titleTeam: "Topics",
    howItWorksAria: "How topics work",
    howItWorksTitle: "How topics work",
    howItWorksBodyDesktop:
      "memax dreams about your memories and groups them into topics. Drag any memory between topics to refit it anywhere.",
    howItWorksBodyMobile:
      "memax dreams about your memories and groups them into topics. Open a memory to reassign it between topics.",
    allTitle: "All Topics",
    memories: "{n} memories",
    memoryOne: "1 memory",
    topics: "{n} topics",
    topicOne: "1 topic",
    subtopics: "{n} subtopics",
    subtopicOne: "1 subtopic",
    nNew: "{n} new",
    whatChanged: "What changed",
    sinceLastVisit: "since your last visit",
    sinceYouWereHere: "Since you were here",
    nothingOlder: "Nothing older here right now.",
    unassigned: "Inbox",
    dreamingNow: "memax is dreaming",
    dreamingNowHint: "If dream needs you, it will show up here.",
    dreamNewTopic: "✦ New topic from last night's dream",
    needsReview: "Needs your review",
    comeBackToThese: "Come back to these",
    unassignedHint: "Will be organized in the next dream cycle",
    unassignedCount: "{n} unassigned",
    firstRun: "✦ memax organized your memories into {n} topics",
    organizing: "Dump memories, memax does the rest",
    organizingHint:
      "Just dump anything — notes, links, thoughts. memax will dream about them overnight and organize everything into topics for you.",
    dreamsOff: "Dreams are turned off",
    dreamsOffHint:
      "Turn on dreams and memax will organize your memories into topics overnight — just dump and go.",
    dreamsOffEnable: "Enable in Settings →",
    dreamsOffInbox: "Enable dreams to auto-organize your inbox",
    create: "New topic",
    createTitle: "New topic",
    createSubtopicTitle: "New subtopic in {name}",
    createNamePlaceholder: "Topic name",
    createSubmit: "Create",
    creating: "Creating…",
    createCancel: "Cancel",
    delete: "Delete topic",
    deleteConfirm: 'Delete "{name}"? Memories will become unassigned.',
    deleteConfirmShort: "Delete this topic? Memories become unassigned.",
    deletePending: "Deleting…",
    deleteKeep: "Keep",
    deleted: "Topic deleted",
    rename: "Rename",
    archive: "Archive topic",
    archiveToast: "Topic archived",
    archiveToastDetail: "Memories stay assigned — restore anytime.",
    restore: "Restore",
    restoreToast: "Topic restored",
    restoring: "Restoring…",
    archivedSection: "Archived",
    archivedCount: "{n} archived",
    archivedOne: "1 archived",
    archivedAt: "Archived {time}",
    moreActions: "Topic actions",
    pinned: "Pinned",
    pin: "Pin topic",
    unpin: "Unpin topic",
    locked: "Locked",
    tree: "Topic Explorer",
    treeTitle: "Topic Explorer",
    treeClose: "Close tree",
    treePin: "Pin sidebar",
    treeOpen: "Open topic explorer",
    brandHome: "memax — home",
    newSubtopic: "New subtopic",
    back: "Your Topics",
    hubBack: "{name}",
    empty: "No memories in this topic yet.",
    noTopics: "No topics yet",
    setTopic: "Set topic",
    changeTopic: "Change topic",
    clearTopic: "Clear topic",
    moveToTopic: "Move to topic",
    moving: "Moving…",
    movePickerHint:
      "Select a topic to move here, or use the arrow for subtopics.",
    movePickerBrowseAction: "Browse subtopics",
    movePickerBrowseSubtopics: "Browse subtopics in {name}",
    movePickerTopicOnly: "Move here · {memories} in this topic",
    movePickerSubtopicsOnly: "Move here · {subtopics} subtopics",
    movePickerTopicWithChildren:
      "Move here · {memories} in this topic · {subtopics} subtopics",
    movePickerEmpty: "Move here",
    moveToHub: "Move to {name}",
    loadingHubTopics: "Loading topics...",
    noOtherTopics: "No other topics",
    topicLabel: "topic",
    other: "Other",
    viewLabel: "Topics view",
    viewGrid: "Grid",
    viewDense: "Dense",
    viewList: "List",
    lastTouched: "Last touched {time}",
    activityWindow: "Last {n} days",
    activityDelta: "+{n}",
    showNMore: "Show {n} more",
    collapse: "Collapse",
    openFullView: "Open full view",
    openFullViewCount: "Open full view ({n})",
    showMore: "+{n} more",
    loadFailed: "Couldn't load your topics right now.",
    loadFailedHint:
      "Topic grouping and inbox counts are temporarily unavailable.",
    detailFailed: "Couldn't open this topic right now.",
    detailFailedHint:
      "The topic header or structure didn't come through. Try again.",
    memoriesFailed: "Couldn't load memories for this topic.",
    memoriesFailedHint:
      "The topic is here, but its memory list didn't load this time.",
    treeFailed: "Couldn't load the topic tree right now.",
    treeFailedHint:
      "Browse is temporarily unavailable until topics load again.",
    // Topic move / reparent (drag-to-reparent + ⋮ menu picker)
    moveTopic: "Move topic",
    topicMovedUnderParent: "Moved {name} under {parent}.",
    // Root destination for topic moves from drag/drop and the ⋮ picker.
    // The root of a tree IS the hub, so the toast and the drop-row label
    // both use the current hub name rather than the internal tree term
    // "top level". Shared with memory drops on the same row (whose
    // success toast uses t.toast.movedTo instead, same hub name).
    topicMovedToHub: "Moved {name} to {hub}.",
    // Neutral fallback for when the current hub name can't be resolved
    // (e.g. auth hasn't hydrated yet at toast time). Used by both the
    // hub-root drag path and the ⋮ picker's root option as a safety net
    // so we never emit an empty destination.
    topicMoved: "Moved {name}.",
    topicMoveFailed: "Couldn't move the topic.",
    topicCycleDetected: "A topic can't move into one of its own descendants.",
    topicMaxDepthReached:
      "That move would exceed the 5-level topic depth limit.",
    topicInvalidParent: "That destination isn't available anymore.",
    topicMoveUndone: "Move undone.",
    topicMoveUndoFailed: "Couldn't undo the move.",
    dragHandleAria: "Drag {name} to reparent",
    cannotDropHere: "Can't drop {name} here",
    expandToDropInside: "Hold to expand",
  },

  // Attribution verbs (memory row context labels)
  attribution: {
    you: "You",
    via: "via",
    with: "with",
    pushed: "saved",
    saved: "saved",
    captured: "captured",
    pushedVia: "saved with {agent}",
    youPushed: "You saved",
    youViaPushed: "You saved with {agent}",
    withAgent: "with {agent}",
    fromAgent: "From {agent}",
    capturedByAgent: "Captured by {agent}",
    savedWithAgentPrefix: "saved with",
    savedWithAgentSuffix: "",
    capturedByAgentPrefix: "captured by",
    capturedByAgentSuffix: "",
  },

  // Lifecycle signals — passive browse-context changes since last visit.
  // Distinct from notifications (attention/review events). Contradictions
  // and dream-run-completed stay on the notification surface; lifecycle
  // only renders the allowed passive action set.
  lifecycle: {
    topicDeltaSummary: "+{added} · {reorganized} reorganized",
    topicDeltaHeading: "Since you last visited",
    topicDeltaAdded: "{n} new memories added",
    topicDeltaReorganized: "{n} reorganized into other topics",
    topicDeltaHint: "memax's dream pass updates this map as your corpus grows.",
    dreamActionVerb: {
      organize: "organized",
      merge: "merged with another memory",
      archive: "archived",
      restructure: "reorganized",
    },
    dreamActionFromTo: "moved from {from} → {to}",
    dreamActionTo: "placed in {to}",
    dreamHistoryHeading: "Reorganized by dream",
    unassigned: "Unassigned",
    popoverHeader: {
      organize: "Moved by dream",
      merge: "Merged by dream",
      archive: "Archived by dream",
      restructure: "Reorganized by dream",
    },
  },

  // Reviews
  reviews: {
    title: "Review",
    empty: "Nothing to review.",
    contradiction: "These memories got tangled",
    kindContradiction: "Tangled",
    kindStale: "Stale",
    kindLowConfidence: "Unsure",
    kindTopicMerge: "Merge topics",
    kindTopicRestructure: "Move topic",
    keepA: "Keep first",
    keepB: "Keep second",
    keepBoth: "Keep both",
    merge: "Merge",
    keepSeparate: "Keep separate",
    apply: "Move it",
    keep: "Leave it",
    accept: "Accept",
    decline: "Decline",
    dismiss: "Dismiss",
    resolved: "Resolved",
    topicMergeBody: "Combine these topics into one.",
    topicRestructureBody: "Reorganize where this topic lives.",
    topicMergeFailed: "Couldn't merge those topics.",
    topicRestructureFailed: "Couldn't move that topic.",
    topicMemoryCount: "{n} memories",
    topicMemoryCountOne: "1 memory",
    topicUntitled: "(untitled topic)",
    topicRemoved: "(removed topic)",
    kindUnsupportedYet: "This review kind isn't actionable yet.",
  },

  // User settings
  userSettings: {
    title: "Settings",
    closeAria: "Close settings",
    plan: "Plan",
    displayName: "Display name",
    usage: "Usage this month",
    pushes: "{n} pushes",
    recalls: "{n} recalls",
    asks: "{n} asks",
    dreamsEnabled: "Memory Dreams",
    dreamsEnabledDesc: "Nightly dedup, contradiction detection, archival",
    mergeEnabled: "Auto-merge duplicates",
    archiveEnabled: "Auto-archive stale notes",
    organizeEnabled: "Auto-organize into topics",
    restructureEnabled: "Restructure topic tree",
    restructureEnabledDesc:
      "Group related root-level topics into hierarchies over time.",
    runDreamNow: "Run a dream now",
    runDreamNowRunning: "Dreaming now…",
    // Shown beneath the trigger button when the hub's
    // subscription is cancelled, past_due, or past its
    // over-limit grace window. Visible (not tooltip) so the
    // reason reaches mobile users too.
    runDreamNowFrozenHint: "Hub is frozen — resolve billing to resume dreams.",
    appearance: "Appearance",
    theme: "Theme",
    themeDesc: "Choose how the app renders across light, dark, and system.",
    hubHeaderMode: "Hub banner",
    hubHeaderModeDesc: "Choose how this hub's page feels at the top.",
    hubHeaderModeTeamDesc:
      "Everyone on this team hub will see the banner you pick.",
    hubHeaderModeTeamViewerDesc:
      "Only hub owners and admins can change the banner.",
    hubHeaderModeNone: "Plain",
    hubHeaderModeNoneDesc: "Typography only. No ambient banner.",
    hubHeaderModeSignature: "Signature",
    hubHeaderModeSignatureDesc:
      "Static memax gradient. Stable across time and teams.",
    hubHeaderModeTime: "Match time of day",
    hubHeaderModeTimeDesc:
      "Banner drifts with the local time — dawn, day, dusk, night.",
    language: "Language",
    light: "Light",
    lightDesc: "Always use the light appearance.",
    dark: "Dark",
    darkDesc: "Always use the dark appearance.",
    auto: "Auto",
    autoDesc: "Follow your device or browser setting.",
    workspace: "Workspace",
    personalHub: "Personal hub",
    personalHubDesc: "This is the default hub for your private memories.",
    connectedAccounts: "Connected accounts",
    connectedAccountsDesc:
      "Use any connected provider to sign in to this memax account.",
    providerGithub: "GitHub",
    providerGoogle: "Google",
    providerConnected: "Connected",
    providerNotConnected: "Not connected",
    connectProvider: "Connect",
    disconnectProvider: "Disconnect",
    lastProvider: "Last login method",
    dangerZone: "Danger zone",
    account: "Account",
    teams: "Teams",
    agents: "Agents",
    intelligence: "Intelligence",
  },

  // Settings scope switcher
  settingsScope: {
    you: "You",
    switchScope: "Switch scope",
    createHub: "Create team hub",
  },

  // API Keys management
  apiKeys: {
    title: "API Keys",
    create: "Create Key",
    label: "Label",
    scope: "Scope",
    global: "Global",
    origin: "Origin",
    manual: "Manual",
    auto: "Auto",
    lastUsed: "Last used",
    neverUsed: "Never used",
    revealOnce: "This key will only be shown once. Store it securely.",
    created: "Key created",
    emptyTitle: "No API keys",
    emptyDesc:
      "API keys authenticate agents and scripts to push and recall memories.",
    learnMore: "Learn how to use API keys →",
    done: "Done",
    cancel: "Cancel",
    creating: "Creating...",
    placeholder: "Claude Code on laptop, CI deploy...",
    summary: "{total} keys · {auto} auto · {manual} manual",
    summaryWithStandalone:
      "{total} keys · {linked} linked · {standalone} standalone · {unassigned} unassigned",
    justNow: "just now",
    // Agent attribution affordances on unassigned keys.
    agentLabel: "Agent (optional)",
    agentPlaceholderNone: "No agent",
    unassigned: "Unassigned",
    assignAgent: "Assign to agent",
    createAgent: "Create agent: {slug}",
    markStandalone: "Mark as standalone",
    clearAssignment: "Clear assignment",
    standalone: "Standalone",
    assigning: "Assigning...",
    updateFailed: "Couldn't update key — retry",
  },

  // Hub intelligence (dreams in hub scope)
  hubIntelligence: {
    dreamSettingsHub:
      "These settings control dream cycles for this hub only. Other hubs have their own Intelligence tab.",
    latestDreamHeading: "Latest dream in this hub",
    needsReviewHeading: "Needs review in this hub",
    foundContradictions: "{n} contradictions found",
    pendingTopicMerges: "{n} topic merges pending",
    pendingTopicRestructures: "{n} topic restructures pending",
    reviewClear: "This hub is clean right now",
    permissionDenied: "Dream triggering is disabled by the hub admin.",
    contactAdmin: "Contact an admin to change this.",
    readOnlyHint: "Only owners and admins can change these settings.",
    // Dream quota indicator copy (shown under the Dream now button).
    // {used}/{limit} for finite caps; unlimited variant has no
    // counter; exhausted variant explains why the button disabled.
    quotaUsed: "{used} of {limit} dreams this month",
    quotaUnlimited: "Unlimited dreams",
    quotaExhausted: "Out of dreams this month — resets {resetDate}",
    quotaDisabled: "Dreams aren't included in this hub's plan",
  },

  // Copy for the hub settings mutation (useUpdateHubSettings).
  // The previous per-hub-overrides panel (a You-scoped disclosure
  // list) was removed in the per-hub intelligence release — every
  // hub now has its own Intelligence tab — so this block is pared
  // down to the strings the mutation still uses.
  hubOverrides: {
    saveFailed: "Couldn't update hub settings",
  },

  // Hub members sub-sections
  hubMembers: {
    agentsWithAccess: "Agents with access",
    manageKeys: "Manage keys",
    keysScoped: "{n} keys scoped",
    keysScopedOne: "1 key scoped",
  },

  // Hub creation
  hubCreate: {
    title: "Create team hub",
    nameLabel: "Hub name",
    namePlaceholder: "e.g. Design crew ✦",
    slugLabel: "Hub handle",
    slugPrefix: "/hubs/",
    slugPlaceholder: "acme-engineering",
    slugPermanent: "This can't be changed later",
    slugAvailable: "Available",
    slugTaken: "Already taken",
    slugInvalid: "Only lowercase letters, numbers, and hyphens (4–50 chars)",
    slugReserved: "This handle is reserved",
    slugError: "Couldn't check availability",
    slugChecking: "Checking...",
    create: "Create Hub",
    creating: "Creating...",
    createFailed: "Failed to create hub",
    cancel: "Cancel",
  },

  // Import
  import: {
    title: "Remember files",
    dropzone: "Drop files here",
    dropzoneHint: "Supports .md, .txt, .cursorrules, and other text files",
    orPaste: "Or paste content",
    importing: "Remembering {current}/{total}...",
    doneOne: "Sent to memax.",
    undo: "Undo",
    doneOneHub: "Sent → {hub} — memax will organize.",
    doneMany: "Remembered {count} files.",
    failed: "Couldn't remember {name}",
    button: "Remember",
    hintLabel: "What is this? (optional)",
    hintPlaceholder: "e.g. Architecture decisions from last sprint",
    stagingOne: "1 file",
    stagingMany: "{count} files",
  },

  // Toast
  toast: {
    saved: "Remembered.",
    processing: "Remembering in the background",
    processingDesc: "This one takes a moment — it'll appear shortly.",
    saveFailed: "Couldn't save that",
    unsupportedFile: "Can't read that file type",
    dropFilesNotFolders: "Folders aren't supported — drop files instead",
    untitled: "untitled",
    // Universal error messages (from MutationCache global handlers)
    deleteFailed: "Couldn't delete that",
    updateFailed: "Couldn't save changes",
    shareFailed: "Couldn't share memory",
    revokeFailed: "Couldn't revoke key",
    linkProviderFailed: "Couldn't connect that account",
    unlinkProviderFailed: "Couldn't disconnect that account",
    moved: "Moved.",
    movedTo: "Moved to {name}.",
    topicCleared: "Topic cleared.",
    forgot: "Forgot.",
    undoing: "Undoing move...",
    moveUndone: "Move undone.",
    moveUndoFailed: "Couldn't undo that move",
    providerDisconnected: "Account disconnected",
    disconnectFailed: "Couldn't disconnect agent",
    organizeQueued: "Dream cycle queued",
    organizeFailed: "Couldn't start organizing",
    dreamTriggerFrozen:
      "Dreams paused — this hub is frozen. Resolve the over-limit or subscription state to resume.",
    dreamTriggerForbidden:
      "You don't have permission to trigger dreams for this hub.",
    dreamTriggerStarted:
      "Dream queued — memax will start it if this hub is eligible.",
    dreamTriggerAlreadyRunning: "A dream is already running for this hub.",
    topicFailed: "Couldn't update topic",
    reviewFailed: "Couldn't resolve review",
    settingsFailed: "Couldn't update settings",
    disconnected: "Agent disconnected",
    shared: "Memory shared",
  },

  // Misc
  misc: {
    now: "now",
    legacy: "Legacy",
    default: "Default",
  },

  // States
  states: {
    error: {
      default: "Something went wrong",
      network: "Couldn't reach memax. Check your connection and try again.",
      unexpected: "memax lost this page for a moment",
      unexpectedDetail:
        "Your memories are safe. Reload and we'll pick it back up.",
      retry: "Try again",
    },
    empty: {
      firstTime: {
        title: "No memories yet",
        subtitle: "Type a thought · ⌘↵ to remember",
        cta: "Remember something",
      },
      filtered: {
        title: "No matching memories",
        cta: "Show all",
      },
      transient: "No results",
    },
    pagination: {
      allLoaded: "{count} memories · all loaded",
    },
  },

  // Memory view
  memoryView: {
    freshMemory: {
      zero: "Fresh memories",
      one: "Fresh memories",
      other: "Fresh memories",
    },
    remembering: "remembering",
    recentEmptyTitle: "Nothing new yet",
    recentEmptyHint: "Capture something and it'll show up here",
    recentFilteredTitle: "No memories in the past {window}",
    recentFilteredHint: "Try widening the window or switching actor",
    recentErrorTitle: "Couldn't reach your recent memories",
    recentErrorDetail: "Connection timed out. Your memories are safe.",
    recentErrorRetry: "Reload recent",
    switchToRows: "Switch to row view",
    switchToCards: "Switch to card view",
    filterPast: "Past {window}",
    filterPastLabel: "Filter recent window",
    filterBy: "Filter",
    filterTimeLabel: "Time",
    filterActorLabel: "Actor",
    filterActorAll: "All actors",
    filterActorYou: "You",
    filterReset: "Clear all",
    copyContent: "Copy",
    copiedContent: "Copied",
    loadingMore: "Loading more...",
    loadMoreRecent: "Show {n} more",
    collapseRecent: "Collapse",
    scrollToLoad: "Scroll to load more",
    copyForAI: "Copy for AI",
    download: "Download",
    recalledCount: "recalled {n}×",
    clearFilters: "Clear all",
  },

  // Auth
  auth: {
    tagline: "A memory home for you and your AI agents",
    continueWithGoogle: "Sign in with Google",
    continueWithGithub: "Sign in with GitHub",
    continueWithEmail: "Sign in with email",
    legal: "By continuing, you agree to our",
    terms: "Terms",
    privacy: "Privacy Policy",
    callbackLoading: "Connecting to your memory...",
    callbackSuccess: "You're in",
    callbackError: "Something went wrong",
    callbackErrorDesc: "We couldn't complete sign in.",
    emailSignIn: {
      orDivider: "or",
      emailHeading: "Sign in with email",
      emailSubheading:
        "We'll send a 6-digit code to your inbox. Works with any email — no GitHub or Google needed.",
      emailLabel: "Work email",
      emailPlaceholder: "you@company.com",
      sendCode: "Send code",
      sending: "Sending code…",
      codeHeading: "Check your email",
      codeSubheading: "We sent a 6-digit code to {email}.",
      codeLabel: "Sign-in code",
      codePlaceholder: "123456",
      verify: "Verify and sign in",
      verifying: "Signing you in…",
      resend: "Resend code",
      resendIn: "Resend in {seconds}s",
      changeEmail: "Use a different email",
      back: "Back",
      errorInvalidEmail: "Enter a valid email address.",
      errorRateLimited:
        "Too many sign-in codes requested. Try again in a few minutes.",
      errorSendFailed: "We couldn't send the code. Please try again.",
      errorCodeInvalid:
        "That code doesn't match. Check your email and try again.",
      errorCodeFormat: "Enter the full 6-digit code from your email.",
      errorCodeExpired:
        "Your sign-in code expired or was already used. Send a new one.",
      errorCodeLocked:
        "Too many attempts. Request a new sign-in code to continue.",
      errorRegistrationRequired:
        "This email isn't allowed to sign up yet. Ask for an invite from your admin.",
      errorInvalidInvite:
        "Your invite is invalid or expired. Request a new one.",
      errorInviteEmailMismatch:
        "This invite was sent to a different email. Use the email that received the invite.",
      errorGeneric: "Something went wrong. Please try again.",
      didntGetIt: "Didn't get it? Check your spam folder.",
      pasteHint: "Tip: you can paste the full code.",
    },
    accountNotAllowedTitle: "Account not allowed",
    accountNotAllowedDesc:
      "This account isn't on memax's allowlist. Ask your workspace admin to add you, or sign in with a different account.",
    tryAgain: "Try again",
    backToSignIn: "Back to sign in",
    registrationRequired:
      "You don't have an account yet. Join the waitlist to get early access.",
    invalidInvite: "This invite is invalid or has expired.",
    inviteEmailMismatch:
      "This invite was sent to a different email. Please sign in with the email that received the invite.",
    requestNewInvite: "Request a new invite",
    joinWaitlist: "Join the waitlist",
    oauthConsent: {
      loading: "Loading authorization request...",
      errorTitle: "Authorization request unavailable",
      errorDescription:
        "This connection request may have expired. Go back to your agent and start again.",
      missingRequest: "Missing authorization request.",
      connectTitle: "Connect {client} to memax",
      connectSubtitle:
        "{client} can only use the capabilities and hubs you approve here.",
      resourceLabel: "MCP endpoint",
      expiresSoon: "This approval link expires soon.",
      capabilitiesTitle: "Capabilities",
      hubsTitle: "Hubs",
      hubsPersonalTitle: "Personal hub",
      hubsTeamTitle: "Team hubs",
      hubsTeamCount: "{count} selected",
      notRequestedTitle: "Not requested",
      notRequestedTitleAgent: "{client} won't access",
      safeDefaultsHint: "Safe defaults selected — adjust as needed",
      postApprovalNote:
        "After you approve, {client} appears in Settings → Integrations. You can revoke or change scope anytime.",
      essentialChip: "essential",
      readOnlyChip: "read-only",
      roleCannotUseChip: "role can't use",
      permissionReadTitle: "Read memories",
      permissionReadDesc:
        "Search, recall, list, and open memories in selected hubs.",
      permissionWriteTitle: "Write memories",
      permissionWriteDesc:
        "Save new memories and session captures into selected hubs.",
      permissionCustomTitle: "Custom capability",
      permissionCustomDesc: "This capability was requested by the agent.",
      capReadWrite: "Read and write available",
      capRead: "Read only",
      capWrite: "Write available",
      capUnavailable: "Unavailable for your role",
      memoriesCount: "{count} memories",
      personalHub: "Personal",
      teamHub: "Team",
      roleOwner: "Owner",
      roleAdmin: "Admin",
      roleContributor: "Contributor",
      roleViewer: "Viewer",
      notRequestedDelete: "Delete memories",
      notRequestedOrganize: "Manage topics or run dreams",
      notRequestedHubAdmin: "Manage hub settings or members",
      notRequestedCustom: "Another capability",
      deny: "Deny",
      approve: "Approve",
      approveAction: "Connect {client}",
      securityNote: "You can revoke this connection anytime in settings.",
      approveDisabled: "Select at least one capability and one hub.",
    },
  },

  // Dev tools (only visible to dev users)
  dev: {
    title: "Dev Tools",
    mockDreams: "Mock dream data",
    mockDreamsDesc: "Show sample dream report, actions, and reviews",
    mockDreaming: "Mock dreaming state",
    mockDreamingDesc: "Show active dreaming banner with progress",
    mockEmptyInbox: "Mock empty inbox",
    mockEmptyInboxDesc: "Show inbox as empty to preview clean state",
    mockProUser: "Mock Pro user",
    mockProUserDesc: "Auto-trigger AI on recall — skips the CTA",
    debuggerToggle: "Enable memax debugger",
    debuggerToggleDesc:
      "Show a live event tray for recall, AI, and remember flows",
    skipRerank: "Skip reranking",
    skipRerankDesc:
      "Use local scoring only — faster results, useful for debugging ranking",
    showChatCapabilities: "Show chat capabilities row",
    showChatCapabilitiesDesc:
      "Surface the per-tool chips strip above the chat composer — operator detail, useful when exercising a new tool",
    debuggerTitle: "memax debugger",
    debuggerDescription:
      "Live event timeline for key memax actions, with extra recall visibility.",
    debuggerLive: "Live",
    debuggerCollapse: "Collapse debugger",
    debuggerClear: "Clear events",
    debuggerEmptyTitle: "Waiting for memax activity",
    debuggerEmptyDescription:
      "Trigger recall, remember, or AI actions to inspect the live event trail.",
    debuggerRecall: "Recall",
    debuggerAI: "AI",
    debuggerRemember: "Remember",
    debuggerSystem: "System",
    // Stream-status header (PR A) — shown above the event list so
    // empty events + connected is an unambiguous live state.
    streamStateIdle: "Idle",
    streamStateConnecting: "Connecting\u2026",
    streamStateConnected: "Connected",
    streamStateReconnecting: "Reconnecting\u2026",
    streamStateDisconnected: "Disconnected",
    streamHubsSingular: "1 hub",
    streamHubsPlural: "{count} hubs",
    streamConnectedSinceLabel: "up",
    streamConnectedSinceTooltip:
      "Time since the current SSE connection was established (last `ready` event). Stays rising while the stream is alive.",
    streamLastEventLabel: "last event",
    streamLastEventTooltip:
      "Time since the most recent SSE frame (event or ping). Resets on every frame; on a quiet stream the server pings every ~25s.",
    streamConnectCountTooltip:
      "Number of connect() invocations since this session started. Healthy steady-state is 1. Higher numbers indicate server/network hang-ups forcing reconnects, or a bug re-mounting the bridge. Reset on logout.",
    debuggerEngaged: "Debugger engaged",
    streamConnectedWithHubs: "Stream connected · watching {count} hub(s)",
    streamIsState: "Stream is {state}",
    eventStreamReady: "Event stream ready",
    eventStreamUnknownError: "Unknown stream error",
    eventStreamCacheInvalidated: "Cache invalidated",
    eventStreamMemoriesChanged: "Memories changed",
    eventStreamStateUpdated: "updated",
    eventStreamDisconnected: "Event stream disconnected",
    eventStreamWatchingHubs: "Watching {count} accessible hub(s)",
    eventStreamWatchingLive: "Watching live updates",
    resetDreams: "Reset dream notification",
    triggerDream: "Trigger dream cycle",
    dreamTriggered: "Dream cycle queued",
    dreamTriggerFailed: "Couldn't trigger dream cycle",
    impersonate: "Impersonate user",
    impersonateDesc: "Sign in as another user to debug from their perspective",
    impersonatePlaceholder: "Email or user ID",
    impersonateButton: "Impersonate",
    impersonateSuccess: "Signed in as target user",
    impersonateFailed: "Could not impersonate user",
    impersonateNotAuth: "Not authenticated",
    impersonateLoading: "…",
    impersonateBanner: "Impersonating: {target} (you: {caller})",
    impersonateStop: "Stop impersonating",
    impersonateUnknown: "unknown user",
  },

  // Waitlist signup page
  waitlistPage: {
    headline: "Get early access",
    subtitle:
      "memax is rolling out in small waves. Join the waitlist and we'll email you when your invite is ready.",
    emailLabel: "Email",
    emailPlaceholder: "you@memax.app",
    useCaseLabel: "What will you use memax for?",
    useCaseOptions: {
      personal_memory: "Personal AI memory",
      team_knowledge: "Team / company knowledge",
      developer_tooling: "Developer tooling / MCP integrations",
      other: "Other",
    },
    aiToolsLabel: "Which AI tools do you use?",
    aiToolsOtherPlaceholder: "Which one?",
    aiToolOptions: {
      claude: "Claude",
      chatgpt: "ChatGPT",
      gemini: "Gemini",
      cursor: "Cursor",
      claude_code: "Claude Code",
      codex: "Codex",
      openclaw: "OpenClaw",
      other: "Other",
    },
    roleLabel: "Your role",
    roleOptions: {
      developer: "Developer / Engineer",
      founder: "Founder / Executive",
      product_design: "Product / Design",
      researcher: "Researcher",
      other: "Other",
    },
    submit: "Join the waitlist",
    submitting: "Joining...",
    successHeadline: "You're on the list",
    successMessage:
      "We'll email you when your invite is ready. No action needed on your end.",
  },

  // Registration page (invite flow)
  registerPage: {
    headline: "You're in",
    subtitle:
      "Your early access to memax is ready. Sign in to create your account.",
    expiry: "This invite expires on {date}.",
    expired: "This invite has expired.",
    invalid: "This invite link is invalid.",
    requestNew: "Request a new invite",
    backToWaitlist: "Back to waitlist",
  },

  // Landing page
  landing: {
    // ── Audience pivot — hero splits into personal / team branding ──
    pivotPersonal: "For you",
    pivotTeam: "For teams",
    // Rotating headline. Line 1 is a fixed possessive prefix; line 2 cycles
    // through hero*Words rendered into heroWordLine ({word} slot carries
    // per-locale punctuation). Whole-line rotation keeps the layout stable —
    // no mid-line wrap jumps at display sizes. "Every AI" lives in the
    // subline, so the headline stays two short lines.
    heroPersonalPrefix: "Your",
    heroPersonalWords: [
      "memory",
      "second brain",
      "AI persona",
      "fleeting ideas",
      "knowledge base",
    ],
    heroTeamPrefix: "Your team's",
    heroTeamWords: ["shared brain", "context", "decisions", "project pulse"],
    heroWordLine: "{word}.",
    sublineFull: "Dump anything. memax organizes. Every AI you use remembers.",
    sublineTeam:
      "Your teammate's AI knows what yours knows. Context, decisions, progress — shared across every agent.",
    // Team pivot contact — sales-touch alongside the self-serve waitlist.
    teamContactPrompt: "Setting up memax for your team?",
    teamContactCta: "Talk to us",
    sublineSafe:
      "Shared memory across Claude Code, Cursor, and every AI agent.",
    openMemax: "Give your AI a memory",
    // Hero waitlist CTA — morphs inline: email pill \u2192 full form \u2192 success.
    ctaEmailPlaceholder: "you@memax.app",
    ctaJoin: "Give your AI a memory",
    ctaJoining: "Joining\u2026",
    ctaMicrocopy: "Early access",
    ctaBack: "Back",
    ctaExpandHeadline: "Give your AI a memory",
    ctaSubmit: "Join the waitlist",
    ctaSuccessHeadline: "You\u2019re on the list",
    ctaSuccessMessage:
      "We\u2019ll email you when your invite is ready. Nothing to do in the meantime.",
    ctaSignInPrompt: "Already using memax?",
    ctaSignIn: "Sign in",
    ctaErrorGeneric: "Something went wrong. Try again in a moment.",
    ctaErrorInvalidEmail: "Enter a valid email address.",
    docs: "Docs",
    github: "GitHub",
    copyright: "\u00a9 2026 memaxlabs",
    you: "You",
    yourTeam: "Your Team",
    // Scenario showcase — 4 coded recreations of real usage surfaces.
    scenarioLabel: "memax, everywhere you work",
    scenarioTabCli: "Terminal",
    scenarioTabWeb: "Web",
    // Claude Code scenario — agent recalls team context mid-session via MCP.
    ccWindowTitle: "~/work/api — Claude Code",
    ccPrompt: "why did we move access tokens to 1h expiry?",
    // Claude Code renders MCP tools as `server - tool (MCP)(args)`; the
    // ⎿ line matches the real collapsed-result chrome. TUI chrome strings
    // stay English in every locale — the real tool isn't localized.
    ccIntro: "I'll check the team hub for that decision.",
    ccToolCall: 'memax - recall (MCP)(query: "auth token expiry decision")',
    ccToolResult: "Found 3 memories (ctrl+r to expand)",
    ccAnswer:
      "April's security review flagged long-lived tokens — access tokens dropped to 1h, refresh stays 30 days. The full tradeoff analysis is in your team hub.",
    ccCaption: "Claude Code pulls the answer from your memory, mid-session.",
    // CLI scenario — content lines reuse the term* keys below.
    cliWindowTitle: "~ — zsh",
    cliCaption: "Push on Monday, recall on Thursday — from any shell.",
    // Web scenario — the memory feed + ask demo, framed as memax.app.
    webWindowTitle: "memax.app",
    webCaption: "Browse, ask, and organize at memax.app.",
    // Third-party agent scenario — any MCP agent taps the same brain.
    agentWindowTitle: "OpenClaw",
    agentUserMsg: "draft the onboarding doc for the new engineer",
    agentToolCall: 'memax_recall("engineering onboarding")',
    agentToolResult: "5 memories · team hub",
    agentAnswer:
      "Pulled your team's setup checklist, coding conventions, and deploy flow from memax — here's the draft.",
    agentCaption:
      "Any MCP agent — OpenClaw, Hermes, yours — taps the same brain.",
    // Benchmark proof strip — numbers live in benchmark-strip.tsx, synced
    // with docs.memax.app/quickstart/benchmarks.
    benchLabel: "LongMemEval — the standard long-term memory benchmark",
    benchRecallLabel: "retrieval recall@5",
    benchQaLabel: "QA accuracy · #9 worldwide",
    benchCostLabel: "per question, end-to-end",
    benchLink: "See the full benchmarks",
    // CLI terminal demo lines (used by the Terminal scenario). Output rows
    // mirror packages/cli push/recall exactly — green "Saved" + bold title +
    // gray meta, recall header with classification + score. Output labels
    // stay English in every locale, like the real CLI.
    termPush:
      "Acme meeting moved to Thursday 2pm. Dashboard demo only, not the full pitch.",
    cliSaved: "Saved",
    cliSavedTitle: "Acme meeting → Thursday 2pm",
    cliSavedMeta:
      "id: mem_9f2e41  classification: episodic/evolving  source: cli",
    cliResultClass: "[episodic/evolving]",
    cliResultScore: "94%",
    cliResultAge: "· 3d ago",
    termComment: "# Thursday morning, new session",
    termRecall: "Acme meeting",
    termAnswer: "Today 2pm. Dashboard demo only \u2014 skip the full pitch.",
    // Demo — Riley brainstorms memax using memax (Claude Code + Codex),
    // then memax synthesizes scattered notes into the pitch when Jordan asks.
    demoActor: "Riley",
    demoNote1Agent: "claude-code",
    demoNote1Time: "3h ago",
    demoNote1Content:
      "what if my ai agents could share a memory? they\u2019d just be smarter. one brain across claude, cursor, codex. no more re-explaining",
    demoNote2Agent: "codex",
    demoNote2Time: "2h ago",
    demoNote2Content:
      "team angle is stronger. not just my agents \u2014 my team\u2019s agents too. shared brain for the whole crew",
    demoNote3Agent: "claude-code",
    demoNote3Time: "1h ago",
    demoNote3Content: "test test test can you see this jordan",
    demoAsker: "Jordan",
    demoAskerAgent: "openclaw",
    demoAskerTime: "just now",
    askedVia: "asked via",
    recallQuery: "what\u2019s riley cooking in our team hub?",
    recallAnswer:
      "A shared brain for you, your team, and every AI you use. You stop re-explaining yourself. Your team stops onboarding each other. Every agent just knows.",
    sourcesLabel: "sources",
    savedVia: "saved via",
    pushed: "pushed",
    saved: "saved",
    asked: "asked",
    flagged: "flagged",
    shipped: "shipped",
    from: "from",
    // Feature blocks (4-block reinforcement strip below the demo)
    featDumpTitle: "Your memory follows you",
    featDumpDesc:
      "Switch from Claude to Cursor to Codex. Your context comes with you. One setup. No re-explaining. Ever.",
    featAskTitle: "Gets smarter over time",
    featAskDesc:
      "Every session adds context. memax consolidates and connects. Compound intelligence, not storage.",
    featOrganizeTitle: "Self-organizing",
    featOrganizeDesc: "You dump. memax organizes.",
    featTeamTitle: "Team memory",
    featTeamDesc: "Your teammate\u2019s AI knows what yours knows.",
    // Overview strip — top-of-hero compatibility band
    overviewConnectLabel: "connect via",
    overviewWorksWithLabel: "works with",
    surfaceMcpLabel: "MCP",
    surfaceMcpDesc: "Any MCP-enabled agent",
    surfaceCliLabel: "CLI",
    surfaceCliDesc: "Scripts, CI, hooks",
    surfaceWebLabel: "Web",
    surfaceWebDesc: "Any browser, no install",
    surfaceSdkLabel: "SDK",
    surfaceSdkDesc: "TypeScript client",
    privacy: "Privacy",
    terms: "Terms",
  },

  // Personas (Beta) — identity objects derived from synced SOUL/identity
  // files, surfaced on the /agents page.
  personas: {
    title: "Personas",
    beta: "Beta",
    subtitle:
      "Identities extracted from your agents' SOUL files. Set one as default — or switch per chat — and the memax agent speaks as it.",
    sourceLabel: "from",
    setDefaultCta: "Set as memax default",
    clearDefaultCta: "Remove default",
    defaultBadge: "default",
    defaultSet: "memax now speaks as {name} — new chats pick it up instantly",
    defaultCleared: "Default persona removed — memax is back to its own voice",
    pickerLabel: "Persona",
    pickerInherit: "Default",
    pickerNone: "No persona",
    historyCta: "History",
    historyTitle: "Versions",
    versionRow: "v{n}",
    restoreCta: "Restore",
    restored: "Restored v{n} — syncing back to the source file",
    forgetConfirm: "Forget this persona? The source file stays untouched.",
    close: "Close",
  },

  errors: {
    title: "Something went wrong",
    description:
      "An unexpected error prevented this page from rendering. The error has been logged.",
    tryAgain: "Try again",
    backToHome: "Back to home",
    backToSignIn: "Back to sign in",
    showDetails: "Show error details",
    adminHint:
      "If this keeps happening, share the error digest with engineering.",
    digestLabel: "Digest",
    notFound: {
      title: "Page not found",
      description:
        "The page you're looking for doesn't exist or has been moved.",
      backToHome: "Back to home",
      signIn: "Sign in",
    },
    // ── Mutation-action error messages (classified by HTTP status) ──
    //
    // These are the user-visible strings the error classifier renders
    // when a mutation fails in a known way. `{action}` is interpolated
    // from errors.action.* below at the callsite; `{seconds}` from
    // the server's Retry-After header.
    //
    // Keep the copy warm and actionable — "wait a moment" beats "try
    // again later" because it tells the user what to DO next.
    mutation: {
      rateLimited:
        "Going a little fast. Wait a moment before trying to {action} again.",
      rateLimitedWithSeconds:
        "Going a little fast. Try to {action} again in {seconds}s.",
      forbidden: "You don't have permission to {action}.",
      missing: "That's gone — it may have been removed by another session.",
      conflict:
        "Someone else changed this. Refresh, then try to {action} again.",
      quotaExceeded:
        "You've hit your plan's limit for this. Upgrade or archive something to continue.",
      offline: "You appear to be offline. Reconnect and try again.",
      server: "memax had a hiccup. Try to {action} again in a moment.",
      badRequest: "That request couldn't be processed. Refresh and try again.",
    },
    // Short phrases interpolated into mutation.* as {action}. Each
    // mutation hook picks one that reads naturally in the template.
    // Pattern: short noun phrase describing what the user was doing.
    // Keep these ≤ 4 words; anything longer makes the toast wrap.
    action: {
      // Memories
      moveMemory: "move that memory",
      moveMemories: "move those memories",
      createMemory: "save that memory",
      deleteMemory: "delete that memory",
      deleteMemories: "delete those memories",
      updateMemory: "save that change",
      shareMemory: "share that memory",
      forgetMemory: "forget that memory",
      forgetMemories: "forget those memories",
      // Personas
      deletePersona: "forget that persona",
      restorePersona: "restore that persona",
      // Board
      resolveBoardCard: "resolve that card",
      // Hubs
      createHub: "create that hub",
      updateHub: "update the hub",
      deleteHub: "delete the hub",
      leaveHub: "leave the hub",
      updateMember: "update that member",
      removeMember: "remove that member",
      createInvite: "create that invite",
      revokeInvite: "revoke that invite",
      regenerateInvite: "regenerate that invite",
      resendInvite: "resend that invite",
      transferOwnership: "transfer hub ownership",
      acceptTransfer: "accept that transfer",
      cancelTransfer: "cancel that transfer",
      recordHubVisit: "record that hub visit",
      // Topics
      createTopic: "create that topic",
      updateTopic: "update that topic",
      deleteTopic: "delete that topic",
      archiveTopic: "archive that topic",
      restoreTopic: "restore that topic",
      moveTopic: "move that topic",
      // Notifications
      markSeen: "mark that as read",
      dismissNotification: "dismiss that notification",
      resolveNotification: "resolve that notification",
      // Dreams
      triggerDream: "trigger a dream",
      // Settings & account
      updateSettings: "update settings",
      updateHubSettings: "update hub settings",
      deleteAllData: "delete your data",
      // Agents & API keys
      updateAgent: "update that agent",
      disconnectAgent: "disconnect that agent",
      revokeApiKey: "revoke that key",
      updateApiKey: "update that key",
      linkProvider: "link that account",
      unlinkProvider: "unlink that account",
      // Admin
      adminApprove: "approve that entry",
      adminReject: "reject that entry",
      adminRestore: "restore that entry",
      adminBatchApprove: "approve those entries",
      adminInvite: "send that invite",
      adminRevokeInvite: "revoke that invite",
      adminSaveEmailTemplate: "save that template",
      adminPublishEmailTemplate: "publish that template",
      adminResetEmailTemplate: "reset that template",
      adminSendEmail: "send that email",
      adminSetUserPlan: "set that user's plan",
      adminSetOverrides: "save those overrides",
      adminDeleteOverrides: "clear those overrides",
      adminUpdatePlan: "update that plan",
      adminSetHubPlan: "set the hub's plan",
      adminCreateAudience: "create that audience",
      adminUpdateAudience: "update that audience",
      adminDeleteAudience: "delete that audience",
      adminEstimateAudience: "estimate that audience",
      adminCreateCampaign: "create that campaign",
      adminUpdateCampaign: "update that campaign",
      adminScheduleCampaign: "schedule that campaign",
      adminCancelCampaign: "cancel that campaign",
      adminSendCampaign: "send that campaign",
      adminDeleteCampaign: "delete that campaign",
      adminTestSendCampaign: "send that test",
      adminReconcileOrphans: "reconcile those invites",
      adminLoadEmailTemplate: "load that template",
      adminSendNotification: "send that notification",
    },
  },

  admin: {
    title: "admin",
    backToApp: "Back to app",
    pagination: {
      previous: "Previous",
      next: "Next",
      total: "{count} total",
    },
    nav: {
      waitlist: "Waitlist",
      users: "Users",
      hubs: "Hubs",
      communications: "Communications",
      notifications: "Notifications",
      plans: "Plans",
      seedMemories: "Onboarding seeds",
      infra: "Infra",
      system: "System",
      openMenu: "Open admin menu",
      closeMenu: "Close admin menu",
    },
    seedMemories: {
      title: "Onboarding seed memories",
      description:
        "Templates copied into every new user's personal hub on signup. Edits affect future signups only — existing users keep their copies.",
      empty: "No seed templates yet.",
      loadFailed: "Could not load seed templates.",
      stateActive: "Active",
      stateArchived: "Archived",
      titleLabel: "Title",
      summaryLabel: "Summary",
      hintLabel: "Hint",
      contentLabel: "Content (markdown)",
      tagsLabel: "Tags (comma-separated)",
      save: "Save changes",
      saving: "Saving…",
      saved: "Saved",
      saveFailed: "Save failed.",
      enable: "Enable on signup",
      disable: "Disable",
      enabled: "Enabled",
      disabled: "Disabled",
      idLabel: "ID",
      contentEditTab: "Edit",
      contentPreviewTab: "Preview",
      contentPlaceholder: "Write the seed body in markdown…",
      contentImageHint:
        "Drag, paste, or type ![alt](url) for images. Uploaded images are publicly readable.",
      imageUploading: "Uploading image…",
      imageUploaded: "Image inserted.",
      imageUploadFailed: "Image upload failed.",
      imageUploadRejectedType: "Only PNG, JPEG, WebP, and GIF are supported.",
      imageUploadRejectedSize: "Image too large (max 5MB).",
      syncToMineLabel: "Sync to my account",
      syncToMineConfirm:
        "Replace my onboarding seed copies with the current templates?",
      syncToMineConfirmAction: "Sync",
      syncToMineCancel: "Cancel",
      syncToMineRunning: "Syncing…",
      syncToMineSuccess: "Synced — {deleted} replaced, {copied} active.",
      syncFailed: "Sync failed.",
      createNewLabel: "New seed",
      createNewAria: "Create a new onboarding seed",
      createNewTitle: "New onboarding seed",
      createNewDescription:
        "Authors a new template that future signups will receive in their personal hub.",
      editTitle: "Edit seed",
      editDescription:
        "Edits affect future signups only. Existing user copies stay untouched.",
      modalCancel: "Cancel",
      modalSaveCreate: "Create seed",
      modalSaveEdit: "Save changes",
      titleRequired: "Title is required.",
      contentRequired: "Content is required.",
      cardMenuLabel: "Seed actions",
      menuArchive: "Archive (hide from new signups)",
      menuUnarchive: "Restore",
      menuDelete: "Delete permanently",
      deleteTitle: "Delete this seed?",
      deleteConfirm:
        'Delete "{title}"? This cannot be undone. Existing users keep their copies.',
      deleteCancel: "Cancel",
      deleteConfirmAction: "Delete",
      deleteRunning: "Deleting…",
      deleteFailed: "Could not delete seed.",
      untitledFallback: "Untitled seed",
    },
    email: {
      title: "Email templates",
      description:
        "Manage transactional email copy, HTML, and plain-text fallbacks from one place.",
      modeTitle: "Authoring mode",
      modeDescription:
        "Use visual mode for layout and formatting. Switch to source mode when you need exact HTML or plain-text control.",
      catalogTitle: "Template catalog",
      catalogDescription:
        "Choose a built-in template to inspect, customize, and preview.",
      previewTitle: "Rendered preview",
      previewFrameTitle: "Email HTML preview",
      previewViewport: {
        desktop: "Desktop preview",
        mobile: "Mobile preview",
      },
      plainTextTitle: "Plain text",
      generatedTextTitle: "Generated plain text",
      generatedTextDescription:
        "Visual mode regenerates the plain-text alternative when you preview or save.",
      sendTitle: "Send email",
      sendDescription:
        "Send the current rendered draft to a specific inbox using the same delivery pipeline as production email.",
      recipientLabel: "Recipient email",
      recipientPlaceholder: "name@example.com",
      sendQueued: 'Queued email to {email} with subject "{subject}".',
      variablesTitle: "Variables",
      defaultsTitle: "Built-in defaults",
      defaultsDescription:
        "Resetting removes the override and falls back to the embedded template shipped with the worker.",
      defaultSubjectLabel: "Default subject",
      editorKindLabel: "Editor mode",
      revisionsTitle: "Revision history",
      revisionsEmpty: "No saved revisions yet.",
      revisionActions: {
        saved: "Saved",
        reset: "Reset",
      },
      variableCount: "{count} vars",
      mode: {
        visual: "Visual",
        source: "Source",
      },
      status: {
        default: "Default",
        custom: "Override",
        draftNotPublished: "Draft — not published",
      },
      actions: {
        preview: "Refresh preview",
        reset: "Reset",
        save: "Save draft",
        publish: "Publish",
        send: "Send email",
      },
      fields: {
        subject: "Subject template",
        html: "HTML template",
        text: "Plain-text template",
        notes: "Notes",
      },
      visualTitle: "Visual editor",
      visualDescription:
        "Compose email layout with blocks and formatting tools. Exported HTML stays the runtime source of truth.",
      visualPlaceholder: "Start writing your email...",
    },
    communications: {
      title: "Communications",
      description:
        "Transactional templates, campaigns, audiences, and brand — one surface for everything memax sends.",
      tabsAriaLabel: "Communications sections",
      tabs: {
        transactional: "Transactional",
        campaigns: "Campaigns",
        audiences: "Audiences",
        brand: "Brand",
      },
      transactional: {
        eyebrow: "Transactional",
        title: "Transactional templates",
        description:
          "Edit tokenized email that fires on product events — invites, reminders, confirmations. Test-send or preview with sample data before publishing a revision.",
      },
      campaigns: {
        eyebrow: "Campaigns",
        title: "Campaigns",
        description:
          "Persistent admin communications. Draft once, pick an audience, send now or schedule — every lifecycle change is captured on the campaign timeline.",
        status: {
          draft: "Draft",
          scheduled: "Scheduled",
          sending: "Sending",
          sent: "Sent",
          cancelled: "Cancelled",
          failed: "Failed",
        },
        kind: {
          announcement: "Announcement",
          gift: "Gift",
          email: "Email",
        },
        deliveryStats: {
          title: "Delivery stats",
          description:
            "Per-recipient delivery state for this email campaign. Delivered/opened counts populate once the Resend webhook is wired.",
          total: "Total",
          queued: "Queued",
          sent: "Sent",
          delivered: "Delivered",
          suppressed: "Suppressed",
          bounced: "Bounced",
          failed: "Failed",
          loadError: "Failed to load delivery stats.",
        },
        list: {
          title: "Campaigns",
          description:
            "Browse drafts, scheduled sends, and history. Newest-first.",
          empty: "No campaigns match this filter yet.",
          loadError: "Could not load campaigns.",
          newButton: "New campaign",
          manageTemplates: "Manage templates",
          sentLabel: "{count} sent",
          filters: {
            all: "All",
          },
        },
        scenarios: {
          announcement: {
            title: "Announcement",
            description:
              "System-style notice with optional link. Best for product news, policy updates, outages.",
          },
          gift: {
            title: "Gift / credit drop",
            description:
              "Send a redeemable gift token with an expiry. Great for thank-yous and early-access perks.",
          },
          email: {
            title: "Email announcement",
            description:
              "Broadcast a full email through your brand layout. Respects marketing opt-outs; tracks per-recipient delivery.",
          },
        },
        audience: {
          sectionTitle: "Audience",
          sectionDescription:
            "Pick a saved audience or define targeting inline. Saved audiences can be reused across campaigns.",
        },
        scaffold: {
          sectionTitle: "Start from a template",
          sectionDescription:
            "Seed subject + body from a custom template you've saved, or from a transactional template's published content. Content is copied — editing the source later won't touch this campaign, and unpublished drafts are never used.",
          selectLabel: "Template",
          placeholder: "Choose a template…",
          groupCustom: "Your custom templates",
          groupSystem: "Transactional templates",
          apply: "Apply",
          confirmOverwrite: "Yes, replace",
          cancel: "Cancel",
          loading: "Loading…",
          empty: "No templates available.",
          loadError: "Could not load the template. Try again.",
          noPublished:
            "This template has an unpublished draft but no published version yet. Publish it first or pick a different template.",
          overwriteHint:
            "Applying this template will replace your current subject and body.",
        },
        saveAsTemplate: {
          button: "Save as template",
          formTitle: "Save this body as a reusable template",
          formDescription:
            "Stores the current subject and body for future campaigns. Edits to this campaign won't retroactively touch the saved template.",
          nameLabel: "Template name",
          namePlaceholder: "e.g. Product update — April",
          descriptionLabel: "Description (optional)",
          descriptionPlaceholder: "Short note so you remember when to use it",
          save: "Save template",
          saving: "Saving…",
          cancel: "Cancel",
          nameRequired: "Give the template a name.",
          successPrefix: "Saved as",
        },
        campaignTemplates: {
          title: "Campaign templates",
          description:
            "Reusable campaign bodies you've saved from past campaigns. Archived templates disappear from the picker but stay here for later.",
          backToCampaigns: "Back to campaigns",
          showArchived: "Include archived",
          archive: "Archive",
          unarchive: "Unarchive",
          archivedBadge: "Archived",
          delete: "Delete",
          confirmDelete: "Yes, delete",
          cancel: "Cancel",
          slugPrefix: "slug: ",
          noSubject: "(no subject)",
          updatedAt: "Updated {time}",
          emptyActive:
            'No custom templates yet. Create one by clicking "Save as template" on an email campaign draft.',
          emptyAll: "No templates at all yet.",
          loadError: "Could not load templates.",
        },
        audiencePicker: {
          modes: {
            inline: "Inline targeting",
            saved: "Saved audience",
          },
          savedEmpty:
            "No saved audiences yet. Create one on the Audiences tab, or use inline targeting.",
          ruleTypes: {
            all: "Everyone",
            users: "Specific people",
            hub: "Hub members",
          },
          usersHint:
            "One per line or comma-separated. Emails resolve to users; unknown emails are listed on send.",
          usersPlaceholder: "user@example.com\nanother-user-id",
          hubIdPlaceholder: "Hub ID (uuid)",
          hubRolePlaceholder: "Role (optional)",
          allWarning:
            "This fans out to every registered user. Cancel before the scheduled send to stop it; once sending, it cannot be paused.",
        },
        content: {
          sectionTitle: "Content",
          sectionDescription:
            "What recipients see in their inbox. Stored on the campaign draft.",
          announcement: {
            titleLabel: "Title",
            titlePlaceholder: "A short headline (optional if body is set)",
            bodyLabel: "Body",
            bodyPlaceholder: "The message body shown in the inbox.",
            linkLabel: "Link URL",
            linkPlaceholder: "https://memax.app/…",
            linkTextLabel: "Link text",
            linkTextPlaceholder: "Learn more",
          },
          gift: {
            senderLabel: "Sender display name",
            senderPlaceholder: "memax team",
            tokenLabel: "Gift token",
            tokenPlaceholder: "Redeemable code",
            urlLabel: "Redemption URL",
            urlPlaceholder: "https://memax.app/redeem/…",
            expiresLabel: "Expires at",
            expiresPlaceholder: "2026-05-30T00:00:00Z",
            expiresHint: "Defaults to 30 days from now if left blank.",
          },
          email: {
            subjectLabel: "Subject",
            subjectPlaceholder: "A short, specific subject line",
            bodyHtmlLabel: "Body (HTML)",
            bodyHtmlPlaceholder: "<h1>Hi there</h1>\n<p>…</p>",
            bodyHtmlHint:
              "Body-only HTML — the brand layout adds logo + footer automatically. No <!DOCTYPE>, <html>, or <body> tags.",
            bodyTextLabel: "Body (plain text)",
            bodyTextPlaceholder:
              "Plain-text fallback shown in clients that don't render HTML.",
          },
        },
        audienceSummary: {
          none: "No audience",
          users: "{count} specific recipient(s)",
          hub: "Hub {id}… • role: {role}",
          all: "Everyone",
        },
        errors: {
          missingTitleOrBody: "Enter a title or body.",
          missingToken: "A gift token is required.",
          missingEmailFields:
            "Email campaigns need a subject, HTML body, and plain-text body.",
        },
        form: {
          newTitle: "New campaign",
          newDescription:
            "A draft is created first. You can review it and send or schedule when you're ready.",
          editTitle: "Edit draft",
          backToList: "Back to campaigns",
          nameLabel: "Campaign name",
          namePlaceholder: "Internal label, e.g. April announcement",
          saveDraft: "Save draft",
          saveEdit: "Save changes",
          saving: "Saving…",
          cancel: "Cancel",
          errors: {
            missingName: "Name the campaign before saving.",
            missingAudience: "Pick an audience or define one inline.",
          },
        },
        detail: {
          loadError: "Could not load campaign.",
          updatedAt: "Updated {time}",
          summaryTitle: "Summary",
          actionsTitle: "Actions",
          audience: "Audience",
          fromSaved: "From saved audience {id}…",
          scheduledAt: "Scheduled for",
          sendStartedAt: "Send started",
          sendFinishedAt: "Send finished",
          counts: "Delivery",
          countsFormat: "{sent} sent · {failed} failed",
          errorLabel: "Error",
          contentLabel: "Content (JSON)",
          states: {
            sending:
              "This campaign is sending right now. It cannot be paused or edited.",
            terminal:
              "This campaign has finished. Content and audience are frozen.",
          },
          actions: {
            edit: "Edit",
            send: "Send now",
            schedule: "Schedule",
            cancel: "Cancel",
            delete: "Delete",
            testSend: "Test send",
          },
          confirm: {
            send: "This will start sending immediately. Continue?",
            cancel:
              "Cancel this campaign? The scheduled send will be skipped by the worker.",
            delete: "Delete this campaign? This cannot be undone.",
            // Broadcast send safety — operator types the phrase below
            // to unlock the confirm button on audience=all campaigns.
            typedPhrase: "SEND",
            typedPrompt:
              "Type {phrase} to confirm sending to every registered user.",
          },
          testSend: {
            title: "Send a test to yourself",
            description:
              "Renders the current draft through the brand layout and emails a single address. No unsubscribe link, no delivery row — test-only.",
            placeholder: "your@email.com",
            invalidEmail: "Enter a valid email address.",
            submit: "Send test",
            sending: "Sending…",
            successPrefix: "Queued a test email to",
          },
          schedule: {
            title: "Schedule send",
            description:
              "Pick a date and time in your local timezone. The worker fires at that moment; cancel before then to skip.",
            submit: "Schedule",
            cancel: "Close",
          },
        },
        audit: {
          title: "Timeline",
          empty: "No audit entries yet.",
          actorPrefix: "by {id}…",
          systemActor: "by the send worker",
          actions: {
            created: "Draft created",
            updated: "Draft updated",
            scheduled: "Scheduled",
            send_requested: "Send requested",
            cancelled: "Cancelled",
            send_completed: "Send completed",
            send_failed: "Send failed",
            deleted: "Deleted",
            test_send: "Test send",
          },
        },
      },
      aiAssist: {
        open: "AI assist",
        close: "Close",
        title: "AI copy assist",
        description:
          "On-brand rewrites in memax voice. Legal-compliant (CAN-SPAM, GDPR, CASL).",
        intentLabel: "What to do",
        toneLabel: "Tone",
        noteLabel: "Optional note (launch, apology, onboarding…)",
        notePlaceholder: "e.g. Launching our new team plan next Tuesday",
        useRecallLabel: "Ground in my memax",
        useRecallHint:
          "Uses your memories + hubs as source material for facts (dates, pricing, positioning).",
        generate: "Generate",
        generating: "Generating…",
        regenerate: "Regenerate",
        apply: "Apply",
        suggestionLabel: "Suggestion",
        groundedBadge: "Grounded in {n} notes",
        notGroundedBadge: "No matching notes",
        groundingUnavailableBadge: "Grounding unavailable",
        aiDisabled:
          "AI assist isn't configured on this server. Set ANTHROPIC_API_KEY to enable.",
        intents: {
          polish: "Polish",
          rewrite: "Rewrite",
          shorten: "Shorten",
          expand: "Expand",
          translate: "Translate",
          draft: "Draft new",
        },
        tones: {
          warm: "Warm",
          professional: "Professional",
          playful: "Playful",
          urgent: "Urgent",
          minimal: "Minimal",
          friendly: "Friendly",
        },
      },
      audiences: {
        eyebrow: "Audiences",
        title: "Audiences",
        description:
          "Saved, reusable recipient rules. Use them in campaigns or estimate on the fly.",
        newButton: "New audience",
        empty: "No audiences yet. Create your first one.",
        loadError: "Could not load audiences.",
        ruleTypeLabel: {
          all: "Everyone",
          users: "Specific people",
          hub: "Hub members",
        },
        form: {
          newTitle: "New audience",
          newDescription:
            "Saved audiences become reusable across campaigns. You can estimate recipient count before saving.",
          editTitle: "Edit audience",
          backToList: "Back to audiences",
          nameLabel: "Name",
          namePlaceholder: "e.g. early-access beta testers",
          descriptionLabel: "Description (optional)",
          descriptionPlaceholder: "Who is this for? When should it be used?",
          ruleTypeLabel: "Targeting rule",
          ruleTypes: {
            all: {
              title: "Everyone",
              description: "Every registered user.",
            },
            users: {
              title: "Specific people",
              description: "A list of user IDs and emails.",
            },
            hub: {
              title: "Hub members",
              description: "Everyone in a hub, optionally filtered by role.",
            },
          },
          usersLabel: "Users (IDs or emails)",
          usersPlaceholder: "user@example.com\nanother-user-id",
          hubIdPlaceholder: "Hub ID (uuid)",
          hubRolePlaceholder: "Role filter (optional)",
          estimateButton: "Estimate recipients",
          estimatePending: "Estimating…",
          estimateResult: "Resolves to {count} recipient(s).",
          save: "Save audience",
          saveEdit: "Save changes",
          saving: "Saving…",
          cancel: "Cancel",
          errors: {
            missingName: "Give the audience a name.",
            missingRecipients: "Add at least one user ID or email.",
            missingHub: "Pick a hub to target.",
          },
        },
        detail: {
          ruleTitle: "Rule",
          edit: "Edit",
          delete: "Delete",
          confirmDelete:
            "Delete this audience? Existing campaigns keep their frozen snapshots.",
        },
      },
      brand: {
        eyebrow: "Brand",
        title: "Email brand kit",
        description:
          "Shared logo, footer, and color tokens wrapping every transactional and campaign email. Edit once — all templates re-render through the base layout.",
        logoSectionTitle: "Logo",
        logoSectionDescription:
          "PNG hosted at a public URL, 28px tall at 1x. Falls back to a wordmark when blank.",
        logoUrlLabel: "Logo URL",
        logoUrlPlaceholder: "https://memax.app/brand/logo-email.png",
        logoPresetsLabel: "Preset",
        logoCustomLabel: "Custom URL",
        logoPresets: {
          icon: "Icon",
          wordmark: "Wordmark",
          mascot: "Mascot",
        },
        logoAltLabel: "Alt text",
        logoAltPlaceholder: "memax",
        productNameLabel: "Wordmark (shown when no logo URL)",
        productNamePlaceholder: "memax",
        footerSectionTitle: "Footer notes",
        footerSectionDescription:
          "Optional tagline or reassurance line rendered above the compliance footer. Keep it short. Company identity, address, support, legal links, and unsubscribe are handled by the fields below — don't duplicate them here.",
        footerHtmlLabel: "Notes HTML",
        footerHtmlPlaceholder: "<p>memax — memory for your AI agents</p>",
        footerTextLabel: "Notes plain-text",
        footerTextPlaceholder: "memax — memory for your AI agents",
        colorsSectionTitle: "Colors",
        colorsSectionDescription:
          "Hex values merged into the base layout's CSS. Use true-black/true-white carefully for dark-mode clients.",
        primaryColorLabel: "Primary",
        backgroundColorLabel: "Background",
        surfaceColorLabel: "Surface",
        borderColorLabel: "Border",
        mutedColorLabel: "Muted",
        bodyColorLabel: "Body text",
        supportSectionTitle: "Support contact",
        supportSectionDescription:
          "Optional support email rendered as a mailto link in the footer of every email. Leave blank to hide.",
        supportEmailLabel: "Support email",
        supportEmailPlaceholder: "hello@memax.app",
        complianceSectionTitle: "Sender identity (CAN-SPAM)",
        complianceSectionDescription:
          "Legal entity and physical postal address. Required by CAN-SPAM §7704(a)(5) on commercial email; rendered in every outbound footer when set.",
        companyNameLabel: "Legal entity name",
        companyNamePlaceholder: "Memax Labs, Inc.",
        companyAddressLabel: "Physical postal address",
        companyAddressPlaceholder:
          "548 Market St PMB 12345, San Francisco, CA 94104",
        legalSectionTitle: "Legal links",
        legalSectionDescription:
          "Optional Privacy Policy and Terms URLs, surfaced in the footer of every email.",
        privacyUrlLabel: "Privacy Policy URL",
        privacyUrlPlaceholder: "https://memax.app/privacy",
        termsUrlLabel: "Terms of Service URL",
        termsUrlPlaceholder: "https://memax.app/terms",
        previewTitle: "Live preview",
        previewDescription:
          "Rendered with a sample template body. Changes apply on save.",
        previewModeTransactional: "Transactional",
        previewModeMarketing: "Marketing",
        previewUnsubscribeChip: "[per-recipient unsubscribe link]",
        save: "Save changes",
        saving: "Saving…",
        saved: "Saved",
        loadError: "Failed to load brand settings.",
        saveError: "Failed to save brand settings.",
      },
    },
    waitlist: {
      title: "Waitlist",
      description: "Manage early access signups and invitations",
      stats: {
        total: "Total",
        pending: "Pending",
        approved: "Approved",
        registered: "Registered",
        rejected: "Rejected",
        capacity: "Capacity",
      },
      table: {
        email: "Email",
        useCase: "Use case",
        aiTools: "AI tools",
        role: "Role",
        status: "Status",
        invite: "Invite",
        signedUp: "Signed up",
        actions: "Actions",
      },
      inviteStatus: {
        active: "Active",
        used: "Used",
        expired: "Expired",
        none: "\u2014",
        usedUnknownUser: "unknown user",
      },
      actions: {
        approve: "Approve",
        reject: "Reject",
        reinvite: "Re-invite",
        revokeInvite: "Revoke invite",
        undoReject: "Restore to pending",
        approveSelected: "Approve {count}",
        rejectSelected: "Reject {count}",
        inviteByEmail: "Invite by email",
        inviteEmailPlaceholder: "user@example.com",
        sendInvite: "Send invite",
        inviteSent: "Invite sent",
        viewDetails: "View details",
      },
      detail: {
        title: "Waitlist entry",
        email: "Email",
        useCase: "Use case",
        aiTools: "AI tools",
        role: "Role",
        status: "Status",
        wave: "Wave",
        invite: "Invite",
        signedUp: "Signed up",
        notes: "Notes",
        none: "\u2014",
      },
      status: {
        pending: "Pending",
        approved: "Approved",
        registered: "Registered",
        rejected: "Rejected",
      },
      useCase: {
        personal_memory: "Personal",
        team_knowledge: "Team",
        developer_tooling: "Developer",
        other: "Other",
      },
      empty: "No waitlist entries yet",
      showing: "{count} total",
      search: "Search by email...",
      filterStatus: "Filter by status",
      allStatuses: "All statuses",
      wave: "Wave",
      assignWave: "Assign wave",
      // Orphan-repair surface for the 2026-04 OAuth invite-drop
      // regression. Operators preview the cohort at /admin/waitlist/orphans
      // and batch-reconcile specific user_ids. See
      // docs/engineering docstring on AdminWaitlistReconcileHandler
      // in packages/server/internal/handler/admin_waitlist_reconcile.go
      // for the server contract.
      orphans: {
        title: "Orphan invites",
        description:
          "Users registered during the 2026-04 OAuth regression without having their invite consumed. Reconciling upgrades their plan and links the invite so the waitlist-gate stops bouncing them.",
        backToWaitlist: "← Back to waitlist",
        bannerTitle: "{count} orphan invite to reconcile",
        bannerTitlePlural: "{count} orphan invites to reconcile",
        bannerCta: "Open orphans →",
        bannerLoading: "Checking for orphan invites…",
        sinceLabel: "Scan since",
        sinceHint:
          "Required — narrows the scan window so the server never does a full users-table join.",
        refresh: "Refresh",
        selectAll: "Select all",
        clearSelection: "Clear",
        selectedCount: "{count} selected",
        reconcileSelected: "Reconcile {count}",
        reconcileOne: "Reconcile",
        reconcileError: "Couldn't reconcile orphan invites",
        reconcileSuccess:
          "Reconciled {reconciled}, skipped {skipped}, errored {errored}.",
        reconcileRunning: "Reconciling…",
        columnEmail: "Email",
        columnPlan: "Current plan",
        columnCreatedAt: "Registered",
        columnInviteExpires: "Invite expires",
        columnWaitlist: "Waitlist",
        columnActions: "Actions",
        viewUserDetail: "View user",
        emptyTitle: "No orphan invites in this window",
        emptyBody:
          "Every waitlist invite in the scan window has been consumed. Change the since date if you want to scan further back.",
        errorTitle: "Couldn't load orphan candidates",
        errorBody: "Check the since format — must be RFC3339.",
        // Per-user reconcile outcomes surfaced in the result table.
        resultReconciled: "Reconciled",
        resultSkipped: "Skipped",
        resultError: "Error",
        reasonNotOrphan: "Not an orphan",
        reasonAlreadyUpgraded: "Already upgraded",
        reasonDuplicateInBatch: "Duplicate in batch",
        reasonUserNotFound: "User not found",
        reasonPlanUpgradeFailed: "Plan upgrade did not apply — check logs",
        reasonPostConsumeLookupFailed: "User lookup failed after consume",
        reasonGeneric: "See server logs",
        planFlowLabel: "{before} → {after}",
        // User-detail page crosslink card.
        userCardTitle: "Waitlist orphan",
        userCardDescription:
          "This user registered without their invite being consumed. Reconciling will consume the invite, link it to the user, and upgrade the plan to early access.",
        userCardInviteExpires: "Invite expires {when}",
      },
    },
    users: {
      title: "Users",
      description: "Manage users, plans, and usage",
      search: "Search by email or name...",
      allPlans: "All plans",
      stats: {
        total: "Total users",
        memories: "Memories",
        activeToday: "Active today",
      },
      table: {
        user: "User",
        plan: "Plan",
        memories: "Memories",
        usage: "Usage",
        created: "Joined",
        actions: "Actions",
      },
      actions: {
        viewDetails: "View details",
        changePlan: "Change plan",
        setOverrides: "Set overrides",
        removeOverrides: "Remove overrides",
      },
      detail: {
        title: "User details",
        plan: "Plan",
        usage: "Usage this period",
        hubs: "Hubs",
        overrides: "Limit overrides",
        noOverrides: "No overrides set",
        memoryCount: "Memories",
        notFound: "User not found",
        joined: "Joined",
        id: "ID",
        reason: "Reason",
        memory: "Memory",
        pushPerMonth: "Push/mo",
        recallPerMonth: "Recall/mo",
        askPerMonth: "Ask/mo",
        pushes: "pushes",
        recalls: "recalls",
        asks: "asks",
        effectivePlan: "Effective plan",
        effectivePlanDescription:
          "What this user actually gets in their personal hub — max(personal plan, best hub plan they belong to). Counters are the same; denominators reflect the boost.",
        effectiveSource: "Source",
        effectiveSourcePersonal: "personal",
        effectiveSourceHub: "hub: {name}",
        effectiveSourceUnknownHub: "hub: {id}",
      },
      pagination: {
        previous: "Previous",
        next: "Next",
      },
      empty: "No users found",
      loadError: "Couldn't load users",
      showing: "{count} total",
      planChanged: "Plan changed successfully",
    },
    hubs: {
      title: "Hubs",
      description: "Manage team hub subscriptions and billing",
      search: "Search by hub name...",
      table: {
        hub: "Hub",
        plan: "Plan",
        seats: "Seats",
        owner: "Owner",
        created: "Created",
        actions: "Actions",
      },
      noSubscription: "No subscription",
      noPlan: "none",
      subscription: "Subscription",
      billingContact: "Billing contact",
      seatCount: "Seats",
      changePlan: "Change plan",
      planChanged: "Hub plan changed",
      effective: "Effective",
      hubLabel: "Hub",
      status: {
        active: "Active",
        past_due: "Past due",
        cancelled: "Cancelled",
        trialing: "Trialing",
      },
      empty: "No team hubs found",
      showing: "{count} total",
      detail: {
        notFound: "Hub not found",
        owner: "Owner",
        stats: {
          members: "Members",
          memories: "Memories",
          plan: "Plan",
          seats: "Seats",
        },
        members: {
          title: "Members",
          search: "Search members by name or email...",
          empty: "No members",
          role: "Role",
          joined: "Joined",
        },
        quota: {
          overLimitTitle: "Over capacity",
          overLimitBody:
            "This hub is at {current}/{limit} memories. It will freeze in {days} day(s) if the count isn't back under the limit — pushes will be rejected and memories hidden from recall/ask for all members.",
          overLimitBodyToday:
            "This hub is at {current}/{limit} memories. It will freeze within 24 hours if not resolved.",
          frozenTitle: "Frozen",
          frozenBody:
            "This hub has been over its plan's memory limit for more than 7 days. Pushes are rejected and memories are hidden from recall/ask. Upgrade the plan or delete memories to restore it.",
          inactiveTitle: "Subscription inactive",
          inactiveBody:
            "The hub subscription is {status}. Pushes are rejected and memories are hidden from recall/ask until the subscription is restored.",
          sinceLabel: "Over limit since",
        },
      },
    },
    settings: {
      adminDashboard: "Admin dashboard",
    },
    plans: {
      title: "Plans",
      description:
        "Edit plan limits in place. Changes propagate to the plan registry immediately after save.",
      tabsAriaLabel: "Plan scopes",
      tabs: {
        personal: "Personal",
        hub: "Team hubs",
      },
      groups: {
        personal: "Personal plans",
        hub: "Team hub plans",
      },
      meta: {
        id: "ID",
        scope: "Scope",
        tierOrder: "Tier order",
        entitlementRank: "Entitlement",
        createdAt: "Created",
        updatedAt: "Updated",
      },
      sections: {
        identity: "Identity",
        limits: "Limits",
        rateLimits: "Rate limits",
        features: "Features",
        hub: "Hub",
      },
      fields: {
        displayName: "Display name",
        monthlyPriceCents: "Price (¢/month)",
        memoryLimit: "Memories",
        pushLimit: "Push / month",
        recallLimit: "Recall / month",
        askLimit: "Ask / month",
        maxAttachmentBytes: "Max attachment",
        storageBytesLimit: "Storage limit",
        askModel: "Ask model",
        dreamsEnabled: "Dreams",
        reviewInbox: "Review inbox",
        maxTeamHubs: "Max team hubs",
        rateLimitRpm: "Requests / minute",
        rateLimitHeavyRpm: "Heavy requests / minute",
        rateLimitLightRpm: "Light requests / minute",
        tierOrder: "Tier order",
        entitlementRank: "Entitlement rank",
        active: "Active",
        maxHubMembers: "Max members",
        seatMinimum: "Seat minimum",
        seatBilled: "Seats billed",
        maxOwnedFreeTeamHubs: "Max owned free team hubs",
      },
      fieldHint: {
        unlimited: "−1 = unlimited",
        nullUnlimited: "empty = unlimited",
      },
      bytesUnitAria: "{label} unit",
      askModel: {
        haiku: "Haiku",
        sonnet: "Sonnet",
      },
      actions: {
        save: "Save",
        saving: "Saving...",
        reset: "Reset",
      },
      saved: "{name} updated",
      error: "Couldn't update plan",
      empty: "No plans registered",
      loadError: "Couldn't load plans",
    },
    infra: {
      // Sub-nav for /admin/infra — visible on desktop + mobile.
      subnav: {
        config: "Config",
        pulse: "Pulse",
        ingestion: "Ingestion",
        jobs: "Jobs",
      },
      ops: {
        title: "Operations",
        description:
          "Live worker status, queue depth, and job health for this environment.",
        liveBadge: "Live",
        liveBadgeDisconnected: "Reconnecting…",
        liveBadgePaused: "Paused",
        lastUpdated: "Updated {ago}",
        loadError: "Couldn't load ops data",
        actionFailed: "Action failed",
        workers: {
          title: "Workers",
          healthy: "{count} healthy",
          stale: "{count} stale",
          noneConnected: "No workers connected",
          leader: "Leader",
          ageSeconds: "{seconds}s ago",
          ageMinutes: "{minutes}m ago",
          ageHours: "{hours}h ago",
          queueSummary: "{count} queue | {count} queues",
        },
        queues: {
          title: "Queues",
          empty: "No jobs queued",
          available: "Available",
          running: "Running",
          scheduled: "Scheduled",
          retryable: "Retryable",
          pending: "Pending",
          discarded: "Discarded",
        },
        throughput: {
          title: "Throughput",
          windowSuffix: " · last {minutes} min",
          memoriesProcessed: "Memories processed",
          memoriesFailed: "Memories failed",
          dreamsCompleted: "Dreams completed",
          emailsSent: "Emails sent",
        },
        ingestionCards: {
          title: "Memory ingestion",
          inProcessing: "In processing",
          stuck: "Stuck > 5 min",
          failedHour: "Failed last hour",
          viewDetails: "View ingestion details →",
        },
        redFlags: {
          title: "Attention needed",
          empty: "All systems normal.",
          severity: {
            info: "Info",
            warn: "Warning",
            error: "Critical",
          },
        },
      },
      ingestion: {
        title: "Memory ingestion",
        description:
          "Stuck memories, silent failures, and ingestion throughput by source.",
        stuck: {
          title: "Stuck in processing",
          empty: "No memories stuck in processing.",
          memoryLabel: "{title}",
          noTitle: "Untitled memory",
          age: "Stuck for {duration}",
          source: "Source",
          contentType: "Type",
          actions: {
            forceActive: "Force active",
            forceActiveConfirm: "Force active",
            keep: "Keep",
            viewJob: "View latest job",
          },
          forceActivePrompt: "Note (optional)",
          forceActivePromptPlaceholder: "e.g. worker host lost mid-process",
          forceActiveSuccess: "{title} moved to active",
          forceActiveError: "Couldn't force active",
        },
        failed: {
          title: "Failed silently",
          description:
            "Memories that hit the processing fallback — active, but with a failure summary.",
          empty: "No silent failures in the window.",
          summaryLabel: "Error",
          age: "Failed {ago}",
          untitled: "Untitled",
          agoSuffix: "ago",
        },
        bySource: {
          title: "By source",
          description: "Ingestion counts grouped by where the push originated.",
          empty: "No pushes in the window.",
          columns: {
            source: "Source",
            created: "Created",
            processing: "Still processing",
            failed: "Failed",
          },
          mobileSummary: "{created} · {processing} proc · {failed} fail",
          windowSuffix: "Window: last {minutes} minutes",
        },
      },
      jobs: {
        title: "Jobs",
        description:
          "Raw River queue view — filter, inspect, retry, or cancel.",
        filters: {
          state: "State",
          kind: "Kind",
          queue: "Queue",
          clear: "Clear filters",
          anyState: "Any state",
          anyKind: "Any kind",
          anyQueue: "Any queue",
        },
        table: {
          job: "Job",
          kind: "Kind",
          queue: "Queue",
          state: "State",
          attempt: "Attempt",
          created: "Created",
          actions: "Actions",
        },
        empty: "No jobs match these filters.",
        loadMore: "Load more",
        loading: "Loading jobs…",
        detail: {
          title: "Job {id}",
          backToList: "All jobs",
          sections: {
            summary: "Summary",
            args: "Arguments",
            errors: "Errors",
            metadata: "Metadata",
          },
          attemptOf: "{attempt} of {max}",
          noErrors: "No errors recorded.",
          copyArgs: "Copy arguments",
          copied: "Copied",
          logs: {
            title: "Logs",
            hint: "Live output from the worker processing this job.",
            lineCount: "{count} lines",
            empty:
              "No logs yet. Logs typically appear 3–5 seconds after the worker starts.",
            unavailable:
              "Log streaming isn't configured on this environment. Ask infra to set LOKI_URL / LOKI_USERNAME / LOKI_PASSWORD.",
            unavailableBadge: "Not configured",
            error: "Couldn't read logs from the log store.",
            live: "Live",
            disconnected: "Disconnected",
            reconnecting: "Reconnecting…",
            pause: "Pause scroll",
            resume: "Resume scroll",
            copyAll: "Copy all",
            copied: "Copied",
            jumpToLatest: "Jump to latest",
            showAbsolute: "Absolute time",
            showRelative: "Relative time",
            toggleTimeHint: "Toggle absolute/relative timestamps (press A)",
          },
        },
        actions: {
          retry: "Retry",
          retryConfirm: "Retry",
          cancel: "Cancel job",
          cancelConfirm: "Cancel",
          keep: "Keep",
          retrySuccess: "Job {id} re-scheduled",
          cancelSuccess: "Job {id} cancelled",
          unavailable: "Job actions unavailable (River client not wired).",
        },
        state: {
          available: "Available",
          running: "Running",
          scheduled: "Scheduled",
          completed: "Completed",
          discarded: "Discarded",
          cancelled: "Cancelled",
          retryable: "Retryable",
          pending: "Pending",
        },
      },
    },
  },
} as const;

// Recursively widen string literals to `string` so other locales can use different values
type DeepStringify<T> = T extends readonly string[]
  ? readonly string[]
  : T extends string
    ? string
    : T extends object
      ? { [K in keyof T]: DeepStringify<T[K]> }
      : T;

export type Translations = DeepStringify<typeof en>;
