import { useState } from "react";
import { AgentListHead, AgentRow } from "../agents/agent-row";
import { DecisionGate } from "../agents/decision-gate";
import { HandoffSlip } from "../agents/handoff-slip";
import { SyncTarget } from "../agents/sync-target";
import type { Autonomy } from "../lib/types";
import { Button } from "../primitives/button";
import { PreviewFrame, type PreviewProps } from "./frame";

export function HandoffSlipPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="handoff-slip" {...props}>
      <div className="mx-stage">
        <div className="pv-measure">
          <HandoffSlip
            id="H-0093"
            from="claude-code"
            to="codex"
            toSurface="cloud task"
            status="sent"
            time="sent 14:20"
            carries="7 memories"
            title="Finish the MCP 2026-07-28 migration"
            done={[
              "Dropped the initialize handshake on the remote server.",
              "Local stdio server passes the parity check.",
            ]}
            next={[
              "Return input_required from memax_push when a write needs confirmation.",
            ]}
            questions={[
              "Keep the ChatGPT tool names, or align them with the core set?",
              "Which deploy target for v2?",
            ]}
            actions={
              <Button size="sm" variant="secondary" icon="copy">
                Copy as prompt
              </Button>
            }
          />
        </div>
      </div>
    </PreviewFrame>
  );
}

/** Interactive: each row's autonomy can be changed. */
export function AgentRowPreview(props: PreviewProps) {
  const [autonomy, setAutonomy] = useState<Record<string, Autonomy>>({
    "claude-code": "write",
    codex: "propose",
    gemini: "read",
  });
  const set = (agent: string) => (next: Autonomy) =>
    setAutonomy((current) => ({ ...current, [agent]: next }));
  return (
    <PreviewFrame name="agent-row" {...props}>
      <div className="mx-stage">
        <div className="mx-panel">
          <AgentListHead />
          <AgentRow
            agent="claude-code"
            autonomy={autonomy["claude-code"] ?? "write"}
            onAutonomyChange={set("claude-code")}
            reads={1204}
            writes={38}
            lastSeen="2 min ago"
            target="CLAUDE.md"
          />
          <AgentRow
            agent="codex"
            autonomy={autonomy.codex ?? "propose"}
            onAutonomyChange={set("codex")}
            reads={512}
            writes={21}
            lastSeen="14 min ago"
            target="AGENTS.md"
          />
          <AgentRow
            agent="gemini"
            autonomy={autonomy.gemini ?? "read"}
            onAutonomyChange={set("gemini")}
            reads={0}
            writes={0}
            paused
          />
        </div>
      </div>
    </PreviewFrame>
  );
}

export function SyncTargetPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="sync-target" {...props}>
      <div className="mx-stage">
        <div className="mx-panel">
          <SyncTarget
            path="CLAUDE.md"
            tool="Claude Code"
            status="synced"
            detail="214 memories · 3.1k tokens"
            time="03:14"
          />
          <SyncTarget
            path=".cursor/rules/memax.mdc"
            tool="Cursor"
            status="drifted"
            detail="1 local edit on Oct 4"
            time="Oct 4"
            action={
              <Button size="sm" variant="secondary">
                Pull edit
              </Button>
            }
          />
          <SyncTarget
            path="AGENTS.md"
            tool="Codex · OpenCode"
            status="pending"
            detail="Recompiling after M-0430"
            time="now"
          />
          <SyncTarget
            path="GEMINI.md"
            tool="Gemini CLI"
            status="off"
            detail="Agent paused"
          />
        </div>
      </div>
    </PreviewFrame>
  );
}

export function DecisionGatePreview(props: PreviewProps) {
  return (
    <PreviewFrame name="decision-gate" {...props}>
      <div className="mx-stage">
        <div className="pv-measure">
          <DecisionGate
            agent="codex"
            time="2 min ago"
            space="memax-v2"
            defaultSelected={0}
            question="Which deploy target should the v2 API use?"
            context="A kept note and a proposal disagree: M-0174 (Railway, kept by Jiahao) and M-0431 (Fly.io, proposed by Codex)."
            options={[
              {
                label: "Fly.io, iad and ams",
                detail: "Matches the current API and workers.",
              },
              { label: "Railway", detail: "Simpler preview environments." },
              {
                label: "Decide later",
                detail: "Codex continues behind a flag.",
              },
            ]}
          />
        </div>
      </div>
    </PreviewFrame>
  );
}
