"use client";

import { useLocale } from "@/i18n";

// Faithful Claude Code TUI palette — a terminal is dark in both themes.
const FG = "text-[oklch(0.85_0_0)]";
const DIM = "text-[oklch(0.55_0_0)]";
const BRIGHT = "text-[oklch(0.93_0_0)]";
// Completed tool-call dot — Claude Code renders these green.
const TOOL_GREEN = { color: "oklch(0.72 0.17 150)" };

/**
 * Coded recreation of a Claude Code session answering from team memory via
 * MCP. Layout mirrors the real TUI beat-for-beat: dim `>` user message,
 * ⏺ assistant prose, green ⏺ MCP tool call in the real
 * `server - tool (MCP)(args)` format, dim ⎿ collapsed result with the
 * ctrl+r hint, then the answer — and the resting ❯ input box that makes it
 * read as a live session, not a mockup.
 */
export function ClaudeCodeScenario() {
  const { t } = useLocale();

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 font-mono text-[12px] sm:text-[13px] leading-relaxed bg-[oklch(0.18_0_0)]">
      {/* Past user message — dim with a > gutter, like the real transcript */}
      <p className={DIM}>&gt; {t.landing.ccPrompt}</p>

      {/* Assistant intro line */}
      <div className="mt-4 flex gap-2">
        <span aria-hidden className={`${FG} shrink-0`}>
          {"⏺"}
        </span>
        <p className={FG}>{t.landing.ccIntro}</p>
      </div>

      {/* MCP tool call — green completed dot + collapsed ⎿ result */}
      <p className="mt-3">
        <span aria-hidden style={TOOL_GREEN}>
          {"⏺"}
        </span>{" "}
        <span className={BRIGHT}>{t.landing.ccToolCall}</span>
      </p>
      <p className={`pl-5 ${DIM}`}>
        {"⎿"}
        {"  "}
        {t.landing.ccToolResult}
      </p>

      {/* Answer */}
      <div className="mt-3 flex gap-2">
        <span aria-hidden className={`${FG} shrink-0`}>
          {"⏺"}
        </span>
        <p className={FG}>{t.landing.ccAnswer}</p>
      </div>

      {/* Resting input box */}
      <div
        aria-hidden
        className="mt-5 rounded-md border border-[oklch(0.35_0_0)] px-3 py-2 flex items-center gap-2"
      >
        <span className={BRIGHT}>{"❯"}</span>
        <span className="w-[7px] h-[15px] bg-[oklch(0.85_0_0)]" />
      </div>
    </div>
  );
}
