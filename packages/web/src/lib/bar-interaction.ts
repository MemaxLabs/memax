/**
 * Slash command space. `/topic` was removed 2026-04-21 — topic scoping
 * now uses `@` mention. The URL route `/memories/topics/{id}` is
 * unchanged; only the bar trigger differs.
 */
export type BarCommand = "hub";

export type BarPhase = "input" | "loading" | "recall-result";

export type MobileComposeState = "docked" | "mirror" | "fullscreen";

export type BarScopeState =
  | { kind: "recall" }
  | { kind: "topic" }
  | { kind: "command"; command: BarCommand };

export interface BarInteractionInput {
  value: string;
  stagedFileCount: number;
  allFilesReady: boolean;
  scope: BarScopeState;
  phase: BarPhase;
  isBarExpanded: boolean;
  /** True while an IME composition session is open (CJK/emoji keyboards).
   *  Mention-mode + chip-Backspace must stay off during composition so a
   *  transient buffer that happens to start with "@" doesn't hijack UX. */
  composing?: boolean;
}

export interface DerivedBarInteractionState {
  hasText: boolean;
  hasFiles: boolean;
  filesUploading: boolean;
  isCommandScope: boolean;
  isSlashDiscovery: boolean;
  isCommandMode: boolean;
  isMentionMode: boolean;
  isRememberMode: boolean;
  isRecallMode: boolean;
  showRecallSurfaces: boolean;
  hasLiftSignal: boolean;
  hasExpandContent: boolean;
  canRemember: boolean;
  sendKind: "recall" | "remember";
  barMode: "push" | "recall";
}

export function parseSlashCommand(input: string) {
  if (!input.startsWith("/")) return null;
  const trimmed = input.slice(1).trimStart();
  if (!trimmed) return { command: "", query: "" };
  const [command = "", ...rest] = trimmed.split(/\s+/);
  return {
    command: command.toLowerCase(),
    query: rest.join(" ").trim().toLowerCase(),
  };
}

export function parseCommittedCommand(
  text: string,
): { command: BarCommand; query: string } | null {
  const match = text.match(/^\/(hub)(?:\s+(.*))?$/i);
  if (!match) return null;
  const command = match[1].toLowerCase() as BarCommand;
  const query = (match[2] ?? "").trimStart();
  return { command, query };
}

/**
 * `@`-mention: start-of-input only. Returns the query suffix (everything
 * after the `@`) when in mention mode, else null. Normalizes full-width
 * `＠` (U+FF20, common on CJK keyboards) and returns null while the user
 * is mid-IME-composition so transient buffers can't trigger mode flips.
 */
export function parseMention(
  value: string,
  options: { composing?: boolean } = {},
): { query: string } | null {
  if (options.composing) return null;
  const ch0 = value.charAt(0);
  if (ch0 !== "@" && ch0 !== "＠") return null;
  return { query: value.slice(1) };
}

export function getRawCommandInput(command: BarCommand) {
  return `/${command}`;
}

export function getSendKind(stagedFileCount: number): "recall" | "remember" {
  // Remember is explicit: staged files or forceRemember (Cmd+Enter / Remember
  // action). Multiline text alone recalls — Enter is always recall per the
  // bar north-star (docs/design/bar-northstar.md), save is via ⌘↵.
  return stagedFileCount > 0 ? "remember" : "recall";
}

export function getShouldRememberSubmit(input: {
  stagedFileCount: number;
  forceRemember: boolean;
}) {
  return (
    input.forceRemember || getSendKind(input.stagedFileCount) === "remember"
  );
}

/**
 * Pick the nav item index that Enter should activate when nothing is
 * highlighted. Returns `null` to signal "no default — fall through to the
 * global submit handler (recall)."
 *
 * Rules:
 *   - An explicitly highlighted index always wins.
 *   - Otherwise, if there is exactly one nav item, activate it — matches
 *     Raycast/Linear "one obvious action, press Enter to run it."
 *   - Exception: the `__remember` entry is never the Enter default per the
 *     bar north-star (docs/design/bar-northstar.md §4.3 — "saving is an
 *     explicit action, never a side-effect of Enter"). Remember has its own
 *     shortcut (⌘↵) and its own clickable row.
 *
 * This keeps non-Pro users consistent with Pro: Pro users have two items
 * (synthesis + remember) so the singleton path never fired; non-Pro users
 * saw only `__remember` as the singleton, which would auto-fire remember
 * before this guard.
 */
export function pickDefaultNavIndex(
  items: ReadonlyArray<{ id: string }>,
  activeIndex: number | null,
): number | null {
  if (activeIndex !== null) return activeIndex;
  if (items.length !== 1) return null;
  if (items[0].id === "__remember") return null;
  return 0;
}

/**
 * What the bar should do when the user clicks outside it. Pure decision so
 * the layout-level pointerdown handler stays a thin dispatch and the
 * branches can be unit-tested (see bar-interaction.test.ts).
 *
 *   · `hide-overlay`         — Cmd+K overlay on a non-bar page → hide.
 *   · `clear-ai`             — AI stream in progress (aiLoading) → cancel
 *                              + dismiss AI only. Narrowed from the prior
 *                              `askAnswer || aiLoading` (codex-review
 *                              2026-04-21): a FINISHED answer preserves
 *                              via `suppress-with-draft` like other
 *                              recall-result state.
 *   · `suppress-with-draft`  — draft OR recall state present → set
 *                              barPanelSuppressed so the bar returns to
 *                              rest while every field (value, recallQuery,
 *                              phase, askAnswer, remember*, stagedFiles)
 *                              stays intact. Focus-into-bar or ⌘K clears
 *                              the suppression. This is now the default
 *                              outside-click behavior in EVERY phase,
 *                              including recall-loading / recall-result.
 *   · `collapse-empty`       — input phase + nothing to preserve → blur
 *                              + collapse expansion chrome.
 *
 * `reset` is no longer emitted — user-facing asks 2026-04-21:
 *   "有 recall 结果后 点击背景应该也只是收起来 而不是回退搜索框状态"
 *   (background click after recall results should collapse, not reset).
 */
export type BarClickOutsideAction =
  | "hide-overlay"
  | "clear-ai"
  | "suppress-with-draft"
  | "collapse-empty";

export interface BarClickOutsideInput {
  isOverlay: boolean;
  aiLoading: boolean;
  phase: BarPhase;
  hasState: boolean;
}

export function getBarClickOutsideAction(
  input: BarClickOutsideInput,
): BarClickOutsideAction {
  if (input.isOverlay) return "hide-overlay";
  // Only active streaming cancels on outside-click — a finished answer
  // is preserved via the suppress path below.
  if (input.aiLoading) return "clear-ai";
  if (input.phase === "input") {
    return input.hasState ? "suppress-with-draft" : "collapse-empty";
  }
  // recall-loading / recall-result → suppress (preserve query + results).
  return "suppress-with-draft";
}

export function deriveBarInteractionState(
  input: BarInteractionInput,
): DerivedBarInteractionState {
  const hasText = input.value.trim().length > 0;
  const hasFiles = input.stagedFileCount > 0;
  const sendKind = getSendKind(input.stagedFileCount);
  const isCommandScope = input.scope.kind === "command";
  const isSlashDiscovery = !isCommandScope && input.value.startsWith("/");
  const isCommandMode = isCommandScope || isSlashDiscovery;
  const isMentionMode = parseMention(input.value, {
    composing: input.composing,
  })
    ? true
    : false;
  const filesUploading = hasFiles && !input.allFilesReady;

  return {
    hasText,
    hasFiles,
    filesUploading,
    isCommandScope,
    isSlashDiscovery,
    isCommandMode,
    isMentionMode,
    isRememberMode: sendKind === "remember",
    isRecallMode: sendKind === "recall",
    // Mention mode owns the expand surface (topic picker) — it must not
    // simultaneously drive recall synthesis / quick-match rows.
    showRecallSurfaces:
      sendKind === "recall" && !hasFiles && !isCommandMode && !isMentionMode,
    hasLiftSignal:
      hasText ||
      isCommandMode ||
      isMentionMode ||
      input.isBarExpanded ||
      input.phase !== "input" ||
      hasFiles,
    hasExpandContent:
      hasText ||
      isCommandMode ||
      isMentionMode ||
      input.phase !== "input" ||
      hasFiles,
    canRemember: hasText || hasFiles,
    sendKind,
    barMode: sendKind === "remember" ? "push" : "recall",
  };
}
