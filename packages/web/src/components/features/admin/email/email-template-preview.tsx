"use client";

import { useState } from "react";
import { Monitor, Smartphone } from "lucide-react";
import { useLocale } from "@/i18n";
import type { AdminEmailTemplatePreview } from "@/lib/admin-client";

type Viewport = "desktop" | "mobile";

// Email renders are sensitive to viewport width because inline CSS +
// clamped image widths behave differently in Gmail mobile vs Apple Mail
// desktop. Two presets mirror the two dominant render surfaces.
const VIEWPORT_WIDTHS: Record<Viewport, number> = {
  desktop: 640, // Apple Mail / desktop Gmail / iPad portrait
  mobile: 380, // iPhone 15 Pro Safari webmail
};

export function EmailTemplatePreview({
  preview,
}: {
  preview: AdminEmailTemplatePreview;
}) {
  const { t } = useLocale();
  const [viewport, setViewport] = useState<Viewport>("desktop");

  return (
    <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div>
          <p className="text-[13px] font-medium text-fg-2">
            {t.admin.email.previewTitle}
          </p>
          <p className="mt-1 text-[12px] text-fg-4">{preview.subject}</p>
        </div>
        <ViewportToggle
          value={viewport}
          onChange={setViewport}
          labels={{
            desktop: t.admin.email.previewViewport?.desktop ?? "Desktop",
            mobile: t.admin.email.previewViewport?.mobile ?? "Mobile",
          }}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="flex justify-center overflow-x-auto rounded-surface border border-border/50 bg-white p-3">
          <iframe
            title={t.admin.email.previewFrameTitle}
            srcDoc={preview.html}
            style={{ width: VIEWPORT_WIDTHS[viewport] }}
            className="min-h-[420px] max-w-full border-0 transition-[width] duration-200"
          />
        </div>

        <div className="rounded-surface border border-border/50 bg-card p-3">
          <p className="text-[12px] font-medium text-fg-2">
            {t.admin.email.plainTextTitle}
          </p>
          <pre className="mt-2 whitespace-pre-wrap break-words text-[12px] leading-5 text-fg-3">
            {preview.text}
          </pre>
        </div>
      </div>
    </div>
  );
}

function ViewportToggle({
  value,
  onChange,
  labels,
}: {
  value: Viewport;
  onChange: (next: Viewport) => void;
  labels: { desktop: string; mobile: string };
}) {
  const options: { id: Viewport; icon: typeof Monitor; label: string }[] = [
    { id: "desktop", icon: Monitor, label: labels.desktop },
    { id: "mobile", icon: Smartphone, label: labels.mobile },
  ];
  return (
    <div
      role="radiogroup"
      aria-label={labels.desktop + " / " + labels.mobile}
      className="flex items-center gap-0.5 rounded-md border border-border/60 bg-card p-0.5"
    >
      {options.map((opt) => {
        const Icon = opt.icon;
        const active = value === opt.id;
        return (
          <button
            key={opt.id}
            type="button"
            role="radio"
            aria-checked={active}
            aria-label={opt.label}
            onClick={() => onChange(opt.id)}
            className={`flex h-7 w-7 items-center justify-center rounded cursor-pointer transition-colors ${
              active
                ? "bg-fg-1 text-background"
                : "text-fg-3 hover:text-fg-1 hover:bg-surface-2"
            }`}
          >
            <Icon className="h-3.5 w-3.5" />
          </button>
        );
      })}
    </div>
  );
}
