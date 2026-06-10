// Maps to: packages/web/src/components/features/memory-row.tsx,
// packages/web/src/components/features/provenance-strip.tsx
"use client";

import { Section, DemoCard, SIGNATURE } from "../_shared";
import { MarkdownSnippet } from "@/components/markdown-snippet";
import {
  Bot,
  FileText,
  Image as ImageIcon,
  Link as LinkIcon,
} from "lucide-react";

type Surface = "recent" | "inbox" | "topic" | "list" | "recall";

type IdentityVariant = "strict" | "hybrid";
type IdentitySurface = "recent" | "topic";
type IdentitySize = "sm" | "md18" | "md20" | "lg";

const NEUTRAL_DOT = "oklch(from var(--foreground) l c h / 0.2)";

interface MockRow {
  title: string;
  summary?: string;
  age: string;
  source?: string;
  sourceAgent?: string;
  authorName?: string;
  authorInitial?: string;
  hubName?: string;
  accessCount?: number;
  contentType?: "text" | "pdf" | "image" | "link";
  dotColor?: string;
}

interface IdentityMockRow {
  title: string;
  age: string;
  summary?: string;
  authorName?: string;
  authorInitial?: string;
  sourceAgent?: string;
  agentAccent?: string;
  personalOwner?: string;
  contentType?: "text" | "pdf" | "image" | "link";
  teamLabel?: string;
}

function SpecRow({
  row,
  surface,
  showDivider = false,
}: {
  row: MockRow;
  surface: Surface;
  showDivider?: boolean;
}) {
  const dot = row.dotColor ?? NEUTRAL_DOT;
  const isQuietSurface = surface === "topic" || surface === "inbox";
  const showAttributionText =
    surface === "recent" || surface === "recall" || surface === "list";
  const showSummary =
    !!row.summary &&
    (surface === "recent" ||
      surface === "topic" ||
      surface === "list" ||
      surface === "recall");
  const showSource = surface === "list";
  const showCount = showSource && (row.accessCount ?? 0) > 0;
  const showHub = surface === "recall" && !!row.hubName;
  const showTrailingActor =
    isQuietSurface && (!!row.authorName || !!row.sourceAgent);

  const indicator = showTrailingActor ? (
    row.contentType === "pdf" ? (
      <FileText className="h-3 w-3 shrink-0" style={{ color: dot }} />
    ) : row.contentType === "image" ? (
      <ImageIcon className="h-3 w-3 shrink-0" style={{ color: dot }} />
    ) : row.contentType === "link" ? (
      <LinkIcon className="h-3 w-3 shrink-0" style={{ color: dot }} />
    ) : (
      <span className="h-3 w-3 flex items-center justify-center shrink-0">
        <span
          className="h-1.5 w-1.5 rounded-full"
          style={{ backgroundColor: dot }}
        />
      </span>
    )
  ) : row.sourceAgent ? (
    <Bot className="h-3.5 w-3.5 text-fg-3 shrink-0" />
  ) : row.authorName ? (
    <div
      className="h-4 w-4 rounded-full flex items-center justify-center text-[8px] font-medium text-fg-2 shrink-0"
      style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
    >
      {row.authorInitial ?? row.authorName[0]?.toUpperCase()}
    </div>
  ) : row.contentType === "pdf" ? (
    <FileText className="h-3 w-3 shrink-0" style={{ color: dot }} />
  ) : row.contentType === "image" ? (
    <ImageIcon className="h-3 w-3 shrink-0" style={{ color: dot }} />
  ) : row.contentType === "link" ? (
    <LinkIcon className="h-3 w-3 shrink-0" style={{ color: dot }} />
  ) : (
    <span className="h-3 w-3 flex items-center justify-center shrink-0">
      <span
        className="h-1.5 w-1.5 rounded-full"
        style={{ backgroundColor: dot }}
      />
    </span>
  );

  const contextLabel = !showAttributionText ? null : row.authorName ? (
    <span className="inline-flex shrink-0 items-center gap-1 text-[13px] text-fg-3">
      <span className="hidden sm:inline">{row.authorName}</span>
      {row.sourceAgent ? (
        <>
          <span>via</span>
          <span className="hidden sm:inline-flex items-center gap-1">
            <Bot className="h-3 w-3 text-fg-3 shrink-0" />
            <span>{row.sourceAgent}</span>
          </span>
        </>
      ) : (
        "pushed"
      )}
    </span>
  ) : row.sourceAgent ? (
    <span className="text-[13px] text-fg-3 shrink-0">
      <span className="hidden sm:inline">{row.sourceAgent} </span>
      captured
    </span>
  ) : surface === "recent" ? (
    <span className="text-[13px] text-fg-3 shrink-0">You saved</span>
  ) : null;

  return (
    <div
      className={`px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${showDivider ? "border-t border-border/30" : ""}`}
    >
      <div className="flex items-center gap-2">
        {indicator}
        {contextLabel}
        <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
          {row.title}
        </span>
        <div className="flex items-center gap-1.5 ml-auto shrink-0">
          {showHub && (
            <span className="text-[10px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
              {row.hubName}
            </span>
          )}
          {showTrailingActor &&
            (row.authorName ? (
              <div
                className="h-[18px] w-[18px] rounded-full flex items-center justify-center text-[9px] font-medium text-fg-1 shrink-0"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.08)",
                }}
                title={row.authorName}
              >
                {row.authorInitial ?? row.authorName[0]?.toUpperCase()}
              </div>
            ) : (
              <div
                className="h-[18px] w-[18px] rounded-lg flex items-center justify-center text-fg-2 shrink-0"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.08)",
                }}
                title={row.sourceAgent}
              >
                <Bot className="h-3 w-3" strokeWidth={1.9} />
              </div>
            ))}
          {showCount && (
            <span className="text-[12px] text-fg-3 tabular-nums">
              {row.accessCount}×
            </span>
          )}
          {showSource && row.source && (
            <span className="text-[10px] text-fg-3">{row.source}</span>
          )}
          <span className="text-[13px] text-fg-3 tabular-nums">{row.age}</span>
        </div>
      </div>
      {showSummary && (
        <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-5">
          {row.summary}
        </p>
      )}
    </div>
  );
}

const ROWS = {
  teamViaAgent: {
    title: "Reranker cost analysis",
    summary: "Cohere rerank v3 at $1/1000 queries, 50ms p95...",
    age: "5h",
    source: "cli",
    sourceAgent: "Claude Code",
    authorName: "Ziyang",
    authorInitial: "Z",
    hubName: "backend-team",
    dotColor: NEUTRAL_DOT,
  },
  teamHuman: {
    title: "API auth flow",
    summary: "OAuth2 refresh token rotation with 30-day expiry...",
    age: "2h",
    source: "web",
    authorName: "Derek",
    authorInitial: "D",
    hubName: "backend-team",
    dotColor: NEUTRAL_DOT,
  },
  personalAgent: {
    title: "Session: refactored ingest pipeline",
    summary: "Moved chunking from handler to worker, added retry logic...",
    age: "8h",
    source: "hook",
    sourceAgent: "Claude Code",
    accessCount: 47,
    dotColor: NEUTRAL_DOT,
  },
  personalText: {
    title: "Blue-green deployment strategy",
    summary: "Zero-downtime deploys using blue-green with Fly.io machines...",
    age: "2d",
    source: "cli",
    accessCount: 5,
    contentType: "text" as const,
    dotColor: NEUTRAL_DOT,
  },
  personalPdf: {
    title: "Security audit checklist.pdf",
    summary: "Quarterly audit process, control mapping, evidence...",
    age: "4d",
    contentType: "pdf" as const,
    dotColor: NEUTRAL_DOT,
  },
  personalLink: {
    title: "OpenTelemetry semantic conventions",
    summary: "External reference for trace and metric naming.",
    age: "6d",
    contentType: "link" as const,
    dotColor: NEUTRAL_DOT,
  },
};

const STRESS_ROWS: IdentityMockRow[] = [
  {
    title: "Homepage load 1.2s after image pipeline rewrite",
    age: "12m",
    sourceAgent: "Claude Code",
    agentAccent: "#ef6a4f",
    summary: "Landing page image compression cut the first meaningful load.",
  },
  {
    title: "Thursday investor demo emphasis for Sarah",
    age: "37m",
    personalOwner: "You",
    authorInitial: "J",
    summary: "Keep the speed improvement above the architecture slide.",
  },
  {
    title: "Media embargo lifts Friday",
    age: "1h",
    authorName: "Sarah",
    authorInitial: "S",
    teamLabel: "marketing",
    summary: "Do not publish performance data before the embargo clears.",
  },
  {
    title: "Security audit checklist.pdf",
    age: "3h",
    contentType: "pdf",
    summary: "Quarterly audit controls and evidence requirements.",
  },
  {
    title: "OpenTelemetry conventions reference",
    age: "4h",
    contentType: "link",
    summary: "External semantic naming reference for traces and metrics.",
  },
  {
    title: "Dream grouped auth fixes into Auth & Security",
    age: "5h",
    sourceAgent: "Memax Dream",
    agentAccent: SIGNATURE,
    summary: "New auth notes were clustered under the topic automatically.",
  },
  {
    title: "Refresh token rotation note",
    age: "7h",
    personalOwner: "You",
    authorInitial: "J",
    summary: "Mention rollback guard in the implementation notes.",
  },
  {
    title: "SAML callback boundary fix",
    age: "9h",
    authorName: "Derek",
    authorInitial: "D",
    teamLabel: "backend",
    summary: "Callback validation tightened around tenant boundaries.",
  },
  {
    title: "On-call handoff image",
    age: "11h",
    contentType: "image",
    summary: "Incident timeline screenshot from the handoff deck.",
  },
  {
    title: "Reranker cost analysis",
    age: "1d",
    sourceAgent: "Claude Code",
    agentAccent: "#ef6a4f",
    summary: "Cohere rerank cost profile versus frequency buckets.",
  },
  {
    title: "Blue-green deployment strategy",
    age: "2d",
    personalOwner: "You",
    authorInitial: "J",
    summary: "Zero-downtime deploy path with machine warmup.",
  },
  {
    title: "Partner launch checklist",
    age: "3d",
    authorName: "Ava",
    authorInitial: "A",
    teamLabel: "growth",
    summary: "Coordinate messaging and final legal review timing.",
  },
];

const IDENTITY_SIZE = {
  sm: {
    shell: "h-3.5 w-3.5 text-[10px]",
    icon: "h-3.5 w-3.5",
    rowTitle: "text-[13px]",
    rowMeta: "text-[11px]",
    rowHeight: "py-2",
  },
  md18: {
    shell: "h-[18px] w-[18px] text-[9px]",
    icon: "h-[18px] w-[18px]",
    rowTitle: "text-[14px]",
    rowMeta: "text-[12px]",
    rowHeight: "py-2.5",
  },
  md20: {
    shell: "h-5 w-5 text-[10px]",
    icon: "h-5 w-5",
    rowTitle: "text-[14px]",
    rowMeta: "text-[12px]",
    rowHeight: "py-3",
  },
  lg: {
    shell: "h-7 w-7 text-[12px]",
    icon: "h-7 w-7",
    rowTitle: "text-[15px]",
    rowMeta: "text-[13px]",
    rowHeight: "py-3.5",
  },
} as const;

function renderArtifactIcon(
  contentType: IdentityMockRow["contentType"],
  size: IdentitySize,
) {
  const className = `${IDENTITY_SIZE[size].icon} text-fg-3`;
  if (contentType === "image") return <ImageIcon className={className} />;
  if (contentType === "link") return <LinkIcon className={className} />;
  return <FileText className={className} />;
}

function MemoryIdentity({
  row,
  size,
  variant,
}: {
  row: IdentityMockRow;
  size: IdentitySize;
  variant: IdentityVariant;
}) {
  const shell = IDENTITY_SIZE[size].shell;

  if (row.sourceAgent) {
    return (
      <div
        className={`${shell} shrink-0 rounded-lg flex items-center justify-center`}
        style={{
          background: `color-mix(in oklch, ${row.agentAccent ?? SIGNATURE} 12%, white)`,
          color: row.agentAccent ?? SIGNATURE,
        }}
      >
        <Bot className="h-[70%] w-[70%]" strokeWidth={1.9} />
      </div>
    );
  }

  if (row.authorName) {
    return (
      <div
        className={`${shell} shrink-0 rounded-full flex items-center justify-center font-medium text-fg-1`}
        style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
      >
        {row.authorInitial ?? row.authorName?.[0]?.toUpperCase() ?? "Y"}
      </div>
    );
  }

  return (
    <div className="shrink-0 flex items-center justify-center">
      {renderArtifactIcon(row.contentType, size)}
    </div>
  );
}

function IdentityRow({
  row,
  size,
  variant,
  surface = "recent",
  showDivider = false,
}: {
  row: IdentityMockRow;
  size: IdentitySize;
  variant: IdentityVariant;
  surface?: IdentitySurface;
  showDivider?: boolean;
}) {
  const sizeSpec = IDENTITY_SIZE[size];
  const isTeam = !!row.authorName;
  const showContentMeta = variant === "strict";
  const leadingLabel = isTeam
    ? `${row.authorName}${row.teamLabel ? ` · ${row.teamLabel}` : ""} · pushed`
    : row.sourceAgent
      ? `${row.sourceAgent} · captured`
      : variant === "strict"
        ? "You · saved"
        : "note";

  return (
    <div
      className={`px-4 ${sizeSpec.rowHeight} hover:bg-surface-1 transition-colors ${
        showDivider ? "border-t border-border/30" : ""
      }`}
    >
      <div className="flex items-center gap-3">
        <MemoryIdentity row={row} size={size} variant={variant} />
        <div className="min-w-0 flex-1">
          {surface === "recent" ? (
            <div
              className={`mb-0.5 flex items-center gap-2 ${sizeSpec.rowMeta}`}
            >
              <span className="text-fg-3 truncate">{leadingLabel}</span>
              <span className="ml-auto text-fg-4 tabular-nums">{row.age}</span>
            </div>
          ) : surface === "topic" && variant === "strict" ? (
            <div
              className={`mb-0.5 flex items-center gap-2 ${sizeSpec.rowMeta}`}
            >
              <span className="text-fg-3 truncate">{leadingLabel}</span>
              <span className="ml-auto flex items-center gap-1 text-fg-4">
                {row.contentType && renderArtifactIcon(row.contentType, "sm")}
                <span className="tabular-nums">{row.age}</span>
              </span>
            </div>
          ) : null}
          <div className="flex items-center gap-2">
            <span
              className={`${sizeSpec.rowTitle} font-medium text-foreground truncate flex-1`}
            >
              {row.title}
            </span>
            {surface !== "recent" && (
              <span
                className={`${sizeSpec.rowMeta} text-fg-4 tabular-nums shrink-0`}
              >
                {row.age}
              </span>
            )}
          </div>
          {row.summary && (
            <p className={`${sizeSpec.rowMeta} text-fg-3 truncate mt-0.5`}>
              {row.summary}
            </p>
          )}
        </div>
        {surface === "recent" && showContentMeta && row.contentType && (
          <div className="shrink-0 flex items-center gap-1.5 text-[11px] text-fg-4">
            {renderArtifactIcon(row.contentType, "sm")}
          </div>
        )}
      </div>
    </div>
  );
}

function IdentityVariantList({
  label,
  rows,
  variant,
  surface = "recent",
  size = "md20",
}: {
  label: string;
  rows: IdentityMockRow[];
  variant: IdentityVariant;
  surface?: IdentitySurface;
  size?: IdentitySize;
}) {
  return (
    <div>
      <p className="text-[12px] text-fg-3 mb-3">{label}</p>
      <div className="rounded-xl overflow-hidden border border-border/60 bg-card">
        {rows.map((row, i) => (
          <IdentityRow
            key={`${label}-${row.title}`}
            row={row}
            size={size}
            variant={variant}
            surface={surface}
            showDivider={i > 0}
          />
        ))}
      </div>
    </div>
  );
}

export function MemoryRowVariantsSection() {
  return (
    <Section
      title="4. Memory Row Variants"
      description="Canonical spec for MemoryRow surfaces, attribution rules, and responsive behavior. This is the source of truth for row-level behavior across recent, topic, inbox, list, and recall."
    >
      <DemoCard label="Rules">
        <div className="space-y-3 text-[13px] text-fg-2">
          <p>
            Feed surfaces are attribution-rich. Content surfaces are
            attribution-light.
          </p>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-left text-[12px]">
              <thead className="text-fg-3">
                <tr className="border-b border-border/40">
                  <th className="py-2 pr-3 font-medium">Surface</th>
                  <th className="py-2 pr-3 font-medium">Indicator</th>
                  <th className="py-2 pr-3 font-medium">Text</th>
                  <th className="py-2 pr-3 font-medium">Summary</th>
                  <th className="py-2 pr-3 font-medium">Meta</th>
                  <th className="py-2 font-medium">Mobile</th>
                </tr>
              </thead>
              <tbody className="text-fg-2">
                <tr className="border-b border-border/20 align-top">
                  <td className="py-2 pr-3">recent</td>
                  <td className="py-2 pr-3">
                    avatar, agent icon, or content icon
                  </td>
                  <td className="py-2 pr-3">
                    team: `human pushed` / `human via agent`; personal: `agent
                    captured`; plain personal recent: `You saved`
                  </td>
                  <td className="py-2 pr-3">yes</td>
                  <td className="py-2 pr-3">content type, age, copy</td>
                  <td className="py-2">hide the name, keep the verb</td>
                </tr>
                <tr className="border-b border-border/20 align-top">
                  <td className="py-2 pr-3">topic</td>
                  <td className="py-2 pr-3">
                    content icon for explicit actors
                  </td>
                  <td className="py-2 pr-3">none</td>
                  <td className="py-2 pr-3">yes</td>
                  <td className="py-2 pr-3">trailing actor, age</td>
                  <td className="py-2">same as desktop</td>
                </tr>
                <tr className="border-b border-border/20 align-top">
                  <td className="py-2 pr-3">inbox</td>
                  <td className="py-2 pr-3">content icon only</td>
                  <td className="py-2 pr-3">none</td>
                  <td className="py-2 pr-3">no</td>
                  <td className="py-2 pr-3">trailing actor, age</td>
                  <td className="py-2">same as desktop</td>
                </tr>
                <tr className="border-b border-border/20 align-top">
                  <td className="py-2 pr-3">recall</td>
                  <td className="py-2 pr-3">avatar or agent icon</td>
                  <td className="py-2 pr-3">
                    same attribution rules as recent
                  </td>
                  <td className="py-2 pr-3">yes</td>
                  <td className="py-2 pr-3">hub badge, age</td>
                  <td className="py-2">hide the name, keep the verb</td>
                </tr>
                <tr className="align-top">
                  <td className="py-2 pr-3">list</td>
                  <td className="py-2 pr-3">avatar or agent icon</td>
                  <td className="py-2 pr-3">
                    same attribution rules as recent
                  </td>
                  <td className="py-2 pr-3">yes</td>
                  <td className="py-2 pr-3">source, count, age</td>
                  <td className="py-2">hide the name, keep the verb</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </DemoCard>

      {/* ══════════════════════════════════════════════════════════════════
          Flat vs full summary — scan anchor rule
          ══════════════════════════════════════════════════════════════════ */}
      <DemoCard label="Summary rendering — flat in rows, full in detail">
        <p className="text-[12px] text-fg-2 mb-3">
          <span className="text-fg-1 font-medium">
            One scan anchor per row.
          </span>{" "}
          In a feed or list, the title is the{" "}
          <span className="text-fg-1">only</span> thing the eye is allowed to
          latch onto. If the summary renders inline{" "}
          <span className="font-mono">**bold**</span> or{" "}
          <span className="font-mono">`code`</span>, every row sprouts a second
          anchor that fights the title, and scannability collapses. This is why
          Gmail, Linear, Notion, Apple Mail, and Readwise all render list
          previews as flat plain text and save rich markdown for the detail
          view.
        </p>

        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Before → after (same AI summary, rendered two ways)
          </p>
          <div className="space-y-4">
            {/* BEFORE: full markdown, bold lead-in */}
            <div>
              <div className="rounded-xl border border-border/40 bg-card px-4 py-3">
                <div className="flex items-baseline gap-3 min-w-0">
                  <span className="h-4 w-4 rounded-full shrink-0 bg-foreground/20 mt-0.5" />
                  <div className="min-w-0 flex-1">
                    <span className="text-[14px] font-medium text-foreground truncate block">
                      Inbox north star: top-right desktop, route-based mobile
                    </span>
                    <p className="text-[13px] text-fg-3 truncate mt-0.5">
                      <MarkdownSnippet
                        text="**Inbox positioning decided:** desktop places entry in top-right chrome between hub chip and avatar, opening anchored."
                        maxLen={120}
                      />
                    </p>
                  </div>
                  <span className="text-[11px] text-fg-4 shrink-0">1h</span>
                </div>
              </div>
              <p className="text-[10px] text-fg-4 font-mono mt-1.5">
                ✗ current — MarkdownSnippet renders{" "}
                <span className="font-mono">**…**</span> as{" "}
                <span className="font-mono">font-semibold text-fg-2</span>. The
                bold lead-in is brighter AND heavier than the body, so the row
                now has three hierarchy levels (title / bold-lead / body). Stack
                ten of these and the eye has nowhere to rest.
              </p>
            </div>

            {/* AFTER: flat, single scan anchor */}
            <div>
              <div className="rounded-xl border border-border/40 bg-card px-4 py-3">
                <div className="flex items-baseline gap-3 min-w-0">
                  <span className="h-4 w-4 rounded-full shrink-0 bg-foreground/20 mt-0.5" />
                  <div className="min-w-0 flex-1">
                    <span className="text-[14px] font-medium text-foreground truncate block">
                      Inbox north star: top-right desktop, route-based mobile
                    </span>
                    <p className="text-[13px] text-fg-3 truncate mt-0.5">
                      <MarkdownSnippet
                        text="**Inbox positioning decided:** desktop places entry in top-right chrome between hub chip and avatar, opening anchored."
                        maxLen={120}
                        flat
                      />
                    </p>
                  </div>
                  <span className="text-[11px] text-fg-4 shrink-0">1h</span>
                </div>
              </div>
              <p className="text-[10px] text-fg-4 font-mono mt-1.5">
                ✓ flat — <span className="font-mono">flat</span> prop strips
                inline <span className="font-mono">**/*/`</span> markers. The
                preview is one quiet glance in{" "}
                <span className="font-mono">text-fg-3</span> regular, and the
                title is the only anchor. Full markdown rendering is still
                available in the detail view where emphasis serves a real
                purpose.
              </p>
            </div>
          </div>
        </div>

        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            The rule
          </p>
          <div className="text-[11px] text-fg-2 space-y-1.5">
            <p>
              <span className="text-fg-1 font-medium">Row preview</span> →{" "}
              <span className="font-mono">
                &lt;MarkdownSnippet text={"{m.summary}"} maxLen={"{120}"} flat
                /&gt;
              </span>
              . Always flat. One scan anchor. Gmail / Linear / Notion
              convention.
            </p>
            <p>
              <span className="text-fg-1 font-medium">Detail view</span> →{" "}
              <span className="font-mono">
                &lt;MarkdownSnippet text={"{m.summary}"} /&gt;
              </span>{" "}
              (default, not flat). User opened the memory to read it; emphasis
              now serves a purpose and doesn&apos;t compete with anything.
            </p>
          </div>
        </div>

        {/* ── Two render paths, one data shape — the correct layering ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Architecture — two render paths, one data shape
          </p>
          <div className="text-[11px] text-fg-2 space-y-1.5 mb-3">
            <p>
              <span className="text-fg-1 font-medium">
                Flat is not a bandaid — it is the correct layering.
              </span>{" "}
              memax distills memories into rich structured markdown on purpose,
              because the <span className="text-fg-1">detail view</span> is
              where users actually read the memory and structure (bold lead-ins,
              lists, headings, citations) earns its keep. Stripping that at the
              distillation layer would make the detail view worse to serve a
              presentation concern that belongs in the UI.
            </p>
            <p>
              The right architecture is{" "}
              <span className="text-fg-1 font-medium">
                one data shape, two render paths
              </span>{" "}
              — same <span className="font-mono">memory.summary</span> field,
              rendered differently by surface:
            </p>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-3">
            <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
              <p className="text-[11px] font-medium text-fg-1 mb-1.5">
                Detail view →{" "}
                <span className="font-mono">&lt;AISummary /&gt;</span>
              </p>
              <div className="text-[10px] text-fg-3 space-y-1">
                <p>
                  Full <span className="font-mono">react-markdown</span> +{" "}
                  <span className="font-mono">remarkGfm</span> — bold, italic,
                  lists, headings, inline code, paragraphs, citation badges.
                </p>
                <p>
                  Custom renderers for{" "}
                  <span className="font-mono">&lt;p&gt;</span>,{" "}
                  <span className="font-mono">&lt;strong&gt;</span>,{" "}
                  <span className="font-mono">&lt;a&gt;</span> (citation{" "}
                  <span className="font-mono">[N]</span> → inline button).
                </p>
                <p>
                  Source:{" "}
                  <span className="font-mono">
                    packages/web/src/components/features/ai-summary.tsx
                  </span>
                </p>
                <p>
                  Structure serves reading — bold lead-ins land, TL;DR patterns
                  land, the user gets what the AI actually wrote.
                </p>
              </div>
            </div>
            <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
              <p className="text-[11px] font-medium text-fg-1 mb-1.5">
                List preview →{" "}
                <span className="font-mono">
                  &lt;MarkdownSnippet flat /&gt;
                </span>
              </p>
              <div className="text-[10px] text-fg-3 space-y-1">
                <p>
                  Strips inline <span className="font-mono">**/*/`</span> and
                  collapses to plain text in{" "}
                  <span className="font-mono">text-fg-3</span>, 120-char
                  truncation.
                </p>
                <p>
                  Block-level stripping (headings / lists / code fences) is
                  always on — previews are one-liners.
                </p>
                <p>
                  Source:{" "}
                  <span className="font-mono">
                    packages/ui/src/components/markdown-snippet.tsx
                  </span>
                </p>
                <p>
                  Scannability serves the feed — one anchor per row, the title.
                  Reading happens on click.
                </p>
              </div>
            </div>
          </div>
          <p className="text-[11px] text-fg-2">
            <span className="text-fg-1 font-medium">
              Do NOT constrain the distillation prompt.
            </span>{" "}
            Rich markdown in the data shape is an asset, not a bug. Any new
            surface that renders summaries gets to pick its own layer —{" "}
            <span className="font-mono">flat</span> for feeds and list rows,
            full <span className="font-mono">&lt;AISummary /&gt;</span> for
            detail and reading contexts. This is standard Separation of
            Concerns: data stays rich, presentation adapts per surface.
          </p>
        </div>

        <div className="pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Industry references
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[11px] text-fg-2">
            <div>
              <span className="text-fg-1 font-medium">Gmail</span> — uses
              read/unread state to bold the{" "}
              <span className="text-fg-1">entire row</span>, never mid-sentence
              bold inside a single snippet.
            </div>
            <div>
              <span className="text-fg-1 font-medium">Linear</span> — issue list
              title is medium fg-1, description is regular fg-3, no inline
              emphasis in preview.
            </div>
            <div>
              <span className="text-fg-1 font-medium">Notion</span> — database
              row title is medium fg-1, property preview is regular, markdown in
              preview is stripped to plain text.
            </div>
            <div>
              <span className="text-fg-1 font-medium">Apple Mail</span> —
              subject medium, preview regular fg-3, no mid-text bold even if the
              source email had it.
            </div>
            <div>
              <span className="text-fg-1 font-medium">Readwise</span> — title
              semibold, excerpt regular, bold is reserved for the open-article
              reading view.
            </div>
            <div>
              <span className="text-fg-1 font-medium">
                Raindrop / Pocket / GitHub notifications
              </span>{" "}
              — same pattern. Title anchor, muted flat preview.
            </div>
          </div>
        </div>

        {/* ── Regression tests — what's locked in ── */}
        <div className="mt-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Regression tests — what&apos;s locked in
          </p>
          <p className="text-[11px] text-fg-2 mb-2">
            The rules on this card are pinned by{" "}
            <span className="font-mono">
              packages/web/src/lib/markdown-snippet.test.ts
            </span>
            . 25 cases, covering both modes + edge cases. If a future refactor
            tries to flatten detail-view emphasis or un-flatten row previews, CI
            fails before the change lands. Highlights:
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-[10px] text-fg-3 font-mono">
            <div>
              <span className="text-fg-2">flat mode:</span> strips **bold** /
              *italic* / `code` → no &lt;strong&gt;&lt;em&gt;&lt;code&gt;
            </div>
            <div>
              <span className="text-fg-2">flat mode:</span> canonical **Label:**
              body pattern flattens to plain prose
            </div>
            <div>
              <span className="text-fg-2">flat mode:</span> search highlight
              still renders &lt;mark&gt; around matches
            </div>
            <div>
              <span className="text-fg-2">default mode:</span> **bold** still
              renders &lt;strong&gt; (detail view intact)
            </div>
            <div>
              <span className="text-fg-2">block stripping:</span> headings /
              code fences / list markers / newlines collapsed
            </div>
            <div>
              <span className="text-fg-2">broken input:</span> unclosed **bold,
              stray *, mismatched ` all fall through fallback without throwing
            </div>
            <div>
              <span className="text-fg-2">truncation:</span> ellipsis appears
              iff text exceeds maxLen; boundary token fixup doesn&apos;t leak
              markers
            </div>
            <div>
              <span className="text-fg-2">normalization:</span> __bold__
              (underscore form) still routes to strong in default mode
            </div>
          </div>
          <p className="text-[10px] text-fg-4 mt-2">
            Run locally:{" "}
            <span className="font-mono">
              pnpm --filter @memaxlabs/web exec vitest run
              src/lib/markdown-snippet.test.ts
            </span>
          </p>
        </div>
      </DemoCard>

      <DemoCard label="Attribution Matrix">
        <div className="grid gap-4 md:grid-cols-2 text-[13px] text-fg-2">
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
            <p className="font-medium text-fg-1 mb-2">Team hub</p>
            <p>`human pushed` when there is no agent</p>
            <p>`human via agent` when a tool captured on their behalf</p>
            <p className="text-fg-3 mt-2">
              Example: `Ziyang via [agent icon] Claude Code`
            </p>
          </div>
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
            <p className="font-medium text-fg-1 mb-2">Personal hub</p>
            <p>`agent captured` when an agent is present</p>
            <p>`You saved` on desktop recent when there is no agent</p>
            <p>mobile plain recent rows collapse to compact artifact layout</p>
            <p>
              no attribution text on quieter surfaces when there is no agent
            </p>
            <p className="text-fg-3 mt-2">
              Example: `Claude Code captured` / `You saved`
            </p>
          </div>
        </div>
      </DemoCard>

      <DemoCard label="Recent — Feed Surface">
        <p className="text-[12px] text-fg-3 mb-3">
          Recent is attribution-rich. Desktop keeps the actor name and, when a
          tool captured on their behalf, shows the agent icon inline before the
          agent name. Mobile drops the name for team and agent rows. Plain
          personal rows collapse to the compact artifact layout instead of
          showing `saved` next to a file icon.
        </p>
        <div className="rounded-xl overflow-hidden border border-border/60 bg-card">
          <SpecRow row={ROWS.teamViaAgent} surface="recent" />
          <SpecRow row={ROWS.teamHuman} surface="recent" showDivider />
          <SpecRow row={ROWS.personalAgent} surface="recent" showDivider />
          <SpecRow row={ROWS.personalText} surface="recent" showDivider />
        </div>
      </DemoCard>

      <DemoCard label="Topic — Quiet Content Surface">
        <p className="text-[12px] text-fg-3 mb-3">
          Topic rows answer “what is in this topic?” Explicit non-self actors
          move to the trailing edge, so the left side stays content-led and the
          title reads first.
        </p>
        <div className="rounded-xl overflow-hidden border border-border/60 bg-card">
          <SpecRow row={ROWS.teamViaAgent} surface="topic" />
          <SpecRow row={ROWS.personalAgent} surface="topic" showDivider />
          <SpecRow row={ROWS.personalPdf} surface="topic" showDivider />
          <SpecRow row={ROWS.personalLink} surface="topic" showDivider />
        </div>
      </DemoCard>

      <DemoCard label="Inbox + Recall">
        <div className="grid gap-4 lg:grid-cols-2">
          <div>
            <p className="text-[12px] text-fg-3 mb-3">
              Inbox stays minimal, but still reserves the trailing slot for
              explicit actors.
            </p>
            <div className="rounded-xl overflow-hidden border border-border/60 bg-card">
              <SpecRow row={ROWS.teamHuman} surface="inbox" />
              <SpecRow row={ROWS.personalAgent} surface="inbox" showDivider />
            </div>
          </div>
          <div>
            <p className="text-[12px] text-fg-3 mb-3">
              Recall stays attribution-rich because it is cross-context and
              often cross-hub.
            </p>
            <div className="rounded-xl overflow-hidden border border-border/60 bg-card">
              <SpecRow row={ROWS.teamViaAgent} surface="recall" />
              <SpecRow row={ROWS.personalAgent} surface="recall" showDivider />
            </div>
          </div>
        </div>
      </DemoCard>

      <DemoCard label="Identity Slot Unification Proposal — decision context">
        <div className="grid gap-4 lg:grid-cols-3 text-[13px] text-fg-2">
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
            <p className="font-medium text-fg-1 mb-2">
              A · Strict identity axis
            </p>
            <p>Team rows use avatars.</p>
            <p>Agent rows use branded agent tiles.</p>
            <p>Personal plain rows also use the user avatar.</p>
            <p className="text-fg-3 mt-2">
              Content type moves to trailing meta.
            </p>
          </div>
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
            <p className="font-medium text-fg-1 mb-2">
              B · Identity-or-artifact hybrid
            </p>
            <p>Team and agent rows stay identity-first.</p>
            <p>Personal plain rows use neutral artifact icons.</p>
            <p className="text-fg-3 mt-2">
              No taxonomy color in the row-leading slot.
            </p>
          </div>
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3">
            <p className="font-medium text-fg-1 mb-2">C · Surface-dependent</p>
            <p>
              Recent behaves like a feed, but plain personal rows stay
              artifact-led.
            </p>
            <p>Topic behaves like content inspection, so artifact can win.</p>
            <p className="text-fg-3 mt-2">
              This is the shipped production direction.
            </p>
          </div>
        </div>
      </DemoCard>

      <DemoCard label="A. Strict identity axis">
        <IdentityVariantList
          label="Team, agent, and personal plain all resolve to one identity lane. Content type is pushed into trailing meta."
          rows={[
            STRESS_ROWS[0],
            STRESS_ROWS[1],
            STRESS_ROWS[2],
            STRESS_ROWS[3],
          ]}
          variant="strict"
          surface="recent"
          size="md20"
        />
      </DemoCard>

      <DemoCard label="B. Identity-or-artifact hybrid">
        <IdentityVariantList
          label="Personal plain notes resolve to a neutral FileText outline. PDF / link / image stay neutral, not kind-colored."
          rows={[
            STRESS_ROWS[0],
            STRESS_ROWS[1],
            STRESS_ROWS[3],
            STRESS_ROWS[4],
          ]}
          variant="hybrid"
          surface="recent"
          size="md20"
        />
      </DemoCard>

      <DemoCard label="C. Surface-dependent">
        <div className="grid gap-4 lg:grid-cols-2">
          <IdentityVariantList
            label="Recent uses the chosen hybridized feed rule: team and agent rows are identity-first, plain personal rows keep artifact icons with `You saved`."
            rows={STRESS_ROWS.slice(0, 4)}
            variant="hybrid"
            surface="recent"
            size="md20"
          />
          <IdentityVariantList
            label="Topic uses B: hybrid content inspection."
            rows={STRESS_ROWS.slice(0, 4)}
            variant="hybrid"
            surface="topic"
            size="md20"
          />
        </div>
      </DemoCard>

      <DemoCard label="Size Primitives">
        <p className="text-[12px] text-fg-3 mb-4">
          Same primitive, four sizes. Decide at a glance before touching
          production.
        </p>
        <div className="grid gap-4 md:grid-cols-4">
          {(["sm", "md18", "md20", "lg"] as IdentitySize[]).map((size) => (
            <div key={size} className="space-y-3">
              <p className="text-[12px] font-medium text-fg-2 uppercase tracking-wide">
                {size}
              </p>
              <div className="rounded-xl border border-border/50 bg-card p-3 space-y-3">
                <div className="flex items-center gap-3">
                  <MemoryIdentity
                    row={STRESS_ROWS[2]}
                    size={size}
                    variant="strict"
                  />
                  <span className="text-[12px] text-fg-2">team</span>
                </div>
                <div className="flex items-center gap-3">
                  <MemoryIdentity
                    row={STRESS_ROWS[0]}
                    size={size}
                    variant="strict"
                  />
                  <span className="text-[12px] text-fg-2">agent</span>
                </div>
                <div className="flex items-center gap-3">
                  <MemoryIdentity
                    row={STRESS_ROWS[1]}
                    size={size}
                    variant="hybrid"
                  />
                  <span className="text-[12px] text-fg-2">personal plain</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="Density Stress">
        <p className="text-[12px] text-fg-3 mb-4">
          Decide at feed length, not at single-row zoom. `md18` and `md20`
          rendered with the same 12-row dataset.
        </p>
        <div className="grid gap-4 xl:grid-cols-2">
          <IdentityVariantList
            label="md18 — denser, less identity presence"
            rows={STRESS_ROWS}
            variant="hybrid"
            surface="recent"
            size="md18"
          />
          <IdentityVariantList
            label="md20 — more identity presence, slightly more vertical cost"
            rows={STRESS_ROWS}
            variant="hybrid"
            surface="recent"
            size="md20"
          />
        </div>
      </DemoCard>

      <DemoCard label="Comparison Criteria">
        <div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr] text-[13px] text-fg-2">
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3 space-y-2">
            <p className="font-medium text-fg-1">Five-point checklist</p>
            <p>1. Personal plain readability</p>
            <p>2. Topic scan speed</p>
            <p>3. Recall speed</p>
            <p>4. Mobile compression</p>
            <p>5. Team + agent compound rows</p>
          </div>
          <div className="rounded-lg border border-border/40 bg-surface-1 p-3 space-y-2">
            <p className="font-medium text-fg-1">Decision locked so far</p>
            <p>Dots stay on topic cards only.</p>
            <p>Inbox keeps `sm`.</p>
            <p>Processing state overrides identity.</p>
            <p>Hero stays `lg`.</p>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
