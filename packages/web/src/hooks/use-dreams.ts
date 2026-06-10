"use client";

import { useQuery, useMutation } from "@tanstack/react-query";
import type { DreamReport } from "memax-sdk";
import { MemaxError } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { useDevMode } from "@/hooks/use-dev-mode";

/**
 * Copy for the mapDreamTriggerError helper. Keeps the i18n lookup
 * in the hook (where `t` is in scope) while leaving the mapping
 * logic pure + unit-testable.
 */
export interface DreamTriggerErrorCopy {
  hubFrozen: string;
  forbidden: string;
}

/**
 * mapDreamTriggerError — pure error → business-code copy mapping.
 * Returns `undefined` when the error is not a recognized business
 * code so the query-client's precedence chain (query-client.ts:88
 * MutationToastMeta) falls through to the status-class classifier.
 * This gives us rate-limit / offline / 5xx copy for free instead
 * of a static "Couldn't start organizing" fallback.
 *
 * Server-known codes (handler/dreams.go): hub_frozen (409),
 * forbidden (403). Everything else — rate-limit, offline, server
 * error, quota — is classified by status downstream.
 */
export function mapDreamTriggerError(
  err: Error,
  copy: DreamTriggerErrorCopy,
): string | undefined {
  if (err instanceof MemaxError) {
    if (err.code === "hub_frozen") return copy.hubFrozen;
    if (err.code === "forbidden") return copy.forbidden;
  }
  return undefined;
}

export function useDreamReport(hubIdOverride?: string) {
  const { mockDreams, mockDreaming } = useMockDreamFlags();
  const { activeHubId } = useAuth();
  const hubId = hubIdOverride ?? activeHubId;
  const mockKey = mockDreaming ? "mock-running" : mockDreams ? "mock" : "live";
  return useQuery<DreamReport>({
    queryKey: ["dreams", "report", hubId || "personal", mockKey],
    queryFn: () => {
      if (mockDreaming) {
        return import("@/lib/dev-mocks").then(
          (m) => m.MOCK_DREAM_REPORT_RUNNING,
        );
      }
      if (mockDreams) {
        return import("@/lib/dev-mocks").then((m) => m.MOCK_DREAM_REPORT);
      }
      return getMemaxClient().dreams.report(hubId || undefined);
    },
    // Live report refresh normally comes from the centralized SSE
    // bridge. When a run is actively `"running"`, also poll every
    // 20s as a safety net — onboarding's "memax is dreaming…" hint
    // and the dream tab's progress indicator both read this query,
    // and an SSE drop would otherwise leave them stuck on stale
    // "running" forever. Terminal status (completed/failed/etc) →
    // interval clears so we don't burn cycles. Window-focus refetch
    // covers tab-switch-back cases without changing the default.
    refetchInterval: (query) =>
      query.state.data?.run?.status === "running" ? 20_000 : false,
    staleTime: 30 * 1000,
  });
}

/**
 * Trigger a dream cycle for a hub.
 *
 * Error handling: the server returns typed error codes for
 * expected user-facing conditions. `hub_frozen` (409) fires when
 * the hub is over-limit past the grace window or the subscription
 * is cancelled/past_due — that's a product state, not a system
 * failure, so the toast copy calls it out explicitly. `forbidden`
 * (403) means the caller doesn't have `canTriggerDreams` on the
 * hub. Every other error falls through to the generic
 * "Couldn't start organizing" copy so we don't leak internal
 * surface into user-facing text.
 *
 * Uses `meta.errorMessage` as a function of the error so the
 * query-client's global toast path (lib/query-client.ts:43) picks
 * the right copy without every caller wiring it manually.
 */
export function useDreamTrigger(hubIdOverride?: string) {
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  const defaultHubId = hubIdOverride ?? activeHubId;
  return useMutation({
    meta: {
      // Success toast closes a subtle UX gap: between the trigger
      // POST returning 200 and the new dream_runs row surfacing
      // via SSE/poll, the button's disabled state briefly reverts
      // to "Run a dream now" — so a click with no visible state
      // change can look like it did nothing. The toast carries
      // the feedback.
      successMessage: t.toast.dreamTriggerStarted,
      // Function-form precedence (query-client.ts MutationToastMeta,
      // steps 1 → 2 → 4; step 3 is the static-string form and is skipped
      // when errorMessage is a function):
      // 1. Function wins first for business codes (hub_frozen → plan
      //    copy; forbidden → permission copy). Returns undefined for
      //    every other error so the classifier takes over.
      // 2. Classifier surfaces rate-limit / offline / 5xx / quota
      //    copy with the "trigger a dream" action noun.
      // 4. errorFallback — static last-resort for errors neither step
      //    matched. Closes the silent-no-toast hole Codex flagged: an
      //    unclassifiable MemaxError or plain Error while online now
      //    shows organizeFailed instead of nothing.
      errorMessage: (err: Error) =>
        mapDreamTriggerError(err, {
          hubFrozen: t.toast.dreamTriggerFrozen ?? t.toast.organizeFailed,
          forbidden: t.toast.dreamTriggerForbidden ?? t.toast.organizeFailed,
        }),
      errorFallback: t.toast.organizeFailed,
      errorAction: t.errors.action.triggerDream,
    },
    mutationFn: (hubId?: string) =>
      getMemaxClient().dreams.trigger((hubId ?? defaultHubId) || undefined),
    onSuccess: (_data, hubId) => {
      const scope = (hubId ?? defaultHubId) || "personal";
      queryClient.invalidateQueries({ queryKey: ["dreams", "report", scope] });
      queryClient.invalidateQueries({ queryKey: ["reviews", scope] });
      queryClient.invalidateQueries({ queryKey: ["hub-summary"] });
      queryClient.invalidateQueries({ queryKey: ["topics", scope] });
      queryClient.invalidateQueries({ queryKey: ["memories", scope] });
      queryClient.invalidateQueries({ queryKey: ["usage", scope] });
      // Bust the dream-quota snapshot so the indicator under "Dream
      // now" updates after a successful trigger. The hub key matches
      // dreamUsageQueryKey: hubId for hub triggers, "personal" for
      // no-hub-context.
      queryClient.invalidateQueries({
        queryKey: ["dream-usage", (hubId ?? defaultHubId) || "personal"],
      });
    },
  });
}

/** Reads mock dream flags from persisted settings-backed dev mode. */
function useMockDreamFlags(): {
  mockDreams: boolean;
  mockDreaming: boolean;
} {
  const { isDevUser, flags } = useDevMode();
  if (!isDevUser) return { mockDreams: false, mockDreaming: false };
  if (flags.mockDreaming) return { mockDreams: false, mockDreaming: true };
  if (flags.mockDreams) return { mockDreams: true, mockDreaming: false };
  return { mockDreams: false, mockDreaming: false };
}
