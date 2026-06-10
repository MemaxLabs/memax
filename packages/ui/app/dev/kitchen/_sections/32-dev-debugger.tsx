"use client";

import { DemoCard, Section } from "../_shared";

const EVENTS = [
  {
    channel: "recall",
    title: "Recall submitted",
    detail: "find the onboarding notes for Codex",
    context: "AI auto-answer on · now",
    color: "var(--signature)",
  },
  {
    channel: "recall",
    title: "Recall results ready",
    detail: "9 ranked memories across hubs",
    context: "184ms · now",
    color: "var(--signature)",
  },
  {
    channel: "ai",
    title: "AI answer completed",
    detail: "Synthesized answer with citations [1] [2] [3]",
    context: "stream finished · 2s",
    color: "oklch(0.68 0.14 250)",
  },
  {
    channel: "remember",
    title: "Remember saved",
    detail: "Launch checklist for the dev debugger",
    context: "web push · 6s",
    color: "oklch(0.68 0.14 155)",
  },
];

function DebuggerTrayMock() {
  return (
    <div className="flex justify-end">
      <div className="w-full max-w-[390px] overflow-hidden rounded-[24px] border border-border/70 bg-card shadow-[var(--bar-shadow)]">
        <div className="border-b border-border/50 px-4 py-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <span
                  className="text-[13px]"
                  style={{ color: "var(--signature)" }}
                >
                  ✦
                </span>
                <p className="text-[14px] font-medium text-foreground">
                  memax debugger
                </p>
                <span
                  className="rounded-full px-2 py-0.5 text-[10px] font-medium"
                  style={{
                    color: "var(--signature)",
                    background:
                      "color-mix(in oklch, var(--signature) 10%, transparent)",
                  }}
                >
                  Live
                </span>
              </div>
              <p className="mt-1 text-[12px] text-fg-3">
                A floating event timeline for recall, AI, and remember flows.
              </p>
            </div>
            <button className="rounded-lg px-2 py-1 text-[12px] text-fg-3">
              Collapse
            </button>
          </div>
        </div>

        <div className="border-b border-border/50 px-4 py-3">
          <div className="flex flex-wrap gap-2">
            {[
              ["Recall", "9", "var(--signature)"],
              ["AI", "3", "oklch(0.68 0.14 250)"],
              ["Remember", "2", "oklch(0.68 0.14 155)"],
              ["System", "1", "oklch(from var(--foreground) l c h / 0.55)"],
            ].map(([label, count, color]) => (
              <div
                key={String(label)}
                className="flex items-center gap-2 rounded-full px-2.5 py-1 text-[11px] text-fg-2"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.04)",
                }}
              >
                <span
                  className="h-1.5 w-1.5 rounded-full"
                  style={{ background: String(color) }}
                />
                <span>{label}</span>
                <span className="tabular-nums text-fg-3">{count}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="space-y-2 px-3 py-3">
          {EVENTS.map((event) => (
            <div
              key={event.title}
              className="rounded-xl border border-border px-3.5 py-3"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.03)",
              }}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span
                      className="h-1.5 w-1.5 rounded-full"
                      style={{ background: event.color }}
                    />
                    <span className="text-[11px] uppercase tracking-[0.14em] text-fg-4">
                      {event.channel}
                    </span>
                  </div>
                  <p className="mt-1 text-[13px] font-medium text-fg-1">
                    {event.title}
                  </p>
                  <p className="mt-1 text-[12px] text-fg-2">{event.detail}</p>
                  <p className="mt-1 text-[11px] text-fg-4">{event.context}</p>
                </div>
                <span className="text-[11px] text-fg-4">now</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function RulesCard() {
  return (
    <div className="space-y-2 text-[12px] text-fg-2 leading-relaxed">
      <p>Debugger is dev-only and hidden behind Dev Tools.</p>
      <p>
        Recall is the primary trace: submit, fetch, results, panel open, AI
        answer.
      </p>
      <p>Remember flows log both direct text saves and staged file uploads.</p>
      <p>
        Tray stays global so the events remain visible while people interact
        with the bar.
      </p>
    </div>
  );
}

export function DevDebuggerSection() {
  return (
    <Section
      title="32. Dev Debugger"
      description="North star for the live Memax event tray used to inspect recall-heavy flows."
    >
      <DemoCard label="Floating Tray">
        <DebuggerTrayMock />
      </DemoCard>

      <DemoCard label="Behavior Rules">
        <RulesCard />
      </DemoCard>
    </Section>
  );
}
