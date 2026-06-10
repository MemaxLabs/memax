"use client";

import { useMemo, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ChevronRight, Minus, ShieldCheck, User, Users } from "lucide-react";

import { cn } from "@memaxlabs/ui";
import { getAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import type { OAuthConsentHub } from "memax-sdk";
import { useInterpolate, useLocale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import {
  type ConsentLoadState,
  notRequestedLabel,
  permissionCopy,
  roleLabel,
  toggleValue,
} from "./consent-labels";
import { ConsentCheck } from "./consent-check";

const EASE: [number, number, number, number] = [0.2, 0.8, 0.2, 1];

export function ConsentForm({
  data,
}: {
  data: Extract<ConsentLoadState, { status: "ready" }>["data"];
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const reduced = useReducedMotion();

  const [selectedPermissions, setSelectedPermissions] = useState<string[]>(
    data.permissions.filter((p) => p.checked).map((p) => p.value),
  );
  const [selectedHubs, setSelectedHubs] = useState<string[]>(
    data.hubs.filter((h) => h.checked && !h.disabled).map((h) => h.id),
  );
  const [showTeamHubs, setShowTeamHubs] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const canApprove = selectedPermissions.length > 0 && selectedHubs.length > 0;
  const agent = getAgentIdentity(data.agent_name);
  const AgentIcon = agent.icon;

  const { personalHubs, teamHubs } = useMemo(() => {
    const personal: OAuthConsentHub[] = [];
    const team: OAuthConsentHub[] = [];
    for (const hub of data.hubs) {
      if (hub.hub_type === "personal") personal.push(hub);
      else team.push(hub);
    }
    return { personalHubs: personal, teamHubs: team };
  }, [data.hubs]);

  const selectedTeamCount = teamHubs.filter((h) =>
    selectedHubs.includes(h.id),
  ).length;

  return (
    <form
      method="post"
      action={data.submit_url}
      onSubmit={() => setSubmitting(true)}
      className="animate-fade-up overflow-hidden rounded-surface border border-border/50 shadow-[0_25px_50px_-12px_oklch(from_var(--foreground)_l_c_h/0.15)]"
      style={{
        background: "oklch(from var(--card) l c h / 0.95)",
        backdropFilter: "blur(24px) saturate(140%)",
      }}
    >
      <input type="hidden" name="session_id" value={data.session_id} />
      <input type="hidden" name="csrf_token" value={data.csrf_token} />
      {selectedHubs.map((hubId) => (
        <input key={hubId} type="hidden" name="hub_id" value={hubId} />
      ))}

      {/* ── Header ── */}
      <header className="border-b border-border/30 px-6 pt-7 pb-5 text-center">
        <div
          className="mx-auto mb-4 flex size-14 items-center justify-center rounded-chrome"
          style={{ background: `oklch(from ${agent.color} l c h / 0.12)` }}
        >
          <AgentIcon
            className="size-6"
            style={{ color: agent.color }}
            strokeWidth={1.8}
            aria-hidden="true"
          />
        </div>

        <h1
          className="text-[20px] font-semibold text-fg-1 sm:text-[22px]"
          style={{ letterSpacing: "-0.01em" }}
        >
          {interpolate(t.auth.oauthConsent.connectTitle, {
            client: data.client_name,
          })}
        </h1>
        <p className="mx-auto mt-1.5 max-w-[380px] text-[13px] leading-relaxed text-fg-3">
          {interpolate(t.auth.oauthConsent.connectSubtitle, {
            client: data.client_name,
          })}
        </p>

        {/* Safe-default nudge — quiet text pill, no icon */}
        <div
          className="mt-3 inline-flex items-center rounded-full px-2.5 py-1 text-[11px] text-fg-3"
          style={{ background: "oklch(from var(--foreground) l c h / 0.04)" }}
        >
          {t.auth.oauthConsent.safeDefaultsHint}
        </div>

        {data.resource ? (
          <p className="mt-3 break-all text-[11px] text-fg-4">
            {data.resource}
          </p>
        ) : null}
      </header>

      {/* ── Capabilities ── */}
      <section className="px-6 pt-5">
        <h2 className="mb-2 text-[11px] font-medium uppercase tracking-wider text-fg-3">
          {t.auth.oauthConsent.capabilitiesTitle}
        </h2>
        <div className="space-y-1.5">
          {data.permissions.map((permission) => {
            const copy = permissionCopy(permission, t);
            const checked = selectedPermissions.includes(permission.value);
            const essential = Boolean(permission.essential);
            return (
              <label
                key={permission.value}
                className={cn(
                  "flex items-start gap-3 rounded-surface border px-4 py-3.5 transition-colors",
                  essential ? "cursor-default" : "cursor-pointer",
                  checked
                    ? "border-foreground/20 bg-foreground/[0.06]"
                    : "border-foreground/[0.06] bg-foreground/[0.02] hover:border-foreground/10",
                )}
              >
                <ConsentCheck
                  name="permission"
                  value={permission.value}
                  checked={checked}
                  disabled={essential}
                  onChange={(event) => {
                    if (essential) return;
                    setSelectedPermissions((cur) =>
                      toggleValue(cur, permission.value, event.target.checked),
                    );
                  }}
                />
                {essential ? (
                  <input
                    type="hidden"
                    name="permission"
                    value={permission.value}
                  />
                ) : null}
                <div className="min-w-0 flex-1">
                  <div className="mb-0.5 flex items-center gap-2">
                    <span className="text-[14px] font-medium text-fg-1">
                      {copy.title}
                    </span>
                    {essential ? (
                      <CapabilityChip tone="muted">
                        {t.auth.oauthConsent.essentialChip}
                      </CapabilityChip>
                    ) : null}
                  </div>
                  <p className="text-[12px] leading-snug text-fg-3">
                    {copy.description}
                  </p>
                </div>
              </label>
            );
          })}
        </div>
      </section>

      {/* ── Personal hub ── */}
      {personalHubs.length > 0 ? (
        <section className="px-6 pt-5">
          <h2 className="mb-2 text-[11px] font-medium uppercase tracking-wider text-fg-3">
            {t.auth.oauthConsent.hubsPersonalTitle}
          </h2>
          <div className="space-y-1.5">
            {personalHubs.map((hub) => (
              <HubRow
                key={hub.id}
                hub={hub}
                checked={selectedHubs.includes(hub.id)}
                t={t}
                interpolate={interpolate}
                onToggle={(next) =>
                  setSelectedHubs((cur) => toggleValue(cur, hub.id, next))
                }
              />
            ))}
          </div>
        </section>
      ) : null}

      {/* ── Team hubs (collapsible) ── */}
      {teamHubs.length > 0 ? (
        <section className="px-6 pt-4">
          <button
            type="button"
            onClick={() => setShowTeamHubs((s) => !s)}
            className="-mx-2 flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-foreground/[0.03]"
          >
            <ChevronRight
              className="size-3 text-fg-3"
              style={{
                transform: showTeamHubs ? "rotate(90deg)" : "rotate(0deg)",
                transition: reduced
                  ? "none"
                  : `transform 0.15s ${EASE.join(",")}`,
              }}
              aria-hidden="true"
            />
            <h2 className="flex-1 text-left text-[11px] font-medium uppercase tracking-wider text-fg-3">
              {t.auth.oauthConsent.hubsTeamTitle} ({teamHubs.length})
            </h2>
            {selectedTeamCount > 0 ? (
              <span className="text-[11px] text-fg-4">
                {interpolate(t.auth.oauthConsent.hubsTeamCount, {
                  count: selectedTeamCount,
                })}
              </span>
            ) : null}
          </button>
          <AnimatePresence initial={false}>
            {showTeamHubs ? (
              <motion.div
                key="team-hubs"
                initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                animate={
                  reduced ? { opacity: 1 } : { opacity: 1, height: "auto" }
                }
                exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
                transition={{ duration: 0.25, ease: EASE }}
                style={{ overflow: "hidden" }}
              >
                <div className="space-y-1.5 pt-2">
                  {teamHubs.map((hub) => (
                    <HubRow
                      key={hub.id}
                      hub={hub}
                      checked={selectedHubs.includes(hub.id)}
                      t={t}
                      interpolate={interpolate}
                      onToggle={(next) =>
                        setSelectedHubs((cur) => toggleValue(cur, hub.id, next))
                      }
                    />
                  ))}
                </div>
              </motion.div>
            ) : null}
          </AnimatePresence>
        </section>
      ) : null}

      {/* ── Won't access ── */}
      {data.not_requested.length > 0 ? (
        <section className="px-6 pt-5">
          <h2 className="mb-2 flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wider text-fg-3">
            <ShieldCheck className="size-3" aria-hidden="true" />
            {interpolate(t.auth.oauthConsent.notRequestedTitleAgent, {
              client: data.client_name,
            })}
          </h2>
          <div
            className="space-y-1 rounded-surface border px-4 py-3"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.02)",
              borderColor: "oklch(from var(--foreground) l c h / 0.06)",
            }}
          >
            {data.not_requested.map((item) => (
              <div
                key={item}
                className="flex items-center gap-2 text-[12px] text-fg-3"
              >
                <Minus
                  className="size-3 shrink-0 text-fg-4"
                  strokeWidth={2.5}
                  aria-hidden="true"
                />
                {notRequestedLabel(item, t)}
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {/* ── Post-approval note ── */}
      <section className="px-6 pt-4">
        <p className="text-[11px] leading-relaxed text-fg-4">
          {interpolate(t.auth.oauthConsent.postApprovalNote, {
            client: data.client_name,
          })}
        </p>
      </section>

      {/* ── Footer ── */}
      <footer className="mt-4 flex items-center justify-end gap-2 border-t border-border/30 px-6 py-5">
        {!canApprove ? (
          <p className="mr-auto text-[11px] text-fg-4">
            {t.auth.oauthConsent.approveDisabled}
          </p>
        ) : null}
        <button
          type="submit"
          name="decision"
          value="deny"
          disabled={submitting}
          className="h-10 cursor-pointer rounded-chrome px-5 text-[13px] font-medium text-fg-3 transition-colors hover:bg-foreground/[0.05] hover:text-fg-1 disabled:pointer-events-none disabled:opacity-50"
        >
          {t.auth.oauthConsent.deny}
        </button>
        <button
          type="submit"
          name="decision"
          value="approve"
          disabled={!canApprove || submitting}
          className="h-10 cursor-pointer rounded-chrome bg-foreground px-5 text-[13px] font-medium text-background transition-opacity hover:opacity-90 active:opacity-80 disabled:pointer-events-none disabled:opacity-40"
        >
          {interpolate(t.auth.oauthConsent.approveAction, {
            client: data.client_name,
          })}
        </button>
      </footer>
    </form>
  );
}

/* ──────────────────────────────────────────────────────────────────
   Capability chip — small semantic pill for role / memory count /
   warning states on hub rows and essential markers on permissions.
   ────────────────────────────────────────────────────────────────── */

function CapabilityChip({
  children,
  tone = "muted",
}: {
  children: React.ReactNode;
  tone?: "muted" | "warning";
}) {
  const styles =
    tone === "warning"
      ? {
          background: "oklch(0.6 0.15 60 / 0.08)",
          color: "oklch(0.6 0.15 60)",
        }
      : {
          background: "oklch(from var(--foreground) l c h / 0.05)",
          color: "var(--fg-4)",
        };
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium"
      style={styles}
    >
      {children}
    </span>
  );
}

/* ──────────────────────────────────────────────────────────────────
   Hub row — personal (User icon) or team (Users icon) with name +
   chip row (role, memory count, optional warning chips).
   ────────────────────────────────────────────────────────────────── */

function HubRow({
  hub,
  checked,
  t,
  interpolate,
  onToggle,
}: {
  hub: OAuthConsentHub;
  checked: boolean;
  t: Translations;
  interpolate: (
    template: string,
    values: Record<string, string | number>,
  ) => string;
  onToggle: (next: boolean) => void;
}) {
  const Icon = hub.hub_type === "personal" ? User : Users;
  const disabled = hub.disabled;
  const canRead = hub.supported_permissions.includes("memory:read");
  const canWrite = hub.supported_permissions.includes("memory:write");
  const readOnly = canRead && !canWrite;
  return (
    <label
      className={cn(
        "flex items-center gap-3 rounded-surface border px-4 py-3 transition-colors",
        disabled && "pointer-events-none opacity-50",
        !disabled && checked
          ? "border-foreground/20 bg-foreground/[0.06]"
          : "border-foreground/[0.06] bg-foreground/[0.02] hover:border-foreground/10",
        !disabled && "cursor-pointer",
      )}
    >
      <ConsentCheck
        name="hub_id"
        value={hub.id}
        checked={checked && !disabled}
        disabled={disabled}
        onChange={(event) => onToggle(event.target.checked)}
      />
      <div
        className="flex size-7 shrink-0 items-center justify-center rounded-lg"
        style={{ background: "oklch(from var(--foreground) l c h / 0.05)" }}
      >
        <Icon
          className="size-3.5 text-fg-2"
          strokeWidth={1.8}
          aria-hidden="true"
        />
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[14px] font-medium text-fg-1">{hub.name}</p>
        <div className="mt-0.5 flex flex-wrap items-center gap-1">
          <CapabilityChip>{roleLabel(hub.role, t)}</CapabilityChip>
          <CapabilityChip>
            {interpolate(t.auth.oauthConsent.memoriesCount, {
              count: hub.memory_count,
            })}
          </CapabilityChip>
          {readOnly ? (
            <CapabilityChip tone="warning">
              {t.auth.oauthConsent.readOnlyChip}
            </CapabilityChip>
          ) : null}
          {disabled ? (
            <CapabilityChip tone="warning">
              {t.auth.oauthConsent.roleCannotUseChip}
            </CapabilityChip>
          ) : null}
        </div>
      </div>
    </label>
  );
}
