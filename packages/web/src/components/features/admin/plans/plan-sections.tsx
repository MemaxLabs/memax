"use client";

import type { PlanDefinition } from "@/lib/admin-client";
import type { Translations } from "@/i18n/locales/en";
import {
  BytesField,
  NumberField,
  NullableNumberField,
  SectionHeader,
  SelectField,
  TextField,
  ToggleField,
} from "./fields";

type PlansCopy = Translations["admin"]["plans"];

// Subset of PlanDefinition the admin form can edit. Identical to the
// Draft type in plan-card.tsx — kept in sync by shared usage.
export type PlanDraft = Pick<
  PlanDefinition,
  | "display_name"
  | "monthly_price_cents"
  | "memory_limit"
  | "push_limit"
  | "recall_limit"
  | "ask_limit"
  | "max_attachment_bytes"
  | "storage_bytes_limit"
  | "ask_model"
  | "dreams_enabled"
  | "review_inbox"
  | "max_team_hubs"
  | "rate_limit_rpm"
  | "rate_limit_heavy_rpm"
  | "rate_limit_light_rpm"
  | "tier_order"
  | "entitlement_rank"
  | "active"
  | "max_hub_members"
  | "seat_minimum"
  | "seat_billed"
  | "max_owned_free_team_hubs"
>;

type SetDraft = <K extends keyof PlanDraft>(
  key: K,
  value: PlanDraft[K],
) => void;

interface SectionProps {
  copy: PlansCopy;
  draft: PlanDraft;
  set: SetDraft;
}

export function IdentitySection({ copy, draft, set }: SectionProps) {
  return (
    <section className="space-y-3">
      <SectionHeader>{copy.sections.identity}</SectionHeader>
      <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
        <TextField
          label={copy.fields.displayName}
          value={draft.display_name}
          onChange={(v) => set("display_name", v)}
        />
        <NumberField
          label={copy.fields.monthlyPriceCents}
          value={draft.monthly_price_cents}
          min={0}
          onChange={(v) => set("monthly_price_cents", v)}
        />
        <NumberField
          label={copy.fields.tierOrder}
          value={draft.tier_order}
          onChange={(v) => set("tier_order", v)}
        />
        <NumberField
          label={copy.fields.entitlementRank}
          value={draft.entitlement_rank}
          onChange={(v) => set("entitlement_rank", v)}
        />
      </div>
    </section>
  );
}

export function LimitsSection({ copy, draft, set }: SectionProps) {
  return (
    <section className="space-y-3">
      <SectionHeader>{copy.sections.limits}</SectionHeader>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <NumberField
          label={copy.fields.memoryLimit}
          value={draft.memory_limit}
          hint={copy.fieldHint.unlimited}
          onChange={(v) => set("memory_limit", v)}
        />
        <NumberField
          label={copy.fields.pushLimit}
          value={draft.push_limit}
          hint={copy.fieldHint.unlimited}
          onChange={(v) => set("push_limit", v)}
        />
        <NumberField
          label={copy.fields.recallLimit}
          value={draft.recall_limit}
          hint={copy.fieldHint.unlimited}
          onChange={(v) => set("recall_limit", v)}
        />
        <NumberField
          label={copy.fields.askLimit}
          value={draft.ask_limit}
          hint={copy.fieldHint.unlimited}
          onChange={(v) => set("ask_limit", v)}
        />
        <BytesField
          label={copy.fields.maxAttachmentBytes}
          value={draft.max_attachment_bytes}
          hint={copy.fieldHint.unlimited}
          unitAriaLabel={copy.bytesUnitAria.replace(
            "{label}",
            copy.fields.maxAttachmentBytes,
          )}
          onChange={(v) => set("max_attachment_bytes", v)}
        />
        <BytesField
          label={copy.fields.storageBytesLimit}
          value={draft.storage_bytes_limit}
          hint={copy.fieldHint.unlimited}
          unitAriaLabel={copy.bytesUnitAria.replace(
            "{label}",
            copy.fields.storageBytesLimit,
          )}
          onChange={(v) => set("storage_bytes_limit", v)}
        />
        <NumberField
          label={copy.fields.maxTeamHubs}
          value={draft.max_team_hubs}
          onChange={(v) => set("max_team_hubs", v)}
        />
        <SelectField
          label={copy.fields.askModel}
          value={draft.ask_model}
          options={[
            { value: "haiku", label: copy.askModel.haiku },
            { value: "sonnet", label: copy.askModel.sonnet },
          ]}
          onChange={(v) => set("ask_model", v)}
        />
      </div>
    </section>
  );
}

export function RateLimitsSection({ copy, draft, set }: SectionProps) {
  return (
    <section className="space-y-3">
      <SectionHeader>{copy.sections.rateLimits}</SectionHeader>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <NumberField
          label={copy.fields.rateLimitRpm}
          value={draft.rate_limit_rpm}
          min={0}
          onChange={(v) => set("rate_limit_rpm", v)}
        />
        <NumberField
          label={copy.fields.rateLimitHeavyRpm}
          value={draft.rate_limit_heavy_rpm}
          min={0}
          onChange={(v) => set("rate_limit_heavy_rpm", v)}
        />
        <NumberField
          label={copy.fields.rateLimitLightRpm}
          value={draft.rate_limit_light_rpm}
          min={0}
          onChange={(v) => set("rate_limit_light_rpm", v)}
        />
      </div>
    </section>
  );
}

export function FeaturesSection({ copy, draft, set }: SectionProps) {
  return (
    <section className="space-y-3">
      <SectionHeader>{copy.sections.features}</SectionHeader>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <ToggleField
          label={copy.fields.dreamsEnabled}
          value={draft.dreams_enabled}
          onChange={(v) => set("dreams_enabled", v)}
        />
        <ToggleField
          label={copy.fields.reviewInbox}
          value={draft.review_inbox}
          onChange={(v) => set("review_inbox", v)}
        />
        <ToggleField
          label={copy.fields.active}
          value={draft.active}
          onChange={(v) => set("active", v)}
        />
      </div>
    </section>
  );
}

export function ScopeSection({
  copy,
  draft,
  set,
  scope,
}: SectionProps & { scope: PlanDefinition["scope"] }) {
  return (
    <section className="space-y-3">
      <SectionHeader>{copy.sections.hub}</SectionHeader>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        {scope === "hub" ? (
          <>
            <NullableNumberField
              label={copy.fields.maxHubMembers}
              value={draft.max_hub_members}
              hint={copy.fieldHint.nullUnlimited}
              onChange={(v) => set("max_hub_members", v)}
            />
            <NumberField
              label={copy.fields.seatMinimum}
              value={draft.seat_minimum}
              min={0}
              onChange={(v) => set("seat_minimum", v)}
            />
            <ToggleField
              label={copy.fields.seatBilled}
              value={draft.seat_billed}
              onChange={(v) => set("seat_billed", v)}
            />
          </>
        ) : (
          <NumberField
            label={copy.fields.maxOwnedFreeTeamHubs}
            value={draft.max_owned_free_team_hubs}
            min={0}
            onChange={(v) => set("max_owned_free_team_hubs", v)}
          />
        )}
      </div>
    </section>
  );
}
