"use client";

import { useState } from "react";
import { Check, ChevronRight, Fingerprint } from "lucide-react";
import type { Persona } from "memax-sdk";
import { resolveAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { useLocale, useInterpolate } from "@/i18n";
import { usePersonas, useApplyPersona } from "@/hooks/use-personas";
import { useAgentConfigs } from "@/hooks/use-agent-configs";

/** Agents whose identity file (SOUL.md) memax can write back. Mirrors the
 * write-path support in the CLI's agent-configs-discovery.ts. */
const PERSONA_TARGET_AGENTS = ["openclaw", "hermes"] as const;

interface ApplyTarget {
  agent: string;
  scope: string; // "global" | "profile:<name>"
}

function targetKey(t: ApplyTarget): string {
  return `${t.agent}|${t.scope}`;
}

/**
 * Personas (Beta) — identities extracted from synced SOUL/identity files.
 * Lives on the /agents page above the grid. Hidden entirely while the user
 * has no personas: the shelf introduces itself only once there is something
 * real to show. Apply is a two-step container morph (card expands into a
 * target list — no modal), and success feedback is honest about mechanics:
 * the write lands in the cloud and reaches the device on the next
 * `memax agents sync`.
 */
export function PersonaShelf() {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { data: personas } = usePersonas();
  const { data: configData } = useAgentConfigs();
  const applyPersona = useApplyPersona();

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [appliedTo, setAppliedTo] = useState<Record<string, string>>({});

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

  const targetLabel = (target: ApplyTarget): string => {
    const name = resolveAgentIdentity(target.agent).displayName;
    if (target.scope === "global") return name;
    return `${name} · ${target.scope.replace("profile:", "")}`;
  };

  const sourceLabel = (persona: Persona): string => {
    const name = resolveAgentIdentity(persona.source_agent).displayName;
    if (persona.source_scope.startsWith("profile:")) {
      return `${name} · ${persona.source_scope.replace("profile:", "")}`;
    }
    return name;
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

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        {personas.map((persona) => {
          const identity = resolveAgentIdentity(persona.source_agent);
          const Icon = identity.icon ?? Fingerprint;
          const expanded = expandedId === persona.id;
          const appliedTarget = appliedTo[persona.id];

          return (
            <div
              key={persona.id}
              className="rounded-2xl border border-border/50 bg-surface-1 px-4 py-3.5"
            >
              <div className="flex items-start gap-3">
                <div
                  className="w-8 h-8 rounded-chrome flex items-center justify-center shrink-0 mt-0.5"
                  style={{
                    backgroundColor: `oklch(from ${identity.color} l c h / 0.12)`,
                  }}
                >
                  <Icon
                    className="w-4 h-4"
                    style={{ color: identity.color }}
                    strokeWidth={1.8}
                  />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-[14px] font-medium text-fg-1 truncate">
                    {persona.name}
                  </p>
                  <p className="text-[12px] text-fg-3 truncate">
                    {t.personas.sourceLabel} {sourceLabel(persona)}
                    <span className="text-fg-4"> · v{persona.version}</span>
                  </p>
                </div>
              </div>

              {/* Apply — the card morphs into the target list; no modal. */}
              {appliedTarget ? (
                <p className="mt-3 flex items-center gap-1.5 text-[12px] text-fg-2">
                  <Check className="w-3.5 h-3.5 shrink-0" strokeWidth={2} />
                  {interpolate(t.personas.applied, { target: appliedTarget })}
                </p>
              ) : expanded ? (
                <div className="mt-3">
                  <p className="text-[11px] uppercase tracking-wide text-fg-4 mb-1.5">
                    {t.personas.applyTargetsTitle}
                  </p>
                  <div className="space-y-0.5">
                    {targets.map((target) => (
                      <button
                        key={targetKey(target)}
                        disabled={applyPersona.isPending}
                        onClick={async () => {
                          try {
                            await applyPersona.mutateAsync({
                              id: persona.id,
                              input: {
                                target_agent: target.agent,
                                target_scope: target.scope as
                                  | "global"
                                  | `profile:${string}`,
                              },
                            });
                            setAppliedTo((prev) => ({
                              ...prev,
                              [persona.id]: targetLabel(target),
                            }));
                            setExpandedId(null);
                          } catch {
                            // Error toast handled by the global mutation cache.
                          }
                        }}
                        className="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-[13px] text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-wait"
                      >
                        <ChevronRight className="w-3 h-3 text-fg-4 shrink-0" />
                        {targetLabel(target)}
                      </button>
                    ))}
                  </div>
                  <button
                    onClick={() => setExpandedId(null)}
                    className="mt-1.5 text-[12px] text-fg-4 hover:text-fg-2 transition-colors cursor-pointer"
                  >
                    {t.forget.keep}
                  </button>
                </div>
              ) : (
                <button
                  onClick={() => setExpandedId(persona.id)}
                  className="mt-3 text-[13px] font-medium text-fg-2 hover:text-fg-1 transition-colors cursor-pointer"
                >
                  {t.personas.applyCta}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
