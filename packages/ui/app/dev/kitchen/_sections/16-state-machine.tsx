// Maps to: ContentState<T> discriminated union pattern
// ui/ components: state-indicator.tsx, agents.ts (ActivityStatus)
"use client";

import { Section, DemoCard, SIGNATURE } from "../_shared";

function StateTransitionDiagram() {
  return (
    <div className="font-mono text-[13px] text-fg-2 leading-relaxed">
      <pre className="overflow-x-auto">
        {`  LOADING ──→ LOADED ──→ UPDATING ──→ LOADED
    │            │                        │
    │            ├──→ DELETING ──→ (removed)
    │            │
    ├──→ EMPTY   ├──→ ERROR
    │            │         │
    └──→ ERROR   └─────────┘ (retry → LOADING)

  Separate track:
  LOADED ──→ PROCESSING ──→ LOADED (auto-poll)`}
      </pre>
    </div>
  );
}

/* ── Activity Dot — reusable connection/liveness indicator ── */

type ActivityStatus = "active" | "idle" | "inactive" | "error";

const ACTIVITY_COLORS: Record<ActivityStatus, string> = {
  active: "oklch(0.72 0.19 145)", // green
  idle: "oklch(0.75 0.15 85)", // amber
  inactive: "var(--fg-4)", // muted
  error: "var(--destructive)", // red
};

function ActivityDot({
  status,
  size = "md",
}: {
  status: ActivityStatus;
  size?: "sm" | "md" | "lg";
}) {
  const sizes = { sm: "h-1.5 w-1.5", md: "h-2 w-2", lg: "h-2.5 w-2.5" };
  const s = sizes[size];
  const color = ACTIVITY_COLORS[status];

  return (
    <span className={`relative flex ${s}`}>
      {status === "active" && (
        <span
          className={`absolute inline-flex h-full w-full rounded-full opacity-50 animate-ping`}
          style={{ backgroundColor: color }}
        />
      )}
      <span
        className={`relative inline-flex ${s} rounded-full`}
        style={{ backgroundColor: color }}
      />
    </span>
  );
}

export function StateMachineSection() {
  return (
    <Section
      title="16. State Machine"
      description="Content states + activity indicators. No ambiguous states."
    >
      {/* Content state transitions */}
      <DemoCard label="16a. Content state transitions">
        <StateTransitionDiagram />
      </DemoCard>

      {/* Content state dots */}
      <div className="grid grid-cols-7 gap-2 mb-6">
        {(
          [
            ["Loading", "var(--muted-foreground)"],
            ["Empty", "var(--muted-foreground)"],
            ["Loaded", "var(--foreground)"],
            ["Updating", "var(--foreground)"],
            ["Deleting", "var(--destructive)"],
            ["Error", "var(--destructive)"],
            ["Processing", SIGNATURE],
          ] as const
        ).map(([name, color]) => (
          <div
            key={name}
            className="border border-border/40 rounded-lg p-2 text-center"
          >
            <div
              className="h-1.5 w-1.5 rounded-full mx-auto mb-1.5"
              style={{ backgroundColor: color }}
            />
            <span className="text-[10px] text-fg-2">{name}</span>
          </div>
        ))}
      </div>

      {/* Activity status dots — connection/liveness */}
      <DemoCard label="16b. Activity Status Dots">
        <p className="text-[13px] text-fg-2 mb-4">
          Reusable liveness indicator for agents, services, connections. Derived
          from timestamps (last_used, last_seen) — no polling, no websocket.
          Green pulses to draw attention; others are static.
        </p>

        {/* Size variants */}
        <div className="space-y-4">
          {/* All statuses at each size */}
          {(["sm", "md", "lg"] as const).map((size) => (
            <div key={size} className="flex items-center gap-6">
              <span className="text-[11px] text-fg-3 font-mono w-6">
                {size}
              </span>
              {(
                ["active", "idle", "inactive", "error"] as ActivityStatus[]
              ).map((status) => (
                <div key={status} className="flex items-center gap-2">
                  <ActivityDot status={status} size={size} />
                  <span className="text-[12px] text-fg-2 capitalize">
                    {status}
                  </span>
                </div>
              ))}
            </div>
          ))}
        </div>

        {/* Usage examples */}
        <div className="mt-5 pt-4 border-t border-border/10 space-y-2.5">
          <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-2">
            Usage
          </p>

          {/* Agent row example */}
          <div className="flex items-center gap-3 rounded-xl bg-surface-1 px-3 py-2.5">
            <div
              className="flex items-center justify-center h-8 w-8 rounded-lg shrink-0"
              style={{
                background: "oklch(from oklch(0.65 0.15 30) l c h / 0.12)",
              }}
            >
              <span
                className="text-[14px]"
                style={{ color: "oklch(0.65 0.15 30)" }}
              >
                &gt;_
              </span>
            </div>
            <span className="text-[14px] text-fg-1 font-medium">
              Claude Code
            </span>
            <ActivityDot status="active" />
            <span className="text-[11px] text-fg-3">2m ago</span>
            <span className="flex-1" />
            <span className="text-[11px] text-fg-3">3 keys</span>
          </div>

          {/* Service health example */}
          <div className="flex items-center gap-3 rounded-xl bg-surface-1 px-3 py-2.5">
            <span className="text-[13px] text-fg-2 font-medium">
              Embedding service
            </span>
            <ActivityDot status="idle" />
            <span className="text-[11px] text-fg-3">queued</span>
          </div>

          {/* Error example */}
          <div className="flex items-center gap-3 rounded-xl bg-surface-1 px-3 py-2.5">
            <span className="text-[13px] text-fg-2 font-medium">
              MCP connection
            </span>
            <ActivityDot status="error" />
            <span className="text-[11px] text-fg-3">timeout</span>
          </div>
        </div>
      </DemoCard>

      {/* ── 16c. Multi-query page — per-query error isolation ── */}
      <DemoCard label="16c. Multi-query page — per-query error isolation">
        <p className="text-[13px] text-fg-2 mb-3">
          A page with N queries has N independent error zones. Collapsing them
          into a single page-level error throws away intact data from successful
          queries and makes retry ambiguous (&ldquo;retry what?&rdquo;). Every
          query renders into its own card; each card owns its own error UI via{" "}
          <span className="font-mono">&lt;DataSectionCard&gt;</span>.
        </p>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {/* Wrong */}
          <div className="space-y-2">
            <p className="text-[10px] uppercase tracking-wider font-medium text-fg-3">
              ❌ Wrong — AND-gate collapse
            </p>
            <div className="font-mono text-[11px] text-fg-2 leading-relaxed bg-surface-1 rounded-lg p-3">
              <pre>{`if (topicsError || memoriesError) {
  return <ContentError retry={refetchAll} />
}
// → good topics data is thrown away
// → user sees red page instead of
//   3 loaded sections + 1 failed card`}</pre>
            </div>
            <p className="text-[10px] text-fg-4">
              Current production:{" "}
              <span className="font-mono">topic-grid.tsx:123-150</span>,{" "}
              <span className="font-mono">topic-detail.tsx</span> (tree error
              silently swallowed).
            </p>
          </div>
          {/* Right */}
          <div className="space-y-2">
            <p className="text-[10px] uppercase tracking-wider font-medium text-fg-3">
              ✅ Right — per-card error
            </p>
            <div className="space-y-2">
              <div
                className="rounded-xl px-4 py-3"
                style={{
                  border: "1px solid var(--border)",
                  background: "var(--card)",
                }}
              >
                <div className="text-[11px] uppercase tracking-wide text-fg-3 mb-1">
                  Recent
                </div>
                <div className="text-[12px] text-fg-2">✓ 3 memories</div>
              </div>
              <div
                className="rounded-xl px-4 py-3"
                style={{
                  border: "1px solid var(--border)",
                  background: "var(--card)",
                }}
              >
                <div className="text-[11px] uppercase tracking-wide text-fg-3 mb-1">
                  Topics
                </div>
                <div className="text-[12px] text-fg-2">✓ 6 topics</div>
              </div>
              <div
                className="rounded-xl px-4 py-3 flex items-start gap-2.5"
                style={{
                  border:
                    "1px solid oklch(from var(--destructive) l c h / 0.10)",
                  background: "oklch(from var(--destructive) l c h / 0.05)",
                }}
              >
                <span
                  className="mt-1.5 inline-block h-1.5 w-1.5 shrink-0 rounded-full state-flash"
                  style={{
                    backgroundColor:
                      "oklch(from var(--destructive) l c h / 0.6)",
                  }}
                />
                <div className="min-w-0 flex-1">
                  <div className="text-[11px] uppercase tracking-wide text-fg-3 mb-0.5">
                    Inbox
                  </div>
                  <div className="text-[12px] text-fg-2">
                    Couldn&apos;t load unorganized items.{" "}
                    <button className="text-fg-3 hover:text-fg-2 underline underline-offset-2 transition-colors cursor-pointer">
                      Reload inbox
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div className="text-[11px] text-fg-3 mt-4 space-y-1">
          <p className="font-medium text-fg-2">Rules</p>
          <p>
            · Each query owns its visual region. No global{" "}
            <span className="font-mono">useQueries</span> reduce-to-error.
          </p>
          <p>
            · Retry handlers target the specific query, never refetch the page.
          </p>
          <p>
            · Partial success is the norm during hub switch / backend rollout —
            design for it.
          </p>
          <p>
            · Error UI uses{" "}
            <span className="font-mono">&lt;ContentError&gt;</span> primitive —
            red pulsing dot + neutral retry link, no SIGNATURE color (design law
            4).
          </p>
        </div>
      </DemoCard>

      {/* ── 16d. Mutation pending vs query refetching — don't conflate ── */}
      <DemoCard label="16d. Mutation pending vs query refetching — don't double-feedback">
        <p className="text-[13px] text-fg-2 mb-3">
          A mutation&apos;s <span className="font-mono">isPending</span> is{" "}
          <em>not</em> the same signal as a query&apos;s{" "}
          <span className="font-mono">isFetching</span>. Showing both a{" "}
          <span className="font-mono">successMessage</span> toast <em>and</em>{" "}
          an inline visual confirmation is redundant — pick one. Production
          example: <span className="font-mono">useDreamTrigger</span> shows a
          success toast AND the inbox morphs to a breathing
          &ldquo;organizing…&rdquo; state (
          <span className="font-mono">use-dreams.ts:37</span>, section 28).
        </p>
        <div className="space-y-3">
          <div className="font-mono text-[11px] text-fg-2 leading-relaxed bg-surface-1 rounded-lg p-3">
            <pre>{`Mutation state  →  Query state  →  Visual
───────────────────────────────────────────────
isPending       →  —              →  button dim + spinner
                                      (per-row, not global)

(settled)       →  isFetching     →  NOTHING (silent refetch,
                                      see section 35c)

isSuccess       →  data updates   →  ONE of:
                                      a) row animates in
                                         via state-memory-arrive
                                         (see section 35e)
                                      b) toast
                                         (irreversible / batch only)

isError         →  data unchanged →  surface-specific error
                                      + retry that names action`}</pre>
          </div>
          <p className="text-[10px] text-fg-4">
            Rule of thumb: if the surface visibly updates (row vanishes, count
            decrements, status chip changes, new row animates in), skip the
            toast. Toasts are for invisible or irreversible actions (forget,
            undo, batch copy). Mutation inventory in section 28 should flag
            every mutation that has both.
          </p>
        </div>
      </DemoCard>

      {/* Implementation notes */}
      <div className="text-[12px] text-fg-3 space-y-1 mt-2">
        <p className="font-medium text-fg-2">Activity status rules</p>
        <p>
          <span className="font-mono text-fg-2">active</span> — last_used
          &lt;24h. Green + <span className="font-mono">animate-ping</span>{" "}
          pulse.
        </p>
        <p>
          <span className="font-mono text-fg-2">idle</span> — last_used 1-7d.
          Amber, static.
        </p>
        <p>
          <span className="font-mono text-fg-2">inactive</span> — &gt;7d or
          never. Muted fg-4, static.
        </p>
        <p>
          <span className="font-mono text-fg-2">error</span> — connection
          failure or expired key. Destructive red, static.
        </p>
        <p>
          Production: <span className="font-mono">lib/agents.ts</span>{" "}
          (StatusDot in agent-configs-section.tsx). Extend for service health,
          MCP status, sync state.
        </p>
      </div>
    </Section>
  );
}
