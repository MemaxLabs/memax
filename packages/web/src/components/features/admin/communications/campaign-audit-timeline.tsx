"use client";

import { useLocale } from "@/i18n";
import type { AdminCommsAuditEntry } from "@/lib/admin-client";

export function CampaignAuditTimeline({
  entries,
}: {
  entries: AdminCommsAuditEntry[];
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.audit;

  if (entries.length === 0) {
    return <p className="text-[13px] text-fg-2">{copy.empty}</p>;
  }

  return (
    <ol className="space-y-3">
      {entries.map((entry) => {
        const actionLabel =
          (copy.actions as Record<string, string>)[entry.action] ??
          entry.action;
        return (
          <li
            key={entry.id}
            className="flex gap-3 rounded-surface border border-border/50 bg-surface-1 px-4 py-3"
          >
            <div className="mt-1 h-2 w-2 shrink-0 rounded-full bg-fg-1" />
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between gap-3">
                <p className="text-[13px] font-medium text-fg-1">
                  {actionLabel}
                </p>
                <time className="text-[11.5px] tabular-nums text-fg-3">
                  {new Date(entry.created_at).toLocaleString()}
                </time>
              </div>
              {entry.actor_id ? (
                <p className="mt-0.5 text-[12px] text-fg-2">
                  {copy.actorPrefix.replace("{id}", entry.actor_id.slice(0, 8))}
                </p>
              ) : (
                <p className="mt-0.5 text-[12px] text-fg-3">
                  {copy.systemActor}
                </p>
              )}
              {Object.keys(entry.metadata ?? {}).length > 0 ? (
                <pre className="mt-2 overflow-x-auto whitespace-pre-wrap rounded-md bg-surface-2 px-2 py-1.5 text-[11.5px] leading-relaxed text-fg-2">
                  {JSON.stringify(entry.metadata, null, 2)}
                </pre>
              ) : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
