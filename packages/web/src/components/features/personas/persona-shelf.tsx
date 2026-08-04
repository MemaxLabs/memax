"use client";

import { useLocale } from "@/i18n";
import { usePersonas } from "@/hooks/use-personas";
import { useSettings } from "@/hooks/use-settings";
import { PersonaCard } from "./persona-card";

/**
 * Personas (Beta) — identities extracted from synced SOUL/identity files.
 * Lives on the /agents page above the grid. Hidden entirely while the user
 * has no personas: the shelf introduces itself only once there is something
 * real to show. Cards bind personas to the memax agent (set as default);
 * per-session switching lives in the chat view's ChatPersonaPicker.
 */
export function PersonaShelf() {
  const { t } = useLocale();
  const { data: personas } = usePersonas();
  const { data: settings } = useSettings();

  if (!personas || personas.length === 0) return null;

  const defaultPersonaId = settings?.chat_default_persona_id ?? "";

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

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        {personas.map((persona) => (
          <PersonaCard
            key={persona.id}
            persona={persona}
            isDefault={persona.id === defaultPersonaId}
          />
        ))}
      </div>
    </section>
  );
}
