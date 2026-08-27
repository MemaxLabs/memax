"use client";

/**
 * Demo kit (G1) — the composable primitives every landing demo is
 * assembled from. Doctrine (demo realism, founder call): recreate
 * REAL tool output from source, never invent; TUI/CLI chrome stays
 * English in every locale (real tools aren't localized) while user
 * content localizes; GIFs were rejected outright — per-locale
 * re-recording, no dark theme, retina blur, and product drift ruled
 * them out. Code recreations stay crisp, bilingual, theme-aware and
 * update with the product.
 */

import type { ReactNode } from "react";

/* ── Window frame — real macOS traffic lights (the authentic colors
      are what make the frame read as a capture, not a mockup). ── */
export function DemoWindow({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="w-full rounded-surface overflow-hidden border border-[oklch(from_var(--foreground)_l_c_h/0.08)] bg-[oklch(from_var(--foreground)_l_c_h/0.02)]">
      <div className="flex items-center gap-2 px-4 py-2.5 border-b border-[oklch(from_var(--foreground)_l_c_h/0.06)]">
        <span aria-hidden className="flex items-center gap-1.5 opacity-90">
          <span className="w-2.5 h-2.5 rounded-full bg-[#ff5f57]" />
          <span className="w-2.5 h-2.5 rounded-full bg-[#febc2e]" />
          <span className="w-2.5 h-2.5 rounded-full bg-[#28c840]" />
        </span>
        <span className="flex-1 text-center font-mono text-[11px] text-fg-4 truncate pr-10">
          {title}
        </span>
      </div>
      {children}
    </div>
  );
}

/* ── Claude Code TUI beats — faithful palette; a terminal is dark in
      both themes. Mirrors the real transcript: dim `>` user line,
      ⏺ prose, green ⏺ tool call, dim ⎿ collapsed result. ── */
const FG = "text-[oklch(0.85_0_0)]";
const DIM = "text-[oklch(0.55_0_0)]";
const BRIGHT = "text-[oklch(0.93_0_0)]";
const TOOL_GREEN = { color: "oklch(0.72 0.17 150)" };

export function TuiPane({ children }: { children: ReactNode }) {
  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 font-mono text-[12px] sm:text-[13px] leading-relaxed bg-[oklch(0.18_0_0)]">
      {children}
    </div>
  );
}

export function TuiUser({ children }: { children: ReactNode }) {
  return <p className={DIM}>&gt; {children}</p>;
}

export function TuiAssistant({ children }: { children: ReactNode }) {
  return (
    <div className="mt-3 flex gap-2">
      <span aria-hidden className={`${FG} shrink-0`}>
        {"⏺"}
      </span>
      <p className={FG}>{children}</p>
    </div>
  );
}

export function TuiTool({ call, result }: { call: string; result: string }) {
  return (
    <>
      <p className="mt-3">
        <span aria-hidden style={TOOL_GREEN}>
          {"⏺"}
        </span>{" "}
        <span className={BRIGHT}>{call}</span>
      </p>
      <p className={`pl-5 ${DIM}`}>
        {"⎿"}
        {"  "}
        {result}
      </p>
    </>
  );
}

/* ── Chat surface — the claude.ai shape: right-aligned user bubble,
      plain assistant text. Light surface, theme-aware. ── */
export function ChatPane({ children }: { children: ReactNode }) {
  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 text-[13px] leading-relaxed flex flex-col gap-3 bg-[oklch(from_var(--background)_l_c_h/0.5)]">
      {children}
    </div>
  );
}

export function ChatUser({ children }: { children: ReactNode }) {
  return (
    <div className="self-end max-w-[85%] rounded-2xl rounded-br-md bg-surface-2 px-3.5 py-2 text-fg-1">
      {children}
    </div>
  );
}

export function ChatAssistant({ children }: { children: ReactNode }) {
  return <div className="max-w-[92%] text-fg-2">{children}</div>;
}

/** The MCP tool-use chip chat surfaces render for a memax call. */
export function ChatToolChip({ children }: { children: ReactNode }) {
  return (
    <div className="inline-flex items-center gap-1.5 self-start rounded-lg border border-border/50 px-2.5 py-1 font-mono text-[11px] text-fg-3">
      <span
        aria-hidden
        className="h-1.5 w-1.5 rounded-full"
        style={{ background: "oklch(0.72 0.17 150)" }}
      />
      {children}
    </div>
  );
}
