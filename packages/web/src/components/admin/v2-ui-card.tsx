"use client";

// The admin user page's V2 UI card (plan 25 E1): whether this person sees
// the V2 Ledger UI and which rule decided (a space on V2, an operator, the
// signup date), and the operator's own choice for them: follow the rules,
// turn it on, or turn it off (which wins over every rule). Their browser
// picks a change up at its next page load.

import { useInterpolate, useLocale } from "@/i18n";
import type { AdminV2UISetting } from "@/lib/admin-client";
import { useAdminV2UI, useSetAdminV2UI } from "@/hooks/use-admin-v2-ui";

const SETTINGS: AdminV2UISetting[] = ["default", "on", "off"];

export function V2UiCard({ userId }: { userId: string }) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const copy = t.admin.users.v2Ui;
  const { data, isLoading, isError } = useAdminV2UI(userId);
  const set = useSetAdminV2UI();

  if (isLoading) return null;

  return (
    <section
      aria-labelledby="admin-v2-ui"
      className="rounded-surface border border-border/50 bg-card p-6 mb-4"
    >
      <div className="flex items-center gap-3 mb-2">
        <h2 id="admin-v2-ui" className="text-[14px] font-medium text-fg-2">
          {copy.title}
        </h2>
        {data ? (
          <span
            className={
              data.ui === "v2"
                ? "inline-block rounded-md bg-emerald-500/15 px-2 py-0.5 text-[12px] font-medium text-emerald-600 dark:text-emerald-400"
                : "inline-block rounded-md bg-surface-2 px-2 py-0.5 text-[12px] font-medium text-fg-2"
            }
          >
            {data.ui === "v2" ? copy.on : copy.off}
          </span>
        ) : null}
      </div>
      {isError || !data ? (
        <p className="text-[13px] text-fg-3" role="alert">
          {copy.loadError}
        </p>
      ) : (
        <>
          <p className="text-[13px] text-fg-2">
            {interpolate(copy.reasons[data.reason], {
              since: data.since ? new Date(data.since).toLocaleString() : "",
            })}
          </p>
          <div
            role="group"
            aria-label={copy.setting}
            className="mt-4 inline-flex rounded-md border border-border/60 p-0.5"
          >
            {SETTINGS.map((s) => (
              <button
                key={s}
                type="button"
                aria-pressed={data.setting === s}
                disabled={set.isPending}
                onClick={() => {
                  if (data.setting !== s) set.mutate({ userId, setting: s });
                }}
                className={
                  data.setting === s
                    ? "rounded px-3 py-1 text-[13px] font-medium bg-surface-2 text-fg-1"
                    : "rounded px-3 py-1 text-[13px] text-fg-3 hover:text-fg-1 transition-colors disabled:opacity-60"
                }
              >
                {copy.settings[s]}
              </button>
            ))}
          </div>
          <p className="mt-2 text-[12px] text-fg-4">{copy.hint}</p>
        </>
      )}
    </section>
  );
}
