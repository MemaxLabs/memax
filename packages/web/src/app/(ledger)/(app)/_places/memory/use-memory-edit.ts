"use client";

import { useCallback, useState } from "react";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type { LatestVersion, MemoryRecord } from "@/lib/v2/data/memories";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { failureText } from "@/lib/v2/records-copy";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { useAfterDecision } from "../../_lib/records";
import { useUndo } from "../../_lib/undo";

interface Base {
  version: number;
  statement: string;
}

export type MemoryEditMode =
  | { kind: "view" }
  | {
      kind: "editing";
      base: Base;
      draft: string;
      reason: string;
      error: boolean;
    }
  | {
      kind: "clash";
      base: Base;
      mine: string;
      reason: string;
      theirs: LatestVersion;
    };

/**
 * Edit (E) on a memory's page: a new version from the one on screen
 * (If-Match), one idempotency key per edit across retries, and the edit
 * clash when someone kept a change first. An edit policy sends to Review
 * comes back as a new proposal, and the toast says so.
 */
export function useMemoryEdit(
  space: SpaceSummary,
  record: MemoryRecord | null | undefined,
) {
  const source = useSource();
  const { t, locale } = useLocale();
  const rc = t.ledger.records;
  const toast = useToast();
  const afterDecision = useAfterDecision(space);
  const undo = useUndo();
  const [mode, setMode] = useState<MemoryEditMode>({ kind: "view" });
  const [pending, setPending] = useState(false);
  const [keys] = useState(() => new IntentKeys());

  const start = useCallback(() => {
    if (!record || record.lifecycle === "forgotten" || mode.kind !== "view")
      return false;
    setMode({
      kind: "editing",
      base: { version: record.version, statement: record.statement },
      draft: record.statement,
      reason: "",
      error: false,
    });
  }, [mode.kind, record]);

  const submit = useCallback(
    async (editing: Extract<MemoryEditMode, { kind: "editing" }>) => {
      if (!record || pending) return;
      const draft = editing.draft.trim();
      if (!draft) {
        setMode({ ...editing, error: true });
        return;
      }
      if (draft === editing.base.statement.trim()) {
        setMode({ kind: "view" });
        return;
      }
      const why = editing.reason.trim() || undefined;
      const intent = intentOf(
        "edit",
        record.ref,
        editing.base.version,
        draft,
        why,
      );
      setPending(true);
      try {
        const result = await source.memories.edit({
          space,
          ref: record.ref,
          version: editing.base.version,
          statement: draft,
          reason: why,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        setMode({ kind: "view" });
        // A person's edit can be undone for 10 minutes, by its receipt.
        const entry =
          result.outcome !== "proposed" && result.receipt
            ? undo.record({
                space,
                command: "edit",
                ref: result.ref,
                receipt: result.receipt,
                restore: null,
              })
            : null;
        toast(
          result.outcome === "proposed"
            ? {
                state: "proposed",
                text: interpolate(t.ledger.memory.page.proposedEdit, {
                  ref: result.ref,
                }),
              }
            : {
                state: "kept",
                text:
                  result.recompiled === null
                    ? interpolate(rc.toast.edited, { ref: result.ref })
                    : count(
                        rc.toast.editedRecompiledOne,
                        rc.toast.editedRecompiled,
                        result.recompiled,
                        {
                          ref: result.ref,
                        },
                      ),
                ...(entry ? { undo: () => void undo.run(entry) } : {}),
              },
        );
        afterDecision({ leftQueue: false });
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        if (failure.kind === "clash") {
          const theirs = await source.memories
            .latest({ space, ref: record.ref })
            .catch(() => null);
          if (theirs) {
            setMode({
              kind: "clash",
              base: editing.base,
              mine: draft,
              reason: editing.reason,
              theirs,
            });
            return;
          }
        }
        toast({
          state: "proposed",
          text: failureText(rc, failure, {
            command: "edit",
            ref: record.ref,
            space: space.name,
            locale,
          }),
          ...(isRetryable(failure)
            ? {
                action: {
                  label: rc.failure.retry,
                  onClick: () => void submit(editing),
                },
              }
            : {}),
        });
      } finally {
        setPending(false);
      }
    },
    [
      afterDecision,
      keys,
      locale,
      pending,
      rc,
      record,
      source,
      space,
      t,
      toast,
      undo,
    ],
  );

  const fromTheirs = useCallback(
    (clash: Extract<MemoryEditMode, { kind: "clash" }>) =>
      ({
        kind: "editing",
        base: {
          version: clash.theirs.version,
          statement: clash.theirs.statement,
        },
        draft: clash.mine,
        reason: clash.reason,
        error: false,
      }) as const,
    [],
  );

  return {
    mode,
    pending,
    start,
    cancel: () => setMode({ kind: "view" }),
    change: (patch: { draft?: string; reason?: string }) =>
      setMode((m) =>
        m.kind === "editing" ? { ...m, ...patch, error: false } : m,
      ),
    submit,
    keepTheirs: () => {
      setMode({ kind: "view" });
      afterDecision({ leftQueue: false });
    },
    combine: (clash: Extract<MemoryEditMode, { kind: "clash" }>) =>
      setMode(fromTheirs(clash)),
    keepMine: (clash: Extract<MemoryEditMode, { kind: "clash" }>) => {
      const next = fromTheirs(clash);
      setMode(next);
      void submit(next);
    },
  };
}
