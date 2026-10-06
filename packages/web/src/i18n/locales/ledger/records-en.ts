// What Review, Memories and a memory's page share, mounted as
// `t.ledger.records`: receipt verbs, actors, times, the statement
// editor, the edit clash, row notes and why a command didn't go
// through. Strings on a board are word for word (screens/source/*).
// Placeholders are {name}; `<key>One` is the singular of `<key>`.
// Ledger voice applies (./voice.test.ts).
export const ledgerRecordsEn = {
  // Receipt verbs, lower case, as rails and receipts print them.
  verbs: {
    proposed: "proposed",
    kept: "kept",
    edited: "edited",
    rejected: "rejected",
    merged: "merged",
    flagged: "flagged",
    resolved: "resolved",
    verified: "verified",
    faded: "faded",
    restored: "restored",
    forgot: "forgotten",
    moved: "moved",
    compiled: "compiled",
    handed_off: "handed off",
    answered: "answered",
    undid: "undone",
    // Review's queue: a proposal that changes a kept memory, and the
    // one being edited (Review.png, ReviewEdit.png).
    updated: "update",
    editing: "editing",
  },
  actor: {
    you: "You",
    teammate: "A teammate",
    dream: "Dream",
    memax: "Memax",
    repository: "The repository",
  },
  time: {
    minutesAgo: "{n} min ago",
    hoursAgo: "{n} h ago",
    justNow: "just now",
    dateTime: "{date}, {time}",
  },
  // A session in a receipt ("session 3e1a").
  sessionRef: "session {session}",
  sections: {
    decisions: "Decisions",
    conventions: "Conventions",
    preferences: "Preferences",
    open_question: "Open question",
  },
  sourceKinds: {
    session: "Session",
    pr: "Pull request",
    file: "Repository",
    url: "Web",
    issue: "Issue",
    email: "Email",
    note: "Note",
    import: "Import",
  },
  notes: {
    conflict: "Conflicts with {ref}, kept by {name}.",
    conflictAsked: "{agent} asked you to decide.",
    stale: "Its source changed on {date}",
    staleSource: "Its source changed on {date} · {source}",
    merged: "Merged into {ref} by {name}",
    faded: "No agent has read it in {n} days. Restore it, or let it rest.",
  },
  editor: {
    statement: "Statement",
    yourChangeTo: "Your change to {name}'s proposal",
    yourChange: "Your change",
    why: "Why you changed it",
    whyHint: "Kept in the receipt, so agents and teammates see your reasoning.",
    editedFrom: "edited from {name}",
    cancel: "Cancel",
    keepEdited: "Keep edited",
    unchanged: "Change the statement first, or press Esc to stop editing.",
    empty:
      "A memory needs its words. Write the statement, or press Esc to stop editing.",
  },
  clash: {
    region: "Edit clash",
    by: "{name} kept a change to this fact {ago}.",
    byYou: "You kept a change to this fact {ago}, in another window.",
    byTeammate: "Someone kept a change to this fact {ago}.",
    yours: "Yours: “{text}”",
    keepTheirs: "Keep theirs",
    combine: "Combine both",
    keepMine: "Keep mine",
  },
  toast: {
    rejected: "Rejected {ref}",
    edited: "Edited {ref}",
    editedRecompiled: "Edited {ref} · {n} files recompiled",
    editedRecompiledOne: "Edited {ref} · 1 file recompiled",
    copied: "Copied [{ref}] and its link",
    copyFailed: "That didn't copy. Select the ID and copy it yourself.",
  },
  // Why a command didn't go through, and what to do (States board).
  failure: {
    keep: "{ref} wasn't kept.",
    reject: "{ref} wasn't rejected.",
    edit: "Your edit to {ref} wasn't kept.",
    unreachable: "It didn't reach Memax, so nothing changed. Try again.",
    rateLimited: "That was a lot at once. Wait {n} seconds, then try again.",
    rateLimitedSoon: "That was a lot at once. Wait a moment, then try again.",
    decided: "It was already decided, so it's gone from your queue.",
    notFound: "It isn't here anymore. It may have been forgotten or moved.",
    clash: "It changed since you opened it. The newest version is on screen.",
    unavailable: "That isn't available yet.",
    unknown: "Try again, and reload the page if it keeps happening.",
    retry: "Try again",
    // By spec PolicyCode.
    refused: {
      viewer: "Viewers can propose and comment. A member keeps.",
      owners_keep: "Only owners keep in {space}. Ask an owner to keep it.",
      external_needs_review:
        "It quotes an outside source, so only a web sign-in can keep it. Sign out, sign in again on this page, then keep it.",
      decision_needs_web:
        "Decisions in {space} need a web sign-in to keep. Sign out, sign in again on this page, then keep it.",
      person_must_review:
        "Only a person can decide it. Sign in as yourself, not with an agent's key.",
      key_cannot_review:
        "API keys can propose but never keep or reject. Sign in on the web to review it.",
      proposal_in_review:
        "It's waiting in Review. Keep or reject it there, or propose a new memory.",
      secret_detected:
        "It looks like a credential, and Memax never stores secrets. Remove it and try again.",
      not_member: "You're not a member of {space}. Ask an owner to invite you.",
      unknown_actor: "Memax doesn't recognise this session. Sign in again.",
      other: "Memax refused it: {message}",
      otherBare: "Memax refused it.",
    },
  },
} as const;
