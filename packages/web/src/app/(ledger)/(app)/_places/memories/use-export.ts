"use client";

import { useCallback, useState } from "react";
import { interpolate, useLocale } from "@/i18n";
import {
  isRetryable,
  toFailure,
  type CommandFailure,
} from "@/lib/v2/data/command-error";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";

/** Saves a file the browser was handed, as a download. */
function save(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  // After the click has handed the file to the browser.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

type ExportFailedCopy = {
  refused: string;
  rateLimited: string;
  busy: string;
  unreachable: string;
  other: string;
};

/** Why an export didn't go through, in the catalogue's words. */
export function exportFailureText(
  copy: ExportFailedCopy,
  failure: CommandFailure,
  space: string,
): string {
  switch (failure.kind) {
    case "refused":
      return copy.refused;
    case "rate-limited":
      return failure.retryAfter !== null
        ? interpolate(copy.rateLimited, { n: failure.retryAfter })
        : copy.busy;
    case "busy":
      return copy.busy;
    case "unreachable":
      return copy.unreachable;
    default:
      return interpolate(copy.other, { space });
  }
}

/**
 * Memories' Export as Markdown (Memories.png, rule 14): the space's whole
 * record as a zip archive in the export format, saved as a download. One
 * idempotency key per press, reused if it is retried, so a dropped
 * connection never counts twice.
 */
export function useSpaceExport(space: SpaceSummary) {
  const source = useSource();
  const { t } = useLocale();
  const m = t.ledger.app.memories;
  const toast = useToast();
  const [pending, setPending] = useState(false);
  const [keys] = useState(() => new IntentKeys());

  const run = useCallback(async () => {
    if (pending) return;
    const intent = `export:${space.slug}`;
    setPending(true);
    try {
      const file = await source.memories.exportSpace({
        space,
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      save(file.blob, file.filename);
      toast({
        text:
          source.kind === "demo"
            ? m.exportedDemo
            : interpolate(m.exported, { space: space.name }),
      });
    } catch (err) {
      const failure = toFailure(err);
      if (!isRetryable(failure)) keys.settle(intent);
      toast({
        state: "proposed",
        text: exportFailureText(m.exportFailed, failure, space.name),
      });
    } finally {
      setPending(false);
    }
  }, [keys, m, pending, source, space, toast]);

  return { run, pending };
}
