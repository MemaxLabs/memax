"use client";

import { useAdminConfig } from "@/hooks/use-admin-users";

/**
 * /admin/infra/config — read-only infra + security status.
 *
 * Answers "what mode is the server actually running in right now?"
 * without shell access. Primary incident debugging surface for the
 * TRUSTED_PROXY / ORIGIN_SHARED_SECRET / meter flags that each shipped
 * with their own "activate by env var" contract — if staging and prod
 * diverge, this page is how you find out.
 */
export default function AdminInfraConfigPage() {
  const { data, isLoading, error } = useAdminConfig();

  if (isLoading) {
    return (
      <div className="max-w-3xl space-y-4">
        <div className="h-6 w-40 bg-surface-2 rounded animate-pulse" />
        <div className="h-60 bg-surface-2 rounded-surface animate-pulse" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="max-w-3xl">
        <p className="text-fg-3">Couldn&apos;t load config.</p>
      </div>
    );
  }

  return (
    <div className="max-w-3xl space-y-6">
      <p className="text-sm text-fg-3">
        Read-only. All values are captured at process startup or env at request
        time — nothing here is editable from the UI.
      </p>

      <div className="glass rounded-surface border border-border divide-y divide-border/60">
        <Row label="Trusted proxy" value={data.trusted_proxy}>
          {renderTrustedProxyNote(data.trusted_proxy)}
        </Row>
        <Row
          label="Origin secret"
          value={data.origin_secret_set ? "configured" : "unset"}
        >
          {data.origin_secret_set
            ? "ORIGIN_SHARED_SECRET is set; direct-to-origin traffic is blocked."
            : "Not set — any client can reach Fly directly. Set when Cloudflare is in front."}
        </Row>
        <Row label="Meter" value={data.meter_enabled ? "enabled" : "degraded"}>
          {data.meter_enabled
            ? "Quota enforcement is active."
            : "Redis unavailable at startup; quotas are NOT enforced."}
        </Row>
        <Row
          label="Rate limiter"
          value={data.rate_limit_enabled ? "enabled" : "degraded"}
        >
          {data.rate_limit_enabled
            ? "Per-user + per-IP rate limits active."
            : "Redis unavailable at startup; rate limits are NOT enforced."}
        </Row>
        <Row
          label="Version"
          value={data.semver && data.semver.length > 0 ? data.semver : "—"}
        >
          {data.semver
            ? "Semver tag for this deploy. Prod: exact release tag. Staging: `git describe` output."
            : "Semver wasn't injected into this binary. Deploy via CI to populate it."}
        </Row>
        <Row label="Commit" value={data.version}>
          {data.version === "unknown"
            ? "Binary built outside a VCS tree — can't correlate with a commit."
            : "Main module VCS revision (short)."}
        </Row>
        <Row label="Server time" value={data.server_time}>
          Clock skew check: compare to your local time.
        </Row>
      </div>
    </div>
  );
}

function renderTrustedProxyNote(mode: string): string {
  switch (mode) {
    case "none":
      return "RemoteAddr only. Safe default. Headers are not trusted.";
    case "cloudflare":
      return "CF-Connecting-IP → XFF leftmost → RemoteAddr. Requires origin lockdown to stop direct spoofing.";
    case "fly":
      return "Fly-Client-IP → XFF → RemoteAddr. Wrong mode if a CDN sits in front of Fly.";
    case "xff":
      return "XFF leftmost → RemoteAddr. Any trusted proxy that appends.";
    default:
      return "Unknown mode — defaulted to none with a startup warning.";
  }
}

function Row({
  label,
  value,
  children,
}: {
  label: string;
  value: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-baseline sm:gap-4">
      <div className="w-full shrink-0 text-sm text-fg-3 sm:w-36">{label}</div>
      <div className="min-w-0 flex-1">
        <div className="font-mono text-sm text-fg-1 break-all">{value}</div>
        <div className="mt-0.5 text-xs text-fg-3">{children}</div>
      </div>
    </div>
  );
}
