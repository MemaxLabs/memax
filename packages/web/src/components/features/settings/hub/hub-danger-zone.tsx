"use client";

import { Trash2 } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { pluralize, useLocale } from "@/i18n";
import {
  useHubDetail,
  useLeaveHub,
  useDeleteHub,
} from "@/hooks/use-hub-management";
import { useSettingsDialog } from "@/contexts/settings-dialog-context";
import { useDestructiveAction } from "@/hooks/use-destructive-action";
import { useBarToast } from "@/hooks/use-bar-toast";
import { getMemaxClient } from "@/lib/memax-client";
import {
  useMemories,
  memoriesTotalCount,
  flattenMemories,
  memoryListQueryPrefix,
} from "@/hooks/use-memories";
import { HubDangerZone as HubDangerZoneComponent } from "@/components/features/teams/hub-danger-zone";
import { Section } from "../shared/section";
import type { HubRole } from "@/components/features/teams/shared";

export function HubDangerZoneSection({ hubId }: { hubId: string }) {
  const { hubs, refreshProfile } = useAuth();
  const hubWithRole = hubs.find((h) => h.hub.id === hubId);
  if (!hubWithRole) return null;

  if (hubWithRole.hub.hub_type === "personal") {
    return <PersonalHubDangerZone />;
  }

  return <TeamHubDangerZone hubId={hubId} />;
}

function PersonalHubDangerZone() {
  const { t } = useLocale();
  const toast = useBarToast();
  const forgetAllAction = useDestructiveAction<"forget-all">();
  const { data: memoriesPages } = useMemories();
  const memories = flattenMemories(memoriesPages);
  const memoryCount = memoriesTotalCount(memoriesPages) || memories.length;

  const handleForgetAll = async () => {
    await forgetAllAction.run(
      "forget-all",
      async () => {
        await getMemaxClient().account.deleteAllData();
        const { queryClient } = await import("@/lib/query-client");
        queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
        queryClient.invalidateQueries({ queryKey: ["topics"] });
        queryClient.invalidateQueries({ queryKey: ["dreams"] });
        queryClient.invalidateQueries({ queryKey: ["reviews"] });
        queryClient.invalidateQueries({ queryKey: ["agent-configs"] });
      },
      {
        onError: (err) => {
          console.error("Failed to delete all data:", err);
          toast.error(t.toast.deleteFailed);
        },
      },
    );
  };

  return (
    <Section title={t.userSettings.dangerZone}>
      {forgetAllAction.isConfirming("forget-all") ? (
        <div
          className="flex items-center justify-between rounded-lg px-3.5 py-3 transition-colors"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
          }}
        >
          <span className="text-[14px] text-fg-2">
            {forgetAllAction.isRunning("forget-all")
              ? pluralize(
                  t.forget.allForgettingOne,
                  t.forget.allForgetting,
                  memoryCount,
                )
              : pluralize(
                  t.forget.allConfirmOne,
                  t.forget.allConfirm,
                  memoryCount,
                )}
          </span>
          {!forgetAllAction.isRunning("forget-all") && (
            <div className="flex items-center gap-3">
              <button
                onClick={handleForgetAll}
                disabled={forgetAllAction.isRunning("forget-all")}
                className="text-[14px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
              >
                {t.forget.button}
              </button>
              <button
                onClick={() => forgetAllAction.cancel("forget-all")}
                disabled={forgetAllAction.isRunning("forget-all")}
                className="text-[14px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
              >
                {t.forget.keep}
              </button>
            </div>
          )}
        </div>
      ) : (
        <button
          onClick={() => forgetAllAction.start("forget-all")}
          className="w-full flex items-center gap-2.5 px-3.5 py-3 rounded-lg text-[15px] text-fg-2 hover:text-destructive/70 hover:bg-destructive/5 transition-colors cursor-pointer"
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t.forget.everything}
        </button>
      )}
    </Section>
  );
}

function TeamHubDangerZone({ hubId }: { hubId: string }) {
  // All hook calls run unconditionally (rules-of-hooks). The
  // `if (!hubWithRole) return null` early return used to sit
  // between `useAuth` and `useHubDetail`, which meant flipping
  // hubs mid-session changed the hook count across renders and
  // tripped React's "Rendered fewer hooks" crash. Hoist every
  // hook above the early return and keep the render guard pure.
  const { hubs, refreshProfile } = useAuth();
  const { t } = useLocale();
  const { close } = useSettingsDialog();
  const leaveHub = useLeaveHub();
  const deleteHub = useDeleteHub();
  const { data: hubDetail } = useHubDetail(hubId);

  const hubWithRole = hubs.find((h) => h.hub.id === hubId);
  if (!hubWithRole) return null;

  const { hub, role, memory_count } = hubWithRole;
  const myRole = role as HubRole;
  const isOwner = myRole === "owner";

  const memberCount = hubDetail?.members?.length ?? 1;
  const isLastMember = memberCount <= 1;

  const handleLeave = async () => {
    try {
      await leaveHub.mutateAsync(hubId);
      await refreshProfile().catch(() => {});
      close();
    } catch {}
  };

  const handleDelete = async () => {
    await deleteHub.mutateAsync(hubId);
    await refreshProfile().catch(() => {});
    close();
  };

  return (
    <HubDangerZoneComponent
      t={t}
      hubName={hub.name}
      memoryCount={memory_count ?? 0}
      memberCount={memberCount}
      isOwner={isOwner}
      isLastMember={isLastMember}
      onLeave={handleLeave}
      onDelete={handleDelete}
    />
  );
}
