// Review and the conflict compare (Review.png, ReviewEdit.png,
// ReviewConflict.png, States2 "Permission"), mounted as
// `t.ledger.review`. The frame's own Review strings (title, filters,
// legend, the empty state) stay in `t.ledger.app`. Word for word where
// a board has the copy. Ledger voice applies (./voice.test.ts).
export const ledgerReviewEn = {
  queueLabel: "Waiting on you",
  loading: "Loading the queue",
  failed: "The queue didn't load. Nothing you kept was lost.",
  retry: "Try again",
  emptyFilter: "Nothing waiting under this filter.",
  showAll: "Show everything waiting",
  more: "Show more",
  // The filters when the queue's counts aren't known yet.
  filtersBare: {
    all: "All",
    conflicts: "Conflicts",
    external: "External",
    stale: "Stale",
  },
  position: "{space} · {n} of {total}",
  positionEditing: "{space} · {n} of {total} · editing",
  session: "Proposed in session {session}",
  previous: "Previous",
  next: "Next",
  external:
    "{agent} read this in {source}. Check the claim against the source before keeping it.",
  externalUnnamed:
    "{agent} read this in an outside source. Check the claim against it before keeping it.",
  quoted: "quoted in session {session}",
  viewer: "Viewers can propose and comment. A member keeps.",
  compareBoth: "Compare both sides",
  openSource: "Open the source",
  legend: {
    keepEdited: "keep edited",
    stopEditing: "stop editing",
    confirmReject: "reject",
    cancel: "cancel",
    stopWaiting: "stop waiting",
  },
  // The judge (plan §5.8): checking a proposal before a person decides.
  // Its mark is the neutral working one, never a spinner.
  judge: {
    checking: "Checking",
    working:
      "Memax is checking it against what's kept, so a duplicate or a conflict shows before you decide.",
    waiting:
      "Checking it against the decision in force. It's kept once the check is done.",
    failed:
      "Memax couldn't check it against what's kept. Review it as usual; Dream looks again tonight.",
  },
  // Keep on a flagged proposal settles the conflict: it replaces the
  // decision in force (ReviewCard's "Keep, replace old").
  keptOver: "Kept {ref} in place of {other}",
  nowConflict:
    "{ref} contradicts {other}, a decision in force. Compare both sides to settle it.",
  edit: {
    title: "Editing a proposal",
  },
  reject: {
    title: "Reject {ref}?",
    why: "Why, if you'd like to say",
    hint: "Kept in the receipt, so {agent} learns from it.",
    hintPerson:
      "Kept in the receipt, so whoever proposed it sees your reasoning.",
    confirm: "Reject",
    cancel: "Cancel",
  },
  flag: {
    stale: "Stale",
    conflict: "Conflicts with a kept memory",
    staleDetail: "{name} flagged it {when}. Check it against its source.",
    conflictDetail:
      "It contradicts another kept memory. Compare both sides to settle it.",
    verify: "Verify",
    verifyLater: "Verifying a stale fact isn't available yet",
    open: "Open the memory",
  },
  touches: {
    title: "This touches",
    replaces: "This replaces",
    memories: "{n} kept memories",
    memoriesOne: "1 kept memory",
    files: "{n} compiled files",
    filesOne: "1 compiled file",
    sameSection: "Kept in {section}",
    recompiles: "Keeping recompiles",
    becomesMerged: "Becomes merged into {ref} when you keep",
    nothing: "Nothing kept is related to it yet.",
    targetsLater: "Compiled files show here once Memax compiles this space.",
  },
  compare: {
    eyebrow: "Review · conflict · {proposal} against {kept}",
    // The title when nobody wrote a question: by the decision's area.
    titleArea: "Which {area} holds?",
    titleNone: "Which of these holds?",
    lede: "{agent}'s proposal contradicts what {name} kept on {date}. One of them gives way, or both stand with a narrower scope.",
    ask: "Ask {name}",
    keptSide: "Kept · in force since {date}",
    proposedSide: "Proposed by {agent} · contradicts {ref}",
    rows: {
      why: "Why",
      source: "Source",
      reaches: "Reaches",
      evidence: "Evidence",
      session: "Session",
    },
    reaches: "{files} · read {reads} times",
    files: "{n} files",
    filesOne: "1 file",
    changed: ", last changed {date}",
    question: "What should every agent read?",
    optionsLabel: "Resolution",
    // An answer's title when nobody wrote one.
    labels: {
      proposal: "{agent}'s proposal",
      kept: "{ref}, as kept",
      both: "Both, each with its own scope",
    },
    proposalDetail: "Keep {agent}'s proposal and replace {ref}.",
    keptDetail: "Reject {agent}'s proposal. {agent} is told why.",
    bothDetail: "Both stay true.",
    open: "Leave it open",
    openDetail: "Agents are told it's undecided.",
    openDetailWith: "Agents are told it's undecided, and {detail}",
    decision: "The decision, as it will read",
    // keep_both narrows each side rather than writing a third memory.
    decisionBoth: "Each side, as it will read",
    side: "{ref}, as it will read",
    openNote:
      "Both become open questions until someone decides. Nothing is kept as the answer.",
    choose: "Choose 1, 2, 3 or 4",
    // What an answer does to each side (spec ConflictChange), one sentence each.
    effects: {
      kept: "{ref} is kept.",
      rejected: "{ref} is rejected.",
      superseded: "{ref} is superseded and stops compiling.",
      faded: "{ref} fades.",
      open: "{ref} becomes an open question.",
      stays: "{ref} stays as it is.",
      // `both`, when the person narrowed that side's words.
      narrowed: "{ref} is kept with the narrower words.",
    },
    reach: "Keeping it recompiles {files} and tells {agents}.",
    reachFiles: "Keeping it recompiles {files}.",
    back: "Back to queue",
    keep: "Keep the decision",
    needsWords: "Write the decision as every agent should read it.",
    kept: "Kept {ref} as the decision",
    keptRecompiled: "Kept {ref} as the decision · {n} files recompiled",
    keptRecompiledOne: "Kept {ref} as the decision · 1 file recompiled",
    stays: "{kept} stays in force. Rejected {proposal}",
    keptBoth: "Kept {proposal} and {kept}, each narrowed",
    leftOpen: "Left {proposal} and {kept} open",
    nothing: {
      title: "There's nothing to compare for {ref}.",
      detail:
        "Only a conflict linked to a kept memory has two sides to compare. Review it in the queue instead.",
      back: "Back to queue",
    },
  },
} as const;
