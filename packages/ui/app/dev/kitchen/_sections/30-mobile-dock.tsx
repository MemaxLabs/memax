// Kitchen 30 — Mobile Dock Navigation
// Maps to: app/(app)/layout.tsx (mobile dock), brain tab, topics tab, discover tab
//
// ═══ LLM AGENT RULES — MOBILE DOCK ═══
//
// STRUCTURE: 3-tab dock — Brain (✦), Topics (📚), Discover (✦).
//   Settings stays in avatar (top-right), NOT a tab.
//   All 3 tabs ship together. /discover route has dream report + reviews.
//
// PAGE HIERARCHY (same on web + mobile):
//   "Recent" and "Your Topics" are SIBLING sections at the same level.
//   "Recent" title → RecentSection card (time-filtered memories).
//   "Your Topics" title + count → topic cards + inbox. Count covers topics+inbox only.
//   On mobile Brain tab: "Recent" title + feed. On mobile Topics tab: topics + inbox.
//   On desktop: both sections visible on /memories page.
//
// BRAIN TAB: Bar inline at bottom (above dock). "Recent" title + feed when idle.
//   Recall → full-screen sheet takeover (90vh). Dump → fullscreen compose on 3+ lines.
//   This is the default/home tab.
//
// TOPICS TAB: "Your Topics" title + topics grid + inbox. Bar hidden.
//   Tree panel accessible via Code icon trigger row.
//
// DISCOVER TAB: Dream reports, contradiction review, weekly insights.
//   The "memax helped you" surface. /discover route.
//
// DOCK PERSISTENCE: Dock is ALWAYS VISIBLE on mobile — every surface, every route.
//   Industry standard (iOS tab bar, Instagram, Twitter/X, WhatsApp): bottom nav
//   persists so users can always switch tabs, even from detail pages.
//   Only hides when virtual keyboard opens (opacity 0, pointer-events none).
//   Active tab: matchesRoute(pathname, route) = exact match OR startsWith(route + "/").
//     Boundary-safe — "/homepage" does NOT match "/home". Matches TABS array routes.
//   Routes with no parent tab (e.g. /settings): dock visible, no tab highlighted.
//   TAP ACTIVE TAB on detail page → navigates to tab root (iOS pattern).
//     On /memories/abc, tap Topics → goes to /memories. Already on root → no-op.
//
// DOCK STYLE: Simple rounded pill (rounded-full), same on all tabs. Icon + label.
//   Active: signature/foreground color + font-weight 600. Inactive: fg-4.
//   Bar sits above dock on Brain tab (64px + safe-area).
// ═══════════════════════════════════════════════
"use client";

import { useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import {
  BookOpen,
  Search,
  ArrowUp,
  FolderTree,
  Inbox,
  Clock,
  ChevronRight,
  Shield,
  Code,
  Rocket,
  FileText,
  Globe,
} from "lucide-react";

/* ── Nav Dots (Brain tab only) ──
 * Naked icons below the bar — no container, no glass.
 * Doesn't compete visually with the bar above.
 */

function NavDots({
  activeTab,
}: {
  activeTab: "brain" | "topics" | "discover";
}) {
  const tabs = [
    { id: "brain" as const, label: "Brain", star: true, signature: true },
    { id: "topics" as const, icon: BookOpen, label: "Topics" },
    { id: "discover" as const, label: "Discover", star: true },
  ];

  return (
    <div className="flex items-center justify-center gap-6 py-1.5">
      {tabs.map((tab) => {
        const TabIcon = tab.icon ?? BookOpen;
        return (
          <button
            key={tab.id}
            className="relative flex items-center justify-center min-h-9 min-w-9 cursor-pointer after:absolute after:-inset-1 after:content-['']"
          >
            {tab.star ? (
              <span
                className="text-[13px] leading-none"
                style={{
                  color:
                    tab.id === activeTab
                      ? tab.signature
                        ? SIGNATURE
                        : "var(--foreground)"
                      : "var(--fg-4)",
                }}
              >
                ✦
              </span>
            ) : (
              <TabIcon
                className="h-4 w-4"
                style={{
                  color:
                    tab.id === activeTab ? "var(--foreground)" : "var(--fg-4)",
                }}
              />
            )}
          </button>
        );
      })}
    </div>
  );
}

/* ── Dock (Topics / Discover tabs) ──
 *
 * Floating glass pill — only shown on non-Brain tabs.
 * Active tab label in center, inactive as icon-only sides.
 * Same bar visual language (--bar-bg, --bar-border, backdrop-blur).
 */

function Dock({ activeTab }: { activeTab: "brain" | "topics" | "discover" }) {
  const tabs = [
    { id: "brain" as const, label: "Brain", star: true, signature: true },
    { id: "topics" as const, icon: BookOpen, label: "Topics" },
    { id: "discover" as const, label: "Discover", star: true },
  ];

  const active = tabs.find((t) => t.id === activeTab)!;
  const sides = tabs.filter((t) => t.id !== activeTab);

  // Brain tab: bar is the primary element — show naked nav dots instead
  if (activeTab === "brain") {
    return <NavDots activeTab={activeTab} />;
  }

  return (
    <div
      className="mx-3 mb-1 flex items-center rounded-2xl"
      style={{
        border: "1px solid var(--bar-border, var(--border))",
        background: "var(--bar-bg, var(--card))",
        boxShadow: "0 1px 3px rgba(0,0,0,0.04), 0 4px 16px rgba(0,0,0,0.03)",
        backdropFilter: "blur(24px)",
        WebkitBackdropFilter: "blur(24px)",
        paddingBottom: "max(2px, env(safe-area-inset-bottom))",
      }}
    >
      {/* Left side tab */}
      {(() => {
        const s = sides[0];
        const LeftIcon = s.icon ?? BookOpen;
        return (
          <button className="relative flex items-center justify-center min-h-11 w-11 shrink-0 cursor-pointer">
            {s.star ? (
              <span className="text-[15px] leading-none text-fg-3">✦</span>
            ) : (
              <LeftIcon className="h-5 w-5 text-fg-3" />
            )}
          </button>
        );
      })()}

      {/* Center: active tab label */}
      {(() => {
        const ActiveIcon = active.icon ?? BookOpen;
        return (
          <div className="flex-1 flex items-center justify-center gap-2 py-2.5">
            {active.star ? (
              <span
                className="text-[14px] leading-none"
                style={{
                  color: active.signature ? SIGNATURE : "var(--foreground)",
                }}
              >
                ✦
              </span>
            ) : (
              <ActiveIcon className="h-4.5 w-4.5 text-foreground" />
            )}
            <span className="text-[14px] font-medium text-foreground">
              {active.label}
            </span>
          </div>
        );
      })()}

      {/* Right side tab */}
      {(() => {
        const s = sides[1];
        const RightIcon = s.icon ?? BookOpen;
        return (
          <button className="relative flex items-center justify-center min-h-11 w-11 shrink-0 cursor-pointer">
            {s.star ? (
              <span className="text-[15px] leading-none text-fg-3">✦</span>
            ) : (
              <RightIcon className="h-5 w-5 text-fg-3" />
            )}
          </button>
        );
      })()}
    </div>
  );
}

/* ── Mini Bar (inside phone frame) ── */

function MiniBar({
  placeholder,
  mode,
}: {
  placeholder: string;
  mode: "push" | "recall";
}) {
  return (
    <div
      className="flex items-center gap-2.5 px-4 py-2.5 rounded-2xl"
      style={{
        border: "1px solid var(--bar-border, var(--border))",
        background:
          mode === "recall"
            ? "color-mix(in oklch, var(--card) 97%, var(--signature))"
            : "var(--card)",
        boxShadow: "0 1px 3px rgba(0,0,0,0.04), 0 4px 16px rgba(0,0,0,0.03)",
      }}
    >
      <div
        className="h-5 w-5 rounded-full bg-surface-3"
        style={{ minWidth: 20 }}
      />
      <span className="text-[14px] text-fg-3 flex-1 truncate">
        {placeholder}
      </span>
      {mode === "push" ? (
        <div
          className="h-6 w-6 rounded-full flex items-center justify-center"
          style={{ background: "var(--foreground)" }}
        >
          <ArrowUp
            className="h-3.5 w-3.5"
            style={{ color: "var(--background)" }}
          />
        </div>
      ) : (
        <div
          className="h-6 w-6 rounded-full flex items-center justify-center"
          style={{ background: SIGNATURE }}
        >
          <span className="text-[14px] leading-none text-white">✦</span>
        </div>
      )}
    </div>
  );
}

/* ── Phone Frame ── */

function PhoneFrame({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-2">
      <div
        className="w-[320px] rounded-2xl overflow-hidden flex flex-col"
        style={{
          height: 568,
          border: "1px solid var(--border)",
          background: "var(--background)",
        }}
      >
        {children}
      </div>
      <p className="text-[11px] text-fg-3 text-center max-w-[320px]">{label}</p>
    </div>
  );
}

/* ── Memory Row (compact) ── */

function MiniRow({
  title,
  age,
  dot,
}: {
  title: string;
  age: string;
  dot?: string;
}) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 hover:bg-surface-1">
      <span
        className="h-1.5 w-1.5 rounded-full shrink-0"
        style={{ background: dot || "var(--fg-4)" }}
      />
      <span className="text-[13px] text-fg-1 font-medium truncate flex-1">
        {title}
      </span>
      <span className="text-[11px] text-fg-3 tabular-nums shrink-0">{age}</span>
    </div>
  );
}

/* ── Topic Pill (horizontal scroll) ── */

function TopicPill({
  icon: Icon,
  name,
  count,
}: {
  icon: React.ElementType;
  name: string;
  count: number;
}) {
  return (
    <div
      className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg shrink-0"
      style={{ border: "1px solid var(--border)", background: "var(--card)" }}
    >
      <Icon className="h-3.5 w-3.5 text-fg-3" />
      <span className="text-[12px] text-fg-2 font-medium whitespace-nowrap">
        {name}
      </span>
      <span className="text-[11px] text-fg-4">({count})</span>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════
 * MAIN SECTION
 * ═══════════════════════════════════════════════════════════════════ */

export function MobileDockSection() {
  const [activeTab, setActiveTab] = useState<"brain" | "topics" | "discover">(
    "brain",
  );

  return (
    <Section
      title="30. Mobile Dock Navigation"
      description="3-tab dock: Brain (✦ dump/ask + recent feed), Topics (📚 topics + inbox), Discover (✧ dreams + insights). Bar coexists above dock on Brain tab. Phase 1: Brain + Topics. Phase 2: add Discover."
    >
      {/* ════════════════════════════════════════════════
          A. All three tabs — side by side
          ════════════════════════════════════════════════ */}
      <DemoCard label="30a. Three tabs — Brain / Topics / Discover">
        <p className="text-[12px] text-fg-2 mb-4">
          Each tab has ONE job. Brain = interact (dump + ask). Topics = browse
          (topics + inbox). Discover = insight (dreams + reports). No overlap.
        </p>
        <div className="flex gap-4 overflow-x-auto pb-2">
          {/* Brain tab */}
          <PhoneFrame label="Brain ✦ — dump/ask + recent feed (default tab)">
            <div className="flex-1 flex flex-col">
              {/* Status bar placeholder */}
              <div className="h-10" />

              {/* Mode pills */}
              <div className="flex justify-center gap-2 mb-3">
                <span
                  className="px-3 py-1 rounded-full text-[12px] font-medium"
                  style={{
                    background: "var(--foreground)",
                    color: "var(--background)",
                  }}
                >
                  ↑ remember
                </span>
                <span
                  className="px-3 py-1 rounded-full text-[12px] font-medium text-fg-3"
                  style={{ border: "1px solid var(--border)" }}
                >
                  ✦ recall
                </span>
              </div>

              {/* Recent feed (idle state — content below bar) */}
              <div className="flex-1 overflow-hidden px-3">
                <div className="flex items-center gap-1.5 px-1 mb-2">
                  <Clock className="h-3 w-3 text-fg-3" />
                  <span className="text-[11px] text-fg-3 uppercase tracking-wider font-medium">
                    Recent
                  </span>
                </div>
                <div
                  className="rounded-xl overflow-hidden"
                  style={{
                    border: "1px solid var(--border)",
                    background: "var(--card)",
                  }}
                >
                  <MiniRow
                    title="API rate limit decision"
                    age="2m"
                    dot="#3b82f6"
                  />
                  <MiniRow
                    title="Deploy rollback procedure"
                    age="1h"
                    dot="#10b981"
                  />
                  <MiniRow
                    title="OAuth token refresh bug"
                    age="3h"
                    dot="#f59e0b"
                  />
                  <MiniRow title="Team standup notes" age="5h" dot="#94a3b8" />
                </div>
              </div>

              {/* Bar above dock */}
              <div className="px-3 pb-1.5">
                <MiniBar placeholder="Search or remember..." mode="push" />
              </div>
            </div>
            <Dock activeTab="brain" />
          </PhoneFrame>

          {/* Topics tab */}
          <PhoneFrame label="Topics 📚 — topics + inbox (bar hidden)">
            <div className="flex-1 flex flex-col">
              <div className="h-10" />

              {/* Page header */}
              <div className="px-4 mb-3">
                <h3 className="text-[17px] font-semibold text-foreground">
                  Your Topics
                </h3>
                <p className="text-[12px] text-fg-3">
                  14 memories &middot; 4 topics
                </p>
              </div>

              {/* Topic tree trigger */}
              <div className="px-3 mb-2">
                <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-surface-1">
                  <FolderTree className="h-3.5 w-3.5 text-fg-3" />
                  <span className="text-[13px] text-fg-2">Topics</span>
                  <ChevronRight className="h-3.5 w-3.5 text-fg-4 ml-auto" />
                </div>
              </div>

              {/* Topic pills (horizontal scroll) */}
              <div className="flex gap-2 overflow-x-auto px-3 pb-2 scrollbar-none">
                <TopicPill icon={Shield} name="Auth" count={23} />
                <TopicPill icon={Code} name="Go" count={31} />
                <TopicPill icon={Rocket} name="Deploy" count={47} />
                <TopicPill icon={Globe} name="Frontend" count={22} />
              </div>

              {/* Inbox section */}
              <div className="flex-1 px-3">
                <div
                  className="rounded-xl overflow-hidden"
                  style={{
                    border: "1px solid var(--border)",
                    background: "var(--card)",
                  }}
                >
                  <div className="flex items-center gap-1.5 px-4 pt-2.5 pb-1">
                    <Inbox className="h-3 w-3 text-fg-3" />
                    <span className="text-[11px] text-fg-3 uppercase tracking-wider font-medium flex-1">
                      Inbox
                    </span>
                    <span className="text-[11px] text-fg-4">3</span>
                  </div>
                  <MiniRow
                    title="New design token spec"
                    age="1h"
                    dot="#6366f1"
                  />
                  <MiniRow title="Pricing feedback" age="4h" dot="#fb7185" />
                  <MiniRow title="CI/CD pipeline fix" age="1d" dot="#84cc16" />
                </div>
              </div>

              {/* No bar — just empty space above dock */}
              <div className="h-3" />
            </div>
            <Dock activeTab="topics" />
          </PhoneFrame>

          {/* Discover tab */}
          <PhoneFrame label="Discover ✧ — dreams + insights (Phase 2)">
            <div className="flex-1 flex flex-col items-center justify-center px-6 text-center">
              <span
                className="text-[24px] mb-3 state-slow-breathe"
                style={{ color: SIGNATURE }}
              >
                ✦
              </span>
              <h3 className="text-[16px] font-semibold text-foreground mb-2">
                Dream Reports
              </h3>
              <p className="text-[13px] text-fg-3 leading-relaxed">
                Weekly insights, contradiction reviews, and knowledge
                consolidation reports. memax works while you sleep.
              </p>
              <div
                className="mt-4 px-3 py-1.5 rounded-full text-[12px] text-fg-3"
                style={{ border: "1px solid var(--border)" }}
              >
                Coming in Phase 2
              </div>
            </div>
            <Dock activeTab="discover" />
          </PhoneFrame>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          B. Brain tab — recall full-screen takeover
          ════════════════════════════════════════════════ */}
      <DemoCard label="30b. Brain tab — recall full-screen takeover (Perplexity pattern)">
        <p className="text-[12px] text-fg-2 mb-4">
          User taps bar → switches to recall → results sheet rises to 90vh. AI
          synthesis streams below results. The bar stays anchored at bottom as
          input. This is NOT the expand slot — it&apos;s a full-screen sheet.
        </p>
        <div className="flex gap-4 overflow-x-auto pb-2">
          {/* Before: idle */}
          <PhoneFrame label="Idle — recent feed + bar">
            <div className="flex-1 flex flex-col">
              <div className="h-10" />
              <div className="flex-1 px-3">
                <div className="flex items-center gap-1.5 px-1 mb-2">
                  <Clock className="h-3 w-3 text-fg-3" />
                  <span className="text-[11px] text-fg-3 uppercase tracking-wider font-medium">
                    Recent
                  </span>
                </div>
                <div
                  className="rounded-xl overflow-hidden"
                  style={{
                    border: "1px solid var(--border)",
                    background: "var(--card)",
                  }}
                >
                  <MiniRow
                    title="API rate limit decision"
                    age="2m"
                    dot="#3b82f6"
                  />
                  <MiniRow
                    title="Deploy rollback procedure"
                    age="1h"
                    dot="#10b981"
                  />
                  <MiniRow
                    title="OAuth token refresh bug"
                    age="3h"
                    dot="#f59e0b"
                  />
                </div>
              </div>
              <div className="px-3 pb-1.5">
                <MiniBar placeholder="Search or remember..." mode="push" />
              </div>
            </div>
            <Dock activeTab="brain" />
          </PhoneFrame>

          {/* After: recall results full-screen */}
          <PhoneFrame label="Recall — results fill 90vh above bar">
            <div className="flex-1 flex flex-col">
              {/* Results sheet (fills available space) */}
              <div className="flex-1 overflow-hidden flex flex-col">
                {/* Topic bridge header */}
                <div className="flex items-center gap-2 px-4 py-2.5">
                  <Shield className="h-3.5 w-3.5 text-fg-3" />
                  <span className="text-[13px] font-medium text-fg-2">
                    Auth & Security
                  </span>
                  <span className="text-[11px] text-fg-4">(3)</span>
                  <ChevronRight className="h-3 w-3 text-fg-4 ml-auto" />
                </div>
                <div className="border-t border-border/20">
                  <MiniRow
                    title="OAuth token refresh"
                    age="92%"
                    dot="#f59e0b"
                  />
                  <MiniRow
                    title="JWT validation strategy"
                    age="87%"
                    dot="#f59e0b"
                  />
                  <MiniRow title="API key rotation" age="81%" dot="#f59e0b" />
                </div>
                <div className="border-t border-border/30">
                  <div className="flex items-center gap-2 px-4 py-2.5">
                    <Rocket className="h-3.5 w-3.5 text-fg-3" />
                    <span className="text-[13px] font-medium text-fg-2">
                      Deployment
                    </span>
                    <span className="text-[11px] text-fg-4">(1)</span>
                    <ChevronRight className="h-3 w-3 text-fg-4 ml-auto" />
                  </div>
                </div>
                <div className="border-t border-border/20">
                  <MiniRow
                    title="Staging deploy guide"
                    age="78%"
                    dot="#10b981"
                  />
                </div>

                {/* AI synthesis */}
                <div className="border-t border-border/30 px-4 py-3">
                  <div className="flex items-start gap-2">
                    <span
                      className="text-[12px] mt-0.5"
                      style={{ color: SIGNATURE }}
                    >
                      ✦
                    </span>
                    <p className="text-[13px] text-fg-1 leading-relaxed">
                      Your API rate limit is set to 100 req/min for free tier
                      and 1000 req/min for Pro. The decision was made in the
                      April security review...
                    </p>
                  </div>
                </div>
              </div>

              {/* Bar anchored at bottom */}
              <div className="px-3 pb-1.5">
                <MiniBar
                  placeholder="What's our API rate limit?"
                  mode="recall"
                />
              </div>
            </div>
            <Dock activeTab="brain" />
          </PhoneFrame>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          C. Dock component spec
          ════════════════════════════════════════════════ */}
      <DemoCard label="30c. Dock component — spec">
        <div className="space-y-3">
          {/* Active states */}
          <div className="flex gap-4">
            <div className="flex-1">
              <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
                Brain active
              </p>
              <div
                className="rounded-xl overflow-hidden"
                style={{
                  border: "1px solid var(--border)",
                  background: "var(--card)",
                }}
              >
                <Dock activeTab="brain" />
              </div>
            </div>
            <div className="flex-1">
              <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
                Topics active
              </p>
              <div
                className="rounded-xl overflow-hidden"
                style={{
                  border: "1px solid var(--border)",
                  background: "var(--card)",
                }}
              >
                <Dock activeTab="topics" />
              </div>
            </div>
            <div className="flex-1">
              <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
                Discover active
              </p>
              <div
                className="rounded-xl overflow-hidden"
                style={{
                  border: "1px solid var(--border)",
                  background: "var(--card)",
                }}
              >
                <Dock activeTab="discover" />
              </div>
            </div>
          </div>

          <div className="space-y-1 text-[11px] text-fg-3 font-mono">
            <p>
              Brain tab: bar (glass pill) + naked nav dots below (no container).
            </p>
            <p>
              Other tabs: dock (glass pill) with active label center, icon-only
              sides.
            </p>
            <p>NEVER two glass pills stacked. Bar and dock alternate.</p>
            <p>
              Nav dots: fg-4 icons, active = foreground/signature. gap-6,
              py-1.5.
            </p>
            <p>Dock sides: icon-only, fg-3, w-11 min-h-11 (44px touch).</p>
            <p>Dock center: icon + label, 14px font-medium, foreground.</p>
            <p>
              Glass pill: mx-3, rounded-2xl, --bar-bg, --bar-border,
              backdrop-blur.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          D. Bar + Dock coexistence
          ════════════════════════════════════════════════ */}
      <DemoCard label="30d. Morphing behavior rules">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p className="text-fg-2">
              <span className="font-medium">Brain active:</span> Center morphs
              into input field. Typing, recall results, compose all happen
              through this element. Mode pills float above. Expand slot grows
              upward.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p className="text-fg-2">
              <span className="font-medium">Tab switch:</span> Center crossfades
              from input → label (or label → input). Side icons swap positions.
              Spring transition, FAST (0.15s).
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p className="text-fg-2">
              <span className="font-medium">Memory detail:</span> Entire dock
              hidden (slide down + fade). Full-screen reading. Back gesture
              restores dock.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p className="text-fg-2">
              <span className="font-medium">Recall (Brain):</span> Results
              expand upward from the dock (same expand slot pattern). Side tabs
              remain tappable — user can switch to Topics mid-recall.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p className="text-fg-2">
              <span className="font-medium">Batch toolbar:</span> Morphs into
              center area (replacing input or label). Side tabs stay. Same
              container.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          E. Implementation phases
          ════════════════════════════════════════════════ */}
      <DemoCard label="30e. Implementation phases">
        <div className="space-y-3 text-[12px]">
          <div>
            <p className="font-medium text-fg-2 mb-1">
              Phase 1 (now): 2 tabs — Brain + Topics
            </p>
            <ul className="space-y-0.5 text-fg-3 ml-4 list-disc">
              <li>Add MobileDock component (fixed bottom-0, 2 tabs)</li>
              <li>Brain tab: current /home with recent feed + bar</li>
              <li>Topics tab: current /memories grid</li>
              <li>
                Bar bottom offset: isMobile ? dock.height + safe-area : current
              </li>
              <li>Hide dock on /memories/[id] detail pages</li>
            </ul>
          </div>
          <div>
            <p className="font-medium text-fg-2 mb-1">
              Phase 2 (dream UI ships): add Discover tab
            </p>
            <ul className="space-y-0.5 text-fg-3 ml-4 list-disc">
              <li>Dream report production UI</li>
              <li>Contradiction review inbox</li>
              <li>Weekly insights surface</li>
              <li>Badge on Discover tab (&ldquo;3 new insights&rdquo;)</li>
            </ul>
          </div>
          <div>
            <p className="font-medium text-fg-2 mb-1">Phase 3: polish</p>
            <ul className="space-y-0.5 text-fg-3 ml-4 list-disc">
              <li>Tab switch micro-animation (active indicator spring)</li>
              <li>Haptic feedback on tab tap</li>
              <li>Brain tab recall full-screen sheet (Perplexity pattern)</li>
              <li>Fullscreen compose (3+ lines expand)</li>
            </ul>
          </div>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          Design rules
          ════════════════════════════════════════════════ */}
      <DemoCard label="Design rules — mobile dock">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p className="text-fg-2">
              <span className="font-medium">Each tab has ONE job.</span> Brain =
              interact. Topics = browse. Discover = insight. No overlap, no
              multi-purpose tabs.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p className="text-fg-2">
              <span className="font-medium">Brain is home.</span> Default tab on
              app open. Shows recent feed when idle — user always sees content,
              never an empty screen.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p className="text-fg-2">
              <span className="font-medium">Recall = full screen.</span> 90vh
              sheet on mobile. Not cramped in expand slot. Perplexity pattern:
              results fill the viewport.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Dock is navigation, bar is action.
              </span>{" "}
              Dock switches contexts. Bar captures input. They coexist but serve
              different purposes.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p className="text-fg-2">
              <span className="font-medium">Settings is NOT a tab.</span>{" "}
              Low-frequency action stays in avatar dropdown. Dock is for daily
              workflows only.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p className="text-fg-2">
              <span className="font-medium">Desktop unchanged.</span> Dock is
              mobile-only. Desktop keeps current bar-centric Spotlight model.
            </p>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
