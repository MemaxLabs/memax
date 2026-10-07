"use client";

import { useCallback, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import type {
  DreamActionKind,
  DreamActionView,
  DreamSettingsView,
  UndoAllResult,
} from "@/lib/v2/data/dream";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useSource } from "./data";
import { recordKeys } from "./records";

/**
 * TanStack Query over the source's Dream (lib/v2/data/dream.ts): an
 * edition, the space's editions and schedule, and the person's settings.
 * Every key sits under the space's, so undoing one of Dream's actions
 * refreshes Today, Review and the overview with it. The demo's data
 * comes from `peek*`, so the first render (and every screenshot) has it.
 */

export const dreamKeys = {
  editions: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "dream", "editions"] as const,
  edition: (kind: string, slug: string, ref: string) =>
    [...recordKeys.space(kind, slug), "dream", "edition", ref] as const,
  settings: (kind: string) => ["v2", kind, "dream-settings"] as const,
};

export function useEditions(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: dreamKeys.editions(source.kind, space.slug),
    queryFn: ({ signal }) => source.dream.editions({ space, limit: 4, signal }),
    initialData: source.dream.peekEditions?.(space.slug),
    staleTime: 60_000,
  });
}

export function useEdition(space: SpaceSummary, ref: string) {
  const source = useSource();
  return useQuery({
    queryKey: dreamKeys.edition(source.kind, space.slug, ref),
    queryFn: ({ signal }) => source.dream.edition({ space, ref, signal }),
    initialData: source.dream.peekEdition?.(space.slug, ref),
    staleTime: 30_000,
  });
}

export function useDreamSettings() {
  const source = useSource();
  return useQuery({
    queryKey: dreamKeys.settings(source.kind),
    queryFn: ({ signal }) => source.dream.settings({ signal }),
    initialData: source.dream.peekSettings?.(),
    staleTime: 60_000,
  });
}

/** What a command on an edition came to. */
export type DreamOutcome<T = void> =
  | { ok: true; value: T }
  | { ok: false; failure: ReturnType<typeof toFailure> };

/**
 * Undo, Undo all, Restore and Run now on a space's edition. One key per
 * intent, reused while a retry is the same command. After each, the
 * space's queries refresh (the record changed).
 */
export function useDreamCommands(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  const keys = useRef(new IntentKeys()).current;
  const [pending, setPending] = useState<string | null>(null);

  const run = useCallback(
    async <T,>(intent: string, fn: (key: string) => Promise<T>) => {
      setPending(intent);
      try {
        const value = await fn(keys.keyFor(intent));
        keys.settle(intent);
        return { ok: true, value } as DreamOutcome<T>;
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        return { ok: false, failure } as DreamOutcome<T>;
      } finally {
        setPending(null);
        await queryClient.invalidateQueries({
          queryKey: recordKeys.space(source.kind, space.slug),
        });
      }
    },
    [keys, queryClient, source.kind, space.slug],
  );

  return {
    pending,
    undo: (action: DreamActionView) =>
      run(`undo:${action.id}`, (idempotencyKey) =>
        source.dream.undo({ space, action, idempotencyKey }),
      ),
    undoAll: (edition: string, kind: DreamActionKind) =>
      run<UndoAllResult>(`undo-all:${edition}:${kind}`, (idempotencyKey) =>
        source.dream.undoAll({ space, edition, kind, idempotencyKey }),
      ),
    restore: (ref: string, version: number | null) =>
      run(`restore:${ref}:${version ?? 0}`, (idempotencyKey) =>
        source.dream.restore({ space, ref, version, idempotencyKey }),
      ),
    runNow: () =>
      run(`run:${space.slug}`, (idempotencyKey) =>
        source.dream.run({ space, idempotencyKey }),
      ),
  };
}

/** Changing the person's Dream settings (the zone, the morning email). */
export function useUpdateDreamSettings() {
  const source = useSource();
  const queryClient = useQueryClient();
  const keys = useRef(new IntentKeys()).current;
  return useCallback(
    async (change: {
      timeZone?: string;
      morningEmail?: boolean;
    }): Promise<DreamOutcome<DreamSettingsView>> => {
      const intent = `settings:${change.timeZone ?? ""}:${String(change.morningEmail ?? "")}`;
      try {
        const value = await source.dream.updateSettings({
          ...change,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        queryClient.setQueryData(dreamKeys.settings(source.kind), value);
        return { ok: true, value };
      } catch (err) {
        const failure = toFailure(err);
        if (!isRetryable(failure)) keys.settle(intent);
        return { ok: false, failure };
      }
    },
    [keys, queryClient, source],
  );
}
