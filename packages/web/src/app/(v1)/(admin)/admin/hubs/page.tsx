"use client";

import { useState } from "react";
import Link from "next/link";
import { Search } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useAdminHubs, useSetHubPlan } from "@/hooks/use-admin-hubs";
import { useAdminPlans } from "@/hooks/use-admin-users";
import { useAdminCursor } from "@/hooks/use-admin-cursor";
import { AdminListFooter } from "@/components/features/admin/admin-list-footer";
import type { AdminTeamHub, HubSubscription } from "@/lib/admin-client";

// Grace window mirrored from server model.HubGracePeriod (7 days).
// Used to badge list rows for hubs whose subscription is past grace
// or otherwise inactive. Kept as a const to avoid importing anything
// server-side into the admin bundle.
const HUB_GRACE_MS = 7 * 24 * 60 * 60 * 1000;

function isHubFrozen(
  subscription: HubSubscription | null | undefined,
): boolean {
  if (!subscription) return false;
  if (subscription.status !== "active") return true;
  if (!subscription.over_limit_since) return false;
  const since = new Date(subscription.over_limit_since);
  if (Number.isNaN(since.getTime())) return false;
  // Strict > to mirror server model.IsHubFrozen.
  return Date.now() - since.getTime() > HUB_GRACE_MS;
}

export default function AdminHubsPage() {
  const { t } = useLocale();
  const [searchQuery, setSearchQuery] = useState("");
  const { cursor, hasPrev, goForward, goBack, reset } = useAdminCursor();

  const { data: listData, isLoading } = useAdminHubs({
    q: searchQuery || undefined,
    cursor,
    limit: 50,
  });
  const { data: plans } = useAdminPlans();
  const hubs = listData?.hubs ?? [];

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-6xl">
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-[20px] font-semibold text-fg-1">
          {t.admin.hubs.title}
        </h1>
        <p className="text-[14px] text-fg-3 mt-1">{t.admin.hubs.description}</p>
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-fg-4" />
          <input
            type="text"
            placeholder={t.admin.hubs.search}
            value={searchQuery}
            onChange={(e) => {
              setSearchQuery(e.target.value);
              reset();
            }}
            className="w-full rounded-lg border border-border/50 bg-card pl-9 pr-3 py-2 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-surface border border-border/50 bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-[14px] min-w-[480px]">
            <thead>
              <tr className="border-b border-border/30 text-fg-3 text-left">
                <th className="px-4 py-3 font-medium">
                  {t.admin.hubs.table.hub}
                </th>
                <th className="px-4 py-3 font-medium">
                  {t.admin.hubs.table.plan}
                </th>
                <th className="px-4 py-3 font-medium hidden md:table-cell">
                  {t.admin.hubs.table.seats}
                </th>
                <th className="px-4 py-3 font-medium hidden lg:table-cell">
                  {t.admin.hubs.table.created}
                </th>
              </tr>
            </thead>
            <tbody>
              {isLoading
                ? Array.from({ length: 6 }).map((_, i) => (
                    <tr
                      key={i}
                      className="border-b border-border/20 last:border-0"
                    >
                      <td className="px-4 py-3">
                        <div className="h-4 w-32 bg-surface-2 rounded animate-pulse" />
                      </td>
                      <td className="px-4 py-3">
                        <div className="h-5 w-16 bg-surface-2 rounded animate-pulse" />
                      </td>
                      <td className="px-4 py-3 hidden md:table-cell">
                        <div className="h-4 w-8 bg-surface-2 rounded animate-pulse" />
                      </td>
                      <td className="px-4 py-3 hidden lg:table-cell">
                        <div className="h-4 w-20 bg-surface-2 rounded animate-pulse" />
                      </td>
                    </tr>
                  ))
                : hubs.map((hub) => (
                    <HubRow key={hub.id} hub={hub} plans={plans ?? []} />
                  ))}
            </tbody>
          </table>
        </div>

        {!isLoading && hubs.length === 0 && (
          <div className="px-4 py-12 text-center text-fg-3 text-[14px]">
            {t.admin.hubs.empty}
          </div>
        )}
        {listData && (
          <AdminListFooter
            total={listData.total}
            hasPrev={hasPrev}
            hasNext={!!listData.next_cursor}
            onPrev={goBack}
            onNext={() => goForward(listData.next_cursor)}
          />
        )}
      </div>
    </div>
  );
}

/** Maps plan IDs (both legacy and scoped) to badge colors. */
const hubPlanColors: Record<string, string> = {
  // Legacy IDs
  free: "bg-surface-2 text-fg-3",
  "": "bg-surface-2 text-fg-3",
  team: "bg-emerald-500/15 text-emerald-400",
  enterprise: "bg-amber-500/15 text-amber-400",
  // Scoped hub IDs
  hub_free_team: "bg-surface-2 text-fg-3",
  hub_team: "bg-emerald-500/15 text-emerald-400",
  hub_enterprise: "bg-amber-500/15 text-amber-400",
  // Scoped personal IDs (shouldn't appear on hubs, but handle gracefully)
  personal_free: "bg-surface-2 text-fg-3",
  personal_early_access: "bg-violet-500/15 text-violet-400",
  personal_pro: "bg-blue-500/15 text-blue-400",
  personal_pro_plus: "bg-indigo-500/15 text-indigo-300",
};

function PlanBadge({ plan }: { plan: string }) {
  const { t } = useLocale();
  return (
    <span
      className={`inline-block rounded-md px-2 py-0.5 text-[12px] font-medium ${hubPlanColors[plan] ?? hubPlanColors.free}`}
    >
      {plan || t.admin.hubs.noPlan}
    </span>
  );
}

function HubRow({
  hub,
  plans,
}: {
  hub: AdminTeamHub;
  plans: Array<{ id: string; display_name: string; scope?: string }>;
}) {
  const { t } = useLocale();
  const setPlanMutation = useSetHubPlan();
  const [showPlanSelect, setShowPlanSelect] = useState(false);

  // Only show hub-scope plans in the hub plan dropdown
  const hubPlans = plans.filter((p) => p.scope === "hub");
  const subPlan = hub.subscription?.plan_id ?? hub.plan;
  const seats = hub.subscription?.seat_count ?? hub.member_count;
  const joinDate = hub.created_at
    ? new Date(hub.created_at).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
      })
    : "";

  return (
    <tr className="border-b border-border/20 last:border-0 hover:bg-surface-1 transition-colors">
      <td className="px-4 py-3">
        <Link href={`/admin/hubs/${hub.id}`} className="block min-w-0 group">
          <div className="flex items-center gap-2">
            <p className="text-[14px] text-fg-1 truncate group-hover:underline underline-offset-2">
              {hub.name}
            </p>
            {isHubFrozen(hub.subscription) && (
              <span className="inline-block rounded-md bg-red-500/15 text-red-400 px-1.5 py-0 text-[10px] font-medium uppercase tracking-wide">
                {t.admin.hubs.detail.quota.frozenTitle}
              </span>
            )}
          </div>
          <p className="text-[12px] text-fg-3 truncate">{hub.slug}</p>
        </Link>
      </td>
      <td className="px-4 py-3">
        {showPlanSelect ? (
          <Select
            value={subPlan}
            onValueChange={(v: string) => {
              setPlanMutation.mutate(
                { hubId: hub.id, planId: v },
                { onSettled: () => setShowPlanSelect(false) },
              );
            }}
            items={Object.fromEntries(
              hubPlans.map((p) => [p.id, p.display_name]),
            )}
          >
            <SelectTrigger className="py-1 px-2 rounded-md text-[12px]" />
            <SelectContent>
              {hubPlans.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.display_name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <button
            onClick={() => setShowPlanSelect(true)}
            className="cursor-pointer"
            title={t.admin.hubs.changePlan}
          >
            <PlanBadge plan={subPlan} />
          </button>
        )}
      </td>
      <td className="px-4 py-3 hidden md:table-cell tabular-nums text-fg-2">
        {seats}
      </td>
      <td className="px-4 py-3 hidden lg:table-cell text-fg-3 text-[13px]">
        {joinDate}
      </td>
    </tr>
  );
}
