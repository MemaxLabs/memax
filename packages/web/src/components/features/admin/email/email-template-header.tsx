"use client";

import { Badge, Button } from "@memaxlabs/ui";

export function EmailTemplateHeader({
  name,
  description,
  hasOverride,
  hasUnpublishedDraft,
  customLabel,
  defaultLabel,
  draftBadgeLabel,
  previewLabel,
  resetLabel,
  saveLabel,
  publishLabel,
  onPreview,
  onReset,
  onSave,
  onPublish,
  previewDisabled,
  resetDisabled,
  saveDisabled,
  publishDisabled,
}: {
  name: string;
  description: string;
  hasOverride: boolean;
  /** true when the admin has saved edits that haven't been promoted to
   *  published yet. Drives the "draft — not published" badge and the
   *  Publish button's enabled state. */
  hasUnpublishedDraft: boolean;
  customLabel: string;
  defaultLabel: string;
  draftBadgeLabel: string;
  previewLabel: string;
  resetLabel: string;
  saveLabel: string;
  publishLabel: string;
  onPreview: () => void;
  onReset: () => void;
  onSave: () => void;
  onPublish: () => void;
  previewDisabled: boolean;
  resetDisabled: boolean;
  saveDisabled: boolean;
  publishDisabled: boolean;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border/40 pb-4">
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-[18px] font-semibold text-fg-1">{name}</h2>
          <Badge variant={hasOverride ? "default" : "secondary"}>
            {hasOverride ? customLabel : defaultLabel}
          </Badge>
          {hasUnpublishedDraft ? (
            <Badge
              variant="outline"
              className="border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300"
            >
              {draftBadgeLabel}
            </Badge>
          ) : null}
        </div>
        <p className="mt-1 text-[13px] text-fg-2">{description}</p>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          onClick={onPreview}
          disabled={previewDisabled}
        >
          {previewLabel}
        </Button>
        <Button variant="outline" onClick={onReset} disabled={resetDisabled}>
          {resetLabel}
        </Button>
        <Button variant="outline" onClick={onSave} disabled={saveDisabled}>
          {saveLabel}
        </Button>
        <Button onClick={onPublish} disabled={publishDisabled}>
          {publishLabel}
        </Button>
      </div>
    </div>
  );
}
