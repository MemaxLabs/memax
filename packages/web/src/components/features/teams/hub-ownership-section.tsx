"use client";

import type { Translations } from "@/i18n/locales/en";
import type { HubMember, HubOwnershipTransfer } from "memax-sdk";
import { formatDate, Section } from "@/components/features/teams/shared";

export function HubOwnershipSection({
  t,
  pendingTransfer,
  isOwner,
  isTransferTarget,
  members,
  onCreate,
  onAccept,
  onCancel,
}: {
  t: Translations;
  pendingTransfer: HubOwnershipTransfer | null;
  isOwner: boolean;
  isTransferTarget: boolean;
  members: HubMember[];
  onCreate: (targetUserId: string) => void;
  onAccept: () => void;
  onCancel: () => void;
}) {
  return (
    <Section title={t.hubs.transferTitle}>
      {pendingTransfer ? (
        <div className="rounded-surface border border-border/60 px-4 py-4 space-y-3">
          <div>
            <p className="text-[14px] font-medium text-fg-2">
              {t.hubs.transferPending}
            </p>
            <p className="text-[13px] text-fg-3 mt-1">
              {t.hubs.transferPendingDesc.replace(
                "{name}",
                pendingTransfer.target_user_name ||
                  pendingTransfer.target_user_email ||
                  t.hubs.team,
              )}
            </p>
            <p className="text-[12px] text-fg-4 mt-1">
              {t.hubs.expiresOn.replace(
                "{date}",
                formatDate(pendingTransfer.expires_at),
              )}
            </p>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            {isTransferTarget && (
              <button
                onClick={onAccept}
                className="text-[13px] font-medium bg-surface-3 text-fg-1 px-3 py-1.5 rounded-lg cursor-pointer"
              >
                {t.hubs.transferAccept}
              </button>
            )}
            <button
              onClick={onCancel}
              className="text-[13px] text-fg-3 hover:text-fg-2 px-3 py-1.5 rounded-lg bg-surface-1 hover:bg-surface-2 transition-colors cursor-pointer"
            >
              {isTransferTarget
                ? t.hubs.transferDecline
                : t.hubs.transferCancel}
            </button>
          </div>
        </div>
      ) : isOwner ? (
        members.length > 0 ? (
          <div className="space-y-2">
            <p className="text-[13px] text-fg-3">{t.hubs.transferEmpty}</p>
            {members.map((member) => (
              <button
                key={member.user_id}
                onClick={() => onCreate(member.user_id)}
                className="w-full flex items-center gap-3 rounded-surface border border-border/60 px-4 py-3 text-left hover:bg-surface-1 transition-colors cursor-pointer"
              >
                <div
                  className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  {member.user_name?.[0]?.toUpperCase() ?? "?"}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-[14px] text-fg-2 font-medium truncate">
                    {member.user_name}
                  </p>
                  <p className="text-[12px] text-fg-3">
                    {(t.hubs as Record<string, string>)[member.role] ??
                      member.role}
                  </p>
                </div>
                <span className="text-[13px] text-fg-3">
                  {t.hubs.transferInitiate}
                </span>
              </button>
            ))}
          </div>
        ) : (
          <p className="text-[13px] text-fg-3">{t.hubs.noTransferTargets}</p>
        )
      ) : null}
    </Section>
  );
}
