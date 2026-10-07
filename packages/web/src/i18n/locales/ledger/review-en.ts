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
  // Rule 11: a Write agent's write kept at once, then put back in Review
  // by the judge (its `returned` receipt, as Activity says it).
  returned:
    "{agent} kept this at once. Memax returned it to Review: it contradicts {decision}, a decision in force.",
  returnedBare:
    "{agent} kept this at once. Memax returned it to Review: it contradicts a decision in force.",
  edit: {
    title: "Editing a proposal",
  },
  // A decision gate (G-) in the queue: an agent asked, and waits on the
  // answer (HANDOFF "Decision gate"; the card is Ledger's DecisionGate).
  gate: {
    session: "Asked in session {session}",
    confirm: "Answer “{label}”?",
    confirmDetail:
      "This becomes a kept decision by you, and compiles into every file.",
    cancel: "Cancel",
    answer: "Answer",
    // D15: decisions in this space need a person on the web, and the
    // frame knows this session wasn't issued to the web app.
    needsWeb:
      "Decisions in {space} are answered only on memax.app, and Memax can't confirm this session is. Sign in again here, then answer.",
    needsWebReason: "Sign in again on memax.app to answer.",
    signInAgain: "Sign in again",
    viewer: "Viewers can read the question. A member answers it.",
    withdraw: "Withdraw",
    withdrawTitle: "Withdraw {ref}?",
    withdrawDetail:
      "{agent} hears that you took the question back on its next read. Nothing is kept.",
    // The head once it isn't waiting.
    answered: "You answered",
    status: {
      answered: "Answered",
      withdrawn: "Withdrawn",
      expired: "Expired",
    },
    // How it ended, in place of the footer.
    ended: {
      answeredByYou: "You answered it already: “{label}”, kept as {memory}.",
      answeredBy:
        "A teammate answered it already: “{label}”, kept as {memory}.",
      answeredBare: "It was answered already.",
      withdrawnByAgent: "{agent} took the question back.",
      withdrawnByYou: "You took the question back.",
      withdrawnBy: "A teammate took the question back.",
      expired:
        "It expired, so {agent} stopped waiting. It can't be answered now.",
    },
    kept: "Kept {memory} as your answer to {ref}",
    keptRecompiled:
      "Kept {memory} as your answer to {ref} · {n} files recompiled",
    keptRecompiledOne:
      "Kept {memory} as your answer to {ref} · 1 file recompiled",
    openDecision: "Open {memory}",
    withdrew: "Withdrew {ref}. {agent} hears it on its next read.",
    notFound: "There's no question {ref} in {space}.",
    failed: "The questions agents asked didn't load.",
    retry: "Try again",
    legend: {
      choose: "choose",
      answer: "answer",
      cancel: "cancel",
    },
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
    // Rule 11 for "both": narrower words that touch another decision in
    // force wait for the judge before both are kept.
    checkingBoth:
      "Checking the narrower words against the other decisions in force. Both are kept once the check is done.",
    stopChecking: "Stop checking",
    stoppedChecking:
      "Stopped checking. Your narrower words stay as you wrote them: keep the decision again to check them.",
    inConflictBoth:
      "The narrower words contradict {with}, a decision in force. Change them, or choose another answer.",
    // The other answers keep a proposal's words as they stand: words the
    // judge hasn't seen yet wait for it the same way.
    checkingWords:
      "Checking these words against the other decisions in force. It's settled once the check is done.",
    stoppedCheckingWords:
      "Stopped checking. Nothing is settled yet: keep the decision again to check it.",
    inConflictWords:
      "{ref} contradicts {with}, a decision in force too. Settle that first, or choose another answer.",
    nothing: {
      title: "There's nothing to compare for {ref}.",
      detail:
        "Only a conflict linked to a kept memory has two sides to compare. Review it in the queue instead.",
      back: "Back to queue",
    },
  },
} as const;
