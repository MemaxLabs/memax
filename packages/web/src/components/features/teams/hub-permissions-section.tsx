"use client";

import type { Translations } from "@/i18n/locales/en";
import { useUpdateHub } from "@/hooks/use-hub-management";
import {
  DELETE_POLICY_OPTIONS,
  Section,
  ToggleRow,
  TEAM_ACCENT,
} from "@/components/features/teams/shared";
import type { ContributorDeletePolicy, Hub } from "memax-sdk";

export function HubPermissionsSection({
  hubId,
  hub,
  updateHub,
  t,
}: {
  hubId: string;
  hub: Hub;
  updateHub: ReturnType<typeof useUpdateHub>;
  t: Translations;
}) {
  const currentPolicy: ContributorDeletePolicy =
    hub.contributor_delete_policy ?? "own";

  return (
    <Section title={t.hubs.permissions}>
      <div className="mb-4">
        <p className="text-[14px] text-fg-2 font-medium mb-1.5">
          {t.hubs.deletePolicy}
        </p>
        <div className="space-y-1">
          {DELETE_POLICY_OPTIONS.map((option) => {
            const isActive = currentPolicy === option.value;
            return (
              <button
                key={option.value}
                onClick={() =>
                  !isActive &&
                  updateHub.mutate({
                    hubId,
                    contributor_delete_policy: option.value,
                  })
                }
                className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-left transition-colors cursor-pointer ${
                  isActive
                    ? "bg-surface-1 ring-1 ring-border/60"
                    : "hover:bg-surface-1"
                }`}
              >
                <div
                  className="w-4 h-4 rounded-full border-2 shrink-0 flex items-center justify-center transition-colors"
                  style={{
                    borderColor: isActive
                      ? TEAM_ACCENT
                      : "oklch(from var(--foreground) l c h / 0.15)",
                  }}
                >
                  {isActive && (
                    <div
                      className="w-2 h-2 rounded-full"
                      style={{ backgroundColor: TEAM_ACCENT }}
                    />
                  )}
                </div>
                <div className="flex-1 min-w-0">
                  <span
                    className={`text-[14px] ${isActive ? "text-fg-1 font-medium" : "text-fg-2"}`}
                  >
                    {(t.hubs as Record<string, string>)[option.labelKey]}
                  </span>
                  <p className="text-[12px] text-fg-3 mt-0.5">
                    {(t.hubs as Record<string, string>)[option.descKey]}
                  </p>
                </div>
              </button>
            );
          })}
        </div>
      </div>

      <div className="border-t border-border/30 pt-3">
        <ToggleRow
          label={t.hubs.allowTopics}
          sublabel={t.hubs.allowTopicsDesc}
          checked={hub.allow_contributor_topics ?? true}
          onToggle={() =>
            updateHub.mutate({
              hubId,
              allow_contributor_topics: !(hub.allow_contributor_topics ?? true),
            })
          }
        />
        <ToggleRow
          label={t.hubs.allowDreams}
          sublabel={t.hubs.allowDreamsDesc}
          checked={hub.allow_contributor_dreams ?? true}
          onToggle={() =>
            updateHub.mutate({
              hubId,
              allow_contributor_dreams: !(hub.allow_contributor_dreams ?? true),
            })
          }
        />
      </div>
    </Section>
  );
}
