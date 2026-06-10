"use client";

import { use, useState } from "react";
import Link from "next/link";
import { ArrowLeft, Search } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  useAdminHubDetail,
  useAdminHubMembers,
  useSetHubPlan,
} from "@/hooks/use-admin-hubs";
import { useAdminPlans } from "@/hooks/use-admin-users";
import { useAdminCursor } from "@/hooks/use-admin-cursor";
import { AdminListFooter } from "@/components/features/admin/admin-list-footer";
import type { AdminHubMember } from "@/lib/admin-client";
import { QuotaBanner } from "@/components/features/admin/hubs/quota-banner";

export default function AdminHubDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { t } = useLocale();
  const copy = t.admin.hubs;
  const { data, isLoading } = useAdminHubDetail(id);
  const { data: plans } = useAdminPlans();
  const setPlanMutation = useSetHubPlan();
  const [memberSearch, setMemberSearch] = useState("");
  const {
    cursor: memberCursor,
    hasPrev: membersHasPrev,
    goForward: membersGoForward,
    goBack: membersGoBack,
    reset: resetMembers,
  } = useAdminCursor();
  const { data: memberData, isLoading: membersLoading } = useAdminHubMembers(
    id,
    {
      q: memberSearch || undefined,
      cursor: memberCursor,
      limit: 50,
    },
  );

  if (isLoading) {
    return (
      <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-5xl">
        <div className="animate-pulse space-y-4">
          <div className="h-6 w-48 bg-surface-2 rounded" />
          <div className="h-32 bg-surface-2 rounded-surface" />
          <div className="h-64 bg-surface-2 rounded-surface" />
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-5xl">
        <p className="text-fg-3">{copy.detail.notFound}</p>
      </div>
    );
  }

  const { hub, owner, subscription, plan, member_count, memory_count } = data;
  const hubPlans = (plans ?? []).filter((p) => p.scope === "hub");
  const currentPlanId = subscription?.plan_id ?? hub.plan;
  const seats = subscription?.seat_count ?? member_count;
  const members = memberData?.members ?? [];

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-5xl">
      <Link
        href="/admin/hubs"
        className="inline-flex items-center gap-1.5 text-[13px] text-fg-3 hover:text-fg-2 mb-6 transition-colors"
      >
        <ArrowLeft className="h-3.5 w-3.5" />
        {copy.title}
      </Link>

      <QuotaBanner
        subscription={subscription}
        memoryCount={memory_count}
        memoryLimit={plan?.memory_limit}
      />

      {/* Header card */}
      <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h1 className="text-[20px] font-semibold text-fg-1 truncate">
              {hub.name}
            </h1>
            <p className="text-[13px] text-fg-3 font-mono truncate">
              {hub.slug} · {hub.id}
            </p>
            {owner && (
              <p className="text-[13px] text-fg-3 mt-2">
                {copy.detail.owner}:{" "}
                <Link
                  href={`/admin/users/${owner.id}`}
                  className="text-fg-2 hover:text-fg-1 underline underline-offset-2 decoration-border"
                >
                  {owner.display_name || owner.name || owner.email}
                </Link>
              </p>
            )}
          </div>
        </div>

        {/* Stats */}
        <div className="mt-5 grid grid-cols-2 sm:grid-cols-4 gap-3">
          <StatCell label={copy.detail.stats.members} value={member_count} />
          <StatCell label={copy.detail.stats.memories} value={memory_count} />
          <StatCell label={copy.detail.stats.seats} value={seats} />
          <StatCell
            label={copy.detail.stats.plan}
            value={plan?.display_name ?? currentPlanId ?? copy.noPlan}
          />
        </div>
      </div>

      {/* Plan + subscription card */}
      <div className="rounded-surface border border-border/50 bg-card p-6 mb-4">
        <h2 className="text-[14px] font-medium text-fg-2 mb-3">
          {copy.subscription}
        </h2>
        <div className="flex items-center gap-3 flex-wrap">
          <span className="text-[13px] text-fg-3">
            {copy.detail.stats.plan}:
          </span>
          <Select
            value={currentPlanId}
            onValueChange={(v: string) =>
              setPlanMutation.mutate({ hubId: hub.id, planId: v })
            }
            items={Object.fromEntries(
              hubPlans.map((p) => [p.id, p.display_name]),
            )}
          >
            <SelectTrigger className="w-56" />
            <SelectContent>
              {hubPlans.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.display_name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {subscription && (
            <span
              className={`inline-block rounded-md px-2 py-0.5 text-[12px] font-medium ${
                subscription.status === "active"
                  ? "bg-emerald-500/15 text-emerald-400"
                  : "bg-amber-500/15 text-amber-400"
              }`}
            >
              {copy.status[subscription.status as keyof typeof copy.status] ??
                subscription.status}
            </span>
          )}
        </div>
      </div>

      {/* Members */}
      <div className="rounded-surface border border-border/50 bg-card overflow-hidden">
        <div className="flex items-center gap-3 px-4 py-3 border-b border-border/30">
          <h2 className="text-[14px] font-medium text-fg-2">
            {copy.detail.members.title}
          </h2>
          <div className="relative flex-1 max-w-sm ml-auto">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-fg-4" />
            <input
              type="text"
              placeholder={copy.detail.members.search}
              value={memberSearch}
              onChange={(e) => {
                setMemberSearch(e.target.value);
                resetMembers();
              }}
              className="w-full rounded-lg border border-border/50 bg-background pl-9 pr-3 py-1.5 text-[13px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-fg-3 transition-colors"
            />
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-[14px] min-w-[480px]">
            <thead>
              <tr className="border-b border-border/30 text-fg-3 text-left">
                <th className="px-4 py-2.5 font-medium">
                  {t.admin.users.table.user}
                </th>
                <th className="px-4 py-2.5 font-medium">
                  {copy.detail.members.role}
                </th>
                <th className="px-4 py-2.5 font-medium hidden md:table-cell">
                  {copy.detail.members.joined}
                </th>
              </tr>
            </thead>
            <tbody>
              {membersLoading
                ? Array.from({ length: 4 }).map((_, i) => (
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
                        <div className="h-4 w-12 bg-surface-2 rounded animate-pulse" />
                      </td>
                      <td className="px-4 py-3 hidden md:table-cell">
                        <div className="h-4 w-20 bg-surface-2 rounded animate-pulse" />
                      </td>
                    </tr>
                  ))
                : members.map((m) => <MemberRow key={m.user_id} member={m} />)}
            </tbody>
          </table>
        </div>
        {!membersLoading && members.length === 0 && (
          <div className="px-4 py-8 text-center text-fg-3 text-[14px]">
            {copy.detail.members.empty}
          </div>
        )}
        {memberData && (
          <AdminListFooter
            total={memberData.total}
            hasPrev={membersHasPrev}
            hasNext={!!memberData.next_cursor}
            onPrev={membersGoBack}
            onNext={() => membersGoForward(memberData.next_cursor)}
          />
        )}
      </div>
    </div>
  );
}

function StatCell({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-lg border border-border/40 bg-surface-1/50 px-4 py-3">
      <div className="text-[11px] text-fg-3 mb-1">{label}</div>
      <div className="text-[18px] font-semibold text-fg-1 tabular-nums truncate">
        {value}
      </div>
    </div>
  );
}

function MemberRow({ member }: { member: AdminHubMember }) {
  const displayName = member.user_name || member.user_email || member.user_id;
  const joined = member.joined_at
    ? new Date(member.joined_at).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
      })
    : "";
  return (
    <tr className="border-b border-border/20 last:border-0 hover:bg-surface-1 transition-colors">
      <td className="px-4 py-3">
        <Link
          href={`/admin/users/${member.user_id}`}
          className="flex items-center gap-3 group"
        >
          {member.user_avatar_url ? (
            <img
              src={member.user_avatar_url}
              alt=""
              className="h-8 w-8 rounded-full bg-surface-2"
            />
          ) : (
            <div className="h-8 w-8 rounded-full bg-surface-2 flex items-center justify-center text-[12px] text-fg-3 font-medium">
              {displayName.charAt(0).toUpperCase()}
            </div>
          )}
          <div className="min-w-0">
            <p className="text-[14px] text-fg-1 group-hover:underline underline-offset-2 truncate">
              {displayName}
            </p>
            {member.user_email && (
              <p className="text-[12px] text-fg-3 truncate">
                {member.user_email}
              </p>
            )}
          </div>
        </Link>
      </td>
      <td className="px-4 py-3 text-fg-2 capitalize">{member.role}</td>
      <td className="px-4 py-3 hidden md:table-cell text-fg-3 text-[13px]">
        {joined}
      </td>
    </tr>
  );
}
