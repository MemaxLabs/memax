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
import { getPublicMemaxClient } from "@/lib/memax-client";
import { isImpersonating, restoreOriginalSession } from "@/lib/impersonation";
import { clearLandingSurfaceHint } from "@/lib/landing-surface";
import { queryClient } from "@/lib/query-client";
import { hubListQueryKey } from "@/hooks/use-hubs";
import type { AuthProviderName } from "memax-sdk";

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
  loading: boolean;
  completeLogin: (
    accessToken: string,
    refreshToken: string,
  ) => Promise<boolean>;
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
  loading: true,
  completeLogin: async () => false,
  login: () => {},
  logout: () => {},
  switchHub: () => {},
  refreshProfile: async () => {},
});

const TOKEN_KEY = "memax_access_token";
const REFRESH_KEY = "memax_refresh_token";
const ACTIVE_HUB_KEY = "memax_active_hub_id";

interface MeResponse {
  user: User;
  hubs: HubWithRole[];
  usage: Usage | null;
  dev_access: boolean;
  admin_role?: string;
  connected_providers?: AuthProviderName[];
}

interface AuthTokens {
  access_token?: string;
  refresh_token?: string;
}

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

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [hubs, setHubs] = useState<HubWithRole[]>([]);
  const [activeHubId, setActiveHubId] = useState("");
  const [usage, setUsage] = useState<Usage | null>(null);
  const [connectedProviders, setConnectedProviders] = useState<
    AuthProviderName[]
  >([]);
  const [loading, setLoading] = useState(true);
  const sessionVersionRef = useRef(0);

  const clearSession = useCallback(() => {
    sessionVersionRef.current += 1;
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(REFRESH_KEY);
    localStorage.removeItem(ACTIVE_HUB_KEY);
    clearLandingSurfaceHint();
    setUser(null);
    setHubs([]);
    setActiveHubId("");
    setUsage(null);
    setConnectedProviders([]);
    try {
      import("@/lib/posthog").then(({ resetUser }) => resetUser());
    } catch {}
  }, []);

  // handleAuthFailure distinguishes impersonation-token expiry from real auth
  // failure. When impersonating and the short-lived token expires, we restore
  // the original dev session and reload — rather than logging the dev out.
  const handleAuthFailure = useCallback(() => {
    if (isImpersonating() && restoreOriginalSession()) {
      window.location.reload();
      return;
    }
    clearSession();
  }, [clearSession]);

  const storeTokens = useCallback(
    (accessToken: string, refreshToken: string) => {
      localStorage.setItem(TOKEN_KEY, accessToken);
      localStorage.setItem(REFRESH_KEY, refreshToken);
    },
    [],
  );

  const fetchUser = useCallback(async (token: string) => {
    const requestID = ++sessionVersionRef.current;
    try {
      const res = await fetch("/api/auth/me", {
        headers: { Authorization: `Bearer ${token}` },
        cache: "no-store",
      });
      if (!res.ok) {
        return false;
      }
      const payload = (await res.json()) as {
        data?: MeResponse | User;
      };
      const data = payload.data;
      if (!data) {
        return false;
      }
      if (requestID !== sessionVersionRef.current) {
        return false;
      }
      if ("user" in data) {
        setUser({
          ...data.user,
          dev_access: data.dev_access,
          admin_role: data.admin_role,
        });
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
        import("@/lib/posthog")
          .then(({ identifyUser }) => {
            identifyUser(data.id, { name: data.name, email: data.email });
          })
          .catch(() => {});
      }
      return true;
    } catch {
      // network error
    }
    return false;
  }, []);

  const tryRefresh = useCallback(async () => {
    const refreshToken = localStorage.getItem(REFRESH_KEY);
    if (!refreshToken) return false;

    try {
      const res = await fetch("/api/auth/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      if (!res.ok) {
        return false;
      }
      const payload = (await res.json()) as {
        data?: AuthTokens;
      };
      const tokens = payload.data;
      if (!tokens?.access_token || !tokens.refresh_token) {
        return false;
      }
      storeTokens(tokens.access_token, tokens.refresh_token);
      return fetchUser(tokens.access_token);
    } catch {
      // refresh failed
    }
    return false;
  }, [fetchUser, storeTokens]);

  const completeLogin = useCallback(
    async (accessToken: string, refreshToken: string) => {
      setLoading(true);
      storeTokens(accessToken, refreshToken);

      try {
        const ok = await fetchUser(accessToken);
        if (ok) {
          return true;
        }

        const refreshed = await tryRefresh();
        if (!refreshed) {
          handleAuthFailure();
        }
        return refreshed;
      } finally {
        setLoading(false);
      }
    },
    [handleAuthFailure, fetchUser, storeTokens, tryRefresh],
  );

  useEffect(() => {
    const init = async () => {
      const token = localStorage.getItem(TOKEN_KEY);
      if (token) {
        const ok = await fetchUser(token);
        if (!ok) {
          const refreshed = await tryRefresh();
          if (!refreshed) {
            handleAuthFailure();
          }
        }
      }
      setLoading(false);
    };
    init();
  }, [handleAuthFailure, fetchUser, tryRefresh]);

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
    // Clear impersonation stash on explicit logout so stale dev tokens
    // can't be resurrected. This is intentionally NOT in clearSession(),
    // because clearSession also fires on token expiry — and during
    // impersonation expiry we want to restore the original session, not
    // destroy it.
    import("@/lib/impersonation").then(({ clearImpersonationState }) =>
      clearImpersonationState(),
    );
    clearSession();
  }, [clearSession]);

  const switchHub = useCallback((hubId: string) => {
    setActiveHubId(hubId);
    localStorage.setItem(ACTIVE_HUB_KEY, hubId);
  }, []);

  const refreshProfile = useCallback(async () => {
    const token = localStorage.getItem(TOKEN_KEY);
    if (token) {
      await fetchUser(token);
    }
  }, [fetchUser]);

  // Stable context value so consumers that depend on the whole auth
  // object (vs. just user?.id) don't re-run effects on every
  // AuthProvider render. Flagged during the SSE pre-promote audit as
  // a latent re-render hazard — not an active bug given current
  // consumers (the SSE bridge depends on user?.id, which is a
  // primitive), but strictly better hygiene. All closures below are
  // useCallback-memoized; scalar state (user/hubs/activeHubId/usage/
  // connectedProviders/loading) is the only remaining source of
  // identity change.
  const contextValue = useMemo(
    () => ({
      user,
      hubs,
      activeHubId,
      usage,
      connectedProviders,
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

export function getAccessToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}
