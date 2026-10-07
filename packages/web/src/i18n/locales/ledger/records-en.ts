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
    // The judge put a write kept at once back in Review (rule 11).
    returned: "back in Review",
    // A decision gate's receipts.
    asked: "asked",
    withdrawn: "withdrawn",
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
  // When a decision gate stops waiting (Review's card, Today's row).
  expires: {
    minutes: "Expires in {n} min",
    today: "Expires today at {time}",
    tomorrow: "Expires tomorrow at {time}",
    date: "Expires {date}",
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
    resolve: "{ref} wasn't settled.",
    answer: "{ref} wasn't answered.",
    withdraw: "{ref} wasn't withdrawn.",
    // A decision gate that ended before the command reached it (409 details.status).
    ended: {
      answered: "It was answered already.",
      withdrawn: "{agent} took the question back.",
      expired: "It expired, so {agent} stopped waiting.",
    },
    unreachable: "It didn't reach Memax, so nothing changed. Try again.",
    busy: "Another change is holding it. Try again in a moment.",
    busyJudge:
      "Memax is still checking it against the decision in force. Try again in a moment.",
    inConflict:
      "It contradicts {with}, a decision in force. Compare both sides to settle it.",
    inConflictBare:
      "It contradicts a decision in force. Compare both sides to settle it.",
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
      undo_by_decider:
        "Only the person who decided {ref} can undo it. Change it instead, or ask them.",
      person_must_answer:
        "Only a person answers a question. Sign in as yourself, not with an agent's key.",
      not_your_gate:
        "Only the agent that asked, the person it works for, or someone who can answer it can withdraw it.",
      other: "Memax refused it: {message}",
      otherBare: "Memax refused it.",
    },
    // Answering a decision gate follows Keep's rules for a decision; these
    // codes read differently there (spec PolicyCode).
    refusedAnswer: {
      decision_needs_web:
        "Decisions in {space} are answered only on memax.app, and Memax couldn't confirm this came from there. Sign in again here, then answer.",
      viewer: "Viewers can read the question. A member answers it.",
      owners_keep: "Only owners answer decisions in {space}. Ask an owner.",
      key_cannot_review:
        "API keys can ask but never answer. Answer it in Review on the web.",
    },
    needsWebDev:
      "In local development, set WEB_SURFACE_SECRET for the web app and the API.",
    signInAgain: "Sign in again",
  },
  // Undo (Review's ⌘Z, the toasts' Undo, a fold's Undo). Toasts about an
  // undo carry no state mark: nothing was kept, and nothing waits on you.
  undo: {
    action: "Undo",
    done: {
      keep: "Undid the keep. {ref} is back in Review.",
      reject: "Undid the rejection. {ref} is back in Review.",
      edit: "Undid your edit. {ref} reads as it did before.",
      editKeep: "Undid the edit and keep. {ref} is back in Review.",
      resolve: "Undid the settlement. {ref} is back in Review as a conflict.",
      fold: "Unfolded {ref}. It's back in Review.",
    },
    // Why an undo didn't go through, by spec UndoRefusal and policy code.
    refused: {
      window_passed:
        "{ref} was decided more than 10 minutes ago, so it can't be undone. Change it on its page instead.",
      window_passed_fold:
        "{ref} was folded more than 14 days ago, so it can't be unfolded. Change it on its page instead.",
      already_undone: "That was already undone. {ref} is as it was before.",
      not_undoable:
        "That change to {ref} can't be undone. Change it on its page instead.",
      later_changes:
        "{blocker} changed after this, so undoing it would lose that change. Undo that first, or change {ref} on its page.",
      later_changes_self:
        "{ref} changed after this, so undoing it would lose that change. Change it on its page instead.",
      later_brief:
        "The Brief cites {ref} now. Take it out of the Brief first, then undo.",
      undo_by_decider:
        "Only the person who decided {ref} can undo it. Change it instead, or ask them.",
    },
    unreachable: "Undo didn't reach Memax, so nothing changed. Try again.",
    notFound: "{ref} isn't here anymore, so there's nothing to undo.",
    other: "Memax refused the undo: {message}",
    unknown:
      "Undo didn't go through. Try again, and reload the page if it keeps happening.",
  },
} as const;
