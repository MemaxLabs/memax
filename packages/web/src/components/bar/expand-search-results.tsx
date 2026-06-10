"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Folder, Hash, type LucideIcon } from "lucide-react";
import type { Memory } from "memax-sdk";
import { usePathname, useRouter } from "next/navigation";
import { useBar } from "@/contexts/bar-context";
import { useMemories, flattenMemories } from "@/hooks/use-memories";
import { useBarSearch } from "@/hooks/use-bar-search";
import { useTopics, type TopicTree } from "@/hooks/use-topics";
import { useAuth, useActiveHub } from "@/lib/auth";
import { getHubDisplayName } from "@/lib/hub-display";
import { useLocale, useInterpolate } from "@/i18n";
import { useCopy } from "@/hooks/use-copy";
import { formatContextBlock } from "@/lib/format-context";
import { requestSurfaceTransitionForNavigation } from "@/lib/recent-navigation";
import { stripMarkdown } from "@/lib/strip-markdown";
import { parseMention, parseSlashCommand } from "@/lib/bar-interaction";
import {
  buildMemoriesPath,
  buildMemoryDetailPath,
  buildTopicPath,
  getHubSlugForPath,
  getTopicIdForPath,
} from "@/lib/route-helpers";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { CommandRow } from "./command-row";
import { HintBar } from "./hint-bar";
import { LoadMoreRow } from "./load-more-row";
import { RememberRow } from "./remember-row";
import { SourceRow } from "./source-row";
import { SynthesisSlot } from "./synthesis-slot";

const MAX_KEYWORD = 8;
const VISIBLE_SOURCES = 3;

function snippet(text: string, maxLen = 120) {
  if (!text) return "";
  return stripMarkdown(text).slice(0, maxLen);
}

// Ranks a memory against the typed tokens for the client-side keyword
// filter. Title-prefix hits outrank title-contains, which outrank body
// hits (body is implicit once the row passes the filter). A mild
// recency bonus pushes recently-updated memories up the list so "aut"
// surfaces the auth note you edited today above the one you wrote
// a year ago — aligns with Notion / Linear quick-find behavior.
function scoreKeywordMatch(memory: Memory, tokens: string[]): number {
  const titleLower = memory.title.toLowerCase();
  let score = 0;
  for (const token of tokens) {
    if (titleLower.startsWith(token)) score += 100;
    else if (titleLower.includes(token)) score += 30;
    else score += 5;
  }
  const updatedMs = Date.parse(memory.updated_at || memory.created_at || "");
  if (Number.isFinite(updatedMs)) {
    const ageDays = (Date.now() - updatedMs) / 86_400_000;
    score += Math.max(0, 50 - ageDays / 7);
  }
  return score;
}

function buildTopicLookup(topics: TopicTree[] | undefined) {
  const map = new Map<string, string>();
  if (!topics) return map;
  const stack = [...topics];
  while (stack.length) {
    const topic = stack.shift();
    if (!topic) continue;
    map.set(topic.id, topic.name);
    stack.push(...topic.children);
  }
  return map;
}

function flattenTopics(topics: TopicTree[] | undefined) {
  const flat: TopicTree[] = [];
  if (!topics) return flat;
  const stack = [...topics];
  while (stack.length) {
    const topic = stack.shift();
    if (!topic) continue;
    flat.push(topic);
    stack.push(...topic.children);
  }
  return flat;
}

export function ExpandSearchResults() {
  const {
    value,
    phase,
    recallResult,
    recallQuery,
    recallFetching,
    recallIsError,
    retryRecall,
    askAnswer,
    aiLoading,
    aiStreaming,
    canUseAI,
    selectedMemory,
    stagedFiles,
    sendKind,
    rememberState,
    rememberedTitle,
    undoRemember,
    retryRemember,
    activeNavigationIndex,
    setNavigationItems,
    commitCommandScope,
    peelForNavigation,
    routeTopicDismissed,
    setValue,
    isMobile,
    inputRef,
    handleRememberSubmit,
    handleSubmit,
    scope,
    interaction,
    surface,
  } = useBar();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const pathname = usePathname();
  const { activeHubId, hubs, switchHub, user } = useAuth();
  const { hubFilter, activeHub, isTeamHub } = useActiveHub();
  const { data: memoriesPages } = useMemories("recent", hubFilter);
  const { data: topicsData } = useTopics(hubFilter);
  const { copied: answerCopied, copy: copyAnswer } = useCopy();
  const { copied: contextCopied, copy: copyContext } = useCopy();
  const [sourcesExpanded, setSourcesExpanded] = useState(false);

  const allMemories = useMemo(
    () => flattenMemories(memoriesPages),
    [memoriesPages],
  );
  const topicLookup = useMemo(
    () => buildTopicLookup(topicsData?.topics),
    [topicsData?.topics],
  );
  const flatTopics = useMemo(
    () => flattenTopics(topicsData?.topics),
    [topicsData?.topics],
  );
  const hubNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const hub of hubs) {
      map.set(
        hub.hub.id,
        getHubDisplayName(hub.hub, t, {
          name: user?.name,
          displayName: user?.display_name,
        }),
      );
    }
    return map;
  }, [hubs, t, user?.display_name, user?.name]);
  // Result-tap pattern: peel the bar (snapshot + clear recall surface)
  // BEFORE navigating. The destination bar rests; return via back or
  // sidebar rehydrates the exact prior state from the snapshot.
  // Hub slug from the URL — `null` on v1 routes / non-hub routes.
  // Builders fall through to v1 paths when null; on v2 hub-scoped pages
  // they emit `/h/<slug>/...` so a recall result tap stays on the
  // current hub instead of being middleware-redirected to personal.
  const currentHubSlug = getHubSlugForPath(pathname);
  const navigateToMemory = useCallback(
    (memoryId: string) => {
      peelForNavigation();
      router.push(buildMemoryDetailPath(currentHubSlug, memoryId));
    },
    [currentHubSlug, peelForNavigation, router],
  );
  const navigateToTopic = useCallback(
    (topicId: string) => {
      peelForNavigation();
      const destination = buildTopicPath(currentHubSlug, topicId);
      requestSurfaceTransitionForNavigation(pathname, destination);
      router.push(destination, { scroll: false });
    },
    [currentHubSlug, pathname, peelForNavigation, router],
  );
  const slashCommand = useMemo(() => parseSlashCommand(value), [value]);
  const rawCommandPrefix = interaction.isCommandScope
    ? ""
    : (slashCommand?.command ?? "");
  const scopedCommand = scope.kind === "command" ? scope.command : null;
  const inCommandMode = phase === "input" && interaction.isCommandMode;
  const mention = useMemo(() => parseMention(value), [value]);
  const inMentionMode = phase === "input" && interaction.isMentionMode;
  const suppressKeywordSearch = interaction.hasFiles || inMentionMode;
  const showRecallSurfaces = surface.showSynthesis || surface.showRecallResults;

  // Derive active topic from route for client-side keyword filtering +
  // labels. When the user has dismissed the route-topic chip, the route
  // no longer drives scoping — recall + filter + labels ALL go global so
  // the chip state and the result surface stay in lockstep (addresses
  // user ask 2026-04-21 — "why am I still seeing Related in {topic} when
  // I deleted the chip"). The URL remains on the topic page.
  // Match either v1 (`/memories/topics/<id>`) or v2 (`/h/<slug>/topics/<id>`)
  // so the route-derived chip and active-topic scoping are identical under
  // both shells.
  const routeTopicIdFromPath = getTopicIdForPath(pathname) ?? null;
  const activeTopicId =
    routeTopicIdFromPath && !routeTopicDismissed ? routeTopicIdFromPath : null;
  const activeTopicName = activeTopicId
    ? (topicLookup.get(activeTopicId) ?? null)
    : null;

  const typingQuery =
    phase === "input" &&
    value.trim().length > 0 &&
    !suppressKeywordSearch &&
    scope.kind !== "command" &&
    !value.startsWith("/")
      ? value.trim().toLowerCase()
      : interaction.isCommandScope
        ? value.trim().toLowerCase()
        : (slashCommand?.query ?? "");

  // Split the typed query into whitespace-separated tokens so multi-word queries
  // use AND semantics ("auth callback" matches only rows that contain both
  // tokens somewhere). Matches Notion/Linear quick-find behavior.
  const keywordTokens = useMemo(() => {
    if (
      !typingQuery ||
      value.startsWith("/") ||
      suppressKeywordSearch ||
      inCommandMode
    )
      return [];
    return typingQuery.split(/\s+/).filter((t) => t.length > 0);
  }, [inCommandMode, suppressKeywordSearch, typingQuery, value]);

  const keywordResults = useMemo(() => {
    if (keywordTokens.length === 0) return [];
    const matches = allMemories.filter((memory) => {
      // When on a topic page, restrict keyword matches to that topic
      if (activeTopicId && memory.topic_id !== activeTopicId) return false;
      const haystack =
        `${memory.title} ${memory.content ?? ""} ${memory.summary ?? ""}`.toLowerCase();
      return keywordTokens.every((token) => haystack.includes(token));
    });
    return matches.sort(
      (a, b) =>
        scoreKeywordMatch(b, keywordTokens) -
        scoreKeywordMatch(a, keywordTokens),
    );
  }, [activeTopicId, allMemories, keywordTokens]);

  const effectiveBarQuery =
    value.startsWith("/") || suppressKeywordSearch || inCommandMode
      ? ""
      : typingQuery;
  const { data: barData, resolvedQuery: barResolvedQuery } = useBarSearch(
    effectiveBarQuery,
    undefined, // hubId — cross-hub by default
    activeTopicId || undefined,
  );
  // The bar search hook is debounced; `barResolvedQuery` is the query
  // whose rows are in `barData`. While the user is still typing, it
  // lags behind the live query and the dataset belongs to a previous
  // keystroke. Treat as stale in that window so we do not flash
  // mismatched memory / topic / hub rows.
  const barIsCurrent = Boolean(
    barData && barResolvedQuery === effectiveBarQuery,
  );
  const ftsResults = barIsCurrent ? barData?.memories : undefined;
  const serverTopicMatches = useMemo(
    () => (barIsCurrent ? (barData?.topics ?? []) : []),
    [barIsCurrent, barData?.topics],
  );
  const serverHubMatches = useMemo(
    () => (barIsCurrent ? (barData?.hubs ?? []) : []),
    [barIsCurrent, barData?.hubs],
  );

  // Unified row shape so FTS hits that are not in the loaded `allMemories`
  // cache still render. Previously we required a cache lookup for every FTS
  // result, which silently dropped rows whose memories were older than the
  // first page of recents — users typed an exact title and saw nothing.
  //
  // `topicName` / `hubName` are direct-from-server fields on the FTS
  // payload, so cross-hub results render the right attribution even
  // when `allMemories` and `topicLookup` / `hubNameMap` don't hold the
  // row — previously those cross-hub chips silently disappeared.
  const preEnterResults = useMemo<
    Array<{
      id: string;
      title: string;
      snippet: string;
      topicId: string | null;
      hubId: string | null;
      topicName: string | null;
      hubName: string | null;
    }>
  >(() => {
    const rows: Array<{
      id: string;
      title: string;
      snippet: string;
      topicId: string | null;
      hubId: string | null;
      topicName: string | null;
      hubName: string | null;
    }> = [];
    const seen = new Set<string>();
    for (const memory of keywordResults) {
      seen.add(memory.id);
      rows.push({
        id: memory.id,
        title: memory.title,
        snippet: snippet(memory.summary || memory.content || ""),
        topicId: memory.topic_id ?? null,
        hubId: memory.hub_id ?? null,
        // Memory rows coming from the client-side keyword scan don't
        // carry an enriched topic_name — render resolves it via
        // topicLookup (scoped to the active hub, which is where
        // allMemories came from anyway).
        topicName: null,
        hubName: memory.hub_name ?? null,
      });
    }
    if (ftsResults) {
      for (const result of ftsResults) {
        if (seen.has(result.memory_id)) continue;
        seen.add(result.memory_id);
        const cached = allMemories.find(
          (memory) => memory.id === result.memory_id,
        );
        if (cached) {
          rows.push({
            id: cached.id,
            title: cached.title,
            // Prefer the server-anchored snippet (aligned on the match)
            // over a head-of-content slice from the cache.
            snippet:
              result.snippet || snippet(cached.summary || cached.content || ""),
            topicId: cached.topic_id ?? null,
            hubId: cached.hub_id ?? null,
            // Prefer server-enriched names (authoritative for cross-hub
            // rows); topic has no cached fallback on Memory itself, so
            // rendering resolves via topicLookup when the server didn't
            // enrich. hub_name IS on Memory and is a valid fallback.
            topicName: result.topic_name ?? null,
            hubName: result.hub_name ?? cached.hub_name ?? null,
          });
        } else {
          // Server enriches `title` from the memory row but can fall back
          // to the chunk heading chain if enrichment errors. Guard against
          // a blank title so the row is never rendered as an empty button.
          const safeTitle =
            result.title.trim() ||
            result.snippet.split("\n")[0].trim().slice(0, 80);
          if (!safeTitle) continue;
          rows.push({
            id: result.memory_id,
            title: safeTitle,
            snippet: result.snippet,
            topicId: result.topic_id ?? null,
            hubId: result.hub_id ?? null,
            topicName: result.topic_name ?? null,
            hubName: result.hub_name ?? null,
          });
        }
      }
    }
    return rows.slice(0, MAX_KEYWORD);
  }, [allMemories, ftsResults, keywordResults]);

  // B1: When the bar is focused with an empty query, surface the 5 most
  // recent memories as a "Recent" layer so the bar has jump-back utility
  // out of the gate. Matches Notion Quick Find / Linear ⌘K behavior.
  const emptyQueryRecents = useMemo<
    Array<{
      id: string;
      title: string;
      snippet: string;
      topicId: string | null;
      hubId: string | null;
      topicName: string | null;
      hubName: string | null;
    }>
  >(() => {
    if (
      typingQuery !== "" ||
      suppressKeywordSearch ||
      inCommandMode ||
      value.startsWith("/")
    )
      return [];
    const scoped = activeTopicId
      ? allMemories.filter((m) => m.topic_id === activeTopicId)
      : allMemories;
    return scoped.slice(0, 5).map((memory) => ({
      id: memory.id,
      title: memory.title,
      snippet: snippet(memory.summary || memory.content || ""),
      topicId: memory.topic_id ?? null,
      hubId: memory.hub_id ?? null,
      // Recent memories feed from the active hub's list cache, so
      // topicLookup resolves the label — no need to duplicate it on
      // the row. hub_name IS on Memory and serves as a stable
      // fallback if the viewer's hub cache is stale.
      topicName: null,
      hubName: memory.hub_name ?? null,
    }));
  }, [
    activeTopicId,
    allMemories,
    inCommandMode,
    suppressKeywordSearch,
    typingQuery,
    value,
  ]);

  // B2: Cross-type suggestions. Topic + hub rows come from the unified
  // bar-search endpoint (via useBarSearch), which runs the same
  // debounce + staleness guard as the memory FTS call. We still route
  // locally — the server only returns IDs and names, not navigation.
  const jumpToRows = useMemo<
    Array<{
      id: string;
      icon: LucideIcon;
      name: string;
      desc: string;
      run: () => void;
    }>
  >(() => {
    if (keywordTokens.length === 0 || inCommandMode || value.startsWith("/"))
      return [];
    const rows: Array<{
      id: string;
      icon: LucideIcon;
      name: string;
      desc: string;
      run: () => void;
    }> = [];
    for (const topic of serverTopicMatches) {
      if (rows.length >= 3) break;
      rows.push({
        id: `jump-topic-${topic.id}`,
        icon: Hash,
        name: topic.name,
        desc: t.bar.mention.topicDesc,
        run: () => {
          navigateToTopic(topic.id);
        },
      });
    }
    for (const hub of serverHubMatches) {
      if (rows.length >= 3) break;
      // Resolve the display name through our hub cache so personal
      // hubs still render as "Personal" / "Derek's memax" per user
      // locale — the server returns the raw hub name.
      const entry = hubs.find((h) => h.hub.id === hub.id);
      const display = entry
        ? getHubDisplayName(entry.hub, t, {
            name: user?.name,
            displayName: user?.display_name,
          })
        : hub.name;
      rows.push({
        id: `jump-hub-${hub.id}`,
        icon: Folder,
        name: display,
        desc: t.bar.command.hub.desc,
        run: () => {
          if (hub.id !== activeHubId) switchHub(hub.id);
          // Land on the target hub's URL when on a v2 route so the
          // path matches the new active hub. Falls through to /memories
          // when we don't have the full hub record (server-only match).
          const onV2Route = currentHubSlug !== null;
          const destination =
            onV2Route && user && entry
              ? buildMemoriesPath(hubRouteSlug(entry.hub, user.id))
              : "/memories";
          requestSurfaceTransitionForNavigation(pathname, destination);
          router.push(destination);
        },
      });
    }
    return rows;
  }, [
    activeHubId,
    currentHubSlug,
    hubs,
    inCommandMode,
    keywordTokens,
    navigateToTopic,
    pathname,
    router,
    serverHubMatches,
    serverTopicMatches,
    switchHub,
    t,
    user,
    value,
  ]);

  // Whichever row-set is currently visible below the jump-to chips:
  // quick matches while typing, recents when input is empty.
  const visibleRows = typingQuery ? preEnterResults : emptyQueryRecents;

  const semanticResults = useMemo(
    () => recallResult?.memories ?? [],
    [recallResult?.memories],
  );
  const visibleSemanticResults = sourcesExpanded
    ? semanticResults
    : semanticResults.slice(0, VISIBLE_SOURCES);
  const hiddenCount = Math.max(
    0,
    semanticResults.length - visibleSemanticResults.length,
  );

  const hubLabel =
    hubs.length > 1 && isTeamHub && activeHub ? activeHub.hub.name : null;

  const commandRows = useMemo(() => {
    if (!inCommandMode) return [];
    if (scope.kind === "command") {
      const query = value.trim().toLowerCase();
      if (scopedCommand === "hub") {
        return hubs
          .filter((entry) => {
            const haystack = entry.hub.name.toLowerCase();
            return !query || haystack.includes(query);
          })
          .slice(0, MAX_KEYWORD)
          .map((entry) => ({
            key: `hub-${entry.hub.id}`,
            icon: Folder,
            name: getHubDisplayName(entry.hub, t, {
              name: user?.name,
              displayName: user?.display_name,
            }),
            desc: t.bar.command.hub.desc,
            run: () => {
              if (entry.hub.id === activeHubId) return;
              switchHub(entry.hub.id);
              const onV2Route = currentHubSlug !== null;
              const destination =
                onV2Route && user
                  ? buildMemoriesPath(hubRouteSlug(entry.hub, user.id))
                  : "/memories";
              requestSurfaceTransitionForNavigation(pathname, destination);
              router.push(destination);
            },
          }));
      }
    }

    // `/topic` was replaced by the `@` mention-mode picker on 2026-04-21.
    // Only `/hub` remains in slash-command discovery.
    const commandRows = [
      {
        key: "hub",
        icon: Folder,
        name: t.bar.command.hub.label,
        desc: t.bar.command.hub.desc,
        run: () => {
          commitCommandScope("hub");
          inputRef.current?.focus();
        },
      },
    ];

    return commandRows.filter(
      (row) => !rawCommandPrefix || row.key.startsWith(rawCommandPrefix),
    );
  }, [
    activeHubId,
    currentHubSlug,
    hubs,
    inCommandMode,
    pathname,
    rawCommandPrefix,
    router,
    scope.kind,
    scopedCommand,
    commitCommandScope,
    switchHub,
    t,
    inputRef,
    user,
    value,
  ]);

  // Mention picker rows — `@foo` live-filters topics and jumps on select.
  const mentionRows = useMemo(() => {
    if (!inMentionMode) return [];
    const query = (mention?.query ?? "").trim().toLowerCase();
    return flatTopics
      .filter((topic) => {
        if (!query) return true;
        const haystack =
          `${topic.name} ${topic.description ?? ""}`.toLowerCase();
        return haystack.includes(query);
      })
      .slice(0, MAX_KEYWORD)
      .map((topic) => ({
        key: `mention-topic-${topic.id}`,
        icon: Hash,
        name: topic.name,
        desc: topic.description || t.bar.mention.topicDesc,
        run: () => {
          setValue("");
          navigateToTopic(topic.id);
        },
      }));
  }, [flatTopics, inMentionMode, mention?.query, navigateToTopic, setValue, t]);

  useEffect(() => {
    const resultItems = inCommandMode
      ? commandRows.map((row) => ({ id: row.key, run: row.run }))
      : inMentionMode
        ? mentionRows.map((row) => ({ id: row.key, run: row.run }))
        : phase === "input"
          ? [
              ...jumpToRows.map((row) => ({ id: row.id, run: row.run })),
              ...visibleRows.map((row) => ({
                id: row.id,
                run: () => navigateToMemory(row.id),
              })),
            ]
          : visibleSemanticResults.map((memory) => ({
              id: memory.id,
              run: () => navigateToMemory(memory.id),
            }));
    // Build nav list matching visual top-to-bottom order:
    //   1. Synthesis placeholder (when AI enabled, input phase)
    //   2. Remember row (when visible + actionable) — visually above results
    //   3. Jump-to rows (topics/hubs that match the typed query)
    //   4. Result rows (quick matches / recents / semantic / command)
    const synthNavEntry =
      showRecallSurfaces && !inCommandMode && phase === "input" && canUseAI
        ? [{ id: "__synthesis", run: handleSubmit }]
        : [];
    const rememberNavEntry =
      surface.showRememberRow && interaction.canRemember
        ? [{ id: "__remember", run: handleRememberSubmit }]
        : [];
    const items = [...synthNavEntry, ...rememberNavEntry, ...resultItems];
    setNavigationItems(items);
  }, [
    canUseAI,
    commandRows,
    handleRememberSubmit,
    handleSubmit,
    inCommandMode,
    inMentionMode,
    mentionRows,
    interaction.canRemember,
    jumpToRows,
    navigateToMemory,
    phase,
    router,
    setNavigationItems,
    showRecallSurfaces,
    surface.showRememberRow,
    visibleRows,
    visibleSemanticResults,
  ]);

  // Whether the synthesis placeholder and remember row occupy nav slots
  // before the result rows. Used to offset highlight indices so they
  // don't collide with the pre-result entries.
  const hasSynthNavEntry =
    showRecallSurfaces && !inCommandMode && phase === "input" && canUseAI;
  const hasRememberNavEntry =
    surface.showRememberRow && interaction.canRemember;
  const resultNavOffset =
    (hasSynthNavEntry ? 1 : 0) + (hasRememberNavEntry ? 1 : 0);

  // Index of the __remember nav entry (right after synthesis, before results)
  const rememberNavIndex = useMemo(() => {
    if (!surface.showRememberRow || !interaction.canRemember) return -1;
    return hasSynthNavEntry ? 1 : 0; // right after synthesis or first
  }, [hasSynthNavEntry, interaction.canRemember, surface.showRememberRow]);

  const copyAnswerText = useCallback(() => {
    const clean = (askAnswer ?? "").replace(/\[\d+(?:-\d+)?\]/g, "");
    copyAnswer(clean.trim());
  }, [askAnswer, copyAnswer]);

  const copyContextBlock = useCallback(() => {
    if (semanticResults.length === 0) return;
    const entries = semanticResults.map((memory) => ({
      title: memory.title,
      content: memory.chunk_content || memory.summary || "",
      kind: memory.kind,
      stability: memory.stability,
      source: memory.source,
    }));
    copyContext(formatContextBlock(entries, `recall: ${recallQuery}`));
  }, [copyContext, recallQuery, semanticResults]);

  // Scroll the highlighted row into view when arrow nav moves.
  // Must be before the early return so hook order is stable.
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (activeNavigationIndex === null) return;
    const container = scrollContainerRef.current;
    if (!container) return;
    const el = container.querySelector<HTMLElement>(
      "[data-nav-highlighted='true']",
    );
    el?.scrollIntoView({ block: "nearest" });
  }, [activeNavigationIndex]);

  if (!surface.showExpandSurface) return null;

  const synthesisState =
    showRecallSurfaces &&
    canUseAI &&
    ((phase === "loading" && !recallIsError) ||
      (phase === "recall-result" && aiLoading && !askAnswer && !recallIsError))
      ? "thinking"
      : phase === "recall-result" && recallIsError
        ? "error"
        : phase === "recall-result" && canUseAI && askAnswer && aiStreaming
          ? "streaming"
          : phase === "recall-result" && canUseAI && askAnswer
            ? "complete"
            : phase === "recall-result" &&
                !canUseAI &&
                semanticResults.length > 0
              ? "free"
              : "placeholder";

  const hintRows =
    inCommandMode || inMentionMode
      ? [
          { key: "↵", label: t.bar.hint.run },
          { key: "↑↓", label: t.bar.hint.navigate },
        ]
      : [
          { key: "↵", label: t.bar.hint.recall },
          { key: "⌘↵", label: t.bar.hint.remember },
          { key: "↑↓", label: t.bar.hint.navigate },
        ];

  return (
    <div className="flex max-h-[60vh] min-h-0 flex-col">
      {/* Single scroll lane for the full expand surface — synthesis,
          RememberRow, then sources (north-star zones ② → ③ → ④). Wheel /
          touch scroll stays inside the expanded bar and the positional
          cascade matches docs/design/bar-northstar.md. */}
      <div
        ref={scrollContainerRef}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain"
      >
        {showRecallSurfaces &&
        (value.trim() || recallQuery) &&
        !value.startsWith("/") ? (
          <>
            {/* Section label: "memax says" / "memax says · {topic}" */}
            {(synthesisState === "streaming" ||
              synthesisState === "complete") && (
              <div className="px-4 pt-3 pb-1.5 text-[11px] font-medium uppercase tracking-wider text-fg-3">
                {activeTopicName
                  ? interpolate(t.bar.section.memaxSaysTopic, {
                      topic: activeTopicName,
                    })
                  : t.bar.section.memaxSays}
              </div>
            )}
            {/* Wrap synthesis in a clickable target when it has a nav entry */}
            {hasSynthNavEntry && synthesisState === "placeholder" ? (
              <button
                type="button"
                onClick={handleSubmit}
                data-nav-highlighted={activeNavigationIndex === 0 || undefined}
                className={`w-full cursor-pointer text-left transition-colors ${
                  activeNavigationIndex === 0
                    ? "bg-surface-1"
                    : "hover:bg-surface-1"
                }`}
              >
                <SynthesisSlot state="placeholder" />
              </button>
            ) : (
              <SynthesisSlot
                state={synthesisState}
                text={askAnswer}
                sources={semanticResults.map((memory) => ({ id: memory.id }))}
                query={recallQuery || value.trim()}
                onRetry={() => void retryRecall()}
                onSourceClick={(id) => navigateToMemory(id)}
                onCopy={copyAnswerText}
                onCopyForAI={copyContextBlock}
              />
            )}
          </>
        ) : null}

        {/* Zone ③ — Remember action, positioned between synthesis and
            sources per the bar north-star. */}
        {surface.showRememberRow ? (
          <RememberRow
            state={rememberState}
            title={rememberedTitle}
            fileCount={stagedFiles.length || undefined}
            hub={hubLabel}
            showShortcut={!isMobile}
            highlighted={
              activeNavigationIndex !== null &&
              interaction.canRemember &&
              activeNavigationIndex === rememberNavIndex
            }
            onUndo={undoRemember}
            onRetry={retryRemember}
            onClick={interaction.canRemember ? handleRememberSubmit : undefined}
          />
        ) : null}

        {/* Section label: "Related" / "Related in {topic}" —
            activeTopicName is gated on !routeTopicDismissed. */}
        {!inCommandMode &&
          !inMentionMode &&
          semanticResults.length > 0 &&
          synthesisState !== "streaming" &&
          synthesisState !== "complete" && (
            <div className="px-4 pt-3 pb-1.5 text-[11px] font-medium uppercase tracking-wider text-fg-3">
              {activeTopicName
                ? interpolate(t.bar.section.relatedInTopic, {
                    topic: activeTopicName,
                  })
                : t.bar.section.related}
            </div>
          )}

        {inCommandMode ? (
          <>
            {commandRows.map((row, index) => (
              <CommandRow
                key={row.key}
                icon={row.icon}
                name={row.name}
                desc={row.desc}
                highlighted={activeNavigationIndex === resultNavOffset + index}
                onClick={row.run}
              />
            ))}
          </>
        ) : inMentionMode ? (
          <>
            <div className="px-4 pt-3 pb-1.5 text-[11px] font-medium uppercase tracking-wider text-fg-3">
              {t.bar.section.filterTopic}
            </div>
            {mentionRows.length > 0 ? (
              mentionRows.map((row, index) => (
                <CommandRow
                  key={row.key}
                  icon={row.icon}
                  name={row.name}
                  desc={row.desc}
                  highlighted={
                    activeNavigationIndex === resultNavOffset + index
                  }
                  onClick={row.run}
                />
              ))
            ) : (
              <div className="px-5 py-4 sm:px-6">
                <p className="text-[15px] text-fg-3">
                  {t.bar.mention.noMatches}
                </p>
                <p className="mt-1 text-[13px] text-fg-3">
                  {t.bar.mention.hint}
                </p>
              </div>
            )}
          </>
        ) : surface.expandSurfaceKind === "recall-input" &&
          !suppressKeywordSearch ? (
          <>
            {jumpToRows.length > 0 && (
              <div className="px-4 pt-3 pb-1.5 text-[11px] font-medium uppercase tracking-wider text-fg-3">
                {activeTopicName
                  ? interpolate(t.bar.section.jumpToInTopic, {
                      topic: activeTopicName,
                    })
                  : t.bar.section.jumpTo}
              </div>
            )}
            {jumpToRows.map((row, index) => (
              <CommandRow
                key={row.id}
                icon={row.icon}
                name={row.name}
                desc={row.desc}
                highlighted={activeNavigationIndex === resultNavOffset + index}
                onClick={row.run}
              />
            ))}
            {visibleRows.length > 0 && (
              <div className="px-4 pt-3 pb-1.5 text-[11px] font-medium uppercase tracking-wider text-fg-3">
                {typingQuery
                  ? activeTopicName
                    ? interpolate(t.bar.section.matchesInTopic, {
                        topic: activeTopicName,
                      })
                    : t.bar.section.matches
                  : activeTopicName
                    ? interpolate(t.bar.section.recentInTopic, {
                        topic: activeTopicName,
                      })
                    : t.bar.section.recent}
              </div>
            )}
            {visibleRows.map((row, index) => (
              <SourceRow
                key={row.id}
                title={row.title}
                snippet={row.snippet}
                query={typingQuery}
                // Prefer the row's server-enriched topicName (authoritative
                // for cross-hub results that topicLookup, which is
                // scoped to the active hub, will not hold). Fall back
                // to topicLookup so in-hub rows keep the same source of
                // truth they had before.
                topic={
                  row.topicName ??
                  (row.topicId ? topicLookup.get(row.topicId) : undefined)
                }
                // Same precedence rule for hubs — the row's hubName is
                // set server-side and works for any accessible hub,
                // whereas hubNameMap only knows about hubs the viewer
                // belongs to in the current session cache.
                hub={
                  row.hubId && row.hubId !== activeHubId
                    ? (row.hubName ?? hubNameMap.get(row.hubId))
                    : undefined
                }
                highlighted={
                  activeNavigationIndex ===
                  resultNavOffset + jumpToRows.length + index
                }
                onClick={() => navigateToMemory(row.id)}
              />
            ))}
            {!jumpToRows.length && !visibleRows.length && value.trim() ? (
              <div className="px-5 py-4 sm:px-6">
                <p className="text-[15px] text-fg-3">{t.bar.noResults}</p>
                <p className="mt-1 text-[13px] text-fg-3">
                  {t.bar.noResultsHint}
                </p>
              </div>
            ) : null}
          </>
        ) : surface.expandSurfaceKind === "recall-result" && recallIsError ? (
          <div className="px-5 py-4 sm:px-6">
            <p className="text-[15px] text-fg-3">{t.bar.recallError}</p>
            <p className="mt-1 text-[13px] text-fg-3">
              {t.bar.recallErrorHint}
            </p>
          </div>
        ) : (
          <>
            {visibleSemanticResults.map((memory, index) => (
              <SourceRow
                key={memory.id}
                index={index + 1}
                title={memory.title}
                snippet={snippet(memory.chunk_content || memory.summary || "")}
                relevance={memory.relevance_score}
                topic={
                  memory.topic_id
                    ? topicLookup.get(memory.topic_id) || memory.topic_name
                    : memory.topic_name
                }
                hub={
                  memory.hub_id && memory.hub_id !== activeHubId
                    ? hubNameMap.get(memory.hub_id)
                    : undefined
                }
                highlighted={activeNavigationIndex === resultNavOffset + index}
                onClick={() => navigateToMemory(memory.id)}
              />
            ))}
            {hiddenCount > 0 ? (
              <LoadMoreRow
                count={hiddenCount}
                onClick={() => setSourcesExpanded(true)}
              />
            ) : null}
            {surface.expandSurfaceKind === "recall-result" &&
            !visibleSemanticResults.length &&
            !recallFetching ? (
              <div className="px-5 py-4 sm:px-6">
                <p className="text-[15px] text-fg-3">{t.bar.noResults}</p>
                <p className="mt-1 text-[13px] text-fg-3">
                  {t.bar.noResultsHint}
                </p>
              </div>
            ) : null}
          </>
        )}
      </div>

      {surface.showHintBar ? (
        <HintBar
          hints={hintRows}
          trailing={{ key: "esc", label: t.bar.hint.close }}
        />
      ) : null}
      {answerCopied || contextCopied ? <div className="hidden" /> : null}
    </div>
  );
}
