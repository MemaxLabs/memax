// Maps to: ui/content-empty.tsx, all routes
"use client";

import { Clock, ArrowRight, Check, X } from "lucide-react";
import { ContentError } from "@memaxlabs/ui";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import { KindDot } from "@/components/kind-dot";

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * 9h — BrainView desktop badge (floating under the bar)
 *
 * Desktop /home is intentionally zen: just the bar + a tiny floating
 * badge showing one of three states. It is NOT a section card, NOT a
 * feed, NOT a summary. It's a single-line status indicator.
 *
 * States:
 *   loading     — MemaxLoader compact inline
 *   has-memories — "✦ {count} memories →" button + subtle tagline
 *   empty       — bold first-time hint + tagline + "Set up agents" link
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

function BrainViewBadgeLoading() {
  return (
    <div className="flex flex-col items-center gap-2 pointer-events-none">
      <div className="flex items-center gap-2">
        <span
          className="text-[12px] state-slow-breathe"
          style={{ color: SIGNATURE }}
        >
          ✦
        </span>
        <span className="text-[13px] text-fg-3">loading…</span>
      </div>
    </div>
  );
}

function BrainViewBadgeHasMemories({ count = 247 }: { count?: number }) {
  return (
    <div className="flex flex-col items-center gap-1.5">
      <button className="flex items-center gap-2 px-3 py-1.5 rounded-lg hover:bg-surface-2 transition-colors cursor-pointer">
        <span
          className="text-[12px] leading-none"
          style={{
            color: SIGNATURE,
            opacity: 0.55,
          }}
        >
          ✦
        </span>
        <span className="text-[15px] text-fg-2 tabular-nums">
          {count.toLocaleString()} memories
        </span>
        <ArrowRight className="h-3 w-3 text-fg-3" />
      </button>
      <span className="text-[13px] text-fg-3">
        type anything to remember · ? to ask
      </span>
    </div>
  );
}

function BrainViewBadgeEmpty() {
  return (
    <div className="flex flex-col items-center gap-1.5 text-center">
      <p className="text-[15px] text-fg-2">
        <span className="text-fg-1 font-medium">Type anything</span> to remember
        it.
      </p>
      <p className="text-[13px] text-fg-3">
        memax will organize it into topics as you build.
      </p>
      <button className="mt-1 text-[13px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors">
        Set up agents →
      </button>
    </div>
  );
}

/** Frame that mimics the bar + background for badge positioning.
 *  Desktop /home renders the badge at ~46vh from top, centered. */
function BrainViewBadgeFrame({
  children,
  label,
}: {
  children: React.ReactNode;
  label: string;
}) {
  return (
    <div className="space-y-2">
      <p className="text-[10px] uppercase tracking-wider text-fg-3">{label}</p>
      <div
        className="relative rounded-2xl overflow-hidden"
        style={{
          background: "var(--background)",
          border: "1px solid var(--border)",
          minHeight: 260,
        }}
      >
        {/* Centered badge */}
        <div
          className="absolute left-1/2 -translate-x-1/2"
          style={{ top: "38%" }}
        >
          {children}
        </div>
        {/* Mock bar at the bottom-ish */}
        <div
          className="absolute left-1/2 -translate-x-1/2 w-70 h-10 rounded-xl flex items-center px-4"
          style={{
            bottom: "18%",
            background: "var(--card)",
            border: "1px solid var(--bar-border, var(--border))",
            boxShadow: "var(--bar-shadow, 0 2px 12px oklch(0 0 0 / 0.04))",
          }}
        >
          <span className="text-[12px] text-fg-4">remember or ask…</span>
        </div>
      </div>
    </div>
  );
}

export function EmptyStatesSection() {
  return (
    <Section
      title="9. Empty States"
      description="Multiple tiers: first-time, filtered (centered + inside-card), transient, recall zero, and recall error. Every empty state is surface-specific — no generic fallback copy. For composed card-level empty handling see section 35 (Data Section Card)."
    >
      <div className="grid grid-cols-3 gap-4">
        {/* Tier 1: First-time */}
        <DemoCard label="First-time">
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <span
              className="text-[20px] leading-none mb-3"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <p className="text-[15px] font-semibold text-foreground mb-1">
              No memories yet
            </p>
            <p className="text-[13px] text-fg-2 mb-4">
              Type anything and press Enter to remember
            </p>
            <span
              className="text-[13px] font-medium cursor-pointer"
              style={{ color: SIGNATURE }}
            >
              Remember something &rarr;
            </span>
          </div>
        </DemoCard>

        {/* Tier 2: Filtered (centered — full-page filter) */}
        <DemoCard label="Filtered (centered, full-page)">
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <KindDot kind="code" size="md" className="mb-3" />
            <p className="text-[15px] font-semibold text-foreground mb-1">
              No memories in code
            </p>
            <button className="text-[13px] text-fg-3 hover:text-fg-2 transition-colors mt-2 cursor-pointer">
              Show all
            </button>
          </div>
        </DemoCard>

        {/* Tier 3: Transient */}
        <DemoCard label="Transient">
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <p className="text-[14px] text-fg-3">No reviews pending</p>
          </div>
        </DemoCard>
      </div>

      {/* Tier 4: Recall zero-results */}
      <DemoCard label="Recall zero-results">
        <div className="flex flex-col items-center gap-1 py-6">
          <p className="text-[15px] text-fg-3">No matching memories found.</p>
          <p className="text-[13px] text-fg-3">
            Try different words or rephrase your question.
          </p>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          i18n: recall.noResults, recall.noResultsHint. Shown inside bar expand
          slot when recall returns 0 results. No icon — text only.
        </p>
      </DemoCard>

      {/* ── 9e. Filtered empty INSIDE a mounted data card ── */}
      <DemoCard label="9e. Filtered empty — inside a mounted card (NOT a centered page)">
        <p className="text-[12px] text-fg-2 mb-4">
          Tier 2 above is the centered &ldquo;No memories in code&rdquo; variant
          — that&apos;s correct for a full-page filter (like the brain
          view&apos;s kind filter). But most filters live <em>inside</em> a
          SectionCard (Recent, Inbox, Topic detail) and the empty state must
          render inside the card&apos;s body — the header and filter chip stay
          visible so the user can clear the narrow filter without losing
          context.
        </p>
        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="flex items-center gap-2 px-4 pt-3 pb-2">
            <Clock className="h-3.5 w-3.5 text-fg-3 shrink-0" />
            <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
              Recent
            </span>
            <div className="flex-1" />
            <span className="text-[12px] text-fg-3">Past 12h · Claude</span>
          </div>
          <div className="border-t border-border/30 px-4 py-4">
            <p className="text-[14px] text-fg-2">
              No memories from Claude in the past 12 hours
            </p>
            <p className="text-[12px] text-fg-3 mt-1">
              Try widening the window or switching actor
            </p>
            <button className="mt-3 text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors">
              Clear filters
            </button>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-3">
          i18n: copy MUST echo the active filter values (&ldquo;in the past 12
          hours&rdquo;, &ldquo;from Claude&rdquo;) — not a generic &ldquo;No
          results.&rdquo; Clear-filter action uses the exact filter label so the
          user knows what they&apos;re undoing. Chinese translations must not
          repeat the card title (anti-pattern: card labeled 最近 showing
          &ldquo;最近 12h 没有结果&rdquo; — &ldquo;最近&rdquo; × 2).
        </p>
      </DemoCard>

      {/* ── 9f. Anti-pattern — return null on empty ── */}
      <DemoCard label="9f. Anti-pattern: `return null` when the list is empty">
        <p className="text-[12px] text-fg-2 mb-4">
          The single most common empty-state bug in memax: a component does{" "}
          <span className="font-mono">if (!data.length) return null</span> and
          the entire surface disappears. The result: layout jumps, users
          can&apos;t see where the section was, and they can&apos;t tell whether
          content was deleted or is just loading. Every such callsite should
          render a minimum viable shell instead.
        </p>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <div className="space-y-2">
            <p className="text-[10px] uppercase tracking-wider font-medium text-fg-3">
              ❌ Wrong — return null
            </p>
            <div
              className="rounded-xl border border-dashed border-destructive/40 p-6 flex items-center justify-center h-28"
              style={{
                background: "oklch(from var(--destructive) l c h / 0.04)",
              }}
            >
              <p className="text-[12px] text-fg-3 italic text-center">
                (component returns null —<br />
                section vanishes from the page)
              </p>
            </div>
          </div>
          <div className="space-y-2">
            <p className="text-[10px] uppercase tracking-wider font-medium text-fg-3">
              ✅ Right — mounted shell with empty body
            </p>
            <div
              className="rounded-xl"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex items-center gap-2 px-4 pt-3 pb-2">
                <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
                  Related memories
                </span>
              </div>
              <div className="border-t border-border/30 px-4 py-4">
                <p className="text-[14px] text-fg-2">Nothing related yet</p>
                <p className="text-[12px] text-fg-3 mt-1">
                  memax will link similar memories as they arrive.
                </p>
              </div>
            </div>
            <p className="text-[10px] text-fg-4">
              Same shape as any{" "}
              <span className="font-mono">&lt;DataSectionCard&gt;</span> empty
              body. Header stays so users know which section it is.
            </p>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-3">
          Historical audit note: keep this list current. Mounted detail sections
          like <span className="font-mono">topic-siblings.tsx</span> and{" "}
          <span className="font-mono">related-memories.tsx</span> should use
          section 35&apos;s{" "}
          <span className="font-mono">&lt;DataSectionCard&gt;</span>. Headless
          providers such as{" "}
          <span className="font-mono">dream-notification.tsx</span> and{" "}
          <span className="font-mono">processing-notification.tsx</span> are not
          card targets.
        </p>
      </DemoCard>

      {/* ── 9g. Bar recall error — currently missing in production ── */}
      <DemoCard label="9g. Bar recall error — NOT &ldquo;no results&rdquo;">
        <p className="text-[12px] text-fg-2 mb-4">
          The single biggest production risk in the audit: bar recall currently
          has <em>zero</em> error UI (
          <span className="font-mono">expand-search-results.tsx:269-292</span>)
          — a network failure falls through to &ldquo;No matching memories
          found.&rdquo; Users think their memories are gone. Recall is
          memax&apos;s core value prop. Fix: use{" "}
          <span className="font-mono">&lt;ContentError compact&gt;</span> inside
          the bar expand slot — red pulsing dot + specific message + named retry
          link. No <span className="font-mono">RotateCw</span> icon, no
          SIGNATURE color — both are reserved for memax AI working, not user
          retry (design law 4).
        </p>
        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <ContentError
            compact
            message="memax couldn't reach the memory server — your memories are safe."
            retryLabel="Retry recall"
            onRetry={() => {}}
          />
        </div>
        <p className="text-[10px] text-fg-4 mt-3">
          i18n (NEW): recall.errorTitle, recall.retry. Must differ from
          zero-results copy. Retry label is specific (&ldquo;Retry
          recall,&rdquo; not &ldquo;Try again&rdquo;). Error state must never
          fall through to the zero-results branch.
        </p>
      </DemoCard>

      {/* ── 9h. BrainView badge — desktop /home floating status ── */}
      <DemoCard label="9h. BrainView badge — desktop /home three-state morph">
        <p className="text-[12px] text-fg-2 mb-3">
          Desktop <span className="font-mono">/home</span> is intentionally zen:
          just the bar and a single floating badge under it. Not a section card,
          not a feed, not a summary — one line of status. Three states morph in
          place with a crossfade when data arrives. This is the <em>only</em>{" "}
          surface in the app that doesn&apos;t use{" "}
          <span className="font-mono">DataSectionCard</span>, because it
          isn&apos;t a section.
        </p>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
          <BrainViewBadgeFrame label="loading (first fetch)">
            <BrainViewBadgeLoading />
          </BrainViewBadgeFrame>
          <BrainViewBadgeFrame label="has memories (returning user)">
            <BrainViewBadgeHasMemories count={247} />
          </BrainViewBadgeFrame>
          <BrainViewBadgeFrame label="empty (first-time user)">
            <BrainViewBadgeEmpty />
          </BrainViewBadgeFrame>
        </div>
        <div className="mt-3 text-[11px] text-fg-3 space-y-1">
          <p>
            <span className="text-fg-2">Position:</span>{" "}
            <span className="font-mono">top: 46vh + 44px</span>, centered
            horizontally. Badge sits above the bar at roughly the golden-ratio
            point of the viewport.
          </p>
          <p>
            <span className="text-fg-2">Transitions:</span>{" "}
            <span className="font-mono">AnimatePresence</span> keyed on state
            string. Crossfade with ~220ms duration, spring easing. No morph
            between states — they&apos;re distinct enough to deserve their own
            frames.
          </p>
          <p>
            <span className="text-fg-2">Why not use DataSectionCard here:</span>{" "}
            desktop /home has no content list, no header, no trailing slot, no
            filter. It&apos;s a bar-centric surface. Forcing it through a
            section-card primitive would invent chrome that the surface
            doesn&apos;t want.
          </p>
          <p className="text-fg-4">
            Mobile /home renders{" "}
            <span className="font-mono">RecentSection</span> instead of this
            badge — mobile has no /topics tab, so the feed lives on /home. That
            path goes through DataSectionCard like every other section.
          </p>
        </div>
      </DemoCard>

      {/* ── 9i. Systematic return-null audit table ── */}
      <DemoCard label="9i. return-null audit — all known violations + migration status">
        <p className="text-[12px] text-fg-2 mb-3">
          Every file in the web package that ships{" "}
          <span className="font-mono">return null</span> when its data list is
          empty. Each row should migrate to{" "}
          <span className="font-mono">&lt;DataSectionCard&gt;</span> from
          section 35 with an appropriate empty body. The anti-pattern is
          documented in 9f above; this table tracks the cleanup.
        </p>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-[12px]">
            <thead className="text-fg-3 border-b border-border/40">
              <tr>
                <th className="py-2 pr-3 text-left font-medium">File</th>
                <th className="py-2 pr-3 text-left font-medium">Surface</th>
                <th className="py-2 pr-3 text-left font-medium">
                  Empty copy suggestion
                </th>
                <th className="py-2 pr-3 text-left font-medium">Priority</th>
                <th className="py-2 text-left font-medium">Status</th>
              </tr>
            </thead>
            <tbody className="text-fg-2">
              {[
                {
                  file: "topic-pills.tsx:27",
                  surface: "Memory detail → topic pills",
                  copy: "Not assigned to any topic yet.",
                  prio: "medium",
                  status: "pending",
                },
                {
                  file: "topic-siblings.tsx:34",
                  surface: "Topic detail → related topics",
                  copy: "No sibling topics yet.",
                  prio: "low",
                  status: "pending",
                },
                {
                  file: "related-memories.tsx:24",
                  surface: "Memory detail → related",
                  copy: "memax will link similar memories as they arrive.",
                  prio: "medium",
                  status: "pending",
                },
                {
                  file: "agent-configs-section.tsx:72",
                  surface: "Settings → agents",
                  copy: "No agent configs synced yet. Run memax setup to link your agents.",
                  prio: "high",
                  status: "pending",
                },
                {
                  file: "hub-management-view.tsx:75",
                  surface: "Settings → hub management",
                  copy: "You're the only member so far. Invite teammates to collaborate.",
                  prio: "high",
                  status: "pending",
                },
                {
                  file: "dream-notification.tsx:18",
                  surface: "Bar notification (dream)",
                  copy: "(being deleted — dream flow rewrite)",
                  prio: "n/a",
                  status: "delete",
                },
                {
                  file: "processing-notification.tsx:42",
                  surface: "Bar notification (processing)",
                  copy: "(migrate to bar notification morph — section 10b)",
                  prio: "medium",
                  status: "pending",
                },
              ].map((row) => (
                <tr
                  key={row.file}
                  className="border-b border-border/20 align-top"
                >
                  <td className="py-2 pr-3 font-mono text-fg-3">{row.file}</td>
                  <td className="py-2 pr-3">{row.surface}</td>
                  <td className="py-2 pr-3 text-fg-2">{row.copy}</td>
                  <td className="py-2 pr-3">
                    <span
                      className="text-[11px] px-1.5 py-0.5 rounded tabular-nums"
                      style={{
                        background:
                          row.prio === "high"
                            ? "oklch(0.65 0.15 25 / 0.1)"
                            : row.prio === "medium"
                              ? "oklch(0.68 0.12 65 / 0.1)"
                              : "oklch(from var(--foreground) l c h / 0.06)",
                        color:
                          row.prio === "high"
                            ? "oklch(0.55 0.16 25)"
                            : row.prio === "medium"
                              ? "oklch(0.55 0.14 65)"
                              : "var(--fg-3)",
                      }}
                    >
                      {row.prio}
                    </span>
                  </td>
                  <td className="py-2">
                    <span className="flex items-center gap-1.5 text-[11px]">
                      {row.status === "pending" ? (
                        <>
                          <X className="h-3 w-3 text-fg-4" />
                          <span className="text-fg-3">pending</span>
                        </>
                      ) : row.status === "delete" ? (
                        <>
                          <X
                            className="h-3 w-3"
                            style={{ color: "oklch(0.6 0.18 25)" }}
                          />
                          <span style={{ color: "oklch(0.55 0.15 25)" }}>
                            delete
                          </span>
                        </>
                      ) : (
                        <>
                          <Check
                            className="h-3 w-3"
                            style={{ color: "oklch(0.6 0.18 145)" }}
                          />
                          <span style={{ color: "oklch(0.5 0.15 145)" }}>
                            done
                          </span>
                        </>
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="mt-3 text-[11px] text-fg-3 space-y-1">
          <p>
            <span className="text-fg-2">Migration pattern:</span> replace the
            top-level{" "}
            <span className="font-mono">if (!data.length) return null</span>{" "}
            with a <span className="font-mono">DataSectionCard</span> wrapper
            whose <span className="font-mono">phase</span> prop transitions
            through loading → empty → loaded based on the same data source. The
            section header always renders; the empty body uses the suggested
            copy above.
          </p>
          <p>
            <span className="text-fg-2">Copy rule:</span> never generic. Each
            empty copy should tell the user something specific about this
            surface — what would appear here, what they can do next, or what
            they&apos;re waiting on. &ldquo;No data&rdquo; or &ldquo;Nothing
            yet&rdquo; is not enough.
          </p>
        </div>
      </DemoCard>

      <div className="text-[12px] text-fg-3 mt-2">
        <p>
          i18n: empty.firstTime.title, empty.firstTime.subtitle,
          empty.firstTime.cta, empty.filtered.title, empty.filtered.cta,
          empty.transient, recall.noResults, recall.noResultsHint,
          recall.errorTitle, recall.retry, brainView.empty.hint,
          brainView.empty.tagline, brainView.empty.setupHint.
        </p>
      </div>
    </Section>
  );
}
