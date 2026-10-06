import { CommandBar } from "../frame/command-bar";
import type { NavRailProps } from "../frame/nav-rail";
import { NavRail } from "../frame/nav-rail";
import { PageHeader } from "../frame/page-header";
import { Shell } from "../frame/shell";
import { Terminal } from "../frame/terminal";
import { MemoryList } from "../memory/memory-list";
import { MemoryRow } from "../memory/memory-row";
import { Button } from "../primitives/button";
import { Cite } from "../provenance/cite";
import { Highlight } from "../provenance/highlight";
import { PreviewFrame, type PreviewProps } from "./frame";

const noop = () => {};

/** The demo rail: memax-v2, five proposals in Review, one handoff, five agents in sync. */
export function demoNav(active: string): NavRailProps {
  return {
    active,
    space: "memax-v2",
    spaceKind: "Project",
    onSpaceClick: noop,
    onAskClick: noop,
    person: "ZZ",
    settingsHref: "#settings",
    status: { state: "kept", label: "5 agents in sync" },
    items: [
      { id: "today", href: "#today" },
      { id: "review", href: "#review", count: 5, tone: "pending" },
      { id: "briefs", href: "#briefs" },
      { id: "memories", href: "#memories" },
      { id: "handoffs", href: "#handoffs", count: 1 },
      { id: "agents", href: "#agents" },
    ],
  };
}

export function CommandBarPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="command-bar" {...props}>
      <div className="mx-stage pv-center">
        <CommandBar defaultQuery="Why did we pick River over Temporal?">
          <p className="mx-section-label">Answer from memax-v2</p>
          <p className="mx-answer">
            <Highlight>River runs on the Postgres we already operate</Highlight>
            , so a job commits in the same transaction as the rows it touches.
            <Cite n={1} title="M-0219" href="#M-0219" /> Temporal was tried in
            August and dropped.
            <Cite n={2} title="M-0144" href="#M-0144" />
          </p>
        </CommandBar>
      </div>
    </PreviewFrame>
  );
}

export function NavRailPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="nav-rail" {...props}>
      <div className="pv-rail">
        <NavRail {...demoNav("review")} />
      </div>
    </PreviewFrame>
  );
}

export function ShellPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="shell" {...props}>
      <Shell nav={demoNav("memories")}>
        <div className="mx-page">
          <PageHeader title="Memories" lede="214 kept in memax-v2." />
          <div className="mx-panel">
            <MemoryList>
              <MemoryRow
                person="ZZ"
                name="You"
                action="kept"
                time="Oct 2"
                id="M-0219"
              >
                Background jobs run on River, not Temporal.
              </MemoryRow>
            </MemoryList>
          </div>
        </div>
      </Shell>
    </PreviewFrame>
  );
}

export function PageHeaderPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="page-header" {...props}>
      <div className="mx-stage">
        <PageHeader
          eyebrow="memax-v2 · Project space"
          title="Monday, October 5"
          lede="Overnight, Dream folded 34 notes into 6 facts. Four proposals are waiting on you."
          actions={
            <>
              <Button variant="secondary" icon="handoff">
                Hand off
              </Button>
              <Button variant="primary" kbd="R">
                Start review
              </Button>
            </>
          }
        />
      </div>
    </PreviewFrame>
  );
}

export function TerminalPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="terminal" {...props}>
      <div className="mx-stage">
        <div className="pv-measure">
          <Terminal
            title="memax"
            lines={[
              { kind: "cmd", text: 'memax recall "deploy target"' },
              {
                kind: "kept",
                text: "M-0174  Deploy the v2 API to Railway.                kept · JY",
              },
              {
                kind: "proposed",
                text: "M-0431  Deploy the v2 API to Fly.io in iad and ams.  proposed · CX",
              },
              {
                kind: "dim",
                text: "2 results · they conflict · memax review M-0431",
              },
              { kind: "cmd", text: "memax forget M-0388" },
              {
                kind: "forgotten",
                text: "Forgotten. Rewrote CLAUDE.md and AGENTS.md; 5 agents told.",
              },
            ]}
          />
        </div>
      </div>
    </PreviewFrame>
  );
}
