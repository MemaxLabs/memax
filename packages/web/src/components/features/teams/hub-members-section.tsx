"use client";

import { useState } from "react";
import { ChevronDown, X } from "lucide-react";
import { formatAge } from "@/lib/format-age";
import { useDestructiveAction } from "@/hooks/use-destructive-action";
import { pluralize } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import type { HubMember } from "memax-sdk";
import {
  MANAGED_ROLE_OPTIONS,
  ROLE_COLORS,
  Section,
  type HubRole,
} from "@/components/features/teams/shared";

export function HubMembersSection({
  members,
  currentUserId,
  canManage,
  t,
  interpolate,
  onUpdateRole,
  onRemoveMember,
}: {
  members: HubMember[];
  currentUserId?: string;
  canManage: boolean;
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
  onUpdateRole: (userId: string, role: Exclude<HubRole, "owner">) => void;
  onRemoveMember: (userId: string) => Promise<unknown>;
}) {
  const destructiveAction = useDestructiveAction<string>();
  const [editingRoleUserId, setEditingRoleUserId] = useState<string | null>(
    null,
  );

  return (
    <Section
      title={pluralize(t.hubs.memberOne, t.hubs.members, members.length)}
    >
      <div className="rounded-surface border border-border/60 overflow-hidden">
        {members.map((member, i) => {
          const isMe = member.user_id === currentUserId;
          const removeKey = `remove:${member.user_id}`;
          const isRemoving = destructiveAction.isConfirming(removeKey);
          const isRemovingPending = destructiveAction.isRunning(removeKey);
          const isEditingRole = editingRoleUserId === member.user_id;
          const canRemove = canManage && !isMe && member.role !== "owner";
          const canEditRole = canManage && !isMe && member.role !== "owner";

          if (isRemoving) {
            return (
              <div
                key={member.user_id}
                className={`flex items-center justify-between px-4 py-3 transition-colors ${
                  i > 0 ? "border-t border-border/30" : ""
                }`}
                style={{
                  backgroundColor:
                    "oklch(from var(--destructive) l c h / 0.06)",
                }}
              >
                <span className="text-[13px] text-fg-2">
                  {interpolate(t.hubs.removeMemberConfirm, {
                    name: member.user_name,
                  })}
                </span>
                <div className="flex items-center gap-2.5 shrink-0">
                  <button
                    onClick={() =>
                      void destructiveAction.run(removeKey, () =>
                        onRemoveMember(member.user_id).then(() => undefined),
                      )
                    }
                    disabled={isRemovingPending}
                    className="text-[13px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
                  >
                    {isRemovingPending ? t.hubs.removing : t.hubs.remove}
                  </button>
                  <button
                    onClick={() => destructiveAction.cancel(removeKey)}
                    disabled={isRemovingPending}
                    className="text-[13px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
                  >
                    {t.forget.keep}
                  </button>
                </div>
              </div>
            );
          }

          return (
            <div
              key={member.user_id}
              className={`flex items-center gap-3 px-4 py-3 ${
                i > 0 ? "border-t border-border/30" : ""
              }`}
            >
              <div
                className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0 overflow-hidden"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                {member.user_avatar_url ? (
                  <img
                    src={member.user_avatar_url}
                    alt=""
                    className="h-7 w-7 rounded-full"
                  />
                ) : (
                  (member.user_name?.[0]?.toUpperCase() ?? "?")
                )}
              </div>
              <span className="text-[15px] text-fg-2 font-medium flex-1 truncate">
                {member.user_name}
                {isMe && (
                  <span className="text-fg-4 text-[13px] ml-1.5">
                    {t.hubs.you}
                  </span>
                )}
              </span>
              <span className="text-[13px] text-fg-4 tabular-nums shrink-0">
                {formatAge(member.joined_at, t, interpolate)}
              </span>
              {isEditingRole ? (
                <div className="flex items-center gap-1 shrink-0">
                  {MANAGED_ROLE_OPTIONS.map((role) => (
                    <button
                      key={role}
                      onClick={() => {
                        onUpdateRole(member.user_id, role);
                        setEditingRoleUserId(null);
                      }}
                      className={`text-[12px] px-2 py-0.5 rounded-chrome transition-colors cursor-pointer ${
                        member.role === role
                          ? "bg-foreground text-background"
                          : "bg-surface-2 text-fg-3 hover:text-fg-2"
                      }`}
                    >
                      {(t.hubs as Record<string, string>)[role] ?? role}
                    </button>
                  ))}
                  <button
                    onClick={() => setEditingRoleUserId(null)}
                    className="p-0.5 text-fg-4 hover:text-fg-2 transition-colors cursor-pointer"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ) : (
                <div className="flex items-center gap-2 shrink-0">
                  {canEditRole ? (
                    <button
                      onClick={() => setEditingRoleUserId(member.user_id)}
                      className={`inline-flex items-center gap-1 text-[12px] px-2 py-0.5 rounded-chrome cursor-pointer transition-colors ${ROLE_COLORS[member.role as HubRole]}`}
                    >
                      {(t.hubs as Record<string, string>)[member.role] ??
                        member.role}
                      <ChevronDown className="h-3 w-3" />
                    </button>
                  ) : (
                    <span
                      className={`text-[12px] px-2 py-0.5 rounded-chrome ${ROLE_COLORS[member.role as HubRole]}`}
                    >
                      {(t.hubs as Record<string, string>)[member.role] ??
                        member.role}
                    </span>
                  )}
                  {canRemove && (
                    <button
                      onClick={() => destructiveAction.start(removeKey)}
                      className="text-[12px] text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer"
                    >
                      {t.hubs.remove}
                    </button>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </Section>
  );
}
