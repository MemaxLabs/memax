"use client";

import { useState } from "react";
import { Check, ChevronRight, Fingerprint, X } from "lucide-react";
import type { Persona } from "memax-sdk";
import { resolveAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { useLocale, useInterpolate } from "@/i18n";
import {
  useApplyPersona,
  useDeletePersona,
  usePersonaRevision,
  usePersonaRevisions,
  useRestorePersonaRevision,
} from "@/hooks/use-personas";

export interface ApplyTarget {
  agent: string;
  scope: string; // "global" | "profile:<name>"
}

// One expandable body at a time — apply targets, history, or delete confirm.
// The card morphs in place per the container-morphing rule; no modals.
type CardMode = "idle" | "apply" | "history" | "confirmDelete";

export function targetLabel(target: ApplyTarget): string {
  const name = resolveAgentIdentity(target.agent).displayName;
  if (target.scope === "global") return name;
  return `${name} · ${target.scope.replace("profile:", "")}`;
}

export function PersonaCard({
  persona,
  targets,
}: {
  persona: Persona;
  targets: ApplyTarget[];
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const applyPersona = useApplyPersona();
  const deletePersona = useDeletePersona();
  const restoreRevision = useRestorePersonaRevision();

  const [mode, setMode] = useState<CardMode>("idle");
  const [feedback, setFeedback] = useState<string | null>(null);
  const [openVersion, setOpenVersion] = useState<number | null>(null);

  // Every mode transition clears stale success feedback — the feedback
  // branch renders in place of the action rows, so a lingering message
  // would otherwise lock the card out of apply/history/delete forever.
  const enterMode = (next: CardMode) => {
    setFeedback(null);
    setMode(next);
  };

  const revisionsQuery = usePersonaRevisions(persona.id, mode === "history");
  const revisionQuery = usePersonaRevision(persona.id, openVersion);

  const identity = resolveAgentIdentity(persona.source_agent);
  const Icon = identity.icon ?? Fingerprint;

  const sourceLabel = (() => {
    const name = resolveAgentIdentity(persona.source_agent).displayName;
    if (persona.source_scope.startsWith("profile:")) {
      return `${name} · ${persona.source_scope.replace("profile:", "")}`;
    }
    return name;
  })();

  const isConfirmingDelete = mode === "confirmDelete";

  return (
    <div
      className="rounded-2xl border border-border/50 bg-surface-1 px-4 py-3.5 transition-colors"
      style={
        isConfirmingDelete
          ? { backgroundColor: "oklch(from var(--destructive) l c h / 0.08)" }
          : undefined
      }
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
            {t.personas.sourceLabel} {sourceLabel}
            <span className="text-fg-4"> · v{persona.version}</span>
          </p>
        </div>
        {!isConfirmingDelete && (
          <button
            onClick={() => enterMode("confirmDelete")}
            className="text-fg-4 hover:text-destructive/70 transition-colors cursor-pointer shrink-0 p-0.5"
            aria-label={t.forget.button}
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {isConfirmingDelete ? (
        <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <span className="text-[12px] text-fg-2">
            {t.personas.forgetConfirm}
          </span>
          {/* Destructive LEFT, safe RIGHT — the cursor is on Keep. */}
          <div className="flex items-center gap-3 shrink-0">
            <button
              onClick={async () => {
                try {
                  await deletePersona.mutateAsync(persona.id);
                } catch {
                  // Error toast handled by the global mutation cache.
                  setMode("idle");
                }
              }}
              disabled={deletePersona.isPending}
              className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
            >
              {t.forget.button}
            </button>
            <button
              onClick={() => enterMode("idle")}
              disabled={deletePersona.isPending}
              className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer"
            >
              {t.forget.keep}
            </button>
          </div>
        </div>
      ) : feedback ? (
        <p className="mt-3 flex items-start gap-1.5 text-[12px] text-fg-2">
          <Check className="w-3.5 h-3.5 shrink-0 mt-0.5" strokeWidth={2} />
          <span>
            {feedback}
            <span className="block text-fg-4">{t.personas.watchHint}</span>
          </span>
        </p>
      ) : mode === "apply" ? (
        <div className="mt-3">
          <p className="text-[11px] uppercase tracking-wide text-fg-4 mb-1.5">
            {t.personas.applyTargetsTitle}
          </p>
          <div className="space-y-0.5">
            {targets.map((target) => (
              <button
                key={`${target.agent}|${target.scope}`}
                disabled={applyPersona.isPending}
                onClick={async () => {
                  try {
                    const res = await applyPersona.mutateAsync({
                      id: persona.id,
                      input: {
                        target_agent: target.agent,
                        target_scope: target.scope as
                          | "global"
                          | `profile:${string}`,
                      },
                    });
                    setFeedback(
                      interpolate(t.personas.applied, {
                        target: targetLabel({
                          agent: res.target_agent,
                          scope: res.target_scope,
                        }),
                      }),
                    );
                    setMode("idle");
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
            onClick={() => enterMode("idle")}
            className="mt-1.5 text-[12px] text-fg-4 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {t.personas.close}
          </button>
        </div>
      ) : mode === "history" ? (
        <div className="mt-3">
          <p className="text-[11px] uppercase tracking-wide text-fg-4 mb-1.5">
            {t.personas.historyTitle}
          </p>
          <div className="space-y-0.5">
            {(revisionsQuery.data ?? []).map((rev) => {
              const open = openVersion === rev.version;
              return (
                <div key={rev.version}>
                  {/* Toggle and Restore are SIBLING buttons — interactive
                      content nested in a button is invalid HTML and locks
                      keyboard users out of Restore. */}
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => setOpenVersion(open ? null : rev.version)}
                      className="flex-1 flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-[13px] text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
                    >
                      <ChevronRight
                        className={`w-3 h-3 text-fg-4 shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
                      />
                      {interpolate(t.personas.versionRow, { n: rev.version })}
                    </button>
                    {rev.version !== persona.version && (
                      <button
                        disabled={restoreRevision.isPending}
                        onClick={async () => {
                          try {
                            const res = await restoreRevision.mutateAsync({
                              id: persona.id,
                              version: rev.version,
                            });
                            setFeedback(
                              interpolate(t.personas.restored, {
                                n: res.restored_version,
                              }),
                            );
                            setMode("idle");
                          } catch {
                            // Error toast handled by the mutation cache.
                          }
                        }}
                        className="px-2 py-1.5 rounded-lg text-[12px] text-fg-3 hover:text-fg-1 hover:bg-surface-2 font-medium cursor-pointer transition-colors shrink-0 disabled:opacity-50 disabled:cursor-wait"
                      >
                        {t.personas.restoreCta}
                      </button>
                    )}
                  </div>
                  {open && revisionQuery.data?.version === rev.version && (
                    <pre className="mx-2.5 mb-1 rounded-lg bg-surface-2 text-[12px] text-fg-2 font-mono leading-relaxed p-2.5 max-h-40 overflow-y-auto whitespace-pre-wrap break-words animate-content-ready">
                      {revisionQuery.data.content}
                    </pre>
                  )}
                </div>
              );
            })}
          </div>
          <button
            onClick={() => {
              enterMode("idle");
              setOpenVersion(null);
            }}
            className="mt-1.5 text-[12px] text-fg-4 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {t.personas.close}
          </button>
        </div>
      ) : (
        <div className="mt-3 flex items-center gap-4">
          <button
            onClick={() => enterMode("apply")}
            className="text-[13px] font-medium text-fg-2 hover:text-fg-1 transition-colors cursor-pointer"
          >
            {t.personas.applyCta}
          </button>
          <button
            onClick={() => enterMode("history")}
            className="text-[13px] text-fg-3 hover:text-fg-1 transition-colors cursor-pointer"
          >
            {t.personas.historyCta}
          </button>
        </div>
      )}
    </div>
  );
}
