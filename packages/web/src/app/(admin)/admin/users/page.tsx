"use client";

import { useState } from "react";
import Link from "next/link";
import { Search, Eye } from "lucide-react";
import type { User } from "memax-sdk";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  useAdminUsers,
  useAdminStats,
  useSetUserPlan,
  useAdminPlans,
} from "@/hooks/use-admin-users";
import { useAdminCursor } from "@/hooks/use-admin-cursor";
import { AdminListFooter } from "@/components/features/admin/admin-list-footer";

export default function AdminUsersPage() {
  const { t } = useLocale();
  const [searchQuery, setSearchQuery] = useState("");
  const [planFilter, setPlanFilter] = useState("");
  const { cursor, hasPrev, goForward, goBack, reset } = useAdminCursor();

  const {
    data: listData,
    isLoading: listLoading,
    error: listError,
  } = useAdminUsers({
    q: searchQuery || undefined,
    plan: planFilter || undefined,
    cursor,
  });
  const { data: stats, isLoading: statsLoading } = useAdminStats();
  const { data: plans } = useAdminPlans();

  const users = listData?.users ?? [];

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-6xl">
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-[20px] font-semibold text-fg-1">
          {t.admin.users.title}
        </h1>
        <p className="text-[14px] text-fg-3 mt-1">
          {t.admin.users.description}
        </p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 mb-6">
        {statsLoading ? (
          Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="rounded-surface border border-border/50 bg-card px-4 py-3"
            >
              <div className="h-3 w-12 bg-surface-2 rounded animate-pulse mb-2" />
              <div className="h-6 w-8 bg-surface-2 rounded animate-pulse" />
            </div>
          ))
        ) : stats ? (
          <>
            <StatCard
              label={t.admin.users.stats.total}
              value={stats.total_users}
              accent
            />
            <StatCard
              label={t.admin.users.stats.memories}
              value={stats.total_memories}
            />
            <StatCard
              label={t.admin.users.stats.activeToday}
              value={stats.active_today}
            />
            {Object.entries(stats.users_by_plan).map(([plan, count]) => (
              <StatCard key={plan} label={plan} value={count} />
            ))}
          </>
        ) : null}
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-fg-4" />
          <input
            type="text"
            placeholder={t.admin.users.search}
            value={searchQuery}
            onChange={(e) => {
              setSearchQuery(e.target.value);
              reset();
            }}
            className="w-full rounded-lg border border-border/50 bg-card pl-9 pr-3 py-2 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
          />
        </div>
        {(() => {
          const personalPlans = (plans ?? []).filter(
            (p) => !("scope" in p) || p.scope === "personal",
          );
          const items: Record<string, string> = {
            "": t.admin.users.allPlans,
          };
          for (const p of personalPlans) items[p.id] = p.display_name;
          return (
            <Select
              value={planFilter}
              onValueChange={(v: string) => {
                setPlanFilter(v);
                reset();
              }}
              items={items}
            >
              <SelectTrigger
                placeholder={t.admin.users.allPlans}
                className="w-44"
              />
              <SelectContent>
                <SelectItem value="">{t.admin.users.allPlans}</SelectItem>
                {personalPlans.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.display_name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          );
        })()}
      </div>

      {/* Table */}
      <div className="rounded-surface border border-border/50 bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-[14px] min-w-[540px]">
            <thead>
              <tr className="border-b border-border/30 text-fg-3 text-left">
                <th className="px-4 py-3 font-medium">
                  {t.admin.users.table.user}
                </th>
                <th className="px-4 py-3 font-medium">
                  {t.admin.users.table.plan}
                </th>
                <th className="px-4 py-3 font-medium hidden lg:table-cell">
                  {t.admin.users.table.created}
                </th>
                <th className="px-4 py-3 font-medium w-20">
                  {t.admin.users.table.actions}
                </th>
              </tr>
            </thead>
            <tbody>
              {listLoading
                ? Array.from({ length: 8 }).map((_, i) => (
                    <tr
                      key={i}
                      className="border-b border-border/20 last:border-0"
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-3">
                          <div className="h-8 w-8 rounded-full bg-surface-2 animate-pulse" />
                          <div>
                            <div className="h-4 w-32 bg-surface-2 rounded animate-pulse mb-1" />
                            <div className="h-3 w-40 bg-surface-2 rounded animate-pulse" />
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="h-5 w-16 bg-surface-2 rounded animate-pulse" />
                      </td>
                      <td className="px-4 py-3 hidden lg:table-cell">
                        <div className="h-4 w-20 bg-surface-2 rounded animate-pulse" />
                      </td>
                      <td className="px-4 py-3">
                        <div className="h-4 w-4 bg-surface-2 rounded animate-pulse" />
                      </td>
                    </tr>
                  ))
                : users.map((user) => (
                    <UserRow key={user.id} user={user} plans={plans ?? []} />
                  ))}
            </tbody>
          </table>
        </div>

        {/* Footer */}
        {!listLoading && listError && (
          <div className="px-4 py-12 text-center text-[14px]">
            <p className="text-red-400">{t.admin.users.loadError}</p>
            <p className="text-fg-4 text-[12px] mt-1">
              {listError instanceof Error
                ? listError.message
                : String(listError)}
            </p>
          </div>
        )}
        {!listLoading && !listError && users.length === 0 && (
          <div className="px-4 py-12 text-center text-fg-3 text-[14px]">
            {t.admin.users.empty}
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

// --- Sub-components ---

function StatCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent?: boolean;
}) {
  return (
    <div className="rounded-surface border border-border/50 bg-card px-4 py-3">
      <p className="text-[12px] text-fg-3 mb-1 capitalize">{label}</p>
      <p
        className={`text-xl font-semibold tabular-nums ${accent ? "text-fg-1" : "text-fg-2"}`}
      >
        {value.toLocaleString()}
      </p>
    </div>
  );
}

/** Maps plan IDs (both legacy and scoped) to badge colors. */
const planColors: Record<string, string> = {
  // Legacy IDs
  free: "bg-surface-2 text-fg-3",
  early_access: "bg-violet-500/15 text-violet-400",
  pro: "bg-blue-500/15 text-blue-400",
  pro_plus: "bg-indigo-500/15 text-indigo-300",
  team: "bg-emerald-500/15 text-emerald-400",
  enterprise: "bg-amber-500/15 text-amber-400",
  // Scoped personal IDs
  personal_free: "bg-surface-2 text-fg-3",
  personal_early_access: "bg-violet-500/15 text-violet-400",
  personal_pro: "bg-blue-500/15 text-blue-400",
  personal_pro_plus: "bg-indigo-500/15 text-indigo-300",
  // Scoped hub IDs
  hub_free_team: "bg-surface-2 text-fg-3",
  hub_team: "bg-emerald-500/15 text-emerald-400",
  hub_enterprise: "bg-amber-500/15 text-amber-400",
};

function PlanBadge({ plan }: { plan: string }) {
  return (
    <span
      className={`inline-block rounded-md px-2 py-0.5 text-[12px] font-medium ${planColors[plan] ?? planColors.free}`}
    >
      {plan}
    </span>
  );
}

function UserRow({
  user,
  plans,
}: {
  user: User;
  plans: Array<{ id: string; display_name: string; scope?: string }>;
}) {
  const { t } = useLocale();
  const setPlanMutation = useSetUserPlan();
  const [showPlanSelect, setShowPlanSelect] = useState(false);

  // Only show personal-scope plans in the user plan dropdown
  const personalPlans = plans.filter((p) => !p.scope || p.scope === "personal");
  const displayPlan = user.personal_plan_id || user.plan;
  const displayName = user.display_name || user.name || user.email;
  const joinDate = user.created_at
    ? new Date(user.created_at).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
      })
    : "";

  return (
    <tr className="border-b border-border/20 last:border-0 hover:bg-surface-1 transition-colors">
      <td className="px-4 py-3">
        {/* Entire username cell navigates to the user detail page —
            matches the HubRow pattern in /admin/hubs so operators
            don't need to hunt for the eye icon in the actions
            column. */}
        <Link
          href={`/admin/users/${user.id}`}
          className="flex items-center gap-3 min-w-0 group"
        >
          {user.avatar_url ? (
            <img
              src={user.avatar_url}
              alt=""
              className="h-8 w-8 rounded-full bg-surface-2"
            />
          ) : (
            <div className="h-8 w-8 rounded-full bg-surface-2 flex items-center justify-center text-[12px] text-fg-3 font-medium">
              {displayName.charAt(0).toUpperCase()}
            </div>
          )}
          <div className="min-w-0">
            <p className="text-[14px] text-fg-1 truncate group-hover:underline underline-offset-2">
              {displayName}
            </p>
            <p className="text-[12px] text-fg-3 truncate">{user.email}</p>
          </div>
        </Link>
      </td>
      <td className="px-4 py-3">
        {showPlanSelect ? (
          <Select
            value={displayPlan}
            onValueChange={(v: string) => {
              setPlanMutation.mutate(
                { userId: user.id, planId: v },
                { onSettled: () => setShowPlanSelect(false) },
              );
            }}
            items={Object.fromEntries(
              personalPlans.map((p) => [p.id, p.display_name]),
            )}
          >
            <SelectTrigger className="py-1 px-2 rounded-md text-[12px]" />
            <SelectContent>
              {personalPlans.map((p) => (
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
            title={t.admin.users.actions.changePlan}
          >
            <PlanBadge plan={displayPlan} />
          </button>
        )}
      </td>
      <td className="px-4 py-3 hidden lg:table-cell text-fg-3 text-[13px]">
        {joinDate}
      </td>
      <td className="px-4 py-3">
        <Link
          href={`/admin/users/${user.id}`}
          className="inline-flex items-center justify-center h-7 w-7 rounded-md text-fg-3 hover:text-fg-1 hover:bg-surface-2 transition-colors"
          title={t.admin.users.actions.viewDetails}
        >
          <Eye className="h-4 w-4" />
        </Link>
      </td>
    </tr>
  );
}
