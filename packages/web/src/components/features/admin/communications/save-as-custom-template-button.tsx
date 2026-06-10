"use client";

import { useState } from "react";
import { Button } from "@memaxlabs/ui";
import { Bookmark, Check } from "lucide-react";
import { useLocale } from "@/i18n";
import { ApiError } from "@/lib/api";
import { useCreateAdminCampaignTemplate } from "@/hooks/use-admin-campaign-templates";

/**
 * SaveAsCustomTemplateButton is a founder-ergonomics affordance on the
 * campaign form: after typing a good subject + body, click Save as
 * template, name it, and it becomes a reusable scaffold for future
 * campaigns. The template is COPIED from the form state — editing the
 * campaign afterwards doesn't retroactively mutate the saved template,
 * and vice versa.
 *
 * Why we don't accept slug from the admin at this step: we auto-derive
 * it from the name via the same normalization rules the server
 * enforces (lowercase, alnum+_-, trimmed) so the founder doesn't have
 * to think about URL-safe identifiers in the middle of campaign
 * authoring. If the derived slug collides with an existing template,
 * we append a short timestamp suffix — the admin can rename via the
 * management page later if they care.
 */
export function SaveAsCustomTemplateButton({
  disabled,
  currentContent,
}: {
  disabled: boolean;
  currentContent: {
    subject: string;
    bodyHtml: string;
    bodyText: string;
  };
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.saveAsTemplate;

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [success, setSuccess] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const createMutation = useCreateAdminCampaignTemplate();

  // Enabled when the campaign has any meaningful content to snapshot.
  // The backend accepts empty subject for body-only scaffolds, so the
  // guard only blocks the truly-empty case (nothing worth saving).
  const hasContent =
    currentContent.subject.trim() !== "" ||
    currentContent.bodyHtml.trim() !== "" ||
    currentContent.bodyText.trim() !== "";

  // randomSlugSuffix draws ~5 chars from crypto.getRandomValues — enough
  // entropy that concurrent retries don't collide, unlike the old
  // Date.now() slice which was deterministic per millisecond.
  function randomSlugSuffix(): string {
    if (typeof crypto !== "undefined" && "getRandomValues" in crypto) {
      const buf = new Uint8Array(4);
      crypto.getRandomValues(buf);
      return Array.from(buf, (b) => b.toString(36).padStart(2, "0"))
        .join("")
        .slice(0, 6);
    }
    // SSR / old-browser fallback — degraded entropy but non-empty.
    return Math.floor(Math.random() * 1e9).toString(36);
  }

  async function handleSave() {
    const trimmedName = name.trim();
    if (trimmedName === "") {
      setError(copy.nameRequired);
      return;
    }
    setError(null);
    const slug = deriveSlug(trimmedName);

    async function create(s: string) {
      return createMutation.mutateAsync({
        slug: s,
        name: trimmedName,
        description: description.trim(),
        subject: currentContent.subject,
        body_html: currentContent.bodyHtml,
        body_text: currentContent.bodyText,
      });
    }

    function finish(tmpl: { name: string }) {
      setSuccess(tmpl.name);
      setOpen(false);
      setName("");
      setDescription("");
    }

    // Up to 3 attempts with a random suffix on conflict. The structured
    // `ApiError.code === "slug_taken"` check is stable across server
    // message-copy changes (unlike regexing err.message). On retry we
    // trim the base so `${base}-${suffix}` fits inside the 64-char
    // server CHECK — a long template name + suffix would otherwise
    // overflow and fail with `invalid_slug`.
    let attemptSlug = slug;
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const tmpl = await create(attemptSlug);
        finish(tmpl);
        return;
      } catch (err) {
        const isConflict = err instanceof ApiError && err.code === "slug_taken";
        if (!isConflict || attempt === 2) {
          setError(err instanceof Error ? err.message : String(err));
          return;
        }
        const suffix = randomSlugSuffix();
        // Reserve `-${suffix}` space on the base so we don't overflow
        // the server's 64-char limit. Leaves room for the hyphen.
        const budget = Math.max(1, 64 - 1 - suffix.length);
        const trimmedBase = slug.slice(0, budget);
        attemptSlug = `${trimmedBase}-${suffix}`;
      }
    }
  }

  if (!open) {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={disabled || !hasContent || createMutation.isPending}
          onClick={() => {
            setOpen(true);
            setSuccess(null);
            setError(null);
          }}
          className="cursor-pointer"
        >
          <Bookmark className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
          {copy.button}
        </Button>
        {success ? (
          <span className="inline-flex items-center gap-1.5 text-[12.5px] text-emerald-700 dark:text-emerald-400">
            <Check className="h-3 w-3" aria-hidden="true" />
            {copy.successPrefix} {success}
          </span>
        ) : null}
      </div>
    );
  }

  return (
    <div className="space-y-3 rounded-surface border border-border/60 bg-surface-1 p-4">
      <div>
        <h3 className="text-[13px] font-semibold text-fg-1">
          {copy.formTitle}
        </h3>
        <p className="mt-0.5 text-[12.5px] text-fg-2">{copy.formDescription}</p>
      </div>
      <label className="block space-y-1.5">
        <span className="block text-[12px] font-medium text-fg-2">
          {copy.nameLabel}
        </span>
        <input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={copy.namePlaceholder}
          className="w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
        />
      </label>
      <label className="block space-y-1.5">
        <span className="block text-[12px] font-medium text-fg-2">
          {copy.descriptionLabel}
        </span>
        <input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder={copy.descriptionPlaceholder}
          className="w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
        />
      </label>
      {error ? <p className="text-[12.5px] text-destructive">{error}</p> : null}
      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setOpen(false);
            setError(null);
          }}
          disabled={createMutation.isPending}
          className="cursor-pointer"
        >
          {copy.cancel}
        </Button>
        <Button
          type="button"
          onClick={() => void handleSave()}
          disabled={createMutation.isPending || name.trim() === ""}
          className="cursor-pointer"
        >
          {createMutation.isPending ? copy.saving : copy.save}
        </Button>
      </div>
    </div>
  );
}

// deriveSlug takes an admin-authored template name and normalizes it
// to match the server's slug CHECK constraint (2-64 chars, lowercase
// alnum + `_` + `-`, must start with alnum). Invalid/empty output
// falls back to a timestamp-only slug.
function deriveSlug(name: string): string {
  const base = name
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-") // non-alnum runs → single dash
    .replace(/-{2,}/g, "-")
    .replace(/^-+|-+$/g, "") // trim leading/trailing dashes
    .slice(0, 64);
  if (/^[a-z0-9][a-z0-9_-]{1,63}$/.test(base)) {
    return base;
  }
  // Fallback: timestamp-only. The CHECK constraint accepts this.
  return `tmpl-${Date.now().toString(36)}`;
}
