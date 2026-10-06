// @memaxlabs/ledger: the Memax V2 component library.
// Styles: import "@memaxlabs/ledger-tokens" then "@memaxlabs/ledger/ledger.css".

// i18n and context
export { LedgerProvider, useLedger, ledgerStrings } from "./i18n/provider";
export type {
  LedgerContextValue,
  LedgerLinkComponent,
  LedgerLinkProps,
  LedgerLocale,
  LedgerProviderProps,
  LedgerStringOverrides,
} from "./i18n/provider";
export { en } from "./i18n/en";
export type { LedgerStrings } from "./i18n/en";
export { zh } from "./i18n/zh";
export { format, formatNodes, plural } from "./lib/format";
export type { Plural } from "./lib/format";

// Shared data and types
export { AGENTS, monogramFor, resolveAgent } from "./lib/agents";
export type {
  AgentInfo,
  AgentKind,
  AgentQuery,
  AgentSurface,
  KnownAgent,
  ResolvedAgent,
} from "./lib/agents";
export type {
  Autonomy,
  DreamItemKind,
  HandoffStatus,
  MarkState,
  MemoryState,
  NavPlace,
  StatementState,
  SyncStatus,
  TerminalLineKind,
} from "./lib/types";
export { toAriaKeyshortcuts } from "./lib/keyshortcuts";

// Brand
export { Icon, ICON_NAMES } from "./brand/icon";
export type { IconName, IconProps } from "./brand/icon";
export { Logo } from "./brand/logo";
export type { LogoProps } from "./brand/logo";
export { Seal } from "./brand/seal";
export type { SealProps } from "./brand/seal";

// Primitives
export { Button } from "./primitives/button";
export type {
  ButtonProps,
  ButtonSize,
  ButtonVariant,
} from "./primitives/button";
export { Kbd } from "./primitives/kbd";
export type { KbdProps } from "./primitives/kbd";
export { Field } from "./primitives/field";
export type { FieldProps } from "./primitives/field";
export { Segmented } from "./primitives/segmented";
export type { SegmentedOption, SegmentedProps } from "./primitives/segmented";

// Provenance
export { AgentStamp } from "./provenance/agent-stamp";
export type { AgentStampProps } from "./provenance/agent-stamp";
export { Receipt } from "./provenance/receipt";
export type { ReceiptProps } from "./provenance/receipt";
export { Cite } from "./provenance/cite";
export type { CiteProps } from "./provenance/cite";
export { Highlight } from "./provenance/highlight";
export type { HighlightProps } from "./provenance/highlight";

// Memory
export { StateMark } from "./memory/state-mark";
export type { StateMarkProps } from "./memory/state-mark";
export { MemoryRow } from "./memory/memory-row";
export type { MemoryRowProps } from "./memory/memory-row";
export { MemoryList } from "./memory/memory-list";
export type { MemoryListProps } from "./memory/memory-list";
export { MemoryText } from "./memory/memory-text";
export type { MemoryTextProps } from "./memory/memory-text";
export { Redaction } from "./memory/redaction";
export type { RedactionProps } from "./memory/redaction";
export { Diff, wordDiff } from "./memory/diff";
export type { DiffPart, DiffProps } from "./memory/diff";
export { Lineage } from "./memory/lineage";
export type { LineageEvent, LineageProps } from "./memory/lineage";
export { ReviewCard } from "./memory/review-card";
export type { ReviewCardProps } from "./memory/review-card";
export { DreamCard } from "./memory/dream-card";
export type { DreamCardProps, DreamItem } from "./memory/dream-card";

// Agents
export { HandoffSlip } from "./agents/handoff-slip";
export type { HandoffSlipProps } from "./agents/handoff-slip";
export { AgentRow, AgentListHead } from "./agents/agent-row";
export type { AgentListHeadProps, AgentRowProps } from "./agents/agent-row";
export { SyncTarget } from "./agents/sync-target";
export type { SyncTargetProps } from "./agents/sync-target";
export { DecisionGate } from "./agents/decision-gate";
export type { DecisionGateProps, DecisionOption } from "./agents/decision-gate";

// Frame
export { CommandBar } from "./frame/command-bar";
export type { CommandBarProps, CommandMode } from "./frame/command-bar";
export { CommandDialog } from "./frame/command-dialog";
export type { CommandDialogProps } from "./frame/command-dialog";
export { NavRail } from "./frame/nav-rail";
export type { NavItem, NavRailProps, NavRailStatus } from "./frame/nav-rail";
export { Shell } from "./frame/shell";
export type { ShellProps } from "./frame/shell";
export { PageHeader } from "./frame/page-header";
export type { PageHeaderProps } from "./frame/page-header";
export { Terminal } from "./frame/terminal";
export type { TerminalLine, TerminalProps } from "./frame/terminal";
