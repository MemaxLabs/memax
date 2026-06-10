// Maps to: landing-full.tsx, future onboarding flow
// Design proposal: landing page refresh + post-login onboarding
// Kitchen-first: review here before implementing in packages/web
"use client";

import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import {
  Bot,
  User,
  Terminal,
  Globe,
  Copy,
  Check,
  MessageSquare,
  Users,
  Mail,
  Send,
  AlertTriangle,
  ArrowRight,
  ArrowUp,
  Search,
  Sparkles,
  Link2,
  Shield,
  Loader2,
  X,
  Bot as RobotIcon,
  Inbox,
  Boxes,
  UsersRound,
  Moon,
  Lock,
  Minus,
  RotateCcw,
  MessageCircle,
} from "lucide-react";
import { NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import { AGENT_IDENTITIES } from "@/tokens/agents";

/* ═══════════════════════════════════════════════
   PART 1: LANDING PAGE REFRESH
   ═══════════════════════════════════════════════ */

/* ── Headline Options ── */

const HEADLINE_OPTIONS = [
  {
    label: "Option A — Portable memory",
    headline: "Your memory.\nEvery AI.",
    subline: "Stop re-explaining yourself every time you switch tools.",
  },
  {
    label: "Option B — Agent-first",
    headline: "One memory.\nEvery agent.",
    subline:
      "Your context follows you — across Claude, Cursor, ChatGPT, and every AI you use.",
  },
  {
    label: "Option C — Provocative",
    headline: "Your AI forgets you.\nmemax doesn't.",
    subline: "Shared memory for humans and AI agents.",
  },
];

function HeadlineOptionsDemo() {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
      {HEADLINE_OPTIONS.map((opt) => (
        <DemoCard key={opt.label} label={opt.label}>
          <div className="flex flex-col items-center text-center py-6 gap-3">
            <h2
              className="font-semibold text-foreground text-[1.75rem] leading-[1.05] whitespace-pre-line"
              style={{ letterSpacing: "-0.035em" }}
            >
              {opt.headline}
            </h2>
            <p className="text-fg-3 text-[14px] max-w-[260px]">{opt.subline}</p>
          </div>
        </DemoCard>
      ))}
    </div>
  );
}

/* ── Demo Flow: Claude → Ziyang handoff (the killer story) ──
   Based on a real moment: Claude discovered a memax API gap while
   helping Jiahao, self-flagged as unverified, and pushed a structured
   issue to the engineering hub for Ziyang. Zero Jira, zero Slack.
   Shows AI-as-responsible-teammate + source_agent attribution as the
   trust mechanism. See memax memory aa92bcea. */

function HandoffDemoCard() {
  return (
    <div className="w-full max-w-[900px] mx-auto">
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3 md:gap-4">
        {/* Panel 1 — Conversation → gap discovered */}
        <HandoffPanel step={1} label="Conversation">
          <div className="flex items-center gap-2 mb-3">
            <div
              className="w-7 h-7 rounded-lg flex items-center justify-center"
              style={{
                background: `oklch(from ${AGENT_IDENTITIES["claude-ai"]?.color ?? "oklch(0.6 0.15 30)"} l c h / 0.12)`,
              }}
            >
              <Bot
                className="w-3.5 h-3.5"
                style={{
                  color:
                    AGENT_IDENTITIES["claude-ai"]?.color ??
                    "oklch(0.6 0.15 30)",
                }}
                strokeWidth={1.8}
              />
            </div>
            <span className="text-[12px] text-fg-3">claude.ai</span>
          </div>
          <div className="space-y-2">
            <div
              className="rounded-xl px-3 py-2 text-[13px] text-fg-2 leading-relaxed self-end ml-auto max-w-[85%]"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              {/* i18n: t.landing.handoff.userAsk */}
              What&apos;s new in my memax team hub?
            </div>
            <div
              className="rounded-xl px-3 py-2 text-[13px] text-fg-2 leading-relaxed max-w-[90%]"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.03)",
                border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              {/* i18n: t.landing.handoff.claudeReply */}I tried recall but hit
              a gap — the{" "}
              <span className="font-mono text-[11px]">memax_recall</span> API
              lacks <span className="font-mono text-[11px]">hub_id</span> +
              author filters, so I can&apos;t scope this properly. Let me log
              it.
            </div>
          </div>
        </HandoffPanel>

        {/* Panel 2 — AI writes structured report, pushes to hub */}
        <HandoffPanel step={2} label="AI self-flags + pushes">
          <div
            className="rounded-xl overflow-hidden"
            style={{
              border: "1px solid oklch(from var(--foreground) l c h / 0.08)",
              background: "oklch(from var(--foreground) l c h / 0.02)",
            }}
          >
            {/* Source_agent attribution chip */}
            <div
              className="flex items-center gap-2 px-3 py-2 border-b"
              style={{
                borderColor: "oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              <div
                className="w-5 h-5 rounded-md flex items-center justify-center"
                style={{
                  background: `oklch(from ${AGENT_IDENTITIES["claude-ai"]?.color ?? "oklch(0.6 0.15 30)"} l c h / 0.12)`,
                }}
              >
                <Bot
                  className="w-3 h-3"
                  style={{
                    color:
                      AGENT_IDENTITIES["claude-ai"]?.color ??
                      "oklch(0.6 0.15 30)",
                  }}
                  strokeWidth={2}
                />
              </div>
              <span className="text-[11px] text-fg-3">
                source_agent:{" "}
                <span className="font-mono text-fg-2">claude-ai</span>
              </span>
              <div className="flex-1" />
              <span className="text-[10px] text-fg-4">engineering hub</span>
            </div>
            {/* Report body */}
            <div className="px-3 py-2.5">
              <div className="flex items-center gap-1.5 mb-1">
                <AlertTriangle
                  className="w-3 h-3"
                  style={{ color: "oklch(0.65 0.15 60)" }}
                  strokeWidth={2}
                />
                <span
                  className="text-[10px] font-medium uppercase tracking-wide"
                  style={{ color: "oklch(0.65 0.15 60)" }}
                >
                  {/* i18n: t.landing.handoff.needsVerification */}
                  needs-human-verification
                </span>
                <span
                  className="text-[10px] font-medium uppercase tracking-wide px-1.5 py-0.5 rounded"
                  style={{
                    color: "oklch(0.6 0.15 25)",
                    background: "oklch(0.6 0.15 25 / 0.08)",
                  }}
                >
                  p1
                </span>
              </div>
              <p className="text-[13px] text-fg-1 font-medium leading-snug mb-1">
                {/* i18n: t.landing.handoff.reportTitle */}
                API gap: memax_recall missing hub_id + author filters
              </p>
              <p className="text-[12px] text-fg-3 leading-snug">
                {/* i18n: t.landing.handoff.reportBody */}
                Can&apos;t scope recall to a single hub or author. Suggested
                fix: add filters to /v1/recall query params.
              </p>
            </div>
          </div>
        </HandoffPanel>

        {/* Panel 3 — Teammate picks it up, verifies, ships */}
        <HandoffPanel step={3} label="Teammate ships">
          <div className="flex items-center gap-2 mb-3">
            <div
              className="w-7 h-7 rounded-lg flex items-center justify-center"
              style={{
                background: `oklch(from ${AGENT_IDENTITIES["cursor"]?.color ?? "oklch(0.6 0.15 180)"} l c h / 0.12)`,
              }}
            >
              <Bot
                className="w-3.5 h-3.5"
                style={{
                  color:
                    AGENT_IDENTITIES["cursor"]?.color ?? "oklch(0.6 0.15 180)",
                }}
                strokeWidth={1.8}
              />
            </div>
            <div className="flex flex-col">
              <span className="text-[12px] text-fg-2 font-medium">Ziyang</span>
              <span className="text-[10px] text-fg-4">
                next morning · Cursor
              </span>
            </div>
          </div>
          <div
            className="rounded-xl px-3 py-2.5 text-[13px] text-fg-2 leading-relaxed mb-2"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.03)",
              border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
            }}
          >
            {/* i18n: t.landing.handoff.ziyangRecall */}
            Anything flagged in the eng hub I should pick up today?
          </div>
          <div className="flex items-start gap-1.5">
            <span
              className="state-slow-breathe text-[13px] mt-0.5"
              style={{ color: "var(--signature)" }}
            >
              {"\u2726"}
            </span>
            <p className="text-[12px] text-fg-2 leading-relaxed">
              {/* i18n: t.landing.handoff.recallAnswer */}
              Claude pushed an API gap yesterday — recall is missing{" "}
              <span className="font-mono text-[11px]">hub_id</span> + author
              filters. Tagged <span className="font-mono text-[11px]">p1</span>,
              needs verification.{" "}
              <span className="text-fg-4">Open in memax →</span>
            </p>
          </div>
          <div className="flex items-center gap-1.5 mt-3 pt-2 border-t border-fg-4/10">
            <Check
              className="w-3.5 h-3.5"
              style={{ color: "oklch(0.7 0.18 145)" }}
              strokeWidth={2.5}
            />
            <span className="text-[11px] text-fg-3">
              {/* i18n: t.landing.handoff.shipped */}
              Verified · PR #52 merged, 43m later
            </span>
          </div>
        </HandoffPanel>
      </div>

      {/* Tagline */}
      <p
        className="text-center mt-5 text-[15px] text-fg-2 leading-relaxed"
        style={{ letterSpacing: "-0.02em" }}
      >
        {/* i18n: t.landing.handoff.tagline */}
        Your AI found a bug. Your teammate got the fix request.{" "}
        <span className="text-fg-1 font-medium">No one opened Jira.</span>
      </p>
    </div>
  );
}

/* ── LandingFeatureStrip ──
   Quiet 4-block reinforcement strip that sits below the handoff
   demo on the landing page. Covers the four distinct value props
   the demo doesn't make explicit: dump-from-anywhere (surface
   coverage), ask-from-anywhere (the headline made literal),
   self-organizing (Dreams), team memory (hubs + handoff payoff).
   No card chrome, no borders. Pure typography on the bg. 2x2 on
   mobile, 4-col on desktop. Linear/Vercel restraint — reinforces
   the demo without becoming a feature wall. */
function LandingFeatureStrip() {
  const features = [
    {
      icon: ArrowUp,
      title: "Dump from anywhere",
      desc: "Every surface is an entry point. CLI, web, every AI you use.",
    },
    {
      icon: Search,
      title: "Ask from anywhere",
      desc: "Your context follows you. Any agent, instant recall.",
    },
    {
      icon: Sparkles,
      title: "Self-organizing",
      desc: "You dump. Memax organizes.",
    },
    {
      icon: Users,
      title: "Team memory",
      desc: "Your teammate\u2019s AI knows what yours knows.",
    },
  ];
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-6 sm:gap-7 max-w-[900px] mx-auto">
      {features.map((feat) => {
        const Icon = feat.icon;
        return (
          <div key={feat.title} className="flex flex-col items-start">
            <Icon className="w-4 h-4 text-fg-3 mb-2" strokeWidth={1.8} />
            {/* i18n: t.landing.feat{Dump|Ask|Organize|Team}Title */}
            <p className="text-[14px] font-medium text-fg-1 mb-1">
              {feat.title}
            </p>
            {/* i18n: t.landing.feat{Dump|Ask|Organize|Team}Desc */}
            <p className="text-[13px] text-fg-3 leading-snug">{feat.desc}</p>
          </div>
        );
      })}
    </div>
  );
}

function HandoffPanel({
  step,
  label,
  children,
}: {
  step: number;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className="rounded-2xl p-4 sm:p-5 flex flex-col relative"
      style={{
        border: "1px solid oklch(from var(--foreground) l c h / 0.08)",
        background: "oklch(from var(--foreground) l c h / 0.02)",
        minHeight: 220,
      }}
    >
      <div className="flex items-center gap-2 mb-3">
        <div
          className="w-5 h-5 rounded-full flex items-center justify-center text-[11px] font-medium tabular-nums"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.08)",
            color: "var(--fg-2)",
          }}
        >
          {step}
        </div>
        <span className="text-[11px] text-fg-3 uppercase tracking-wider font-medium">
          {label}
        </span>
      </div>
      <div className="flex-1">{children}</div>
    </div>
  );
}

/* ── CTA Options ── */

function CTAOptionsDemo() {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <DemoCard label="Current">
        <div className="flex flex-col items-center gap-3 py-4">
          <button
            className="flex items-center justify-center h-11 px-6 rounded-xl text-[15px] font-medium cursor-default"
            style={{
              background: "var(--foreground)",
              color: "var(--background)",
            }}
          >
            Open memax
          </button>
          <div
            className="flex items-center gap-3 h-11 px-4 rounded-xl font-mono text-[13px] text-fg-3"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.04)",
              border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
            }}
          >
            <span className="whitespace-nowrap">
              npx memax-cli@latest login && npx memax-cli@latest setup --all
            </span>
            <Copy className="w-3.5 h-3.5 text-fg-4 shrink-0" />
          </div>
        </div>
      </DemoCard>
      <DemoCard label="Proposed">
        <div className="flex flex-col items-center gap-3 py-4">
          <button
            className="flex items-center justify-center h-11 px-6 rounded-xl text-[15px] font-medium cursor-default"
            style={{
              background: "var(--foreground)",
              color: "var(--background)",
            }}
          >
            Give your AI a memory
          </button>
          <div
            className="flex items-center gap-3 h-11 px-4 rounded-xl font-mono text-[13px] text-fg-3"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.04)",
              border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
            }}
          >
            <span className="whitespace-nowrap">
              npx memax-cli@latest login && npx memax-cli@latest setup --all
            </span>
            <Copy className="w-3.5 h-3.5 text-fg-4 shrink-0" />
          </div>
        </div>
      </DemoCard>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   PART 2: POST-LOGIN ONBOARDING
   ═══════════════════════════════════════════════ */

/* ── How memax works — 3 panels (Dump / Ask / Discover) ── */

const HOW_IT_WORKS = [
  {
    icon: null,
    title: "Dump",
    description:
      "Throw anything in — text, files, or let your agents capture it automatically. No organizing needed.",
  },
  {
    icon: MessageSquare,
    title: "Ask",
    description:
      "Ask from anywhere — CLI, web, or your AI agent. Get one clear answer, not a list of results.",
  },
  {
    icon: null,
    title: "Discover",
    description:
      "Come back tomorrow. memax organizes your knowledge into topics and surfaces what changed.",
    isDream: true,
  },
];

function HowItWorksDemo() {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-3 gap-4">
        {HOW_IT_WORKS.map((panel) => {
          const Icon = panel.icon;
          return (
            <div
              key={panel.title}
              className="flex flex-col items-center text-center rounded-xl p-5 gap-3"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.02)",
                border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              <div
                className="w-10 h-10 rounded-xl flex items-center justify-center"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.05)",
                }}
              >
                {Icon ? (
                  <Icon className="w-5 h-5 text-fg-2" strokeWidth={1.5} />
                ) : (
                  <span
                    className="text-[20px] leading-none"
                    style={{ color: "var(--signature)" }}
                  >
                    {"\u2726"}
                  </span>
                )}
              </div>
              <h3 className="text-[15px] font-semibold text-fg-1">
                {panel.title}
              </h3>
              <p className="text-[13px] text-fg-3 leading-relaxed">
                {panel.description}
              </p>
            </div>
          );
        })}
      </div>
      <div className="text-center text-[13px] text-fg-4">
        Interfaces: CLI &middot; MCP &middot; Web
      </div>
    </div>
  );
}

/* ── Preference Selector ── */

const PREFERENCES = [
  {
    id: "cli" as const,
    icon: Terminal,
    title: "I live in the terminal",
    subtitle: "CLI-first setup",
  },
  {
    id: "agent" as const,
    icon: Bot,
    title: "I work through AI agents",
    subtitle: "MCP-first setup",
  },
  {
    id: "web" as const,
    icon: Globe,
    title: "I prefer the web app",
    subtitle: "Start right here",
  },
];

type Preference = "cli" | "agent" | "web";

function PreferenceSelectorDemo() {
  const [selected, setSelected] = useState<Preference | null>(null);

  return (
    <div className="space-y-4">
      <p className="text-[13px] text-fg-3 text-center">
        How do you work? (click to select)
      </p>
      <div className="grid grid-cols-3 gap-4">
        {PREFERENCES.map((pref) => {
          const Icon = pref.icon;
          const isSelected = selected === pref.id;
          return (
            <button
              key={pref.id}
              onClick={() => setSelected(pref.id)}
              className="flex flex-col items-center text-center rounded-xl p-5 gap-3 transition-all cursor-pointer"
              style={{
                background: isSelected
                  ? "oklch(from var(--foreground) l c h / 0.06)"
                  : "oklch(from var(--foreground) l c h / 0.02)",
                border: isSelected
                  ? "1.5px solid oklch(from var(--foreground) l c h / 0.15)"
                  : "1px solid oklch(from var(--foreground) l c h / 0.06)",
                boxShadow: isSelected
                  ? "0 2px 12px oklch(from var(--foreground) l c h / 0.04)"
                  : "none",
              }}
            >
              <div
                className="w-10 h-10 rounded-xl flex items-center justify-center"
                style={{
                  background: isSelected
                    ? "oklch(from var(--foreground) l c h / 0.08)"
                    : "oklch(from var(--foreground) l c h / 0.05)",
                }}
              >
                <Icon
                  className="w-5 h-5"
                  style={{
                    color: isSelected ? "var(--foreground)" : "var(--fg-2)",
                  }}
                  strokeWidth={1.5}
                />
              </div>
              <div>
                <h3
                  className="text-[14px] font-medium"
                  style={{
                    color: isSelected ? "var(--foreground)" : "var(--fg-2)",
                  }}
                >
                  {pref.title}
                </h3>
                <p className="text-[12px] text-fg-4 mt-0.5">{pref.subtitle}</p>
              </div>
            </button>
          );
        })}
      </div>
      {selected && (
        <p className="text-[12px] text-fg-4 text-center">
          Selected: <span className="text-fg-2 font-medium">{selected}</span>{" "}
          &mdash; see the corresponding track below
        </p>
      )}
    </div>
  );
}

/* ── CLI Track ── */

function TerminalBlock({
  command,
  output,
  sparkle,
}: {
  command: string;
  output?: string;
  sparkle?: boolean;
}) {
  return (
    <div
      className="rounded-xl overflow-hidden font-mono text-[13px]"
      style={{
        background: "oklch(from var(--foreground) l c h / 0.02)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <div className="px-4 py-3 space-y-1">
        <div className="text-fg-1">
          <span className="text-fg-4">$ </span>
          {command}
        </div>
        {output && (
          <div className="text-fg-3 flex items-center gap-1.5">
            {sparkle && (
              <span
                className="state-slow-breathe"
                style={{ color: "var(--signature)" }}
              >
                {"\u2726"}
              </span>
            )}
            {output}
          </div>
        )}
      </div>
    </div>
  );
}

function StepLabel({ step, label }: { step: number; label: string }) {
  return (
    <div className="flex items-center gap-2 mb-2">
      <div
        className="w-5 h-5 rounded-full flex items-center justify-center text-[11px] font-medium"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.08)",
          color: "var(--fg-2)",
        }}
      >
        {step}
      </div>
      <span className="text-[14px] font-medium text-fg-1">{label}</span>
    </div>
  );
}

function CLITrackDemo() {
  return (
    <div className="space-y-5">
      <div>
        <StepLabel step={1} label="Set up your agents" />
        <TerminalBlock
          command="npx memax-cli@latest login && npx memax-cli@latest setup --all"
          output="Configured: Claude Code, Cursor, Copilot, Windsurf"
        />
        <p className="text-[12px] text-fg-4 mt-1.5 ml-7">
          One command configures all your agents.
        </p>
      </div>
      <div>
        <StepLabel step={2} label="Push your first memory" />
        <TerminalBlock
          command='memax push "I prefer TypeScript, use pnpm, deploy to Vercel"'
          output="Remembered."
        />
        <p className="text-[12px] text-fg-4 mt-1.5 ml-7">
          Push a preference, a fact, a decision your agents should know.
        </p>
      </div>
      <div>
        <StepLabel step={3} label="Now recall it" />
        <TerminalBlock
          command={'memax recall "what\'s my deploy target?"'}
          output="You deploy to Vercel, using pnpm and TypeScript."
          sparkle
        />
        <p className="text-[12px] text-fg-4 mt-1.5 ml-7">
          This works from any agent, any device.
        </p>
      </div>
    </div>
  );
}

/* ── Agent Track ── */

function AgentBubble({
  role,
  text,
  agent,
}: {
  role: "user" | "agent";
  text: string;
  agent?: string;
}) {
  const isAgent = role === "agent";
  return (
    <div className={`flex gap-2.5 ${isAgent ? "" : "flex-row-reverse"}`}>
      <div
        className="w-7 h-7 rounded-lg flex items-center justify-center shrink-0"
        style={{
          background: isAgent
            ? `oklch(from ${AGENT_IDENTITIES["claude-code"].color} l c h / 0.12)`
            : "oklch(from var(--foreground) l c h / 0.06)",
        }}
      >
        {isAgent ? (
          <Bot
            className="w-3.5 h-3.5"
            style={{ color: AGENT_IDENTITIES["claude-code"].color }}
            strokeWidth={1.8}
          />
        ) : (
          <User className="w-3.5 h-3.5 text-fg-3" strokeWidth={1.8} />
        )}
      </div>
      <div
        className="rounded-xl px-3.5 py-2.5 max-w-[320px]"
        style={{
          background: isAgent
            ? "oklch(from var(--foreground) l c h / 0.03)"
            : "oklch(from var(--foreground) l c h / 0.06)",
          border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
        }}
      >
        {agent && <p className="text-[11px] text-fg-4 mb-1">{agent}</p>}
        <p className="text-[13px] text-fg-2 leading-relaxed">{text}</p>
      </div>
    </div>
  );
}

function AgentTrackDemo() {
  return (
    <div className="space-y-5">
      <div>
        <StepLabel step={1} label="Your agent is ready" />
        <div
          className="rounded-xl px-4 py-3 flex items-center gap-3"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <div
            className="w-8 h-8 rounded-lg flex items-center justify-center"
            style={{
              background: `oklch(from ${AGENT_IDENTITIES["claude-code"].color} l c h / 0.12)`,
            }}
          >
            <Bot
              className="w-4 h-4"
              style={{ color: AGENT_IDENTITIES["claude-code"].color }}
              strokeWidth={1.8}
            />
          </div>
          <div className="flex-1">
            <p className="text-[14px] font-medium text-fg-1">Claude Code</p>
            <p className="text-[12px] text-fg-4">Connected via MCP</p>
          </div>
          <Check className="w-4 h-4 text-emerald-500" />
        </div>
      </div>
      <div>
        <StepLabel step={2} label="Tell your agent something" />
        <div className="space-y-2.5 pl-2">
          <AgentBubble
            role="user"
            text="Remember that I prefer dark mode and use vim bindings"
          />
          <AgentBubble
            role="agent"
            agent="Claude Code"
            text="Got it — saved to your memax. All your agents will know this now."
          />
        </div>
      </div>
      <div>
        <StepLabel step={3} label="Now ask your agent" />
        <div className="space-y-2.5 pl-2">
          <AgentBubble role="user" text="What are my editor preferences?" />
          <AgentBubble
            role="agent"
            agent="Claude Code"
            text="You prefer dark mode and use vim bindings."
          />
        </div>
        <p className="text-[12px] text-fg-4 mt-2 ml-7">
          This works from Cursor, Copilot, or any connected agent.
        </p>
      </div>
    </div>
  );
}

/* ── Web Track ── */

function BarMock({
  mode,
  text,
  placeholder,
}: {
  mode: "push" | "recall";
  text?: string;
  placeholder?: string;
}) {
  return (
    <div
      className="rounded-2xl h-12 flex items-center px-4 gap-2"
      style={{
        background: "var(--card, oklch(from var(--foreground) l c h / 0.03))",
        border: `1.5px solid oklch(from var(--foreground) l c h / ${mode === "recall" ? "0.12" : "0.08"})`,
        boxShadow:
          mode === "recall"
            ? "0 2px 12px oklch(from var(--foreground) l c h / 0.04)"
            : "none",
      }}
    >
      {mode === "recall" && (
        <span
          className="state-slow-breathe text-[14px]"
          style={{ color: "var(--signature)" }}
        >
          {"\u2726"}
        </span>
      )}
      <span
        className={`text-[14px] flex-1 ${text ? "text-fg-1" : "text-fg-4"}`}
      >
        {text || placeholder}
      </span>
      {mode === "push" && (
        <span className="text-[11px] text-fg-4 font-mono">Enter</span>
      )}
      {mode === "recall" && (
        <span className="text-[11px] text-fg-4 font-mono">Enter</span>
      )}
    </div>
  );
}

function WebTrackDemo() {
  return (
    <div className="space-y-5 max-w-[480px] mx-auto">
      <div>
        <StepLabel step={1} label="You're already here" />
        <BarMock
          mode="push"
          placeholder="Type anything and press Enter to remember..."
        />
        <p className="text-[12px] text-fg-4 mt-1.5 ml-7">
          The bar is your entry point. Just type.
        </p>
      </div>
      <div>
        <StepLabel step={2} label="Push your first memory" />
        <BarMock
          mode="push"
          text="I prefer TypeScript, use pnpm, deploy to Vercel"
        />
        <p className="text-[12px] text-fg-4 mt-1.5 ml-7">
          Press Enter. Remembered.
        </p>
      </div>
      <div>
        <StepLabel step={3} label="Switch to recall" />
        <div className="flex items-center gap-2 mb-2 ml-7">
          <span className="text-[12px] text-fg-4">Press</span>
          <span
            className="text-[11px] font-mono px-1.5 py-0.5 rounded"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.06)",
              color: "var(--fg-2)",
            }}
          >
            Tab
          </span>
          <span className="text-[12px] text-fg-4">to switch modes</span>
        </div>
        <BarMock mode="recall" text="what's my deploy target?" />
        <div
          className="mt-2 rounded-xl px-4 py-3"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.03)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <div className="flex items-center gap-1.5 mb-1">
            <span
              className="state-slow-breathe text-[13px]"
              style={{ color: "var(--signature)" }}
            >
              {"\u2726"}
            </span>
            <span className="text-[12px] text-fg-4">memax</span>
          </div>
          <p className="text-[14px] text-fg-1 leading-relaxed">
            You deploy to Vercel, using pnpm and TypeScript.
          </p>
        </div>
      </div>
    </div>
  );
}

/* ── Team Track ── */

function TeamTrackDemo() {
  return (
    <div className="space-y-5">
      <div>
        <StepLabel step={1} label="Name your team's shared brain" />
        <div
          className="rounded-xl px-4 py-4 flex items-center gap-3"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <Users className="w-5 h-5 text-fg-3 shrink-0" strokeWidth={1.5} />
          <div
            className="flex-1 h-9 rounded-lg px-3 flex items-center text-[14px] text-fg-1"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.03)",
              border: "1px solid oklch(from var(--foreground) l c h / 0.08)",
            }}
          >
            engineering
          </div>
          <button
            className="h-9 px-4 rounded-lg text-[13px] font-medium cursor-default"
            style={{
              background: "var(--foreground)",
              color: "var(--background)",
            }}
          >
            Create
          </button>
        </div>
      </div>
      <div>
        <StepLabel step={2} label="Invite a teammate" />
        <div
          className="rounded-xl px-4 py-4 space-y-3"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <div className="flex items-center gap-2">
            <Mail className="w-4 h-4 text-fg-4" strokeWidth={1.5} />
            <div
              className="flex-1 h-9 rounded-lg px-3 flex items-center text-[14px] text-fg-3"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.03)",
                border: "1px solid oklch(from var(--foreground) l c h / 0.08)",
              }}
            >
              ziyang@team.com
            </div>
            <button
              className="h-9 px-3 rounded-lg text-[13px] font-medium cursor-default flex items-center gap-1.5"
              style={{
                background: "var(--foreground)",
                color: "var(--background)",
              }}
            >
              <Send className="w-3.5 h-3.5" />
              Invite
            </button>
          </div>
          <p className="text-[12px] text-fg-4">
            Or share a join link — one click to join.
          </p>
        </div>
      </div>
      <div>
        <StepLabel step={3} label="Push something your team should know" />
        <TerminalBlock
          command='memax push --hub engineering "Our staging env is staging.memax.dev, deploy via fly deploy"'
          output="Remembered in engineering hub."
        />
      </div>
      <div>
        <StepLabel step={4} label="The magic" />
        <div
          className="rounded-xl px-4 py-4 space-y-3"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <div className="flex items-center gap-2.5">
            <div
              className="w-7 h-7 rounded-lg flex items-center justify-center"
              style={{
                background: `oklch(from ${AGENT_IDENTITIES["cursor"].color} l c h / 0.12)`,
              }}
            >
              <Bot
                className="w-3.5 h-3.5"
                style={{ color: AGENT_IDENTITIES["cursor"].color }}
                strokeWidth={1.8}
              />
            </div>
            <div>
              <p className="text-[13px] text-fg-1">
                <span className="font-medium">Ziyang&apos;s Cursor</span>{" "}
                <span className="text-fg-4">asked</span>
              </p>
              <p className="text-[13px] text-fg-3">
                &ldquo;Where do we deploy staging?&rdquo;
              </p>
            </div>
          </div>
          <div className="flex items-start gap-1.5 pl-1">
            <span
              className="state-slow-breathe text-[13px] mt-0.5"
              style={{ color: "var(--signature)" }}
            >
              {"\u2726"}
            </span>
            <p className="text-[14px] text-fg-1 leading-relaxed">
              Staging is at staging.memax.dev. Deploy via{" "}
              <span className="font-mono text-[13px]">fly deploy</span>.
              <span className="text-fg-4 text-[12px] ml-1.5">
                &mdash; from Jiahao, engineering hub
              </span>
            </p>
          </div>
          <p className="text-[12px] text-fg-4 pt-1">
            Your teammate&apos;s agent just recalled your knowledge. No Slack
            DM, no wiki search.
          </p>
        </div>
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   PART 2.5: END-TO-END INTERACTIVE WALKTHROUGH
   The full inline journey: welcome → preference → track → team
   prompt → completed. Stepper at top, AnimatePresence between
   phases. Modal A/B are demoed separately (34-modal-agents /
   34-modal-hub); this walkthrough shows the inline state flow
   only, with pointers to where modals are invoked.
   ═══════════════════════════════════════════════ */

type OnboardingPhase =
  | "welcome"
  | "preference"
  | "track"
  | "team-prompt"
  | "completed";

function OnboardingE2EDemo() {
  const [phase, setPhase] = useState<OnboardingPhase>("welcome");
  const [selectedPref, setSelectedPref] = useState<Preference | null>(null);
  const reduced = useReducedMotion();

  const phases: { id: OnboardingPhase; label: string }[] = [
    { id: "welcome", label: "Welcome" },
    { id: "preference", label: "How you work" },
    { id: "track", label: "Your track" },
    { id: "team-prompt", label: "Team" },
    { id: "completed", label: "Done" },
  ];
  const currentIndex = phases.findIndex((p) => p.id === phase);

  const reset = () => {
    setPhase("welcome");
    setSelectedPref(null);
  };
  const chooseTrack = (pref: Preference) => {
    setSelectedPref(pref);
    setPhase("track");
  };

  const trackMeta = selectedPref
    ? PREFERENCES.find((p) => p.id === selectedPref)
    : null;

  return (
    <div className="space-y-4">
      {/* Stepper — 5 phases, current one highlighted */}
      <div
        className="flex items-center justify-between gap-2 rounded-xl px-4 py-3"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.03)",
          border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
        }}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0 overflow-x-auto">
          {phases.map((p, i) => {
            const isActive = p.id === phase;
            const isPast = i < currentIndex;
            return (
              <div key={p.id} className="flex items-center gap-2 shrink-0">
                <div
                  className="flex items-center gap-1.5"
                  style={{ opacity: isActive || isPast ? 1 : 0.4 }}
                >
                  <div
                    className="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-medium tabular-nums"
                    style={{
                      background: isActive
                        ? "var(--foreground)"
                        : isPast
                          ? "oklch(from var(--foreground) l c h / 0.15)"
                          : "oklch(from var(--foreground) l c h / 0.08)",
                      color: isActive ? "var(--background)" : "var(--fg-2)",
                    }}
                  >
                    {isPast ? (
                      <Check className="w-3 h-3" strokeWidth={2.5} />
                    ) : (
                      i + 1
                    )}
                  </div>
                  <span
                    className={`text-[12px] ${isActive ? "text-fg-1 font-medium" : "text-fg-3"}`}
                  >
                    {p.label}
                  </span>
                </div>
                {i < phases.length - 1 && (
                  <ArrowRight className="w-3 h-3 text-fg-4 shrink-0" />
                )}
              </div>
            );
          })}
        </div>
        <button
          type="button"
          onClick={reset}
          className="shrink-0 text-[11px] text-fg-4 hover:text-fg-2 transition-colors cursor-pointer"
        >
          {/* i18n: t.onboarding.reset */}
          Reset
        </button>
      </div>

      {/* Phase container */}
      <div
        className="rounded-2xl p-6 relative overflow-hidden"
        style={{
          border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          background: "oklch(from var(--foreground) l c h / 0.01)",
          minHeight: 320,
        }}
      >
        <AnimatePresence mode="wait">
          {phase === "welcome" && (
            <motion.div
              key="welcome"
              initial={reduced ? { opacity: 0 } : { opacity: 0, x: 16 }}
              animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
              exit={reduced ? { opacity: 0 } : { opacity: 0, x: -16 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="flex flex-col items-center text-center"
            >
              <div
                className="w-14 h-14 rounded-2xl flex items-center justify-center mb-4"
                style={{
                  background: "oklch(from var(--signature) l c h / 0.1)",
                }}
              >
                <span
                  className="text-[24px]"
                  style={{ color: "var(--signature)" }}
                >
                  {"\u2726"}
                </span>
              </div>
              <h2
                className="text-[22px] font-semibold text-fg-1 mb-2"
                style={{ letterSpacing: "-0.01em" }}
              >
                {/* i18n: t.onboarding.welcome.title */}
                Welcome to memax
              </h2>
              <p className="text-[13px] text-fg-3 max-w-[440px] leading-relaxed mb-4">
                {/* i18n: t.onboarding.welcome.body */}
                Your memory layer across every AI. Dump anything in, ask from
                anywhere, come back tomorrow to find it organized.
              </p>
              <button
                type="button"
                onClick={() => setPhase("preference")}
                className="h-10 px-5 rounded-xl text-[13px] font-medium cursor-pointer transition-opacity hover:opacity-90"
                style={{
                  background: "var(--foreground)",
                  color: "var(--background)",
                }}
              >
                {/* i18n: t.onboarding.welcome.cta */}
                Get started
              </button>
            </motion.div>
          )}

          {phase === "preference" && (
            <motion.div
              key="preference"
              initial={reduced ? { opacity: 0 } : { opacity: 0, x: 16 }}
              animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
              exit={reduced ? { opacity: 0 } : { opacity: 0, x: -16 }}
              transition={{ duration: NORMAL, ease: EASE }}
            >
              <div className="text-center mb-5">
                <h2 className="text-[18px] font-semibold text-fg-1 mb-1">
                  {/* i18n: t.onboarding.preference.title */}
                  How do you work?
                </h2>
                <p className="text-[12px] text-fg-3">
                  {/* i18n: t.onboarding.preference.body */}
                  Pick the surface you&apos;ll use most. Memax works across all
                  three — this just sets the first track.
                </p>
              </div>
              <div className="grid grid-cols-3 gap-3 max-w-[560px] mx-auto">
                {PREFERENCES.map((pref) => {
                  const Icon = pref.icon;
                  return (
                    <button
                      key={pref.id}
                      type="button"
                      onClick={() => chooseTrack(pref.id)}
                      className="flex flex-col items-center text-center rounded-xl p-4 gap-2 cursor-pointer hover:bg-surface-1 transition-colors"
                      style={{
                        background:
                          "oklch(from var(--foreground) l c h / 0.02)",
                        border:
                          "1px solid oklch(from var(--foreground) l c h / 0.06)",
                      }}
                    >
                      <div
                        className="w-9 h-9 rounded-xl flex items-center justify-center"
                        style={{
                          background:
                            "oklch(from var(--foreground) l c h / 0.05)",
                        }}
                      >
                        <Icon className="w-4 h-4 text-fg-2" strokeWidth={1.5} />
                      </div>
                      <div>
                        <p className="text-[13px] text-fg-1 font-medium">
                          {pref.title}
                        </p>
                        <p className="text-[11px] text-fg-4 mt-0.5">
                          {pref.subtitle}
                        </p>
                      </div>
                    </button>
                  );
                })}
              </div>
              <div className="mt-5 text-center">
                <button
                  type="button"
                  onClick={() => setPhase("welcome")}
                  className="text-[12px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
                >
                  Back
                </button>
              </div>
            </motion.div>
          )}

          {phase === "track" && selectedPref && trackMeta && (
            <motion.div
              key={`track-${selectedPref}`}
              initial={reduced ? { opacity: 0 } : { opacity: 0, x: 16 }}
              animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
              exit={reduced ? { opacity: 0 } : { opacity: 0, x: -16 }}
              transition={{ duration: NORMAL, ease: EASE }}
            >
              <div className="flex items-center gap-2 mb-4">
                <div
                  className="w-8 h-8 rounded-lg flex items-center justify-center"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  <trackMeta.icon
                    className="w-4 h-4 text-fg-2"
                    strokeWidth={1.5}
                  />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-[12px] text-fg-3">{trackMeta.subtitle}</p>
                  <p className="text-[14px] text-fg-1 font-medium truncate">
                    {trackMeta.title}
                  </p>
                </div>
              </div>
              <div
                className="rounded-xl p-4 mb-4"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.02)",
                  border:
                    "1px solid oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                {selectedPref === "cli" && <CLITrackDemo />}
                {selectedPref === "agent" && <AgentTrackDemo />}
                {selectedPref === "web" && <WebTrackDemo />}
              </div>
              <div className="flex items-center justify-between gap-3">
                <button
                  type="button"
                  onClick={() => setPhase("preference")}
                  className="text-[12px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
                >
                  Back
                </button>
                <button
                  type="button"
                  onClick={() => setPhase("team-prompt")}
                  className="h-9 px-4 rounded-lg text-[12px] font-medium cursor-pointer transition-opacity hover:opacity-90"
                  style={{
                    background: "var(--foreground)",
                    color: "var(--background)",
                  }}
                >
                  {/* i18n: t.onboarding.track.continue */}
                  Done — continue
                </button>
              </div>
            </motion.div>
          )}

          {phase === "team-prompt" && (
            <motion.div
              key="team-prompt"
              initial={reduced ? { opacity: 0 } : { opacity: 0, x: 16 }}
              animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
              exit={reduced ? { opacity: 0 } : { opacity: 0, x: -16 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="flex flex-col items-center text-center"
            >
              <div
                className="w-12 h-12 rounded-2xl flex items-center justify-center mb-3"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                <Users className="w-5 h-5 text-fg-2" strokeWidth={1.8} />
              </div>
              <h2 className="text-[18px] font-semibold text-fg-1 mb-2">
                {/* i18n: t.onboarding.team.title */}
                Working with a team?
              </h2>
              <p className="text-[13px] text-fg-3 max-w-[440px] mb-2 leading-relaxed">
                {/* i18n: t.onboarding.team.body */}
                Create a shared hub to sync memories across people and agents.
                Or skip — you can create one anytime from the hub switcher.
              </p>
              <p className="text-[11px] text-fg-4 max-w-[440px] mb-5 leading-relaxed">
                In prod, the &ldquo;Create a team hub&rdquo; button below
                launches §34-modal-hub. Kitchen demo advances directly to the
                completed state for the walkthrough.
              </p>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => setPhase("completed")}
                  className="h-10 px-4 rounded-xl text-[13px] text-fg-3 hover:text-fg-1 hover:bg-surface-1 cursor-pointer transition-colors"
                >
                  {/* i18n: t.onboarding.team.skip */}
                  Skip for now
                </button>
                <button
                  type="button"
                  onClick={() => setPhase("completed")}
                  className="h-10 px-5 rounded-xl text-[13px] font-medium cursor-pointer transition-opacity hover:opacity-90"
                  style={{
                    background: "var(--foreground)",
                    color: "var(--background)",
                  }}
                >
                  {/* i18n: t.onboarding.team.cta */}
                  Create a team hub
                </button>
              </div>
              <button
                type="button"
                onClick={() => setPhase("track")}
                className="mt-5 text-[11px] text-fg-4 hover:text-fg-2 cursor-pointer transition-colors"
              >
                Back
              </button>
            </motion.div>
          )}

          {phase === "completed" && (
            <motion.div
              key="completed"
              initial={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.96 }}
              animate={reduced ? { opacity: 1 } : { opacity: 1, scale: 1 }}
              exit={reduced ? { opacity: 0 } : { opacity: 0 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="flex flex-col items-center text-center"
            >
              <div
                className="w-14 h-14 rounded-2xl flex items-center justify-center mb-3"
                style={{ background: "oklch(0.7 0.18 145 / 0.1)" }}
              >
                <Check
                  className="w-6 h-6"
                  style={{ color: "oklch(0.7 0.18 145)" }}
                  strokeWidth={2.5}
                />
              </div>
              <h2 className="text-[18px] font-semibold text-fg-1 mb-2">
                {/* i18n: t.onboarding.completed.title */}
                You&apos;re set up
              </h2>
              <p className="text-[13px] text-fg-3 max-w-[440px] mb-4 leading-relaxed">
                {/* i18n: t.onboarding.completed.body */}
                Dashboard is now live. Push from any surface, recall from any
                agent — memax does the rest.
              </p>
              <p className="text-[11px] text-fg-4 mb-4 max-w-[400px]">
                {/* i18n: t.onboarding.completed.replay */}
                Replay this flow anytime from{" "}
                <span className="font-mono">Settings → Onboarding tour</span>.
                Prod writes{" "}
                <span className="font-mono">
                  onboarding_completed_at = now()
                </span>{" "}
                at this point.
              </p>
              <button
                type="button"
                onClick={reset}
                className="text-[12px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
              >
                Reset demo
              </button>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   PART 3: STEPPED MODALS
   Two legitimate modal walkthroughs — everything else is inline.
   Modal criteria: settings-heavy + one-off + high-stakes + sequential.
   Rule 5 (onboarding): inline by default, modal for settings-heavy
   flows only.
   ═══════════════════════════════════════════════ */

/* ── Modal shell — glass panel with step header + footer ── */

function ModalShell({
  title,
  subtitle,
  step,
  totalSteps,
  onClose,
  footer,
  children,
}: {
  title: string;
  subtitle?: string;
  step: number;
  totalSteps: number;
  onClose?: () => void;
  footer?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div
      className="rounded-2xl overflow-hidden shadow-2xl flex flex-col mx-auto"
      style={{
        width: 520,
        maxWidth: "100%",
        maxHeight: 640,
        background: "oklch(from var(--card) l c h / 0.96)",
        backdropFilter: "blur(24px) saturate(140%)",
        border: "1px solid oklch(from var(--border) l c h / 0.5)",
      }}
    >
      {/* Header */}
      <div className="px-6 pt-5 pb-4 border-b border-border/30 shrink-0">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-[11px] text-fg-4 tabular-nums font-mono">
                {step} / {totalSteps}
              </span>
              {/* Step dots */}
              <div className="flex items-center gap-1">
                {Array.from({ length: totalSteps }).map((_, i) => (
                  <div
                    key={i}
                    className="h-1 rounded-full transition-all"
                    style={{
                      width: i + 1 === step ? 16 : 4,
                      background:
                        i + 1 <= step
                          ? "var(--fg-1)"
                          : "oklch(from var(--foreground) l c h / 0.15)",
                    }}
                  />
                ))}
              </div>
            </div>
            <h2
              className="text-[20px] font-semibold text-fg-1"
              style={{ letterSpacing: "-0.01em" }}
            >
              {title}
            </h2>
            {subtitle && (
              <p className="text-[13px] text-fg-3 mt-1 leading-relaxed">
                {subtitle}
              </p>
            )}
          </div>
          {onClose && (
            <button
              type="button"
              onClick={onClose}
              className="shrink-0 p-1 rounded-md hover:bg-surface-1 cursor-pointer text-fg-3 hover:text-fg-1 transition-colors"
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
      {/* Body — scrollable */}
      <div className="flex-1 overflow-y-auto overscroll-contain px-6 py-5">
        {children}
      </div>
      {/* Footer — actions */}
      {footer && (
        <div className="px-6 py-4 border-t border-border/30 shrink-0">
          {footer}
        </div>
      )}
    </div>
  );
}

/* ── Modal A — Connect your agents (4 steps) ──
   Sequential, terminal-driven, each agent turns green as MCP handshake
   completes. One-off flow, invoked from Agent track OR Settings → Agents. */

type AgentKey =
  | "claude-code"
  | "cursor"
  | "copilot"
  | "windsurf"
  | "chatgpt"
  | "gemini";

interface DetectedAgent {
  key: AgentKey;
  name: string;
  installed: boolean;
  connected: "pending" | "connecting" | "connected" | "failed";
}

const INITIAL_AGENTS: DetectedAgent[] = [
  {
    key: "claude-code",
    name: "Claude Code",
    installed: true,
    connected: "pending",
  },
  { key: "cursor", name: "Cursor", installed: true, connected: "pending" },
  {
    key: "copilot",
    name: "GitHub Copilot",
    installed: true,
    connected: "pending",
  },
  { key: "windsurf", name: "Windsurf", installed: false, connected: "pending" },
  {
    key: "chatgpt",
    name: "ChatGPT Desktop",
    installed: true,
    connected: "pending",
  },
  { key: "gemini", name: "Gemini CLI", installed: false, connected: "pending" },
];

function AgentsModalDemo() {
  const [step, setStep] = useState(1);
  const [agents, setAgents] = useState<DetectedAgent[]>(INITIAL_AGENTS);
  const [copied, setCopied] = useState(false);
  const totalSteps = 4;
  const reduced = useReducedMotion();
  const installedAgents = agents.filter((a) => a.installed);
  const connectedCount = agents.filter(
    (a) => a.connected === "connected",
  ).length;

  // Step 2 simulation — when user clicks "Run command", flip agents to
  // connecting then connected one by one to show the terminal progress.
  const simulateConnect = () => {
    setCopied(true);
    installedAgents.forEach((agent, i) => {
      setTimeout(
        () => {
          setAgents((prev) =>
            prev.map((a) =>
              a.key === agent.key ? { ...a, connected: "connecting" } : a,
            ),
          );
        },
        300 + i * 400,
      );
      setTimeout(
        () => {
          setAgents((prev) =>
            prev.map((a) =>
              a.key === agent.key ? { ...a, connected: "connected" } : a,
            ),
          );
        },
        800 + i * 400,
      );
    });
  };

  const canAdvance = (() => {
    if (step === 1) return true;
    if (step === 2) return connectedCount > 0;
    if (step === 3) return connectedCount > 0;
    return false;
  })();

  const titles = [
    {
      title: "Connect your agents",
      subtitle: "memax works across the AI tools you already use.",
    },
    {
      title: "Run one command",
      subtitle:
        "This configures MCP + hooks for every installed agent. No per-app setup.",
    },
    {
      title: "Verify connections",
      subtitle: "Each agent is connected and attributable.",
    },
    {
      title: "You're set up",
      subtitle: `${connectedCount} agent${connectedCount === 1 ? "" : "s"} connected. Memories you push from any agent show up everywhere.`,
    },
  ];

  return (
    <ModalShell
      title={titles[step - 1]!.title}
      subtitle={titles[step - 1]!.subtitle}
      step={step}
      totalSteps={totalSteps}
      footer={
        <div className="flex items-center justify-between gap-3">
          {step > 1 ? (
            <button
              type="button"
              onClick={() => setStep(step - 1)}
              className="text-[13px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
            >
              Back
            </button>
          ) : (
            <div />
          )}
          <button
            type="button"
            disabled={!canAdvance}
            onClick={() => {
              if (step < totalSteps) setStep(step + 1);
            }}
            className="h-9 px-4 rounded-lg text-[13px] font-medium cursor-pointer transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
            style={{
              background: "var(--foreground)",
              color: "var(--background)",
            }}
          >
            {step === totalSteps
              ? "Open memax"
              : step === 1
                ? "Detect agents"
                : "Continue"}
          </button>
        </div>
      }
    >
      <AnimatePresence mode="wait">
        <motion.div
          key={step}
          initial={reduced ? { opacity: 0 } : { opacity: 0, x: 12 }}
          animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
          exit={reduced ? { opacity: 0 } : { opacity: 0, x: -12 }}
          transition={{ duration: NORMAL, ease: EASE }}
        >
          {/* Step 1 — Detect */}
          {step === 1 && (
            <div className="space-y-2">
              <p className="text-[12px] text-fg-3 mb-3">
                {/* i18n: t.onboarding.agents.step1.description */}
                Detected on this machine:
              </p>
              {agents.map((agent) => (
                <div
                  key={agent.key}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.02)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  <div
                    className="w-7 h-7 rounded-lg flex items-center justify-center shrink-0"
                    style={{
                      background: agent.installed
                        ? `oklch(from ${AGENT_IDENTITIES[agent.key]?.color ?? "oklch(0.6 0.1 270)"} l c h / 0.12)`
                        : "oklch(from var(--foreground) l c h / 0.04)",
                    }}
                  >
                    <Bot
                      className="w-3.5 h-3.5"
                      style={{
                        color: agent.installed
                          ? (AGENT_IDENTITIES[agent.key]?.color ??
                            "oklch(0.6 0.1 270)")
                          : "var(--fg-4)",
                      }}
                      strokeWidth={1.8}
                    />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-[13px] text-fg-1 font-medium truncate">
                      {agent.name}
                    </p>
                    <p className="text-[11px] text-fg-4">
                      {agent.installed ? "found" : "not installed"}
                    </p>
                  </div>
                  {agent.installed ? (
                    <Check
                      className="w-4 h-4 shrink-0"
                      style={{ color: "oklch(0.7 0.18 145)" }}
                      strokeWidth={2.5}
                    />
                  ) : (
                    <X
                      className="w-4 h-4 text-fg-4 shrink-0"
                      strokeWidth={1.8}
                    />
                  )}
                </div>
              ))}
            </div>
          )}
          {/* Step 2 — Command + terminal progress */}
          {step === 2 && (
            <div className="space-y-3">
              <div
                className="rounded-xl overflow-hidden"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.04)",
                  border:
                    "1px solid oklch(from var(--foreground) l c h / 0.08)",
                }}
              >
                <div className="flex items-center gap-2 px-4 py-3">
                  <Terminal className="w-3.5 h-3.5 text-fg-4" />
                  <code className="flex-1 text-[13px] font-mono text-fg-1 truncate">
                    npx memax-cli@latest setup --all
                  </code>
                  <button
                    type="button"
                    onClick={simulateConnect}
                    className="shrink-0 text-[11px] text-fg-3 hover:text-fg-1 flex items-center gap-1 cursor-pointer transition-colors"
                  >
                    {copied ? (
                      <>
                        <Check className="w-3 h-3" strokeWidth={2.5} /> Running
                      </>
                    ) : (
                      <>
                        <Copy className="w-3 h-3" /> Copy & run
                      </>
                    )}
                  </button>
                </div>
                {/* Progress rows */}
                {copied && (
                  <div
                    className="px-4 pb-3 space-y-1"
                    style={{
                      borderTop:
                        "1px solid oklch(from var(--foreground) l c h / 0.06)",
                    }}
                  >
                    {installedAgents.map((agent) => (
                      <div
                        key={agent.key}
                        className="flex items-center gap-2 text-[12px] font-mono pt-2"
                      >
                        {agent.connected === "connected" ? (
                          <Check
                            className="w-3 h-3"
                            style={{ color: "oklch(0.7 0.18 145)" }}
                            strokeWidth={2.5}
                          />
                        ) : agent.connected === "connecting" ? (
                          <Loader2 className="w-3 h-3 text-fg-3 animate-spin" />
                        ) : (
                          <span className="w-3 h-3 flex items-center justify-center text-fg-4">
                            ○
                          </span>
                        )}
                        <span
                          className={
                            agent.connected === "connected"
                              ? "text-fg-2"
                              : "text-fg-4"
                          }
                        >
                          {agent.name}
                        </span>
                        <span className="text-fg-4 ml-auto">
                          {agent.connected === "connected"
                            ? "mcp handshake ✓"
                            : agent.connected === "connecting"
                              ? "connecting…"
                              : "waiting"}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
              <p className="text-[11px] text-fg-4">
                {/* i18n: t.onboarding.agents.step2.hint */}
                This runs locally. memax never reads your files — only the agent
                config dirs (~/.claude, ~/.cursor, etc.).
              </p>
            </div>
          )}
          {/* Step 3 — Verify */}
          {step === 3 && (
            <div className="space-y-2">
              {installedAgents.map((agent) => (
                <div
                  key={agent.key}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.02)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  <div
                    className="w-7 h-7 rounded-lg flex items-center justify-center shrink-0"
                    style={{
                      background: `oklch(from ${AGENT_IDENTITIES[agent.key]?.color ?? "oklch(0.6 0.1 270)"} l c h / 0.12)`,
                    }}
                  >
                    <Bot
                      className="w-3.5 h-3.5"
                      style={{
                        color:
                          AGENT_IDENTITIES[agent.key]?.color ??
                          "oklch(0.6 0.1 270)",
                      }}
                      strokeWidth={1.8}
                    />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-[13px] text-fg-1 font-medium truncate">
                      {agent.name}
                    </p>
                    <p className="text-[11px] text-fg-4">
                      source_agent:{" "}
                      <span className="font-mono">{agent.key}</span>
                    </p>
                  </div>
                  <div className="shrink-0 flex items-center gap-1.5 text-[11px] text-fg-3">
                    <div
                      className="w-1.5 h-1.5 rounded-full"
                      style={{ background: "oklch(0.7 0.18 145)" }}
                    />
                    Connected
                  </div>
                </div>
              ))}
            </div>
          )}
          {/* Step 4 — Done */}
          {step === 4 && (
            <div className="flex flex-col items-center text-center py-4 gap-4">
              <div
                className="w-14 h-14 rounded-2xl flex items-center justify-center"
                style={{
                  background: "oklch(0.7 0.18 145 / 0.1)",
                }}
              >
                <Check
                  className="w-6 h-6"
                  style={{ color: "oklch(0.7 0.18 145)" }}
                  strokeWidth={2.5}
                />
              </div>
              <div className="space-y-1">
                <p className="text-[15px] text-fg-1 font-medium">
                  {/* i18n: t.onboarding.agents.step4.title */}
                  You&apos;re set up
                </p>
                <p className="text-[13px] text-fg-3 max-w-[320px]">
                  {/* i18n: t.onboarding.agents.step4.body */}
                  Push from any agent — it shows up everywhere. Recall from any
                  agent — it reads everything.
                </p>
              </div>
            </div>
          )}
        </motion.div>
      </AnimatePresence>
    </ModalShell>
  );
}

/* ── Modal B — Create or join a team hub (5 steps) ──
   High-stakes boundary creation. Multi-field with visibility picker,
   invite flow, first-push priming. One-off per hub. */

type HubIntent = "create" | "join";

function HubModalDemo() {
  const [step, setStep] = useState(1);
  const [intent, setIntent] = useState<HubIntent | null>(null);
  const [hubName, setHubName] = useState("engineering");
  const [hubDescription, setHubDescription] = useState(
    "Shared memory for the memax engineering team.",
  );
  const [visibility, setVisibility] = useState<"private" | "link">("private");
  const [invites, setInvites] = useState("ziyang@memax.dev");
  const [firstPush, setFirstPush] = useState(
    "Our staging env is staging.memax.dev. Deploy via `fly deploy`.",
  );
  const reduced = useReducedMotion();
  const totalSteps = 5;

  const canAdvance = (() => {
    if (step === 1) return intent !== null;
    if (step === 2) return intent === "join" || hubName.trim().length > 0;
    if (step === 3) return true; // invite optional
    if (step === 4) return true; // first push optional
    return false;
  })();

  const titles = [
    {
      title: "Team hub",
      subtitle:
        "Shared memory between people and agents. One place your team remembers from.",
    },
    {
      title: intent === "join" ? "Join a hub" : "Name your hub",
      subtitle:
        intent === "join"
          ? "Paste the invite link your teammate shared."
          : "Pick a name your team will recognize. You can rename later.",
    },
    {
      title: "Invite your team",
      subtitle:
        "Add teammates now or skip — you can invite anytime from Settings → Members.",
    },
    {
      title: "Push something your team should know",
      subtitle:
        "Prime the hub with one decision, preference, or fact. Skippable.",
    },
    {
      title: "Your hub is live",
      subtitle: `${hubName} is ready. Memories scoped here are visible to members only.`,
    },
  ];

  return (
    <ModalShell
      title={titles[step - 1]!.title}
      subtitle={titles[step - 1]!.subtitle}
      step={step}
      totalSteps={totalSteps}
      footer={
        <div className="flex items-center justify-between gap-3">
          {step > 1 ? (
            <button
              type="button"
              onClick={() => setStep(step - 1)}
              className="text-[13px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
            >
              Back
            </button>
          ) : (
            <div />
          )}
          <div className="flex items-center gap-2">
            {(step === 3 || step === 4) && (
              <button
                type="button"
                onClick={() => setStep(step + 1)}
                className="text-[13px] text-fg-3 hover:text-fg-1 px-3 py-2 cursor-pointer transition-colors"
              >
                Skip
              </button>
            )}
            <button
              type="button"
              disabled={!canAdvance}
              onClick={() => {
                if (step < totalSteps) setStep(step + 1);
              }}
              className="h-9 px-4 rounded-lg text-[13px] font-medium cursor-pointer transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
              style={{
                background: "var(--foreground)",
                color: "var(--background)",
              }}
            >
              {step === totalSteps
                ? "Open the hub"
                : step === 1
                  ? "Continue"
                  : step === 2
                    ? intent === "join"
                      ? "Join"
                      : "Create"
                    : step === 3
                      ? "Send invites"
                      : "Push it"}
            </button>
          </div>
        </div>
      }
    >
      <AnimatePresence mode="wait">
        <motion.div
          key={step}
          initial={reduced ? { opacity: 0 } : { opacity: 0, x: 12 }}
          animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
          exit={reduced ? { opacity: 0 } : { opacity: 0, x: -12 }}
          transition={{ duration: NORMAL, ease: EASE }}
        >
          {/* Step 1 — Intent */}
          {step === 1 && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <button
                type="button"
                onClick={() => setIntent("create")}
                className="flex flex-col items-start text-left rounded-xl p-4 gap-2 transition-all cursor-pointer"
                style={{
                  background:
                    intent === "create"
                      ? "oklch(from var(--foreground) l c h / 0.06)"
                      : "oklch(from var(--foreground) l c h / 0.02)",
                  border:
                    intent === "create"
                      ? "1.5px solid oklch(from var(--foreground) l c h / 0.2)"
                      : "1px solid oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                <div
                  className="w-9 h-9 rounded-xl flex items-center justify-center"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  <Users className="w-4 h-4 text-fg-2" strokeWidth={1.8} />
                </div>
                <p className="text-[14px] text-fg-1 font-medium">
                  Create a new hub
                </p>
                <p className="text-[12px] text-fg-3 leading-snug">
                  Start fresh. You can invite teammates next.
                </p>
              </button>
              <button
                type="button"
                onClick={() => setIntent("join")}
                className="flex flex-col items-start text-left rounded-xl p-4 gap-2 transition-all cursor-pointer"
                style={{
                  background:
                    intent === "join"
                      ? "oklch(from var(--foreground) l c h / 0.06)"
                      : "oklch(from var(--foreground) l c h / 0.02)",
                  border:
                    intent === "join"
                      ? "1.5px solid oklch(from var(--foreground) l c h / 0.2)"
                      : "1px solid oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                <div
                  className="w-9 h-9 rounded-xl flex items-center justify-center"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  <Link2 className="w-4 h-4 text-fg-2" strokeWidth={1.8} />
                </div>
                <p className="text-[14px] text-fg-1 font-medium">
                  Join an existing hub
                </p>
                <p className="text-[12px] text-fg-3 leading-snug">
                  Paste an invite link from your teammate.
                </p>
              </button>
            </div>
          )}
          {/* Step 2 — Name + visibility (or paste link) */}
          {step === 2 && intent === "create" && (
            <div className="space-y-4">
              <div>
                <label className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-1.5 block">
                  Name
                </label>
                <input
                  type="text"
                  value={hubName}
                  onChange={(e) => setHubName(e.target.value)}
                  placeholder="engineering"
                  className="w-full h-10 rounded-lg px-3 text-[14px] text-fg-1 outline-none focus:border-fg-2 transition-colors"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.03)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.08)",
                  }}
                />
              </div>
              <div>
                <label className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-1.5 block">
                  Description{" "}
                  <span className="text-fg-4 normal-case">— optional</span>
                </label>
                <textarea
                  value={hubDescription}
                  onChange={(e) => setHubDescription(e.target.value)}
                  rows={2}
                  className="w-full rounded-lg px-3 py-2 text-[14px] text-fg-1 outline-none focus:border-fg-2 transition-colors resize-none"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.03)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.08)",
                  }}
                />
              </div>
              <div>
                <label className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-1.5 block">
                  Visibility
                </label>
                <div className="space-y-2">
                  {(
                    [
                      {
                        value: "private" as const,
                        label: "Private",
                        hint: "Only invited members can see or recall memories.",
                      },
                      {
                        value: "link" as const,
                        label: "Link-accessible",
                        hint: "Anyone with the invite link can join. Same per-member access after joining.",
                      },
                    ] as const
                  ).map((opt) => (
                    <button
                      key={opt.value}
                      type="button"
                      onClick={() => setVisibility(opt.value)}
                      className="w-full flex items-start gap-3 rounded-lg px-3 py-2.5 text-left cursor-pointer transition-all"
                      style={{
                        background:
                          visibility === opt.value
                            ? "oklch(from var(--foreground) l c h / 0.05)"
                            : "oklch(from var(--foreground) l c h / 0.02)",
                        border:
                          visibility === opt.value
                            ? "1px solid oklch(from var(--foreground) l c h / 0.15)"
                            : "1px solid oklch(from var(--foreground) l c h / 0.06)",
                      }}
                    >
                      <div
                        className="w-4 h-4 rounded-full border-2 shrink-0 mt-0.5 flex items-center justify-center"
                        style={{
                          borderColor:
                            visibility === opt.value
                              ? "var(--fg-1)"
                              : "var(--fg-4)",
                        }}
                      >
                        {visibility === opt.value && (
                          <div
                            className="w-1.5 h-1.5 rounded-full"
                            style={{ background: "var(--fg-1)" }}
                          />
                        )}
                      </div>
                      <div className="min-w-0">
                        <p className="text-[13px] text-fg-1 font-medium">
                          {opt.label}
                        </p>
                        <p className="text-[11px] text-fg-3 mt-0.5">
                          {opt.hint}
                        </p>
                      </div>
                      {opt.value === "private" && (
                        <Shield
                          className="w-3.5 h-3.5 text-fg-4 ml-auto shrink-0"
                          strokeWidth={1.8}
                        />
                      )}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          )}
          {step === 2 && intent === "join" && (
            <div className="space-y-3">
              <div>
                <label className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-1.5 block">
                  Invite link
                </label>
                <input
                  type="text"
                  defaultValue="https://memax.app/join/eng-abc123"
                  readOnly
                  className="w-full h-10 rounded-lg px-3 text-[13px] font-mono text-fg-2 outline-none"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.03)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.08)",
                  }}
                />
              </div>
              <div
                className="rounded-lg px-3 py-3 flex items-center gap-3"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.02)",
                  border:
                    "1px solid oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                <div
                  className="w-9 h-9 rounded-xl flex items-center justify-center"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  <Users className="w-4 h-4 text-fg-2" strokeWidth={1.8} />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-[13px] text-fg-1 font-medium">
                    engineering
                  </p>
                  <p className="text-[11px] text-fg-3">
                    2 members · Shared memory for the memax engineering team
                  </p>
                </div>
              </div>
            </div>
          )}
          {/* Step 3 — Invite */}
          {step === 3 && (
            <div className="space-y-3">
              <div>
                <label className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-1.5 block">
                  Emails — one per line
                </label>
                <textarea
                  value={invites}
                  onChange={(e) => setInvites(e.target.value)}
                  rows={4}
                  placeholder="ziyang@team.com&#10;someone-else@team.com"
                  className="w-full rounded-lg px-3 py-2 text-[13px] text-fg-1 outline-none transition-colors resize-none"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.03)",
                    border:
                      "1px solid oklch(from var(--foreground) l c h / 0.08)",
                  }}
                />
              </div>
              <div
                className="flex items-center gap-2 rounded-lg px-3 py-2.5"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.02)",
                  border:
                    "1px solid oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                <Link2 className="w-3.5 h-3.5 text-fg-3 shrink-0" />
                <span className="flex-1 text-[12px] text-fg-2 font-mono truncate">
                  memax.app/join/eng-abc123
                </span>
                <button
                  type="button"
                  className="text-[11px] text-fg-3 hover:text-fg-1 flex items-center gap-1 cursor-pointer transition-colors"
                >
                  <Copy className="w-3 h-3" /> Copy link
                </button>
              </div>
              <p className="text-[11px] text-fg-4">
                {/* i18n: t.onboarding.hub.step3.hint */}
                Teammates join with one click. You can set per-member roles
                after they accept.
              </p>
            </div>
          )}
          {/* Step 4 — First push */}
          {step === 4 && (
            <div className="space-y-3">
              <div
                className="rounded-xl px-4 py-3"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.02)",
                  border:
                    "1px solid oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-2">
                  pushing to {hubName}
                </p>
                <textarea
                  value={firstPush}
                  onChange={(e) => setFirstPush(e.target.value)}
                  rows={3}
                  className="w-full bg-transparent text-[14px] text-fg-1 outline-none resize-none leading-relaxed"
                />
              </div>
              <p className="text-[11px] text-fg-4">
                {/* i18n: t.onboarding.hub.step4.hint */}
                One decision, preference, or fact your team should remember.
                Skippable — but priming the hub with one real memory makes the
                first recall feel alive.
              </p>
            </div>
          )}
          {/* Step 5 — Done */}
          {step === 5 && (
            <div className="flex flex-col items-center text-center py-4 gap-4">
              <div
                className="w-14 h-14 rounded-2xl flex items-center justify-center"
                style={{
                  background: "oklch(0.7 0.18 145 / 0.1)",
                }}
              >
                <Users
                  className="w-6 h-6"
                  style={{ color: "oklch(0.7 0.18 145)" }}
                  strokeWidth={2}
                />
              </div>
              <div className="space-y-1">
                <p className="text-[15px] text-fg-1 font-medium">
                  {hubName} is live
                </p>
                <p className="text-[13px] text-fg-3 max-w-[340px]">
                  {/* i18n: t.onboarding.hub.step5.body */}
                  Invite more members anytime from{" "}
                  <span className="font-mono text-[12px]">
                    Settings → Members
                  </span>
                  . Push with{" "}
                  <span className="font-mono text-[12px]">--hub {hubName}</span>{" "}
                  or switch via the hub dropdown.
                </p>
              </div>
            </div>
          )}
        </motion.div>
      </AnimatePresence>
    </ModalShell>
  );
}

/* ═══════════════════════════════════════════════
   PART 4: FAST-PATH TIMELINE — silent CLI ambient onboarding
   The aha moment isn't a tutorial; it's the product working for them
   before they ever visit the web app.
   ═══════════════════════════════════════════════ */

function FastPathTimelineDemo() {
  const steps = [
    {
      time: "0:00",
      icon: Terminal,
      title: "npx memax-cli login",
      detail: "GitHub OAuth. 10 seconds.",
      tone: "user",
    },
    {
      time: "0:10",
      icon: Terminal,
      title: "memax setup",
      detail:
        "Auto-detects installed agents. Configures MCP + hooks. Imports existing config files: MEMORY.md, .cursorrules, CLAUDE.md, AGENTS.md.",
      tone: "user",
    },
    {
      time: "0:45",
      icon: Sparkles,
      title: "✓ Imported 23 memories from 4 agents",
      detail: "User closes the terminal and goes back to work.",
      tone: "system",
    },
    {
      time: "— silent period —",
      icon: null,
      title: "Dreams organize imported memories into topics",
      detail: "User is doing something else. The product is working for them.",
      tone: "ambient",
    },
    {
      time: "Next morning",
      icon: Bot,
      title: "User opens Claude Code on a new task",
      detail:
        "Memax hook injects context. Claude says: &ldquo;based on your preferences, you use pnpm + TypeScript + Vercel.&rdquo;",
      tone: "agent",
    },
    {
      time: "+5 seconds",
      icon: Sparkles,
      title: "&ldquo;wait, how does it know that?&rdquo;",
      detail:
        "The aha moment. No tutorial. No guided tour. The product just remembered. User opens the memax web app — curious, not instructed.",
      tone: "aha",
    },
  ];

  return (
    <div className="relative">
      {/* Vertical rail */}
      <div
        className="absolute left-[18px] top-3 bottom-3 w-px"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.1)",
        }}
      />
      <div className="space-y-5">
        {steps.map((s, i) => {
          const Icon = s.icon;
          const isAha = s.tone === "aha";
          return (
            <div key={i} className="flex items-start gap-4 relative">
              <div
                className="w-9 h-9 rounded-full flex items-center justify-center shrink-0 relative"
                style={{
                  background:
                    s.tone === "user"
                      ? "oklch(from var(--foreground) l c h / 0.08)"
                      : s.tone === "ambient"
                        ? "oklch(from var(--foreground) l c h / 0.03)"
                        : s.tone === "agent"
                          ? `oklch(from ${AGENT_IDENTITIES["claude-code"]?.color ?? "oklch(0.6 0.15 30)"} l c h / 0.12)`
                          : "oklch(from var(--signature) l c h / 0.12)",
                  border: isAha
                    ? "1.5px solid var(--signature)"
                    : "1px solid oklch(from var(--foreground) l c h / 0.08)",
                }}
              >
                {Icon ? (
                  <Icon
                    className="w-4 h-4"
                    style={{
                      color: isAha
                        ? "var(--signature)"
                        : s.tone === "agent"
                          ? (AGENT_IDENTITIES["claude-code"]?.color ??
                            "oklch(0.6 0.15 30)")
                          : s.tone === "ambient"
                            ? "var(--fg-4)"
                            : "var(--fg-2)",
                    }}
                    strokeWidth={1.8}
                  />
                ) : (
                  <span
                    className="text-[14px]"
                    style={{ color: "var(--fg-4)" }}
                  >
                    {"\u2726"}
                  </span>
                )}
              </div>
              <div className="flex-1 min-w-0 pt-1">
                <div className="flex items-baseline gap-2 mb-0.5">
                  <p
                    className="text-[13px] font-medium"
                    style={{
                      color: isAha ? "var(--fg-1)" : "var(--fg-2)",
                    }}
                    dangerouslySetInnerHTML={{ __html: s.title }}
                  />
                  <span className="text-[11px] text-fg-4 tabular-nums font-mono">
                    {s.time}
                  </span>
                </div>
                <p
                  className="text-[12px] text-fg-3 leading-relaxed"
                  dangerouslySetInnerHTML={{ __html: s.detail }}
                />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   PART 5: STATE MACHINE — first-run detection + transitions
   Documents the onboarding state flow so Codex knows when to show
   what and how to recover from dismissals.
   ═══════════════════════════════════════════════ */

function OnboardingStateMachineDemo() {
  const states = [
    {
      state: "not_started",
      condition: "onboarding_completed_at IS NULL AND dismissed_at IS NULL",
      surface: "Welcome card (screen 1) inline on empty dashboard",
      exit: "User clicks [Get started] → in_progress",
    },
    {
      state: "in_progress",
      condition: "current_screen IN (preference, track, team)",
      surface: "Current inline card morphs in place (§29n container morph)",
      exit: "Track complete OR user dismisses → completed / dismissed",
    },
    {
      state: "modal_agents",
      condition: "Agent track triggered agents modal",
      surface: "ModalShell with 4-step connect flow",
      exit: "Close / Open memax → back to previous inline state",
    },
    {
      state: "modal_hub",
      condition: "Team prompt OR hub switcher → create hub",
      surface: "ModalShell with 5-step create/join flow",
      exit: "Close / Open hub → back to previous inline state",
    },
    {
      state: "completed",
      condition: "onboarding_completed_at IS NOT NULL",
      surface: "Dashboard renders real state. No onboarding cards.",
      exit: "Re-entry via Settings → Onboarding tour",
    },
    {
      state: "dismissed",
      condition: "dismissed_at IS NOT NULL AND completed_at IS NULL",
      surface:
        "Dashboard real state + small persistent 'Finish setup' pill in top bar",
      exit: "Click pill → resume at saved current_screen",
    },
  ];

  return (
    <div className="space-y-2">
      {states.map((s) => (
        <div
          key={s.state}
          className="rounded-xl px-4 py-3 flex items-start gap-4"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <div className="w-28 shrink-0">
            <p className="text-[12px] font-mono text-fg-1 font-medium">
              {s.state}
            </p>
          </div>
          <div className="flex-1 space-y-1 min-w-0">
            <p className="text-[11px] text-fg-4 font-mono">{s.condition}</p>
            <p className="text-[12px] text-fg-2">{s.surface}</p>
            <p className="text-[11px] text-fg-3 flex items-center gap-1">
              <ArrowRight className="w-3 h-3" />
              {s.exit}
            </p>
          </div>
        </div>
      ))}
    </div>
  );
}

/* ═══════════════════════════════════════════════
   PART 4: SUPER-NOTIF APPROACH (plan 18 RFC)
   Onboarding via two new notification kinds (checklist, digest).
   Founder note = system_notice. Checklist = decision super-notif.
   Pinned on /memories between HubHeader and RecentSection.
   ═══════════════════════════════════════════════ */

// Finalized copy for the 7 checklist items.
// i18n key prefix: onboarding.checklist.items.{id}
type SnStepId =
  | "welcome"
  | "connect_agent"
  | "first_memory"
  | "first_ask"
  | "five_memories"
  | "first_hub_invite"
  | "first_dream";

type SnStep = {
  id: SnStepId;
  icon: typeof Sparkles;
  title: string;
  description: string;
  completedTitle: string;
  ctaLabel?: string;
  ctaGlyph?: "down" | "up" | "arrow" | "none";
  required: boolean;
  lockedBy?: SnStepId[];
  progressTarget?: number;
};

const SN_STEPS: SnStep[] = [
  {
    id: "welcome",
    icon: Sparkles,
    title: "Read the welcome note",
    description: "90 seconds. Worth it.",
    completedTitle: "Read the welcome note",
    required: false,
  },
  {
    id: "connect_agent",
    icon: RobotIcon,
    title: "Connect your first agent",
    description:
      "Claude Code, Cursor, Codex — one command, memory flows both ways.",
    completedTitle: "Connected your first agent",
    ctaLabel: "Set up",
    ctaGlyph: "arrow",
    required: true,
  },
  {
    id: "first_memory",
    icon: Inbox,
    title: "Dump your first memory",
    description:
      "Type anything in the bar, hit ⌘↵. A wiki, a link, a half-thought.",
    completedTitle: "First memory saved",
    ctaLabel: "Try the bar",
    ctaGlyph: "down",
    required: true,
  },
  {
    id: "first_ask",
    icon: MessageCircle,
    title: "Ask memax a question",
    description:
      "Press ↵ instead of ⌘↵ — memax answers from everything you've dumped.",
    completedTitle: "Asked your first question",
    ctaLabel: "Try asking",
    ctaGlyph: "up",
    required: true,
  },
  {
    id: "five_memories",
    icon: Boxes,
    title: "Dump 5 memories",
    description:
      "memax needs a critical mass to start seeing patterns. Unlocks dreams.",
    completedTitle: "5 memories — dreams unlocked",
    required: true,
    progressTarget: 5,
  },
  {
    id: "first_hub_invite",
    icon: UsersRound,
    title: "Join or start a team hub",
    description:
      "Shared brain with people you work with. Jump into an existing hub or start your own.",
    completedTitle: "You're on a team hub",
    ctaLabel: "Explore hubs",
    ctaGlyph: "arrow",
    required: false,
  },
  {
    id: "first_dream",
    icon: Moon,
    title: "Let memax dream",
    description:
      "Dreams run nightly to connect the dots. Trigger one now to see it live.",
    completedTitle: "First dream complete",
    ctaLabel: "Trigger dream",
    ctaGlyph: "arrow",
    required: false,
    lockedBy: ["five_memories"],
  },
];

const SN_REQUIRED_COUNT = SN_STEPS.filter((s) => s.required).length;

// Founder note copy — i18n key: onboarding.welcome.*
// PMM voice: pain-first, specific, confident without hype.
// The three-beat product promise (agents pull / team sees / dreams connect)
// primes the three "aha" moments a new user will actually have.
const FOUNDER_NOTE = {
  greeting: "Hi, I'm Jiahao.",
  paragraphs: [
    "Your AI forgets you every session. You re-explain the same context, re-paste the same link, start from scratch every time you switch tools. It's exhausting.",
    "memax is a shared brain that makes it stop. Dump anything — your agents pull what they need, your team sees what you saw, and a dream engine connects the dots while you sleep.",
    "We ship every Friday. Things will break; tell us when they do.",
  ],
  signature: "— Jiahao",
  ctaLabel: "Start dumping",
  community: "Come say hi in our community — coming soon",
};

/* ── Founder note card (kind=system_notice, source_kind=onboarding_welcome) ── */

function SnFounderNoteCard({ onDismiss }: { onDismiss?: () => void }) {
  return (
    <div
      className="relative rounded-2xl p-5 overflow-hidden"
      style={{
        background: "var(--card)",
        boxShadow:
          "0 1px 0 oklch(from var(--foreground) l c h / 0.04), 0 8px 24px -12px oklch(from var(--foreground) l c h / 0.12)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      {/* Soft signature gradient in the top-right corner */}
      <div
        aria-hidden
        className="absolute -top-16 -right-20 w-60 h-60 rounded-full pointer-events-none"
        style={{
          background: `radial-gradient(circle, oklch(from ${SIGNATURE} l c h / 0.18) 0%, transparent 60%)`,
        }}
      />
      <div className="relative flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-2">
            <span
              className="inline-flex items-center justify-center w-6 h-6 rounded-md"
              style={{
                background: `oklch(from ${SIGNATURE} l c h / 0.14)`,
              }}
              aria-hidden
            >
              <Sparkles
                className="w-3.5 h-3.5"
                style={{ color: SIGNATURE }}
                strokeWidth={2.2}
              />
            </span>
            <h3
              className="text-[17px] font-semibold text-foreground"
              style={{ letterSpacing: "-0.015em" }}
            >
              {FOUNDER_NOTE.greeting}
            </h3>
          </div>
          {FOUNDER_NOTE.paragraphs.map((p, i) => (
            <p
              key={i}
              className="text-[14px] leading-relaxed text-fg-2 mb-2 last:mb-0"
            >
              {p}
            </p>
          ))}
          <p className="text-[13px] text-fg-3 mt-3">{FOUNDER_NOTE.signature}</p>
          <p className="text-[12px] text-fg-4 mt-2">{FOUNDER_NOTE.community}</p>
          <div className="mt-4">
            <button
              type="button"
              className="inline-flex items-center gap-1.5 h-9 px-4 rounded-lg text-[13px] font-medium text-foreground transition-colors"
              style={{
                background: `oklch(from ${SIGNATURE} l c h / 0.12)`,
                color: SIGNATURE,
              }}
            >
              {FOUNDER_NOTE.ctaLabel}
              <ArrowUp className="w-3.5 h-3.5 rotate-180" strokeWidth={2.2} />
            </button>
          </div>
        </div>
        {onDismiss && (
          <button
            type="button"
            onClick={onDismiss}
            className="shrink-0 w-7 h-7 -mr-1 -mt-1 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
            aria-label="Dismiss welcome note"
          >
            <X className="w-3.5 h-3.5" strokeWidth={2} />
          </button>
        )}
      </div>
    </div>
  );
}

/* ── Progress dots (●●●○○○○) ── */

function SnProgressDots({ total, done }: { total: number; done: number }) {
  return (
    <div className="flex items-center gap-1" aria-hidden>
      {Array.from({ length: total }).map((_, i) => (
        <span
          key={i}
          className="block rounded-full transition-colors"
          style={{
            width: 6,
            height: 6,
            background:
              i < done
                ? SIGNATURE
                : "oklch(from var(--foreground) l c h / 0.18)",
          }}
        />
      ))}
    </div>
  );
}

/* ── Per-item row ── */

function SnItemRow({
  step,
  completed,
  locked,
  progressCurrent,
  showCta = true,
}: {
  step: SnStep;
  completed: boolean;
  locked: boolean;
  progressCurrent?: number;
  showCta?: boolean;
}) {
  const Icon = step.icon;
  const isProgressItem =
    typeof step.progressTarget === "number" &&
    typeof progressCurrent === "number" &&
    !completed &&
    !locked;

  const ctaArrow =
    step.ctaGlyph === "down" ? (
      <ArrowUp className="w-3 h-3 rotate-180" strokeWidth={2.2} />
    ) : step.ctaGlyph === "up" ? (
      <ArrowUp className="w-3 h-3" strokeWidth={2.2} />
    ) : step.ctaGlyph === "arrow" ? (
      <ArrowRight className="w-3 h-3" strokeWidth={2.2} />
    ) : null;

  return (
    <div
      className="flex items-center gap-3 py-2 px-2 -mx-2 rounded-lg transition-colors hover:bg-surface-2/60"
      style={{
        opacity: locked ? 0.55 : 1,
      }}
    >
      {/* Status icon */}
      <div
        className="shrink-0 w-7 h-7 rounded-full flex items-center justify-center"
        style={{
          background: completed
            ? `oklch(from ${SIGNATURE} l c h / 0.12)`
            : "oklch(from var(--foreground) l c h / 0.04)",
        }}
      >
        {completed ? (
          <Check
            className="w-3.5 h-3.5"
            strokeWidth={2.5}
            style={{ color: SIGNATURE }}
          />
        ) : locked ? (
          <Lock className="w-3 h-3 text-fg-4" strokeWidth={2} />
        ) : (
          <Icon className="w-3.5 h-3.5 text-fg-3" strokeWidth={1.8} />
        )}
      </div>

      {/* Title + description */}
      <div className="flex-1 min-w-0">
        <p
          className={`text-[13px] font-medium ${
            completed ? "text-fg-3" : "text-foreground"
          }`}
        >
          {completed ? step.completedTitle : step.title}
        </p>
        {!completed && (
          <p className="text-[12px] text-fg-4 mt-0.5 leading-snug">
            {locked ? "Unlocks after you dump 5 memories." : step.description}
          </p>
        )}
      </div>

      {/* Right rail — CTA / progress / done */}
      <div className="shrink-0 flex items-center gap-2">
        {isProgressItem && (
          <div className="flex items-center gap-2">
            <div
              className="w-20 h-1.5 rounded-full overflow-hidden"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.08)",
              }}
            >
              <div
                className="h-full rounded-full transition-all"
                style={{
                  width: `${((progressCurrent ?? 0) / (step.progressTarget ?? 1)) * 100}%`,
                  background: SIGNATURE,
                }}
              />
            </div>
            <span className="text-[11px] tabular-nums text-fg-3 font-medium">
              {progressCurrent}/{step.progressTarget}
            </span>
          </div>
        )}
        {!completed &&
          !locked &&
          !isProgressItem &&
          step.ctaLabel &&
          showCta && (
            <button
              type="button"
              className="inline-flex items-center gap-1 h-7 px-2.5 rounded-md text-[12px] font-medium text-fg-2 hover:text-foreground hover:bg-surface-3 transition-colors"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.05)",
              }}
            >
              {step.ctaLabel}
              {ctaArrow}
            </button>
          )}
      </div>
    </div>
  );
}

/* ── Checklist card (kind=checklist) — full card, progress aware ── */

function SnChecklistCard({
  completedIds,
  memoryCount = 0,
  onDismiss,
  onCollapse,
  celebrate = false,
}: {
  completedIds: Set<SnStepId>;
  memoryCount?: number;
  onDismiss?: () => void;
  onCollapse?: () => void;
  celebrate?: boolean;
}) {
  const doneRequired = SN_STEPS.filter(
    (s) => s.required && completedIds.has(s.id),
  ).length;
  const doneAll = completedIds.size;

  return (
    <div
      className="relative rounded-2xl overflow-hidden"
      style={{
        background: "var(--card)",
        boxShadow:
          "0 1px 0 oklch(from var(--foreground) l c h / 0.04), 0 8px 24px -12px oklch(from var(--foreground) l c h / 0.12)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      {/* Header */}
      <div className="flex items-center gap-3 px-5 pt-5 pb-3">
        <div className="flex-1 min-w-0">
          <h3 className="text-[14px] font-semibold text-foreground">
            Your first week
          </h3>
          <p className="text-[12px] text-fg-3 mt-0.5">
            {doneRequired === SN_REQUIRED_COUNT
              ? "All set — memax dreams nightly now."
              : `${doneAll} of ${SN_STEPS.length} done · ${doneRequired}/${SN_REQUIRED_COUNT} required`}
          </p>
        </div>
        <SnProgressDots total={SN_STEPS.length} done={doneAll} />
        {onCollapse && (
          <button
            type="button"
            onClick={onCollapse}
            className="shrink-0 w-7 h-7 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
            aria-label="Collapse checklist"
          >
            <Minus className="w-3.5 h-3.5" strokeWidth={2} />
          </button>
        )}
        {onDismiss && (
          <button
            type="button"
            onClick={onDismiss}
            className="shrink-0 w-7 h-7 -mr-1 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
            aria-label="Dismiss checklist"
          >
            <X className="w-3.5 h-3.5" strokeWidth={2} />
          </button>
        )}
      </div>

      {/* Hairline tone shift instead of a divider */}
      <div
        aria-hidden
        className="h-px mx-5"
        style={{ background: "oklch(from var(--foreground) l c h / 0.05)" }}
      />

      {/* Items */}
      <div className="px-5 py-2 relative">
        {SN_STEPS.map((step) => {
          const isCompleted = completedIds.has(step.id);
          const isLocked = !!step.lockedBy?.some(
            (dep) => !completedIds.has(dep),
          );
          const progressCurrent =
            step.id === "five_memories" ? Math.min(memoryCount, 5) : undefined;
          return (
            <SnItemRow
              key={step.id}
              step={step}
              completed={isCompleted}
              locked={isLocked}
              progressCurrent={progressCurrent}
            />
          );
        })}

        {/* Celebration pulse — overlay on the card when auto-resolve fires */}
        {celebrate && (
          <motion.div
            initial={{ opacity: 0, scale: 0.96 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.5, ease: [0.34, 1.56, 0.64, 1] }}
            className="absolute inset-0 flex flex-col items-center justify-center rounded-2xl"
            style={{
              background: "oklch(from var(--card) l c h / 0.92)",
              backdropFilter: "blur(8px)",
            }}
          >
            <motion.div
              animate={{ rotate: [0, 10, -10, 0] }}
              transition={{ duration: 1.5, ease: "easeInOut" }}
              className="text-[36px] mb-2"
            >
              ✨
            </motion.div>
            <p className="text-[16px] font-semibold text-foreground">
              You&apos;re all set up
            </p>
            <p className="text-[13px] text-fg-3 mt-1">
              memax dreams nightly now. See you tomorrow.
            </p>
          </motion.div>
        )}
      </div>
    </div>
  );
}

/* ── Checklist strip (collapsed form) ── */

function SnChecklistStrip({
  completedIds,
  onOpen,
}: {
  completedIds: Set<SnStepId>;
  onOpen: () => void;
}) {
  const doneAll = completedIds.size;
  return (
    <button
      type="button"
      onClick={onOpen}
      className="w-full rounded-xl px-4 py-2.5 flex items-center gap-3 hover:bg-surface-2/60 transition-colors"
      style={{
        background: "var(--card)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <span className="text-[13px] font-medium text-foreground">
        Your first week
      </span>
      <span className="text-[12px] text-fg-3">
        · {doneAll}/{SN_STEPS.length}
      </span>
      <SnProgressDots total={SN_STEPS.length} done={doneAll} />
      <span className="ml-auto text-[12px] text-fg-3 inline-flex items-center gap-1">
        Open
        <ArrowRight className="w-3 h-3" strokeWidth={2.2} />
      </span>
    </button>
  );
}

/* ── Demo wrappers ── */

function SnFounderNoteDemo() {
  const [dismissed, setDismissed] = useState(false);
  return (
    <div className="max-w-[620px] mx-auto">
      <AnimatePresence mode="wait">
        {!dismissed ? (
          <motion.div
            key="note"
            initial={{ opacity: 1 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: NORMAL, ease: EASE }}
          >
            <SnFounderNoteCard onDismiss={() => setDismissed(true)} />
          </motion.div>
        ) : (
          <motion.div
            key="empty"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="text-center py-8 text-fg-3 text-[13px]"
          >
            Dismissed. The row is now{" "}
            <span className="font-mono">status=dismissed</span> in the
            notifications table.
            <button
              type="button"
              onClick={() => setDismissed(false)}
              className="ml-2 inline-flex items-center gap-1 text-fg-2 hover:text-foreground"
            >
              <RotateCcw className="w-3 h-3" />
              Replay
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function SnChecklistStatesGrid() {
  const all = new Set<SnStepId>(SN_STEPS.map((s) => s.id));
  const threeDone = new Set<SnStepId>([
    "welcome",
    "connect_agent",
    "first_memory",
  ]);
  const allRequired = new Set<SnStepId>(
    SN_STEPS.filter((s) => s.required).map((s) => s.id),
  );

  return (
    <div className="grid grid-cols-1 gap-4">
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          State A — first visit · 0/7
        </p>
        <div className="max-w-[620px]">
          <SnChecklistCard completedIds={new Set()} memoryCount={0} />
        </div>
      </div>
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          State B — mid-flow · 3/7 (welcome + connect + memory done, 3/5
          progress)
        </p>
        <div className="max-w-[620px]">
          <SnChecklistCard completedIds={threeDone} memoryCount={3} />
        </div>
      </div>
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          State C — collapsed strip · 3/7
        </p>
        <div className="max-w-[620px]">
          <SnChecklistStrip completedIds={threeDone} onOpen={() => {}} />
        </div>
      </div>
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          State D — auto-resolve celebration (all required done)
        </p>
        <div className="max-w-[620px]">
          <SnChecklistCard
            completedIds={allRequired}
            memoryCount={5}
            celebrate
          />
        </div>
      </div>
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          State E — all 7 done (pre-unmount)
        </p>
        <div className="max-w-[620px]">
          <SnChecklistCard completedIds={all} memoryCount={5} />
        </div>
      </div>
    </div>
  );
}

/* ── /memories composition: HubHeader + founder + checklist + empty sections ── */

function SnFakeHubHeader() {
  return (
    <div className="flex items-center gap-3 px-1 py-2">
      <div
        className="w-9 h-9 rounded-lg flex items-center justify-center text-[16px]"
        style={{
          background: `oklch(from ${SIGNATURE} l c h / 0.14)`,
          color: SIGNATURE,
        }}
      >
        ◐
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-[15px] font-semibold text-foreground">Your brain</p>
        <p className="text-[12px] text-fg-3">
          Personal hub · 0 memories · 0 topics
        </p>
      </div>
    </div>
  );
}

function SnFakeRecentEmpty() {
  return (
    <div className="mt-4">
      <div className="flex items-center gap-2 mb-2 px-1">
        <Sparkles className="w-3.5 h-3.5 text-fg-3" />
        <span className="text-[13px] font-medium text-fg-2">Fresh Memory</span>
      </div>
      <div
        className="rounded-xl px-4 py-8 text-center text-[12px] text-fg-4"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.02)",
          border: "1px dashed oklch(from var(--foreground) l c h / 0.08)",
        }}
      >
        No memories yet. Dump something in the bar to get started.
      </div>
    </div>
  );
}

function SnFakeTopicsEmpty() {
  return (
    <div className="mt-4">
      <p className="text-[13px] font-medium text-fg-2 mb-2 px-1">Topics</p>
      <div className="grid grid-cols-3 gap-2">
        {[1, 2, 3].map((i) => (
          <div
            key={i}
            className="rounded-xl aspect-[3/2]"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.02)",
              border: "1px dashed oklch(from var(--foreground) l c h / 0.06)",
            }}
          />
        ))}
      </div>
      <p className="text-[11px] text-fg-4 mt-2 text-center">
        Topics will appear after your first dream run.
      </p>
    </div>
  );
}

function SnMemoriesCompositionDemo() {
  return (
    <div
      className="rounded-xl p-4 max-w-[640px] mx-auto"
      style={{
        background: "oklch(from var(--foreground) l c h / 0.015)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.04)",
      }}
    >
      <p className="text-[10px] font-mono text-fg-4 mb-3">/memories</p>
      <SnFakeHubHeader />
      <div className="space-y-3 mt-2">
        <SnFounderNoteCard onDismiss={undefined} />
        <SnChecklistCard completedIds={new Set()} memoryCount={0} />
      </div>
      <SnFakeRecentEmpty />
      <SnFakeTopicsEmpty />
    </div>
  );
}

/* ── Inbox variants (compact + expanded) ── */

function SnInboxCompact({ completedIds }: { completedIds: Set<SnStepId> }) {
  const doneAll = completedIds.size;
  return (
    <div
      className="rounded-lg px-3 py-2.5 flex items-center gap-3"
      style={{
        background: "var(--card)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <div
        className="w-7 h-7 rounded-md flex items-center justify-center shrink-0"
        style={{
          background: `oklch(from ${SIGNATURE} l c h / 0.12)`,
        }}
      >
        <Sparkles
          className="w-3.5 h-3.5"
          style={{ color: SIGNATURE }}
          strokeWidth={2}
        />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-[13px] font-medium text-foreground">
          Your first week
        </p>
        <p className="text-[11px] text-fg-3">
          {doneAll}/{SN_STEPS.length} · keep going to unlock dreams
        </p>
      </div>
      <SnProgressDots total={SN_STEPS.length} done={doneAll} />
      <ArrowRight className="w-3.5 h-3.5 text-fg-4" strokeWidth={2} />
    </div>
  );
}

function SnInboxVariantsDemo() {
  const threeDone = new Set<SnStepId>([
    "welcome",
    "connect_agent",
    "first_memory",
  ]);
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          Compact row (Needs action bucket)
        </p>
        <SnInboxCompact completedIds={threeDone} />
        <p className="text-[11px] text-fg-4 mt-2">
          Sits among other inbox rows. Click chevron → expand in place.
        </p>
      </div>
      <div>
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          Expand-in-place (same InboxRow shell)
        </p>
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="text-[11px] text-fg-3 hover:text-fg-2 mb-2 inline-flex items-center gap-1"
        >
          <RotateCcw className="w-3 h-3" /> Toggle
        </button>
        <AnimatePresence mode="wait" initial={false}>
          {expanded ? (
            <motion.div
              key="expanded"
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: NORMAL, ease: EASE }}
              style={{ overflow: "hidden" }}
            >
              <SnChecklistCard completedIds={threeDone} memoryCount={3} />
            </motion.div>
          ) : (
            <motion.div
              key="collapsed"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
            >
              <SnInboxCompact completedIds={threeDone} />
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}

/* ── Interactive full-lifecycle stepper ── */

type SnScene =
  | "first_visit"
  | "note_dismissed"
  | "one_memory"
  | "three_done"
  | "five_memories"
  | "collapsed"
  | "all_done"
  | "celebrating"
  | "resolved";

const SN_SCENES: { id: SnScene; label: string; hint: string }[] = [
  {
    id: "first_visit",
    label: "1 · First visit",
    hint: "signup → emit note + checklist",
  },
  {
    id: "note_dismissed",
    label: "2 · Note dismissed",
    hint: "status=dismissed on the system_notice",
  },
  {
    id: "one_memory",
    label: "3 · First memory",
    hint: "memory_count=1 → first_memory.completed_at",
  },
  {
    id: "three_done",
    label: "4 · Three done",
    hint: "welcome + connect + memory + ask",
  },
  {
    id: "five_memories",
    label: "5 · Five memories",
    hint: "five_memories.completed_at + first_dream unlocks",
  },
  {
    id: "collapsed",
    label: "6 · User collapses",
    hint: "client-only, strip form",
  },
  {
    id: "all_done",
    label: "7 · All required complete",
    hint: "connect+memory+ask+five_memories",
  },
  {
    id: "celebrating",
    label: "8 · Auto-resolve",
    hint: "server /resolve complete_all → applied_auto",
  },
  {
    id: "resolved",
    label: "9 · Unmounted",
    hint: "page returns to normal rhythm",
  },
];

function getCompletedForScene(scene: SnScene): Set<SnStepId> {
  switch (scene) {
    case "first_visit":
      return new Set();
    case "note_dismissed":
      return new Set();
    case "one_memory":
      return new Set(["welcome", "first_memory"]);
    case "three_done":
      return new Set(["welcome", "connect_agent", "first_memory", "first_ask"]);
    case "five_memories":
    case "collapsed":
      return new Set([
        "welcome",
        "connect_agent",
        "first_memory",
        "first_ask",
        "five_memories",
      ]);
    case "all_done":
    case "celebrating":
    case "resolved":
      return new Set([
        "welcome",
        "connect_agent",
        "first_memory",
        "first_ask",
        "five_memories",
      ]);
  }
}

function getMemoryCountForScene(scene: SnScene): number {
  switch (scene) {
    case "first_visit":
    case "note_dismissed":
      return 0;
    case "one_memory":
      return 1;
    case "three_done":
      return 3;
    default:
      return 5;
  }
}

function SnInteractiveLifecycle() {
  const [scene, setScene] = useState<SnScene>("first_visit");
  const completed = getCompletedForScene(scene);
  const memoryCount = getMemoryCountForScene(scene);
  const noteGone = scene !== "first_visit";
  const collapsed = scene === "collapsed";
  const celebrating = scene === "celebrating";
  const unmounted = scene === "resolved";

  return (
    <div>
      {/* Scene control rail */}
      <div className="flex flex-wrap gap-1.5 mb-4">
        {SN_SCENES.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => setScene(s.id)}
            className="inline-flex items-center h-7 px-2.5 rounded-md text-[11px] font-medium transition-colors"
            style={{
              background:
                scene === s.id
                  ? `oklch(from ${SIGNATURE} l c h / 0.14)`
                  : "oklch(from var(--foreground) l c h / 0.04)",
              color: scene === s.id ? SIGNATURE : "var(--fg-2)",
            }}
          >
            {s.label}
          </button>
        ))}
      </div>
      <p className="text-[11px] text-fg-4 mb-3 font-mono">
        {SN_SCENES.find((s) => s.id === scene)?.hint}
      </p>

      {/* Stage */}
      <div
        className="rounded-xl p-4 max-w-[640px] mx-auto min-h-[200px]"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.015)",
          border: "1px solid oklch(from var(--foreground) l c h / 0.04)",
        }}
      >
        <p className="text-[10px] font-mono text-fg-4 mb-3">/memories</p>
        <SnFakeHubHeader />

        <AnimatePresence mode="sync" initial={false}>
          {!noteGone && (
            <motion.div
              key="note"
              initial={{ opacity: 0, y: -6 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -6 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="mt-3"
            >
              <SnFounderNoteCard onDismiss={() => setScene("note_dismissed")} />
            </motion.div>
          )}
          {!unmounted && !collapsed && (
            <motion.div
              key="card"
              initial={{ opacity: 0, y: -6 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -6 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="mt-3"
            >
              <SnChecklistCard
                completedIds={completed}
                memoryCount={memoryCount}
                celebrate={celebrating}
                onCollapse={() => setScene("collapsed")}
                onDismiss={() => setScene("resolved")}
              />
            </motion.div>
          )}
          {collapsed && (
            <motion.div
              key="strip"
              initial={{ opacity: 0, y: -6 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -6 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="mt-3"
            >
              <SnChecklistStrip
                completedIds={completed}
                onOpen={() => setScene("five_memories")}
              />
            </motion.div>
          )}
        </AnimatePresence>

        {unmounted ? <SnFakeRecentEmpty /> : null}
        {unmounted ? <SnFakeTopicsEmpty /> : null}
      </div>
    </div>
  );
}

/* ── Mobile viewport ── */

function SnMobileDemo() {
  return (
    <div className="flex justify-center">
      <div
        className="rounded-[28px] p-3"
        style={{
          width: 360,
          background: "oklch(from var(--foreground) l c h / 0.04)",
          border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
        }}
      >
        <div
          className="rounded-[22px] p-3"
          style={{ background: "var(--background)" }}
        >
          <SnFakeHubHeader />
          <div className="space-y-3 mt-2">
            <SnFounderNoteCard />
            <SnChecklistCard
              completedIds={new Set(["welcome", "first_memory"])}
              memoryCount={2}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   MAIN EXPORT
   ═══════════════════════════════════════════════ */

export function LandingAndOnboardingSection() {
  return (
    <Section
      title="34. Landing + Onboarding"
      description="Landing page refresh (headlines, demo, CTAs) and post-login onboarding flow design."
    >
      {/* ── Part 1: Landing ── */}
      <div className="mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          Landing Page
        </h3>
      </div>

      <DemoCard label="Headline Options — pick one">
        <HeadlineOptionsDemo />
      </DemoCard>

      <DemoCard label="34-demo. Landing demo — Claude → Ziyang handoff (the killer story)">
        <p className="text-[12px] text-fg-2 mb-1">
          Based on a real moment (memax memory{" "}
          <span className="font-mono">aa92bcea</span>): Claude discovered a
          memax API gap while helping Jiahao, self-flagged as unverified, and
          pushed a structured issue directly to the engineering hub for Ziyang —
          who verified and shipped the fix 43 minutes later. No Jira. No Slack
          DM.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Why this is the killer demo: (1) it&apos;s real; (2) it shows AI as a{" "}
          <span className="text-fg-1">responsible teammate</span>, not just a
          retrieval tool; (3){" "}
          <span className="font-mono">source_agent: claude-ai</span> is the
          trust mechanism — memax makes AI contributions traceable and
          auditable, not a black box; (4) cross-agent + cross-person +
          zero-friction handoff is a product-driven story, not generic &ldquo;AI
          good&rdquo;.
        </p>
        <HandoffDemoCard />
      </DemoCard>

      <DemoCard label="34-features. Reinforcement strip — 4 quiet value blocks">
        <p className="text-[12px] text-fg-2 mb-1">
          Below the handoff demo, above the CTAs. Four distinct value props the
          demo alone doesn&apos;t communicate explicitly:{" "}
          <span className="text-fg-1">dump-from-anywhere</span> (surface
          coverage), <span className="text-fg-1">ask-from-anywhere</span> (the
          headline made literal),{" "}
          <span className="text-fg-1">self-organizing</span> (Dreams,
          memax-unique), <span className="text-fg-1">team memory</span> (the
          handoff payoff stated plainly).
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Quiet treatment — no card chrome, no borders, no shadows, just a tiny
          icon (h-4 w-4 text-fg-3) + 14px medium title + 13px fg-3 description.
          2×2 on mobile, 4-col on desktop. Reinforcement strip, not a feature
          wall. The Claude→Ziyang demo above does the storytelling; this row
          covers the audiences (non-tech, team buyers) the demo doesn&apos;t
          serve as well. We deliberately do NOT add a competing tagline below
          the headline — the current{" "}
          <span className="font-mono">sublineFull</span> ( &ldquo;Stop
          re-explaining yourself every time you switch tools.&rdquo;) is sharper
          because it has a single emotional hook (the pain). Two sublines fight
          each other; pick the pain hook over the feature list.
        </p>
        <LandingFeatureStrip />
      </DemoCard>

      <DemoCard label="CTA Options">
        <CTAOptionsDemo />
      </DemoCard>

      {/* ── Part 2: Onboarding ── */}
      <div className="mt-8 mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          Post-Login Onboarding
        </h3>
        <p className="text-[13px] text-fg-3 mt-1">
          Inline cards replace the empty dashboard. Steps animate away as
          completed — container morphing, not overlays.
        </p>
      </div>

      <DemoCard label="Screen 1 — Dump, Ask, Discover (shown once)">
        <HowItWorksDemo />
      </DemoCard>

      <DemoCard label="Screen 2 — How do you work? (interactive)">
        <PreferenceSelectorDemo />
      </DemoCard>

      <DemoCard label="Track: CLI-first (3 steps)">
        <CLITrackDemo />
      </DemoCard>

      <DemoCard label="Track: Agent-first (3 steps)">
        <AgentTrackDemo />
      </DemoCard>

      <DemoCard label="Track: Web-first (3 steps)">
        <WebTrackDemo />
      </DemoCard>

      <DemoCard label="Team Track (after personal, optional)">
        <TeamTrackDemo />
      </DemoCard>

      {/* ── Part 2.5: End-to-end interactive walkthrough ── */}
      <div className="mt-8 mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          End-to-end walkthrough
        </h3>
        <p className="text-[13px] text-fg-3 mt-1">
          Click through the full inline journey so you can see the state machine
          in motion. Welcome → preference → track → team prompt → completed.
          Each phase morphs in via{" "}
          <span className="font-mono">AnimatePresence</span> with the memax
          NORMAL + EASE spring.
        </p>
      </div>

      <DemoCard label="34-e2e. Interactive walkthrough — full new-user flow">
        <p className="text-[12px] text-fg-2 mb-1">
          The full first-run journey as a single connected demo. Stepper at the
          top tracks the 5 phases; each phase morphs in via framer-motion. Click
          &ldquo;Get started&rdquo; → pick a preference → see the matching track
          → optional team prompt → completion. Reset button replays from the
          top.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Reuses the existing track demos (
          <span className="font-mono">CLITrackDemo</span>,{" "}
          <span className="font-mono">AgentTrackDemo</span>,{" "}
          <span className="font-mono">WebTrackDemo</span>) inside the track
          phase, so this card is the connective tissue not a duplicate of those
          flows. Stepped modals (§34-modal-agents + §34-modal-hub) are demoed
          separately — in prod the &ldquo;Create a team hub&rdquo; button on the
          team prompt phase launches §34-modal-hub; here the demo advances
          directly to <span className="font-mono">completed</span> for clarity.
          Codex should wire the actual modal handoff at production
          implementation time. State machine columns:{" "}
          <span className="font-mono">onboarding_current_screen</span> tracks
          the active phase,{" "}
          <span className="font-mono">onboarding_completed_at</span> gets
          stamped at the completed phase, and{" "}
          <span className="font-mono">onboarding_dismissed_at</span> allows
          resume from any cursor (see §34-state).
        </p>
        <OnboardingE2EDemo />
      </DemoCard>

      {/* ── Part 3: Stepped modals (2 only) ── */}
      <div className="mt-8 mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          Stepped modals — 2 only
        </h3>
        <p className="text-[13px] text-fg-3 mt-1">
          Memax design DNA: container morph over overlays, inline wins by
          default. Modals are reserved for two flows that are settings-heavy +
          one-off + high-stakes + sequential: connecting agents, and
          creating/joining a team hub. Everything else is inline.
        </p>
      </div>

      <DemoCard label="34-modal-agents. Modal A — Connect your agents (4 steps, interactive)">
        <p className="text-[12px] text-fg-2 mb-1">
          Invoked from the inline Agent-track prompt OR from{" "}
          <span className="font-mono">Settings → Integrations</span>. Four
          steps: <span className="text-fg-1">Detect</span> installed agents →{" "}
          <span className="text-fg-1">Run</span> one command with live terminal
          progress → <span className="text-fg-1">Verify</span> MCP handshake per
          agent → <span className="text-fg-1">Done</span>.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Why modal: 5–6 agents to configure in sequence, user needs focus on
          terminal progress, one-time flow, high information density. Reuses the{" "}
          <span className="font-mono">settings-dialog</span> pattern already in
          memax (same chrome, Esc to close, same glass treatment). Click
          &ldquo;Copy &amp; run&rdquo; below to see the progress animation.
        </p>
        <AgentsModalDemo />
      </DemoCard>

      <DemoCard label="34-modal-hub. Modal B — Create or join a team hub (5 steps, interactive)">
        <p className="text-[12px] text-fg-2 mb-1">
          Invoked from the inline team prompt, the hub switcher &rarr;
          &ldquo;Create hub&rdquo;, OR from an invite link landing. Five steps:{" "}
          <span className="text-fg-1">Intent</span> (create vs join) →{" "}
          <span className="text-fg-1">Details</span> (name + description +
          visibility; or paste invite link) →{" "}
          <span className="text-fg-1">Invite</span> teammates (skippable) →{" "}
          <span className="text-fg-1">First push</span> to prime the hub
          (skippable) → <span className="text-fg-1">Done</span>.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Why modal: creating an org boundary is high-stakes (name is public,
          members have memory access), multi-field form needs focus, the
          first-push priming step needs user attention without dashboard
          distraction. Steps 3 and 4 are explicitly skippable — invites and
          first push can happen anytime.
        </p>
        <HubModalDemo />
      </DemoCard>

      {/* ── Part 4: Fast path (silent, no tutorial) ── */}
      <div className="mt-8 mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          Fast path — no tutorial
        </h3>
        <p className="text-[13px] text-fg-3 mt-1">
          The best onboarding for users who live in agents isn&apos;t a
          tutorial. It&apos;s the product working for them before they visit the
          web app. This is a visual reference for Codex — not a card the user
          ever sees.
        </p>
      </div>

      <DemoCard label="34-fast-path. Silent ambient onboarding (CLI → aha moment)">
        <p className="text-[12px] text-fg-2 mb-1">
          For developers who run{" "}
          <span className="font-mono">npx memax-cli setup</span> and close the
          terminal, the aha moment happens ambiently the next time they open an
          AI agent and the hook injects context. No guided tour required.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          The first web app visit is{" "}
          <span className="text-fg-1">voluntary and curious</span>, not
          instructed. The empty dashboard isn&apos;t empty — Dreams have already
          organized imported memories into topics. Web users still get the
          inline onboarding cards; CLI users skip them entirely (first-run
          detection checks if the user already has memories on login and hides
          the welcome card).
        </p>
        <FastPathTimelineDemo />
      </DemoCard>

      {/* ── Part 5: State machine ── */}
      <div className="mt-8 mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          State machine — first-run detection + recovery
        </h3>
      </div>

      <DemoCard label="34-state. Onboarding state machine (Codex implementation guide)">
        <p className="text-[12px] text-fg-2 mb-1">
          Backend tracks two timestamps on the user row:{" "}
          <span className="font-mono">onboarding_completed_at</span> and{" "}
          <span className="font-mono">onboarding_dismissed_at</span>, plus a
          resume cursor{" "}
          <span className="font-mono">onboarding_current_screen</span>. Frontend
          renders the appropriate surface based on which state the user is in.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Key recovery rule: dismissing is never permanent. The dashboard always
          renders real state, but a small &ldquo;Finish setup&rdquo; pill
          appears in the top bar whenever{" "}
          <span className="font-mono">
            dismissed_at IS NOT NULL AND completed_at IS NULL
          </span>
          . Clicking it resumes at the saved cursor. Completed users see
          &ldquo;Onboarding tour&rdquo; in Settings to replay.
        </p>
        <OnboardingStateMachineDemo />
      </DemoCard>

      {/* ── Part 6: Consolidated rules ── */}
      <DemoCard label="34-rules. Landing + Onboarding design rules">
        <div className="space-y-2 text-[12px] text-fg-2">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p>
              <span className="text-fg-1 font-medium">
                CLI, MCP, and web are equal-class surfaces.
              </span>{" "}
              Landing copy never frames memax as &ldquo;developer-first. &rdquo;
              The web app is a legitimate primary surface for non-technical
              users, not a viewer for the CLI. Lead with the universal value
              prop (&ldquo;Your memory. Every AI.&rdquo;) and let CLI / Agent /
              Web appear as equal choices in the onboarding preference picker.
              See memax memory <span className="font-mono">18d58b0e</span>.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Landing hero is the validated positioning.
              </span>{" "}
              Headline: &ldquo;Your memory. Every AI.&rdquo; Subline:
              &ldquo;Stop re-explaining yourself every time you switch
              tools.&rdquo; CTA: &ldquo;Give your AI a memory&rdquo; + the
              one-line install command. Single-screen, static, Linear/Vercel
              restraint. No purple, no gradients, no glow.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Landing demo is the Claude → Ziyang handoff story.
              </span>{" "}
              3-panel horizontal strip (stacked on mobile): (1) Jiahao asks
              Claude about team hub activity, Claude discovers an API gap; (2)
              Claude self-flags as{" "}
              <span className="font-mono">source_agent: claude-ai</span> +{" "}
              <span className="font-mono">needs-human-verification</span> and
              pushes a structured report to the hub; (3) Ziyang&apos;s Cursor
              surfaces it next morning, ships the fix 43 min later. Tagline:
              &ldquo;Your AI found a bug. Your teammate got the fix request. No
              one opened Jira.&rdquo; Shows AI as a responsible teammate +
              source_agent as the trust mechanism. See §34-demo.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Two-speed onboarding: fast path for CLI, inline for web.
              </span>{" "}
              CLI users who complete{" "}
              <span className="font-mono">npx memax-cli setup</span> skip the
              welcome/preference/track cards entirely — first-run detection
              hides them if the user already has imported memories on first
              login. Their aha moment is ambient (the next agent session shows
              context injected by the hook). Web-first users get the inline
              cards: welcome → preference picker → track steps → optional team
              prompt. See §34-fast-path + memax memory{" "}
              <span className="font-mono">00e29042</span>.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Inline cards by default, modals for settings-heavy flows only.
              </span>{" "}
              Memax design DNA: container morph over overlays. Inline wins by
              default because it lets users experiment alongside the guidance
              without feeling interrupted. Modals are reserved for flows that
              are{" "}
              <span className="text-fg-1">
                settings-heavy + one-off + high-stakes + sequential
              </span>
              . By those criteria, exactly{" "}
              <span className="text-fg-1">two modals</span> qualify:
              &ldquo;Connect your agents&rdquo; (§34-modal-agents) and
              &ldquo;Create or join a team hub&rdquo; (§34-modal-hub).
              Everything else — preference picker, track steps, dreams
              explanation, feature tooltips, &ldquo;connect one more
              agent&rdquo; nudges — stays inline.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Modals are invoked from multiple entry points.
              </span>{" "}
              The agents modal is reachable from the Agent track inline card AND
              from <span className="font-mono">Settings → Integrations</span>{" "}
              AND from a top-bar &ldquo;Connect more agents&rdquo; notification.
              The hub modal is reachable from the team prompt inline card AND
              from the hub switcher &ldquo;Create hub&rdquo; option AND from an
              invite link landing page. Both modals are full-flows each time —
              they don&apos;t depend on onboarding state.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Onboarding is replayable, not one-shot.
              </span>{" "}
              After completion, users can replay the inline sequence from{" "}
              <span className="font-mono">Settings → Onboarding tour</span>.
              Dismissing mid-flow is not permanent: a small &ldquo;Finish
              setup&rdquo; pill appears in the top bar until the user either
              resumes or explicitly marks setup complete. State machine: see
              §34-state.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">8.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Modal chrome matches settings-dialog pattern.
              </span>{" "}
              Both modals use <span className="font-mono">ModalShell</span>:
              glass panel (oklch 0.96 opacity + 24px blur + 140% saturate +
              border), <span className="font-mono">max-height 640px</span>,{" "}
              <span className="font-mono">overflow-y: auto</span> on body,
              sticky header with step dots + step count, sticky footer with
              Back/Skip/Continue. Step transitions use framer-motion slide +
              fade (NORMAL + EASE), reduced-motion fallback to opacity-only. Esc
              closes; click-outside closes. Matches the existing prod{" "}
              <span className="font-mono">settings-dialog.tsx</span> treatment.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* Notes */}
      <div className="text-[12px] text-fg-3 mt-2 space-y-1">
        <p>
          i18n: All strings need translation keys before production
          implementation. Kitchen has{" "}
          <span className="font-mono">// i18n:</span> TODO markers next to
          literal strings — Codex must replace them.
        </p>
        <p>Landing: packages/web/src/components/landing/landing-full.tsx</p>
        <p>
          Onboarding inline cards: new component in brain-view.tsx empty state
          (container morph, not overlay).
        </p>
        <p>
          Modals: new <span className="font-mono">OnboardingAgentsModal</span>{" "}
          and <span className="font-mono">OnboardingHubModal</span> built on the
          existing <span className="font-mono">settings-dialog</span> pattern.
          Reuse don&apos;t recreate the modal chrome.
        </p>
        <p>
          State machine: add{" "}
          <span className="font-mono">onboarding_completed_at</span>,{" "}
          <span className="font-mono">onboarding_dismissed_at</span>,{" "}
          <span className="font-mono">onboarding_current_screen</span> columns
          on the user table.
        </p>
      </div>

      {/* ── Part 4: Super-notif approach (plan 18 RFC) ── */}
      <div className="mt-10 mb-2">
        <h3 className="text-[15px] font-semibold text-fg-1 uppercase tracking-wider">
          Super-notif approach — plan 18 RFC
        </h3>
        <p className="text-[13px] text-fg-3 mt-1">
          Alternative to the preference-based track flow above. Onboarding is
          two notifications (founder note + checklist), rendered on{" "}
          <span className="font-mono">/memories</span> via a pinned region.
          Reuses plan 17&apos;s notifications framework — no new table, no
          onboarding-specific SDK surface. See{" "}
          <span className="font-mono">docs/plans/18-onboarding-journey.md</span>
          .
        </p>
      </div>

      <DemoCard label="34-sn-founder. Founder note — kind=system_notice, source_kind=onboarding_welcome">
        <p className="text-[12px] text-fg-2 mb-3">
          Pinned on <span className="font-mono">/memories</span> via{" "}
          <span className="font-mono">payload.pin_context=memories_hero</span>.
          One-shot per user — not versioned on restart ({" "}
          <span className="font-mono">notifications_source_unique</span>{" "}
          intentionally blocks replay). Dismissal is a single{" "}
          <span className="font-mono">
            POST /v1/notifications/{"{id}"}/dismiss
          </span>
          .
        </p>
        <SnFounderNoteDemo />
      </DemoCard>

      <DemoCard label="34-sn-checklist. Checklist — kind=checklist, all lifecycle states">
        <p className="text-[12px] text-fg-2 mb-3">
          Each state at rest. Required items (
          <span className="font-mono">
            connect_agent, first_memory, first_ask, five_memories
          </span>
          ) gate auto-resolve. Weakly-required items (
          <span className="font-mono">first_hub_invite, first_dream</span>) are
          discovery hooks — auto-resolve fires at 4/7 required.
        </p>
        <SnChecklistStatesGrid />
      </DemoCard>

      <DemoCard label="34-sn-memories. /memories composition — first visit, stacked pinned region">
        <p className="text-[12px] text-fg-2 mb-3">
          Single mount of{" "}
          <span className="font-mono">
            &lt;PinnedNotifications context=&quot;memories_hero&quot; /&gt;
          </span>{" "}
          between <span className="font-mono">&lt;HubHeader /&gt;</span> and{" "}
          <span className="font-mono">&lt;RecentSection /&gt;</span> in{" "}
          <span className="font-mono">topic-grid.tsx:219</span>. Renders zero,
          one, or both notifications depending on state. Page rhythm is
          identical to today once the region is empty.
        </p>
        <SnMemoriesCompositionDemo />
      </DemoCard>

      <DemoCard label="34-sn-inbox. Inbox row — compact + expand-in-place">
        <p className="text-[12px] text-fg-2 mb-3">
          Same component, different <span className="font-mono">variant</span>{" "}
          prop. Compact row sits in the{" "}
          <span className="font-mono">Needs action</span> bucket (checklist is a
          decision kind). Click the chevron to expand inside the existing{" "}
          <span className="font-mono">InboxRow</span> shell — no modal. Dismiss
          here or on <span className="font-mono">/memories</span> hits the same
          row; SSE propagates.
        </p>
        <SnInboxVariantsDemo />
      </DemoCard>

      <DemoCard label="34-sn-e2e. Interactive lifecycle — click through the 9 scenes">
        <p className="text-[12px] text-fg-2 mb-3">
          Full journey from signup to auto-resolve. Each scene maps to a
          notification state transition. Transitions use memax{" "}
          <span className="font-mono">NORMAL + EASE</span> motion tokens.
          Celebration pulse = scene 8; the page returns to its normal rhythm at
          scene 9 after unmount.
        </p>
        <SnInteractiveLifecycle />
      </DemoCard>

      <DemoCard label="34-sn-mobile. Mobile layout — 360px viewport">
        <p className="text-[12px] text-fg-2 mb-3">
          Items stack vertically; progress bar spans full width; CTA chips wrap
          below the description on narrow rows. Same card chrome as desktop —
          the container-morphing principle holds across breakpoints.
        </p>
        <SnMobileDemo />
      </DemoCard>

      <div className="text-[12px] text-fg-3 mt-2 space-y-1">
        <p>
          <span className="text-fg-1 font-medium">Copy lives in i18n.</span> All
          strings under <span className="font-mono">onboarding.welcome.*</span>{" "}
          and <span className="font-mono">onboarding.checklist.items.*</span>.
          See RFC §5.5 for the full key list.
        </p>
        <p>
          <span className="text-fg-1 font-medium">Motion.</span> All transitions
          use <span className="font-mono">NORMAL + EASE</span>; the celebration
          pulse uses a soft overshoot (
          <span className="font-mono">[0.34, 1.56, 0.64, 1]</span>) over 500ms.
        </p>
        <p>
          <span className="text-fg-1 font-medium">Design.</span> No dividers:
          the hairline between the card header and item list is a 5% foreground
          tone shift, not a <span className="font-mono">&lt;hr /&gt;</span>.
          Glass card surface, signature-accented dots, shadow-premium.
        </p>
      </div>
    </Section>
  );
}
