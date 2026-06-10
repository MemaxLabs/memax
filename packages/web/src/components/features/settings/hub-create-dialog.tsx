"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { Check, X, Loader2, Users } from "lucide-react";
import { Surface } from "@memaxlabs/ui";
import type { Hub } from "memax-sdk";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { getMemaxClient } from "@/lib/memax-client";

// Mirror of handler/hubs.go: slug is 4–50 chars, start+end
// alphanumeric, internal hyphens allowed. The length floor is 4 —
// any shorter and the name won't carry enough meaning for a multi-
// member hub. We do NOT mirror the backend's reserved-slug list
// here on purpose: the backend responds to reserved names with the
// same "taken" reason a real collision returns, so the frontend
// cannot enumerate the set even if someone read the code.
const SLUG_REGEX = /^[a-z0-9][a-z0-9-]{2,48}[a-z0-9]$/;
const MIN_SLUG_LEN = 4;
const DEBOUNCE_MS = 300;

// "reserved" no longer exists as a distinct UI state — reserved
// names return as "taken" from both the frontend validator and the
// backend, so the UI shows the same message either way. Old server
// versions sending `reason: "reserved"` are mapped to taken in
// checkSlug below.
type SlugStatus =
  | "idle"
  | "checking"
  | "available"
  | "taken"
  | "invalid"
  | "error";

function nameToSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 50);
}

export function HubCreateDialog({
  onCreated,
  onCancel,
}: {
  // Receives the full Hub (direct SDK response) so callers don't depend
  // on auth-state hydration order after refreshProfile.
  onCreated: (hub: Hub) => void;
  onCancel: () => void;
}) {
  const { refreshProfile } = useAuth();
  const { t } = useLocale();

  const [nameValue, setNameValue] = useState("");
  const [slugValue, setSlugValue] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const [slugStatus, setSlugStatus] = useState<SlugStatus>("idle");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Generation counter to discard stale async responses
  const checkGenRef = useRef(0);

  const checkSlug = useCallback(async (slug: string, gen: number) => {
    if (!slug || slug.length < MIN_SLUG_LEN) {
      if (checkGenRef.current === gen) setSlugStatus("idle");
      return;
    }
    if (!SLUG_REGEX.test(slug)) {
      if (checkGenRef.current === gen) setSlugStatus("invalid");
      return;
    }
    if (checkGenRef.current === gen) setSlugStatus("checking");
    try {
      const result = await getMemaxClient().hubs.checkSlug(slug);
      // Discard if a newer check has started
      if (checkGenRef.current !== gen) return;
      if (result.available) {
        setSlugStatus("available");
      } else {
        // The backend collapses "reserved" into "taken" so the UI
        // treats both as the same unavailable state. "reserved" can
        // still arrive from older server versions; map it to taken.
        setSlugStatus(
          result.reason === "taken" || result.reason === "reserved"
            ? "taken"
            : "invalid",
        );
      }
    } catch {
      if (checkGenRef.current === gen) setSlugStatus("error");
    }
  }, []);

  const debouncedCheck = useCallback(
    (slug: string) => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      const gen = ++checkGenRef.current;
      debounceRef.current = setTimeout(() => checkSlug(slug, gen), DEBOUNCE_MS);
    },
    [checkSlug],
  );

  const handleNameChange = (value: string) => {
    setNameValue(value);
    if (!slugTouched) {
      const slug = nameToSlug(value);
      setSlugValue(slug);
      debouncedCheck(slug);
    }
  };

  const handleSlugChange = (value: string) => {
    const cleaned = value.toLowerCase().replace(/[^a-z0-9-]/g, "");
    setSlugValue(cleaned);
    setSlugTouched(true);
    debouncedCheck(cleaned);
  };

  const handleCreate = async () => {
    if (!nameValue.trim() || slugStatus !== "available" || creating) return;
    setCreating(true);
    setError("");
    try {
      const hub = await getMemaxClient().hubs.create(
        nameValue.trim(),
        slugValue,
      );
      await refreshProfile();
      onCreated(hub);
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : (t.hubCreate?.createFailed ?? "");
      setError(message);
      setCreating(false);
    }
  };

  // Cleanup debounce on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  const canCreate =
    nameValue.trim().length > 0 && slugStatus === "available" && !creating;

  return (
    <Surface variant="subtle" rounded="2xl" className="px-5 py-4">
      <div className="flex items-center gap-2.5 mb-4">
        <div
          className="h-8 w-8 rounded-chrome flex items-center justify-center shrink-0"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <Users className="h-4 w-4 text-fg-2" />
        </div>
        <p className="text-[15px] font-medium text-fg-1">
          {t.hubCreate?.title ?? ""}
        </p>
      </div>

      <div className="space-y-4">
        {/* Hub name */}
        <div>
          <label className="text-[13px] text-fg-3 mb-1.5 block">
            {t.hubCreate?.nameLabel ?? ""}
          </label>
          <input
            type="text"
            value={nameValue}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder={t.hubCreate?.namePlaceholder ?? ""}
            className="w-full rounded-lg bg-surface-1 border border-border/60 px-3 py-2.5 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-foreground/20 transition-colors"
            autoFocus
            onKeyDown={(e) => {
              if (e.key === "Enter" && canCreate) handleCreate();
            }}
          />
        </div>

        {/* Hub handle / slug */}
        <div>
          <label className="text-[13px] text-fg-3 mb-1.5 block">
            {t.hubCreate?.slugLabel ?? ""}
          </label>
          <div className="flex items-center rounded-lg bg-surface-1 border border-border/60 overflow-hidden focus-within:border-foreground/20 transition-colors">
            <span className="pl-3 text-[14px] text-fg-4 select-none shrink-0">
              {t.hubCreate?.slugPrefix ?? "/hubs/"}
            </span>
            <input
              type="text"
              value={slugValue}
              onChange={(e) => handleSlugChange(e.target.value)}
              className="flex-1 py-2.5 pr-3 text-[14px] text-fg-1 bg-transparent outline-none"
              placeholder={t.hubCreate?.slugPlaceholder ?? ""}
            />
          </div>

          {/* Slug status feedback */}
          <div className="mt-1.5 flex items-center gap-1.5 min-h-[20px]">
            {slugStatus === "checking" && (
              <span className="flex items-center gap-1 text-[12px] text-fg-3">
                <Loader2 className="h-3 w-3 animate-spin" />
                {t.hubCreate?.slugChecking ?? ""}
              </span>
            )}
            {slugStatus === "available" && (
              <span className="flex items-center gap-1 text-[12px] text-emerald-500">
                <Check className="h-3 w-3" />
                {t.hubCreate?.slugAvailable ?? ""}
              </span>
            )}
            {slugStatus === "taken" && (
              <span className="flex items-center gap-1 text-[12px] text-destructive/70">
                <X className="h-3 w-3" />
                {t.hubCreate?.slugTaken ?? ""}
              </span>
            )}
            {slugStatus === "invalid" && slugValue.length > 0 && (
              <span className="flex items-center gap-1 text-[12px] text-destructive/70">
                <X className="h-3 w-3" />
                {t.hubCreate?.slugInvalid ?? ""}
              </span>
            )}
            {slugStatus === "error" && (
              <span className="flex items-center gap-1 text-[12px] text-destructive/70">
                <X className="h-3 w-3" />
                {t.hubCreate?.slugError ?? ""}
              </span>
            )}
          </div>

          <p className="text-[11px] text-fg-4 mt-0.5">
            {t.hubCreate?.slugPermanent ?? ""}
          </p>
        </div>

        {/* Error */}
        {error && <p className="text-[12px] text-destructive/70">{error}</p>}

        {/* Actions */}
        <div className="flex items-center gap-2 pt-1">
          <button
            onClick={handleCreate}
            disabled={!canCreate}
            className="px-4 py-2 rounded-lg text-[13px] font-medium bg-foreground text-background hover:opacity-90 transition-opacity cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {creating
              ? (t.hubCreate?.creating ?? "")
              : (t.hubCreate?.create ?? "")}
          </button>
          <button
            onClick={onCancel}
            className="px-4 py-2 rounded-lg text-[13px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {t.hubCreate?.cancel ?? ""}
          </button>
        </div>
      </div>
    </Surface>
  );
}
