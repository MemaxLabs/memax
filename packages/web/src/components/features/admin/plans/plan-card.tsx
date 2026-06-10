"use client";

import { useMemo, useState } from "react";
import { Button } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useUpdatePlan } from "@/hooks/use-admin-users";
import type { PlanDefinition } from "@/lib/admin-client";
import {
  FeaturesSection,
  IdentitySection,
  LimitsSection,
  RateLimitsSection,
  ScopeSection,
  type PlanDraft,
} from "./plan-sections";

// Structural fields (id, scope, created_at, updated_at, features) stay
// read-only — change those via migration, not the admin console.

function toDraft(plan: PlanDefinition): PlanDraft {
  return {
    display_name: plan.display_name,
    monthly_price_cents: plan.monthly_price_cents,
    memory_limit: plan.memory_limit,
    push_limit: plan.push_limit,
    recall_limit: plan.recall_limit,
    ask_limit: plan.ask_limit,
    max_attachment_bytes: plan.max_attachment_bytes,
    storage_bytes_limit: plan.storage_bytes_limit,
    ask_model: plan.ask_model,
    dreams_enabled: plan.dreams_enabled,
    review_inbox: plan.review_inbox,
    max_team_hubs: plan.max_team_hubs,
    rate_limit_rpm: plan.rate_limit_rpm,
    rate_limit_heavy_rpm: plan.rate_limit_heavy_rpm,
    rate_limit_light_rpm: plan.rate_limit_light_rpm,
    tier_order: plan.tier_order,
    entitlement_rank: plan.entitlement_rank,
    active: plan.active,
    max_hub_members: plan.max_hub_members,
    seat_minimum: plan.seat_minimum,
    seat_billed: plan.seat_billed,
    max_owned_free_team_hubs: plan.max_owned_free_team_hubs,
  };
}

function diffDraft(base: PlanDraft, next: PlanDraft): Partial<PlanDraft> {
  const patch: Partial<PlanDraft> = {};
  for (const k of Object.keys(next) as Array<keyof PlanDraft>) {
    if (base[k] !== next[k]) {
      // @ts-expect-error — mapped assignment across keyof union
      patch[k] = next[k];
    }
  }
  return patch;
}

function isDirty(base: PlanDraft, next: PlanDraft): boolean {
  for (const k of Object.keys(next) as Array<keyof PlanDraft>) {
    if (base[k] !== next[k]) return true;
  }
  return false;
}

export function PlanCard({ plan }: { plan: PlanDefinition }) {
  const { t } = useLocale();
  const copy = t.admin.plans;
  const mutation = useUpdatePlan();
  const base = useMemo(() => toDraft(plan), [plan]);
  const [draft, setDraft] = useState<PlanDraft>(base);
  const [savedFlash, setSavedFlash] = useState(false);

  const dirty = isDirty(base, draft);
  const patch = dirty ? diffDraft(base, draft) : {};

  function set<K extends keyof PlanDraft>(key: K, value: PlanDraft[K]) {
    setDraft((d) => ({ ...d, [key]: value }));
  }

  function handleReset() {
    setDraft(base);
  }

  function handleSave() {
    mutation.mutate(
      { planId: plan.id, patch },
      {
        onSuccess: () => {
          setSavedFlash(true);
          setTimeout(() => setSavedFlash(false), 1500);
        },
      },
    );
  }

  return (
    <div className="rounded-surface border border-border/50 bg-card p-5 space-y-5">
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-baseline gap-2">
            <h2 className="text-[16px] font-semibold text-fg-1 truncate">
              {draft.display_name || plan.id}
            </h2>
            <span className="text-[12px] font-mono text-fg-3">{plan.id}</span>
          </div>
          <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-fg-3">
            <span>
              {copy.meta.scope}: <span className="text-fg-2">{plan.scope}</span>
            </span>
            <span>
              {copy.meta.updatedAt}:{" "}
              <span className="text-fg-2">
                {new Date(plan.updated_at).toLocaleString()}
              </span>
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {savedFlash ? (
            <span className="text-[12px] text-emerald-400">
              {copy.saved.replace("{name}", draft.display_name || plan.id)}
            </span>
          ) : mutation.isError ? (
            <span className="text-[12px] text-red-400">{copy.error}</span>
          ) : null}
          <Button
            variant="ghost"
            size="sm"
            disabled={!dirty || mutation.isPending}
            onClick={handleReset}
          >
            {copy.actions.reset}
          </Button>
          <Button
            size="sm"
            disabled={!dirty || mutation.isPending}
            onClick={handleSave}
          >
            {mutation.isPending ? copy.actions.saving : copy.actions.save}
          </Button>
        </div>
      </header>

      <IdentitySection copy={copy} draft={draft} set={set} />
      <LimitsSection copy={copy} draft={draft} set={set} />
      <RateLimitsSection copy={copy} draft={draft} set={set} />
      <FeaturesSection copy={copy} draft={draft} set={set} />
      <ScopeSection copy={copy} draft={draft} set={set} scope={plan.scope} />
    </div>
  );
}
