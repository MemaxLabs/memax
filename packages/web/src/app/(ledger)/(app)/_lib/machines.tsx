"use client";

import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { interpolate, useLocale } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import { machineName, type MachineView } from "@/lib/v2/data/machines";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { useToast } from "../_components/toasts";
import { useSource, useSpaces } from "./data";
import { recordKeys } from "./records";

/**
 * TanStack Query over `source.machines` (lib/v2/data/machines.ts): the
 * machines a Personal space is written to, and renaming and unlinking
 * them. The list sits under the space's key, so an unlink refreshes its
 * targets, the overview and the status line with it. Only a personal
 * space on the V2 record has machines; for any other the query is off.
 */

export const machineKeys = {
  list: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "machines"] as const,
};

export function hasMachines(space: SpaceSummary | null | undefined): boolean {
  return space?.kind === "personal" && space.onV2 !== false;
}

export function useMachines(space: SpaceSummary | null | undefined) {
  const source = useSource();
  const on = hasMachines(space);
  return useQuery({
    queryKey: machineKeys.list(source.kind, space?.slug ?? ""),
    queryFn: ({ signal }) => source.machines.list({ space: space!, signal }),
    enabled: on,
    initialData: on ? source.machines.peekList?.(space!.slug) : undefined,
    staleTime: 60_000,
  });
}

/** The person's Personal space on the V2 record, from their spaces; null when there's none. */
export function usePersonalSpace(): SpaceSummary | null {
  const spaces = useSpaces().data ?? [];
  return spaces.find(hasMachines) ?? null;
}

/**
 * Rename (PATCH with If-Match on the version on screen) and Stop
 * compiling here (unlink), one idempotency key per intent, the same on a
 * retry. A clash refreshes the list and says the machine changed first.
 * Each says what happened in a toast and returns whether it went through.
 */
export function useMachineCommands(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  const { t } = useLocale();
  const m = t.ledger.machines;
  const toast = useToast();
  const [keys] = useState(() => new IntentKeys());
  const [pending, setPending] = useState<string | null>(null);

  const refresh = useCallback(
    (changed?: MachineView) => {
      if (changed) {
        queryClient.setQueryData<MachineView[]>(
          machineKeys.list(source.kind, space.slug),
          (list) => list?.map((x) => (x.id === changed.id ? changed : x)),
        );
      }
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
    },
    [queryClient, source.kind, space.slug],
  );

  const reason = useCallback(
    (err: unknown, intent: string) => {
      const failure = toFailure(err);
      if (!isRetryable(failure)) keys.settle(intent);
      return {
        failure,
        text: commandReason(
          t.ledger.records,
          t.ledger.brief,
          failure,
          space.name,
        ),
      };
    },
    [keys, space.name, t],
  );

  const rename = useCallback(
    async (
      machine: MachineView,
      name: string,
      all: readonly MachineView[],
    ): Promise<boolean> => {
      if (pending) return false;
      const intent = intentOf(
        "machine.rename",
        machine.id,
        machine.version,
        name,
      );
      const from = machineName(machine, all);
      setPending(`rename:${machine.id}`);
      try {
        const next = await source.machines.rename({
          space,
          machine,
          name,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        refresh(next);
        if (next.name !== machine.name) {
          toast({
            text: interpolate(m.toast.renamed, {
              from,
              to: machineName(next, all),
            }),
          });
        }
        return true;
      } catch (err) {
        const { failure, text } = reason(err, intent);
        refresh();
        toast({
          state: "proposed",
          text:
            failure.kind === "clash"
              ? interpolate(m.toast.renameClash, { machine: from })
              : interpolate(m.toast.renameFailed, {
                  machine: from,
                  reason: text,
                }),
        });
        return false;
      } finally {
        setPending(null);
      }
    },
    [keys, m, pending, reason, refresh, source, space, toast],
  );

  const unlink = useCallback(
    async (
      machine: MachineView,
      all: readonly MachineView[],
    ): Promise<boolean> => {
      if (pending) return false;
      const intent = intentOf("machine.unlink", machine.id, machine.version);
      const name = machineName(machine, all);
      setPending(`unlink:${machine.id}`);
      try {
        const next = await source.machines.unlink({
          space,
          machine,
          idempotencyKey: keys.keyFor(intent),
        });
        keys.settle(intent);
        refresh(next);
        toast({
          state: "off",
          text: interpolate(m.toast.stopped, { machine: name }),
        });
        return true;
      } catch (err) {
        const { text } = reason(err, intent);
        refresh();
        toast({
          state: "proposed",
          text: interpolate(m.toast.stopFailed, {
            machine: name,
            reason: text,
          }),
        });
        return false;
      } finally {
        setPending(null);
      }
    },
    [keys, m, pending, reason, refresh, source, space, toast],
  );

  return { rename, unlink, pending };
}
