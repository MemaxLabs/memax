import { trackEvent } from "@/lib/posthog";

// The first session's funnel on the web (plan 25 §5.18). The phase gates
// are computed from receipts on the server (activation, first file), but
// receipts start once a person has a V2 space: someone who signs in and
// stops at FirstRun leaves none. These events show where people stop
// before then, in PostHog, beside the shell's other events.
//
// An event carries the step and, at most, counts. Never memory text, file
// paths, space names or slugs: FunnelCounts only takes numbers, so the
// type refuses anything else. Without NEXT_PUBLIC_POSTHOG_KEY nothing is
// sent.

export const FUNNEL_STEPS = [
  "signed_in",
  "device_approved",
  "first_run_reached",
  "first_import_seen",
  "cleanup_settled",
  "compile_done_reached",
] as const;

export type FunnelStep = (typeof FUNNEL_STEPS)[number];

/** Counts only (files, conflicts): never words. */
export type FunnelCounts = Readonly<Record<string, number>>;

/** Sends `onboarding.<step>`, with the step and counts as properties. */
export function trackFunnelStep(step: FunnelStep, counts?: FunnelCounts) {
  trackEvent(`onboarding.${step}`, { ...counts, step });
}
