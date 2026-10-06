// @memaxlabs/ledger/previews: demo compositions of every component, for the
// workbench gallery and the screenshot tests. Never import this from product code.
// Styles: ledger-tokens, then @memaxlabs/ledger/ledger.css, then
// @memaxlabs/ledger/previews.css.
import type { ComponentType } from "react";
import {
  AgentRowPreview,
  DecisionGatePreview,
  HandoffSlipPreview,
  SyncTargetPreview,
} from "./agents";
import { IconPreview, LogoPreview, SealPreview } from "./brand";
import type { PreviewProps } from "./frame";
import {
  DiffPreview,
  DreamCardPreview,
  LineagePreview,
  MemoryRowPreview,
  RedactionPreview,
  ReviewCardPreview,
  StateMarkPreview,
} from "./memory";
import {
  ButtonPreview,
  FieldPreview,
  KbdPreview,
  SegmentedPreview,
} from "./primitives";
import {
  AgentStampPreview,
  CitePreview,
  HighlightPreview,
  ReceiptPreview,
} from "./provenance";
import {
  CommandBarPreview,
  NavRailPreview,
  PageHeaderPreview,
  ShellPreview,
  TerminalPreview,
} from "./surfaces";

export { PreviewFrame } from "./frame";
export type { PreviewFrameProps, PreviewProps, PreviewTheme } from "./frame";
export { demoNav } from "./surfaces";
export {
  AgentRowPreview,
  AgentStampPreview,
  ButtonPreview,
  CitePreview,
  CommandBarPreview,
  DecisionGatePreview,
  DiffPreview,
  DreamCardPreview,
  FieldPreview,
  HandoffSlipPreview,
  HighlightPreview,
  IconPreview,
  KbdPreview,
  LineagePreview,
  LogoPreview,
  MemoryRowPreview,
  NavRailPreview,
  PageHeaderPreview,
  ReceiptPreview,
  RedactionPreview,
  ReviewCardPreview,
  SealPreview,
  SegmentedPreview,
  ShellPreview,
  StateMarkPreview,
  SyncTargetPreview,
  TerminalPreview,
};

export type PreviewGroup =
  | "Brand"
  | "Actions"
  | "Provenance"
  | "Memory"
  | "Agents"
  | "Surfaces";

export interface PreviewEntry {
  /** The component's name, which is also the handoff PNG's name: `<name>-light.png`, `<name>-dark.png`. */
  name: string;
  group: PreviewGroup;
  /** The artboard the PNG was rendered at, in CSS pixels. The PNGs are 2× (deviceScaleFactor 2). */
  width: number;
  height: number;
  /** Whether it responds to input (Keep, Undo, autonomy). */
  interactive?: boolean;
  Component: ComponentType<PreviewProps>;
}

/** Every component preview, in the handoff's order within each group. */
export const PREVIEWS: readonly PreviewEntry[] = [
  {
    name: "Logo",
    group: "Brand",
    width: 960,
    height: 232,
    Component: LogoPreview,
  },
  {
    name: "Seal",
    group: "Brand",
    width: 960,
    height: 184,
    Component: SealPreview,
  },
  {
    name: "Icon",
    group: "Brand",
    width: 960,
    height: 324,
    Component: IconPreview,
  },
  {
    name: "Button",
    group: "Actions",
    width: 960,
    height: 132,
    Component: ButtonPreview,
  },
  {
    name: "Kbd",
    group: "Actions",
    width: 960,
    height: 68,
    Component: KbdPreview,
  },
  {
    name: "Field",
    group: "Actions",
    width: 960,
    height: 126,
    Component: FieldPreview,
  },
  {
    name: "Segmented",
    group: "Actions",
    width: 960,
    height: 80,
    Component: SegmentedPreview,
  },
  {
    name: "AgentStamp",
    group: "Provenance",
    width: 960,
    height: 102,
    Component: AgentStampPreview,
  },
  {
    name: "Receipt",
    group: "Provenance",
    width: 960,
    height: 148,
    Component: ReceiptPreview,
  },
  {
    name: "Cite",
    group: "Provenance",
    width: 960,
    height: 104,
    Component: CitePreview,
  },
  {
    name: "Highlight",
    group: "Provenance",
    width: 960,
    height: 104,
    Component: HighlightPreview,
  },
  {
    name: "StateMark",
    group: "Memory",
    width: 960,
    height: 64,
    Component: StateMarkPreview,
  },
  {
    name: "MemoryRow",
    group: "Memory",
    width: 960,
    height: 406,
    Component: MemoryRowPreview,
  },
  {
    name: "Redaction",
    group: "Memory",
    width: 960,
    height: 192,
    Component: RedactionPreview,
  },
  {
    name: "Diff",
    group: "Memory",
    width: 960,
    height: 78,
    Component: DiffPreview,
  },
  {
    name: "Lineage",
    group: "Memory",
    width: 960,
    height: 288,
    Component: LineagePreview,
  },
  {
    name: "ReviewCard",
    group: "Memory",
    width: 960,
    height: 616,
    interactive: true,
    Component: ReviewCardPreview,
  },
  {
    name: "DreamCard",
    group: "Memory",
    width: 960,
    height: 470,
    Component: DreamCardPreview,
  },
  {
    name: "HandoffSlip",
    group: "Agents",
    width: 960,
    height: 412,
    Component: HandoffSlipPreview,
  },
  {
    name: "AgentRow",
    group: "Agents",
    width: 960,
    height: 242,
    interactive: true,
    Component: AgentRowPreview,
  },
  {
    name: "SyncTarget",
    group: "Agents",
    width: 960,
    height: 270,
    Component: SyncTargetPreview,
  },
  {
    name: "DecisionGate",
    group: "Agents",
    width: 960,
    height: 456,
    Component: DecisionGatePreview,
  },
  {
    name: "CommandBar",
    group: "Surfaces",
    width: 960,
    height: 254,
    Component: CommandBarPreview,
  },
  {
    name: "NavRail",
    group: "Surfaces",
    width: 360,
    height: 640,
    Component: NavRailPreview,
  },
  {
    name: "Shell",
    group: "Surfaces",
    width: 1100,
    height: 420,
    Component: ShellPreview,
  },
  {
    name: "PageHeader",
    group: "Surfaces",
    width: 960,
    height: 202,
    Component: PageHeaderPreview,
  },
  {
    name: "Terminal",
    group: "Surfaces",
    width: 960,
    height: 244,
    Component: TerminalPreview,
  },
];
