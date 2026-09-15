"use client";

/**
 * SecurityStrip (E3) — the landing's security section. One rule
 * inherited from the docs page it links to: only claim mechanisms
 * that are shipped and testable. Four points, each mapping to an
 * enforced mechanism (isolation layers, credential-bound identity,
 * verified synthesis, push-time credential rejection).
 */

import { ShieldCheck } from "lucide-react";
import { useLocale } from "@/i18n";
import { DOCS_URL } from "@/lib/urls";

export function SecurityStrip() {
  const { t } = useLocale();
  const sec = t.landing.security;
  const points = [sec.point1, sec.point2, sec.point3, sec.point4];
  return (
    <div className="w-full rounded-surface border border-[oklch(from_var(--foreground)_l_c_h/0.08)] px-5 py-5 sm:px-6">
      <div className="flex items-center gap-2">
        <ShieldCheck className="h-4 w-4 text-fg-2" aria-hidden />
        <h2 className="text-[15px] font-semibold text-fg-1">{sec.title}</h2>
      </div>
      <p className="mt-1 text-[13px] text-fg-3">{sec.subtitle}</p>
      <ul className="mt-3 grid gap-2 sm:grid-cols-2">
        {points.map((p) => (
          <li key={p} className="flex gap-2 text-[13px] text-fg-2">
            <span
              aria-hidden
              className="mt-[7px] h-1 w-1 shrink-0 rounded-full bg-fg-3"
            />
            {p}
          </li>
        ))}
      </ul>
      <a
        href={`${DOCS_URL}/docs/concepts/security`}
        target="_blank"
        rel="noopener noreferrer"
        className="mt-3 inline-block text-[13px] font-medium text-fg-2 underline decoration-border underline-offset-4 transition-colors hover:text-fg-1"
      >
        {sec.link}
      </a>
    </div>
  );
}
