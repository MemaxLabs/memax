// Dream's catalogue (DreamEdition.png, the Dream card, the morning
// email's settings and its unsubscribe page), mounted as `t.ledger.dream`.
// ./dream-zh.ts matches it key for key.
export const ledgerDreamEn = {
  title: "Dream",
  eyebrow: "{space} · Dream edition No. {n}",
  heading: "{date}, overnight",
  headingRun: "{date}, run at {time}",
  lede: "Dream read {notes} from {who} between {from} and {to}. Everything it changed is listed here, and every change can be undone.",
  ledeSince:
    "Dream read {notes} from {who} since {from}. Everything it changed is listed here, and every change can be undone.",
  ledeFirst:
    "Dream read {notes} from {who}. Everything it changed is listed here, and every change can be undone.",
  ledeNoNotes:
    "Dream read what changed in the record, and no new notes. Everything it changed is listed here, and every change can be undone.",
  notes: "{n} notes",
  notesOne: "1 note",
  who: {
    agents: "{n} agents",
    agentsOne: "one agent",
    chats: "{n} chats",
    chatsOne: "one chat",
    chatsSome: "chats",
    you: "you",
  },
  settings: "Dream settings",
  resolve: "Resolve the conflict",
  resolveMany: "Resolve the conflicts",
  card: {
    open: "Read edition No. {n}",
    faded: "{n} memories unread by any agent for 60 days.",
    fadedOne: "1 memory unread by any agent for 60 days.",
  },
  folded: {
    title: "Folded into what you kept",
    undo: "Undo",
    undoBoth: "Undo both",
    undoAll: "Undo all",
    note: "Dream folded {n} notes into it: {from} to {to}",
    noteOne: "Dream folded 1 note into it: {ref}",
    notePlain: "Dream folded {n} notes into it",
  },
  facts: {
    title: "New facts",
    meta: "{n} from {notes}",
    review: "Review {n}",
    by: "From {notes} by {who}",
    inChats: "From {notes} in {chats}",
    plain: "From {notes}",
    waiting: "waiting in Review",
    mayWrite: "which may write here",
    withdraw: "Withdraw",
  },
  repeats: {
    title: "Repeats folded",
    note: "It repeated {ref}, which waits in Review.",
    noteBare: "It repeated a proposal waiting in Review.",
  },
  needsYou: {
    title: "Needs you",
    conflict: "Contradicts {ref}, kept by {name}. Compare them side by side.",
    conflictBare: "Contradicts {ref}. Compare them side by side.",
    conflictPlain: "It contradicts a memory in force. Settle it in Review.",
    staleSource: "Its source changed on {date} in {source}. Verify it.",
    staleDate: "Its date to check again has passed. Verify it.",
    unflag: "Unflag",
  },
  faded: {
    title: "Faded",
    meta: "{n} unread for 60 days",
    restore: "Restore",
    restoreAll: "Restore all",
    more: "{n} more",
    fewer: "Show fewer",
  },
  brief: {
    title: "The Brief",
    line: "Dream made {n} small changes to the Brief ({ref}), each citing what it rests on.",
    lineOne:
      "Dream made one small change to the Brief ({ref}), citing what it rests on.",
    undo: "Undo",
  },
  side: {
    lastNight: "Last night",
    thisRun: "This run",
    notesRead: "Notes read",
    you: "You",
    chats: "Chats",
    facts: "Facts",
    undone: "Undone",
    run: "Run",
    runValue: "{time} · {n} s",
    earlier: "Earlier editions",
    issue: "No. {n} · {date}",
    became: "{notes} became {facts}.",
    factsCount: "{n} facts",
    factsOne: "1 fact",
    first: "This is the space's first edition.",
    next: "Next edition {day} at {time}.",
    never: "Dream never keeps anything an agent could not have kept itself.",
    runNow: "Run Dream now",
    queued:
      "Dream is on its way. Its edition appears here when it's done, if there's something new to read.",
  },
  empty: {
    title: "No edition yet",
    detail:
      "Dream publishes its first edition the night after there's something to read: notes from your agents, or changes to the record.",
    next: "The first can come {day} at {time}.",
    notFound: "There's no edition {ref} in this space.",
    latest: "Go to the latest edition",
  },
  done: {
    undone: "Undid it. {ref} is as it was before Dream.",
    undoneBrief: "Undid Dream's change to the Brief.",
    undoneMany: "Undid {n} of Dream's changes.",
    undoneOne: "Undid 1 of Dream's changes.",
    restored: "Restored {ref}. It compiles again.",
    restoredMany: "Restored {n} memories. They compile again.",
    restoredOne: "Restored 1 memory. It compiles again.",
    someRefused:
      "{n} couldn't be undone: something changed them since Dream acted.",
    someRefusedOne:
      "1 couldn't be undone: something changed it since Dream acted.",
  },
  refused: {
    window_passed:
      "Dream's changes can be undone for 30 days, and this one is older. Change {ref} on its page instead.",
    already_undone: "That was already undone. {ref} is as it was before Dream.",
    later_changes:
      "{ref} changed after Dream acted, so undoing it would lose that change. Change it on its page instead.",
    not_undoable:
      "That change can't be undone any more. Change {ref} on its page instead.",
    unreachable: "Undo didn't reach Memax, so nothing changed. Try again.",
    runSoon: "Dream is already on its way for this space. Give it a minute.",
    runCap:
      "Dream has run now as often as your plan allows for this space. It still runs on its own each night.",
    runOwner: "Only the space's owner can run Dream now.",
    failed: "That didn't go through. Try again.",
  },
  settingsPanel: {
    title: "Dream",
    meta: "For every space you own",
    timeZone: "Time zone",
    timeZoneHint:
      "Dream runs in your night, at 03:00 where you are. It learns your zone from this app's clock until you set one.",
    timeZoneDefault:
      "Memax doesn't know your zone yet, so Dream runs at 03:00 UTC.",
    useDevice: "Use {zone}",
    email: "Morning email",
    emailHint: "The edition, each morning Dream has one, with what needs you.",
    on: "On",
    off: "Off",
    changed: "Dream settings changed.",
  },
  unsubscribe: {
    title: "Morning email",
    lede: "Dream sends its edition by email each morning there is one. Turn that off here, without signing in.",
    confirm: "Turn the morning email off",
    done: "The morning email is off",
    doneDetail:
      "You won't get Dream's morning edition by email any more. Each edition is still on Today, and you can turn the email back on in Settings.",
    missing:
      "This link has no token. Use the link from the email, or turn the email off in Settings.",
    failed:
      "That didn't go through. Try the link again, or turn the email off in Settings.",
    settings: "Open Settings",
  },
} as const;
