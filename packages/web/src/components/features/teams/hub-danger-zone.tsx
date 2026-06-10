"use client";

import { Section } from "@/components/features/teams/shared";
import type { Translations } from "@/i18n/locales/en";
import { useDestructiveAction } from "@/hooks/use-destructive-action";

export function HubDangerZone({
  t,
  hubName,
  memoryCount,
  memberCount,
  isOwner,
  isLastMember,
  onLeave,
  onDelete,
}: {
  t: Translations;
  hubName: string;
  memoryCount: number;
  memberCount: number;
  isOwner: boolean;
  isLastMember: boolean;
  onLeave: () => Promise<void>;
  onDelete: () => Promise<void>;
}) {
  const destructiveAction = useDestructiveAction<"leave" | "delete">();

  return (
    <Section title={t.userSettings.dangerZone}>
      {!isOwner ? (
        destructiveAction.isConfirming("leave") ? (
          <div
            className="flex items-center justify-between rounded-lg px-3.5 py-3"
            style={{
              backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
            }}
          >
            <span className="text-[14px] text-destructive/70">
              {t.hubs.leaveConfirm}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() =>
                  void destructiveAction.run("leave", () => onLeave())
                }
                disabled={destructiveAction.isRunning("leave")}
                className="text-[14px] text-destructive/70 hover:text-destructive font-medium cursor-pointer disabled:opacity-40"
              >
                {destructiveAction.isRunning("leave")
                  ? t.hubs.leaving
                  : t.hubs.leaveConfirmYes}
              </button>
              <button
                onClick={() => destructiveAction.cancel("leave")}
                disabled={destructiveAction.isRunning("leave")}
                className="text-[14px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:opacity-40"
              >
                {t.forget.keep}
              </button>
            </div>
          </div>
        ) : (
          <button
            onClick={() => destructiveAction.start("leave")}
            className="text-[14px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer"
          >
            {t.hubs.leaveHub}
          </button>
        )
      ) : destructiveAction.isConfirming("delete") ? (
        <div
          className="rounded-surface px-4 py-4"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
          }}
        >
          <p className="text-[14px] text-fg-2 mb-2">
            {t.hubs.deleteConfirmFull
              .replace("{name}", hubName)
              .replace("{memories}", String(memoryCount))
              .replace("{members}", String(Math.max(memberCount - 1, 0)))}
          </p>
          <p className="text-[13px] text-fg-3 mb-4">
            {t.hubs.deletePersonalSafe}
          </p>
          <div className="flex items-center gap-3">
            <button
              onClick={() =>
                void destructiveAction.run("delete", () => onDelete())
              }
              disabled={destructiveAction.isRunning("delete")}
              className="text-[14px] text-destructive/70 hover:text-destructive font-medium cursor-pointer disabled:opacity-40"
            >
              {destructiveAction.isRunning("delete")
                ? t.hubs.deleting
                : t.hubs.deleteForever}
            </button>
            <button
              onClick={() => destructiveAction.cancel("delete")}
              disabled={destructiveAction.isRunning("delete")}
              className="text-[14px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:opacity-40"
            >
              {t.forget.keep}
            </button>
          </div>
        </div>
      ) : (
        <div className="space-y-1.5">
          <button
            onClick={() => destructiveAction.start("delete")}
            className="text-[14px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer"
          >
            {t.hubs.deleteHub}
          </button>
          <p className="text-[13px] text-fg-4">{t.hubs.deleteHubDesc}</p>
          {isLastMember && (
            <p className="text-[13px] text-fg-4">{t.hubs.lastMemberDelete}</p>
          )}
        </div>
      )}
    </Section>
  );
}
