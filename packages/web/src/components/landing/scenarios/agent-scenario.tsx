"use client";

import { Sparkles } from "lucide-react";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { useLocale } from "@/i18n";

/**
 * Coded recreation of a third-party MCP agent (OpenClaw) using memax —
 * chat UI: user request, agent's memax tool call as a quiet mono chip,
 * then the reply grounded in team memory. Proves the agent-agnostic claim:
 * any MCP agent taps the same brain, no memax-specific integration.
 */
export function AgentScenario() {
  const { t } = useLocale();

  const agent = AGENT_IDENTITIES["openclaw"];
  const Icon = agent?.icon ?? Sparkles;
  const color = agent?.color ?? "oklch(0.65 0.12 220)";

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 space-y-4">
      {/* User message — right-aligned bubble, standard chat pattern */}
      <div className="flex justify-end">
        <p className="max-w-[85%] rounded-2xl bg-surface-2 px-3.5 py-2 text-sm sm:text-[15px] text-fg-1">
          {t.landing.agentUserMsg}
        </p>
      </div>

      {/* Agent reply */}
      <div className="flex items-start gap-3">
        <div
          className="w-7 h-7 sm:w-8 sm:h-8 rounded-chrome flex items-center justify-center shrink-0 mt-0.5"
          style={{ backgroundColor: `oklch(from ${color} l c h / 0.12)` }}
        >
          <Icon
            className="w-3.5 h-3.5 sm:w-4 sm:h-4"
            style={{ color }}
            strokeWidth={1.8}
          />
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-[13px] sm:text-sm font-medium text-fg-1">
            {agent?.displayName ?? "OpenClaw"}
          </p>

          {/* memax MCP tool call — quiet mono chip, signature marker on the
              memax-AI moment */}
          <p className="mt-1.5 inline-flex flex-wrap items-center gap-x-1.5 gap-y-0.5 rounded-chrome bg-surface-1 border border-border/40 px-2.5 py-1 font-mono text-[11px] sm:text-[12px] text-fg-3">
            <span aria-hidden style={{ color: "var(--signature)" }}>
              {"⏺"}
            </span>
            <span>{t.landing.agentToolCall}</span>
            <span className="text-fg-4">· {t.landing.agentToolResult}</span>
          </p>

          <p className="mt-2 text-sm sm:text-[15px] text-fg-2 leading-relaxed">
            {t.landing.agentAnswer}
          </p>
        </div>
      </div>
    </div>
  );
}
