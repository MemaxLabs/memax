/**
 * Pure helper that maps a dreams.usage() snapshot into the small
 * line of copy shown under the "Run a dream now" button.
 *
 * Why this lives outside the component:
 *
 *   - Branching logic (limit=0 disabled, limit=-1 unlimited,
 *     allowed=false at finite cap, finite under cap) is the kind
 *     that benefits from focused unit tests; a component test
 *     would have to render and the JSDOM cost isn't worth it.
 *   - Sharing: the same hint surface lands in hub-intelligence
 *     (per-hub) and may move to settings (personal) later — one
 *     module, one contract, one set of tests.
 *
 * Returns undefined to signal "render nothing":
 *
 *   - Usage not yet loaded: avoid flashing placeholder copy.
 *   - Unlimited tier: a "Unlimited dreams" line under every Pro+
 *     user's button is noise. The absence of a counter is the
 *     message; the user knows their plan.
 *
 * Reset date: API contract is `period_end` is the LAST day of the
 * billing period, so reset = period_end + 1 day. We format with
 * the user's locale (toLocaleDateString) so zh users see a
 * locale-appropriate date string without a separate i18n key.
 */
export interface DreamQuotaHint {
  text: string;
  tone: "info" | "warn";
}

export interface DreamQuotaHintCopy {
  used: string; // "{used} of {limit} dreams this month"
  unlimited: string; // unused today (kept for parity if we change tactics)
  exhausted: string; // "Out of dreams this month — resets {resetDate}"
  disabled: string; // "Dreams aren't included in this hub's plan"
}

interface DreamUsageShape {
  allowed: boolean;
  limit: number;
  used: number;
  period_end: string;
}

export function renderDreamQuotaHint(
  usage: DreamUsageShape | undefined,
  copy: DreamQuotaHintCopy,
  interpolate: (template: string, vars: Record<string, string>) => string,
  options?: { now?: Date; locale?: string },
): DreamQuotaHint | undefined {
  if (!usage) return undefined;

  if (usage.limit === 0) {
    return { text: copy.disabled, tone: "warn" };
  }

  if (usage.limit === -1) {
    return undefined;
  }

  if (!usage.allowed) {
    // period_end is a UTC boundary (server returns RFC3339 at UTC
    // midnight). Adding a UTC day gives the reset instant — but if
    // we then format in the browser's local timezone, US users see
    // 2026-05-01T00:00:00Z as "Apr 30". Pin formatting to UTC so the
    // reset date matches the API contract regardless of the user's
    // local clock.
    const reset = new Date(usage.period_end);
    reset.setUTCDate(reset.getUTCDate() + 1);
    const resetDate = reset.toLocaleDateString(options?.locale, {
      month: "short",
      day: "numeric",
      timeZone: "UTC",
    });
    return {
      text: interpolate(copy.exhausted, { resetDate }),
      tone: "warn",
    };
  }

  return {
    text: interpolate(copy.used, {
      used: String(usage.used),
      limit: String(usage.limit),
    }),
    tone: "info",
  };
}
