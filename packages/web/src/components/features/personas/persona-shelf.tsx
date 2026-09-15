"use client";

import { MemaxLogo } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { usePersonas } from "@/hooks/use-personas";
import { useSettings, useUpdateSettings } from "@/hooks/use-settings";
import { PersonaCard } from "./persona-card";

/**
 * Personas (Beta) — identities extracted from synced SOUL/identity
 * files, on the /agents page.
 *
 * 2026-09-15 founder redesign: the shelf is a RADIO GROUP. Clicking a
 * card selects it (highlight = the selected state; no 设为默认 CTA,
 * no 默认 badge, no success sentence — the highlight moving IS the
 * feedback). A leading standalone card represents memax's own voice
 * (no persona) and is selected when nothing else is — "default" is a
 * choice you can see, not a label on someone else's card.
 */
export function PersonaShelf() {
  const { t } = useLocale();
  const { data: personas } = usePersonas();
  const { data: settings } = useSettings();
  const updateSettings = useUpdateSettings();

  if (!personas || personas.length === 0) return null;

  const defaultPersonaId = settings?.chat_default_persona_id ?? "";
  const select = (id: string) => {
    if (id === defaultPersonaId) return;
    updateSettings.mutate({ chat_default_persona_id: id });
  };

  return (
    <section className="mb-8">
      <div className="flex items-center gap-2 mb-1">
        <h2 className="text-[15px] font-semibold text-fg-1">
          {t.personas.title}
        </h2>
        <span className="text-[10px] font-medium uppercase tracking-wide text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded">
          {t.personas.beta}
        </span>
      </div>
      <p className="text-[13px] text-fg-3 mb-4">{t.personas.subtitle}</p>

      <div
        role="radiogroup"
        aria-label={t.personas.title}
        className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3"
      >
        <MemaxVoiceCard
          selected={defaultPersonaId === ""}
          onSelect={() => select("")}
        />
        {personas.map((persona) => (
          <PersonaCard
            key={persona.id}
            persona={persona}
            selected={persona.id === defaultPersonaId}
            onSelect={() => select(persona.id)}
          />
        ))}
      </div>
    </section>
  );
}

/**
 * The standalone default card — memax speaking as itself. Leads the
 * radio group so "no persona" is a first-class visible choice.
 */
function MemaxVoiceCard({
  selected,
  onSelect,
}: {
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useLocale();
  return (
    <div
      role="radio"
      aria-checked={selected}
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
      className={`cursor-pointer rounded-2xl border px-4 py-3.5 transition-colors ${
        selected
          ? "border-transparent bg-surface-1 shadow-glow"
          : "border-border/50 bg-surface-1 hover:bg-surface-2/60"
      }`}
      style={
        selected
          ? { borderColor: "oklch(from var(--signature) l c h / 0.55)" }
          : undefined
      }
    >
      <div className="flex items-start gap-3">
        <div
          className="w-8 h-8 rounded-chrome flex items-center justify-center shrink-0 mt-0.5"
          style={{
            backgroundColor: "var(--sig-soft, oklch(0.62 0.16 290 / 0.1))",
          }}
        >
          <MemaxLogo size={16} />
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-[14px] font-medium text-fg-1 truncate">
            {t.personas.defaultCardTitle}
          </p>
          <p className="text-[12px] text-fg-3 truncate">
            {t.personas.defaultCardBody}
          </p>
        </div>
      </div>
    </div>
  );
}
