// VerifyStale (VerifyStale.png), mounted as `t.ledger.verify`: a stale
// fact, what changed, and the person's answer. Strings on the board are
// word for word (screens/source/VerifyStale.dc.html). Placeholders are
// {name}; `<key>One` is the singular of `<key>`. Ledger voice applies
// (./voice.test.ts).
export const ledgerVerifyEn = {
  title: "Verify {ref}",
  close: "Close",
  // The StateMark's label beside the title.
  since: "Stale since {date}",
  sinceBare: "Stale",
  // The receipt under the statement: "kept · Jul 14 · read 88 times since".
  readsSince: "read {n} times since",
  readsSinceOne: "read once since",
  readsSinceNone: "not read since",
  changed: "What changed",
  // A pull request or commit that changed what it cites.
  merged: "merged {date} by",
  mergedBare: "merged {date}",
  // Its date to check again passed (Dream's flag, or the date sweep's).
  datePassed: "Its date to check again, {date}, has passed.",
  flaggedBy: "flagged {date} by",
  flaggedBare: "flagged {date}",
  noCause:
    "It was flagged as stale. Check it against its sources before you answer.",
  hunkLabel: "The change to {path}",
  trueNow: "What's true now?",
  trueNowLabel: "What's true now",
  stillTrue: "Still true, keep it",
  stillTrueSource: "It's checked again when the source next changes.",
  stillTrueDate: "Its date to check again goes. Nothing else changes.",
  update: "Update it",
  updateWrite: "Write what's true now. It's kept as a new version.",
  updateLabel: "What's true now, in your words",
  updateEdit: "Change the words",
  updateHint: "{key} keeps the update.",
  forget: "Forget it",
  forgetChecking: "Checking what forgetting it would remove…",
  forgetFailed:
    "What forgetting it would remove didn't load. Forget it from its page instead.",
  removes: "Removed from {where}. A tombstone stays.",
  removesFiles: "{n} files and every agent",
  removesFilesOne: "1 file and every agent",
  removesAgents: "every agent",
  forgetNote: "A note for the tombstone, if you'd like",
  forgetNoteHint: "It stays after the words are gone, so don't repeat them.",
  // The footer's stamp and the primary button, which follows the answer.
  suggested: "{name} suggested the update from {source}",
  cancel: "Cancel",
  choose: "Choose what's true now: 1, 2 or 3.",
  keep: "Keep it",
  keepUpdate: "Keep the update",
  forgetSubmit: "Forget it",
  // While it loads, and when there's nothing to verify any more.
  loading: "Reading why {ref} is stale…",
  failed: "Why {ref} is stale didn't load.",
  retry: "Try again",
  notStale: "{ref} isn't stale any more. Someone verified it, or it changed.",
  notFound: "{ref} isn't here anymore. It may have been forgotten.",
  // The toasts after an answer.
  done: {
    stillTrue: "Verified {ref}. It's kept as it is.",
    update: "Updated {ref} and verified it.",
    forget: "Forgot {ref}",
  },
  // Its words are the same as before: nothing to update.
  unchanged: "Change the words first, or answer still true.",
  viewer: "Viewers can read why it's stale. A member verifies it.",
  empty: "A memory needs its words. Write what's true now.",
} as const;
