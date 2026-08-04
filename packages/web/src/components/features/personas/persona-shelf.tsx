"use client";

import { useLocale } from "@/i18n";
import { usePersonas } from "@/hooks/use-personas";
import { useAgentConfigs } from "@/hooks/use-agent-configs";
import { PersonaCard, type ApplyTarget } from "./persona-card";

/** Agents whose identity file (SOUL.md) memax can write back. Mirrors the
 * write-path support in the CLI's agent-configs-discovery.ts. */
const PERSONA_TARGET_AGENTS = ["openclaw", "hermes"] as const;

/**
 * Personas (Beta) — identities extracted from synced SOUL/identity files.
 * Lives on the /agents page above the grid. Hidden entirely while the user
 * has no personas: the shelf introduces itself only once there is something
 * real to show. Each card owns its interactions (apply / history / forget)
 * — see persona-card.tsx.
 */
export function PersonaShelf() {
  const { t } = useLocale();
  const { data: personas } = usePersonas();
  const { data: configData } = useAgentConfigs();

  if (!personas || personas.length === 0) return null;

  // Candidate targets: each personal agent's global SOUL.md slot, plus one
  // per known Hermes profile (derived from already-synced config scopes).
  const targets: ApplyTarget[] = [];
  for (const agent of PERSONA_TARGET_AGENTS) {
    targets.push({ agent, scope: "global" });
  }
  const profileScopes = new Set<string>();
  for (const c of configData?.configs ?? []) {
    if (c.agent === "hermes" && c.scope.startsWith("profile:")) {
      profileScopes.add(c.scope);
    }
  }
  for (const scope of [...profileScopes].sort()) {
    targets.push({ agent: "hermes", scope });
  }

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
          <PersonaCard key={persona.id} persona={persona} targets={targets} />
        ))}
      </div>
    </section>
  );
}
