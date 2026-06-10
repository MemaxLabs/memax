import posthog from "posthog-js";
import { API_URL } from "@/lib/urls";

const POSTHOG_KEY = process.env.NEXT_PUBLIC_POSTHOG_KEY;
const POSTHOG_HOST =
  process.env.NEXT_PUBLIC_POSTHOG_HOST || "https://us.i.posthog.com";

// Derive environment from the API URL
const ENVIRONMENT = (() => {
  if (API_URL.includes("staging")) return "staging";
  if (API_URL.includes("localhost")) return "dev";
  return "production";
})();

let initialized = false;

/**
 * Initialize PostHog. Idempotent — repeated calls are no-ops.
 *
 * `extras` lets the caller register additional super properties
 * synchronously before the FIRST captured event. Callers that know the
 * shell version at app boot (Providers, reading the `memax_shell`
 * cookie) pass `{ shellVersion }` here so the initial auto-captured
 * `$pageview` is tagged. Without this, the very first event in a
 * session would be missing `shellVersion` because posthog-js fires
 * the initial pageview synchronously inside `init()`, before any
 * `register()` call returns. Codex 5.5 caught this on the rollout-
 * gate query.
 */
export function initPostHog(extras?: Record<string, any>) {
  if (typeof window === "undefined") return;
  if (initialized) return;
  if (!POSTHOG_KEY) return;

  posthog.init(POSTHOG_KEY, {
    api_host: POSTHOG_HOST,
    // Defer the first pageview so we can register super properties
    // first; we manually capture immediately after register below.
    capture_pageview: false,
    capture_pageleave: true,
    persistence: "localStorage",
  });

  // Register super properties — sent with every event automatically.
  posthog.register({
    environment: ENVIRONMENT,
    app: "web",
    ...(extras ?? {}),
  });

  // Manual initial pageview, now that super properties are attached.
  posthog.capture("$pageview");

  initialized = true;
}

export function identifyUser(userId: string, properties?: Record<string, any>) {
  if (!POSTHOG_KEY) return;
  posthog.identify(userId, properties);
}

/**
 * Register additional super properties post-init. Used by AppShellClient
 * to attach the resolved shell version (`v1` | `v2`) so every captured
 * event — including auto-captured `$pageview` and `$autocapture` — is
 * tagged with which shell rendered it. That makes the v2 soak query a
 * one-liner: error rate WHERE shellVersion='v2' / WHERE shellVersion='v1'.
 *
 * `register` overwrites an existing super property of the same name, so
 * this is safe to call repeatedly when the shell version flips at
 * runtime (kill-switch cookie change → reload → re-register).
 *
 * Calls `initPostHog()` first (idempotent) to handle the React effect-
 * ordering race: child effects fire before parent effects, so
 * AppShellClient's super-property effect can run BEFORE Providers'
 * `initPostHog()` effect. Without the explicit init here,
 * `posthog.register()` becomes a no-op against an uninitialized client
 * and the shell version never makes it onto events. Codex 5.5 caught
 * this.
 */
export function registerSuperProperties(properties: Record<string, any>) {
  if (!POSTHOG_KEY) return;
  initPostHog();
  posthog.register(properties);
}

export function trackEvent(event: string, properties?: Record<string, any>) {
  if (!POSTHOG_KEY) return;
  posthog.capture(event, properties);
}

export function resetUser() {
  if (!POSTHOG_KEY) return;
  posthog.reset();
}
