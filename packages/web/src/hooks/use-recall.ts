"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import type { RecallResult, RecalledMemory } from "memax-sdk";
import { useDevMode } from "@/hooks/use-dev-mode";
import { useMemaxDebugger } from "@/hooks/use-memax-debugger";
import { getMemaxClient } from "@/lib/memax-client";

export type { RecalledMemory, RecallResult };

/**
 * Recall hook — cross-hub by default.
 *
 * Respects SDK contract: the server searches all accessible hubs.
 * Passing hubId marks the active hub for a ranking boost only.
 */
export function useRecall(query: string, hubId?: string, topicId?: string) {
  const { flags } = useDevMode();
  const { enabled: debuggerEnabled, recordEvent } = useMemaxDebugger();
  const fetchStartedAtRef = useRef<number | null>(null);
  const lastSuccessAtRef = useRef(0);
  const lastErrorAtRef = useRef(0);

  const noRerank = flags.skipRerank || undefined;
  const recallQuery = useQuery<RecallResult>({
    queryKey: [
      "recall",
      query,
      hubId ?? "all",
      topicId ?? "all",
      noRerank ? "no-rerank" : "",
    ],
    queryFn: ({ signal }) =>
      getMemaxClient().memories.recall(query, {
        limit: 10,
        hubId,
        topicId,
        noRerank,
        signal,
      }),
    enabled: query.length > 0,
    staleTime: 60 * 1000,
    gcTime: 2 * 60 * 1000,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (debuggerEnabled === false || query.length === 0) {
      fetchStartedAtRef.current = null;
      return;
    }
    if (recallQuery.isFetching && fetchStartedAtRef.current === null) {
      fetchStartedAtRef.current = Date.now();
      recordEvent({
        channel: "recall",
        tone: "active",
        title: "Recall fetch started",
        detail: query,
        context: hubId ?? "all hubs",
      });
    }
  }, [debuggerEnabled, query, hubId, recallQuery.isFetching, recordEvent]);

  useEffect(() => {
    if (
      debuggerEnabled === false ||
      query.length === 0 ||
      recallQuery.dataUpdatedAt === 0 ||
      recallQuery.dataUpdatedAt === lastSuccessAtRef.current
    ) {
      return;
    }
    lastSuccessAtRef.current = recallQuery.dataUpdatedAt;
    const count = recallQuery.data?.memories.length ?? 0;
    const elapsed =
      fetchStartedAtRef.current === null
        ? undefined
        : Date.now() - fetchStartedAtRef.current;
    fetchStartedAtRef.current = null;
    recordEvent({
      channel: "recall",
      tone: count > 0 ? "success" : "neutral",
      title:
        count > 0 ? "Recall results ready" : "Recall finished with no results",
      detail: query,
      context: elapsed === undefined ? undefined : elapsed + "ms",
    });
  }, [
    debuggerEnabled,
    query,
    recallQuery.data,
    recallQuery.dataUpdatedAt,
    recordEvent,
  ]);

  useEffect(() => {
    if (
      debuggerEnabled === false ||
      query.length === 0 ||
      recallQuery.errorUpdatedAt === 0 ||
      recallQuery.errorUpdatedAt === lastErrorAtRef.current
    ) {
      return;
    }
    lastErrorAtRef.current = recallQuery.errorUpdatedAt;
    const elapsed =
      fetchStartedAtRef.current === null
        ? undefined
        : Date.now() - fetchStartedAtRef.current;
    fetchStartedAtRef.current = null;
    recordEvent({
      channel: "recall",
      tone: "error",
      title: "Recall failed",
      detail: query,
      context:
        elapsed === undefined
          ? recallQuery.error?.message
          : elapsed + "ms · " + recallQuery.error?.message,
    });
  }, [
    debuggerEnabled,
    query,
    recallQuery.error,
    recallQuery.errorUpdatedAt,
    recordEvent,
  ]);

  return recallQuery;
}
