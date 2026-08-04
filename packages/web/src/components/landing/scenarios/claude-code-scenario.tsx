"use client";

import { useLocale } from "@/i18n";

/**
 * Coded recreation of a Claude Code session where the agent answers a
 * question by calling memax over MCP — prompt line, ⏺ tool call with ⎿
 * result, then the synthesized answer. The terminal is intentionally dark
 * in both themes (it's a terminal); memax's tool-call marker uses the
 * signature color because that's the memax-AI moment.
 */
export function ClaudeCodeScenario() {
  const { t } = useLocale();

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 font-mono text-[12px] sm:text-[13px] leading-relaxed bg-[oklch(0.18_0_0)]">
      {/* User prompt */}
      <p>
        <span className="text-[oklch(0.55_0_0)]">&gt; </span>
        <span className="text-[oklch(0.93_0_0)]">{t.landing.ccPrompt}</span>
      </p>

      {/* memax MCP tool call + collapsed result */}
      <p className="mt-4">
        <span style={{ color: "var(--signature)" }}>{"⏺"}</span>{" "}
        <span className="text-[oklch(0.85_0_0)]">{t.landing.ccToolCall}</span>
      </p>
      <p className="pl-5 text-[oklch(0.55_0_0)]">
        {"⎿"} {t.landing.ccToolResult}
      </p>

      {/* Assistant answer */}
      <div className="mt-4 flex gap-2">
        <span aria-hidden className="text-[oklch(0.85_0_0)] shrink-0">
          {"⏺"}
        </span>
        <p className="text-[oklch(0.85_0_0)]">{t.landing.ccAnswer}</p>
      </div>
    </div>
  );
}
