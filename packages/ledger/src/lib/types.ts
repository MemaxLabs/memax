/** A memory's displayed state: its lifecycle, with the stale and conflict flags folded in. */
export type MemoryState =
  | "proposed"
  | "kept"
  | "merged"
  | "stale"
  | "faded"
  | "conflict"
  | "forgotten";

/** Lifecycle states plus the two process states: working (in progress) and off (paused). */
export type MarkState = MemoryState | "working" | "off";

/** States a statement can be shown in. A forgotten memory has no words left: use `Redaction`. */
export type StatementState = Exclude<MemoryState, "forgotten">;

/** An agent's autonomy: Read never writes, Propose sends writes to Review, Write keeps them. */
export type Autonomy = "read" | "propose" | "write";

/** The six places, plus Decisions in a team space. */
export type NavPlace =
  | "today"
  | "review"
  | "briefs"
  | "memories"
  | "handoffs"
  | "agents"
  | "decisions";

export type SyncStatus = "synced" | "drifted" | "pending" | "off";

export type HandoffStatus = "drafted" | "sent" | "accepted";

export type DreamItemKind = "merged" | "conflict" | "faded" | "kept";

export type TerminalLineKind =
  | "cmd"
  | "out"
  | "ok"
  | "kept"
  | "proposed"
  | "forgotten"
  | "warn"
  | "dim"
  | "blank";
