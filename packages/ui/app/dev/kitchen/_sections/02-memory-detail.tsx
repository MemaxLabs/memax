// Maps to: memories/[id]/page.tsx (detail view), provenance-strip.tsx, content-disclosure.tsx
// Memory detail redesign: borderless reading surface, provenance-first, AI distillation hero.
// North star: Notion/Linear/Bear/Craft — all use borderless detail. No card wrapping content.
// For memory LIST rows, see section 01. For recall rows, see section 24.
"use client";

import { useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import { Pill } from "@/components/pill";
import { Copy, ChevronLeft, ChevronRight, Terminal, Globe } from "lucide-react";

/* ── Provenance Strip Mock ── */

function ProvenanceStrip({
  agent,
  agentColor,
  agentIcon: AgentIcon,
  source,
  project,
  file,
  age,
  recallCount,
}: {
  agent?: string;
  agentColor?: string;
  agentIcon?: React.ElementType;
  source: string;
  project?: string;
  file?: string;
  age: string;
  recallCount: number;
}) {
  const Icon = AgentIcon || Globe;
  const label = agent || source;
  const iconColor = agentColor || "var(--fg-3)";

  const recallClass =
    recallCount >= 50
      ? "text-fg-1 font-medium"
      : recallCount >= 10
        ? "text-fg-2 font-medium"
        : "text-fg-3";

  return (
    <div className="flex items-center flex-wrap gap-x-1.5 gap-y-0.5 text-[13px]">
      <Icon className="h-3.5 w-3.5 shrink-0" style={{ color: iconColor }} />
      <span className={agent ? "text-fg-2 font-medium" : "text-fg-3"}>
        {label}
      </span>
      {agent && <span className="text-fg-3">captured</span>}
      {project && (
        <>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">{project}</span>
        </>
      )}
      {file && (
        <>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">{file}</span>
        </>
      )}
      <span className="text-fg-4">·</span>
      <span className="text-fg-3">{age}</span>
      {recallCount > 0 && (
        <>
          <span className="text-fg-4">·</span>
          <span className={`tabular-nums ${recallClass}`}>
            recalled {recallCount}×
          </span>
        </>
      )}
    </div>
  );
}

/* ── Content Disclosure Mock ── */

function ContentDisclosure({
  label,
  detail,
  children,
  defaultOpen = false,
}: {
  label: string;
  detail?: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1.5 text-[13px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors py-2"
      >
        <ChevronRight
          className="h-3 w-3 shrink-0 transition-transform"
          style={{
            transform: open ? "rotate(90deg)" : "rotate(0deg)",
            transitionTimingFunction: "var(--ease-spring)",
            transitionDuration: "0.25s",
          }}
        />
        <span>{label}</span>
        {detail && (
          <>
            <span className="text-fg-4">·</span>
            <span className="text-fg-4">{detail}</span>
          </>
        )}
      </button>
      <div
        style={{
          maxHeight: open ? "2000px" : "0px",
          overflow: "hidden",
          transition: "max-height 0.35s var(--ease-spring)",
        }}
      >
        {children}
      </div>
    </div>
  );
}

/* ── Topic Pill Mock (navigable) ── */

function TopicPill({ name, icon }: { name: string; icon: string }) {
  return (
    <div className="inline-flex items-center gap-1.5 bg-surface-1 border border-border/30 rounded-lg px-2.5 py-1">
      <span className="text-[13px]">{icon}</span>
      <span className="text-[13px] text-fg-2 hover:text-fg-1 cursor-pointer transition-colors">
        {name}
      </span>
      <button className="text-fg-4 hover:text-fg-2 cursor-pointer ml-0.5">
        ×
      </button>
    </div>
  );
}

/* ── Sibling Memory Row ── */

function SiblingRow({
  dot,
  title,
  age,
}: {
  dot: string;
  title: string;
  age: string;
}) {
  return (
    <div className="flex items-center gap-2 py-2 hover:bg-surface-1 transition-colors cursor-pointer -mx-2 px-2 rounded-lg">
      <span
        className="h-1.5 w-1.5 rounded-full shrink-0"
        style={{ background: dot }}
      />
      <span className="text-[13px] text-fg-2 truncate flex-1">{title}</span>
      <span className="text-[12px] text-fg-3 shrink-0 tabular-nums">{age}</span>
    </div>
  );
}

export function MemoryCardsSection() {
  return (
    <Section
      title="2. Memory Detail & Recall"
      description="Borderless reading surface. No card wrapping content — like Notion, Linear, Bear. Title → provenance → AI summary → collapsed raw content → meta footer → topic siblings. Whitespace and faint lines create structure."
    >
      {/* ════════════════════════════════════════════════
          CASE 1: Agent-captured memory WITH AI summary
          ════════════════════════════════════════════════ */}
      <DemoCard label="2a. Agent-captured, with AI summary (primary case)">
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <div className="px-6 sm:px-8 pt-8 pb-8 max-w-3xl mx-auto">
            {/* ── Header: back + title + actions ── */}
            <div className="flex items-start gap-3 mb-1">
              <ChevronLeft className="h-5 w-5 text-fg-3 shrink-0 mt-1 cursor-pointer hover:text-fg-2 transition-colors" />
              <div className="flex-1 min-w-0">
                <span
                  className="text-[21px] font-bold text-foreground leading-snug"
                  style={{ letterSpacing: "-0.02em" }}
                >
                  React Server Components Architecture
                </span>
              </div>
              <div className="flex items-center gap-2.5 shrink-0 mt-1.5">
                <Copy className="h-3.5 w-3.5 text-fg-4 hover:text-fg-2 cursor-pointer transition-colors" />
                <span className="text-[13px] text-fg-3 cursor-pointer hover:text-destructive/60 transition-colors">
                  Forget
                </span>
              </div>
            </div>

            {/* ── Provenance strip ── */}
            <div className="pl-8 mt-1.5">
              <ProvenanceStrip
                agent="Claude Code"
                agentColor="oklch(0.65 0.15 30)"
                agentIcon={Terminal}
                source="CLI"
                project="memax"
                file="ARCHITECTURE.md"
                age="2d ago"
                recallCount={47}
              />
            </div>

            {/* ── Topic pill ── */}
            <div className="pl-8 mt-2.5">
              <TopicPill name="Architecture" icon="🏗️" />
            </div>

            {/* ── AI Distillation — the hero ── */}
            <div className="pl-8 mt-8">
              <div className="flex items-center gap-1.5 mb-3">
                <span className="text-[12px]" style={{ color: SIGNATURE }}>
                  ✦
                </span>
                <span className="text-[13px] text-fg-3">memax summary</span>
              </div>
              <div className="text-[16px] text-fg-1 leading-[1.65] space-y-3">
                <p>
                  React Server Components render on the server and send HTML to
                  the client. <strong>Client Components</strong> hydrate and
                  become interactive. The boundary is the{" "}
                  <code className="text-[14px] bg-surface-1 px-1.5 py-0.5 rounded">
                    &quot;use client&quot;
                  </code>{" "}
                  directive.
                </p>
                <p>
                  This enables zero-JS for static content while maintaining
                  interactivity where needed. Data fetching happens at the
                  component level with async/await.
                </p>
              </div>
            </div>

            {/* ── Raw content — collapsed ── */}
            <div className="pl-8 mt-6 pt-5 border-t border-border/20">
              <ContentDisclosure label="Original content" detail="342 words">
                <div className="text-[15px] text-fg-2 leading-[1.65] mt-2 space-y-3">
                  <p>
                    Server Components are a new kind of Component that renders
                    ahead of time, before bundling, in an environment separate
                    from your client app or SSR server...
                  </p>
                  <p className="text-fg-3">
                    [Full raw content here. text-fg-2 = secondary to the
                    distillation above.]
                  </p>
                </div>
              </ContentDisclosure>
            </div>

            {/* ── Tags + classification ── */}
            <div className="pl-8 mt-5 pt-5 border-t border-border/20">
              <div className="flex flex-wrap gap-1.5 mb-3">
                <Pill variant="static">react</Pill>
                <Pill variant="static">architecture</Pill>
                <Pill variant="remove" onRemove={() => {}}>
                  server-components
                </Pill>
                <input
                  className="text-[13px] bg-transparent outline-none w-16 text-fg-3 placeholder:text-fg-4"
                  placeholder="+ tag"
                />
              </div>
              <div className="flex items-center gap-2 text-[13px]">
                <span
                  className="h-1.5 w-1.5 rounded-full shrink-0"
                  style={{ background: "#3b82f6" }}
                />
                <span className="text-fg-3">core</span>
                <span className="text-fg-4">·</span>
                <span className="text-fg-4">private</span>
              </div>
            </div>

            {/* ── Topic siblings ── */}
            <div className="pl-8 mt-5 pt-5 border-t border-border/20">
              <span className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold block mb-1.5">
                Also in Architecture
              </span>
              <SiblingRow
                dot="#10b981"
                title="Deployment strategy — blue-green with canary"
                age="5h"
              />
              <SiblingRow
                dot="#f59e0b"
                title="Auth middleware decision — JWT with refresh rotation"
                age="1d"
              />
              <SiblingRow
                dot="#3b82f6"
                title="Database migration patterns"
                age="3d"
              />
            </div>
          </div>
        </div>

        <div className="mt-3 space-y-0.5 text-[10px] text-fg-4 font-mono">
          <p>
            Entirely on --background. No Surface card. No border. No rounded
            container.
          </p>
          <p>
            Sections separated by border-t border-border/20 + mt-5 pt-5
            (whitespace + faint line)
          </p>
          <p>All content pl-8 (aligned past back chevron)</p>
          <p>
            Summary: text-fg-1 hero. Raw content: text-fg-2 collapsed.
            Tags/kind: bottom.
          </p>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 2: Web-captured, no summary (short memory)
          ════════════════════════════════════════════════ */}
      <DemoCard label="2b. Web-captured, no summary (short memory)">
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <div className="px-6 sm:px-8 pt-8 pb-8 max-w-3xl mx-auto">
            <div className="flex items-start gap-3 mb-1">
              <ChevronLeft className="h-5 w-5 text-fg-3 shrink-0 mt-1" />
              <div className="flex-1 min-w-0">
                <span
                  className="text-[21px] font-bold text-foreground leading-snug"
                  style={{ letterSpacing: "-0.02em" }}
                >
                  Quick note about lunch meeting
                </span>
              </div>
              <div className="flex items-center gap-2.5 shrink-0 mt-1.5">
                <Copy className="h-3.5 w-3.5 text-fg-4" />
                <span className="text-[13px] text-fg-3">Forget</span>
              </div>
            </div>

            <div className="pl-8 mt-1.5">
              <ProvenanceStrip source="you" age="3h ago" recallCount={0} />
            </div>

            {/* Content directly — no ✦, no disclosure */}
            <div className="pl-8 mt-8">
              <div className="text-[16px] text-fg-1 leading-[1.65]">
                <p>
                  Met with Sarah about the Q3 roadmap. Key takeaways: focus on
                  performance first, then features. Ship the caching layer
                  before the new dashboard.
                </p>
              </div>
            </div>

            {/* Tags + classification */}
            <div className="pl-8 mt-5 pt-5 border-t border-border/20">
              <div className="flex flex-wrap gap-1.5 mb-3">
                <Pill variant="static">meetings</Pill>
              </div>
              <div className="flex items-center gap-2 text-[13px]">
                <span
                  className="h-1.5 w-1.5 rounded-full shrink-0"
                  style={{ background: "#f97316" }}
                />
                <span className="text-fg-3">meetings</span>
                <span className="text-fg-4">·</span>
                <span className="text-fg-4">private</span>
              </div>
            </div>
          </div>
        </div>

        <p className="text-[10px] text-fg-4 font-mono mt-2">
          No summary → content at text-fg-1 directly. No ✦, no disclosure. No
          topic → no siblings. recalled 0× → hidden. Cleanest possible page.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 3: Processing state
          ════════════════════════════════════════════════ */}
      <DemoCard label="2c. Processing state (just captured)">
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <div className="px-6 sm:px-8 pt-8 pb-8 max-w-3xl mx-auto">
            <div className="flex items-start gap-3 mb-1">
              <ChevronLeft className="h-5 w-5 text-fg-3 shrink-0 mt-1" />
              <div className="flex-1 min-w-0">
                <span
                  className="text-[21px] font-bold text-foreground leading-snug"
                  style={{ letterSpacing: "-0.02em" }}
                >
                  Docker multi-stage builds
                </span>
              </div>
            </div>
            <div className="pl-8 mt-1.5">
              <ProvenanceStrip
                agent="Cursor"
                agentColor="oklch(0.6 0.14 170)"
                source="MCP"
                age="just now"
                recallCount={0}
              />
            </div>

            <div className="pl-8 mt-12 mb-8 flex flex-col items-center gap-3 text-center">
              <span
                className="text-[16px] state-slow-breathe"
                style={{ color: SIGNATURE }}
              >
                ✦
              </span>
              <p className="text-[15px] text-fg-2">Still remembering...</p>
              <p className="text-[12px] text-fg-3">
                memax is organizing this memory
              </p>
            </div>
          </div>
        </div>

        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Processing: ✦ slow-breathe centered on --background. Provenance
          visible. No Surface card needed.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 3b: Loading skeleton (initial fetch)
          ════════════════════════════════════════════════ */}
      <DemoCard label="2c-ii. Loading skeleton (borderless)">
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <div className="px-6 sm:px-8 pt-8 pb-8 max-w-3xl mx-auto">
            {/* Title skeleton */}
            <div className="flex items-start gap-3 mb-1">
              <div className="h-5 w-5 rounded bg-foreground/[0.07] shrink-0 mt-1" />
              <div className="flex-1 min-w-0 space-y-2">
                <div className="h-5 w-3/4 rounded bg-foreground/[0.07] animate-pulse" />
                <div
                  className="h-3 w-1/3 rounded bg-foreground/[0.07] animate-pulse"
                  style={{ animationDelay: "0.1s" }}
                />
              </div>
            </div>

            {/* Provenance skeleton */}
            <div className="pl-8 mt-3">
              <div className="flex items-center gap-2">
                <div className="h-3.5 w-3.5 rounded bg-foreground/[0.07] animate-pulse" />
                <div
                  className="h-3 w-24 rounded bg-foreground/[0.07] animate-pulse"
                  style={{ animationDelay: "0.15s" }}
                />
                <div
                  className="h-3 w-16 rounded bg-foreground/[0.07] animate-pulse"
                  style={{ animationDelay: "0.2s" }}
                />
              </div>
            </div>

            {/* Content skeleton */}
            <div className="pl-8 mt-8 space-y-3">
              <div
                className="h-4 w-full rounded bg-foreground/[0.07] animate-pulse"
                style={{ animationDelay: "0.1s" }}
              />
              <div
                className="h-4 w-5/6 rounded bg-foreground/[0.07] animate-pulse"
                style={{ animationDelay: "0.2s" }}
              />
              <div
                className="h-4 w-4/6 rounded bg-foreground/[0.07] animate-pulse"
                style={{ animationDelay: "0.3s" }}
              />
              <div
                className="h-4 w-3/4 rounded bg-foreground/[0.07] animate-pulse"
                style={{ animationDelay: "0.4s" }}
              />
            </div>

            {/* Footer skeleton */}
            <div className="pl-8 mt-8 pt-5 border-t border-border/20">
              <div className="flex gap-2">
                <div className="h-6 w-16 rounded-lg bg-foreground/[0.07] animate-pulse" />
                <div
                  className="h-6 w-20 rounded-lg bg-foreground/[0.07] animate-pulse"
                  style={{ animationDelay: "0.1s" }}
                />
              </div>
            </div>
          </div>
        </div>

        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Skeleton mirrors the borderless layout exactly: back button area →
          provenance line → content lines → footer tags. All on --background.
          Uses bg-foreground/[0.07] + animate-pulse with staggered delays.
          Transitions to loaded via animate-content-ready.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 3c: Error state
          ════════════════════════════════════════════════ */}
      <DemoCard label="2c-iii. Error state">
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <div className="px-6 sm:px-8 pt-8 pb-8 max-w-3xl mx-auto">
            <div className="flex items-start gap-3 mb-8">
              <ChevronLeft className="h-5 w-5 text-fg-3 shrink-0 mt-1 cursor-pointer" />
            </div>

            <div className="pl-8 flex flex-col items-center text-center py-8">
              <div
                className="h-2 w-2 rounded-full mb-3"
                style={{ background: "var(--destructive)" }}
              />
              <p className="text-[15px] text-fg-2 mb-1">
                Couldn&apos;t load this memory
              </p>
              <p className="text-[13px] text-fg-3 mb-4">
                Check your connection and try again
              </p>
              <button className="text-[13px] text-fg-2 hover:text-fg-1 cursor-pointer transition-colors px-3 py-1.5 rounded-lg hover:bg-surface-1">
                Retry
              </button>
            </div>
          </div>
        </div>

        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Error: back button visible (escape route). Centered message + retry on
          --background. Red dot (destructive), text-fg-2 message, text-fg-3
          hint, ghost retry button.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 3d: Loaded transition (skeleton → content)
          ════════════════════════════════════════════════ */}
      <DemoCard label="2c-iv. Loading → loaded transition">
        <p className="text-[12px] text-fg-2 mb-2">
          Skeleton → content uses{" "}
          <code className="text-[11px] font-mono text-fg-3 bg-surface-1 px-1 py-0.5 rounded">
            animate-content-ready
          </code>{" "}
          on the content wrapper. 0.15s opacity 0→1 + translateY 6px→0.
        </p>
        <div className="bg-surface-1 rounded-lg p-3 font-mono text-[11px] text-fg-2 leading-relaxed">
          <p className="text-fg-3">{"// Pattern:"}</p>
          <p>
            {"{"}
            <span className="text-fg-3">isLoading</span> ? (
          </p>
          <p className="pl-4">
            {"<"}
            <span className="text-fg-2">DetailSkeleton</span> /{">"}
          </p>
          <p>) : (</p>
          <p className="pl-4">
            {"<"}
            <span className="text-fg-2">div</span>{" "}
            <span className="text-fg-3">className</span>=
            <span className="text-fg-2">&quot;animate-content-ready&quot;</span>
            {">"}
          </p>
          <p className="pl-6 text-fg-3">{"// loaded content"}</p>
          <p className="pl-4">
            {"</"}
            <span className="text-fg-2">div</span>
            {">"}
          </p>
          <p>){"}"}</p>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          MANDATORY for every loading→loaded swap. No pop-in without animation.
          Skeleton shapes must match loaded layout exactly.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 3e: Sticky header (scroll compact)
          ════════════════════════════════════════════════ */}
      <DemoCard label="2c-v. Sticky header (appears on scroll)">
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <div className="px-5 sm:px-8 py-3">
            <div className="flex items-center gap-3 h-11 max-w-3xl mx-auto">
              <ChevronLeft className="h-5 w-5 text-fg-3 shrink-0" />
              <span
                className="h-2 w-2 rounded-full shrink-0"
                style={{ background: "#3b82f6" }}
              />
              <span
                className="text-[16px] font-semibold text-fg-1 truncate"
                style={{ letterSpacing: "-0.02em" }}
              >
                React Server Components Architecture
              </span>
              <span className="hidden sm:inline shrink-0 text-[13px] text-fg-3">
                2d ago
              </span>
              <span className="hidden sm:inline text-fg-4 text-[13px]">·</span>
              <span className="hidden sm:inline shrink-0 text-[13px] text-fg-2 font-medium tabular-nums">
                recalled 47×
              </span>
            </div>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          MemoryStickyHeader component. Fixed z-40, fades in on scroll via
          IntersectionObserver. mask-image dissolve at bottom. Shows recall
          count when ≥10 (activity signal in compact form). Extracted as
          reusable component.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 4: Forget flow
          ════════════════════════════════════════════════ */}
      <DemoCard label="2d. Forget flow — header morph">
        <div className="flex gap-4">
          <div className="flex-1 rounded-lg bg-surface-1 p-3">
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Default
            </p>
            <div className="flex items-center gap-2.5">
              <Copy className="h-3.5 w-3.5 text-fg-4" />
              <span className="text-[13px] text-fg-3 cursor-pointer">
                Forget
              </span>
            </div>
          </div>
          <div
            className="flex-1 rounded-lg p-3"
            style={{
              background: "oklch(from var(--destructive) l c h / 0.06)",
            }}
          >
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Confirming
            </p>
            <div className="flex items-center gap-2.5">
              <span className="text-[14px] text-destructive/60 font-medium cursor-pointer">
                Forget
              </span>
              <span className="text-[14px] text-fg-2 font-medium cursor-pointer">
                Keep
              </span>
            </div>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Forget left (destructive, requires movement), Keep right (safe, cursor
          already there). Navigation cancels.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          CASE 5: Mobile (same design — already borderless)
          ════════════════════════════════════════════════ */}
      <DemoCard label="2e. Mobile layout">
        <div
          className="rounded-xl overflow-hidden max-w-[320px] mx-auto"
          style={{ background: "var(--background)" }}
        >
          <div className="px-4 pt-5 pb-6">
            {/* Mobile: back on its own line */}
            <ChevronLeft className="h-5 w-5 text-fg-3 mb-2" />
            <div className="flex items-start justify-between gap-2 mb-1">
              <span
                className="text-[21px] font-bold text-foreground leading-snug flex-1"
                style={{ letterSpacing: "-0.02em" }}
              >
                React Server Components
              </span>
              <Copy className="h-3.5 w-3.5 text-fg-4 shrink-0 mt-1.5" />
            </div>

            <div className="mt-1.5">
              <ProvenanceStrip
                agent="Claude Code"
                agentColor="oklch(0.65 0.15 30)"
                agentIcon={Terminal}
                source="CLI"
                project="memax"
                age="2d ago"
                recallCount={47}
              />
            </div>

            <div className="mt-2.5">
              <TopicPill name="Architecture" icon="🏗️" />
            </div>

            {/* Summary */}
            <div className="mt-8">
              <div className="flex items-center gap-1.5 mb-3">
                <span className="text-[12px]" style={{ color: SIGNATURE }}>
                  ✦
                </span>
                <span className="text-[13px] text-fg-3">memax summary</span>
              </div>
              <p className="text-[16px] text-fg-1 leading-[1.65]">
                Server Components render on the server. Client Components
                hydrate on the client. The boundary is &quot;use client&quot;.
              </p>
            </div>

            {/* Collapsed content */}
            <div className="mt-5 pt-5 border-t border-border/20">
              <ContentDisclosure label="Original content" detail="342 words">
                <p className="text-[15px] text-fg-2 leading-[1.65] mt-2">
                  Server Components are a new kind of Component...
                </p>
              </ContentDisclosure>
            </div>

            {/* Tags */}
            <div className="mt-5 pt-5 border-t border-border/20">
              <div className="flex flex-wrap gap-1.5 mb-2">
                <Pill variant="static">react</Pill>
                <Pill variant="static">architecture</Pill>
              </div>
              <div className="flex items-center gap-2 text-[13px]">
                <span
                  className="h-1.5 w-1.5 rounded-full shrink-0"
                  style={{ background: "#3b82f6" }}
                />
                <span className="text-fg-3">core</span>
                <span className="text-fg-4">·</span>
                <span className="text-fg-4">private</span>
              </div>
            </div>
          </div>
        </div>

        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Mobile: identical design, not &quot;narrower desktop.&quot; Back above
          title. No pl-8 (no back button inline). Provenance wraps. Same
          borderless surface as desktop now.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          Activity signal scaling
          ════════════════════════════════════════════════ */}
      <DemoCard label="2f. Activity signal — recall count scaling">
        <div className="space-y-3">
          <div className="flex items-center gap-2 text-[13px]">
            <Terminal
              className="h-3.5 w-3.5"
              style={{ color: "oklch(0.65 0.15 30)" }}
            />
            <span className="text-fg-2 font-medium">Claude Code</span>
            <span className="text-fg-3">captured</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-3">2d ago</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-3 tabular-nums">recalled 3×</span>
          </div>
          <div className="flex items-center gap-2 text-[13px]">
            <Terminal
              className="h-3.5 w-3.5"
              style={{ color: "oklch(0.65 0.15 30)" }}
            />
            <span className="text-fg-2 font-medium">Claude Code</span>
            <span className="text-fg-3">captured</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-3">1w ago</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-2 font-medium tabular-nums">
              recalled 23×
            </span>
          </div>
          <div className="flex items-center gap-2 text-[13px]">
            <Terminal
              className="h-3.5 w-3.5"
              style={{ color: "oklch(0.65 0.15 30)" }}
            />
            <span className="text-fg-2 font-medium">Claude Code</span>
            <span className="text-fg-3">captured</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-3">1mo ago</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-1 font-medium tabular-nums">
              recalled 127×
            </span>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          0=hidden, 1-9=fg-3, 10-49=fg-2 medium, 50+=fg-1 medium. Typographic
          weight only, no color.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          Design rules
          ════════════════════════════════════════════════ */}
      <DemoCard label="Design rules — memory detail">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p className="text-fg-2">
              <span className="font-medium">Borderless.</span> No Surface card.
              Entire page is --background. Whitespace + faint border-border/20
              lines separate sections. Same on desktop and mobile.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p className="text-fg-2">
              <span className="font-medium">Provenance first.</span> After
              title: WHO captured, FROM WHERE, how often recalled. This is
              memax, not a note app.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p className="text-fg-2">
              <span className="font-medium">Summary = hero.</span> AI
              distillation gets text-fg-1. Raw content collapses at text-fg-2.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p className="text-fg-2">
              <span className="font-medium">No summary → no split.</span>{" "}
              Content renders directly. No ✦, no disclosure. Simplest case.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p className="text-fg-2">
              <span className="font-medium">Classification at bottom.</span>{" "}
              Kind + tags + boundary in footer. Top = provenance. Middle =
              content. Bottom = organization.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p className="text-fg-2">
              <span className="font-medium">Not a dead end.</span> Topic pill →
              /topics/[id]. Siblings show 2-3 neighbors. Tags filterable.
            </p>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
