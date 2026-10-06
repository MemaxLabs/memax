import type { Plural } from "../lib/format";
import type {
  DreamItemKind,
  MarkState,
  NavPlace,
  StatementState,
  SyncStatus,
} from "../lib/types";
import type { AgentSurface } from "../lib/agents";

/**
 * Every word the Ledger components show or announce. Data (statements, names,
 * IDs, times) comes in through props; these are the interface's own words.
 *
 * Voice: sentence case, no "AI", no exclamation marks or emoji. The verbs are
 * Keep, Edit, Reject, Forget, Remember, Review, Hand off, Compile and Verify.
 * Placeholders are `{name}`; counts use `Plural`. `en` is the source of truth,
 * and the type makes every other locale complete.
 */
export interface LedgerStrings {
  common: {
    /** Joins parts of an accessible label ("Kept, Oct 2, M-0219"). */
    listSeparator: string;
  };
  /** State words, as StateMark shows them. */
  state: Record<MarkState, string>;
  /** Lower-case state words for receipts ("kept · Oct 2"), when no action is given. */
  receiptVerb: Record<StatementState, string>;
  surface: Record<AgentSurface | "person" | "agent", string>;
  agent: {
    /** Name for an actor with no name at all. */
    fallback: string;
    /** The signed-in person, beside their stamp. */
    you: string;
  };
  time: {
    justNow: string;
  };
  diff: {
    /** Read before removed words by assistive technology. */
    removed: string;
    added: string;
  };
  cite: {
    source: string;
    sourceTitled: string;
  };
  memoryRow: {
    actions: string;
    actionsFor: string;
  };
  memoryText: {
    /** Read after a statement that isn't simply kept. */
    stateSuffix: string;
  };
  redaction: {
    bar: string;
    atYourRequest: string;
    by: string;
  };
  seal: {
    /** Set in capitals around the seal's ring. */
    label: string;
  };
  review: {
    keptByYou: string;
    keptBy: string;
    conflict: string;
    external: string;
    updates: string;
    replaced: string;
    aKeptMemory: string;
    keptNow: string;
    into: string;
    proposedBy: string;
    keep: string;
    keepReplace: string;
    edit: string;
    reject: string;
    undo: string;
  };
  dream: {
    masthead: string;
    issue: string;
    title: string;
    notes: Plural;
    facts: Plural;
    fold: string;
    kind: Record<DreamItemKind, string>;
  };
  handoff: {
    done: string;
    next: string;
    questions: string;
    /** Read between the two stamps of a route. */
    to: string;
    accepted: string;
    withAgent: string;
    drafted: string;
  };
  autonomy: {
    label: string;
    read: string;
    propose: string;
    write: string;
    readHint: string;
    proposeHint: string;
    writeHint: string;
  };
  agentList: {
    agent: string;
    autonomy: string;
    reads: string;
    writes: string;
    lastSeen: string;
    target: string;
    readsLabel: string;
    writesLabel: string;
    mcpOnly: string;
    paused: string;
    notSeen: string;
    /** A count the server doesn't record yet (shown as "—"). */
    notRecorded: string;
  };
  sync: Record<SyncStatus, string>;
  gate: {
    waiting: string;
    keptIn: string;
    kept: string;
    answer: string;
    /** Why Answer is unavailable: "Choose 1, 2 or 3". */
    choose: string;
  };
  command: {
    label: string;
    placeholder: string;
    ask: string;
    remember: string;
    mode: string;
    open: string;
    keepAnswer: string;
    switchMode: string;
    close: string;
  };
  nav: {
    label: string;
    ask: string;
    settings: string;
    places: Record<NavPlace, string>;
  };
  shell: {
    skip: string;
  };
  /** Read before a terminal line's glyph, which is hidden from assistive technology. */
  terminal: {
    region: string;
    ok: string;
    kept: string;
    proposed: string;
    forgotten: string;
    warn: string;
  };
}

export const en: LedgerStrings = {
  common: {
    listSeparator: ", ",
  },
  state: {
    proposed: "Proposed",
    kept: "Kept",
    merged: "Merged",
    stale: "Stale",
    faded: "Faded",
    conflict: "Conflict",
    forgotten: "Forgotten",
    working: "Working",
    off: "Off",
  },
  receiptVerb: {
    proposed: "proposed",
    kept: "kept",
    merged: "merged",
    stale: "stale",
    faded: "faded",
    conflict: "conflict",
  },
  surface: {
    cli: "CLI",
    ide: "IDE",
    cloud: "Cloud",
    chat: "Chat",
    memax: "Memax",
    person: "Person",
    agent: "Agent",
  },
  agent: {
    fallback: "Agent",
    you: "You",
  },
  time: {
    justNow: "just now",
  },
  diff: {
    removed: "Removed:",
    added: "Added:",
  },
  cite: {
    source: "Source {n}",
    sourceTitled: "Source {n}: {title}",
  },
  memoryRow: {
    actions: "Memory actions",
    actionsFor: "Actions for {id}",
  },
  memoryText: {
    stateSuffix: "({state})",
  },
  redaction: {
    bar: "Forgotten memory",
    atYourRequest: "Forgotten {date} at your request",
    by: "Forgotten {date} by {name}",
  },
  seal: {
    label: "Kept",
  },
  review: {
    keptByYou: "Kept by you",
    keptBy: "Kept by {name}",
    conflict: "Conflicts with a kept memory",
    external: "From external content.",
    updates: "Updates {id}",
    replaced: "Replaced {id}",
    aKeptMemory: "a kept memory",
    keptNow: "Kept now",
    into: "into {space}",
    proposedBy: "Proposed by {name}",
    keep: "Keep",
    keepReplace: "Keep, replace old",
    edit: "Edit",
    reject: "Reject",
    undo: "Undo",
  },
  dream: {
    masthead: "Dream",
    issue: "No. {n}",
    title: "{notes} became {facts}.",
    notes: { one: "1 note", other: "{n} notes" },
    facts: { one: "1 fact", other: "{n} facts" },
    fold: "{notes} folded into {facts}",
    kind: {
      merged: "merged",
      conflict: "conflict",
      faded: "faded",
      kept: "kept",
    },
  },
  handoff: {
    done: "Done",
    next: "Next",
    questions: "Open questions",
    to: "to",
    accepted: "Accepted",
    withAgent: "With {name}",
    drafted: "Draft",
  },
  autonomy: {
    label: "Autonomy for {name}",
    read: "Read",
    propose: "Propose",
    write: "Write",
    readHint: "Reads context. Never writes.",
    proposeHint: "Writes go to Review first.",
    writeHint: "Writes are kept directly. Still receipted.",
  },
  agentList: {
    agent: "Agent",
    autonomy: "Autonomy",
    reads: "Reads 7d",
    writes: "Writes 7d",
    lastSeen: "Last seen",
    target: "Compiles to",
    readsLabel: "Reads in 7 days:",
    writesLabel: "Writes in 7 days:",
    mcpOnly: "MCP only",
    paused: "Paused",
    notSeen: "Not seen yet",
    notRecorded: "Not recorded yet",
  },
  sync: {
    synced: "In sync",
    drifted: "Drifted",
    pending: "Compiling",
    off: "Off",
  },
  gate: {
    waiting: "{name} is waiting on you",
    keptIn: "Your answer is kept in {space}, authored by you.",
    kept: "Your answer is kept, authored by you.",
    answer: "Answer",
    choose: "Choose {options}",
  },
  command: {
    label: "Ask or remember",
    placeholder: "Ask your context, or remember something…",
    ask: "Ask",
    remember: "Remember",
    mode: "Mode",
    open: "open",
    keepAnswer: "keep answer as memory",
    switchMode: "ask / remember",
    close: "close",
  },
  nav: {
    label: "Main",
    ask: "Ask or remember",
    settings: "Settings",
    places: {
      today: "Today",
      review: "Review",
      briefs: "Briefs",
      memories: "Memories",
      handoffs: "Handoffs",
      agents: "Agents",
      decisions: "Decisions",
    },
  },
  shell: {
    skip: "Skip to content",
  },
  terminal: {
    region: "Terminal: {title}",
    ok: "Done:",
    kept: "Kept:",
    proposed: "Proposed:",
    forgotten: "Forgotten:",
    warn: "Needs you:",
  },
};
