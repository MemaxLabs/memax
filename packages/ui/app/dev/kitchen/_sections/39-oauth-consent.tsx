// ═══════════════════════════════════════════════
// 39. OAuth Consent — agent permission grant screen
//
// THE ROLE
// When an MCP client (Claude Code, Cursor, ChatGPT desktop, etc.)
// connects to memax, the user lands here to grant capabilities and
// pick which hubs the agent can access. This is a HIGH-TRUST moment
// — the user is deciding what an external agent can read and write
// on their behalf. The screen needs to feel premium, predictable,
// and quietly confident.
//
// CURRENT PROD
// packages/web/src/app/(auth)/oauth/consent/{page,consent-form,
// consent-check,consent-labels,consent-wordmark}.tsx — flat form
// using memax tokens but missing the glass treatment, state
// animation, permission grouping, and post-approval preview that
// the rest of memax enjoys. This kitchen section audits the current
// state and ships a redesigned version aligned with §29/§34/§36 DNA.
//
// MAPS TO PROD
//   packages/web/src/app/(auth)/oauth/consent/consent-form.tsx
//   packages/web/src/app/(auth)/oauth/consent/consent-check.tsx
//   packages/web/src/app/(auth)/oauth/consent/consent-labels.ts
//   packages/server/internal/handler/mcp_oauth.go
//     buildConsentData() / consentPermissions() / oauthPermissionsFromScope()
//   memax-sdk types
//     OAuthConsentRequest / OAuthConsentPermission / OAuthConsentHub
// ═══════════════════════════════════════════════

"use client";

import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import {
  Check,
  ChevronRight,
  Eye,
  Minus,
  PencilLine,
  ShieldCheck,
  User,
  Users,
  X,
} from "lucide-react";
import { Section, DemoCard } from "../_shared";
import { NORMAL, FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { getAgentIdentity } from "@memaxlabs/ui/tokens/agents";

/* ──────────────────────────────────────────────────────────────────
   Local types — mirror the SDK OAuthConsentRequest shape exactly so
   the kitchen is a true northstar for wire-up.
   ────────────────────────────────────────────────────────────────── */

type Permission = {
  id: "read" | "write" | "delete" | "manage";
  /** Required = pre-checked + cannot uncheck (memax flags this when
   *  the agent's requested scope makes the permission essential). */
  essential?: boolean;
  title: string;
  description: string;
  icon: React.ElementType;
};

type Hub = {
  id: string;
  name: string;
  type: "personal" | "team";
  role: "owner" | "admin" | "contributor" | "viewer";
  memoryCount: number;
  capability: "read-write" | "write" | "read" | "unavailable";
};

const REQUESTED_PERMISSIONS: Permission[] = [
  {
    id: "read",
    essential: true,
    title: "Recall memories",
    description: "Search and read the memories you give it access to.",
    icon: Eye,
  },
  {
    id: "write",
    title: "Save new memories",
    description:
      "Push new captures (text, links, decisions) into selected hubs.",
    icon: PencilLine,
  },
];

const HUBS: Hub[] = [
  {
    id: "personal",
    name: "Personal",
    type: "personal",
    role: "owner",
    memoryCount: 1247,
    capability: "read-write",
  },
  {
    id: "engineering",
    name: "Engineering",
    type: "team",
    role: "contributor",
    memoryCount: 482,
    capability: "read-write",
  },
  {
    id: "marketing",
    name: "Marketing",
    type: "team",
    role: "viewer",
    memoryCount: 89,
    capability: "read",
  },
  {
    id: "ops",
    name: "Platform Ops",
    type: "team",
    role: "admin",
    memoryCount: 215,
    capability: "read-write",
  },
];

const NOT_REQUESTED = [
  "Delete memories",
  "Manage topics or run dreams",
  "Manage hub settings or members",
];

/* ──────────────────────────────────────────────────────────────────
   AUDIT SUMMARY — current prod state vs memax DNA
   ────────────────────────────────────────────────────────────────── */

function CurrentProdAudit() {
  const findings = [
    {
      label: "✓ Memax tokens",
      status: "good",
      detail: "Uses text-fg-1/2/3/4, bg-surface-1/2, --foreground.",
    },
    {
      label: "✓ i18n",
      status: "good",
      detail: "All strings via t.auth.oauthConsent.* keys.",
    },
    {
      label: "✓ Animation",
      status: "good",
      detail: "animate-fade-up + stagger-1/2/3/4 entrance.",
    },
    {
      label: "✓ Agent identity hero",
      status: "good",
      detail: "48px agent icon with color tint at top.",
    },
    {
      label: "✓ Trust footer",
      status: "good",
      detail: "Shield icon + revoke-anytime note.",
    },
    {
      label: "⚠ No glass treatment",
      status: "gap",
      detail:
        "Form sits on flat background. High-trust moment deserves the glass + blur premium feel that settings-dialog uses.",
    },
    {
      label: "⚠ No state animation on selection",
      status: "gap",
      detail:
        "Click toggles bg-color only. No spring, no scale, no commit feedback. Memax DNA uses framer-motion for state transitions.",
    },
    {
      label: "⚠ Permissions are flat",
      status: "gap",
      detail:
        "Read + write rendered identically. No 'essential' vs 'optional' hierarchy. User can't see at a glance what the minimum grant is.",
    },
    {
      label: "⚠ Hubs are ungrouped",
      status: "gap",
      detail:
        "Personal + team mixed in one list. With 5+ hubs the list gets long. Should group: personal first (default-checked), team hubs second (opt-in).",
    },
    {
      label: "⚠ 'Not Requested' uses ✕ marks",
      status: "gap",
      detail:
        "✕ reads as denied/blocked — over-alarms. A green Check inverts the semantics (reads as 'granted' next to 'Delete memories'). Use muted Minus — the universal 'not in scope' glyph.",
    },
    {
      label: "⚠ No post-approval preview",
      status: "gap",
      detail:
        "User doesn't see what happens after they approve. 'Claude Code will appear in Settings → Integrations, revocable anytime' would close the trust loop.",
    },
    {
      label: "⚠ Capability label is bullet-string",
      status: "gap",
      detail:
        "Personal hub · Owner · 47 memories · Read and write — hard to scan at a glance. Should split into chip row.",
    },
    {
      label: "⚠ No safe-default hint",
      status: "gap",
      detail:
        "User lands cold. A subtle 'we've selected the safest defaults' note would reduce friction.",
    },
  ];
  return (
    <div className="space-y-1.5">
      {findings.map((f) => (
        <div
          key={f.label}
          className="flex items-start gap-3 rounded-lg px-3 py-2"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          <span
            className="text-[12px] font-medium shrink-0 w-32"
            style={{
              color:
                f.status === "good" ? "oklch(0.7 0.18 145)" : "var(--fg-2)",
            }}
          >
            {f.label}
          </span>
          <span className="text-[12px] text-fg-3 leading-snug flex-1">
            {f.detail}
          </span>
        </div>
      ))}
    </div>
  );
}

/* ──────────────────────────────────────────────────────────────────
   Capability chip — replaces the "Personal hub · Owner · 47 mem ·
   read-write" bullet string with a row of small chips for scan
   ────────────────────────────────────────────────────────────────── */

function CapabilityChip({
  children,
  tone = "neutral",
}: {
  children: React.ReactNode;
  tone?: "neutral" | "muted" | "warning";
}) {
  const styles =
    tone === "warning"
      ? {
          background: "oklch(0.6 0.15 60 / 0.08)",
          color: "oklch(0.6 0.15 60)",
        }
      : tone === "muted"
        ? {
            background: "oklch(from var(--foreground) l c h / 0.04)",
            color: "var(--fg-4)",
          }
        : {
            background: "oklch(from var(--foreground) l c h / 0.06)",
            color: "var(--fg-2)",
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
   Permission row — checkbox + title + description, with essential
   indicator for required scopes
   ────────────────────────────────────────────────────────────────── */

function PermissionRow({
  permission,
  checked,
  onToggle,
}: {
  permission: Permission;
  checked: boolean;
  onToggle: () => void;
}) {
  const reduced = useReducedMotion();
  const Icon = permission.icon;
  return (
    <button
      type="button"
      onClick={permission.essential ? undefined : onToggle}
      disabled={permission.essential}
      className="w-full flex items-start gap-3 rounded-xl px-4 py-3.5 cursor-pointer transition-colors text-left"
      style={{
        background: checked
          ? "oklch(from var(--foreground) l c h / 0.06)"
          : "oklch(from var(--foreground) l c h / 0.02)",
        border: checked
          ? "1px solid oklch(from var(--foreground) l c h / 0.2)"
          : "1px solid oklch(from var(--foreground) l c h / 0.06)",
        cursor: permission.essential ? "default" : "pointer",
      }}
    >
      {/* Custom checkbox */}
      <div className="relative mt-0.5 shrink-0">
        <motion.div
          className="w-[18px] h-[18px] rounded-md flex items-center justify-center"
          style={{
            background: checked ? "var(--foreground)" : "transparent",
            border: checked
              ? "1.5px solid var(--foreground)"
              : "1.5px solid oklch(from var(--foreground) l c h / 0.25)",
          }}
          animate={{ scale: checked ? 1 : 0.94 }}
          transition={{ duration: reduced ? 0 : FAST, ease: EASE }}
        >
          <AnimatePresence>
            {checked && (
              <motion.span
                key="check"
                initial={{ opacity: 0, scale: 0.5 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.5 }}
                transition={{ duration: reduced ? 0 : FAST, ease: EASE }}
                className="flex"
              >
                <Check
                  className="w-3 h-3"
                  style={{ color: "var(--background)" }}
                  strokeWidth={3}
                />
              </motion.span>
            )}
          </AnimatePresence>
        </motion.div>
      </div>
      {/* Icon + content */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <Icon className="w-3.5 h-3.5 text-fg-3 shrink-0" strokeWidth={1.8} />
          <span className="text-[14px] font-medium text-fg-1">
            {permission.title}
          </span>
          {permission.essential && (
            <CapabilityChip tone="muted">essential</CapabilityChip>
          )}
        </div>
        <p className="text-[12px] text-fg-3 leading-snug">
          {permission.description}
        </p>
      </div>
    </button>
  );
}

/* ──────────────────────────────────────────────────────────────────
   Hub row — checkbox + name + capability chips
   ────────────────────────────────────────────────────────────────── */

function HubRow({
  hub,
  checked,
  onToggle,
}: {
  hub: Hub;
  checked: boolean;
  onToggle: () => void;
}) {
  const reduced = useReducedMotion();
  const disabled = hub.capability === "unavailable";
  const Icon = hub.type === "personal" ? User : Users;
  return (
    <button
      type="button"
      onClick={disabled ? undefined : onToggle}
      disabled={disabled}
      className="w-full flex items-center gap-3 rounded-xl px-4 py-3 text-left transition-colors"
      style={{
        background:
          checked && !disabled
            ? "oklch(from var(--foreground) l c h / 0.06)"
            : "oklch(from var(--foreground) l c h / 0.02)",
        border:
          checked && !disabled
            ? "1px solid oklch(from var(--foreground) l c h / 0.2)"
            : "1px solid oklch(from var(--foreground) l c h / 0.06)",
        cursor: disabled ? "not-allowed" : "pointer",
        opacity: disabled ? 0.5 : 1,
      }}
    >
      <div className="relative shrink-0">
        <motion.div
          className="w-[18px] h-[18px] rounded-md flex items-center justify-center"
          style={{
            background:
              checked && !disabled ? "var(--foreground)" : "transparent",
            border:
              checked && !disabled
                ? "1.5px solid var(--foreground)"
                : "1.5px solid oklch(from var(--foreground) l c h / 0.25)",
          }}
          animate={{ scale: checked && !disabled ? 1 : 0.94 }}
          transition={{ duration: reduced ? 0 : FAST, ease: EASE }}
        >
          <AnimatePresence>
            {checked && !disabled && (
              <motion.span
                key="check"
                initial={{ opacity: 0, scale: 0.5 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.5 }}
                transition={{ duration: reduced ? 0 : FAST, ease: EASE }}
                className="flex"
              >
                <Check
                  className="w-3 h-3"
                  style={{ color: "var(--background)" }}
                  strokeWidth={3}
                />
              </motion.span>
            )}
          </AnimatePresence>
        </motion.div>
      </div>
      <div
        className="w-7 h-7 rounded-lg flex items-center justify-center shrink-0"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.05)",
        }}
      >
        <Icon className="w-3.5 h-3.5 text-fg-2" strokeWidth={1.8} />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-[14px] font-medium text-fg-1 truncate">{hub.name}</p>
        <div className="flex items-center gap-1 mt-0.5 flex-wrap">
          <CapabilityChip tone="muted">{hub.role}</CapabilityChip>
          <CapabilityChip tone="muted">
            {hub.memoryCount.toLocaleString()} memories
          </CapabilityChip>
          {hub.capability === "read" && (
            <CapabilityChip tone="warning">read-only access</CapabilityChip>
          )}
          {hub.capability === "unavailable" && (
            <CapabilityChip tone="warning">role can&apos;t use</CapabilityChip>
          )}
        </div>
      </div>
    </button>
  );
}

/* ──────────────────────────────────────────────────────────────────
   The main redesigned consent demo — interactive
   ────────────────────────────────────────────────────────────────── */

function OAuthConsentRedesignDemo() {
  // Safe defaults: read+write essentials checked, personal hub selected.
  const [perms, setPerms] = useState<Set<string>>(
    () => new Set(["read", "write"]),
  );
  const [hubs, setHubs] = useState<Set<string>>(() => new Set(["personal"]));
  const [showTeamHubs, setShowTeamHubs] = useState(false);
  const [resolved, setResolved] = useState<"approved" | "denied" | null>(null);
  const reduced = useReducedMotion();

  const togglePerm = (id: string) => {
    setPerms((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const toggleHub = (id: string) => {
    setHubs((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const reset = () => {
    setPerms(new Set(["read", "write"]));
    setHubs(new Set(["personal"]));
    setShowTeamHubs(false);
    setResolved(null);
  };

  const personalHubs = HUBS.filter((h) => h.type === "personal");
  const teamHubs = HUBS.filter((h) => h.type === "team");
  const canApprove = perms.size > 0 && hubs.size > 0;

  if (resolved === "approved") {
    return (
      <div
        className="rounded-2xl p-8 text-center"
        style={{
          background: "oklch(from var(--card) l c h / 0.95)",
          backdropFilter: "blur(24px) saturate(140%)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
        }}
      >
        <div
          className="w-14 h-14 rounded-2xl flex items-center justify-center mx-auto mb-3"
          style={{ background: "oklch(0.7 0.18 145 / 0.1)" }}
        >
          <Check
            className="w-6 h-6"
            style={{ color: "oklch(0.7 0.18 145)" }}
            strokeWidth={2.5}
          />
        </div>
        <h2 className="text-[18px] font-semibold text-fg-1 mb-2">
          {/* i18n: t.auth.oauthConsent.approvedTitle */}
          Claude Code is connected
        </h2>
        <p className="text-[13px] text-fg-3 max-w-[400px] mx-auto mb-4 leading-relaxed">
          {/* i18n: t.auth.oauthConsent.approvedBody (interpolated) */}
          {perms.size} {perms.size === 1 ? "capability" : "capabilities"} ·{" "}
          {hubs.size} {hubs.size === 1 ? "hub" : "hubs"}. You can revoke this
          connection anytime from{" "}
          <span className="font-mono text-[12px]">Settings → Integrations</span>
          .
        </p>
        <button
          type="button"
          onClick={reset}
          className="text-[12px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
        >
          Reset demo
        </button>
      </div>
    );
  }

  if (resolved === "denied") {
    return (
      <div
        className="rounded-2xl p-8 text-center"
        style={{
          background: "oklch(from var(--card) l c h / 0.95)",
          backdropFilter: "blur(24px) saturate(140%)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
        }}
      >
        <div
          className="w-14 h-14 rounded-2xl flex items-center justify-center mx-auto mb-3"
          style={{ background: "oklch(from var(--foreground) l c h / 0.05)" }}
        >
          <X className="w-6 h-6 text-fg-3" strokeWidth={2.5} />
        </div>
        <h2 className="text-[18px] font-semibold text-fg-1 mb-2">
          {/* i18n: t.auth.oauthConsent.deniedTitle */}
          Connection declined
        </h2>
        <p className="text-[13px] text-fg-3 max-w-[400px] mx-auto mb-4">
          {/* i18n: t.auth.oauthConsent.deniedBody */}
          Claude Code did not get access. You can re-authorize from the agent
          anytime.
        </p>
        <button
          type="button"
          onClick={reset}
          className="text-[12px] text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
        >
          Reset demo
        </button>
      </div>
    );
  }

  return (
    <div
      className="rounded-2xl overflow-hidden mx-auto"
      style={{
        maxWidth: 560,
        background: "oklch(from var(--card) l c h / 0.95)",
        backdropFilter: "blur(24px) saturate(140%)",
        border: "1px solid oklch(from var(--border) l c h / 0.5)",
        boxShadow:
          "0 25px 50px -12px oklch(from var(--foreground) l c h / 0.15)",
      }}
    >
      {/* Header — agent identity hero. Uses getAgentIdentity() — the same
          token the memory-row attribution, agents settings, and prod
          consent-form already use. Prod passes data.agent_name here; the
          kitchen hard-codes "claude-code" for the demo. Generic Bot
          fallback is baked into getAgentIdentity for unknown slugs. */}
      <div className="px-6 pt-7 pb-5 text-center border-b border-border/30">
        {(() => {
          const agent = getAgentIdentity("claude-code");
          const AgentIcon = agent.icon;
          return (
            <div
              className="w-14 h-14 rounded-2xl flex items-center justify-center mx-auto mb-4"
              style={{
                background: `oklch(from ${agent.color} l c h / 0.12)`,
              }}
            >
              <AgentIcon
                className="w-6 h-6"
                style={{ color: agent.color }}
                strokeWidth={1.8}
                aria-hidden="true"
              />
            </div>
          );
        })()}
        <h2
          className="text-[20px] font-semibold text-fg-1 mb-1.5"
          style={{ letterSpacing: "-0.01em" }}
        >
          {/* i18n: t.auth.oauthConsent.connectTitle (interpolated) */}
          Connect Claude Code to memax
        </h2>
        <p className="text-[13px] text-fg-3 max-w-[380px] mx-auto leading-relaxed">
          {/* i18n: t.auth.oauthConsent.connectSubtitle */}
          Claude Code can only use the capabilities and hubs you approve here.
        </p>
        {/* Safe-default nudge — quiet pill, no icon (kitchen restraint) */}
        <div
          className="inline-flex items-center mt-3 px-2.5 py-1 rounded-full text-[11px] text-fg-3"
          style={{ background: "oklch(from var(--foreground) l c h / 0.04)" }}
        >
          {/* i18n: t.auth.oauthConsent.safeDefaultsHint */}
          Safe defaults selected — adjust as needed
        </div>
      </div>

      {/* Capabilities — grouped: essentials always, additional opt-in */}
      <div className="px-6 pt-5">
        <h3 className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          {/* i18n: t.auth.oauthConsent.capabilitiesTitle */}
          Capabilities
        </h3>
        <div className="space-y-1.5">
          {REQUESTED_PERMISSIONS.map((p) => (
            <PermissionRow
              key={p.id}
              permission={p}
              checked={perms.has(p.id)}
              onToggle={() => togglePerm(p.id)}
            />
          ))}
        </div>
      </div>

      {/* Hubs — grouped: personal first (auto), teams collapsible */}
      <div className="px-6 pt-5">
        <h3 className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
          {/* i18n: t.auth.oauthConsent.hubsPersonalTitle */}
          Personal hub
        </h3>
        <div className="space-y-1.5">
          {personalHubs.map((hub) => (
            <HubRow
              key={hub.id}
              hub={hub}
              checked={hubs.has(hub.id)}
              onToggle={() => toggleHub(hub.id)}
            />
          ))}
        </div>
      </div>

      <div className="px-6 pt-4">
        <button
          type="button"
          onClick={() => setShowTeamHubs((s) => !s)}
          className="w-full flex items-center gap-2 py-2 cursor-pointer hover:bg-surface-1 rounded-lg px-2 -mx-2 transition-colors"
        >
          <ChevronRight
            className="w-3 h-3 text-fg-3"
            style={{
              transform: showTeamHubs ? "rotate(90deg)" : "rotate(0deg)",
              transition: reduced ? "none" : `transform ${FAST}s ${EASE}`,
            }}
          />
          <h3 className="text-[11px] font-medium text-fg-3 uppercase tracking-wider flex-1 text-left">
            {/* i18n: t.auth.oauthConsent.hubsTeamTitle */}
            Team hubs ({teamHubs.length})
          </h3>
          <span className="text-[11px] text-fg-4">
            {hubs.size - (hubs.has("personal") ? 1 : 0)} selected
          </span>
        </button>
        <AnimatePresence initial={false}>
          {showTeamHubs && (
            <motion.div
              key="team-hubs"
              initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
              animate={
                reduced ? { opacity: 1 } : { opacity: 1, height: "auto" }
              }
              exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
              transition={{ duration: NORMAL, ease: EASE }}
              style={{ overflow: "hidden" }}
            >
              <div className="space-y-1.5 pt-2">
                {teamHubs.map((hub) => (
                  <HubRow
                    key={hub.id}
                    hub={hub}
                    checked={hubs.has(hub.id)}
                    onToggle={() => toggleHub(hub.id)}
                  />
                ))}
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* Won't access — positive trust signal via muted Minus (absence), not
          a green Check (which would invert the semantics). */}
      <div className="px-6 pt-5">
        <h3 className="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2 flex items-center gap-1.5">
          <ShieldCheck className="w-3 h-3" />
          {/* i18n: t.auth.oauthConsent.notRequestedTitle */}
          Claude Code won&apos;t access
        </h3>
        <div
          className="rounded-xl px-4 py-3 space-y-1"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.02)",
            border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
          }}
        >
          {NOT_REQUESTED.map((item) => (
            <div
              key={item}
              className="flex items-center gap-2 text-[12px] text-fg-3"
            >
              {/* Muted Minus, not green Check — a green check next to
                  "Delete memories" would invert the semantics (reads as
                  "granted"). Minus is the universal "not in scope" glyph. */}
              <Minus
                className="w-3 h-3 shrink-0 text-fg-4"
                strokeWidth={2.5}
                aria-hidden="true"
              />
              {item}
            </div>
          ))}
        </div>
      </div>

      {/* Post-approval preview — no icon. ShieldCheck already appears
          once above in the "Won't access" header; a second ShieldCheck
          here would feel decorative, not semantic. */}
      <div className="px-6 pt-4">
        <p className="text-[11px] text-fg-4 leading-relaxed">
          {/* i18n: t.auth.oauthConsent.postApprovalNote */}
          After you approve, Claude Code appears in{" "}
          <span className="font-mono text-[10px]">Settings → Integrations</span>
          . You can revoke or change scope anytime.
        </p>
      </div>

      {/* Footer */}
      <div className="px-6 py-5 mt-3 border-t border-border/30 flex items-center justify-end gap-2">
        <button
          type="button"
          onClick={() => setResolved("denied")}
          className="h-10 px-5 rounded-xl text-[13px] font-medium text-fg-3 hover:bg-surface-1 hover:text-fg-1 cursor-pointer transition-colors"
        >
          {/* i18n: t.auth.oauthConsent.deny */}
          Deny
        </button>
        <button
          type="button"
          onClick={() => setResolved("approved")}
          disabled={!canApprove}
          className="h-10 px-5 rounded-xl text-[13px] font-medium cursor-pointer transition-opacity hover:opacity-90 disabled:opacity-40 disabled:cursor-not-allowed"
          style={{
            background: "var(--foreground)",
            color: "var(--background)",
          }}
        >
          {/* i18n: t.auth.oauthConsent.approve */}
          Connect Claude Code
        </button>
      </div>
    </div>
  );
}

/* ──────────────────────────────────────────────────────────────────
   Section export
   ────────────────────────────────────────────────────────────────── */

export function OAuthConsentSection() {
  return (
    <Section
      title="39. OAuth Consent"
      description="MCP client connecting to memax — agents request capabilities and hub access; user grants. Audit of current prod + redesign aligned with §29/§34/§36 DNA (glass + state animation + grouping + trust signals + post-approval preview)."
    >
      {/* ── Principle ── */}
      <DemoCard label="39. The principle — high-trust grant moment, premium quiet">
        <div className="space-y-2 text-[13px] text-fg-2">
          <p>
            <span className="text-fg-1 font-medium">
              An MCP client is asking to act on your behalf.
            </span>{" "}
            This is the highest-trust moment in the entire memax flow — the user
            is deciding what Claude / Cursor / ChatGPT desktop can read and
            write. Everything about the screen should feel premium, predictable,
            and quietly confident.
          </p>
          <p>
            <span className="text-fg-1 font-medium">
              Three jobs the screen does:
            </span>{" "}
            (1) tell the user <span className="text-fg-1">who is asking</span>{" "}
            with high confidence (agent identity hero), (2) let the user{" "}
            <span className="text-fg-1">grant the minimum needed</span> quickly
            via safe defaults, (3) close the trust loop by showing{" "}
            <span className="text-fg-1">what won&apos;t happen</span> + how to
            revoke.
          </p>
          <p className="text-fg-3 text-[12px]">
            Wired to:{" "}
            <span className="font-mono">
              packages/web/src/app/(auth)/oauth/consent/
            </span>{" "}
            +{" "}
            <span className="font-mono">
              packages/server/internal/handler/mcp_oauth.go
            </span>{" "}
            +{" "}
            <span className="font-mono">
              memax-sdk types (OAuthConsentRequest)
            </span>
            .
          </p>
        </div>
      </DemoCard>

      {/* ── Audit ── */}
      <DemoCard label="39-audit. Current prod state — what works + what diverges from DNA">
        <p className="text-[12px] text-fg-2 mb-3">
          Audit of{" "}
          <span className="font-mono">
            packages/web/src/app/(auth)/oauth/consent/
          </span>{" "}
          against memax kitchen DNA. Five strengths + eight gaps. The form is
          structurally sound and uses memax tokens correctly — the gaps are
          about premium feel, hierarchy, and trust-loop completion.
        </p>
        <CurrentProdAudit />
      </DemoCard>

      {/* ── Redesigned consent demo ── */}
      <DemoCard label="39-redesigned. Redesigned consent (interactive)">
        <p className="text-[12px] text-fg-2 mb-1">
          Click capabilities and hubs to toggle. Approve / Deny resolves to the
          post-approval state. All eight gap fixes from the audit are wired in:
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            <span className="text-fg-1">Glass treatment</span> — same chrome as
            settings-dialog (oklch 0.95 + 24px blur + saturate 140% + 1px border
            + 25px shadow).
          </li>
          <li>
            <span className="text-fg-1">State animation on selection</span> —
            checkbox spring-scales on toggle, Check icon morphs in via
            AnimatePresence.
          </li>
          <li>
            <span className="text-fg-1">Permission grouping</span> — read is
            marked <span className="font-mono">essential</span> ( pre-checked,
            locked); write is opt-in. Visual hierarchy makes "what's the
            minimum" obvious.
          </li>
          <li>
            <span className="text-fg-1">Hub grouping</span> — Personal hub
            renders first, default-checked. Team hubs collapse under a "Team
            hubs (N)" expander to keep the form tight; opens on demand.
            Read-only hubs get a warning chip explaining scope limits.
          </li>
          <li>
            <span className="text-fg-1">Capability chips</span> — replaces the
            bullet-string{" "}
            <span className="font-mono">
              "Personal hub · Owner · 47 memories · Read-write"
            </span>{" "}
            with a row of small chips (<span className="font-mono">role</span>,{" "}
            <span className="font-mono">memory count</span>, optional warning
            chip).
          </li>
          <li>
            <span className="text-fg-1">Won&apos;t-access trust signal</span> —
            reframed from "Not Requested" and marked with muted{" "}
            <span className="font-mono">Minus</span> glyphs (not green
            <span className="font-mono"> Check</span> — green check next to
            "Delete memories" inverts the semantics). Reads as absence of scope,
            not approval.
          </li>
          <li>
            <span className="text-fg-1">Post-approval preview</span> — a small
            footer line tells the user exactly where they&apos;ll find this
            connection later (
            <span className="font-mono">Settings → Integrations</span>) and that
            it&apos;s revocable. Closes the trust loop.
          </li>
          <li>
            <span className="text-fg-1">Safe-default nudge</span> — sparkle chip
            under the title says &ldquo;Safe defaults selected — adjust as
            needed&rdquo; so the user knows they can hit Approve immediately
            without reading every checkbox.
          </li>
        </ul>
        <OAuthConsentRedesignDemo />
      </DemoCard>

      {/* ── Rules ── */}
      <DemoCard label="39-rules. OAuth consent design rules">
        <div className="space-y-2 text-[12px] text-fg-2">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Glass shell, not a flat form.
              </span>{" "}
              Consent is a high-trust moment. The container uses the same glass
              treatment as <span className="font-mono">settings-dialog</span> +
              dropdown panels (oklch 0.95 card + 24px blur + saturate 140% + 1px
              border + 25px shadow). Premium = restraint, not decoration.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Agent identity hero at the top.
              </span>{" "}
              48–56px agent icon with the agent&apos;s color tint, agent name in
              the title (
              <span className="font-mono">Connect {"{client}"} to memax</span>
              ). The user must know who is asking before they read anything
              else.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Safe defaults, pre-selected.
              </span>{" "}
              The form lands with a working set already chosen: read is checked
              + locked (<span className="font-mono">essential</span> chip),
              write is checked but unlockable, personal hub is checked. A quiet
              text pill below the title (no icon, kitchen restraint) says
              &ldquo;Safe defaults selected — adjust as needed&rdquo; so the
              user knows they can hit Approve immediately. Reduces decision
              friction without removing control.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Permissions show essentiality.
              </span>{" "}
              Required permissions render with an{" "}
              <span className="font-mono">essential</span> chip and are not
              togglable. Optional permissions are clearly opt-in. Visual
              hierarchy makes &ldquo;what&apos;s the minimum grant&rdquo;
              obvious in one glance.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Hubs grouped: personal first, team collapsible.
              </span>{" "}
              Personal hub is its own section, default-checked. Team hubs
              collapse under a <span className="font-mono">Team hubs (N)</span>{" "}
              expander with chevron rotation animation. Keeps the form compact
              for the common case (most users grant personal only), opens on
              demand for team-hub power users.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Capability metadata as chip row, not bullet string.
              </span>{" "}
              Each hub row has a row of small chips below the name (
              <span className="font-mono">role</span>,{" "}
              <span className="font-mono">memory count</span>, optional warning
              chip for read-only or unavailable). Easier to scan than{" "}
              <span className="font-mono">
                "Personal hub · Owner · 47 memories · Read-write"
              </span>
              .
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p>
              <span className="text-fg-1 font-medium">
                State animation on every selection.
              </span>{" "}
              Checkbox toggles spring-scale via framer-motion (FAST 0.15s EASE).
              Check icon morphs in via{" "}
              <span className="font-mono">AnimatePresence</span>. Row background
              transitions on hover and select. Reduced-motion gates all of it.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">8.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Won&apos;t-access uses the universal &ldquo;not in scope&rdquo;
                glyph.
              </span>{" "}
              Reframed as &ldquo;{"{client}"} won&apos;t access:&rdquo; with
              muted <span className="font-mono">Minus</span> icons at{" "}
              <span className="font-mono">text-fg-4</span>. A green{" "}
              <span className="font-mono">Check</span> next to &ldquo;Delete
              memories&rdquo; would invert the semantics (reads as
              &ldquo;granted&rdquo;); a red <span className="font-mono">X</span>{" "}
              over-alarms. Minus reads as absence of scope — quiet, accurate,
              industry-standard (GitHub Apps, Google OAuth, Slack all handle
              this with neutral typographic signals, not affirmative checks).
              Items: delete memories, manage topics or run dreams, manage hub
              settings or members.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">9.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Post-approval preview closes the trust loop.
              </span>{" "}
              Small footer line above the action buttons tells the user what
              happens next:{" "}
              <span className="font-mono">
                After you approve, Claude Code appears in Settings →
                Integrations. You can revoke or change scope anytime.
              </span>
              Reduces fear of "what did I just agree to".
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">10.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Approve is the primary action; Deny is quiet.
              </span>{" "}
              Approve uses{" "}
              <span className="font-mono">
                bg-foreground text-background font-medium
              </span>
              ; Deny is a ghost button at{" "}
              <span className="font-mono">text-fg-3</span> with no background.
              Approve is disabled (40% opacity) when no capabilities or hubs are
              selected — paired with the safe defaults rule above, this state is
              rare.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">11.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Resolved states are confident, not chatty.
              </span>{" "}
              Approve →{" "}
              <span className="font-mono">"Claude Code is connected"</span> +
              count of capabilities + hubs + revoke pointer. Deny →{" "}
              <span className="font-mono">"Connection declined"</span> +
              re-authorize hint. Both render in the same glass shell so the
              surface morphs in place — no overlay, no second screen.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">12.</span>
            <p>
              <span className="text-fg-1 font-medium">i18n + brand voice.</span>{" "}
              Every string goes through{" "}
              <span className="font-mono">t.auth.oauthConsent.*</span> with{" "}
              <span className="font-mono">{"{client}"}</span> interpolation for
              the agent name. Brand voice: warm, precise, quiet. No demanding
              phrasing (&ldquo;You must&rdquo;), no fear language (&ldquo;Risk
              of data loss&rdquo;). Bilingual parity: each locale gets its own
              pass.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Migration notes ── */}
      <DemoCard label="39-migration. What Codex needs to wire">
        <div className="space-y-2 text-[12px] text-fg-2">
          <p>
            <span className="text-fg-1 font-medium">Same data shape:</span> no
            SDK / backend changes required. The existing{" "}
            <span className="font-mono">OAuthConsentRequest</span> /{" "}
            <span className="font-mono">OAuthConsentPermission</span> /{" "}
            <span className="font-mono">OAuthConsentHub</span> types in{" "}
            <span className="font-mono">memax-sdk</span> types have everything
            needed. The redesign is purely UI.
          </p>
          <p>
            <span className="text-fg-1 font-medium">Files to refactor:</span>{" "}
            <span className="font-mono">consent-form.tsx</span> (rewrite with
            the redesigned shape),{" "}
            <span className="font-mono">consent-check.tsx</span> (replace with
            the framer-motion checkbox in this section),{" "}
            <span className="font-mono">consent-labels.ts</span> (extend with{" "}
            <span className="font-mono">essential</span> flag detection and
            capability chip helpers).
          </p>
          <p>
            <span className="text-fg-1 font-medium">i18n keys to add:</span>{" "}
            <span className="font-mono">approvedTitle</span>,{" "}
            <span className="font-mono">approvedBody</span>,{" "}
            <span className="font-mono">deniedTitle</span>,{" "}
            <span className="font-mono">deniedBody</span>,{" "}
            <span className="font-mono">safeDefaultsHint</span>,{" "}
            <span className="font-mono">postApprovalNote</span>,{" "}
            <span className="font-mono">hubsPersonalTitle</span>,{" "}
            <span className="font-mono">hubsTeamTitle</span> (interpolated with
            hub count), <span className="font-mono">essentialChip</span>,{" "}
            <span className="font-mono">readOnlyChip</span>,{" "}
            <span className="font-mono">roleCannotUseChip</span>,{" "}
            <span className="font-mono">notRequestedTitleAgent</span>{" "}
            (interpolated with agent name).
          </p>
          <p>
            <span className="text-fg-1 font-medium">Backend tag:</span>{" "}
            <span className="font-mono">
              packages/server/internal/handler/mcp_oauth.go
            </span>{" "}
            <span className="font-mono">consentPermissions()</span> needs to
            flag essential permissions (currently it doesn&apos;t — all
            requested permissions are returned as togglable). The frontend uses
            the new <span className="font-mono">essential: boolean</span> field
            to lock the checkbox. Tiny SDK type addition.
          </p>
        </div>
      </DemoCard>
    </Section>
  );
}
