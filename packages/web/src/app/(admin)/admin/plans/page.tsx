"use client";

import { useMemo, useState } from "react";
import { User, Users2 } from "lucide-react";
import { useLocale } from "@/i18n";
import { useAdminPlans } from "@/hooks/use-admin-users";
import { PlanCard } from "@/components/features/admin/plans/plan-card";
import type { PlanDefinition } from "@/lib/admin-client";

/**
 * /admin/plans — edit plan limits.
 *
 * Plans are stored in the `plans` table and loaded into the in-memory
 * registry at startup (refreshed every 60s). This page saves via PATCH
 * /v1/admin/plans/{id}, which forces a synchronous registry reload so
 * the new limits take effect immediately across middleware.
 *
 * Structural fields (id, scope, features, timestamps) are read-only —
 * adding a new plan or changing scope belongs in a migration, not an
 * admin click.
 *
 * The two scopes (personal + hub) are surfaced as sub-tabs rather than
 * stacked sections so an operator searching for a specific plan isn't
 * scrolling through the other scope first. Matches the visual tab
 * pattern used by /admin/communications — underline indicator, icons
 * from lucide.
 */
type Scope = "personal" | "hub";

export default function AdminPlansPage() {
  const { t } = useLocale();
  const copy = t.admin.plans;
  const { data: plans, isLoading, error } = useAdminPlans();
  const [scope, setScope] = useState<Scope>("personal");

  const grouped = useMemo(() => groupByScope(plans ?? []), [plans]);
  const active = grouped[scope];

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-5xl">
      <header className="mb-5">
        <h1 className="text-[20px] font-semibold text-fg-1">{copy.title}</h1>
        <p className="text-[14px] text-fg-3 mt-1">{copy.description}</p>
      </header>

      <div
        role="tablist"
        aria-label={copy.tabsAriaLabel}
        className="flex items-end gap-1 border-b border-border/50 mb-6 overflow-x-auto"
      >
        <ScopeTab
          id="personal"
          active={scope === "personal"}
          label={copy.tabs.personal}
          count={grouped.personal.length}
          icon={<User className="h-4 w-4 shrink-0" />}
          onSelect={setScope}
        />
        <ScopeTab
          id="hub"
          active={scope === "hub"}
          label={copy.tabs.hub}
          count={grouped.hub.length}
          icon={<Users2 className="h-4 w-4 shrink-0" />}
          onSelect={setScope}
        />
      </div>

      {isLoading ? <PlansSkeleton /> : null}

      {!isLoading && error ? (
        <p className="text-[14px] text-fg-3">{copy.loadError}</p>
      ) : null}

      {!isLoading && !error && active.length === 0 ? (
        <p className="text-[14px] text-fg-3">{copy.empty}</p>
      ) : null}

      {!isLoading && !error && active.length > 0 ? (
        <div className="space-y-4">
          {active.map((p) => (
            <PlanCard key={p.id} plan={p} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ScopeTab({
  id,
  active,
  label,
  count,
  icon,
  onSelect,
}: {
  id: Scope;
  active: boolean;
  label: string;
  count: number;
  icon: React.ReactNode;
  onSelect: (id: Scope) => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={() => onSelect(id)}
      className={`group relative flex items-center gap-2 rounded-t-lg px-4 py-3 text-[13.5px] font-medium transition-colors whitespace-nowrap ${
        active ? "text-fg-1" : "text-fg-3 hover:text-fg-1 hover:bg-surface-1/60"
      }`}
    >
      {icon}
      <span>{label}</span>
      <span
        className={`text-[11px] tabular-nums rounded-full px-1.5 ${
          active ? "bg-surface-2 text-fg-2" : "bg-surface-1 text-fg-3"
        }`}
      >
        {count}
      </span>
      {active ? (
        <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-fg-1" />
      ) : null}
    </button>
  );
}

function groupByScope(plans: PlanDefinition[]): {
  personal: PlanDefinition[];
  hub: PlanDefinition[];
} {
  const personal: PlanDefinition[] = [];
  const hub: PlanDefinition[] = [];
  for (const p of plans) {
    if (p.scope === "hub") hub.push(p);
    else personal.push(p);
  }
  const byTier = (a: PlanDefinition, b: PlanDefinition) =>
    a.tier_order - b.tier_order;
  personal.sort(byTier);
  hub.sort(byTier);
  return { personal, hub };
}

function PlansSkeleton() {
  return (
    <div className="space-y-4">
      {Array.from({ length: 3 }).map((_, i) => (
        <div
          key={i}
          className="h-48 rounded-surface border border-border/30 bg-surface-2/50 animate-pulse"
        />
      ))}
    </div>
  );
}
