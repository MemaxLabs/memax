"use client";

import {
  createContext,
  useContext,
  useReducer,
  useRef,
  useCallback,
  useEffect,
  useMemo,
  type RefObject,
} from "react";
import { usePathname, useRouter } from "next/navigation";
import {
  useCreateMemory,
  useDeleteMemory,
  type Memory,
} from "@/hooks/use-memories";
import { useRecall } from "@/hooks/use-recall";
import { useUsage, usageQueryKey, hasLimits } from "@/hooks/use-usage";
import { queryClient } from "@/lib/query-client";
import { apiPost } from "@/lib/api";
import { getMemaxClient } from "@/lib/memax-client";
import { processDroppedFiles } from "@/lib/process-dropped-files";
import { requestSurfaceTransitionForNavigation } from "@/lib/recent-navigation";
import { useCompose } from "@/contexts/compose-context";
import {
  isBarMemorySurfaceRoute,
  isBrainViewRoute,
  isChatSurfaceRoute,
  isTopicRoute,
  getTopicIdForPath,
  getHubSlugForPath,
  buildMemoriesPath,
} from "@/lib/route-helpers";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import {
  decodeBinaryBase64,
  detectUploadMimeType,
  encodeTextBytes,
  shouldUsePresignedUpload,
  uploadMemoryObject,
} from "@/lib/memory-upload";
import {
  registerMutationToastHandler,
  unregisterMutationToastHandler,
} from "@/lib/mutation-toast";
import { useAuth } from "@/lib/auth";
import { useDevMode } from "@/hooks/use-dev-mode";
import { useMemaxDebugger } from "@/hooks/use-memax-debugger";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useDreamRunStatus } from "@/hooks/use-dream-run-status";
import {
  useNotifications,
  useNotificationMarkSeen,
} from "@/hooks/use-notifications";
import { useLocale, useInterpolate } from "@/i18n";
import type {
  DreamRunCompletedPayload,
  Notification as NotificationRow,
} from "memax-sdk";
import type { RecalledMemory } from "memax-sdk";
import { useInboxSurface } from "@/contexts/inbox-surface-context";
import type { BarScope } from "@/components/bar/scope-indicator";
import type { RememberState } from "@/components/bar/remember-row";
import type { BarNotification, StagedFile } from "@/lib/bar-types";
import {
  barDomainReducer,
  createInitialBarDomainState,
} from "@/lib/bar-domain";
import {
  deriveBarInteractionState,
  getRawCommandInput,
  getShouldRememberSubmit,
  parseCommittedCommand,
  pickDefaultNavIndex,
  type BarCommand,
  type BarPhase,
  type MobileComposeState,
} from "@/lib/bar-interaction";
import {
  deriveBarSurfaceState,
  type DerivedBarSurfaceState,
} from "@/lib/bar-surface";
import {
  barMachineReducer,
  computeNextNavigationIndex,
  createInitialBarMachineState,
  getPeelBackStep,
  type PeelBackStep,
} from "@/lib/bar-machine";
import {
  consumeBarRestoreSnapshot,
  peekBarRestoreSnapshot,
  saveBarRestoreSnapshot,
} from "@/lib/bar-restore-snapshot";

/** Detect if text is a single URL (pasted link) */
function isSingleUrl(text: string): boolean {
  const t = text.trim();
  if (t.includes("\n") || t.includes(" ")) return false;
  try {
    const url = new URL(t);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

function getCommandLabel(
  _command: BarCommand,
  t: ReturnType<typeof useLocale>["t"],
) {
  // Only `/hub` remains — `/topic` was replaced by the `@` mention-mode
  // topic picker on 2026-04-21.
  return t.bar.command.hub.label;
}

export type View = "brain" | "memory" | "none";
export type Phase = BarPhase;

const DREAM_ACTION_DISMISS_TYPES = new Set([
  "dream_complete",
  "dream_clean",
  "dream_partial",
]);

/** Eager-upload file staging (ChatGPT pattern): upload starts on drop/paste, not on send. */

interface BarNavigationItem {
  id: string;
  run: () => void;
}

interface BarInteractionState {
  hasText: boolean;
  hasFiles: boolean;
  filesUploading: boolean;
  isCommandScope: boolean;
  isSlashDiscovery: boolean;
  isCommandMode: boolean;
  isMentionMode: boolean;
  isRememberMode: boolean;
  isRecallMode: boolean;
  showRecallSurfaces: boolean;
  hasLiftSignal: boolean;
  hasExpandContent: boolean;
  canRemember: boolean;
  sendState: "loading" | "ready" | "disabled";
  mobileComposeState: MobileComposeState;
}

type BarSurfaceState = DerivedBarSurfaceState;

interface BarState {
  // View (derived from route)
  view: View;
  isMobile: boolean;

  // Input
  value: string;
  setValue: (v: string) => void;
  barMode: "push" | "recall";
  /** True briefly during push (send button shows spinner) */
  pushing: boolean;
  sendKind: "recall" | "remember";
  scope: BarScope;
  setScope: (scope: BarScope) => void;
  clearScope: () => void;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  barFocused: boolean;
  setBarFocused: (v: boolean) => void;

  // Phase
  phase: Phase;
  setPhase: (p: Phase) => void;

  // Recall
  recallQuery: string;
  recallResult:
    | {
        memories: RecalledMemory[];
      }
    | undefined;
  recallFetching: boolean;
  recallIsError: boolean;
  recallErrorMessage?: string;
  retryRecall: () => void;

  // AI
  askAnswer: string;
  aiLoading: boolean;
  aiStreaming: boolean;
  canUseAI: boolean;
  triggerAI: (explicitQuery?: string) => void;
  setAskAnswer: (v: string) => void;
  rememberState: RememberState;
  rememberedTitle: string;
  retryRemember: () => void;
  undoRemember: () => void;

  // Notifications — three independent channels
  /** Transient notifications (success/error/info) — auto-dismiss */
  barNotification: BarNotification | null;
  setBarNotification: (n: BarNotification | null) => void;
  /** Ambient dream notification — persistent until dismissed */
  dreamNotification: BarNotification | null;
  setDreamNotification: (n: BarNotification | null) => void;
  /** Ambient status notification — processing, syncing (lowest priority) */
  statusNotification: BarNotification | null;
  setStatusNotification: (n: BarNotification | null) => void;
  /** Resolved: transient > dream > status */
  activeNotification: BarNotification | null;

  // File staging (eager upload — ChatGPT pattern)
  stagedFiles: StagedFile[];
  addStagedFiles: (
    files: {
      name: string;
      content: string;
      binary: boolean;
      contentType: string;
    }[],
  ) => void;
  removeStagedFile: (id: string) => void;
  clearStagedFiles: () => void;
  /** True when all staged files have finished uploading (or have errors) */
  allFilesReady: boolean;

  // URL auto-detect (paste detection)
  /** Detected URL from pasted content — shown as chip with capture option */
  detectedUrl: string | null;
  clearDetectedUrl: () => void;

  // Memory selection (for bar detail + MemoryModal)
  selectedMemory: Memory | null;
  setSelectedMemory: (n: Memory | null) => void;
  memorySearch: string;
  setMemorySearch: (v: string) => void;

  // Drag
  isDragging: boolean;
  setIsDragging: (v: boolean) => void;

  // Mutations (used by bar sub-components)
  createMemory: ReturnType<typeof useCreateMemory>;

  // Overlay (Cmd+K on non-bar pages)
  barOverlayOpen: boolean;
  openBar: () => void;
  hideBar: () => void;
  closeBar: () => void;
  mobileComposeActive: boolean;
  openMobileComposeActive: () => void;
  closeMobileComposeActive: () => void;
  setMobileComposeActive: (open: boolean) => void;
  mobileComposeOpen: boolean;
  openMobileCompose: () => void;
  closeMobileCompose: () => void;
  setMobileComposeOpen: (open: boolean) => void;
  mobileComposeState: MobileComposeState;
  peelBack: () => void;
  nextPeelBackStep: PeelBackStep;
  interaction: BarInteractionState;
  surface: BarSurfaceState;
  /** True if the bar holds any state worth preserving (text, files, scope,
   *  recall, AI stream, remember in-flight, etc.) — used by chrome that
   *  must only dismiss during a fully idle bar (scroll-hide, Cmd+K). */
  barHasState: () => boolean;
  /** Whether the expand surface is suppressed while preserving state (e.g.
   *  after ⌘K on a bar with content, or after tap-outside on a bar with
   *  a draft). Focus on the bar clears this. */
  barPanelSuppressed: boolean;
  setBarPanelSuppressed: (v: boolean) => void;

  // AI state setter (for dismiss)
  setAiLoading: (v: boolean) => void;
  /** Abort any active AI stream, clear the answer, and bump the request
   *  id so late SSE deltas are treated as stale. Use from outside-click /
   *  Esc paths instead of manually calling setAskAnswer + setAiLoading —
   *  otherwise the stream keeps appending chunks and re-hydrates the
   *  answer after dismiss (codex-review 2026-04-21). */
  cancelAI: () => void;
  activeNavigationIndex: number | null;
  setNavigationItems: (items: BarNavigationItem[]) => void;
  clearNavigationSelection: () => void;

  // Expand morph (ChatGPT pattern: single-line = inline, multiline = stacked)
  isBarExpanded: boolean;
  setIsBarExpanded: (v: boolean) => void;

  // Handlers
  handleChange: (text: string) => void;
  handleSubmit: () => void;
  handleRememberSubmit: () => void;
  commitCommandScope: (command: "hub", query?: string) => void;
  handleKeyDown: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void;
  /** IME composition tracking — gates mention-mode + chip-Backspace so CJK /
   *  emoji keyboards don't flip UX on transient buffers. */
  setComposing: (value: boolean) => void;
  /** Dismiss the route-derived topic chip without leaving the topic route.
   *  Recall falls back to global. Reset automatically on pathname change
   *  (unless a restore snapshot preserves it). */
  dismissRouteTopic: () => void;
  /** True when the user has dismissed the route-topic chip on the current
   *  topic route. Gates the topicId passed to useRecall + topic-scoped
   *  labels + keyword filter. */
  routeTopicDismissed: boolean;
  /** Capture a restore snapshot + collapse expand chrome AND clear the
   *  recall surface state. Call BEFORE `router.push` from any result-tap
   *  so the bar auto-rests on the next route AND restores to its exact
   *  prior state when the user returns (back button or sidebar nav). */
  peelForNavigation: () => void;
  reset: () => void;
  /** Send all staged files that completed upload — creates memories */
  uploadFiles: () => void;
  goMemory: () => void;
  goBack: () => void;
}

const BarContext = createContext<BarState>(null!);

export function useBar() {
  const ctx = useContext(BarContext);
  if (!ctx) throw new Error("useBar must be used within BarProvider");
  return ctx;
}

export function BarProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const isMobile = useIsMobile();
  const { activeHubId, hubs, user, switchHub } = useAuth();
  const activeHubEntry = hubs.find((h) => h.hub.id === activeHubId) ?? null;
  const { flags } = useDevMode();
  const { recordEvent } = useMemaxDebugger();
  const { locale, t } = useLocale();
  const interpolate = useInterpolate();
  const dreamRunStatus = useDreamRunStatus();
  // Compose context for the "graduate from bar to compose modal"
  // flow (Shift+Enter from the bar input → open compose modal with
  // the bar's draft pre-loaded as the modal's content). Requires
  // BarProvider to live below ComposeProvider in app-shell-client.
  const compose = useCompose();
  // Unseen dream_run_completed receipts — hydrated on mount and
  // kept fresh via the SSE-driven notification.created invalidation
  // in memax-event-bridge.tsx. Phase 4 Chunk 2 of the inbox
  // notification framework: the bar no longer reads pending review
  // counts, and the completed-run banner is notification-backed +
  // replay-safe (see the seen-on-mount effect below).
  const { data: unseenDreamReceipts } = useNotifications({
    kind: ["dream_run_completed"],
    unseenOnly: true,
  });
  const notificationMarkSeen = useNotificationMarkSeen();
  const { openInbox } = useInboxSurface();

  // canUseAI gates the bar's inline answer synthesis. True when the
  // caller's plan grants ask quota AND they haven't exhausted it:
  //   - ask_limit  > 0 and ask_count <  ask_limit → quota remaining
  //   - ask_limit == -1                           → unlimited (paid)
  //   - ask_limit == 0                            → plan excludes ask
  // The free plan ships with ask_limit=10, so free users now get AI
  // answers until the tenth ask lands; previously this flag was
  // hard-coded to flags.mockProUser, which locked every non-dev
  // session out of the quota the server already allocates.
  //
  // Loading + error states fall through to false (gated) on purpose:
  // better to render the inert "upgrade" stub briefly than to let a
  // click race ahead of limit data and eat a 402 quota_exceeded
  // without a surfaced reason. The mockProUser dev toggle stays as
  // a local override.
  const { data: usage } = useUsage();
  const canUseAI =
    flags.mockProUser ||
    (hasLimits(usage) &&
      (usage.limits.ask_limit < 0 ||
        (usage.limits.ask_limit > 0 &&
          usage.ask_count < usage.limits.ask_limit)));

  // Derive view from route. "memory" = main app content surface with a
  // bottom-docked bar (focus-to-engage, drag-to-stage). /pulse rides in
  // this bucket so the bar mounts + docks consistently across browsing
  // surfaces — without it /pulse was view="none", producing a
  // ghost-docked opacity-0 bar (codex-review 2026-04-21).
  // `isBarMemorySurfaceRoute` covers both shell-v1 (/memories[/...] +
  // /pulse) and shell-v2 (/h/<slug>/memories[/...]) so the bar mounts
  // identically across the cookie flag.
  const view: View = isBrainViewRoute(pathname)
    ? "brain"
    : isBarMemorySurfaceRoute(pathname)
      ? "memory"
      : "none";

  // Derive active topic from URL (route wins over transient scope).
  // Uses the same shell-v1/v2-aware helper as the topic tree.
  const routeTopicCandidate = getTopicIdForPath(pathname);
  const routeTopicId =
    routeTopicCandidate &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      routeTopicCandidate,
    )
      ? routeTopicCandidate
      : null;

  // Mutations (bar submit flow)
  const createMemory = useCreateMemory();
  const deleteMemory = useDeleteMemory({ skipGlobalToast: true });

  // M1 (codex-review 2026-04-21): atomic snapshot restore. The pathname
  // effect below would run AFTER the first render against the destination
  // route, so `useRecall` would briefly subscribe to the unscoped/global
  // key even when returning to a topic page where the chip had been
  // dismissed. Seeding the initial reducer state from any matching
  // snapshot closes that race — the first render already has the right
  // `routeTopicDismissed` / `isBarExpanded` / `mobileComposeState`.
  //
  // Important: this init uses PEEK (non-destructive). If we consumed
  // here, React Strict Mode's double-invocation of lazy init would eat
  // the snapshot on the first pass and the committed render would land
  // in default state. A one-shot mount effect below DOES consume, so
  // the snapshot doesn't linger in sessionStorage and rehydrate on a
  // later remount of the same route.
  //
  // Snapshots are bfcache/remount fallback ONLY, not a second source of
  // truth (codex-review 2026-04-21). Live state persists across app-
  // route transitions via BarProvider in layout.tsx; only template.tsx
  // remounts per-route. The pathname effect no longer restores — it
  // only resets route-local state (selectedMemory, memorySearch,
  // routeTopicDismissed).
  const consumedInitSnapshotRef = useRef(false);
  const [uiState, dispatch] = useReducer(barMachineReducer, undefined, () => {
    const base = createInitialBarMachineState();
    if (typeof window === "undefined") return base;
    const snapshot = peekBarRestoreSnapshot(window.location.pathname);
    if (!snapshot) return base;
    consumedInitSnapshotRef.current = true;
    return {
      ...base,
      value: snapshot.value,
      phase: snapshot.phase,
      activeNavigationIndex: snapshot.activeNavigationIndex,
      routeTopicDismissed: snapshot.routeTopicDismissed,
      isBarExpanded: snapshot.isBarExpanded,
      mobileComposeState: snapshot.mobileComposeState,
    };
  });
  const {
    value,
    scope,
    phase,
    barFocused,
    barOverlayOpen,
    barPanelSuppressed,
    mobileComposeState,
    activeNavigationIndex,
    isBarExpanded,
    detectedUrl,
    memorySearch,
    isDragging,
    composing,
    routeTopicDismissed,
  } = uiState;
  const [domainState, domainDispatch] = useReducer(
    barDomainReducer,
    undefined,
    createInitialBarDomainState,
  );
  const {
    recallQuery,
    askAnswer,
    aiLoading,
    aiStreaming,
    rememberState,
    rememberedTitle,
    selectedMemory,
    barNotification,
    dreamNotification,
    statusNotification,
    stagedFiles,
    pushing,
  } = domainState;

  // Resolved notification: transient > dream (rare) > status (frequent)
  const activeNotification =
    barNotification ?? dreamNotification ?? statusNotification;

  // Auto-dismiss timer for notifications with autoDismissMs
  const dismissTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const setBarNotification = useCallback((n: BarNotification | null) => {
    if (dismissTimerRef.current) {
      clearTimeout(dismissTimerRef.current);
      dismissTimerRef.current = null;
    }
    domainDispatch({ type: "setBarNotification", value: n });
    if (n?.autoDismissMs && n.autoDismissMs > 0) {
      dismissTimerRef.current = setTimeout(
        () => domainDispatch({ type: "setBarNotification", value: null }),
        n.autoDismissMs,
      );
    }
  }, []);

  const dreamDismissTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const setDreamNotification = useCallback((n: BarNotification | null) => {
    if (dreamDismissTimerRef.current) {
      clearTimeout(dreamDismissTimerRef.current);
      dreamDismissTimerRef.current = null;
    }
    domainDispatch({ type: "setDreamNotification", value: n });
  }, []);
  // Bridge: MutationCache → bar notification (universal async action pattern)
  useEffect(() => {
    registerMutationToastHandler((n) => {
      setBarNotification({
        ...n,
        onDismiss: () => setBarNotification(null),
      });
    });
    return () => unregisterMutationToastHandler();
  }, [setBarNotification]);

  // Bar rewire. Three independent states:
  //
  //   - Live dreaming capsule   → useDreamRunStatus() + effect A below.
  //                               Plain lifecycle state from dream_runs.
  //                               No notification backing.
  //   - Completed run bar push  → unseen dream_run_completed notifications
  //                               + effect B below. Replay-safe via the
  //                               notifications markSeen mutation on mount.
  //   - Pending decisions       → NOT in bar logic. The inbox badge reads
  //                               from the notification summary.
  //
  // Per-session dedup by notification id prevents a double-render when
  // an SSE notification.created event and the query hydration both
  // observe the same row. The ref does NOT need to persist across
  // refreshes — the server-side seen write (gated on actual visibility
  // below) ensures the same row cannot re-push on a reconnect.
  const shownNotificationIdsRef = useRef<Set<string>>(new Set());
  // Tracks whether Effect A is currently showing the dreaming
  // capsule. Effect B sets this to false when a completed-run push
  // takes over the slot, so Effect A's status-change clear path
  // cannot clobber the push.
  const dreamingSlotOwnedRef = useRef(false);
  // Per-run dismiss: when the user closes the "Dreaming…" capsule,
  // we don't want Effect A to immediately re-show it on the next
  // render (the run is still "running"). Stash the run.id in this
  // ref so Effect A skips re-presenting that specific run. A NEW
  // run (different runId) will surface a fresh dreaming capsule
  // because the dismissed-id no longer matches.
  const dismissedDreamingRunIdRef = useRef<string | null>(null);
  // Mark-seen is gated on actual visibility (Effect C below), not
  // fired from Effect B. This ref carries the id of the receipt
  // that Effect B just queued but has not yet been confirmed
  // visible. If a transient barNotification is shadowing the dream
  // slot, or Effect A reclaims the slot with a new "dreaming"
  // capsule, Effect C stays silent and the receipt remains in the
  // unseen queue — the reconnect / next-render path will re-present
  // it when the bar is actually clear. Without this gate, a receipt
  // that the user never saw would get marked seen, creating the
  // "shadowed yet consumed" bug the Phase 4 review flagged.
  const pendingSeenIdRef = useRef<string | null>(null);

  // Effect A — live "Dreaming…" capsule driven by dream run status.
  //
  // Shows only while a run is actually in flight. When the run
  // leaves the running state (completed / failed / stale), the
  // dreaming capsule is cleared BUT only if it is still the active
  // dream notification — a completed-run push arriving mid-handoff
  // must not be clobbered by a late status change.
  //
  // Dismissable per-run: onDismiss writes the current runId to
  // dismissedDreamingRunIdRef so the effect's next firing doesn't
  // re-present the same run. A NEW run gets a fresh capsule
  // because the dismissed-id no longer matches.
  useEffect(() => {
    const currentRunId = dreamRunStatus.runId ?? null;
    if (dreamRunStatus.status === "running") {
      // User already dismissed this exact run — stay silent until a
      // new run starts.
      if (
        currentRunId !== null &&
        dismissedDreamingRunIdRef.current === currentRunId
      ) {
        return;
      }
      setDreamNotification({
        type: "dreaming",
        message: t.dreams.barDreamingTitle,
        onDismiss: () => {
          if (currentRunId !== null) {
            dismissedDreamingRunIdRef.current = currentRunId;
          }
          setDreamNotification(null);
          dreamingSlotOwnedRef.current = false;
        },
      });
      dreamingSlotOwnedRef.current = true;
      return;
    }
    // Status has left "running". Clear the dismissed-run guard so
    // a hypothetical re-run of the same id (shouldn't happen, but
    // defensive) gets a fresh capsule. Only clear the slot if
    // Effect A still owns it — Effect B may have replaced the
    // dreaming capsule with a completed-run push and that push must
    // win over a late status change.
    dismissedDreamingRunIdRef.current = null;
    if (dreamingSlotOwnedRef.current) {
      setDreamNotification(null);
      dreamingSlotOwnedRef.current = false;
    }
  }, [dreamRunStatus.status, dreamRunStatus.runId, setDreamNotification, t]);

  // Effect B — completed-run receipt push from the notification
  // framework. Observes the useNotifications cache for unseen
  // dream_run_completed rows and pushes the newest one to the bar.
  // Fires on both initial hydration (missed-while-offline receipts)
  // and SSE-invalidated refetches (live run completion).
  //
  // Replay guarantees:
  //   1. Per-session ref dedup catches double-observation of the
  //      same row during a single session.
  //   2. The notifications markSeen mutation marks the row as seen
  //      on the server, so a reconnect or a hard refresh will not
  //      re-fetch it via unseenOnly=true and will not re-push.
  //   3. The notification has a server-side 24h expiry — a stale
  //      row never reaches the bar outside its freshness window.
  useEffect(() => {
    const rows = unseenDreamReceipts?.notifications ?? [];
    if (rows.length === 0) return;
    const fresh = rows.find(
      (row: NotificationRow) => !shownNotificationIdsRef.current.has(row.id),
    );
    if (!fresh) return;
    shownNotificationIdsRef.current.add(fresh.id);

    const payload = fresh.payload as DreamRunCompletedPayload | undefined;
    const counts = payload?.counts;
    const changeCount =
      (counts?.merged ?? 0) +
      (counts?.contradictions ?? 0) +
      (counts?.archived ?? 0) +
      (counts?.organized ?? 0) +
      (counts?.restructures ?? 0);
    // Scan count reads from the notification payload, NOT from the
    // active hub's dreamReport. dream_run_completed is a
    // user-audience row that can be for any hub the user belongs
    // to, so using the active hub's run would leak cross-hub
    // state into the push (the Phase 4 review's "medium" finding).
    // The server adds memories_scanned to the payload in the same
    // commit that wires this effect.
    const scannedCount = payload?.memories_scanned ?? 0;
    const targetHubId = fresh.hub_id ?? activeHubId ?? "personal";

    const handleRecent = () => {
      const storageKey = `memax-recent-filters-${targetHubId}`;
      if (typeof globalThis.localStorage !== "undefined") {
        localStorage.setItem(
          storageKey,
          JSON.stringify({ window: "12h", actor: "all" }),
        );
      }
      if (fresh.hub_id && fresh.hub_id !== activeHubId) {
        switchHub(fresh.hub_id);
      }
      // Navigate straight to the target hub's memories overview.
      // Pushing "/home" here sent the flow through the entry resolver,
      // which (a) could land before the switchHub state propagated and
      // (b) fired the #recent-section scroll below before the resolver
      // settled — so the scroll never happened.
      const targetEntry = hubs.find(({ hub }) => hub.id === targetHubId);
      const targetPath =
        targetEntry && user
          ? buildMemoriesPath(hubRouteSlug(targetEntry.hub, user.id))
          : "/h/personal/memories";
      router.push(targetPath);
      setTimeout(() => {
        document
          .getElementById("recent-section")
          ?.scrollIntoView({ behavior: "smooth", block: "start" });
      }, 120);
    };
    const handleInbox = () => {
      openInbox();
    };

    // partial_failed wins over count-based classification: if the
    // engine flagged phase issues, surface that even when some
    // phases still produced changes. The server adds `status` to
    // the payload; older rows without it are treated as clean.
    const isPartial = payload?.status === "partial_failed";
    const dreamType = isPartial
      ? "dream_partial"
      : changeCount > 0
        ? "dream_complete"
        : "dream_clean";
    const dreamMessage = isPartial
      ? t.dreams.barPartialTitle
      : changeCount > 0
        ? interpolate(t.dreams.barCompleteTitle, {
            n: String(changeCount),
          })
        : t.dreams.barCleanTitle;

    setDreamNotification({
      type: dreamType,
      message: dreamMessage,
      detail:
        scannedCount > 0
          ? interpolate(t.dreams.barDetailLastNight, {
              n: String(scannedCount),
            })
          : undefined,
      actionLabel: changeCount > 0 ? t.dreams.barView : undefined,
      onAction:
        changeCount === 0
          ? undefined
          : (counts?.contradictions ?? 0) > 0
            ? handleInbox
            : handleRecent,
      onDismiss: () => {
        setDreamNotification(null);
        // Explicit dismiss is a direct visibility signal — the
        // user saw the card. Safe to mark seen even though the
        // visibility-gate effect below would also have done it.
        if (pendingSeenIdRef.current === fresh.id) {
          pendingSeenIdRef.current = null;
          notificationMarkSeen.mutate(fresh.id);
        }
      },
      autoDismissMs: 6000,
    });
    // Effect B now owns the dream slot — tell Effect A so its
    // next status-change clear does not clobber this push.
    dreamingSlotOwnedRef.current = false;
    // Stage the mark-seen write for Effect C below to fire AFTER
    // it observes that the receipt is actually the visible active
    // notification. Do NOT mark seen here: if a transient
    // barNotification is shadowing the dream slot at this moment,
    // the user hasn't seen the receipt yet, and marking it seen
    // would consume the row from the unseen queue without ever
    // presenting it.
    pendingSeenIdRef.current = fresh.id;
  }, [
    activeHubId,
    interpolate,
    notificationMarkSeen,
    openInbox,
    router,
    setDreamNotification,
    t,
    unseenDreamReceipts,
    switchHub,
    hubs,
    user,
  ]);

  // Effect C — mark-seen visibility gate for completed-run receipts.
  //
  // Fires whenever the notification stack state changes. Marks the
  // pending receipt seen ONLY when the dream slot is genuinely the
  // visible active notification — i.e., barNotification is null
  // (no transient shadowing) AND dreamNotification is the
  // dream completion receipt (not a dreaming capsule or a stale
  // leftover). If neither condition holds, the pending id stays in
  // place and this effect re-runs on the next state change; the
  // receipt remains in the server-side unseen queue and will
  // re-push cleanly on the next tick.
  useEffect(() => {
    const pendingId = pendingSeenIdRef.current;
    if (!pendingId) return;
    if (barNotification != null) return;
    if (
      dreamNotification?.type !== "dream_complete" &&
      dreamNotification?.type !== "dream_clean" &&
      dreamNotification?.type !== "dream_partial"
    ) {
      return;
    }
    pendingSeenIdRef.current = null;
    notificationMarkSeen.mutate(pendingId);
  }, [barNotification, dreamNotification, notificationMarkSeen]);

  useEffect(() => {
    if (barNotification != null) {
      if (dreamDismissTimerRef.current) {
        clearTimeout(dreamDismissTimerRef.current);
        dreamDismissTimerRef.current = null;
      }
      return;
    }
    if (
      !dreamNotification?.autoDismissMs ||
      dreamNotification.autoDismissMs <= 0
    ) {
      if (dreamDismissTimerRef.current) {
        clearTimeout(dreamDismissTimerRef.current);
        dreamDismissTimerRef.current = null;
      }
      return;
    }
    dreamDismissTimerRef.current = setTimeout(() => {
      dreamDismissTimerRef.current = null;
      dreamNotification.onDismiss?.();
      if (!dreamNotification.onDismiss) {
        domainDispatch({ type: "setDreamNotification", value: null });
      }
    }, dreamNotification.autoDismissMs);
    return () => {
      if (dreamDismissTimerRef.current) {
        clearTimeout(dreamDismissTimerRef.current);
        dreamDismissTimerRef.current = null;
      }
    };
  }, [barNotification, dreamNotification]);

  useEffect(() => {
    if (!activeNotification) return;
    if (!DREAM_ACTION_DISMISS_TYPES.has(activeNotification.type)) return;

    const dismiss = () => setDreamNotification(null);
    window.addEventListener("pointerdown", dismiss, { once: true });
    window.addEventListener("keydown", dismiss, { once: true });
    return () => {
      window.removeEventListener("pointerdown", dismiss);
      window.removeEventListener("keydown", dismiss);
    };
  }, [activeNotification, setDreamNotification]);

  const setRecallQuery = useCallback(
    (nextValue: string) =>
      domainDispatch({ type: "setRecallQuery", value: nextValue }),
    [],
  );
  const setAskAnswer = useCallback(
    (nextValue: string) =>
      domainDispatch({ type: "setAskAnswer", value: nextValue }),
    [],
  );
  const setAiLoading = useCallback(
    (nextValue: boolean) =>
      domainDispatch({ type: "setAiLoading", value: nextValue }),
    [],
  );
  const setAiStreaming = useCallback(
    (nextValue: boolean) =>
      domainDispatch({ type: "setAiStreaming", value: nextValue }),
    [],
  );
  const setRememberState = useCallback(
    (nextValue: RememberState) =>
      domainDispatch({ type: "setRememberState", value: nextValue }),
    [],
  );
  const setRememberedTitle = useCallback(
    (nextValue: string) =>
      domainDispatch({ type: "setRememberedTitle", value: nextValue }),
    [],
  );
  const setSelectedMemory = useCallback(
    (nextValue: Memory | null) =>
      domainDispatch({ type: "setSelectedMemory", value: nextValue }),
    [],
  );
  const setStatusNotification = useCallback(
    (nextValue: BarNotification | null) =>
      domainDispatch({ type: "setStatusNotification", value: nextValue }),
    [],
  );
  const setStagedFiles = useCallback(
    (nextValue: StagedFile[]) =>
      domainDispatch({ type: "setStagedFiles", value: nextValue }),
    [],
  );
  const updateStagedFiles = useCallback(
    (updater: (files: StagedFile[]) => StagedFile[]) =>
      domainDispatch({ type: "updateStagedFiles", updater }),
    [],
  );
  const setPushing = useCallback(
    (nextValue: boolean) =>
      domainDispatch({ type: "setPushing", value: nextValue }),
    [],
  );
  const setValue = useCallback(
    (nextValue: string) => dispatch({ type: "setValue", value: nextValue }),
    [],
  );
  const setScope = useCallback(
    (nextScope: BarScope) => dispatch({ type: "setScope", scope: nextScope }),
    [],
  );
  const setPhase = useCallback(
    (nextPhase: Phase) => dispatch({ type: "setPhase", phase: nextPhase }),
    [],
  );
  const setBarFocused = useCallback(
    (nextValue: boolean) =>
      dispatch({ type: "setBarFocused", value: nextValue }),
    [],
  );
  const setBarOverlayOpen = useCallback(
    (nextValue: boolean) =>
      dispatch({ type: "setBarOverlayOpen", value: nextValue }),
    [],
  );
  const setMobileComposeState = useCallback(
    (nextValue: MobileComposeState) =>
      dispatch({ type: "setMobileComposeState", value: nextValue }),
    [],
  );
  const setActiveNavigationIndex = useCallback(
    (nextValue: number | null) =>
      dispatch({ type: "setActiveNavigationIndex", value: nextValue }),
    [],
  );
  const setIsBarExpanded = useCallback(
    (nextValue: boolean) =>
      dispatch({ type: "setBarExpanded", value: nextValue }),
    [],
  );
  const setBarPanelSuppressed = useCallback(
    (nextValue: boolean) =>
      dispatch({ type: "setBarPanelSuppressed", value: nextValue }),
    [],
  );
  const setDetectedUrl = useCallback(
    (nextValue: string | null) =>
      dispatch({ type: "setDetectedUrl", value: nextValue }),
    [],
  );
  const clearDetectedUrl = useCallback(
    () => dispatch({ type: "setDetectedUrl", value: null }),
    [],
  );
  const setMemorySearch = useCallback(
    (nextValue: string) =>
      dispatch({ type: "setMemorySearch", value: nextValue }),
    [],
  );
  const setIsDragging = useCallback(
    (nextValue: boolean) =>
      dispatch({ type: "setIsDragging", value: nextValue }),
    [],
  );
  const resetRecallState = useCallback(() => {
    domainDispatch({ type: "resetRecall" });
  }, []);
  // AbortControllers keyed by file id — kept outside state to avoid re-renders
  const abortControllers = useRef<Map<string, AbortController>>(new Map());
  const nextFileId = useRef(0);

  const allFilesReady =
    stagedFiles.length > 0 &&
    stagedFiles.every((f) => f.status === "uploaded" || f.status === "error");

  /** Start eager upload for a single staged file */
  const startEagerUpload = useCallback(
    (file: StagedFile) => {
      const ac = new AbortController();
      abortControllers.current.set(file.id, ac);

      // Mark uploading
      updateStagedFiles((prev) =>
        prev.map((f) =>
          f.id === file.id ? { ...f, status: "uploading" as const } : f,
        ),
      );

      (async () => {
        try {
          const bytes = file.binary
            ? decodeBinaryBase64(file.content)
            : encodeTextBytes(file.content);
          const contentType = detectUploadMimeType(
            file.name,
            file.binary,
            file.binary
              ? file.name.toLowerCase().endsWith(".pdf")
                ? "pdf"
                : "image"
              : undefined,
          );

          let fileRef: import("@/hooks/use-memories").FileRef | undefined;
          if (shouldUsePresignedUpload(bytes.byteLength, file.binary)) {
            fileRef = await uploadMemoryObject({
              name: file.name,
              bytes,
              contentType,
              signal: ac.signal,
            });
          } else {
            // Small text files don't need presigned upload — sent inline on submit
            fileRef = undefined;
          }

          updateStagedFiles((prev) =>
            prev.map((f) =>
              f.id === file.id
                ? { ...f, status: "uploaded" as const, fileRef }
                : f,
            ),
          );
        } catch (err: unknown) {
          if ((err as Error)?.name === "AbortError") return; // cancelled — file removed
          updateStagedFiles((prev) =>
            prev.map((f) =>
              f.id === file.id
                ? {
                    ...f,
                    status: "error" as const,
                    error: (err as Error)?.message || "Upload failed",
                  }
                : f,
            ),
          );
        } finally {
          abortControllers.current.delete(file.id);
        }
      })();
    },
    [updateStagedFiles],
  );

  const addStagedFiles = useCallback(
    (
      files: {
        name: string;
        content: string;
        binary: boolean;
        contentType: string;
      }[],
    ) => {
      const toAdd: StagedFile[] = [];

      setActiveNavigationIndex(null);
      setDetectedUrl(null);
      resetRecallState();
      setPhase("input");
      // Clear peel-suppression so the bar engages once files are staged.
      // Without this, a bar that was previously suppressed (via peel or
      // ⌘K-hide) would stay at rest even though hasFiles=true provides
      // the lift signal — files would be "saved" into a hidden bar.
      // This is the shared file-intake seam — drag-drop, paste, and any
      // future picker all route through addStagedFiles, so the invariant
      // is fixed once here (codex-review 2026-04-21). Spec: bar does NOT
      // elevate during the drag gesture itself — only after the drop
      // commits files, which triggers this path.
      dispatch({ type: "setBarPanelSuppressed", value: false });

      updateStagedFiles((prev) => {
        const existing = new Set(prev.map((f) => f.name));
        const newFiles: StagedFile[] = files
          .filter((f) => !existing.has(f.name))
          .map((f) => ({
            id: `file-${nextFileId.current++}`,
            name: f.name,
            content: f.content,
            binary: f.binary,
            contentType: f.contentType || "application/octet-stream",
            size: f.binary
              ? Math.ceil((f.content.length * 3) / 4)
              : new TextEncoder().encode(f.content).byteLength,
            status: "pending" as const,
          }));
        toAdd.push(...newFiles);
        return [...prev, ...newFiles];
      });

      // Start eager upload for each new file (after state update)
      requestAnimationFrame(() => {
        toAdd.forEach((f) => startEagerUpload(f));
      });
    },
    [
      resetRecallState,
      setActiveNavigationIndex,
      setDetectedUrl,
      setPhase,
      startEagerUpload,
      updateStagedFiles,
    ],
  );

  const removeStagedFile = useCallback(
    (id: string) => {
      // Abort in-flight upload
      const ac = abortControllers.current.get(id);
      if (ac) {
        ac.abort();
        abortControllers.current.delete(id);
      }
      updateStagedFiles((prev) => prev.filter((f) => f.id !== id));
    },
    [updateStagedFiles],
  );

  const clearStagedFiles = useCallback(() => {
    // Abort all in-flight uploads
    abortControllers.current.forEach((ac) => ac.abort());
    abortControllers.current.clear();
    setStagedFiles([]);
  }, [setStagedFiles]);
  const retryRemember = useCallback(() => {
    rememberRetryRef.current?.();
  }, []);

  const undoRemember = useCallback(() => {
    rememberUndoRef.current?.();
  }, []);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const aiRequestId = useRef(0);
  const aiAbortRef = useRef<AbortController | null>(null);
  // Monotonic epoch for remember submissions. Incremented on every
  // `peelForNavigation()` so any in-flight mutation's .then/.catch can
  // detect that the user has navigated away and suppress the UI
  // rehydration (setRememberState/setPushing/enterRememberSurface).
  // The network write still proceeds — we only drop the UI feedback,
  // since the destination bar must stay rested.
  const rememberEpochRef = useRef(0);
  const aiBufferRef = useRef("");
  // RAF handle for the buffered AI flush. 72ms setTimeout was replaced with
  // requestAnimationFrame so we coalesce at the browser's paint cadence
  // (~60fps, drops frames under load) instead of force-scheduling every 72ms.
  // Industry pattern — ChatGPT/Claude.ai/Perplexity all RAF-batch tokens.
  const aiFlushRafRef = useRef<number | null>(null);
  const askAnswerRef = useRef(askAnswer);
  const uploadingRef = useRef(false);
  const forceRememberRef = useRef(false);
  // Tracks whether the currently-open compose modal was opened FROM
  // the bar via ⇧↵ ("graduate to compose"). Used by the
  // restore-on-cancel effect below so the bar can recover the
  // user's text if they back out of compose without saving.
  const composeGraduatedFromBarRef = useRef(false);
  const composeWasOpenRef = useRef(false);
  const sentResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const rememberUndoRef = useRef<(() => void) | null>(null);
  const rememberRetryRef = useRef<(() => void) | null>(null);
  const navigationItemsRef = useRef<BarNavigationItem[]>([]);
  const activeNavigationIndexRef = useRef<number | null>(activeNavigationIndex);

  useEffect(() => {
    activeNavigationIndexRef.current = activeNavigationIndex;
  }, [activeNavigationIndex]);

  // ⇧↵-graduate restore: when the user opens compose from the bar
  // and then backs out without saving (Esc, X, backdrop click),
  // push the draft back into the bar so they can either finish in
  // the quick-capture surface or re-graduate. Successful save
  // calls compose.clear(), so we use "draft content was non-empty
  // when modal closed" as the cancel signal.
  //
  // Effect fires only on compose.open transitions; ignores other
  // draft / open mutations to avoid double-restores.
  useEffect(() => {
    const wasOpen = composeWasOpenRef.current;
    composeWasOpenRef.current = compose.open;
    if (!wasOpen || compose.open) return;
    if (!composeGraduatedFromBarRef.current) return;
    composeGraduatedFromBarRef.current = false;
    const draftContent = compose.draft.content;
    if (!draftContent || !draftContent.trim()) return;
    // Cancelled with text intact → restore + clear compose so the
    // bar becomes the single source for the user's text again.
    setValue(draftContent);
    compose.setDraft({ content: "" });
    inputRef.current?.focus();
  }, [compose, compose.open, compose.draft.content, setValue]);

  useEffect(() => {
    askAnswerRef.current = askAnswer;
  }, [askAnswer]);

  /**
   * Cancel any in-flight AI stream AND clear the visible answer. Aborts
   * the SSE controller, bumps `aiRequestId` so late deltas are discarded
   * (the id guard in the stream handler is what actually prevents
   * setState-on-stale-request), cancels any pending RAF flush, and
   * clears the draft text + loading/streaming flags.
   *
   * Used by outside-click when `aiLoading` is true (codex-review
   * 2026-04-21 — previously the outside-click handler only called
   * `setAskAnswer("")` + `setAiLoading(false)` which hid the UI without
   * stopping the stream; chunks kept appending and rehydrated the
   * answer).
   */
  const cancelAI = useCallback(() => {
    aiRequestId.current += 1;
    aiAbortRef.current?.abort();
    aiAbortRef.current = null;
    if (aiFlushRafRef.current !== null) {
      cancelAnimationFrame(aiFlushRafRef.current);
      aiFlushRafRef.current = null;
    }
    aiBufferRef.current = "";
    setAskAnswer("");
    setAiLoading(false);
    setAiStreaming(false);
  }, [setAskAnswer, setAiLoading, setAiStreaming]);

  // Unmount cleanup for the AI stream: cancel any pending RAF flush, abort
  // the active SSE controller, and bump the request id so late deltas
  // arriving after unmount are treated as stale (the id check in the event
  // handler is what actually guards against setState-on-unmounted-tree).
  useEffect(() => {
    return () => {
      if (aiFlushRafRef.current !== null) {
        cancelAnimationFrame(aiFlushRafRef.current);
        aiFlushRafRef.current = null;
      }
      aiAbortRef.current?.abort();
      aiAbortRef.current = null;
      aiRequestId.current += 1;
    };
  }, []);

  const mobileComposeActive = mobileComposeState !== "docked";
  const mobileComposeOpen = mobileComposeState === "fullscreen";
  const derivedInteraction = deriveBarInteractionState({
    value,
    stagedFileCount: stagedFiles.length,
    allFilesReady,
    scope:
      scope.kind === "command"
        ? { kind: "command", command: scope.command }
        : scope.kind === "topic"
          ? { kind: "topic" }
          : { kind: "recall" },
    phase,
    isBarExpanded,
    composing,
  });
  const rememberActive = rememberState !== "idle" || pushing;
  const interaction: BarInteractionState = {
    ...derivedInteraction,
    hasLiftSignal:
      derivedInteraction.hasLiftSignal ||
      recallQuery.length > 0 ||
      rememberActive,
    hasExpandContent:
      derivedInteraction.hasExpandContent ||
      recallQuery.length > 0 ||
      rememberActive,
    sendState:
      phase === "loading" || pushing || derivedInteraction.filesUploading
        ? "loading"
        : derivedInteraction.canRemember
          ? "ready"
          : "disabled",
    mobileComposeState,
  };
  const surface = deriveBarSurfaceState({
    view,
    isMobile,
    barFocused,
    barOverlayOpen,
    barPanelSuppressed,
    mobileComposeState,
    phase,
    value,
    recallQuery,
    rememberState,
    pushing,
    selectedMemory: Boolean(selectedMemory),
    hasText: interaction.hasText,
    hasFiles: interaction.hasFiles,
    isBarExpanded,
    isCommandMode: interaction.isCommandMode,
    showRecallSurfaces: interaction.showRecallSurfaces,
    hasLiftSignal: interaction.hasLiftSignal,
    hasExpandContent: interaction.hasExpandContent,
  });
  const { hasText, hasFiles, isCommandMode, filesUploading } = interaction;
  const { sendKind, barMode } = derivedInteraction;

  // Recall searches every accessible hub; activeHubId is only a ranking boost.
  // Route-derived topic scope is suppressed when the user has explicitly
  // dismissed the chip on this route — recall falls back to global so the
  // user can stay on the topic page but search across everything. M1 race
  // is avoided because `routeTopicDismissed` is seeded from the snapshot
  // at reducer-init time (above), not via an effect.
  const effectiveRecallTopicId =
    routeTopicDismissed || !routeTopicId ? undefined : routeTopicId;
  const {
    data: recallResult,
    isFetching: recallFetching,
    isError: recallIsError,
    error: recallError,
    refetch: retryRecall,
  } = useRecall(recallQuery, activeHubId || undefined, effectiveRecallTopicId);

  // On route change: first check if a nav-peel snapshot matches the new
  // pathname (user returned via back/forward/sidebar). If so, rehydrate
  // the bar (including recallQuery — React Query cache rehydrates the
  // result rows for free via the `[recall, q, hub, topic]` key). Otherwise
  // treat as forward navigation: clear transient surfaces UNLESS the bar
  // is explicitly Cmd+K-suppressed (that's a separate, narrower intent).
  // Also clear `routeTopicDismissed` on natural route change — the flag is
  // only meaningful on the topic route it was set for.
  // One-shot mount cleanup: if the reducer init seeded state from a
  // peeked snapshot, finalize the consume now so the snapshot doesn't
  // resurrect on a later remount of the same route within TTL. Strict-
  // mode safe because the ref flips to false after the first pass.
  useEffect(() => {
    if (!consumedInitSnapshotRef.current) return;
    consumedInitSnapshotRef.current = false;
    if (typeof window !== "undefined") {
      consumeBarRestoreSnapshot(window.location.pathname);
    }
  }, []);

  // Pathname change → reset ROUTE-LOCAL state only. Live bar state
  // (value, recallQuery, phase, askAnswer, remember*, stagedFiles)
  // persists across routes. `peelForNavigation` is the only path that
  // flips `barPanelSuppressed`; clear paths: (a) focus / ⌘K / click-in,
  // (b) `addStagedFiles()` on file intake, (c) returning to peel-origin
  // via the peek-match branch at the bottom of this effect.
  //
  // Snapshot restore is NOT performed here — snapshots are a
  // bfcache/remount fallback only (consumed at init + pageshow).
  // Restoring here would clobber newer live state with an old snapshot.
  // The peek-match branch below is "did user return to origin?" —
  // it only flips the suppression flag, never touches live state.
  useEffect(() => {
    clearNavigationSelection();
    // routeTopicDismissed is scoped to its origin topic route. Always
    // reset on pathname change; it's meaningless on unrelated routes.
    dispatch({ type: "clearRouteTopicDismissal" });
    // Route-local surfaces that should NOT leak to the next route,
    // regardless of suppression state (codex-review v2 M2):
    setSelectedMemory(null);
    setMemorySearch("");
    if (!barPanelSuppressed) {
      // ⌘K-hide intent preserves the overlay; other pathname changes
      // dismiss it. (suppressed == user hid the panel intentionally.)
      setBarOverlayOpen(false);
      setMobileComposeState("docked");
      return;
    }
    // Bar is suppressed on this pathname change. If the new pathname
    // matches a peel-origin snapshot, the user just returned to where
    // the bar was peeled from (back/forward/sidebar nav) — clear
    // suppression so the preserved live state re-drives hasLiftSignal
    // → engaged. The snapshot's tracking job is done; consume it so a
    // later navigation can't re-trigger this branch on the same entry.
    //
    // We restore ONLY the chrome state (isBarExpanded, mobileComposeState)
    // that `peelForNavigation` explicitly collapsed. The old behavior —
    // flipping suppression and nothing else — left the bar visibly
    // deactivated on mobile: users had to tap the bar to see the recall
    // snapshot they had queued before navigating, because expanded/
    // compose state never came back. Value / phase / recallQuery /
    // activeNavigationIndex / routeTopicDismissed stay live-state (no
    // clobber of newer user input that happened off-route), but chrome
    // has to be rehydrated because peel cleared it on exit.
    if (typeof window === "undefined") return;
    const snapshot = peekBarRestoreSnapshot(pathname);
    if (snapshot) {
      dispatch({
        type: "restoreChromeAfterPeel",
        isBarExpanded: snapshot.isBarExpanded,
        mobileComposeState: snapshot.mobileComposeState,
      });
      consumeBarRestoreSnapshot(pathname);
    }
  }, [pathname]); // eslint-disable-line react-hooks/exhaustive-deps

  // Brain (/home) is the bar-first surface — when the user lands
  // there, auto-focus the input so they can start typing
  // immediately without a second click. Matches macOS Spotlight's
  // open-ready-to-type behavior. Skip on mobile to avoid forcing
  // the virtual keyboard open on navigation (user might just be
  // reading memory counts); mobile users tap to engage explicitly.
  // Also skip if the input already has focus to avoid thrash on
  // rapid route changes.
  useEffect(() => {
    // Brain autofocus: only the LEGACY brain-view shape (v1 /home)
    // wants the bar grabbed on mount. The v2 chat surface (/brain*)
    // owns its own composer; stealing focus here would tug it off
    // the user's first keystroke. isChatSurfaceRoute is the gate.
    if (!isBrainViewRoute(pathname)) return;
    if (isChatSurfaceRoute(pathname)) return;
    if (isMobile) return;
    if (document.activeElement === inputRef.current) return;
    // Small defer so the focus lands after Next.js finishes its
    // route transition paint, preventing the focus from being
    // yanked back by a subsequent render in the same tick.
    const handle = setTimeout(() => {
      inputRef.current?.focus();
    }, 0);
    return () => clearTimeout(handle);
  }, [pathname, isMobile]);

  // iOS Safari bfcache / pageshow: if the browser restored the page from
  // cache, `useEffect([pathname])` may not fire (the React tree stays
  // mounted). Explicit `pageshow` listener handles that path.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onPageShow = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      const snapshot = consumeBarRestoreSnapshot(window.location.pathname);
      if (!snapshot) return;
      dispatch({
        type: "restoreFromSnapshot",
        snapshot: {
          value: snapshot.value,
          phase: snapshot.phase,
          activeNavigationIndex: snapshot.activeNavigationIndex,
          routeTopicDismissed: snapshot.routeTopicDismissed,
          isBarExpanded: snapshot.isBarExpanded,
          mobileComposeState: snapshot.mobileComposeState,
        },
      });
      setRecallQuery(snapshot.recallQuery);
    };
    window.addEventListener("pageshow", onPageShow);
    return () => window.removeEventListener("pageshow", onPageShow);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Pick up pending notifications from cross-page navigations (invite join, etc.)
  useEffect(() => {
    const raw = localStorage.getItem("memax_pending_notif");
    if (raw) {
      localStorage.removeItem("memax_pending_notif");
      try {
        const notif = JSON.parse(raw);
        setBarNotification({
          ...notif,
          autoDismissMs: 3000,
          onDismiss: () => setBarNotification(null),
        });
      } catch {
        // ignore malformed
      }
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Phase transition: loading → recall-result when data arrives
  useEffect(() => {
    if (
      phase === "loading" &&
      recallQuery &&
      recallResult !== undefined &&
      !recallIsError
    ) {
      setPhase("recall-result");
      recordEvent({
        channel: "recall",
        tone: (recallResult?.memories.length ?? 0) > 0 ? "success" : "neutral",
        title: "Recall panel opened",
        detail: recallQuery,
        context: String(recallResult?.memories.length ?? 0) + " results",
      });
      // Always dismiss notification when results appear — results ARE the feedback.
      // AI synthesis shows inline ✦ cursor, doesn't need a separate notification.
      setBarNotification(null);
    }
  }, [
    phase,
    recallIsError,
    recallQuery,
    recallResult,
    recordEvent,
    setBarNotification,
    setPhase,
  ]);

  // Remember-result stays until user acts (typing, Esc, or link click).
  // No auto-dismiss — gives time for forget confirmation and hint.

  const clearScope = useCallback(() => {
    dispatch({ type: "clearScope" });
  }, []);

  const commitCommandScope = useCallback(
    (command: "hub", query = "") => {
      resetRecallState();
      setPhase("input");
      dispatch({
        type: "commitCommandScope",
        command,
        label: getCommandLabel(command, t),
        query,
      });
      if (detectedUrl) {
        setDetectedUrl(null);
      }
    },
    [detectedUrl, resetRecallState, setDetectedUrl, setPhase, t],
  );

  const setComposing = useCallback((value: boolean) => {
    dispatch({ type: "setComposing", value });
  }, []);

  const dismissRouteTopic = useCallback(() => {
    dispatch({ type: "dismissRouteTopic" });
  }, []);

  const setNavigationItems = useCallback(
    (items: BarNavigationItem[]) => {
      navigationItemsRef.current = items;
      const currentIndex = activeNavigationIndexRef.current;
      const nextIndex =
        items.length === 0
          ? null
          : currentIndex === null
            ? null
            : Math.min(currentIndex, items.length - 1);
      if (currentIndex === nextIndex) {
        return;
      }
      setActiveNavigationIndex(nextIndex);
    },
    [setActiveNavigationIndex],
  );

  const clearNavigationSelection = useCallback(() => {
    setActiveNavigationIndex(null);
  }, [setActiveNavigationIndex]);

  const moveNavigationSelection = useCallback(
    (delta: 1 | -1) => {
      const items = navigationItemsRef.current;
      if (items.length === 0) return false;
      const result = computeNextNavigationIndex(
        activeNavigationIndex,
        items.length,
        delta,
      );
      if (result.next !== activeNavigationIndex) {
        setActiveNavigationIndex(result.next);
      }
      if (result.focusInput) {
        inputRef.current?.focus();
      }
      return true;
    },
    [activeNavigationIndex, setActiveNavigationIndex],
  );

  const activateNavigationSelection = useCallback(() => {
    const items = navigationItemsRef.current;
    const targetIndex = pickDefaultNavIndex(items, activeNavigationIndex);
    if (targetIndex === null) return false;
    clearNavigationSelection();
    items[targetIndex]?.run();
    return true;
  }, [activeNavigationIndex, clearNavigationSelection]);

  const clearRememberFeedback = useCallback(() => {
    if (sentResetTimerRef.current) {
      clearTimeout(sentResetTimerRef.current);
      sentResetTimerRef.current = null;
    }
    domainDispatch({ type: "resetRemember" });
    rememberUndoRef.current = null;
    rememberRetryRef.current = null;
  }, []);

  const resetState = useCallback(
    ({
      refocusInput = true,
      keepFocused = true,
    }: { refocusInput?: boolean; keepFocused?: boolean } = {}) => {
      aiRequestId.current++;
      aiAbortRef.current?.abort();
      domainDispatch({ type: "resetDomain" });
      dispatch({ type: "resetUi" });
      clearRememberFeedback();
      setStagedFiles([]);
      setSelectedMemory(null);
      clearNavigationSelection();
      if (!keepFocused) {
        setBarFocused(false);
      }
      if (refocusInput) {
        setTimeout(() => inputRef.current?.focus(), 50);
      }
    },
    [
      clearNavigationSelection,
      clearRememberFeedback,
      setBarFocused,
      setSelectedMemory,
      setStagedFiles,
    ],
  );

  const reset = useCallback(() => {
    resetState({ refocusInput: true, keepFocused: true });
  }, [resetState]);

  const resetForNavigation = useCallback(() => {
    resetState({ refocusInput: false, keepFocused: false });
  }, [resetState]);

  /**
   * Collapse the bar in preparation for a result-tap navigation AND clear
   * the bar visually (Linear/Raycast ⌘K parity — state is the source of
   * truth; `barPanelSuppressed` is the single "hide it" lever). Also
   * captures a sessionStorage snapshot as a bfcache/remount safety net
   * only — NOT as a second source of truth. Live state persists
   * in-memory across `(app)` route transitions because `BarProvider`
   * is mounted in [app/(app)/layout.tsx] and only `template.tsx`
   * remounts on navigation.
   *
   * Intentionally preserves: `value`, `recallQuery`, `phase`,
   * `askAnswer`, `rememberState`, `pushing`, `stagedFiles`. The
   * `rememberEpoch` bump lets in-flight mutation callbacks suppress
   * UI rehydration without cancelling the network write.
   */
  const peelForNavigation = useCallback(() => {
    const snapshotWorthy =
      value.length > 0 ||
      recallQuery.length > 0 ||
      phase !== "input" ||
      routeTopicDismissed ||
      isBarExpanded ||
      mobileComposeState !== "docked";
    if (snapshotWorthy) {
      saveBarRestoreSnapshot({
        pathname,
        value,
        recallQuery,
        phase,
        activeNavigationIndex: activeNavigationIndexRef.current,
        routeTopicDismissed,
        isBarExpanded,
        mobileComposeState,
        savedAt: Date.now(),
      });
    }
    // Bump remember epoch so any in-flight createMemory mutation's
    // async completion handler suppresses its UI rehydration — without
    // cancelling the network write itself (the memory still gets saved).
    rememberEpochRef.current += 1;
    // Force visibilityState='rest' via the suppressed flag (bar-surface
    // checks it BEFORE any lift signal including rememberActive). Blur
    // input to collapse the keyboard on mobile.
    dispatch({ type: "setBarPanelSuppressed", value: true });
    dispatch({ type: "setBarExpanded", value: false });
    dispatch({ type: "setMobileComposeState", value: "docked" });
    inputRef.current?.blur();
    setBarFocused(false);
  }, [
    pathname,
    value,
    recallQuery,
    phase,
    routeTopicDismissed,
    isBarExpanded,
    mobileComposeState,
    setBarFocused,
  ]);

  // Auto-grow only ever EXPANDS (see use-auto-grow.ts: collapse from
  // scrollHeight is fundamentally circular and produced the wrap-edge
  // flicker). Collapse is owned here. The only safe per-keystroke
  // collapse signal is "value is now empty" — anything else risks
  // collapsing into a layout where the residual content still wraps,
  // which would re-trigger expansion on the next keystroke and bring
  // back a narrower version of the original flicker (e.g. dense
  // glyphs, URLs, narrow viewports — codex review 2026-04-21).
  // Other collapse intents (Esc, peel-back, blur, route change) are
  // already handled as discrete state transitions elsewhere in the
  // bar machine.
  const collapseFromValueChange = useCallback(
    (next: string) => {
      if (!isBarExpanded) return;
      if (next.length === 0) {
        setIsBarExpanded(false);
      }
    },
    [isBarExpanded, setIsBarExpanded],
  );

  const handleChange = useCallback(
    (text: string) => {
      if (selectedMemory) {
        setMemorySearch(text);
        return;
      }
      clearNavigationSelection();
      if (rememberState === "sent" || rememberState === "error") {
        clearRememberFeedback();
      }

      const committedCommand = parseCommittedCommand(text);
      if (committedCommand) {
        commitCommandScope(committedCommand.command, committedCommand.query);
        collapseFromValueChange(committedCommand.query ?? "");
        return;
      }

      if (scope.kind === "command" && text.startsWith("/")) {
        setScope({ kind: "recall" });
        setValue(text);
        collapseFromValueChange(text);
        return;
      }

      if (scope.kind === "command") {
        setValue(text);
        collapseFromValueChange(text);
        return;
      }

      // During recall-result, typing updates value but keeps results
      if (phase === "recall-result" && text !== value) {
        setValue(text);
        collapseFromValueChange(text);
        return;
      }

      setValue(text);
      // URL auto-detect: single pasted URL triggers capture chip
      if (isSingleUrl(text)) {
        setDetectedUrl(text.trim());
      } else if (detectedUrl) {
        setDetectedUrl(null);
      }
      collapseFromValueChange(text);
    },
    [
      commitCommandScope,
      selectedMemory,
      rememberState,
      clearRememberFeedback,
      clearNavigationSelection,
      phase,
      value,
      detectedUrl,
      scope.kind,
      collapseFromValueChange,
      setDetectedUrl,
      setMemorySearch,
      setScope,
      setValue,
    ],
  );

  const enterRememberSurface = useCallback(
    (nextDraft = "") => {
      clearNavigationSelection();
      setPhase("input");
      setRecallQuery("");
      setAskAnswer("");
      setValue(nextDraft);
      setSelectedMemory(null);
      setBarOverlayOpen(true);
      setIsBarExpanded(nextDraft.includes("\n"));
    },
    [
      clearNavigationSelection,
      setAskAnswer,
      setBarOverlayOpen,
      setIsBarExpanded,
      setPhase,
      setRecallQuery,
      setSelectedMemory,
      setValue,
    ],
  );

  const submitRememberContent = useCallback(
    (originalContent: string) => {
      const title = originalContent.split("\n")[0].slice(0, 80);
      clearRememberFeedback();
      enterRememberSurface("");
      setRememberedTitle(title);
      setRememberState("sending");
      setPushing(true);
      // Capture the current remember epoch. If peelForNavigation fires
      // before the mutation resolves, it will bump the epoch and the
      // callbacks below skip UI rehydration — the destination bar stays
      // rested even though the network write completes normally.
      const submissionEpoch = rememberEpochRef.current;
      recordEvent({
        channel: "remember",
        tone: "active",
        title: "Remember submitted",
        detail: title,
      });

      createMemory
        .mutateAsync({
          title,
          content: originalContent,
          source: "web",
          hubId: activeHubId || undefined,
        })
        .then((memory) => {
          if (rememberEpochRef.current !== submissionEpoch) {
            recordEvent({
              channel: "remember",
              tone: "success",
              title: "Remember saved after peel",
              detail: title,
              context: "UI rehydration suppressed",
            });
            return;
          }
          setPushing(false);
          recordEvent({
            channel: "remember",
            tone: "success",
            title: "Remember saved",
            detail: title,
          });
          setRememberState("sent");
          rememberUndoRef.current = () => {
            deleteMemory.mutate(memory.id);
            enterRememberSurface(originalContent);
            clearRememberFeedback();
          };
          rememberRetryRef.current = () => {
            submitRememberContent(originalContent);
          };
          if (sentResetTimerRef.current) {
            clearTimeout(sentResetTimerRef.current);
            sentResetTimerRef.current = null;
          }
        })
        .catch(() => {
          if (rememberEpochRef.current !== submissionEpoch) {
            recordEvent({
              channel: "remember",
              tone: "error",
              title: "Remember failed after peel",
              detail: title,
              context: "UI rehydration suppressed",
            });
            return;
          }
          setPushing(false);
          enterRememberSurface(originalContent);
          recordEvent({
            channel: "remember",
            tone: "error",
            title: "Remember failed",
            detail: title,
          });
          setRememberState("error");
          rememberRetryRef.current = () => {
            submitRememberContent(originalContent);
          };
        });
    },
    [
      activeHubId,
      clearRememberFeedback,
      createMemory,
      deleteMemory,
      enterRememberSurface,
      recordEvent,
      setPushing,
      setRememberState,
      setRememberedTitle,
    ],
  );

  // Send staged files — files are already uploaded to R2 (eager upload).
  // This only creates memory records referencing the uploaded objects.
  const uploadFiles = useCallback(() => {
    const toSend = stagedFiles.filter((f) => f.status === "uploaded");
    if (uploadingRef.current || toSend.length === 0) return;
    uploadingRef.current = true;
    const hint = value.trim();
    clearRememberFeedback();
    enterRememberSurface("");
    setRememberState("sending");
    setPushing(true);
    // Capture remember epoch — peelForNavigation bumps this, and the
    // then/catch below will suppress UI rehydration if the user has
    // navigated away during the upload.
    const submissionEpoch = rememberEpochRef.current;
    recordEvent({
      channel: "remember",
      tone: "active",
      title: "Remember upload started",
      detail:
        toSend.length === 1 ? toSend[0].name : String(toSend.length) + " files",
      context: hint || undefined,
    });
    Promise.allSettled(
      toSend.map(async (file) => {
        const title = file.name.replace(/\.[^/.]+$/, "");
        const contentType = file.binary
          ? file.name.toLowerCase().endsWith(".pdf")
            ? "pdf"
            : "image"
          : undefined;
        return createMemory.mutateAsync({
          title,
          content: file.fileRef ? "" : file.content,
          source: "web",
          source_path: file.name,
          hubId: activeHubId || undefined,
          ...(hint && { hint }),
          ...(contentType && { content_type: contentType }),
          ...(file.fileRef && { file_ref: file.fileRef }),
        });
      }),
    )
      .then((results) => {
        const fulfilled = results.filter((r) => r.status === "fulfilled");
        // Peeled while upload was in-flight — skip UI rehydration. Still
        // clear stagedFiles (they're already done) and mark uploadingRef
        // false so a subsequent retry can re-enter. The network writes
        // already completed.
        if (rememberEpochRef.current !== submissionEpoch) {
          clearStagedFiles();
          uploadingRef.current = false;
          recordEvent({
            channel: "remember",
            tone: fulfilled.length > 0 ? "success" : "error",
            title: "Remember upload settled after peel",
            context: "UI rehydration suppressed",
          });
          return;
        }
        clearStagedFiles();
        uploadingRef.current = false;
        setPushing(false);
        if (fulfilled.length === 0) {
          enterRememberSurface(hint);
          setRememberState("error");
          rememberRetryRef.current = uploadFiles;
          return;
        }
        if (fulfilled.length > 0) {
          const title =
            fulfilled.length === 1
              ? (toSend[0]?.name ?? "")
              : `${fulfilled.length} files`;
          recordEvent({
            channel: "remember",
            tone: "success",
            title:
              fulfilled.length === 1
                ? "Remember upload saved"
                : "Remember uploads saved",
            detail:
              fulfilled.length === 1
                ? toSend[0]?.name
                : String(fulfilled.length) + " memories",
          });
          setRememberedTitle(title);
          setRememberState("sent");
          rememberUndoRef.current = null;
          if (sentResetTimerRef.current) {
            clearTimeout(sentResetTimerRef.current);
            sentResetTimerRef.current = null;
          }
        }
      })
      .catch(() => {
        if (rememberEpochRef.current !== submissionEpoch) {
          uploadingRef.current = false;
          recordEvent({
            channel: "remember",
            tone: "error",
            title: "Remember upload failed after peel",
            context: "UI rehydration suppressed",
          });
          return;
        }
        recordEvent({
          channel: "remember",
          tone: "error",
          title: "Remember upload failed",
          detail: hint || undefined,
        });
        uploadingRef.current = false;
        setPushing(false);
        enterRememberSurface(hint);
        setRememberState("error");
        rememberRetryRef.current = uploadFiles;
      });
  }, [
    clearRememberFeedback,
    clearStagedFiles,
    createMemory,
    enterRememberSurface,
    recordEvent,
    activeHubId,
    setPushing,
    setRememberState,
    setRememberedTitle,
    stagedFiles,
    value,
  ]);

  const triggerAI = useCallback(
    (explicitQuery?: string) => {
      const q = explicitQuery || value;
      const id = ++aiRequestId.current;
      const flushBufferedAI = () => {
        if (!aiBufferRef.current) return "";
        const chunk = aiBufferRef.current;
        aiBufferRef.current = "";
        const next = `${askAnswerRef.current}${chunk}`;
        askAnswerRef.current = next;
        setAskAnswer(next);
        if (next.length > 20) {
          setAiLoading(false);
          setBarNotification(null);
        }
        return chunk;
      };
      const scheduleBufferedAIFlush = () => {
        if (aiFlushRafRef.current !== null) return;
        aiFlushRafRef.current = requestAnimationFrame(() => {
          aiFlushRafRef.current = null;
          flushBufferedAI();
        });
      };

      recordEvent({
        channel: "ai",
        tone: "active",
        title: "AI answer started",
        detail: q,
      });
      setAiLoading(true);
      setAiStreaming(true);
      setAskAnswer("");
      aiBufferRef.current = "";
      if (aiFlushRafRef.current !== null) {
        cancelAnimationFrame(aiFlushRafRef.current);
        aiFlushRafRef.current = null;
      }

      // Abort any previous stream
      aiAbortRef.current?.abort();

      const controller = getMemaxClient().memories.askStream(
        q,
        {
          model: "auto",
          locale,
          hubId: activeHubId || undefined,
          // AI stream must honor the same topic-dismiss gate as useRecall.
          // Otherwise dismissing the chip shows global recall rows but the
          // AI answer stays topic-scoped (codex-review v4 M).
          topicId: effectiveRecallTopicId,
          noRerank: flags.skipRerank || undefined,
        },
        (event, data) => {
          if (aiRequestId.current !== id) return; // stale
          if (event === "delta") {
            const d = data as { text: string };
            aiBufferRef.current += d.text;
            scheduleBufferedAIFlush();
          } else if (event === "done") {
            if (aiFlushRafRef.current !== null) {
              cancelAnimationFrame(aiFlushRafRef.current);
              aiFlushRafRef.current = null;
            }
            flushBufferedAI();
            recordEvent({
              channel: "ai",
              tone: "success",
              title: "AI answer completed",
              detail: q,
            });
            setAiLoading(false);
            setAiStreaming(false);
            setBarNotification(null);
            // Bump the usage cache so the next render sees the
            // incremented ask_count. Without this the canUseAI gate
            // reads stale data and a user at ask_limit-1 finishes an
            // ask, stays "enabled", and races into a 402 on the next
            // try.
            queryClient.invalidateQueries({ queryKey: usageQueryKey });
          } else if (event === "error") {
            if (aiFlushRafRef.current !== null) {
              cancelAnimationFrame(aiFlushRafRef.current);
              aiFlushRafRef.current = null;
            }
            aiBufferRef.current = "";
            recordEvent({
              channel: "ai",
              tone: "error",
              title: "AI answer failed",
              detail: q,
            });
            setAiLoading(false);
            setAiStreaming(false);
            // The SDK surfaces stream errors as
            // `{code, message, status, details}`. The server uses
            // code="quota_exceeded" + status 402 for plan-quota
            // rejection (meter/middleware.go). Give that its own
            // copy and kick a usage refetch so the gate flips to the
            // exhausted state instead of lying about availability.
            const payload = data as {
              code?: string;
              status?: number;
            } | null;
            const isQuotaError =
              payload?.code === "quota_exceeded" || payload?.status === 402;
            if (isQuotaError) {
              queryClient.invalidateQueries({ queryKey: usageQueryKey });
              setAskAnswer(t.bar.ai.quotaExhausted);
            } else {
              setAskAnswer(t.bar.ai.error);
            }
            setBarNotification(null);
          }
        },
      );
      aiAbortRef.current = controller;
    },
    [
      activeHubId,
      effectiveRecallTopicId,
      flags.skipRerank,
      locale,
      recordEvent,
      setAiLoading,
      setAiStreaming,
      setAskAnswer,
      setBarNotification,
      t.bar.ai.error,
      t.bar.ai.quotaExhausted,
      value,
    ],
  );

  const handleSubmit = useCallback(() => {
    const forceRemember = forceRememberRef.current;
    forceRememberRef.current = false;

    if (selectedMemory) return; // In-note search is live

    // Files staged — send already-uploaded files as memories
    if (stagedFiles.length > 0) {
      uploadFiles();
      return;
    }

    if (!value.trim()) return;

    const shouldRemember = getShouldRememberSubmit({
      stagedFileCount: stagedFiles.length,
      forceRemember,
    });

    if (!shouldRemember) {
      const query = value.trim();
      if (!query) return;
      clearRememberFeedback();
      recordEvent({
        channel: "recall",
        tone: "active",
        title: "Recall submitted",
        detail: query,
        context: canUseAI ? "AI auto-answer on" : "sources only",
      });
      setRecallQuery(query);
      setAskAnswer("");
      setSelectedMemory(null);
      setPhase("loading");
      // No notification — send button spinner is the feedback (kitchen 24c)
      // Pro users: fire AI synthesis in parallel with recall
      if (canUseAI) {
        triggerAI(query);
      }
    } else {
      const originalContent = value;
      submitRememberContent(originalContent);
    }
  }, [
    canUseAI,
    clearRememberFeedback,
    recordEvent,
    selectedMemory,
    setAskAnswer,
    setPhase,
    setRecallQuery,
    setSelectedMemory,
    stagedFiles,
    submitRememberContent,
    triggerAI,
    uploadFiles,
    value,
  ]);

  const handleRememberSubmit = useCallback(() => {
    if (selectedMemory) return;
    if (!value.trim() && stagedFiles.length === 0) return;
    forceRememberRef.current = true;
    handleSubmit();
  }, [handleSubmit, selectedMemory, stagedFiles.length, value]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      const isComposeMode =
        isBarExpanded || mobileComposeActive || mobileComposeOpen;

      // ⌘↵ saves the bar draft (quick capture). ⌘⇧↵ is reserved for
      // the global Compose modal hotkey (plan 20) — explicitly skip
      // here so the bar doesn't ALSO submit when the user means
      // "open the longer-form compose surface".
      if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.shiftKey) {
        e.preventDefault();
        if (!value.trim() && stagedFiles.length === 0) return;
        if (selectedMemory) return;
        forceRememberRef.current = true;
        handleSubmit();
        return;
      }

      // ⇧↵ in the bar → "graduate to compose". The bar is for
      // one-liner quick capture; the moment the user reaches for a
      // newline they're signaling intent to write something longer
      // (multi-paragraph, titled, considered) — exactly what the
      // Compose modal exists for. Hand off the in-progress text as
      // the modal's seed content, clear the bar, and let the user
      // finish in the surface that fits the intent.
      //
      // Skip this when the bar is in compose mode (expanded) or
      // mobile-compose-active — those surfaces already support
      // multi-line via plain Enter and the Shift+Enter shortcut is
      // redundant. Also skip during IME composition so CJK users
      // don't lose their composition mid-character.
      if (
        e.key === "Enter" &&
        e.shiftKey &&
        !(e.metaKey || e.ctrlKey) &&
        !composing &&
        !isComposeMode
      ) {
        const trimmed = value.trim();
        if (trimmed.length > 0) {
          e.preventDefault();
          // Mark that compose was opened FROM the bar so the
          // cancel-restore effect (below) knows to push the draft
          // back into the bar if the user backs out without saving.
          composeGraduatedFromBarRef.current = true;
          compose.setDraft({ content: value });
          compose.openModal();
          setValue("");
          return;
        }
        // Empty bar + Shift+Enter falls through to the existing
        // "newline" branch — pressing it as the first keystroke
        // doesn't graduate (there's nothing to carry).
      }
      // Arrow keys: let the textarea own caret movement within multi-line
      // text, and only capture for row navigation when the caret is at the
      // boundary (or a row is already selected — Raycast/Linear pattern).
      if (e.key === "ArrowUp" || e.key === "ArrowDown") {
        const delta = e.key === "ArrowDown" ? 1 : -1;
        const rowSelected = activeNavigationIndexRef.current !== null;
        const target = e.currentTarget;
        const start = target.selectionStart ?? 0;
        const end = target.selectionEnd ?? start;
        const atBoundary =
          delta < 0
            ? !target.value.slice(0, start).includes("\n")
            : !target.value.slice(end).includes("\n");
        if (rowSelected || atBoundary) {
          if (moveNavigationSelection(delta)) {
            e.preventDefault();
          }
        }
        return;
      }
      if (e.key === "Backspace" && scope.kind === "command" && !value) {
        e.preventDefault();
        setScope({ kind: "recall" });
        setValue(getRawCommandInput(scope.command));
        setActiveNavigationIndex(null);
        return;
      }
      // Route-derived topic chip: Backspace on empty input dismisses the
      // chip without leaving the topic route. Recall falls back to global.
      // Gated on !composing so CJK/emoji IME sessions don't nuke the chip.
      if (
        e.key === "Backspace" &&
        !value &&
        !composing &&
        routeTopicId &&
        !routeTopicDismissed
      ) {
        e.preventDefault();
        dispatch({ type: "dismissRouteTopic" });
        return;
      }
      // Compose mode: plain Enter and Shift+Enter both insert newlines.
      // Submit is Cmd/Ctrl+Enter only.
      if (e.key === "Enter" && isComposeMode) return;
      // Shift+Enter = new line (browser default, don't prevent)
      if (e.key === "Enter" && e.shiftKey) return;
      // Mobile: Enter = newline always (send via button only)
      if (e.key === "Enter" && isMobile) return;
      // Desktop: Enter = submit
      if (e.key === "Enter") {
        e.preventDefault();
        if (activateNavigationSelection()) return;
        handleSubmit();
      }
    },
    [
      activateNavigationSelection,
      handleSubmit,
      isBarExpanded,
      isMobile,
      mobileComposeActive,
      mobileComposeOpen,
      moveNavigationSelection,
      selectedMemory,
      scope,
      setActiveNavigationIndex,
      setScope,
      setValue,
      stagedFiles.length,
      value,
      composing,
      routeTopicId,
      routeTopicDismissed,
      compose,
    ],
  );

  // Drag-and-drop file ingestion
  const dragCounter = useRef(0);
  useEffect(() => {
    const onDragEnter = (e: DragEvent) => {
      e.preventDefault();
      dragCounter.current++;
      if (e.dataTransfer?.types.includes("Files")) {
        setIsDragging(true);
      }
    };
    const onDragLeave = (e: DragEvent) => {
      e.preventDefault();
      dragCounter.current--;
      if (dragCounter.current <= 0) {
        dragCounter.current = 0;
        setIsDragging(false);
      }
    };
    const onDragOver = (e: DragEvent) => {
      e.preventDefault();
    };
    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      dragCounter.current = 0;
      setIsDragging(false);
      const droppedFiles = e.dataTransfer?.files;
      if (!droppedFiles || droppedFiles.length === 0) return;

      // Hand off to the shared `processDroppedFiles` utility so the
      // compose modal (plan 20 P2-4) can reuse the same filter +
      // read pipeline. Bar-side wraps the result with `addStagedFiles`
      // (which resets bar state); compose-side will skip that reset
      // and feed the items into the draft.
      void processDroppedFiles(droppedFiles).then((result) => {
        if (result.rejectedAsFolders) {
          setBarNotification({
            type: "error",
            message: t.toast.dropFilesNotFolders,
            autoDismissMs: 4000,
            onDismiss: () => setBarNotification(null),
          });
          return;
        }
        if (result.items.length > 0) {
          addStagedFiles(result.items);
        }
      });
    };
    window.addEventListener("dragenter", onDragEnter);
    window.addEventListener("dragleave", onDragLeave);
    window.addEventListener("dragover", onDragOver);
    window.addEventListener("drop", onDrop);
    return () => {
      window.removeEventListener("dragenter", onDragEnter);
      window.removeEventListener("dragleave", onDragLeave);
      window.removeEventListener("dragover", onDragOver);
      window.removeEventListener("drop", onDrop);
    };
  }, [
    addStagedFiles,
    setBarNotification,
    setIsDragging,
    t.toast.dropFilesNotFolders,
  ]);

  const openBar = useCallback(() => {
    setBarOverlayOpen(true);
    // Hand focus off from whatever owned it (e.g. the chat composer
    // on /brain when ⌘K summons the overlay). The bar's input is
    // focused after a 50ms paint window — without this synchronous
    // blur the prior owner keeps caret + keystrokes during that
    // window, so the first character the user types lands in the
    // wrong field. Blur is a no-op when document.activeElement is
    // the body, so this is safe on every entry path.
    if (typeof document !== "undefined") {
      const active = document.activeElement;
      if (active instanceof HTMLElement && active !== document.body) {
        active.blur();
      }
    }
    // Promote mobile state to "mirror" directly on mobile so the
    // MobileBarSurface (the input echo) renders alongside the bar's
    // input. Previously the promotion happened only via the input's
    // onFocus handler (openMobileComposeActive), which silently
    // failed if inputRef.current wasn't mounted yet at the time
    // openBar fired (FAB tap on a route where the bar wasn't
    // previously rendered). Result: user saw mirror without input
    // (or input without mirror), couldn't edit. Plan 26 follow-up.
    if (isMobile && mobileComposeState !== "fullscreen") {
      setMobileComposeState("mirror");
    }
    setTimeout(() => {
      const el = inputRef.current;
      if (el) {
        el.focus();
        el.selectionStart = el.selectionEnd = el.value.length;
      }
    }, 50);
  }, [setBarOverlayOpen, isMobile, mobileComposeState, setMobileComposeState]);

  // Hide bar — preserves state (Raycast model). Cmd+K re-shows.
  const hideBar = useCallback(() => {
    setBarOverlayOpen(false);
  }, [setBarOverlayOpen]);

  // Close bar — hides AND resets all state.
  const closeBar = useCallback(() => {
    setBarOverlayOpen(false);
    reset();
  }, [reset, setBarOverlayOpen]);

  const openMobileCompose = useCallback(() => {
    if (!isMobile) return;
    setMobileComposeState("fullscreen");
  }, [isMobile, setMobileComposeState]);

  const closeMobileCompose = useCallback(() => {
    setMobileComposeState(
      mobileComposeState === "fullscreen" ? "mirror" : "docked",
    );
  }, [mobileComposeState, setMobileComposeState]);

  const openMobileComposeActive = useCallback(() => {
    if (!isMobile) return;
    setMobileComposeState(
      mobileComposeState === "fullscreen" ? "fullscreen" : "mirror",
    );
  }, [isMobile, mobileComposeState, setMobileComposeState]);

  const closeMobileComposeActive = useCallback(() => {
    setMobileComposeState("docked");
  }, [setMobileComposeState]);

  const setMobileComposeActive = useCallback(
    (open: boolean) => {
      setMobileComposeState(
        !open
          ? "docked"
          : mobileComposeState === "fullscreen"
            ? "fullscreen"
            : "mirror",
      );
    },
    [mobileComposeState, setMobileComposeState],
  );

  const setMobileComposeOpen = useCallback(
    (open: boolean) => {
      setMobileComposeState(
        open
          ? "fullscreen"
          : mobileComposeState === "fullscreen"
            ? "mirror"
            : "docked",
      );
    },
    [mobileComposeState, setMobileComposeState],
  );

  // Check if bar has any active state worth preserving (P1-T3)
  const barHasState = useCallback(() => {
    return !!(
      value ||
      recallQuery ||
      askAnswer ||
      aiLoading ||
      phase !== "input" ||
      stagedFiles.length > 0 ||
      rememberState !== "idle" ||
      pushing ||
      scope.kind !== "recall" ||
      isBarExpanded ||
      selectedMemory ||
      detectedUrl ||
      mobileComposeState !== "docked"
    );
  }, [
    value,
    recallQuery,
    askAnswer,
    aiLoading,
    phase,
    stagedFiles.length,
    rememberState,
    pushing,
    scope,
    isBarExpanded,
    selectedMemory,
    detectedUrl,
    mobileComposeState,
  ]);

  const peelBack = useCallback(() => {
    const step = getPeelBackStep({
      phase,
      value,
      recallQuery,
      rememberActive: rememberState !== "idle" || pushing,
      selectedMemory: Boolean(selectedMemory),
      mobileComposeState,
      isBarExpanded,
      scope,
      onTopicRoute: isTopicRoute(pathname),
      barOverlayOpen,
      onBarPage: view !== "none",
      barHasPreservedState: barHasState(),
    });

    switch (step) {
      case "restore-query":
        setValue(recallQuery);
        inputRef.current?.focus();
        return;
      case "reset-phase":
        // Preserve one explicit "clear input" Esc step after recall-result dismiss.
        // If input is currently empty but recallQuery exists, repopulate it now,
        // then clear recall state; next Esc will clear-value.
        if (!value && recallQuery) {
          setValue(recallQuery);
        }
        setPhase("input");
        setRecallQuery("");
        setAskAnswer("");
        setAiLoading(false);
        setAiStreaming(false);
        clearRememberFeedback();
        inputRef.current?.focus();
        return;
      case "reset-remember":
        clearRememberFeedback();
        inputRef.current?.focus();
        return;
      case "clear-selected-memory":
        setSelectedMemory(null);
        setMemorySearch("");
        inputRef.current?.focus();
        return;
      case "clear-value":
        setValue("");
        clearRememberFeedback();
        return;
      case "close-mobile-compose":
        setMobileComposeActive(false);
        return;
      case "collapse-expanded":
        setIsBarExpanded(false);
        inputRef.current?.focus();
        return;
      case "clear-scope":
        // Route-aware: on topic pages, navigate back to memories overview.
        // The destination shape mirrors the current route. Topic routes
        // always carry a slug in the path, so the pathname-derived slug
        // wins; we still fall back to the active hub for defensive parity
        // with goMemory below in case a hub-less topic route is ever
        // introduced (would silently route to /memories → personal hub
        // otherwise).
        if (isTopicRoute(pathname)) {
          const pathSlug = getHubSlugForPath(pathname);
          const activeSlug =
            activeHubEntry && user
              ? hubRouteSlug(activeHubEntry.hub, user.id)
              : null;
          const memoriesPath = buildMemoriesPath(pathSlug ?? activeSlug);
          requestSurfaceTransitionForNavigation(pathname, memoriesPath);
          router.push(memoriesPath);
        }
        clearScope();
        inputRef.current?.focus();
        return;
      case "hide-overlay":
        hideBar();
        return;
      case "suppress-panel":
        dispatch({ type: "setBarPanelSuppressed", value: true });
        inputRef.current?.blur();
        return;
      case "blur":
        inputRef.current?.blur();
        return;
      case "noop":
      default:
        return;
    }
  }, [
    activeHubEntry,
    barHasState,
    barOverlayOpen,
    clearRememberFeedback,
    clearScope,
    hideBar,
    isBarExpanded,
    mobileComposeState,
    pathname,
    phase,
    pushing,
    recallQuery,
    rememberState,
    router,
    scope,
    selectedMemory,
    setAiLoading,
    setAiStreaming,
    setAskAnswer,
    setIsBarExpanded,
    setMemorySearch,
    setMobileComposeActive,
    setPhase,
    setRecallQuery,
    setSelectedMemory,
    user,
    setValue,
    view,
    value,
  ]);

  // Derived: what the next peelBack() call would do (for chevron visibility)
  const nextPeelBackStep = useMemo(
    () =>
      getPeelBackStep({
        phase,
        value,
        recallQuery,
        rememberActive: rememberState !== "idle" || pushing,
        selectedMemory: Boolean(selectedMemory),
        mobileComposeState,
        isBarExpanded,
        scope,
        onTopicRoute: isTopicRoute(pathname),
        barOverlayOpen,
        onBarPage: view !== "none",
        barHasPreservedState: false, // conservative: don't show chevron just for suppression
      }),
    [
      barOverlayOpen,
      isBarExpanded,
      mobileComposeState,
      pathname,
      phase,
      pushing,
      recallQuery,
      rememberState,
      scope,
      selectedMemory,
      value,
      view,
    ],
  );

  // Global keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Cmd+K / Ctrl+K — toggle bar visibility (Raycast model).
      // After plan 26's "FAB-as-rest-state" shift, the bar is inline
      // ONLY on the brain page; every other route uses the FAB
      // (ScanRestButton) as the rest state and surfaces the bar as a
      // centered overlay on demand. Cmd+K is the keyboard equivalent
      // of tapping the FAB — it toggles the overlay open/closed.
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        // Chat surface (/brain*): the bar isn't inline — Cmd+K is the
        // ONLY way to summon it. Treat like every other FAB-state
        // route and toggle the centered overlay. The chat composer
        // is blurred on overlay-open by `<ChatBrainView>` itself
        // (see use-bar-overlay-blur effect) so focus lands cleanly
        // on the bar input.
        if (isChatSurfaceRoute(pathname)) {
          if (barOverlayOpen) {
            hideBar();
          } else {
            openBar();
          }
          return;
        }
        if (view === "brain") {
          // Brain page: the bar IS the page's primary surface (centered
          // capture). Cmd+K cycles between focused-empty / suppress-with-
          // state / expanded states without ever fully hiding the bar.
          if (barPanelSuppressed) {
            dispatch({ type: "setBarPanelSuppressed", value: false });
            requestAnimationFrame(() => {
              const el = inputRef.current;
              if (el) {
                el.focus();
                el.selectionStart = el.selectionEnd = el.value.length;
              }
            });
          } else if (barHasState()) {
            dispatch({ type: "setBarPanelSuppressed", value: true });
            inputRef.current?.blur();
          } else {
            const isOpen =
              barFocused || isBarExpanded || mobileComposeState !== "docked";
            if (isOpen) {
              inputRef.current?.blur();
              setIsBarExpanded(false);
              if (mobileComposeState !== "docked") {
                setMobileComposeState("docked");
              }
              clearNavigationSelection();
            } else {
              const el = inputRef.current;
              if (el) {
                el.focus();
                el.selectionStart = el.selectionEnd = el.value.length;
              }
            }
          }
        } else if (barOverlayOpen) {
          // FAB-state route with bar overlay open: Cmd+K hides it
          // (preserves any typed state — Cmd+K again brings it back).
          hideBar();
        } else {
          // FAB-state route with overlay closed: Cmd+K opens it,
          // mirroring the FAB tap.
          openBar();
        }
        return;
      }

      // Cmd+J / Ctrl+J — prime remember shortcut without changing copy or mode
      if ((e.metaKey || e.ctrlKey) && e.key === "j") {
        e.preventDefault();
        forceRememberRef.current = true;
        inputRef.current?.focus();
        return;
      }

      // Cmd+M / Ctrl+M — toggle brain/memory view.
      // Brain → Memory destination: v1 path /memories (shell-v2's
      // ShellRouter redirects this to /h/<active-hub-slug>/memories
      // when the v2 cookie is set, so we don't branch here).
      if ((e.metaKey || e.ctrlKey) && e.key === "m") {
        e.preventDefault();
        if (view === "brain") {
          requestSurfaceTransitionForNavigation(pathname, "/memories");
          router.push("/memories");
        } else if (view === "memory") {
          // Direct to /brain — /home is the neutral entry resolver now
          // and is NOT a brain surface: routing through it would drop
          // the memory→brain morph and add a resolver hop.
          requestSurfaceTransitionForNavigation(pathname, "/brain");
          router.push("/brain");
        }
        return;
      }

      // Escape — layered dismiss chain (Raycast model)
      if (e.key === "Escape") {
        e.preventDefault();
        peelBack();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [
    view,
    barOverlayOpen,
    barPanelSuppressed,
    barHasState,
    openBar,
    hideBar,
    peelBack,
    barFocused,
    isBarExpanded,
    mobileComposeState,
    pathname,
    router,
    setIsBarExpanded,
    setMobileComposeState,
    clearNavigationSelection,
  ]);

  const goMemory = useCallback(() => {
    inputRef.current?.blur();
    resetForNavigation();
    // Resolve the destination hub. Order:
    //   1. Pathname slug (if we're already on a v2 hub-scoped surface,
    //      stay in that hub's memories overview).
    //   2. Active hub from auth (covers /brain, /agents, /pulse — non-
    //      hub-scoped routes that have no slug in the path). Without
    //      this, callers from /brain landed on /memories which middleware
    //      routes to /h/personal/memories regardless of which hub the
    //      user was actually viewing.
    //   3. v1 fallback for the rare case neither resolves.
    const pathSlug = getHubSlugForPath(pathname);
    const activeSlug =
      activeHubEntry && user ? hubRouteSlug(activeHubEntry.hub, user.id) : null;
    const memoriesPath = buildMemoriesPath(pathSlug ?? activeSlug);
    requestSurfaceTransitionForNavigation(pathname, memoriesPath);
    router.push(memoriesPath);
  }, [pathname, resetForNavigation, router, activeHubEntry, user]);

  const goBack = useCallback(() => {
    inputRef.current?.blur();
    resetForNavigation();
    // Direct to /brain (see Cmd+M above) — /home is the neutral entry
    // resolver, not a brain surface.
    requestSurfaceTransitionForNavigation(pathname, "/brain");
    router.push("/brain");
  }, [pathname, resetForNavigation, router]);

  return (
    <BarContext.Provider
      value={{
        view,
        isMobile,
        value,
        setValue,
        barMode,
        pushing,
        sendKind,
        scope,
        setScope,
        clearScope,
        inputRef,
        barFocused,
        setBarFocused,
        isBarExpanded,
        setIsBarExpanded,
        phase,
        setPhase,
        recallQuery,
        recallResult,
        recallFetching,
        recallIsError,
        recallErrorMessage: recallError?.message,
        retryRecall: () => {
          void retryRecall();
        },
        askAnswer,
        aiLoading,
        aiStreaming,
        canUseAI,
        triggerAI,
        setAskAnswer,
        setAiLoading,
        cancelAI,
        activeNavigationIndex,
        setNavigationItems,
        clearNavigationSelection,
        rememberState,
        rememberedTitle,
        retryRemember,
        undoRemember,
        barNotification,
        setBarNotification,
        dreamNotification,
        setDreamNotification,
        statusNotification,
        setStatusNotification,
        activeNotification,
        stagedFiles,
        addStagedFiles,
        removeStagedFile,
        clearStagedFiles,
        allFilesReady,
        detectedUrl,
        clearDetectedUrl,
        selectedMemory,
        setSelectedMemory,
        memorySearch,
        setMemorySearch,
        isDragging,
        setIsDragging,
        createMemory,
        handleChange,
        handleSubmit,
        handleRememberSubmit,
        commitCommandScope,
        handleKeyDown,
        setComposing,
        dismissRouteTopic,
        routeTopicDismissed,
        peelForNavigation,
        reset,
        uploadFiles,
        goMemory,
        goBack,
        barOverlayOpen,
        openBar,
        hideBar,
        closeBar,
        mobileComposeActive,
        openMobileComposeActive,
        closeMobileComposeActive,
        setMobileComposeActive,
        mobileComposeOpen,
        openMobileCompose,
        closeMobileCompose,
        setMobileComposeOpen,
        mobileComposeState,
        peelBack,
        nextPeelBackStep,
        interaction,
        surface,
        barHasState,
        barPanelSuppressed,
        setBarPanelSuppressed,
      }}
    >
      {children}
    </BarContext.Provider>
  );
}
