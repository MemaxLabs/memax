"use client";

import React from "react";
import type { DreamActionRef } from "memax-sdk";
import type { Memory } from "@/hooks/use-memories";
import { useLocale, useInterpolate } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { resolveMemoryAttribution } from "@/lib/memory-attribution";
import {
  linkedAgentAttributionCopy,
  standaloneAgentAttributionText,
} from "@/lib/memory-attribution-copy";
import { AgentInlineIdentity } from "@/components/features/agent-inline-identity";
import { TopicChip } from "@/components/features/topic/topic-chip";
import { ArrowRight, Globe } from "lucide-react";
import { MemaxStar } from "@/components/features/memax-star";
import { formatAge } from "@/lib/format-age";

function hasMeaningfulUpdate(memory: Memory) {
  return (
    new Date(memory.updated_at).getTime() -
      new Date(memory.created_at).getTime() >
    60_000
  );
}

/**
 * hasProvenanceExtras — pure predicate used by the detail page to
 * decide whether the "Show details" ⋯ menu item should render. If
 * there are no extras to reveal, the control would be misleading.
 *
 * Kept in sync with the atoms rendered by ProvenanceStrip mode="extras":
 *   - agent link (hasAgent)
 *   - project_context.project
 *   - source_path file segment
 *   - meaningful updated age
 *   - dream_history entries
 */
export function hasProvenanceExtras(memory: Memory): boolean {
  if (memory.provenance?.created_by_slug || memory.source_agent) return true;
  if (memory.project_context?.project) return true;
  if (memory.source_path) return true;
  if (hasMeaningfulUpdate(memory)) return true;
  if ((memory.lifecycle?.dream_history?.length ?? 0) > 0) return true;
  return false;
}

/**
 * ActorGlyph — identity glyph shared with memory-row's
 * renderAuthorAvatar / renderAgentIdentity pattern. Chain:
 *   1. authorAvatar       → user photo
 *   2. agentIconEmoji     → agent emoji
 *   3. agentIdentity.icon → agent lucide glyph
 *   4. authorName         → initials bubble (known-but-avatarless author)
 *   5. agentDisplayName   → initials bubble (known-but-glyphless agent)
 *   6. Globe              → truly anonymous fallback (no author, no agent)
 *
 * Keep this chain in sync with memory-row.tsx (renderAuthorAvatar +
 * renderAgentIdentity) so rows and detail page read as one identity
 * system. Extract into a shared component when a third consumer lands.
 */
function ActorGlyph({
  attribution,
  iconColor,
  selfName,
  selfAvatar,
}: {
  attribution: ReturnType<typeof resolveMemoryAttribution>;
  iconColor: string;
  /** Logged-in user's display name — used for own-memory fallback. */
  selfName?: string | null;
  /** Logged-in user's avatar URL — used for own-memory fallback. */
  selfAvatar?: string | null;
}) {
  const AgentIcon = attribution.agentIdentity?.icon;
  const initialsClass =
    "flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full text-[9px] font-medium text-foreground/55";
  const initialsStyle: React.CSSProperties = {
    background: "oklch(from var(--foreground) l c h / 0.08)",
  };

  // Own-memory avatar takes precedence over the attribution fallbacks
  // — resolveMemoryAttribution leaves authorAvatar null for own memories
  // on purpose, so we inject the live user avatar here.
  if (selfAvatar) {
    return (
      <img
        src={selfAvatar}
        alt=""
        className="h-3.5 w-3.5 rounded-full shrink-0"
      />
    );
  }
  if (attribution.authorAvatar) {
    return (
      <img
        src={attribution.authorAvatar}
        alt=""
        className="h-3.5 w-3.5 rounded-full shrink-0"
      />
    );
  }
  if (attribution.agentIconEmoji) {
    return (
      <span className="flex h-3.5 w-3.5 shrink-0 items-center justify-center text-[12px] leading-none">
        {attribution.agentIconEmoji}
      </span>
    );
  }
  if (AgentIcon) {
    return (
      <AgentIcon
        className="h-3.5 w-3.5 shrink-0"
        style={{ color: iconColor }}
      />
    );
  }
  if (selfName) {
    return (
      <span className={initialsClass} style={initialsStyle}>
        {selfName[0]?.toUpperCase() ?? "Y"}
      </span>
    );
  }
  if (attribution.authorName) {
    return (
      <span className={initialsClass} style={initialsStyle}>
        {attribution.authorName[0]?.toUpperCase() ?? "?"}
      </span>
    );
  }
  if (attribution.hasAgent && attribution.agentDisplayName) {
    return (
      <span className={initialsClass} style={initialsStyle}>
        {attribution.agentDisplayName[0]?.toUpperCase() ?? "A"}
      </span>
    );
  }
  return <Globe className="h-3.5 w-3.5 shrink-0 text-fg-3" />;
}

/**
 * ProvenanceStrip — elevated provenance display for memory detail.
 *
 * Three modes:
 *  - "full" (default, back-compat): identity + extended atoms + age + recall
 *  - "compact": identity + captured age + recall count ONLY
 *  - "extras": ONLY the extended atoms (agent link · project · file ·
 *    updated age · dream history) — for use as a secondary strip below
 *    a compact band, without duplicating identity/age/recall already
 *    shown above.
 *
 * Agent-captured (full): [AgentIcon] AgentName captured · project · file · 2d ago · recalled 47×
 * Team member (compact): [Avatar] Derek · 2d ago · recalled 47×
 * Own-memory (compact):  [Avatar] You · 3h ago
 *
 * Activity signal: typographic weight scales with access_count:
 *   0 = hidden, 1-9 = fg-3, 10-49 = fg-2 medium, 50+ = fg-1 medium
 *
 * Kitchen reference: section 02, demo 2a (agent) and 2b (web), 2f (scaling).
 */
export function ProvenanceStrip({
  memory,
  mode = "full",
}: {
  memory: Memory;
  mode?: "full" | "compact" | "extras";
}) {
  const verbose = mode === "full";
  const extrasOnly = mode === "extras";
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { user } = useAuth();

  const attribution = resolveMemoryAttribution(memory, user?.id);
  const linkedCopy = linkedAgentAttributionCopy(attribution, t);
  // Own-memory identity fallback. resolveMemoryAttribution deliberately
  // leaves authorName/authorAvatar null for own memories (rows don't
  // need to say "you" redundantly), but the detail page is where
  // identity is the whole point of the strip — so we fill in the gap
  // from the logged-in user.
  const ownIdentityName =
    attribution.isOwnMemory && !attribution.hasAgent
      ? user?.display_name || user?.name || t.attribution.you
      : null;
  const ownIdentityAvatar =
    attribution.isOwnMemory && !attribution.hasAgent
      ? user?.avatar_url || null
      : null;
  // Agent glyph only renders when we actually have an agent identity.
  // Transport-only labels (web/CLI/API) are NOT surfaced on the detail
  // page — per memax 8eda95df, "we already have attribution model, I
  // don't think web and web icon should even be used here". Identity
  // is: author OR agent OR an anonymous stranger (rendered as a
  // neutral Globe with no text). Transport is telemetry; it belongs
  // in a debug affordance, not the reading surface.
  const AgentIcon = attribution.agentIdentity?.icon;
  const iconColor = attribution.agentIdentity?.color || "var(--fg-3)";
  // In the compact (non-verbose) form, an agent-captured memory still
  // shows the author name — but the "via {agent}" link is an extended
  // atom that belongs in the details expansion, not the header.
  const sourceLabel = attribution.authorName ? (
    attribution.hasAgent && verbose ? (
      <>
        <span>{attribution.authorName}</span>
        <span className="text-fg-4">·</span>
        <span className="text-fg-3">{linkedCopy.prefix}</span>
        <AgentInlineIdentity attribution={attribution} />
        {linkedCopy.suffix ? <span>{linkedCopy.suffix}</span> : null}
      </>
    ) : (
      attribution.authorName
    )
  ) : attribution.hasAgent ? (
    standaloneAgentAttributionText(attribution, t, interpolate, "regular")
  ) : (
    ownIdentityName
  );

  // Extended atoms — shown in "full" and "extras" modes, hidden in "compact".
  const showExtras = verbose || extrasOnly;
  const project = showExtras ? memory.project_context?.project : undefined;
  const file = showExtras ? memory.source_path?.split("/").pop() : undefined;
  const capturedAge = interpolate(t.note.capturedAt, {
    time: formatAge(memory.created_at, t, interpolate),
  });
  const updatedAge =
    showExtras && hasMeaningfulUpdate(memory)
      ? interpolate(t.note.updatedAt, {
          time: formatAge(memory.updated_at, t, interpolate),
        })
      : null;
  const count = memory.access_count || 0;

  // Activity signal: typographic weight scales with recall frequency
  const recallClass =
    count >= 50
      ? "text-fg-1 font-medium"
      : count >= 10
        ? "text-fg-2 font-medium"
        : "text-fg-3";

  // "extras" mode renders only the supplementary atoms — no identity
  // glyph, no authored name, no captured age, no recall count (all of
  // which already appear in the compact band above the disclosure).
  if (extrasOnly) {
    const hasAgentLink =
      attribution.hasAgent && (attribution.authorName || linkedCopy.prefix);
    const parts: Array<{ key: string; node: React.ReactNode }> = [];
    if (hasAgentLink) {
      parts.push({
        key: "agent",
        node: (
          <span className="inline-flex items-center gap-1 text-fg-3">
            <span>{linkedCopy.prefix}</span>
            <AgentInlineIdentity attribution={attribution} />
            {linkedCopy.suffix ? <span>{linkedCopy.suffix}</span> : null}
          </span>
        ),
      });
    }
    if (project) {
      parts.push({
        key: "project",
        node: <span className="text-fg-3">{project}</span>,
      });
    }
    if (file) {
      parts.push({
        key: "file",
        node: <span className="text-fg-3">{file}</span>,
      });
    }
    if (updatedAge) {
      parts.push({
        key: "updated",
        node: <span className="text-fg-3">{updatedAge}</span>,
      });
    }
    const hasDreams = (memory.lifecycle?.dream_history?.length ?? 0) > 0;
    if (parts.length === 0 && !hasDreams) return null;
    return (
      <div className="flex items-center flex-wrap gap-x-1.5 gap-y-0.5 text-[13px]">
        {parts.map((p, i) => (
          <React.Fragment key={p.key}>
            {i > 0 ? <span className="text-fg-4">·</span> : null}
            {p.node}
          </React.Fragment>
        ))}
        {hasDreams && memory.lifecycle?.dream_history && (
          <DreamHistoryLines entries={memory.lifecycle.dream_history} />
        )}
      </div>
    );
  }

  return (
    <div className="flex items-center flex-wrap gap-x-1.5 gap-y-0.5 text-[13px]">
      <ActorGlyph
        attribution={attribution}
        iconColor={iconColor}
        selfName={ownIdentityName}
        selfAvatar={ownIdentityAvatar}
      />
      {sourceLabel ? (
        <span
          className={
            attribution.hasAgent || attribution.authorName
              ? "text-fg-2 font-medium"
              : "text-fg-3"
          }
        >
          {sourceLabel}
        </span>
      ) : null}
      {project && (
        <>
          {sourceLabel ? <span className="text-fg-4">·</span> : null}
          <span className="text-fg-3">{project}</span>
        </>
      )}
      {file && (
        <>
          {(sourceLabel || project) && <span className="text-fg-4">·</span>}
          <span className="text-fg-3">{file}</span>
        </>
      )}
      {(sourceLabel || project || file) && <span className="text-fg-4">·</span>}
      <span className="text-fg-3">{capturedAge}</span>
      {updatedAge && (
        <>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">{updatedAge}</span>
        </>
      )}
      {count > 0 && (
        <>
          <span className="text-fg-4">·</span>
          <span className={`tabular-nums ${recallClass}`}>
            {interpolate(t.note.recalledN, { n: String(count) })}
          </span>
        </>
      )}
      {verbose &&
        memory.lifecycle?.dream_history &&
        memory.lifecycle.dream_history.length > 0 && (
          <DreamHistoryLines entries={memory.lifecycle.dream_history} />
        )}
    </div>
  );
}

/**
 * DreamHistoryLines — low-emphasis dream lineage for the memory detail
 * provenance strip. One section heading with a single ✦, then each entry
 * as a visual from → to transition using the shared TopicChip primitive.
 *
 * Entry shape: [from chip] <ArrowRight /> [to chip] · {age} · {reason?}
 *   - No per-line ✦ (the section heading carries it once).
 *   - from_topic absent (organize-from-unassigned) → renders an
 *     "Unassigned" pseudo-chip via TopicChip kind="unassigned".
 *   - to_topic absent (merge/archive) → renders only action-verb text
 *     without pills; reason carries the detail when present.
 *   - reason only renders when non-empty (empty is a valid, expected
 *     state — the from → to pills alone are sufficient for comprehension).
 *
 * Grammar matches the surrounding provenance row: 12px, text-fg-3,
 * dot separators. Reason at text-fg-4 (lightest) with max-w-xs truncate
 * and native title fallback for the full string.
 */
function DreamHistoryLines({ entries }: { entries: DreamActionRef[] }) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  return (
    <div className="basis-full mt-2 flex flex-col gap-1">
      {/* Section heading — single ✦ anchor for the entire block. Uses the
          memax signature star glyph (MemaxStar → ✦) for brand continuity
          with Dream bar, delta chips, and popover header. Not the lucide
          "sparkles" icon — that reads as a generic AI/intelligence mark. */}
      <div className="flex items-center gap-1.5 text-[12px] text-fg-2 font-medium">
        <MemaxStar
          className="text-[11px] shrink-0"
          style={{ color: "var(--signature)" }}
        />
        <span>{t.lifecycle.dreamHistoryHeading}</span>
      </div>
      {/* Entry lines — no per-line ✦. */}
      <div className="flex flex-col gap-0.5 text-[12px] text-fg-3">
        {entries.map((entry, i) => (
          <DreamHistoryEntry
            key={`${entry.run_id}-${entry.at}-${i}`}
            entry={entry}
            t={t}
            interpolate={interpolate}
          />
        ))}
      </div>
    </div>
  );
}

type LocaleT = ReturnType<typeof useLocale>["t"];
type InterpolateFn = ReturnType<typeof useInterpolate>;

function DreamHistoryEntry({
  entry,
  t,
  interpolate,
}: {
  entry: DreamActionRef;
  t: LocaleT;
  interpolate: InterpolateFn;
}) {
  const hasTo = !!entry.to_topic;
  const showPills = hasTo; // organize & restructure with to_topic
  // Organize without from_topic (from unassigned) uses the pseudo-chip.
  const fromIsUnassigned =
    entry.action_type === "organize" && !entry.from_topic;

  return (
    <div className="flex items-center flex-wrap gap-x-1.5 gap-y-1">
      {showPills ? (
        <>
          {fromIsUnassigned ? (
            <TopicChip kind="unassigned" label={t.lifecycle.unassigned} />
          ) : entry.from_topic ? (
            <TopicChip
              label={entry.from_topic.name}
              icon={entry.from_topic.icon}
            />
          ) : null}
          {(fromIsUnassigned || entry.from_topic) && (
            <ArrowRight className="h-3 w-3 shrink-0 text-fg-4" aria-hidden />
          )}
          {entry.to_topic && (
            <TopicChip label={entry.to_topic.name} icon={entry.to_topic.icon} />
          )}
        </>
      ) : (
        // merge / archive — no pill transition; show the verb inline.
        <span>{describeVerbOnly(entry, t)}</span>
      )}
      <span className="text-fg-4">·</span>
      <span className="tabular-nums">
        {formatAge(entry.at, t, interpolate)}
      </span>
      {entry.reason && (
        <>
          <span className="text-fg-4">·</span>
          <span className="text-fg-4 truncate max-w-xs" title={entry.reason}>
            {entry.reason}
          </span>
        </>
      )}
    </div>
  );
}

/**
 * describeVerbOnly — for action types that don't carry a from → to
 * transition (merge, archive). Returns the localized verb phrase only.
 * Organize + restructure with to_topic use the visual pill path above.
 *
 * "contradiction" is intentionally absent — the lifecycle resolver
 * filters contradictions out at the query layer; they stay in the
 * notification surface.
 */
function describeVerbOnly(action: DreamActionRef, t: LocaleT): string {
  const verbs = t.lifecycle.dreamActionVerb;
  switch (action.action_type) {
    case "merge":
      return verbs.merge;
    case "archive":
      return verbs.archive;
    case "restructure":
      return verbs.restructure;
    case "organize":
      return verbs.organize;
    default:
      return verbs.organize;
  }
}
