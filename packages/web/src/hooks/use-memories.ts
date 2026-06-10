"use client";

import { useEffect } from "react";
import {
  useQuery,
  useInfiniteQuery,
  useMutation,
  type InfiniteData,
} from "@tanstack/react-query";
import type { Memory, MemoryAttachment, MemoryUpdateInput } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { hubListQueryKey } from "./use-hubs";
import { usageQueryKey } from "./use-usage";
import { RECENT_PREVIEW_LIMIT } from "./use-recent-memories";
import {
  markRecentArrival,
  transferRecentArrival,
} from "@/lib/recent-arrivals";
import { memoryMatchesRecentActor } from "@/lib/recent-actor";
import {
  cancelMemoryListMutations,
  invalidateMemoryWriteCaches,
  applyOptimisticMemoryRemoval,
  restoreMemoryListCaches,
  snapshotMemoryListCaches,
  memoryDetailQueryKey,
  memoryListQueryKey,
  memoryListQueryPrefix,
  recentMemoriesQueryPrefix,
  type MemoriesListResponse,
  type MemoryListCacheSnapshot,
  type MemorySort,
} from "./memory-cache";

export type { Memory, MemoryAttachment, MemoriesListResponse, MemorySort };

/**
 * Plan 23 §4.1 — onboarding seed fields the server now writes but the
 * public `memax-sdk` Memory type doesn't yet expose. Runtime JSON
 * tolerance means consumers don't break; this bridge type lets any
 * web-side reader access the fields with proper TS narrowing without
 * an unsafe cast.
 *
 * Delete this when memax-sdk bumps to a version whose Memory type
 * includes source_kind + metadata. The .refs/memax/packages/sdk SDK
 * source has already been updated; the npm publish round-trip
 * removes the need for this shim.
 */
export type MemoryWithSeedFields = Memory & {
  source_kind?: string;
  metadata?: Record<string, unknown>;
};
export {
  memoryDetailQueryKey,
  memoryListQueryKey,
  memoryListQueryPrefix,
  recentMemoriesQueryPrefix,
};

export interface FileRef {
  object_key: string;
  filename: string;
  content_type: string;
  size_bytes?: number;
  sha256?: string;
}

export interface CreateMemoryInput {
  title: string;
  content?: string;
  content_type?: string;
  hint?: string;
  tags?: string[];
  boundary?: string;
  source?: string;
  source_path?: string;
  batch_id?: string;
  file_ref?: FileRef;
  hubId?: string;
  assisted_by_agent?: string;
}

/** Map frontend sort labels to backend query param values */
const SORT_PARAM: Record<MemorySort, "newest" | "relevant"> = {
  recent: "newest",
  recalled: "relevant",
};

function isRecentQueryKey(
  value: readonly unknown[],
): value is readonly [
  "recent-memories",
  string,
  "12h" | "1d" | "3d" | "7d",
  string,
  "preview" | "full",
] {
  return (
    value[0] === "recent-memories" &&
    typeof value[1] === "string" &&
    typeof value[2] === "string" &&
    typeof value[3] === "string" &&
    (value[4] === "preview" || value[4] === "full")
  );
}

function stripMemoryFromPages(
  pages: MemoriesListResponse[],
  ids: string[],
): {
  pages: MemoriesListResponse[];
  foundPageIndex: number;
  foundItemIndex: number;
} {
  const idSet = new Set(ids.filter(Boolean));
  let foundPageIndex = -1;
  let foundItemIndex = -1;

  const nextPages = pages.map((page, pageIndex) => {
    const memories: Memory[] = [];
    page.memories.forEach((item, itemIndex) => {
      if (idSet.has(item.id)) {
        if (foundPageIndex === -1) {
          foundPageIndex = pageIndex;
          foundItemIndex = itemIndex;
        }
        return;
      }
      memories.push(item);
    });
    return memories.length === page.memories.length
      ? page
      : { ...page, memories };
  });

  return { pages: nextPages, foundPageIndex, foundItemIndex };
}

function insertMemoryAtIndex(
  pages: MemoriesListResponse[],
  memory: Memory,
  pageIndex: number,
  itemIndex: number,
) {
  if (!pages.length) return pages;
  const nextPages = [...pages];
  const targetPageIndex = pageIndex >= 0 ? pageIndex : 0;
  const target = nextPages[targetPageIndex];
  if (!target) return pages;
  const memories = [...target.memories];
  const insertAt =
    pageIndex >= 0 && itemIndex >= 0 ? Math.min(itemIndex, memories.length) : 0;
  memories.splice(insertAt, 0, memory);
  nextPages[targetPageIndex] = { ...target, memories };
  return nextPages;
}

function upsertIntoRecentQueries(memory: Memory, optimisticId?: string) {
  queryClient
    .getQueriesData<InfiniteData<MemoriesListResponse>>({
      queryKey: recentMemoriesQueryPrefix,
    })
    .forEach(([queryKey, data]) => {
      if (
        !Array.isArray(queryKey) ||
        !isRecentQueryKey(queryKey) ||
        !data?.pages?.length
      ) {
        return;
      }

      const [, hubId, , actor, mode] = queryKey;
      if (hubId !== (memory.hub_id || "all")) return;
      if (
        !memoryMatchesRecentActor(
          memory,
          actor as "all" | `agent:${string}` | `author:${string}` | "self",
        )
      ) {
        return;
      }

      const hadExisting = data.pages.some((page) =>
        page.memories.some(
          (item) => item.id === memory.id || item.id === optimisticId,
        ),
      );
      const {
        pages: strippedPages,
        foundPageIndex,
        foundItemIndex,
      } = stripMemoryFromPages(data.pages, [memory.id, optimisticId ?? ""]);

      let nextPages = insertMemoryAtIndex(
        strippedPages,
        memory,
        foundPageIndex,
        foundItemIndex,
      );
      if (!hadExisting && nextPages[0]) {
        nextPages[0] = {
          ...nextPages[0],
          total: (nextPages[0].total ?? nextPages[0].memories.length) + 1,
        };
      }
      if (mode === "preview" && nextPages[0]) {
        nextPages[0] = {
          ...nextPages[0],
          memories: nextPages[0].memories.slice(0, RECENT_PREVIEW_LIMIT),
        };
      }

      queryClient.setQueryData(queryKey, {
        ...data,
        pages: nextPages,
      });
    });
}

function removeFromRecentQueries(optimisticId: string) {
  queryClient.setQueriesData<InfiniteData<MemoriesListResponse>>(
    { queryKey: recentMemoriesQueryPrefix },
    (old) => {
      if (!old?.pages?.length) return old;
      let removed = 0;
      const pages = old.pages.map((page) => {
        const memories = page.memories.filter((item) => {
          const keep = item.id !== optimisticId;
          if (!keep) removed += 1;
          return keep;
        });
        return memories.length === page.memories.length
          ? page
          : { ...page, memories };
      });
      if (removed === 0) return old;
      pages[0] = {
        ...pages[0],
        total: Math.max(
          0,
          (pages[0].total ?? pages[0].memories.length) - removed,
        ),
      };
      return { ...old, pages };
    },
  );
}

export function getMemoriesInfiniteQueryOptions({
  hubId,
  sort,
}: {
  hubId?: string;
  sort: MemorySort;
}) {
  return {
    queryKey: memoryListQueryKey(hubId, sort),
    queryFn: async ({
      pageParam,
      signal,
    }: {
      pageParam: unknown;
      signal: AbortSignal;
    }) => {
      const res = await getMemaxClient().memories.list({
        limit: 50,
        sort: SORT_PARAM[sort],
        cursor: (pageParam as string) || undefined,
        hubId,
        signal,
      });
      if (Array.isArray(res)) {
        return {
          memories: res,
          next_cursor: "",
          has_more: false,
          total: res.length,
        };
      }
      return res;
    },
    initialPageParam: "",
    getNextPageParam: (last: MemoriesListResponse) =>
      last.has_more ? last.next_cursor : undefined,
    staleTime: 30 * 1000,
    refetchInterval: false,
    refetchIntervalInBackground: false,
  } as const;
}

export function useMemories(sort: MemorySort = "recent", hubId?: string) {
  const { activeHubId } = useAuth();
  const resolvedHubId = (hubId ?? activeHubId) || undefined;

  return useInfiniteQuery<MemoriesListResponse, Error>({
    ...getMemoriesInfiniteQueryOptions({ hubId: resolvedHubId, sort }),
  });
}

export function getMemoryQueryOptions(id: string) {
  return {
    queryKey: memoryDetailQueryKey(id),
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      getMemaxClient().memories.get(id, { signal }),
    enabled: !!id,
    staleTime: 60 * 1000,
  } as const;
}

/** Flatten infinite query pages into a single Memory[] */
export function flattenMemories(
  data: InfiniteData<MemoriesListResponse> | undefined,
): Memory[] {
  if (!data) return [];
  return data.pages.flatMap((p) => p.memories);
}

/** Get total count from first page */
export function memoriesTotalCount(
  data: InfiniteData<MemoriesListResponse> | undefined,
): number {
  return data?.pages[0]?.total ?? 0;
}

export function useMemory(id: string) {
  return useQuery<Memory>({
    ...getMemoryQueryOptions(id),
    // SSE is the primary refresh path (hub.memories.changed via
    // MemaxEventBridge). Processing-aware polling is the safety net for
    // "worker-generated summary appears on reload but not live" — matches
    // Linear's sync-status / Vercel's deployment-status pattern.
    //
    // refetchInterval becomes 3000ms only while the memory is in the
    // transient processing state; once the fetch returns state=active (or
    // anything else terminal), React Query stops polling automatically.
    // refetchIntervalInBackground:false keeps hidden tabs quiet.
    refetchInterval: (query) =>
      query.state.data?.state === "processing" ? 3000 : false,
    refetchIntervalInBackground: false,
  });
}

// Per-session dedup: each memory id only fires one access signal for the
// lifetime of the tab, no matter how many times a detail page or modal
// is mounted for it. Two mounts inside a session (e.g. detail → back →
// detail, or strict-mode double-invoke) should not double-count the
// view — once the user has seen the memory this session, the decay
// signal is already recorded server-side.
/**
 * Dwell threshold for the access signal. The hook fires `trackAccessed`
 * only after the user has spent ≥ DWELL_MS of CUMULATIVE visible time
 * on the memory surface. Plan 21 §3.4 table: "Tab backgrounded mid-
 * dwell — timer pauses; if user returns and reaches 2s TOTAL, fires
 * once." Hidden time doesn't count toward the threshold but doesn't
 * reset it either; visible time accumulates across visibility flips.
 *
 * The intent is "intentional engagement," not "the modal mounted for
 * 50ms before the user dismissed it" or "user backgrounded the tab
 * 1.9s in to take a call and lost their progress."
 */
const DWELL_MS = 2000;

/**
 * sessionStorage key for the per-tab seen-memory set. Persisting to
 * sessionStorage (not module-level memory) means a hard reload
 * doesn't re-fire the access signal, but a new tab does — that
 * matches the user's mental model of "I've already seen this here."
 * Plan 21 §3.4 + Codex 21-8.
 */
const SEEN_MEMORIES_STORAGE_KEY = "memax_seen_memory_ids";

function readSeenMemoryIds(): Set<string> {
  try {
    // The `typeof sessionStorage` check sits inside the `try` so a
    // storage-blocked context (Safari ITP, restrictive iframe) where
    // even ACCESSING `sessionStorage` throws falls through to the
    // empty-set fallback instead of crashing. Codex 5.5 L-finding.
    if (typeof sessionStorage === "undefined") return new Set();
    const raw = sessionStorage.getItem(SEEN_MEMORIES_STORAGE_KEY);
    if (!raw) return new Set();
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? new Set(parsed.map(String)) : new Set();
  } catch {
    // Storage quota exceeded, JSON parse error, private-mode throw,
    // or storage-blocked-context throw — fall back to empty set.
    // Worst case: a refresh re-fires the access signal, which the
    // server-side SetNX dedup window catches.
    return new Set();
  }
}

function markMemorySeen(id: string): void {
  try {
    if (typeof sessionStorage === "undefined") return;
    const seen = readSeenMemoryIds();
    seen.add(id);
    sessionStorage.setItem(
      SEEN_MEMORIES_STORAGE_KEY,
      JSON.stringify(Array.from(seen)),
    );
  } catch {
    // Same fall-back rationale as readSeenMemoryIds: silent — server
    // dedup is the safety net.
  }
}

/**
 * Fire the deliberate-view signal for a memory ID exactly once per
 * tab session per ID, AND only after the user has visibly dwelled on
 * the surface for at least DWELL_MS. Mount this from surfaces that
 * represent real user engagement (detail page, modal) — never from
 * speculative paths like row prefetch or topic-tree mount.
 *
 * Plan 21 §3.4 / §4.3 redesign: the original implementation fired on
 * mount, so every modal-open and every page refresh incremented
 * `access_count` and updated `accessed_at`. "Recalled 12 times"
 * became noise instead of signal. The new tracker:
 *
 *   - Waits for ≥ DWELL_MS of CUMULATIVE visible time before firing.
 *     Tab-hidden time doesn't count toward the threshold but doesn't
 *     reset it either; visible time accumulates across visibility
 *     flips. visibility-flips update `lastTick` so the next tick
 *     doesn't credit hidden time as visible.
 *   - Persists fired IDs to sessionStorage so a hard reload within
 *     the same tab doesn't re-fire (was a module-level Set, lost on
 *     reload).
 *   - Fires once per ID per tab session — no `delete` on failure.
 *     Server-side SetNX dedup handles double-fires from the rare
 *     case where two tabs open the same memory around the same time.
 *
 * Silent on failure: the signal is advisory. A dropped ping does not
 * warrant surfacing an error to the user.
 */
export function useTrackMemoryAccessed(id: string | undefined | null) {
  useEffect(() => {
    if (!id) return;
    if (readSeenMemoryIds().has(id)) return;

    let elapsed = 0;
    let lastTick =
      typeof performance !== "undefined" ? performance.now() : Date.now();
    let fired = false;
    const tick = () => {
      const now =
        typeof performance !== "undefined" ? performance.now() : Date.now();
      if (
        typeof document === "undefined" ||
        document.visibilityState === "visible"
      ) {
        elapsed += now - lastTick;
      }
      lastTick = now;
      if (!fired && elapsed >= DWELL_MS) {
        fired = true;
        if (timer !== null) {
          clearInterval(timer);
          timer = null;
        }
        markMemorySeen(id);
        getMemaxClient()
          .memories.trackAccessed(id)
          .catch(() => {
            // Swallow: see jsdoc.
          });
      }
    };
    let timer: ReturnType<typeof setInterval> | null = setInterval(tick, 250);
    const onVisibilityChange = () => {
      // Reset the lastTick reference on visibility flip so the next
      // tick doesn't credit hidden time as visible time. The
      // accumulated `elapsed` carries forward unchanged.
      lastTick =
        typeof performance !== "undefined" ? performance.now() : Date.now();
    };
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", onVisibilityChange);
    }
    return () => {
      if (timer !== null) clearInterval(timer);
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", onVisibilityChange);
      }
    };
  }, [id]);
}

export function useCreateMemory() {
  const { activeHubId, hubs, user } = useAuth();
  const activeHub = hubs.find((entry) => entry.hub.id === activeHubId);
  return useMutation<
    Memory,
    Error,
    CreateMemoryInput,
    { optimisticId: string }
  >({
    // Push flow has its own undo-capable toast in bar-context — skip global handler
    meta: { skipGlobalToast: true },
    mutationFn: (input) =>
      getMemaxClient().memories.push(input.content ?? "", {
        title: input.title,
        hint: input.hint,
        tags: input.tags,
        source: input.source,
        assistedByAgent: input.assisted_by_agent,
        initiationType: input.assisted_by_agent
          ? "human_requested_agent"
          : undefined,
        sourcePath: input.source_path,
        contentType: input.content_type,
        fileRef: input.file_ref,
        hubId: input.hubId,
      }),
    // Optimistic insert — memory appears INSTANTLY as "processing" row
    // before the API call even starts. No "remembering" wait.
    onMutate: async (input) => {
      await queryClient.cancelQueries({ queryKey: memoryListQueryPrefix });

      const optimisticId = `optimistic-${Date.now()}`;
      const optimistic: Memory = {
        id: optimisticId,
        hub_id: input.hubId ?? activeHubId ?? "",
        owner_id: user?.id ?? "",
        title: input.title,
        content: input.content ?? "",
        content_type: input.content_type ?? "text",
        content_hash: "",
        summary: "",
        hint: input.hint,
        kind: "semantic",
        stability: "evolving",
        retrieval_weight: 1,
        tags: input.tags ?? [],
        boundary: "private",
        state: "processing",
        pinned: false,
        source: input.source ?? "web",
        source_agent: "",
        assisted_by_agent: input.assisted_by_agent ?? "",
        provenance: {
          created_by_type: "human",
          created_via: input.source ?? "web",
          assisted_by_agent: input.assisted_by_agent ?? "",
          initiation_type: input.assisted_by_agent
            ? "human_requested_agent"
            : "human_direct",
          attribution_source: "human",
        },
        source_path: input.source_path,
        hub_name:
          input.hubId && input.hubId !== activeHubId
            ? undefined
            : activeHub?.hub.name,
        version: 1,
        access_count: 0,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        accessed_at: new Date().toISOString(),
      };

      queryClient.setQueriesData<InfiniteData<MemoriesListResponse>>(
        { queryKey: memoryListQueryPrefix },
        (old) => {
          if (!old?.pages?.[0]) return old;
          const first = old.pages[0];
          return {
            ...old,
            pages: [
              {
                ...first,
                memories: [optimistic, ...first.memories],
                total: (first.total ?? 0) + 1,
              },
              ...old.pages.slice(1),
            ],
          };
        },
      );

      markRecentArrival(optimisticId);
      upsertIntoRecentQueries(optimistic);

      return { optimisticId };
    },
    onSuccess: (memory, _input, context) => {
      // Replace optimistic entry with real server response
      queryClient.setQueriesData<InfiniteData<MemoriesListResponse>>(
        { queryKey: memoryListQueryPrefix },
        (old) => {
          if (!old?.pages) return old;
          const { pages, foundPageIndex, foundItemIndex } =
            stripMemoryFromPages(old.pages, [
              memory.id,
              context?.optimisticId ?? "",
            ]);
          return {
            ...old,
            pages: insertMemoryAtIndex(
              pages,
              memory,
              foundPageIndex,
              foundItemIndex,
            ),
          };
        },
      );
      upsertIntoRecentQueries(memory, context?.optimisticId);
      if (context?.optimisticId) {
        transferRecentArrival(context.optimisticId, memory.id);
      } else {
        markRecentArrival(memory.id);
      }
    },
    onError: (_err, _input, context) => {
      // Rollback: remove the optimistic entry
      if (context?.optimisticId) {
        queryClient.setQueriesData<InfiniteData<MemoriesListResponse>>(
          { queryKey: memoryListQueryPrefix },
          (old) => {
            if (!old?.pages) return old;
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                memories: page.memories.filter(
                  (m) => m.id !== context.optimisticId,
                ),
                total: Math.max(0, (page.total ?? 0) - 1),
              })),
            };
          },
        );
        removeFromRecentQueries(context.optimisticId);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
      queryClient.invalidateQueries({ queryKey: recentMemoriesQueryPrefix });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
      queryClient.invalidateQueries({ queryKey: usageQueryKey });
    },
  });
}

/**
 * Web-side extension of the SDK's `MemoryUpdateInput`.
 *
 * The server's PATCH memories-by-id handler accepts `content` and
 * `content_type` (see `packages/server/internal/handler/memories.go`
 * `Update`), but the npm-published `memax-sdk@0.4.x` types omit them
 * — they were added upstream in `.refs/memax/packages/sdk/src/types.ts`
 * but haven't been released + bumped here yet.
 *
 * Rather than rely on a patched `node_modules/memax-sdk/dist/types.d.ts`
 * (which a clean `pnpm install` would blow away), we widen the
 * mutation input here. The SDK's `memories.update(id, patch)` already
 * forwards the entire `patch` body to PATCH, so the runtime path
 * works fine; this typing extension just lets the call site send the
 * fields the server already accepts. Codex 5.5 H-finding.
 *
 * Delete the `Pick<>` widening + this comment when the SDK is bumped
 * to a version whose `MemoryUpdateInput` includes content/content_type.
 */
type MemoryUpdateInputExtended = MemoryUpdateInput & {
  content?: string;
  content_type?: string;
};

type UpdateInput = { id: string } & MemoryUpdateInputExtended;

export function useUpdateMemory() {
  const { t } = useLocale();
  return useMutation<
    Memory,
    Error,
    UpdateInput,
    { previous: Memory | undefined; id: string }
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.updateMemory,
    },
    mutationFn: ({ id, ...data }) =>
      // Cast at the SDK boundary: the published `memax-sdk@0.4.x`
      // `MemoryUpdateInput` doesn't yet include content/content_type
      // (added upstream in .refs but not released). The SDK's
      // `update()` forwards the body verbatim to PATCH, so the
      // runtime path is fine; the cast just bypasses the stale
      // published type. Drop when the SDK bump lands.
      getMemaxClient().memories.update(id, data as MemoryUpdateInput),
    onMutate: async (variables) => {
      const { id, ...patch } = variables;
      await queryClient.cancelQueries({ queryKey: memoryDetailQueryKey(id) });
      const previous = queryClient.getQueryData<Memory>(
        memoryDetailQueryKey(id),
      );
      if (previous) {
        queryClient.setQueryData<Memory>(memoryDetailQueryKey(id), {
          ...previous,
          ...patch,
          updated_at: new Date().toISOString(),
        });
      }
      return { previous, id };
    },
    onError: (_err, _vars, context) => {
      if (context?.previous) {
        queryClient.setQueryData(
          memoryDetailQueryKey(context.id),
          context.previous,
        );
      }
      // Error toast handled globally via meta.errorMessage
    },
    onSettled: (_, __, variables) => {
      queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
      queryClient.invalidateQueries({
        queryKey: memoryDetailQueryKey(variables.id),
      });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}

export function useDeleteMemory(options?: { skipGlobalToast?: boolean }) {
  const { t } = useLocale();
  return useMutation<void, Error, string, MemoryListCacheSnapshot>({
    meta: options?.skipGlobalToast
      ? { skipGlobalToast: true }
      : {
          errorMessage: t.toast.deleteFailed,
          errorAction: t.errors.action.deleteMemory,
        },
    mutationFn: (id) => getMemaxClient().memories.delete(id),
    onMutate: async (id) => {
      await cancelMemoryListMutations();
      const snapshot = snapshotMemoryListCaches();
      applyOptimisticMemoryRemoval([id]);
      return snapshot;
    },
    onError: (_err, _id, snapshot) => {
      restoreMemoryListCaches(snapshot);
    },
    onSettled: (_data, _error, id) => {
      invalidateMemoryWriteCaches([id]);
    },
  });
}

export function useShareMemory() {
  const { t } = useLocale();
  return useMutation<Memory, Error, { id: string; targetHubId: string }>({
    meta: {
      successMessage: t.toast.shared,
      errorMessage: t.toast.shareFailed,
      errorAction: t.errors.action.shareMemory,
    },
    mutationFn: ({ id, targetHubId }) =>
      getMemaxClient().memories.share(id, targetHubId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: memoryListQueryPrefix });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: hubListQueryKey });
    },
  });
}
