"use client";

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useCallback,
} from "react";
import { isImpersonating, stopImpersonating } from "@/lib/impersonation";
import { forgetLegacyTokens } from "@/lib/legacy-tokens";
import { clearSessionPresence } from "@/lib/session-presence";
import { queryClient } from "@/lib/query-client";
import { hubListQueryKey } from "@/hooks/use-hubs";
import type { AuthProviderName, WebUi } from "memax-sdk";

export interface User {
  id: string;
  name: string;
  display_name?: string;
  email: string;
  avatar_url: string;
  plan: string; // legacy — use personal_plan_id for scoped resolution
  personal_plan_id: string; // scoped personal plan (personal_free, personal_pro, etc.)
  can_create_hub?: boolean;
  dev_access?: boolean;
  admin_role?: string; // "super_admin" when user is admin
}

export interface Hub {
  id: string;
  name: string;
  icon?: string;
  accent?: "violet" | "blue" | "green" | "amber" | "rose" | "slate";
  slug: string;
  hub_type: string;
  owner_id: string;
  allow_contributor_topics?: boolean;
  allow_contributor_dreams?: boolean;
  contributor_delete_policy?: "none" | "own" | "any";
  header_aurora_mode?: "none" | "signature" | "time";
  /**
   * Per-hub dream-phase overrides. Populated by the auth-me
   * response's hubs list (ListUserHubs includes the settings
   * column) so the Intelligence tab reads from here rather than
   * issuing a second hub-detail fetch. Mirrors memax-sdk's
   * HubSettings shape.
   */
  settings?: {
    dreams_enabled?: boolean;
    dreams_merge_enabled?: boolean;
    dreams_archive_enabled?: boolean;
    dreams_organize_enabled?: boolean;
    dreams_restructure_enabled?: boolean;
  };
}

export interface HubWithRole {
  hub: Hub;
  role: string;
  memory_count: number;
}

import type { Usage } from "memax-sdk";
export type { Usage };

/**
 * What the signed-in session is, as the web app's server read it from the
 * session's access token (the page never sees the token itself).
 * `surface` is "web" for a sign-in on the web app: with /api/proxy's
 * signature, that is what lets a person keep as a person on the web
 * (human_web, D15). Sessions from before migration 030 have none.
 */
export interface SessionInfo {
  surface: string | null;
  impersonating: boolean;
}

/**
 * Hub model (Slack/Notion pattern):
 *   activeHubId = the hub you're in. Controls both what you SEE and where you PUSH.
 *   Recall crosses all hubs regardless (server VisibilityScope).
 *   No "All" scope — you're always in a specific hub.
 */
interface AuthState {
  user: User | null;
  hubs: HubWithRole[];
  activeHubId: string;
  usage: Usage | null; // basic usage from auth/me; enriched usage (with limits) via useUsage()
  connectedProviders: AuthProviderName[];
  /** The signed-in session, or null when signed out. */
  session: SessionInfo | null;
  /**
   * The web UI this person sees (the V2 UI flag, decided by the API), or
   * null when signed out or not said.
   */
  ui: WebUi | null;
  loading: boolean;
  /**
   * Picks up the session the web app's server just stored (after
   * /api/auth/exchange answered): loads the profile; false when there is
   * no session after all.
   */
  completeLogin: () => Promise<boolean>;
  /**
   * Start an OAuth login.
   *
   * The first argument has two shapes:
   * - Path (e.g. `"/invite/abc"`) — stashed as `memax_return_to` and
   *   used by the callback page to route after successful token exchange.
   * - Full URL (e.g. `"${origin}/auth/callback?invite=TOKEN"`) — used
   *   directly as the OAuth `redirect_uri` so the backend can extract
   *   waitlist invite tokens via `state.ClientRedirect`. No
   *   `memax_return_to` is written in this shape, so post-login
   *   navigation falls through to `/home`.
   */
  login: (returnToOrCallback?: string, provider?: AuthProviderName) => void;
  logout: () => void;
  switchHub: (hubId: string) => void;
  refreshProfile: () => Promise<void>;
}

const AuthContext = createContext<AuthState>({
  user: null,
  hubs: [],
  activeHubId: "",
  usage: null,
  connectedProviders: [],
  session: null,
  ui: null,
  loading: true,
  completeLogin: async () => false,
  login: () => {},
  logout: () => {},
  switchHub: () => {},
  refreshProfile: async () => {},
});

const ACTIVE_HUB_KEY = "memax_active_hub_id";

interface MeResponse {
  user: User;
  hubs: HubWithRole[];
  usage: Usage | null;
  dev_access: boolean;
  admin_role?: string;
  connected_providers?: AuthProviderName[];
  session?: SessionInfo;
  ui?: WebUi;
}

/** What loading the profile came to. */
type ProfileResult = "ok" | "signed_out" | "impersonation_expired" | "error";

function chooseActiveHubID(hubs: HubWithRole[], preferred?: string): string {
  if (preferred && hubs.some((entry) => entry.hub.id === preferred)) {
    return preferred;
  }
  const personal = hubs.find((entry) => entry.hub.hub_type === "personal");
  if (personal) {
    return personal.hub.id;
  }
  return hubs[0]?.hub.id ?? "";
}

/** Signs the session out on the server and clears its cookies. */
async function signOutOnServer(): Promise<void> {
  try {
    await fetch("/api/auth/logout", { method: "POST" });
  } catch {
    // Offline: the cookies stay until the next request clears them.
  }
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [hubs, setHubs] = useState<HubWithRole[]>([]);
  const [activeHubId, setActiveHubId] = useState("");
  const [usage, setUsage] = useState<Usage | null>(null);
  const [connectedProviders, setConnectedProviders] = useState<
    AuthProviderName[]
  >([]);
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [ui, setUi] = useState<WebUi | null>(null);
  const [loading, setLoading] = useState(true);
  const sessionVersionRef = useRef(0);

  const clearSession = useCallback(() => {
    sessionVersionRef.current += 1;
    localStorage.removeItem(ACTIVE_HUB_KEY);
    // Retired landing-surface hint (home now always lands on
    // memories); still evicted here so browsers that wrote it under
    // the old scheme don't carry the stale key forever.
    localStorage.removeItem("memax_landing_surface");
    forgetLegacyTokens();
    clearSessionPresence();
    setUser(null);
    setHubs([]);
    setActiveHubId("");
    setUsage(null);
    setConnectedProviders([]);
    setSession(null);
    setUi(null);
    try {
      import("@/lib/posthog").then(({ resetUser }) => resetUser());
    } catch {}
  }, []);

  // handleAuthFailure distinguishes impersonation expiry from real auth
  // failure. When impersonating and the short-lived token expires, the
  // server restores the original dev session and the page reloads into it,
  // rather than logging the dev out.
  const handleAuthFailure = useCallback(() => {
    if (isImpersonating()) {
      void stopImpersonating();
      return;
    }
    clearSession();
    void signOutOnServer();
  }, [clearSession]);

  const fetchUser = useCallback(async (): Promise<ProfileResult> => {
    const requestID = ++sessionVersionRef.current;
    try {
      // The session's cookies go with it; the web app's server attaches
      // the token and refreshes it when it needs to.
      const res = await fetch("/api/auth/me", { cache: "no-store" });
      if (res.status === 401) {
        const payload = (await res.json().catch(() => null)) as {
          error?: { code?: string };
        } | null;
        return payload?.error?.code === "impersonation_expired"
          ? "impersonation_expired"
          : "signed_out";
      }
      if (!res.ok) {
        return "error";
      }
      const payload = (await res.json()) as {
        data?: MeResponse | User;
      };
      const data = payload.data;
      if (!data) {
        return "error";
      }
      if (requestID !== sessionVersionRef.current) {
        return "error";
      }
      if ("user" in data) {
        setUser({
          ...data.user,
          dev_access: data.dev_access,
          admin_role: data.admin_role,
        });
        setSession(data.session ?? { surface: null, impersonating: false });
        setUi(data.ui ?? null);
        const hubsData = data.hubs ?? [];
        setHubs(hubsData);
        // Seed TanStack Query cache so useHubs() has data instantly.
        // Mutations invalidate this key, keeping counts fresh.
        queryClient.setQueryData(hubListQueryKey, hubsData);
        const nextActiveHubID = chooseActiveHubID(
          hubsData,
          localStorage.getItem(ACTIVE_HUB_KEY) ?? undefined,
        );
        setActiveHubId(nextActiveHubID);
        if (nextActiveHubID) {
          localStorage.setItem(ACTIVE_HUB_KEY, nextActiveHubID);
        } else {
          localStorage.removeItem(ACTIVE_HUB_KEY);
        }
        const usageData = data.usage ?? null;
        setUsage(usageData);
        setConnectedProviders(data.connected_providers ?? []);
        // usageData from /auth/me is the bare Usage shape (no plan
        // limits — see field doc on line 64). Seeding usageQueryKey
        // with it would poison useUsage() readers that need the
        // enriched UsageWithLimits: React Query sees a fresh cache
        // entry and skips the SDK fetch, so consumers like the
        // bar's canUseAI gate never observe ask_limit. Leave the
        // cache empty and let useUsage() do its own fetch through
        // the SDK's settings.usage().
        import("@/lib/posthog")
          .then(({ identifyUser }) => {
            identifyUser(data.user.id, {
              name: data.user.name,
              email: data.user.email,
              plan: data.user.personal_plan_id || data.user.plan,
            });
          })
          .catch(() => {
            // PostHog not configured
          });
      } else {
        setUser(data);
        setSession({ surface: null, impersonating: false });
        import("@/lib/posthog")
          .then(({ identifyUser }) => {
            identifyUser(data.id, { name: data.name, email: data.email });
          })
          .catch(() => {});
      }
      return "ok";
    } catch {
      // network error
    }
    return "error";
  }, []);

  const completeLogin = useCallback(async () => {
    setLoading(true);
    try {
      const result = await fetchUser();
      if (result === "ok") return true;
      if (result !== "error") handleAuthFailure();
      return false;
    } finally {
      setLoading(false);
    }
  }, [handleAuthFailure, fetchUser]);

  useEffect(() => {
    const init = async () => {
      // Tokens an older version kept in localStorage are deleted unread.
      forgetLegacyTokens();
      const result = await fetchUser();
      if (result === "impersonation_expired") {
        handleAuthFailure();
        return;
      }
      if (result === "signed_out") {
        // The server cleared the session's cookies (and the presence
        // marker the middleware's fast path trusts) with its answer.
        clearSession();
      } else if (result === "error") {
        // The API is unreachable: the session's cookies stay, but the
        // presence marker goes, so the middleware's fast path doesn't
        // bounce /login → /home while this shell bounces /home → /login.
        // The next successful /api/auth/me plants it again.
        clearSessionPresence();
      }
      setLoading(false);
    };
    init();
  }, [clearSession, handleAuthFailure, fetchUser]);

  useEffect(() => {
    const onAuthExpired = () => {
      handleAuthFailure();
      setLoading(false);
    };

    window.addEventListener("memax:auth-expired", onAuthExpired);
    return () => {
      window.removeEventListener("memax:auth-expired", onAuthExpired);
    };
  }, [handleAuthFailure]);

  const login = useCallback(
    (returnToOrCallback?: string, provider: AuthProviderName = "github") => {
      // Two argument shapes, both backwards-compatible:
      //   - path (e.g. "/invite/abc")          → stored as
      //     memax_return_to for post-login routing; callback URL
      //     stays the default.
      //   - full URL (e.g. ".../auth/callback?invite=TOKEN") →
      //     used as the OAuth callback URL directly. The only
      //     current caller is the register page, which needs the
      //     invite token to flow THROUGH the OAuth redirect so
      //     the server's extractInviteToken() can consume it —
      //     storing the token in localStorage alone doesn't
      //     help because the server never reads localStorage.
      //
      // Without this propagation, invited waitlist users were
      // treated as not-yet-registered by the invite-only gate
      // and bounced to /waitlist immediately after GitHub login,
      // forcing them to re-join the waitlist they had already
      // been approved from.
      const isFullURL = /^https?:\/\//i.test(returnToOrCallback ?? "");
      const callbackUrl =
        isFullURL && returnToOrCallback
          ? returnToOrCallback
          : `${window.location.origin}/auth/callback`;
      if (returnToOrCallback && !isFullURL) {
        localStorage.setItem("memax_return_to", returnToOrCallback);
      }
      window.location.href = `/api/auth/provider/${provider}?redirect_uri=${encodeURIComponent(callbackUrl)}`;
    },
    [],
  );

  const logout = useCallback(() => {
    // The server revokes the session (and a dev's own session waiting
    // behind an impersonation) and clears every session cookie.
    clearSession();
    void signOutOnServer();
  }, [clearSession]);

  const switchHub = useCallback((hubId: string) => {
    setActiveHubId(hubId);
    localStorage.setItem(ACTIVE_HUB_KEY, hubId);
  }, []);

  const refreshProfile = useCallback(async () => {
    await fetchUser();
  }, [fetchUser]);

  // Stable context value so consumers that depend on the whole auth
  // object (vs. just user?.id) don't re-run effects on every
  // AuthProvider render. Flagged during the SSE pre-promote audit as
  // a latent re-render hazard — not an active bug given current
  // consumers (the SSE bridge depends on user?.id, which is a
  // primitive), but strictly better hygiene. All closures below are
  // useCallback-memoized; scalar state (user/hubs/activeHubId/usage/
  // connectedProviders/session/loading) is the only remaining source
  // of identity change.
  const contextValue = useMemo(
    () => ({
      user,
      hubs,
      activeHubId,
      usage,
      connectedProviders,
      session,
      ui,
      loading,
      completeLogin,
      login,
      logout,
      switchHub,
      refreshProfile,
    }),
    [
      user,
      hubs,
      activeHubId,
      usage,
      connectedProviders,
      session,
      ui,
      loading,
      completeLogin,
      login,
      logout,
      switchHub,
      refreshProfile,
    ],
  );

  return (
    <AuthContext.Provider value={contextValue}>{children}</AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}

/**
 * Derived hub state. You're always in a specific hub (Slack/Notion model).
 * Recall crosses all hubs regardless (server-side VisibilityScope).
 */
export function useActiveHub() {
  const { hubs, activeHubId } = useAuth();
  const activeHub = hubs.find((h) => h.hub.id === activeHubId);
  const isTeamHub = activeHub?.hub.hub_type === "team";
  return {
    activeHub,
    isTeamHub,
    /** Pass to useMemories/useTopics — always a real hub ID. */
    hubFilter: activeHubId,
  };
}
