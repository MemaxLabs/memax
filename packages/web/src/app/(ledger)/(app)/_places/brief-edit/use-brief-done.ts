"use client";

import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { interpolate, useLocale } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type { BriefView } from "@/lib/v2/data/brief";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { useToast } from "../../_components/toasts";
import { compileKeys, useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";
import { briefHref } from "../brief";
import { donePlan, type EditorState } from "./editor-state";

export type DoneFailure =
  | { kind: "clash"; base: number | null }
  | { kind: "failed"; message: string }
  /** New facts were kept, but the version wasn't written. */
  | { kind: "partial"; message: string };

/**
 * Done (⌘↵): keeps every edit with the person's receipt, in order:
 * remember each new fact (kept as theirs), edit each fact whose words
 * changed (If-Match on the version they started from), then revise the
 * Brief from the version on screen. Each step has one idempotency key,
 * reused when Done is pressed again after a failure, so nothing is kept
 * twice. A clash leaves the edits in place on the newer version.
 */
export function useBriefDone(space: SpaceSummary, brief: BriefView) {
  const source = useSource();
  const { t } = useLocale();
  const b = t.ledger.brief;
  const toast = useToast();
  const router = useRouter();
  const queryClient = useQueryClient();
  const afterCompile = useAfterCompile(space);
  const [keys] = useState(() => new IntentKeys());
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<DoneFailure | null>(null);

  const done = useCallback(
    async (state: EditorState): Promise<number | null> => {
      if (pending) return null;
      setPending(true);
      setFailure(null);
      const plan = donePlan(state);
      const refs = new Map<string, string>();
      // Every step's key lives until the whole Done lands: pressed again
      // after a failure, a step that already went through replays.
      const intents: string[] = [];
      let kept = 0;
      try {
        for (const fact of plan.remember) {
          const intent = intentOf(
            "brief.remember",
            fact.key,
            0,
            fact.statement,
          );
          const result = await source.remember({
            space,
            statement: fact.statement,
            section: fact.section,
            idempotencyKey: keys.keyFor(intent),
          });
          intents.push(intent);
          kept += 1;
          // Only a kept memory can be placed; a viewer's goes to Review.
          if (result.outcome === "kept") refs.set(fact.key, result.ref);
        }
        for (const edit of plan.edits) {
          const intent = intentOf(
            "brief.edit",
            edit.ref,
            edit.version,
            edit.statement,
          );
          await source.memories.edit({
            space,
            ref: edit.ref,
            version: edit.version,
            statement: edit.statement,
            idempotencyKey: keys.keyFor(intent),
          });
          intents.push(intent);
        }
        const structure = plan.structure(refs);
        const intent = intentOf(
          "brief.revise",
          brief.id,
          state.base,
          JSON.stringify(structure),
        );
        const result = await source.brief.revise({
          space,
          base: state.base,
          structure,
          idempotencyKey: keys.keyFor(intent),
        });
        for (const step of [...intents, intent]) keys.settle(step);
        afterCompile();
        toast({
          state: "kept",
          text: interpolate(b.toast.kept, { ref: result.ref }),
        });
        router.push(briefHref(space.slug));
        return result.version;
      } catch (err) {
        const reason = toFailure(err);
        if (reason.kind === "clash") {
          // Someone revised it first: read theirs, keep ours on top.
          await queryClient.invalidateQueries({
            queryKey: compileKeys.brief(source.kind, space.slug),
          });
          const fresh = queryClient.getQueryData<BriefView | null>(
            compileKeys.brief(source.kind, space.slug),
          );
          setFailure({ kind: "clash", base: fresh?.version ?? null });
        } else {
          const message = commandReason(
            t.ledger.records,
            b,
            reason,
            space.name,
          );
          setFailure(
            kept > 0
              ? { kind: "partial", message }
              : { kind: "failed", message },
          );
          if (!isRetryable(reason)) {
            // A refusal would only repeat: the next Done is a new attempt.
            afterCompile();
          }
        }
        return null;
      } finally {
        setPending(false);
      }
    },
    [
      afterCompile,
      b,
      brief.id,
      keys,
      pending,
      queryClient,
      router,
      source,
      space,
      t,
      toast,
    ],
  );

  return { done, pending, failure, clearFailure: () => setFailure(null) };
}
