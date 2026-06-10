"use client";

import { useMemo, useState } from "react";
import { Button, Skeleton } from "@memaxlabs/ui";
import { Layers } from "lucide-react";
import { useLocale } from "@/i18n";
import {
  useAdminEmailTemplate,
  useAdminEmailTemplates,
} from "@/hooks/use-admin-email";
import {
  useAdminCampaignTemplate,
  useAdminCampaignTemplates,
} from "@/hooks/use-admin-campaign-templates";

/**
 * CampaignTemplateScaffoldPicker lets an admin seed a channel=email
 * campaign's subject + body from either:
 *
 *   - a SYSTEM transactional template (hub_invite, waitlist_*, …) —
 *     referenced by name; we copy the PUBLISHED content via
 *     `detail.published_*` (never drafts). This prevents
 *     admin-in-progress transactional drafts from leaking into
 *     campaign sends.
 *   - a CUSTOM campaign template (admin-authored reusable body) —
 *     referenced by id; we copy the subject/body_html/body_text
 *     directly. Custom templates have no draft/published split; what
 *     the admin saved IS the usable content.
 *
 * Two sources, one dropdown. We disambiguate via a prefix in the
 * option value: `system:<name>` or `custom:<id>`. The UI groups them
 * with `<optgroup>` so admins can visually tell them apart.
 *
 * **Destructive action guard.** Apply overwrites subject/body fields.
 * If the admin has already entered content, we require an explicit
 * confirmation click before replacing it.
 */
export function CampaignTemplateScaffoldPicker({
  onScaffold,
  currentContent,
}: {
  onScaffold: (next: {
    subject: string;
    bodyHtml: string;
    bodyText: string;
  }) => void;
  /** Snapshot of the campaign form's current email fields so we can
   *  warn before Apply replaces admin-entered content. */
  currentContent: { subject: string; bodyHtml: string; bodyText: string };
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.scaffold;

  // The select value is `{kind}:{ref}` where kind is "system" | "custom".
  // Empty = no selection. Parsing happens via parseSelection(); building
  // via buildSelection(). This keeps a single React state slot and
  // avoids a mode dropdown + id dropdown combination.
  const [selected, setSelected] = useState<string>("");
  const [confirming, setConfirming] = useState(false);

  const { kind: selectedKind, ref: selectedRef } = parseSelection(selected);

  const systemTemplatesQ = useAdminEmailTemplates();
  const customTemplatesQ = useAdminCampaignTemplates(false);

  // Mutually exclusive — only one detail fetch runs at a time.
  const systemDetailQ = useAdminEmailTemplate(
    selectedKind === "system" ? selectedRef : undefined,
  );
  const customDetailQ = useAdminCampaignTemplate(
    selectedKind === "custom" ? selectedRef : undefined,
  );

  const systemTemplates = useMemo(
    () => systemTemplatesQ.data?.templates ?? [],
    [systemTemplatesQ.data],
  );
  const customTemplates = useMemo(
    () => customTemplatesQ.data?.templates ?? [],
    [customTemplatesQ.data],
  );

  const loading = systemTemplatesQ.isLoading || customTemplatesQ.isLoading;
  const hasAnyTemplates =
    systemTemplates.length > 0 || customTemplates.length > 0;

  // Apply disabled when no selection, detail fetch in-flight or errored,
  // or (for system) the template has no publishable snapshot. Custom
  // templates are always publishable — the content IS the content.
  const detailLoading =
    (selectedKind === "system" && systemDetailQ.isLoading) ||
    (selectedKind === "custom" && customDetailQ.isLoading);
  const detailError =
    (selectedKind === "system" && systemDetailQ.isError) ||
    (selectedKind === "custom" && customDetailQ.isError);
  const systemUnpublished = Boolean(
    selectedKind === "system" &&
    systemDetailQ.data &&
    !systemDetailQ.data.published_available,
  );
  const applyDisabled =
    !selected || detailLoading || detailError || systemUnpublished;

  // True when the admin already has meaningful content in any of the
  // target fields. "meaningful" = non-empty after trim — a stray space
  // shouldn't trigger the confirm gate.
  const hasExistingContent =
    currentContent.subject.trim() !== "" ||
    currentContent.bodyHtml.trim() !== "" ||
    currentContent.bodyText.trim() !== "";

  function doApply() {
    if (selectedKind === "system") {
      const detail = systemDetailQ.data;
      if (!detail) return;
      onScaffold({
        subject: detail.published_subject,
        bodyHtml: detail.published_html,
        bodyText: detail.published_text,
      });
    } else if (selectedKind === "custom") {
      const detail = customDetailQ.data;
      if (!detail) return;
      onScaffold({
        subject: detail.subject,
        bodyHtml: detail.body_html,
        bodyText: detail.body_text,
      });
    }
    setConfirming(false);
  }

  function handleApply() {
    if (hasExistingContent && !confirming) {
      setConfirming(true);
      return;
    }
    doApply();
  }

  return (
    <section
      className="rounded-surface border border-border/50 bg-surface-1 p-4"
      aria-label={copy.sectionTitle}
    >
      <div className="mb-3 flex items-start gap-2">
        <Layers className="mt-0.5 h-4 w-4 text-fg-2" aria-hidden="true" />
        <div>
          <h3 className="text-[13px] font-semibold text-fg-1">
            {copy.sectionTitle}
          </h3>
          <p className="mt-0.5 text-[12.5px] text-fg-2">
            {copy.sectionDescription}
          </p>
        </div>
      </div>

      {loading ? (
        <Skeleton className="h-9 w-full" />
      ) : !hasAnyTemplates ? (
        <p className="text-[12.5px] text-fg-3">{copy.empty}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={selected}
            onChange={(e) => {
              setSelected(e.target.value);
              setConfirming(false);
            }}
            className="min-w-50 flex-1 rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
            aria-label={copy.selectLabel}
          >
            <option value="">{copy.placeholder}</option>
            {customTemplates.length > 0 ? (
              <optgroup label={copy.groupCustom}>
                {customTemplates.map((tmpl) => (
                  <option
                    key={`custom-${tmpl.id}`}
                    value={buildSelection("custom", tmpl.id)}
                  >
                    {tmpl.name}
                    {tmpl.subject ? ` — ${tmpl.subject}` : ""}
                  </option>
                ))}
              </optgroup>
            ) : null}
            {systemTemplates.length > 0 ? (
              <optgroup label={copy.groupSystem}>
                {systemTemplates.map((tmpl) => (
                  <option
                    key={`system-${tmpl.name}`}
                    value={buildSelection("system", tmpl.name)}
                  >
                    {tmpl.name} — {tmpl.subject}
                  </option>
                ))}
              </optgroup>
            ) : null}
          </select>
          <Button
            type="button"
            variant={confirming ? "default" : "outline"}
            onClick={handleApply}
            disabled={applyDisabled}
            className="cursor-pointer"
          >
            {detailLoading
              ? copy.loading
              : confirming
                ? copy.confirmOverwrite
                : copy.apply}
          </Button>
          {confirming ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirming(false)}
              className="cursor-pointer"
            >
              {copy.cancel}
            </Button>
          ) : null}
        </div>
      )}
      {/* Published-only notice. Shown when admin picked a system
          template that doesn't have a publishable snapshot yet —
          scaffolding from an unpublished draft would be a silent
          test-vs-prod leak. */}
      {systemUnpublished ? (
        <p className="mt-2 text-[12.5px] text-amber-700 dark:text-amber-300">
          {copy.noPublished}
        </p>
      ) : null}
      {/* Overwrite warning — only surfaces when admin has entered
          content and hasn't clicked through the confirm yet. */}
      {hasExistingContent && !confirming && selected ? (
        <p className="mt-2 text-[12.5px] text-fg-3">{copy.overwriteHint}</p>
      ) : null}
      {detailError ? (
        <p className="mt-2 text-[12.5px] text-destructive">{copy.loadError}</p>
      ) : null}
    </section>
  );
}

// Encoding helpers for the single-slot select. We intentionally don't
// use JSON-in-value because `<option value>` is a string in the DOM and
// JSON round-trips add noise to HTML inspectors and test selectors.
type SelectionKind = "system" | "custom" | null;

function buildSelection(kind: "system" | "custom", ref: string): string {
  return `${kind}:${ref}`;
}

function parseSelection(value: string): { kind: SelectionKind; ref: string } {
  if (!value) return { kind: null, ref: "" };
  const idx = value.indexOf(":");
  if (idx <= 0) return { kind: null, ref: "" };
  const kind = value.slice(0, idx);
  const ref = value.slice(idx + 1);
  if (kind !== "system" && kind !== "custom") return { kind: null, ref: "" };
  return { kind, ref };
}
