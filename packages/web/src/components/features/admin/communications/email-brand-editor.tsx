"use client";

import { useEffect, useMemo, useState } from "react";
import { Button, Skeleton, Textarea } from "@memaxlabs/ui";
import { Input } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  useAdminEmailBrand,
  useUpdateAdminEmailBrand,
} from "@/hooks/use-admin-email-brand";
import { useAdminEmailTemplate } from "@/hooks/use-admin-email";
import type {
  AdminEmailBrandSettings,
  AdminEmailBrandUpdateParams,
} from "@/lib/admin-client";
import { CommunicationsSectionHeader } from "./communications-section-header";
import { AIAssistButton } from "./ai-assist-button";

type FormState = Pick<
  AdminEmailBrandSettings,
  | "logo_url"
  | "logo_alt"
  | "product_name"
  | "footer_html"
  | "footer_text"
  | "primary_color"
  | "background_color"
  | "surface_color"
  | "border_color"
  | "muted_color"
  | "body_color"
  | "support_email"
  | "company_name"
  | "company_address"
  | "privacy_url"
  | "terms_url"
>;

const EMPTY_FORM: FormState = {
  logo_url: "",
  logo_alt: "",
  product_name: "",
  footer_html: "",
  footer_text: "",
  primary_color: "",
  background_color: "",
  surface_color: "",
  border_color: "",
  muted_color: "",
  body_color: "",
  support_email: "",
  company_name: "",
  company_address: "",
  privacy_url: "",
  terms_url: "",
};

// BRAND_LOGO_PRESETS enumerates the shipped brand assets under
// packages/web/public/images/. Paths are stored as RELATIVE URLs in
// the brand settings row; the server's TemplateManager resolves them
// to absolute via APP_BASE_URL at render time (email clients require
// absolute src for <img>). Admins can still paste a custom absolute
// URL via the Custom tab — presets just remove the "find the right
// path" step for the common case.
const BRAND_LOGO_PRESETS = [
  { key: "icon", path: "/images/memax-icon.svg" },
  { key: "wordmark", path: "/images/memax-wordmark.svg" },
  { key: "mascot", path: "/images/memo-mascot.svg" },
] as const;
type BrandLogoPresetKey = (typeof BRAND_LOGO_PRESETS)[number]["key"];

export function EmailBrandEditor() {
  const { t } = useLocale();
  const copy = t.admin.communications.brand;

  const { data: settings, isLoading, error } = useAdminEmailBrand();
  const update = useUpdateAdminEmailBrand();
  // Preview uses hub_invite — a short template that shows the logo, body,
  // and footer, so changes to brand are obvious in the iframe.
  const { data: previewDetail, isLoading: previewLoading } =
    useAdminEmailTemplate("hub_invite");

  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  // Preview mode toggles whether the iframe shows the transactional
  // footer (reason + identity + legal, no unsubscribe) or the marketing
  // footer (same + unsubscribe row). The server's preview render always
  // targets the transactional path; we inject the unsubscribe row into
  // the preview HTML client-side for the marketing view so the admin
  // sees what a campaign send would actually produce. See
  // decoratePreviewHTML below.
  const [previewMode, setPreviewMode] = useState<"transactional" | "marketing">(
    "transactional",
  );

  useEffect(() => {
    if (settings) {
      setForm({
        logo_url: settings.logo_url,
        logo_alt: settings.logo_alt,
        product_name: settings.product_name,
        footer_html: settings.footer_html,
        footer_text: settings.footer_text,
        primary_color: settings.primary_color,
        background_color: settings.background_color,
        surface_color: settings.surface_color,
        border_color: settings.border_color,
        muted_color: settings.muted_color,
        body_color: settings.body_color,
        support_email: settings.support_email,
        company_name: settings.company_name,
        company_address: settings.company_address,
        privacy_url: settings.privacy_url,
        terms_url: settings.terms_url,
      });
    }
  }, [settings]);

  const dirty = useMemo(() => {
    if (!settings) return false;
    return (Object.keys(form) as (keyof FormState)[]).some(
      (key) => form[key] !== settings[key],
    );
  }, [form, settings]);

  const handleSave = () => {
    if (!settings) return;
    const patch: AdminEmailBrandUpdateParams = {};
    for (const key of Object.keys(form) as (keyof FormState)[]) {
      if (form[key] !== settings[key]) {
        patch[key] = form[key];
      }
    }
    if (Object.keys(patch).length === 0) return;
    update.mutate(patch);
  };

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-7 w-56" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error || !settings) {
    return (
      <div className="rounded-surface border border-border/50 bg-card p-6">
        <p className="text-[14px] text-fg-2">{copy.loadError}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <CommunicationsSectionHeader
        eyebrow={copy.eyebrow}
        title={copy.title}
        description={copy.description}
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="space-y-6">
          <Section
            title={copy.logoSectionTitle}
            description={copy.logoSectionDescription}
          >
            <Field label={copy.logoUrlLabel}>
              <LogoPicker
                value={form.logo_url}
                onChange={(value) => setForm({ ...form, logo_url: value })}
                placeholder={copy.logoUrlPlaceholder}
                presetsLabel={copy.logoPresetsLabel}
                customLabel={copy.logoCustomLabel}
                presets={BRAND_LOGO_PRESETS.map((p) => ({
                  value: p.path,
                  label: copy.logoPresets[p.key],
                  thumbnail: p.path,
                }))}
              />
            </Field>
            <Field label={copy.logoAltLabel}>
              <Input
                value={form.logo_alt}
                placeholder={copy.logoAltPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, logo_alt: event.target.value })
                }
              />
            </Field>
            <Field label={copy.productNameLabel}>
              <Input
                value={form.product_name}
                placeholder={copy.productNamePlaceholder}
                onChange={(event) =>
                  setForm({ ...form, product_name: event.target.value })
                }
              />
            </Field>
          </Section>

          <Section
            title={copy.footerSectionTitle}
            description={copy.footerSectionDescription}
          >
            <Field label={copy.footerHtmlLabel}>
              <Textarea
                rows={3}
                value={form.footer_html}
                placeholder={copy.footerHtmlPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, footer_html: event.target.value })
                }
              />
              <div className="mt-2">
                <AIAssistButton
                  fieldKind="footer_html"
                  current={form.footer_html}
                  onApply={(text) => setForm({ ...form, footer_html: text })}
                />
              </div>
            </Field>
            <Field label={copy.footerTextLabel}>
              <Textarea
                rows={2}
                value={form.footer_text}
                placeholder={copy.footerTextPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, footer_text: event.target.value })
                }
              />
              <div className="mt-2">
                <AIAssistButton
                  fieldKind="footer_text"
                  current={form.footer_text}
                  onApply={(text) => setForm({ ...form, footer_text: text })}
                />
              </div>
            </Field>
          </Section>

          <Section
            title={copy.colorsSectionTitle}
            description={copy.colorsSectionDescription}
          >
            <div className="grid grid-cols-2 gap-3">
              <ColorField
                label={copy.primaryColorLabel}
                value={form.primary_color}
                onChange={(value) => setForm({ ...form, primary_color: value })}
              />
              <ColorField
                label={copy.backgroundColorLabel}
                value={form.background_color}
                onChange={(value) =>
                  setForm({ ...form, background_color: value })
                }
              />
              <ColorField
                label={copy.surfaceColorLabel}
                value={form.surface_color}
                onChange={(value) => setForm({ ...form, surface_color: value })}
              />
              <ColorField
                label={copy.borderColorLabel}
                value={form.border_color}
                onChange={(value) => setForm({ ...form, border_color: value })}
              />
              <ColorField
                label={copy.mutedColorLabel}
                value={form.muted_color}
                onChange={(value) => setForm({ ...form, muted_color: value })}
              />
              <ColorField
                label={copy.bodyColorLabel}
                value={form.body_color}
                onChange={(value) => setForm({ ...form, body_color: value })}
              />
            </div>
          </Section>

          <Section
            title={copy.supportSectionTitle}
            description={copy.supportSectionDescription}
          >
            <Field label={copy.supportEmailLabel}>
              <Input
                type="email"
                value={form.support_email}
                placeholder={copy.supportEmailPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, support_email: event.target.value })
                }
              />
            </Field>
          </Section>

          <Section
            title={copy.complianceSectionTitle}
            description={copy.complianceSectionDescription}
          >
            <Field label={copy.companyNameLabel}>
              <Input
                value={form.company_name}
                placeholder={copy.companyNamePlaceholder}
                onChange={(event) =>
                  setForm({ ...form, company_name: event.target.value })
                }
              />
            </Field>
            <Field label={copy.companyAddressLabel}>
              <Textarea
                rows={2}
                value={form.company_address}
                placeholder={copy.companyAddressPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, company_address: event.target.value })
                }
              />
            </Field>
          </Section>

          <Section
            title={copy.legalSectionTitle}
            description={copy.legalSectionDescription}
          >
            <Field label={copy.privacyUrlLabel}>
              <Input
                type="url"
                value={form.privacy_url}
                placeholder={copy.privacyUrlPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, privacy_url: event.target.value })
                }
              />
            </Field>
            <Field label={copy.termsUrlLabel}>
              <Input
                type="url"
                value={form.terms_url}
                placeholder={copy.termsUrlPlaceholder}
                onChange={(event) =>
                  setForm({ ...form, terms_url: event.target.value })
                }
              />
            </Field>
          </Section>

          <div className="flex items-center justify-end gap-3">
            <Button
              onClick={handleSave}
              disabled={!dirty || update.isPending}
              className="cursor-pointer"
            >
              {update.isPending ? copy.saving : copy.save}
            </Button>
          </div>
        </div>

        <div className="space-y-3">
          <div className="flex items-start justify-between gap-3">
            <div>
              <p className="text-[13px] font-medium text-fg-2">
                {copy.previewTitle}
              </p>
              <p className="mt-1 text-[12px] text-fg-4">
                {copy.previewDescription}
              </p>
            </div>
            <div className="inline-flex shrink-0 rounded-lg border border-border/60 p-0.5">
              <button
                type="button"
                onClick={() => setPreviewMode("transactional")}
                className={`rounded-md px-3 py-1 text-[12px] transition-colors cursor-pointer ${
                  previewMode === "transactional"
                    ? "bg-foreground text-background font-medium"
                    : "text-fg-2 hover:text-fg-1"
                }`}
                aria-pressed={previewMode === "transactional"}
              >
                {copy.previewModeTransactional}
              </button>
              <button
                type="button"
                onClick={() => setPreviewMode("marketing")}
                className={`rounded-md px-3 py-1 text-[12px] transition-colors cursor-pointer ${
                  previewMode === "marketing"
                    ? "bg-foreground text-background font-medium"
                    : "text-fg-2 hover:text-fg-1"
                }`}
                aria-pressed={previewMode === "marketing"}
              >
                {copy.previewModeMarketing}
              </button>
            </div>
          </div>
          {previewLoading || !previewDetail ? (
            <Skeleton className="h-[520px] w-full" />
          ) : (
            <iframe
              title={copy.previewTitle}
              srcDoc={decoratePreviewHTML(
                previewDetail.preview.html,
                previewMode,
                copy.previewUnsubscribeChip,
              )}
              className="min-h-[520px] w-full rounded-surface border border-border/50 bg-white"
            />
          )}
        </div>
      </div>
    </div>
  );
}

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-surface border border-border/50 bg-card p-5">
      <div className="mb-3">
        <h3 className="text-[14px] font-semibold tracking-tight text-fg-1">
          {title}
        </h3>
        <p className="mt-0.5 text-[12.5px] text-fg-3">{description}</p>
      </div>
      <div className="space-y-3">{children}</div>
    </section>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] font-medium text-fg-2">
        {label}
      </span>
      {children}
    </label>
  );
}

function ColorField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] font-medium text-fg-2">
        {label}
      </span>
      <div className="flex items-center gap-2">
        <input
          type="color"
          value={normalizeHex(value)}
          onChange={(event) => onChange(event.target.value)}
          className="h-9 w-9 shrink-0 cursor-pointer rounded-md border border-border/60 bg-transparent p-0"
          aria-label={label}
        />
        <Input
          value={value}
          onChange={(event) => onChange(event.target.value)}
          className="font-mono"
        />
      </div>
    </label>
  );
}

/**
 * decoratePreviewHTML augments the server-rendered preview HTML for
 * admin display:
 *   - always replaces the raw `__MEMAX_UNSUBSCRIBE_URL__` sentinel with
 *     a styled `[per-recipient unsubscribe link]` chip, so admins never
 *     see the internal placeholder string
 *   - in marketing mode, injects that same chip as a fresh footer row
 *     so admins can see what a campaign send's footer would look like.
 *     The server's preview path never includes the unsubscribe row —
 *     it's worker-injected per recipient — so the client has to add it
 *     here to avoid the admin wondering where their unsubscribe went.
 */
function decoratePreviewHTML(
  html: string,
  mode: "transactional" | "marketing",
  chipLabel: string,
): string {
  const chipMarkup = `<span style="display:inline-block;padding:2px 8px;background:#f4f4f4;border:1px dashed #c7c7c7;border-radius:4px;font-size:11px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:#666">${chipLabel}</span>`;
  let decorated = html
    .split("__MEMAX_UNSUBSCRIBE_URL__")
    .join("#preview-unsub");
  if (mode === "marketing") {
    const marketingRow = `      <p class="footer-unsub" style="margin-top:12px;padding-top:12px;border-top:1px solid rgba(0,0,0,0.08)">${chipMarkup}</p>\n`;
    // Inject into the .footer div by inserting before its closing tag
    // — we target the last "</div>" immediately before the container's
    // closing "</div></body>". If the markup shape ever changes, the
    // injection silently no-ops and the preview falls back to the
    // transactional view; that's preferable to showing half-baked HTML.
    decorated = decorated.replace(
      /(\s*<\/div>\s*<\/div>\s*<\/body>)/,
      `${marketingRow}$1`,
    );
  }
  return decorated;
}

function normalizeHex(value: string): string {
  // <input type="color"> needs a 7-char #RRGGBB. Pass through anything
  // else as #000000 so the control stays interactive.
  const trimmed = value.trim();
  if (/^#[0-9a-fA-F]{6}$/.test(trimmed)) return trimmed;
  return "#000000";
}

/**
 * LogoPicker lets the admin either pick one of the shipped brand
 * presets (stored as relative `/images/*` paths — server resolves to
 * absolute via APP_BASE_URL) or paste a custom absolute URL (hosted
 * wherever). The mode auto-detects from the current value: a known
 * preset path snaps to "Preset"; anything else stays on "Custom".
 */
function LogoPicker({
  value,
  onChange,
  placeholder,
  presetsLabel,
  customLabel,
  presets,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder: string;
  presetsLabel: string;
  customLabel: string;
  presets: { value: string; label: string; thumbnail: string }[];
}) {
  // Mode lives in local state so a click on Custom sticks even when
  // the current value happens to match a preset. Auto-init from value
  // only on first mount — after that, admin intent owns the mode.
  const [mode, setMode] = useState<"presets" | "custom">(() =>
    presets.some((p) => p.value === value) ? "presets" : "custom",
  );
  const isPresetMatch = (url: string) => presets.some((p) => p.value === url);

  return (
    <div className="space-y-2">
      <div className="inline-flex rounded-lg border border-border/60 p-0.5">
        <button
          type="button"
          onClick={() => setMode("presets")}
          className={`rounded-md px-3 py-1 text-[12px] transition-colors cursor-pointer ${
            mode === "presets"
              ? "bg-foreground text-background font-medium"
              : "text-fg-2 hover:text-fg-1"
          }`}
          aria-pressed={mode === "presets"}
        >
          {presetsLabel}
        </button>
        <button
          type="button"
          onClick={() => setMode("custom")}
          className={`rounded-md px-3 py-1 text-[12px] transition-colors cursor-pointer ${
            mode === "custom"
              ? "bg-foreground text-background font-medium"
              : "text-fg-2 hover:text-fg-1"
          }`}
          aria-pressed={mode === "custom"}
        >
          {customLabel}
        </button>
      </div>

      {mode === "presets" ? (
        <div className="grid grid-cols-3 gap-2">
          {presets.map((p) => {
            const active = value === p.value;
            return (
              <button
                key={p.value}
                type="button"
                onClick={() => onChange(p.value)}
                aria-pressed={active}
                className={`group flex aspect-square flex-col items-center justify-center gap-1 rounded-lg border p-2 transition-colors cursor-pointer ${
                  active
                    ? "border-fg-1/40 bg-surface-2"
                    : "border-border/60 hover:border-border hover:bg-surface-1"
                }`}
              >
                <img
                  src={p.thumbnail}
                  alt={p.label}
                  className="h-10 w-10 object-contain"
                />
                <span className="text-[11px] font-medium text-fg-2">
                  {p.label}
                </span>
              </button>
            );
          })}
        </div>
      ) : (
        <Input
          value={value}
          placeholder={placeholder}
          onChange={(event) => onChange(event.target.value)}
        />
      )}

      {/* Show the resolved value below the picker so the admin sees
          exactly what's stored, regardless of which tab they're on.
          Preset-matching values get an icon; custom values show the
          raw URL verbatim. */}
      {value ? (
        <p className="mt-1 font-mono text-[11px] text-fg-3">
          {isPresetMatch(value) ? "preset · " : ""}
          {value}
        </p>
      ) : null}
    </div>
  );
}

// Re-export for tests / future pickers that want the same preset set.
export { BRAND_LOGO_PRESETS, type BrandLogoPresetKey };
