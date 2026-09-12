"use client";

/**
 * UseCaseShowcase (G2) — the four-act use-case section, every act
 * assembled from the demo kit. Act order is an argument:
 *
 *   1. 跨 agent 记忆闭环 — save in Claude Code, recall in claude.ai:
 *      the shot no platform's walled-garden memory can perform.
 *   2. 落盘 + 交接 — agent work products land in the team hub with a
 *      named handoff.
 *   3. 团队共享脑 — a teammate asks the hub directly; shared context
 *      resumes.
 *   4. 跨设备续作 — talk an idea through on the phone, implement from
 *      another machine's coding agent. Store→ask was act 1; this is
 *      talk→build.
 *
 * TUI/CLI chrome stays English (real tools aren't localized); user
 * content goes through i18n. Entrance animation is the section-level
 * animate-fade-up rhythm; reduced-motion falls back via the global
 * CSS rules.
 */

import { useState } from "react";
import { useLocale } from "@/i18n";
import {
  DemoWindow,
  TuiPane,
  TuiUser,
  TuiAssistant,
  TuiTool,
  ChatPane,
  ChatUser,
  ChatAssistant,
  ChatToolChip,
} from "./demo-kit";

const FOCUS_RING =
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

type ActKey = "crossAgent" | "handoff" | "teamBrain" | "crossDevice";

const ACTS: ActKey[] = ["crossAgent", "handoff", "teamBrain", "crossDevice"];

export function UseCaseShowcase() {
  const { t } = useLocale();
  const [active, setActive] = useState<ActKey>("crossAgent");
  const u = t.landing.usecases;

  const tabLabel: Record<ActKey, string> = {
    crossAgent: u.tabCrossAgent,
    handoff: u.tabHandoff,
    teamBrain: u.tabTeamBrain,
    crossDevice: u.tabCrossDevice,
  };
  const caption: Record<ActKey, string> = {
    crossAgent: u.captionCrossAgent,
    handoff: u.captionHandoff,
    teamBrain: u.captionTeamBrain,
    crossDevice: u.captionCrossDevice,
  };

  return (
    <div className="flex flex-col items-center gap-5">
      <div
        role="tablist"
        aria-label={u.sectionTitle}
        className="flex flex-wrap justify-center gap-1.5"
      >
        {ACTS.map((key) => {
          const selected = active === key;
          return (
            <button
              key={key}
              role="tab"
              aria-selected={selected}
              onClick={() => setActive(key)}
              className={`inline-flex items-center px-3 py-1.5 rounded-full text-[13px] border transition-colors cursor-pointer ${FOCUS_RING} ${
                selected
                  ? "bg-surface-2 border-border text-fg-1"
                  : "bg-transparent border-transparent text-fg-3 hover:text-fg-1"
              }`}
            >
              {tabLabel[key]}
            </button>
          );
        })}
      </div>

      <div className="w-full flex flex-col gap-3">
        {active === "crossAgent" && <CrossAgentAct />}
        {active === "handoff" && <HandoffAct />}
        {active === "teamBrain" && <TeamBrainAct />}
        {active === "crossDevice" && <CrossDeviceAct />}
      </div>

      <p className="text-[13px] text-fg-4 text-center">{caption[active]}</p>
    </div>
  );
}

/* Act 1 — save the ETF strategy in Claude Code, recall it in chat. */
function CrossAgentAct() {
  const { t } = useLocale();
  const u = t.landing.usecases;
  return (
    <>
      <DemoWindow title="Claude Code — ~/finance">
        <TuiPane>
          <TuiUser>{u.a1SavePrompt}</TuiUser>
          <TuiTool
            call='memax - push (MCP)(title: "ETF portfolio strategy")'
            result="Saved · 1 memory (ctrl+r to expand)"
          />
          <TuiAssistant>{u.a1SaveDone}</TuiAssistant>
        </TuiPane>
      </DemoWindow>
      <DemoWindow title="claude.ai">
        <ChatPane>
          <ChatUser>{u.a1AskPrompt}</ChatUser>
          <ChatToolChip>memax_recall</ChatToolChip>
          <ChatAssistant>{u.a1AskAnswer}</ChatAssistant>
        </ChatPane>
      </DemoWindow>
    </>
  );
}

/* Act 2 — gap analysis lands in the team hub, handoff named. */
function HandoffAct() {
  const { t } = useLocale();
  const u = t.landing.usecases;
  return (
    <DemoWindow title="Claude Code — ~/product/api">
      <TuiPane>
        <TuiUser>{u.a2Prompt}</TuiUser>
        <TuiAssistant>{u.a2Working}</TuiAssistant>
        <TuiTool
          call='memax - push (MCP)(title: "API v2 gap analysis + migration plan", hub_id: "team")'
          result="Saved to team hub · cited 14 memories (ctrl+r to expand)"
        />
        <TuiAssistant>{u.a2Done}</TuiAssistant>
      </TuiPane>
    </DemoWindow>
  );
}

/* Act 3 — a teammate asks the hub directly. */
function TeamBrainAct() {
  const { t } = useLocale();
  const u = t.landing.usecases;
  return (
    <DemoWindow title="memax.app — Ask">
      <ChatPane>
        <ChatUser>{u.a3Question}</ChatUser>
        <ChatToolChip>memax_recall · 3 sources</ChatToolChip>
        <ChatAssistant>{u.a3Answer}</ChatAssistant>
      </ChatPane>
    </DemoWindow>
  );
}

/* Act 4 — idea on the phone, implementation on another machine. */
function CrossDeviceAct() {
  const { t } = useLocale();
  const u = t.landing.usecases;
  return (
    <>
      <DemoWindow title="Claude — iPhone">
        <ChatPane>
          <ChatUser>{u.a4IdeaPrompt}</ChatUser>
          <ChatAssistant>{u.a4IdeaReply}</ChatAssistant>
          <ChatToolChip>memax_push · personal hub</ChatToolChip>
        </ChatPane>
      </DemoWindow>
      {/* Claude Code, not codex: the kit's TUI beats are faithful to
          Claude Code specifically (⏺/⎿/ctrl+r are ITS chrome) —
          labeling them codex would be faithful chrome on the wrong
          tool (review finding 1). Cross-device is the point; the
          second machine runs Claude Code. */}
      <DemoWindow title="Claude Code — workstation">
        <TuiPane>
          <TuiUser>{u.a4ResumePrompt}</TuiUser>
          <TuiTool
            call='memax - recall (MCP)(query: "onboarding widget idea")'
            result="Found 3 memories (ctrl+r to expand)"
          />
          <TuiAssistant>{u.a4ResumeReply}</TuiAssistant>
        </TuiPane>
      </DemoWindow>
    </>
  );
}
