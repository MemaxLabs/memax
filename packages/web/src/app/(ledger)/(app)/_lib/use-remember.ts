"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type {
  KeepResult,
  RememberCheck,
  Section,
  SpaceSummary,
} from "@/lib/v2/data/types";
import { ledgerQueryKeys, useSource } from "./data";

/** Plan §5.8: the near-duplicate check runs debounced while the person types. */
export const REMEMBER_CHECK_DEBOUNCE_MS = 150;

interface Checked {
  statement: string;
  slug: string;
  result: RememberCheck;
}

/**
 * Remember (⌘K): the debounced near-duplicate check, the section and
 * target space, and Keep. Keep waits for the check of the exact text
 * it keeps, so a duplicate the person hasn't seen yet is shown first
 * instead of kept past (the check is "synchronous", plan §6.4).
 */
export function useRemember(statement: string, initialSpace: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  const [space, setSpace] = useState(initialSpace);
  const [sectionChoice, setSectionChoice] = useState<Section | null>(null);
  const [checked, setChecked] = useState<Checked | null>(null);
  const [pending, setPending] = useState(false);
  const shownDuplicate = useRef<string | null>(null);
  const text = statement.trim();

  const runCheck = useCallback(
    (value: string, target: SpaceSummary, signal?: AbortSignal) => {
      return source
        .checkRemember({ space: target, statement: value, signal })
        .then((result) => ({ statement: value, slug: target.slug, result }));
    },
    [source],
  );

  useEffect(() => {
    if (!text) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      runCheck(text, space, controller.signal)
        .then((next) => {
          if (!controller.signal.aborted) setChecked(next);
        })
        .catch(() => {});
    }, REMEMBER_CHECK_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [text, space, runCheck]);

  const current =
    checked && checked.statement === text && checked.slug === space.slug
      ? checked.result
      : null;
  const duplicate = text ? (current?.duplicate ?? null) : null;
  const duplicateRef = duplicate?.ref ?? null;
  useEffect(() => {
    // Once the offer is on screen, Keep may go past it.
    if (duplicateRef) shownDuplicate.current = duplicateRef;
  }, [duplicateRef]);
  const section: Section = sectionChoice ?? current?.section ?? "conventions";

  const invalidate = useCallback(
    (slug: string) =>
      queryClient.invalidateQueries({
        queryKey: ledgerQueryKeys.overview(source.kind, slug),
      }),
    [queryClient, source.kind],
  );

  /** Keeps the statement; resolves to null when a fresh duplicate was shown instead. */
  const keep = useCallback(async (): Promise<KeepResult | null> => {
    if (!text || pending) return null;
    setPending(true);
    try {
      let result = current;
      if (!result) {
        const fresh = await runCheck(text, space);
        setChecked(fresh);
        result = fresh.result;
      }
      if (result.duplicate && shownDuplicate.current !== result.duplicate.ref) {
        shownDuplicate.current = result.duplicate.ref;
        return null;
      }
      const kept = await source.remember({
        space,
        statement: text,
        section: sectionChoice ?? result.section ?? "conventions",
        idempotencyKey: crypto.randomUUID(),
      });
      void invalidate(space.slug);
      return kept;
    } finally {
      setPending(false);
    }
  }, [
    text,
    pending,
    current,
    runCheck,
    space,
    source,
    sectionChoice,
    invalidate,
  ]);

  const keepDuplicate = useCallback(async (): Promise<KeepResult | null> => {
    if (!duplicate || pending) return null;
    setPending(true);
    try {
      const kept = await source.keepProposal({
        space,
        ref: duplicate.ref,
        idempotencyKey: crypto.randomUUID(),
      });
      void invalidate(space.slug);
      return kept;
    } finally {
      setPending(false);
    }
  }, [duplicate, pending, source, space, invalidate]);

  return {
    text,
    space,
    setSpace,
    section,
    setSection: setSectionChoice,
    duplicate,
    condition: text ? (current?.condition ?? null) : null,
    pending,
    keep,
    keepDuplicate,
  };
}
