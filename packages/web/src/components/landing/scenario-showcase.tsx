"use client";

import { useState } from "react";
import { Globe, Terminal } from "lucide-react";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { AGENT_BRAND_MARKS } from "@memaxlabs/ui/tokens/agent-brand-marks";
import { useLocale } from "@/i18n";
import { ClaudeCodeScenario } from "./scenarios/claude-code-scenario";
import { CliScenario } from "./scenarios/cli-scenario";
import { WebScenario } from "./scenarios/web-scenario";
import { AgentScenario } from "./scenarios/agent-scenario";

type ScenarioKey = "claude-code" | "cli" | "web" | "agent";

const FOCUS_RING =
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

/**
 * Scenario showcase — four coded recreations of real memax surfaces, in one
 * framed window with a tab row (Stripe-style surface switcher):
 *
 *   Claude Code — agent recalls team memory mid-session via MCP
 *   Terminal    — memax push / recall from any shell
 *   Web         — the memory feed + ask experience at memax.app
 *   OpenClaw    — any third-party MCP agent taps the same brain
 *
 * Coded recreations (not raster screenshots) so the demos follow the
 * light/dark theme and the active locale. Tab switch is an instant content
 * swap per the design rules — no crossfade.
 */
export function ScenarioShowcase() {
  const { t } = useLocale();
  const [active, setActive] = useState<ScenarioKey>("claude-code");

  const ClaudeMark = AGENT_BRAND_MARKS["claude-code"];
  const OpenClawMark = AGENT_BRAND_MARKS["openclaw"];

  const tabs: {
    key: ScenarioKey;
    label: string;
    icon: React.ReactNode;
  }[] = [
    {
      key: "claude-code",
      label: AGENT_IDENTITIES["claude-code"]?.displayName ?? "Claude Code",
      icon: ClaudeMark ? (
        <ClaudeMark className="w-3.5 h-3.5" aria-hidden />
      ) : null,
    },
    {
      key: "cli",
      label: t.landing.scenarioTabCli,
      icon: <Terminal className="w-3.5 h-3.5" aria-hidden strokeWidth={1.8} />,
    },
    {
      key: "web",
      label: t.landing.scenarioTabWeb,
      icon: <Globe className="w-3.5 h-3.5" aria-hidden strokeWidth={1.8} />,
    },
    {
      key: "agent",
      label: AGENT_IDENTITIES["openclaw"]?.displayName ?? "OpenClaw",
      icon: OpenClawMark ? (
        <OpenClawMark className="w-3.5 h-3.5" aria-hidden />
      ) : null,
    },
  ];

  const windowTitle: Record<ScenarioKey, string> = {
    "claude-code": t.landing.ccWindowTitle,
    cli: t.landing.cliWindowTitle,
    web: t.landing.webWindowTitle,
    agent: t.landing.agentWindowTitle,
  };

  const caption: Record<ScenarioKey, string> = {
    "claude-code": t.landing.ccCaption,
    cli: t.landing.cliCaption,
    web: t.landing.webCaption,
    agent: t.landing.agentCaption,
  };

  return (
    <div className="w-full flex flex-col items-center gap-4">
      {/* Section label — same typographic register as the overview strip */}
      <p className="text-fg-4 text-[11px] tracking-[0.08em] uppercase font-medium">
        {t.landing.scenarioLabel}
      </p>

      {/* Surface tabs */}
      <div
        role="tablist"
        className="flex flex-wrap items-center justify-center gap-1.5"
      >
        {tabs.map((tab) => {
          const selected = tab.key === active;
          return (
            <button
              key={tab.key}
              role="tab"
              aria-selected={selected}
              onClick={() => setActive(tab.key)}
              className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-[13px] border transition-colors cursor-pointer ${FOCUS_RING} ${
                selected
                  ? "bg-surface-2 border-border text-fg-1"
                  : "bg-transparent border-transparent text-fg-3 hover:text-fg-1"
              }`}
            >
              {tab.icon}
              {tab.label}
            </button>
          );
        })}
      </div>

      {/* Window frame — traffic dots + mono title, content swaps inside the
          same persistent container (no dual-container flash). */}
      <div className="w-full rounded-surface overflow-hidden border border-[oklch(from_var(--foreground)_l_c_h/0.08)] bg-[oklch(from_var(--foreground)_l_c_h/0.02)]">
        <div className="flex items-center gap-2 px-4 py-2.5 border-b border-[oklch(from_var(--foreground)_l_c_h/0.06)]">
          {/* Real macOS traffic lights — the authentic colors are what make
              the frame read as a genuine window capture, not a mockup. */}
          <span aria-hidden className="flex items-center gap-1.5 opacity-90">
            <span className="w-2.5 h-2.5 rounded-full bg-[#ff5f57]" />
            <span className="w-2.5 h-2.5 rounded-full bg-[#febc2e]" />
            <span className="w-2.5 h-2.5 rounded-full bg-[#28c840]" />
          </span>
          <span className="flex-1 text-center font-mono text-[11px] text-fg-4 truncate pr-10">
            {windowTitle[active]}
          </span>
        </div>
        {active === "claude-code" && <ClaudeCodeScenario />}
        {active === "cli" && <CliScenario />}
        {active === "web" && <WebScenario />}
        {active === "agent" && <AgentScenario />}
      </div>

      <p className="text-[13px] text-fg-4 text-center">{caption[active]}</p>
    </div>
  );
}
