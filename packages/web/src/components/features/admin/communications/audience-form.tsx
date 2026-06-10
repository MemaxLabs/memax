"use client";

import { useState } from "react";
import { Button } from "@memaxlabs/ui";
import { AlertCircle, Sparkles } from "lucide-react";
import { useLocale } from "@/i18n";
import type {
  AdminAudienceCreateParams,
  AdminAudienceRule,
  AdminAudienceRuleType,
  AdminAudienceEstimate,
} from "@/lib/admin-client";
import { useEstimateAdminAudience } from "@/hooks/use-admin-audiences";

const inputClass =
  "w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10";

export interface AudienceFormInitial {
  name?: string;
  description?: string;
  ruleType?: AdminAudienceRuleType;
  rule?: AdminAudienceRule;
}

export function AudienceForm({
  initial,
  mode,
  onSubmit,
  onCancel,
  pending,
  submitError,
}: {
  initial?: AudienceFormInitial;
  mode: "create" | "edit";
  onSubmit: (params: AdminAudienceCreateParams) => Promise<void>;
  onCancel?: () => void;
  pending: boolean;
  submitError?: string | null;
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.audiences.form;

  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [ruleType, setRuleType] = useState<AdminAudienceRuleType>(
    initial?.ruleType ?? "users",
  );
  const [rule, setRule] = useState<AdminAudienceRule>(initial?.rule ?? {});
  const [formError, setFormError] = useState<string | null>(null);
  const [estimate, setEstimate] = useState<AdminAudienceEstimate | null>(null);

  const estimateMutation = useEstimateAdminAudience();

  function setRulePartial(patch: Partial<AdminAudienceRule>) {
    setRule((r) => ({ ...r, ...patch }));
  }

  async function handleEstimate() {
    setFormError(null);
    setEstimate(null);
    try {
      const result = await estimateMutation.mutateAsync({
        snapshot: { rule_type: ruleType, rule },
      });
      setEstimate(result);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);
    if (!name.trim()) {
      setFormError(copy.errors.missingName);
      return;
    }
    if (ruleType === "users") {
      const ids = rule.user_ids ?? [];
      const emails = rule.emails ?? [];
      if (ids.length === 0 && emails.length === 0) {
        setFormError(copy.errors.missingRecipients);
        return;
      }
    }
    if (ruleType === "hub" && !rule.hub_id) {
      setFormError(copy.errors.missingHub);
      return;
    }
    await onSubmit({
      name: name.trim(),
      description,
      rule_type: ruleType,
      rule,
    });
  }

  const userLines = [...(rule.user_ids ?? []), ...(rule.emails ?? [])].join(
    "\n",
  );

  return (
    <form onSubmit={handleSubmit} className="space-y-5">
      <div className="space-y-4 rounded-surface border border-border/50 bg-card p-6">
        <label className="block space-y-1.5">
          <span className="block text-[12px] font-medium text-fg-2">
            {copy.nameLabel}
          </span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={copy.namePlaceholder}
            className={inputClass}
          />
        </label>
        <label className="block space-y-1.5">
          <span className="block text-[12px] font-medium text-fg-2">
            {copy.descriptionLabel}
          </span>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={copy.descriptionPlaceholder}
            rows={2}
            className={inputClass}
          />
        </label>

        <div>
          <p className="text-[12px] font-medium text-fg-2">
            {copy.ruleTypeLabel}
          </p>
          <div className="mt-2 grid gap-2 sm:grid-cols-3">
            {(["all", "users", "hub"] as AdminAudienceRuleType[]).map((rt) => (
              <button
                key={rt}
                type="button"
                onClick={() => {
                  setRuleType(rt);
                  setRule({});
                  setEstimate(null);
                }}
                aria-pressed={ruleType === rt}
                className={`rounded-surface border px-3 py-2 text-left transition-colors cursor-pointer ${
                  ruleType === rt
                    ? "border-fg-1/30 bg-surface-2"
                    : "border-border/50 bg-surface-1 hover:border-border hover:bg-surface-2"
                }`}
              >
                <span className="block text-[13px] font-medium text-fg-1">
                  {copy.ruleTypes[rt].title}
                </span>
                <span className="mt-0.5 block text-[11.5px] text-fg-2">
                  {copy.ruleTypes[rt].description}
                </span>
              </button>
            ))}
          </div>
        </div>

        {ruleType === "users" ? (
          <label className="block space-y-1.5">
            <span className="block text-[12px] font-medium text-fg-2">
              {copy.usersLabel}
            </span>
            <textarea
              rows={4}
              value={userLines}
              onChange={(e) => {
                const lines = e.target.value
                  .split(/[,\n]/)
                  .map((s) => s.trim())
                  .filter(Boolean);
                setRulePartial({
                  user_ids: lines.filter((l) => !l.includes("@")),
                  emails: lines.filter((l) => l.includes("@")),
                });
                setEstimate(null);
              }}
              placeholder={copy.usersPlaceholder}
              className={inputClass}
            />
          </label>
        ) : null}

        {ruleType === "hub" ? (
          <div className="grid gap-2 sm:grid-cols-[1fr_12rem]">
            <input
              value={rule.hub_id ?? ""}
              onChange={(e) => {
                setRulePartial({ hub_id: e.target.value });
                setEstimate(null);
              }}
              placeholder={copy.hubIdPlaceholder}
              className={inputClass}
            />
            <input
              value={rule.hub_member_role ?? ""}
              onChange={(e) => {
                setRulePartial({ hub_member_role: e.target.value });
                setEstimate(null);
              }}
              placeholder={copy.hubRolePlaceholder}
              className={inputClass}
            />
          </div>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="outline"
          onClick={handleEstimate}
          disabled={estimateMutation.isPending}
          className="cursor-pointer"
        >
          <Sparkles className="mr-1 h-3.5 w-3.5" />
          {estimateMutation.isPending
            ? copy.estimatePending
            : copy.estimateButton}
        </Button>
        {estimate ? (
          <p className="text-[13px] text-fg-1">
            {copy.estimateResult.replace("{count}", String(estimate.count))}
          </p>
        ) : null}
      </div>

      {formError || submitError ? (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          <span>{formError ?? submitError}</span>
        </div>
      ) : null}

      <div className="flex justify-end gap-2">
        {onCancel ? (
          <Button
            type="button"
            variant="outline"
            onClick={onCancel}
            disabled={pending}
            className="cursor-pointer"
          >
            {copy.cancel}
          </Button>
        ) : null}
        <Button type="submit" disabled={pending} className="cursor-pointer">
          {pending
            ? copy.saving
            : mode === "create"
              ? copy.save
              : copy.saveEdit}
        </Button>
      </div>
    </form>
  );
}
