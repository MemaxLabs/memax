// Maps to: hub switcher, memory attribution, management, invite, bar behavior
// Hub = ambient workspace context. Personal by default, team by intent.
// Recall crosses boundaries, push respects them.
// Roles: owner → admin → contributor → viewer
// All demos use current production patterns (flat dividers, 14px titles, icon containers).
"use client";

// Role display labels — backend values map to branded display names
const TEAM_ACCENT = "oklch(from var(--foreground) l c h / 0.24)";
const TEAM_TRACK_OFF = "oklch(from var(--foreground) l c h / 0.08)";
const TEAM_TRACK_ON = "oklch(from var(--foreground) l c h / 0.14)";
const TEAM_BORDER_OFF = "oklch(from var(--foreground) l c h / 0.12)";
const TEAM_BORDER_ON = "oklch(from var(--foreground) l c h / 0.18)";

const ROLE_LABELS: Record<string, string> = {
  owner: "Owner",
  admin: "Admin",
  contributor: "Contributor",
  viewer: "Viewer",
  member: "Contributor",
  reader: "Viewer",
};

import { useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Users,
  Copy,
  Plus,
  Clock,
  Inbox,
  FileText,
  Image as ImageIcon,
  Globe,
  User,
  Bot,
  ArrowRight,
  Pencil,
  RefreshCw,
  Shield,
  X,
} from "lucide-react";
import { DREAM_PURPLE, DREAM_BLUE, DREAM_PINK } from "@/tokens/dream";
import { getHubAccentStyle } from "@/tokens/hubs";
import { MemaxWordmark, Surface } from "@memaxlabs/ui";

const NEUTRAL_DOT = "oklch(from var(--foreground) l c h / 0.2)";

/* ── 20a. Hub Switcher — anchored beside avatar (top-right) ── */

function HubIcon({
  kind,
  label,
  accent,
}: {
  kind: "personal" | "team";
  label: string;
  accent?: string;
}) {
  const accentStyle = getHubAccentStyle(accent, kind);
  return (
    <div
      className="flex h-5 w-5 shrink-0 items-center justify-center rounded-md text-[10px] font-medium"
      style={{
        background: accentStyle.background,
        color: accentStyle.foreground,
      }}
    >
      {label.trim() || "?"}
    </div>
  );
}

function HubTypeTag({ kind }: { kind: "personal" | "team" }) {
  return (
    <span className="shrink-0 rounded-md bg-surface-2 px-1.5 py-0.5 text-[10px] font-medium text-fg-3">
      {kind === "personal" ? "Personal" : "Team"}
    </span>
  );
}

function HubSwitcherDemo() {
  return (
    <div className="space-y-6">
      {/* Single hub — no hub chip beside avatar */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Single hub user (no hub chip)
        </p>
        <div
          className="w-[220px] rounded-xl p-3 space-y-2"
          style={{
            background: "var(--card)",
            border: "1px solid var(--border)",
            boxShadow:
              "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
          }}
        >
          <div className="flex items-center gap-2">
            <div
              className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              D
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[14px] font-medium text-foreground truncate">
                Derek
              </p>
              <p className="text-[12px] text-fg-3">52 memories</p>
            </div>
          </div>
          <div className="border-t border-border/30 pt-2 space-y-1">
            <div className="text-[13px] text-fg-3 px-1">🌙 Dark</div>
            <div className="text-[13px] text-fg-3 px-1">EN / 中</div>
          </div>
        </div>
      </div>

      {/* Multi hub — hub switcher chip sits beside avatar; dropdown stays account-only */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Multi-hub user (chip beside avatar, not inside dropdown)
        </p>
        <div
          className="w-[220px] rounded-xl p-3 space-y-2"
          style={{
            background: "var(--card)",
            border: "1px solid var(--border)",
            boxShadow:
              "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
          }}
        >
          <div className="flex items-center gap-2">
            <div
              className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              D
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[14px] font-medium text-foreground truncate">
                Derek
              </p>
              <p className="text-[12px] text-fg-3">
                backend-team · 31 memories
              </p>
            </div>
          </div>
          {/* Hub list */}
          <div className="border-t border-border/30 pt-2">
            {[
              {
                name: "Personal",
                type: "personal" as const,
                count: 52,
                active: false,
              },
              {
                name: "backend-team",
                type: "team" as const,
                count: 31,
                active: true,
              },
              {
                name: "design-team",
                type: "team" as const,
                count: 12,
                active: false,
              },
            ].map((hub) => (
              <div
                key={hub.name}
                className={`flex items-center gap-2 px-2 py-1.5 rounded-lg text-[14px] cursor-pointer transition-colors ${
                  hub.active
                    ? "text-foreground bg-surface-2"
                    : "text-fg-2 hover:text-fg-2 hover:bg-surface-1"
                }`}
              >
                <HubIcon kind={hub.type} label={hub.name} />
                <span className="flex-1 truncate">{hub.name}</span>
                <span className="text-[12px] text-fg-4 tabular-nums">
                  {hub.count}
                </span>
                {hub.active && <Check className="h-3 w-3 text-fg-2" />}
              </div>
            ))}
            <div className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[14px] text-fg-3 cursor-pointer hover:text-fg-2 hover:bg-surface-1 transition-colors mt-0.5">
              <Plus className="h-3 w-3" />
              <span>Create team hub</span>
            </div>
          </div>
          <div className="border-t border-border/30 pt-2 space-y-1">
            <div className="text-[13px] text-fg-3 px-1">🌙 Dark</div>
            <div className="text-[13px] text-fg-3 px-1">EN / 中</div>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20b. Memory Attribution — hub badge on rows ── */

function MemoryAttributionDemo() {
  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{ border: "1px solid var(--border)", background: "var(--card)" }}
    >
      <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
        <Clock className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide flex-1">
          Recent
        </span>
      </div>
      <div>
        {/* Team memory — attribution format: avatar + name via agent + title */}
        <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors">
          <div className="flex items-center gap-2">
            <div
              className="h-4 w-4 rounded-full flex items-center justify-center text-[8px] font-medium text-fg-2 shrink-0"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.08)",
              }}
            >
              D
            </div>
            <span className="text-[13px] text-fg-3 shrink-0">
              Derek <span className="text-fg-4">via Claude Code</span>
            </span>
            <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
              API authentication flow
            </span>
            <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
              3d
            </span>
            <Copy className="h-3 w-3 text-fg-4 ml-0.5" />
          </div>
          <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-6">
            OAuth2 refresh flow with token rotation strategy...
          </p>
        </div>

        {/* Personal memory — kind + title format (no attribution) */}
        <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 flex items-center justify-center shrink-0">
              <span
                className="h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: NEUTRAL_DOT }}
              />
            </span>
            <span className="text-[13px] text-fg-3 shrink-0">architecture</span>
            <span className="text-fg-4 text-[13px] shrink-0">·</span>
            <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
              Personal auth notes
            </span>
            <span className="text-[10px] text-fg-3 shrink-0">web</span>
            <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
              5d
            </span>
            <Copy className="h-3 w-3 text-fg-4 ml-0.5" />
          </div>
        </div>

        {/* Processing memory with hub target */}
        <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
          <div className="flex items-center gap-2">
            <span
              className="h-3 w-3 flex items-center justify-center text-[10px] leading-none state-slow-breathe shrink-0"
              style={{ color: "var(--signature)" }}
            >
              ✦
            </span>
            <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
              Deploy runbook update
            </span>
            <span
              className="text-[13px] shrink-0 state-slow-breathe"
              style={{ color: "var(--signature)" }}
            >
              organizing...
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20c. Bar Behavior — hub-aware push + recall ── */

function HubAwareBarDemo() {
  return (
    <div className="space-y-4">
      {/* Push to personal — no hub label */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Push to personal hub (toast, no hub label)
        </p>
        <div
          className="rounded-xl px-4 py-2.5"
          style={{ background: "oklch(0.45 0.12 145 / 0.08)" }}
        >
          <span className="text-[14px] text-fg-2">
            Sent — memax will organize.
          </span>
        </div>
      </div>

      {/* Push to team hub — shows hub name */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Push to team hub (toast with target)
        </p>
        <div
          className="rounded-xl px-4 py-2.5"
          style={{ background: "oklch(0.45 0.12 145 / 0.08)" }}
        >
          <span className="text-[14px] text-fg-2">
            Sent → backend-team — memax will organize.
          </span>
        </div>
      </div>

      {/* Recall results with hub attribution */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Recall results (cross-hub, shows attribution)
        </p>
        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div>
            {[
              {
                title: "API authentication flow",
                score: 92,
                hub: "backend-team",
                group: NEUTRAL_DOT,
              },
              {
                title: "My personal auth notes",
                score: 87,
                hub: null,
                group: NEUTRAL_DOT,
              },
              {
                title: "Auth middleware architecture",
                score: 84,
                hub: "backend-team",
                group: NEUTRAL_DOT,
              },
            ].map((r, i) => (
              <div
                key={r.title}
                className={`flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${i > 0 ? "border-t border-border/30" : ""}`}
              >
                <span className="h-3 w-3 flex items-center justify-center shrink-0">
                  <span
                    className="h-1.5 w-1.5 rounded-full"
                    style={{ backgroundColor: r.group }}
                  />
                </span>
                <span className="text-[15px] text-fg-2 flex-1 truncate">
                  {r.title}
                </span>
                {r.hub && (
                  <span className="text-[12px] text-fg-4 shrink-0">
                    <Users className="h-2.5 w-2.5 inline mr-0.5" />
                    {r.hub}
                  </span>
                )}
                <span className="text-[12px] text-fg-3 tabular-nums shrink-0">
                  {r.score}%
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20d. Hub Creation — inline in settings Account tab ── */

function HubCreationDemo() {
  const hubs = [
    { name: "backend-team", memories: 31, role: "Owner" },
    { name: "design-ops", memories: 12, role: "Admin" },
    { name: "memax-test", memories: 1, role: "Contributor" },
  ];

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4 rounded-xl border border-border/60 px-4 py-4">
        <div>
          <p className="text-[15px] font-medium text-fg-2">Team hubs</p>
          <p className="text-[13px] text-fg-3 mt-1">
            Open a hub to manage members, roles, invites, permissions, and
            ownership.
          </p>
        </div>
        <button className="inline-flex items-center gap-2 rounded-xl bg-surface-3 px-3.5 py-2.5 text-[14px] font-medium text-fg-1 hover:bg-surface-2 transition-colors cursor-pointer shrink-0">
          <Plus className="h-3.5 w-3.5" />
          Create team hub
        </button>
      </div>

      <div className="rounded-xl border border-border/60 overflow-hidden">
        {hubs.map((hub, index) => (
          <button
            key={hub.name}
            className={`w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-surface-1 transition-colors cursor-pointer ${index > 0 ? "border-t border-border/30" : ""}`}
          >
            <div
              className="h-9 w-9 rounded-xl flex items-center justify-center shrink-0"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.05)",
              }}
            >
              <Users className="h-4 w-4 text-fg-3" />
            </div>
            <div className="min-w-0 flex-1">
              <p className="text-[15px] font-medium text-fg-2 truncate">
                {hub.name}
              </p>
              <p className="text-[13px] text-fg-3 mt-0.5">
                {hub.memories} shared memories
              </p>
            </div>
            <span className="text-[12px] px-2 py-0.5 rounded bg-surface-2 text-fg-2 shrink-0">
              {hub.role}
            </span>
            <ChevronRight className="h-4 w-4 text-fg-4 shrink-0" />
          </button>
        ))}
      </div>

      <div className="rounded-xl border border-border/60 px-4 py-4">
        <p className="text-[14px] text-fg-2 font-medium mb-3">
          Create team hub
        </p>
        <input
          type="text"
          placeholder="Hub name"
          defaultValue="design-team"
          className="w-full text-[15px] text-foreground bg-surface-2 border border-border/40 rounded-lg px-3 py-2 mb-2 outline-none focus:border-foreground/20"
        />
        <p className="text-[13px] text-fg-2 mb-3">
          Your team will share a knowledge base. Invite members after creation.
        </p>
        <div className="flex justify-end gap-2">
          <button className="text-[14px] text-fg-3 px-3 py-1.5 rounded-lg hover:bg-surface-2 transition-colors cursor-pointer">
            Cancel
          </button>
          <button className="text-[14px] text-foreground font-medium bg-surface-3 px-3 py-1.5 rounded-lg hover:bg-surface-2 transition-colors cursor-pointer">
            Create
          </button>
        </div>
      </div>
    </div>
  );
}

/* ── 20e. Hub Management — members + invite lifecycle + rename ── */

function HubManagementDemo() {
  const members = [
    { name: "Derek", role: "owner", avatar: "D", time: "2w ago", isMe: true },
    { name: "Sarah", role: "admin", avatar: "S", time: "1w ago", isMe: false },
    {
      name: "James",
      role: "contributor",
      avatar: "J",
      time: "3d ago",
      isMe: false,
    },
    {
      name: "Alex",
      role: "contributor",
      avatar: "A",
      time: "3d ago",
      isMe: false,
    },
    { name: "Priya", role: "viewer", avatar: "P", time: "1d ago", isMe: false },
  ];

  const invites = [
    { role: "contributor", expires: "Apr 14, 2026", token: "a8f3c2e9" },
    { role: "viewer", expires: "Apr 16, 2026", token: "b7d4e1f0" },
  ];

  const roleColors: Record<string, string> = {
    owner: "text-fg-2 bg-surface-3",
    admin: "text-fg-2 bg-surface-2",
    contributor: "text-fg-3 bg-surface-2",
    viewer: "text-fg-3 bg-surface-2",
    member: "text-fg-3 bg-surface-2",
    reader: "text-fg-3 bg-surface-2",
  };

  return (
    <div className="space-y-4">
      {/* Header — back + hub name + team badge + rename */}
      <div className="flex items-center gap-3">
        <ChevronLeft className="h-4 w-4 text-fg-3" />
        <Users className="h-4 w-4 text-fg-3" />
        <span
          className="text-[18px] font-bold text-foreground flex-1"
          style={{ letterSpacing: "-0.02em" }}
        >
          backend-team
        </span>
        <span className="text-[13px] text-fg-3 border border-border rounded-lg px-2 py-0.5">
          team
        </span>
        <button className="p-1 text-fg-3 hover:text-fg-2 transition-colors cursor-pointer">
          <Pencil className="h-3.5 w-3.5" />
        </button>
      </div>
      <p className="text-[14px] text-fg-3">5 members · 31 memories</p>

      <div className="grid gap-2 sm:grid-cols-3">
        {[
          { label: "Your role", value: "Owner" },
          { label: "Members", value: "5" },
          { label: "Shared memories", value: "31" },
        ].map((item) => (
          <div
            key={item.label}
            className="rounded-xl border border-border/60 px-4 py-3"
            style={{ background: "oklch(from var(--foreground) l c h / 0.02)" }}
          >
            <p className="text-[12px] text-fg-3 uppercase tracking-wider">
              {item.label}
            </p>
            <p className="text-[18px] font-semibold text-fg-2 mt-1">
              {item.value}
            </p>
          </div>
        ))}
      </div>

      {/* Members — flat dividers, hover-to-reveal remove for non-owners */}
      <div>
        <p className="text-[13px] text-fg-3 uppercase tracking-wider mb-2">
          Members
        </p>
        <div>
          <div className="rounded-xl border border-border/60 overflow-hidden">
            {members.map((m, i) => (
              <div
                key={m.name}
                className={`flex items-center gap-3 px-4 py-3 group ${i > 0 ? "border-t border-border/30" : ""}`}
              >
                <div
                  className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  {m.avatar}
                </div>
                <span className="text-[15px] text-fg-2 font-medium flex-1">
                  {m.name}
                  {m.isMe && (
                    <span className="text-fg-4 text-[13px] ml-1.5">(you)</span>
                  )}
                </span>
                <span className="text-[13px] text-fg-4 tabular-nums">
                  {m.time}
                </span>
                <div className="flex items-center gap-2 shrink-0">
                  <button
                    className={`inline-flex items-center gap-1 text-[12px] px-2 py-0.5 rounded transition-colors ${roleColors[m.role]}`}
                  >
                    {ROLE_LABELS[m.role] ?? m.role}
                    {m.role !== "owner" && !m.isMe && (
                      <ChevronDown className="h-3 w-3" />
                    )}
                  </button>
                  {m.role !== "owner" && !m.isMe && (
                    <button className="text-[12px] text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer">
                      Remove
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div>
        <p className="text-[13px] text-fg-3 uppercase tracking-wider mb-2">
          Ownership
        </p>
        <div className="rounded-xl border border-border/60 px-4 py-4 space-y-3">
          <div>
            <p className="text-[14px] font-medium text-fg-2">
              Transfer pending
            </p>
            <p className="text-[13px] text-fg-3 mt-1">
              Sarah needs to accept ownership before the role changes.
            </p>
            <p className="text-[12px] text-fg-4 mt-1">Expires Apr 14, 2026</p>
          </div>
          <div className="flex items-center gap-2">
            <button className="text-[13px] text-fg-3 hover:text-fg-2 px-3 py-1.5 rounded-lg bg-surface-1 hover:bg-surface-2 transition-colors cursor-pointer">
              Cancel transfer
            </button>
          </div>
        </div>
      </div>

      {/* Remove member confirmation */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Member removal confirmation
        </p>
        <div
          className="flex items-center justify-between rounded-xl px-4 py-3 transition-colors"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.06)",
          }}
        >
          <span className="text-[13px] text-fg-2">Remove James?</span>
          <div className="flex items-center gap-2.5 shrink-0">
            <button className="text-[13px] text-destructive/60 hover:text-destructive font-medium cursor-pointer">
              Remove
            </button>
            <button className="text-[13px] text-fg-2 hover:text-foreground font-medium cursor-pointer">
              Keep
            </button>
          </div>
        </div>
      </div>

      {/* Invite management — list active invites with actions */}
      <div>
        <p className="text-[13px] text-fg-3 uppercase tracking-wider mb-2">
          Invite links
        </p>
        <div className="space-y-2">
          {invites.map((inv) => (
            <div
              key={inv.token}
              className="rounded-xl border border-border/60 px-4 py-3"
            >
              {/* Link + copy */}
              <div className="flex items-center gap-2">
                <code className="text-[13px] text-fg-2 bg-surface-2 px-2.5 py-1.5 rounded-lg flex-1 truncate font-mono">
                  https://memax.app/invite/{inv.token}...
                </code>
                <button className="text-fg-3 hover:text-fg-2 transition-colors cursor-pointer p-1.5 shrink-0">
                  <Copy className="h-3.5 w-3.5" />
                </button>
              </div>
              {/* Meta — role + expiry + actions */}
              <div className="flex items-center gap-3 mt-2 text-[12px] text-fg-3">
                <span className="flex items-center gap-1">
                  <Shield className="h-2.5 w-2.5" />
                  {ROLE_LABELS[inv.role] ?? inv.role}
                </span>
                <span className="text-fg-4">·</span>
                <span>Expires {inv.expires}</span>
                <span className="flex-1" />
                <button className="flex items-center gap-1 text-fg-3 hover:text-fg-2 transition-colors cursor-pointer">
                  <RefreshCw className="h-2.5 w-2.5" />
                  Regenerate
                </button>
                <button className="text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer">
                  Revoke
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Revoke invite confirmation */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Revoke invite confirmation
        </p>
        <div
          className="flex items-center justify-between rounded-xl px-3.5 py-3 transition-colors"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
          }}
        >
          <span className="text-[13px] text-fg-2">
            Revoke this invite link?
          </span>
          <div className="flex items-center gap-2.5 shrink-0">
            <button className="text-[13px] text-destructive/60 hover:text-destructive font-medium cursor-pointer">
              Revoke
            </button>
            <button className="text-[13px] text-fg-2 hover:text-foreground font-medium cursor-pointer">
              Keep
            </button>
          </div>
        </div>
      </div>

      {/* Generate new invite (below existing ones) */}
      <button className="w-full py-2.5 rounded-xl text-[14px] font-medium text-fg-2 bg-surface-1 hover:bg-surface-2 transition-colors cursor-pointer">
        Generate link
      </button>

      <div>
        <p className="text-[13px] text-fg-3 uppercase tracking-wider mb-2">
          Contributor permissions
        </p>
        <div className="rounded-xl border border-border/60 overflow-hidden">
          <div className="flex items-center justify-between px-4 py-3">
            <div>
              <p className="text-[14px] text-fg-2">Memory deletion</p>
              <p className="text-[12px] text-fg-3 mt-0.5">
                Contributors can delete memories they contributed
              </p>
            </div>
            <span className="text-[12px] px-2 py-0.5 rounded bg-surface-2 text-fg-2">
              Own memories only
            </span>
          </div>
          <div className="border-t border-border/30 px-4 py-3 flex items-center justify-between gap-3">
            <div>
              <p className="text-[14px] text-fg-2">Manage topics</p>
              <p className="text-[12px] text-fg-3 mt-0.5">
                Contributors can create and organize topics
              </p>
            </div>
            <div
              className="relative h-6 w-10 rounded-full border shrink-0"
              style={{
                borderColor: TEAM_BORDER_ON,
                backgroundColor: TEAM_TRACK_ON,
              }}
            >
              <div
                className="absolute top-0.5 right-0.5 h-4.5 w-4.5 rounded-full"
                style={{ backgroundColor: "var(--card)" }}
              />
            </div>
          </div>
        </div>
      </div>

      <div>
        <p className="text-[13px] text-fg-3 uppercase tracking-wider mb-2">
          Danger zone
        </p>
        <div
          className="rounded-xl px-4 py-4"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
          }}
        >
          <p className="text-[14px] text-fg-2 mb-2">
            Permanently delete backend-team and all 31 memories. 4 members will
            lose access.
          </p>
          <p className="text-[13px] text-fg-3 mb-4">
            Personal memories are not affected.
          </p>
          <button className="text-[14px] text-destructive/70 hover:text-destructive font-medium cursor-pointer">
            Delete forever
          </button>
        </div>
      </div>
    </div>
  );
}

/* ── 20f. Invite Accept Page ── */

function InviteAcceptDemo() {
  return (
    <div className="flex justify-center">
      <div
        className="w-[300px] rounded-2xl px-8 py-10 text-center"
        style={{
          background: "var(--card)",
          border: "1px solid var(--border)",
          boxShadow: "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
        }}
      >
        <MemaxWordmark height={18} className="text-foreground mx-auto mb-6" />

        <p
          className="text-[19px] font-bold text-foreground mb-4"
          style={{ letterSpacing: "-0.02em" }}
        >
          Join backend-team
        </p>

        <div className="text-[14px] text-fg-3 space-y-1 mb-6">
          <p>31 shared memories</p>
          <p>5 members</p>
          <p>Invited by Derek</p>
        </div>

        <p className="text-[13px] text-fg-3 mb-6">
          Search and contribute to
          <br />
          your team&apos;s shared knowledge.
        </p>

        <button className="w-full py-2.5 rounded-xl text-[15px] font-semibold text-background bg-foreground hover:bg-foreground/90 transition-colors cursor-pointer">
          Join backend-team
        </button>

        <p className="text-[12px] text-fg-3 mt-4">Expires Apr 12, 2026</p>
      </div>
    </div>
  );
}

/* ── 20g. Hub-scoped Inbox — dream-aware states ── */

function HubInboxDemo() {
  return (
    <div className="grid grid-cols-2 gap-4">
      {/* Team hub inbox — has items */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Team inbox (has items)
        </p>
        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
            <Inbox className="h-3.5 w-3.5 text-fg-3 shrink-0" />
            <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide flex-1">
              Inbox
            </span>
            <span className="text-[13px] text-fg-3 tabular-nums">4</span>
          </div>
          <p className="px-4 pb-2 text-[13px] text-fg-3">
            Will be organized in the next dream cycle
          </p>
          <p className="px-4 pb-2 text-[13px] text-fg-3">
            Will be organized in the next dream cycle
          </p>
          <div>
            {[
              { title: "Staging deploy checklist", age: "2h", actor: "D" },
              { title: "Meeting notes — API review", age: "5h", actor: "S" },
            ].map((m, i) => (
              <div
                key={i}
                className={`px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${i > 0 ? "border-t border-border/30" : ""}`}
              >
                <div className="flex items-center gap-2">
                  <span className="h-3 w-3 flex items-center justify-center shrink-0">
                    <span
                      className="h-1.5 w-1.5 rounded-full"
                      style={{ backgroundColor: NEUTRAL_DOT }}
                    />
                  </span>
                  <span className="text-[13px] text-fg-3 shrink-0">note</span>
                  <span className="text-fg-4 text-[13px] shrink-0">·</span>
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    {m.title}
                  </span>
                  <div
                    className="h-[18px] w-[18px] rounded-full flex items-center justify-center text-[9px] font-medium text-fg-1 shrink-0"
                    style={{
                      background: "oklch(from var(--foreground) l c h / 0.08)",
                    }}
                    title={m.actor}
                  >
                    {m.actor}
                  </div>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    {m.age}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Empty inbox — clean state */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Inbox clean
        </p>
        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
            <Inbox className="h-3.5 w-3.5 text-fg-3 shrink-0" />
            <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide flex-1">
              Inbox
            </span>
            <span className="text-[13px] text-fg-3 tabular-nums">0</span>
          </div>
          <div className="flex flex-col items-center text-center py-4 px-4 gap-1.5">
            <span style={{ color: DREAM_PURPLE, fontSize: 15 }}>✦</span>
            <p className="text-[14px] text-fg-2">Your memory is clean</p>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20h. Hub Creation States ── */

function HubCreationStatesDemo() {
  return (
    <div className="grid grid-cols-3 gap-4">
      {/* Creating — button disabled, text changes to "Creating..." */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Creating
        </p>
        <div className="rounded-xl border border-border/60 px-4 py-4">
          <input
            type="text"
            value="design-team"
            readOnly
            className="w-full text-[15px] text-fg-3 bg-surface-2 border border-border/40 rounded-lg px-3 py-2 mb-3"
          />
          <div className="flex justify-end">
            <button
              disabled
              className="text-[14px] text-fg-3 font-medium bg-surface-2 px-3 py-1.5 rounded-lg opacity-60 flex items-center gap-1.5"
            >
              <span
                className="text-[12px] state-fast-pulse"
                style={{ color: SIGNATURE }}
              >
                ✦
              </span>
              Creating...
            </button>
          </div>
        </div>
      </div>

      {/* Error */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Error
        </p>
        <div className="rounded-xl border border-border/60 px-4 py-4">
          <input
            type="text"
            value="backend-team"
            readOnly
            className="w-full text-[15px] text-foreground bg-surface-2 border border-border/40 rounded-lg px-3 py-2 mb-1"
          />
          <p className="text-[13px] text-destructive/60 mb-3">
            Hub name already taken
          </p>
          <div className="flex justify-end">
            <button className="text-[14px] text-foreground font-medium bg-surface-3 px-3 py-1.5 rounded-lg">
              Create
            </button>
          </div>
        </div>
      </div>

      {/* Success → redirects to management */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Success → management
        </p>
        <div className="rounded-xl border border-border/60 px-4 py-4 flex flex-col items-center gap-2">
          <span className="text-[14px] text-fg-2">design-team created</span>
          <span className="text-[12px] text-fg-3">
            Auto-switches + opens management
          </span>
        </div>
      </div>
    </div>
  );
}

/* ── 20i. Empty Hub Onboarding ── */

function EmptyHubOnboardingDemo() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <ChevronLeft className="h-4 w-4 text-fg-3" />
        <Users className="h-4 w-4 text-fg-3" />
        <span
          className="text-[18px] font-bold text-foreground"
          style={{ letterSpacing: "-0.02em" }}
        >
          design-team
        </span>
      </div>
      <p className="text-[14px] text-fg-3">1 member · 0 memories · just now</p>

      {/* Invite CTA — replaces member list when alone */}
      <div className="rounded-xl border border-border/60 px-6 py-6 flex flex-col items-center gap-2 text-center">
        <Users className="h-5 w-5 text-fg-4" />
        <p className="text-[14px] font-medium text-fg-2">
          Invite your first teammate
        </p>
        <p className="text-[13px] text-fg-3">
          Share a link to start building shared knowledge.
        </p>
        <button className="mt-2 w-full py-2.5 rounded-xl text-[14px] font-medium text-fg-2 hover:text-fg-1 bg-surface-1 hover:bg-surface-2 transition-colors cursor-pointer">
          Generate link
        </button>
      </div>
    </div>
  );
}

/* ── 20j. Invite Link States ── */

function InviteLinkStatesDemo() {
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-4">
        {/* Not generated */}
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Not generated
          </p>
          <button className="w-full py-2.5 rounded-xl text-[14px] font-medium text-fg-2 bg-surface-1 hover:bg-surface-2 transition-colors cursor-pointer">
            Generate link
          </button>
        </div>

        {/* Generating — same button shape, disabled with fast-pulse ✦ */}
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Generating
          </p>
          <button
            disabled
            className="w-full py-2.5 rounded-xl text-[14px] font-medium text-fg-3 bg-surface-1 flex items-center justify-center gap-1.5 opacity-60"
          >
            <span
              className="text-[12px] state-fast-pulse"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            Generating...
          </button>
        </div>

        {/* Generated */}
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Generated
          </p>
          <div className="flex items-center gap-2">
            <code className="text-[12px] text-fg-2 bg-surface-2 px-2 py-1.5 rounded-lg flex-1 truncate">
              https://memax.app/invite/a8f3c2e9...
            </code>
            <button className="text-[13px] font-medium text-fg-2 hover:text-fg-2 cursor-pointer px-2 py-1 rounded-lg hover:bg-surface-2">
              Copy
            </button>
          </div>
          <p className="text-[12px] text-fg-3 mt-1.5">Expires Apr 14, 2026</p>
        </div>

        {/* Error */}
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Error
          </p>
          <div className="space-y-2">
            <button className="w-full py-2.5 rounded-xl text-[14px] font-medium text-fg-2 bg-surface-1 cursor-pointer">
              Generate link
            </button>
            <p className="text-[13px] text-destructive/60 text-center">
              Could not generate invite link.
            </p>
          </div>
        </div>
      </div>

      {/* Role picker — shown before generating */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Role picker (before generate)
        </p>
        <div className="rounded-xl border border-border/60 px-4 py-3 space-y-3">
          <p className="text-[13px] text-fg-3 font-medium">Invite as</p>
          <div className="flex gap-1.5">
            {["Contributor", "Viewer", "Admin"].map((role, idx) => (
              <button
                key={role}
                className={`text-[13px] px-3 py-1.5 rounded-lg transition-colors cursor-pointer ${
                  idx === 0
                    ? "bg-foreground text-background font-medium"
                    : "text-fg-2 bg-surface-1 hover:bg-surface-2"
                }`}
              >
                {role}
              </button>
            ))}
          </div>
          <div className="flex justify-end gap-2">
            <button className="text-[13px] text-fg-3 px-3 py-1.5 rounded-lg hover:bg-surface-2 transition-colors cursor-pointer">
              Cancel
            </button>
            <button className="text-[13px] text-foreground font-medium bg-surface-3 px-3 py-1.5 rounded-lg transition-colors cursor-pointer">
              Generate link
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20k. Invite Accept Page States ── */

function InviteAcceptStatesDemo() {
  const states = [
    {
      label: "Logged in",
      content: (
        <>
          <p
            className="text-[19px] font-bold text-foreground mb-4"
            style={{ letterSpacing: "-0.02em" }}
          >
            Join backend-team
          </p>
          <p className="text-[14px] text-fg-3 mb-6">
            5 members · Invited by Derek
          </p>
          <button className="w-full py-2.5 rounded-xl text-[15px] font-semibold text-background bg-foreground">
            Join backend-team
          </button>
        </>
      ),
    },
    {
      label: "Not logged in",
      content: (
        <>
          <p
            className="text-[19px] font-bold text-foreground mb-4"
            style={{ letterSpacing: "-0.02em" }}
          >
            Join backend-team
          </p>
          <p className="text-[14px] text-fg-3 mb-6">
            5 members · Invited by Derek
          </p>
          <button className="w-full py-2.5 rounded-xl text-[15px] font-semibold text-background bg-foreground">
            Login to join
          </button>
          <p className="text-[12px] text-fg-4 mt-2">
            → OAuth → returnTo in localStorage → auto-accept on return
          </p>
        </>
      ),
    },
    {
      label: "Expired",
      content: (
        <>
          <p className="text-[18px] font-bold text-foreground mb-2">
            Invite expired
          </p>
          <p className="text-[14px] text-fg-3">
            Ask the hub owner for a new link.
          </p>
        </>
      ),
    },
    {
      label: "Accepted",
      content: (
        <>
          <p className="text-[18px] font-bold text-foreground mb-2">Joined!</p>
          <p className="text-[14px] text-fg-3">
            Redirecting to your team&apos;s memory hub...
          </p>
        </>
      ),
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-4">
      {states.map((s) => (
        <div key={s.label}>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            {s.label}
          </p>
          <div
            className="rounded-2xl px-6 py-8 text-center"
            style={{
              background: "var(--card)",
              border: "1px solid var(--border)",
              boxShadow:
                "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
            }}
          >
            <MemaxWordmark
              height={16}
              className="text-foreground mx-auto mb-4"
            />
            {s.content}
          </div>
        </div>
      ))}
    </div>
  );
}

/* ── 20l. Delete Hub Confirmation ── */

function DeleteHubDemo() {
  return (
    <div className="grid grid-cols-2 gap-4">
      {/* Step 1: button */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Default
        </p>
        <button className="text-[14px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer">
          Delete hub
        </button>
      </div>

      {/* Step 2: confirmation */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Confirmation
        </p>
        <div
          className="rounded-xl px-4 py-4 space-y-3"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.06)",
          }}
        >
          <p className="text-[14px] text-fg-2">
            Permanently delete <strong>backend-team</strong> and all{" "}
            <strong>31 memories</strong>. 4 members will lose access.
          </p>
          <p className="text-[13px] text-fg-3">
            Personal memories are not affected.
          </p>
          <div className="flex items-center gap-3">
            <button className="text-[14px] text-destructive/60 hover:text-destructive font-medium cursor-pointer">
              Delete forever
            </button>
            <button className="text-[14px] text-fg-2 hover:text-foreground font-medium cursor-pointer">
              Cancel
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20m. Role-Based Management ── */

function RoleBasedManagementDemo() {
  const members = [
    { name: "Derek", role: "owner", avatar: "D", isMe: true },
    { name: "Sarah", role: "admin", avatar: "S", isMe: false },
    { name: "James", role: "contributor", avatar: "J", isMe: false },
  ];

  const roleColors: Record<string, string> = {
    owner: "text-fg-2 bg-surface-3",
    admin: "text-fg-2 bg-surface-2",
    contributor: "text-fg-3 bg-surface-2",
    viewer: "text-fg-3 bg-surface-2",
    member: "text-fg-3 bg-surface-2",
    reader: "text-fg-3 bg-surface-2",
  };

  return (
    <div className="grid grid-cols-2 gap-6">
      {/* Owner view — rename, remove members, invite management, delete */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Owner / Admin
        </p>
        <div className="rounded-xl border border-border/60 overflow-hidden">
          {members.map((m, i) => (
            <div
              key={m.name}
              className={`flex items-center gap-3 px-4 py-3 group ${i > 0 ? "border-t border-border/30" : ""}`}
            >
              <div
                className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                {m.avatar}
              </div>
              <span className="text-[15px] text-fg-2 font-medium flex-1">
                {m.name}
                {m.isMe && (
                  <span className="text-fg-4 text-[13px] ml-1.5">(you)</span>
                )}
              </span>
              <span
                className={`text-[12px] px-2 py-0.5 rounded ${roleColors[m.role]}`}
              >
                {ROLE_LABELS[m.role] ?? m.role}
              </span>
              {/* Remove — hover-to-reveal, not for owner or self */}
              {m.role !== "owner" && !m.isMe && (
                <button className="p-0.5 text-fg-4 hover:text-destructive/70 transition-colors cursor-pointer shrink-0 opacity-0 group-hover:opacity-100">
                  <X className="h-3 w-3" />
                </button>
              )}
            </div>
          ))}
        </div>
        <div className="mt-3 space-y-2">
          <button className="w-full py-2.5 rounded-xl text-[14px] font-medium text-fg-2 bg-surface-1 cursor-pointer">
            Generate link
          </button>
          <div className="flex items-center gap-3">
            <button className="p-1 text-fg-3 hover:text-fg-2 transition-colors cursor-pointer">
              <Pencil className="h-3.5 w-3.5" />
            </button>
            <span className="text-[12px] text-fg-4">Rename hub</span>
          </div>
          <button className="text-[14px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer">
            Delete hub
          </button>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Full control: rename, remove members, manage invites
          (list/revoke/regenerate), delete hub.
        </p>
      </div>

      {/* Member view — read-only members, leave only */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Contributor / Viewer
        </p>
        <div className="rounded-xl border border-border/60 overflow-hidden">
          {members.map((m, i) => (
            <div
              key={m.name}
              className={`flex items-center gap-3 px-4 py-3 ${i > 0 ? "border-t border-border/30" : ""}`}
            >
              <div
                className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                {m.avatar}
              </div>
              <span className="text-[15px] text-fg-2 font-medium flex-1">
                {m.name}
              </span>
              <span
                className={`text-[12px] px-2 py-0.5 rounded ${roleColors[m.role]}`}
              >
                {ROLE_LABELS[m.role] ?? m.role}
              </span>
            </div>
          ))}
        </div>
        <div className="mt-3">
          <button className="text-[14px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer">
            Leave hub
          </button>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          No invite button. No rename. No delete. No remove. Read-only member
          list + leave only.
        </p>
      </div>
    </div>
  );
}

/* ── 20n. Hub Switch Feedback ── */

function HubSwitchFeedbackDemo() {
  return (
    <div className="space-y-3">
      <p className="text-[12px] text-fg-3">
        After switching hub in dropdown, bar shows confirmation notification.
        Avatar subtitle updates immediately.
      </p>
      <div className="space-y-2">
        <div
          className="flex items-center gap-2 px-4 py-2 rounded-xl text-[13px]"
          style={{ background: "oklch(0.55 0.15 155 / 0.06)" }}
        >
          <span style={{ color: "oklch(0.55 0.15 155)" }}>✓</span>
          <span className="text-fg-2">Switched to backend-team</span>
        </div>
        <div
          className="flex items-center px-5 h-14 rounded-xl"
          style={{
            border: "1px solid var(--bar-border)",
            background: "var(--bar-bg, var(--card))",
            boxShadow: "var(--bar-shadow)",
          }}
        >
          <span className="text-[14px] text-fg-3">remember or ask...</span>
        </div>
      </div>
      <p className="text-[10px] text-fg-4">
        Success type, 2s auto-dismiss. Uses existing bar notification system. No
        new component needed.
      </p>
    </div>
  );
}

/* ── 20o. Hub Scope Filter — view + push ── */

function HubScopeFilterDemo() {
  const [active, setActive] = useState<
    "all" | "personal" | "backend" | "design"
  >("all");

  const hubs = [
    { id: "all", label: "All", icon: Globe, type: "all", count: 95 },
    {
      id: "personal",
      label: "Personal",
      icon: User,
      type: "personal",
      count: 52,
    },
    {
      id: "backend",
      label: "backend-team",
      icon: Users,
      type: "team",
      count: 31,
    },
    {
      id: "design",
      label: "design-team",
      icon: Users,
      type: "team",
      count: 12,
    },
  ] as const;

  const activeItem = hubs.find((h) => h.id === active)!;
  const pushTarget = active === "all" ? "Personal" : activeItem.label;

  return (
    <div className="space-y-6">
      {/* Dropdown with scope filter */}
      <div className="flex gap-6">
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Hub scope selector (in dropdown)
          </p>
          <div
            className="w-[240px] rounded-xl overflow-hidden"
            style={{
              background: "var(--card)",
              border: "1px solid var(--border)",
              boxShadow:
                "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
            }}
          >
            {/* Account header */}
            <div className="px-4 pt-3.5 pb-2.5">
              <div className="flex items-center gap-2">
                <div
                  className="h-7 w-7 rounded-full flex items-center justify-center text-[13px] font-medium text-fg-2 shrink-0"
                  style={{
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                  }}
                >
                  D
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-[14px] font-medium text-foreground truncate">
                    Derek
                  </p>
                  <p className="text-[12px] text-fg-3">
                    {activeItem.label} · {activeItem.count} memories
                  </p>
                </div>
              </div>
            </div>

            <div className="mx-3 border-t border-border/30" />

            {/* Hub scope list */}
            <div className="px-3 py-1.5">
              {hubs.map((hub) => {
                const Icon = hub.icon;
                const isActive = hub.id === active;
                return (
                  <button
                    key={hub.id}
                    onClick={() => setActive(hub.id as typeof active)}
                    className={`w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-[14px] transition-colors cursor-pointer ${
                      isActive
                        ? "text-foreground bg-surface-2 font-medium"
                        : "text-fg-2 hover:text-fg-2 hover:bg-surface-1"
                    }`}
                  >
                    <Icon className="h-3 w-3 text-fg-4 shrink-0" />
                    <span className="flex-1 text-left truncate">
                      {hub.label}
                    </span>
                    <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
                      {hub.count}
                    </span>
                    {isActive && <Check className="h-3 w-3 text-fg-3" />}
                  </button>
                );
              })}
              <button className="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-[14px] text-fg-3 hover:text-fg-2 hover:bg-surface-1 transition-colors cursor-pointer">
                <Plus className="h-3 w-3" />
                <span>Create team hub</span>
              </button>
            </div>

            <div className="mx-3 border-t border-border/30" />
            <div className="px-4 py-2 text-[12px] text-fg-4">
              Push → {pushTarget}
            </div>
          </div>
        </div>

        {/* Scope mental model */}
        <div className="flex-1">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Scope model
          </p>
          <div className="space-y-2 text-[13px]">
            <div className="flex items-center gap-2">
              <Globe className="h-3 w-3 text-fg-3" />
              <span className="text-fg-2 font-medium w-20">All</span>
              <span className="text-fg-3">Read: everything</span>
              <span className="text-fg-4 mx-1">·</span>
              <span className="text-fg-3">Push: → personal</span>
            </div>
            <div className="flex items-center gap-2">
              <User className="h-3 w-3 text-fg-3" />
              <span className="text-fg-2 font-medium w-20">Personal</span>
              <span className="text-fg-3">Read: personal only</span>
              <span className="text-fg-4 mx-1">·</span>
              <span className="text-fg-3">Push: → personal</span>
            </div>
            <div className="flex items-center gap-2">
              <Users className="h-3 w-3 text-fg-3" />
              <span className="text-fg-2 font-medium w-20">Team</span>
              <span className="text-fg-3">Read: team only</span>
              <span className="text-fg-4 mx-1">·</span>
              <span className="text-fg-3">Push: → team</span>
            </div>
            <div className="mt-3 p-2.5 rounded-lg bg-surface-1 text-[12px] text-fg-3">
              <p className="font-medium text-fg-2 mb-1">
                Recall always crosses all hubs.
              </p>
              <p>
                Regardless of view scope, recall searches everything accessible.
                Hub badges on results show origin.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── 20p. Main-view hub identity anchor ── */
//
// The only production signal of "which hub am I in" is a string change
// inside the <h2> ("Your Topics" ↔ "backend-team's Topics"). That is too
// subtle to answer the question "which hub am I broadly in" at a glance.
//
// New pattern: a persistent hub identity CHIP beside the top-right avatar.
// Shows hub icon + name + role (if team). Click opens the hub switcher
// popover from that global anchor. Page bodies stay quiet.
//
// Scales: more hubs = just changes the label. Zero noise for single-hub
// users — the chip only renders when hubs.length >= 2.
//
// h2 fork: personal → "Your Topics" (warm, possessive); team → "Topics"
// (neutral — "Your Topics" inside a shared team hub is semantically off).

/** HubIdentityChip — pill that sits beside the avatar in the top-right chrome */
function HubIdentityChip({
  kind,
  name,
  role,
  open = false,
}: {
  kind: "personal" | "team";
  name: string;
  role?: string;
  open?: boolean;
}) {
  return (
    <button
      type="button"
      className={`inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-[13px] transition-colors cursor-pointer ${
        open
          ? "bg-surface-2 text-fg-1"
          : "text-fg-2 hover:bg-surface-1 hover:text-fg-1"
      }`}
      style={{ border: "1px solid var(--border)" }}
    >
      {kind === "personal" ? (
        <User className="h-3 w-3 text-fg-3 shrink-0" />
      ) : (
        <Users className="h-3 w-3 text-fg-3 shrink-0" />
      )}
      <span className="font-medium">{name}</span>
      {role && (
        <span className="text-[10px] text-fg-4 uppercase tracking-wider">
          {role}
        </span>
      )}
      <ChevronDown className="h-3 w-3 text-fg-3 shrink-0" />
    </button>
  );
}

/** Mock switcher popover (anchored under a chip) */
function HubIdentityPopover({ active }: { active: string }) {
  const hubs = [
    { name: "Personal", type: "personal" as const, count: 52 },
    {
      name: "backend-team",
      type: "team" as const,
      count: 31,
      role: "Admin",
    },
    {
      name: "design-team",
      type: "team" as const,
      count: 12,
      role: "Contributor",
    },
  ];
  return (
    <div
      className="w-[240px] rounded-xl p-1.5"
      style={{
        background: "var(--card)",
        border: "1px solid var(--border)",
        boxShadow: "0 2px 16px rgba(0,0,0,0.06), 0 8px 40px rgba(0,0,0,0.04)",
      }}
    >
      <div className="text-[10px] text-fg-4 uppercase tracking-wider px-2 py-1.5">
        Switch hub
      </div>
      {hubs.map((h) => {
        const isActive = h.name === active;
        const showPersonalTag = h.type === "personal";
        return (
          <div
            key={h.name}
            className={`flex items-center gap-2 px-2 py-1.5 rounded-lg text-[13px] cursor-pointer transition-colors ${
              isActive
                ? "bg-surface-2 text-fg-1"
                : "text-fg-2 hover:bg-surface-1"
            }`}
          >
            <HubIcon kind={h.type} label={h.name} />
            <span className="flex-1 truncate">{h.name}</span>
            {showPersonalTag && <HubTypeTag kind={h.type} />}
            {h.role && (
              <span className="text-[10px] text-fg-4 uppercase tracking-wider">
                {h.role}
              </span>
            )}
            <span className="text-[11px] text-fg-4 tabular-nums">
              {h.count}
            </span>
            {isActive && <Check className="h-3 w-3 text-fg-2" />}
          </div>
        );
      })}
      <div className="flex items-center gap-2 px-2 py-1.5 rounded-lg text-[13px] text-fg-3 cursor-pointer hover:bg-surface-1 hover:text-fg-2 transition-colors border-t border-border/30 mt-1 pt-2">
        <Plus className="h-3 w-3" />
        <span>Create team hub</span>
      </div>
    </div>
  );
}

function MainViewMock({
  chip,
  title,
  subtitle,
}: {
  chip: React.ReactNode;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="p-4 rounded-xl bg-surface-1">
      <div className="mb-2 flex items-center justify-between gap-3">
        <h2 className="text-[18px] font-semibold text-foreground">{title}</h2>
        {chip}
      </div>
      <p className="text-[13px] text-fg-3">{subtitle}</p>
    </div>
  );
}

function ViewTitlesDemo() {
  const [active, setActive] = useState<"personal" | "team">("team");

  return (
    <div className="space-y-5">
      {/* ── Three states side-by-side ── */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Single-hub user (no chip)
          </p>
          <MainViewMock
            chip={null}
            title="Your Topics"
            subtitle="52 memories · 5 topics"
          />
          <p className="text-[10px] text-fg-4 mt-2">
            Zero noise when hubs.length &lt; 2. h2 is warm &ldquo;Your
            Topics&rdquo;.
          </p>
        </div>
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Multi-hub — personal active
          </p>
          <MainViewMock
            chip={<HubIdentityChip kind="personal" name="Personal" />}
            title="Your Topics"
            subtitle="52 memories · 5 topics"
          />
          <p className="text-[10px] text-fg-4 mt-2">
            Hub context lives in persistent top chrome, while h2 keeps the warm
            possessive &ldquo;Your Topics.&rdquo;
          </p>
        </div>
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Multi-hub — team active
          </p>
          <MainViewMock
            chip={
              <HubIdentityChip kind="team" name="backend-team" role="Admin" />
            }
            title="Topics"
            subtitle="31 memories · 3 topics"
          />
          <p className="text-[10px] text-fg-4 mt-2">
            Team h2 drops to neutral &ldquo;Topics&rdquo; — &ldquo;Your
            Topics&rdquo; inside a shared team hub is semantically off. Role tag
            stays inline on the chip (admin / owner only).
          </p>
        </div>
      </div>

      {/* ── Interactive: top chrome chip + popover ── */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Interactive — click top-right chip to open the switcher popover
        </p>
        <div className="flex items-center gap-1.5 mb-3">
          {(["personal", "team"] as const).map((s) => (
            <button
              key={s}
              onClick={() => setActive(s)}
              className={`px-2.5 py-1 rounded-md text-[11px] transition-all cursor-pointer ${
                active === s
                  ? "bg-foreground text-background font-medium"
                  : "bg-surface-2 text-fg-2 hover:bg-surface-3"
              }`}
            >
              {s === "personal" ? "Personal active" : "Team active"}
            </button>
          ))}
        </div>
        <div className="flex items-start gap-6">
          <MainViewMock
            chip={
              active === "personal" ? (
                <HubIdentityChip kind="personal" name="Personal" open />
              ) : (
                <HubIdentityChip
                  kind="team"
                  name="backend-team"
                  role="Admin"
                  open
                />
              )
            }
            title={active === "personal" ? "Your Topics" : "Topics"}
            subtitle={
              active === "personal"
                ? "52 memories · 5 topics"
                : "31 memories · 3 topics"
            }
          />
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Popover from the global top-right anchor
            </p>
            <HubIdentityPopover
              active={active === "personal" ? "Personal" : "backend-team"}
            />
          </div>
        </div>
      </div>

      {/* ── Rationale ── */}
      <div className="p-3 rounded-lg bg-surface-1 text-[12px] space-y-1.5">
        <p className="font-medium text-fg-2">Contract</p>
        <p className="text-fg-3">
          · Chip renders in the top-right chrome beside the avatar, not inside
          page bodies. The page h2 stays responsible only for page identity.
        </p>
        <p className="text-fg-3">
          · Chip is hidden entirely when{" "}
          <span className="font-mono">hubs.length &lt; 2</span>. Single-hub
          users see nothing — zero chrome.
        </p>
        <p className="text-fg-3">
          · Click opens the hub switcher popover from the global top-right
          anchor. The avatar dropdown no longer repeats the hub list.
        </p>
        <p className="text-fg-3">
          · Inside the switcher list, both personal and team keep the leading
          icon. Only personal keeps the text tag, so users can spot the default
          home quickly without duplicating team identity.
        </p>
        <p className="text-fg-3">
          · <span className="font-mono">h2</span> forks on hub kind: personal →{" "}
          <span className="font-mono">&ldquo;Your Topics&rdquo;</span> (warm,
          possessive), team →{" "}
          <span className="font-mono">&ldquo;Topics&rdquo;</span> (neutral,
          shared). Chip carries hub identity, h2 carries page identity. No more{" "}
          <span className="font-mono">
            &ldquo;{"{name}"}&apos;s Topics&rdquo;
          </span>{" "}
          interpolation fork in i18n.
        </p>
        <p className="text-fg-3">
          · Role tag shown inline only for Owner / Admin. Contributors & Viewers
          see just the hub name — keeps the chip quiet for readers.
        </p>
        <p className="text-fg-3">
          · Production mapping:{" "}
          <span className="font-mono">app/(app)/layout.tsx</span> (top-right
          chrome anchor), <span className="font-mono">settings-panel.tsx</span>{" "}
          (remove duplicated hub list),{" "}
          <span className="font-mono">topic-grid.tsx</span> (keep{" "}
          <span className="font-mono">t.topics.titlePersonal</span> vs{" "}
          <span className="font-mono">t.topics.titleTeam</span> fork only).
        </p>
      </div>
    </div>
  );
}

/* ── 20q. Push target indicator ── */

function PushTargetDemo() {
  return (
    <div className="space-y-5">
      {/* Bar with push target indicator */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Bar — team scope (push target shown)
        </p>
        <div
          className="rounded-2xl px-5 py-3 flex items-center gap-3"
          style={{
            background: "var(--card)",
            border: "1px solid var(--bar-border)",
            boxShadow: "var(--bar-shadow)",
          }}
        >
          <span className="text-fg-3 text-[14px] flex-1">
            type to remember, ask to recall...
          </span>
          <span className="text-[12px] text-fg-3 bg-surface-2 px-2 py-0.5 rounded flex items-center gap-1 shrink-0">
            <ArrowRight className="h-2.5 w-2.5" />
            backend-team
          </span>
        </div>
      </div>

      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Bar — personal scope (no indicator needed)
        </p>
        <div
          className="rounded-2xl px-5 py-3 flex items-center gap-3"
          style={{
            background: "var(--card)",
            border: "1px solid var(--bar-border)",
            boxShadow: "var(--bar-shadow)",
          }}
        >
          <span className="text-fg-3 text-[14px] flex-1">
            type to remember, ask to recall...
          </span>
        </div>
      </div>

      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Bar — &ldquo;All&rdquo; scope (push defaults to personal)
        </p>
        <div
          className="rounded-2xl px-5 py-3 flex items-center gap-3"
          style={{
            background: "var(--card)",
            border: "1px solid var(--bar-border)",
            boxShadow: "var(--bar-shadow)",
          }}
        >
          <span className="text-fg-3 text-[14px] flex-1">
            type to remember, ask to recall...
          </span>
          <span className="text-[12px] text-fg-4 bg-surface-1 px-2 py-0.5 rounded flex items-center gap-1 shrink-0">
            <ArrowRight className="h-2.5 w-2.5" />
            Personal
          </span>
        </div>
      </div>

      {/* Push toasts per scope */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
          Push toasts by scope
        </p>
        <div className="space-y-2">
          <div
            className="rounded-xl px-4 py-2.5"
            style={{ background: "oklch(0.45 0.12 145 / 0.08)" }}
          >
            <span className="text-[14px] text-fg-2">
              Sent — memax will organize.
            </span>
            <span className="text-[12px] text-fg-4 ml-2">(personal scope)</span>
          </div>
          <div
            className="rounded-xl px-4 py-2.5"
            style={{ background: "oklch(0.45 0.12 145 / 0.08)" }}
          >
            <span className="text-[14px] text-fg-2">
              Sent → backend-team — memax will organize.
            </span>
            <span className="text-[12px] text-fg-4 ml-2">(team scope)</span>
          </div>
          <div
            className="rounded-xl px-4 py-2.5"
            style={{ background: "oklch(0.45 0.12 145 / 0.08)" }}
          >
            <span className="text-[14px] text-fg-2">
              Sent — memax will organize.
            </span>
            <span className="text-[12px] text-fg-4 ml-2">
              (&ldquo;All&rdquo; scope → defaults to personal)
            </span>
          </div>
        </div>
      </div>

      {/* Decision summary */}
      <div className="p-3 rounded-lg bg-surface-1 text-[12px] space-y-1.5">
        <p className="font-medium text-fg-2">Push target rules:</p>
        <p className="text-fg-3">
          • Personal scope → push to personal hub (no indicator)
        </p>
        <p className="text-fg-3">
          • Team scope → push to that team (arrow pill indicator)
        </p>
        <p className="text-fg-3">
          • All scope → push to personal (muted indicator, tappable to switch)
        </p>
        <p className="text-fg-3">
          • Single-hub users → no scope UI, no push indicator
        </p>
      </div>
    </div>
  );
}

function OwnershipTransferDemo() {
  return (
    <div className="grid grid-cols-2 gap-4">
      <div className="rounded-xl border border-border/60 px-4 py-4 space-y-3">
        <p className="text-[14px] font-medium text-fg-2">Owner view</p>
        <p className="text-[13px] text-fg-3">
          Transfer pending to Sarah. Derek stays owner until Sarah accepts.
        </p>
        <div className="rounded-lg bg-surface-1 px-3 py-3 flex items-center justify-between">
          <div>
            <p className="text-[14px] text-fg-2 font-medium">Sarah</p>
            <p className="text-[12px] text-fg-3">
              Admin · Expires Apr 18, 2026
            </p>
          </div>
          <button className="text-[13px] text-fg-2 px-3 py-1.5 rounded-lg bg-surface-2">
            Cancel transfer
          </button>
        </div>
      </div>
      <div className="rounded-xl border border-border/60 px-4 py-4 space-y-3">
        <p className="text-[14px] font-medium text-fg-2">Target member view</p>
        <p className="text-[13px] text-fg-3">
          Sarah sees one focused decision: accept ownership or decline.
        </p>
        <div className="rounded-lg bg-surface-1 px-3 py-3">
          <p className="text-[14px] text-fg-2 font-medium mb-1">
            Take ownership of backend-team
          </p>
          <p className="text-[12px] text-fg-3 mb-3">
            Derek will become Admin after you accept.
          </p>
          <div className="flex items-center gap-2">
            <button className="text-[13px] font-medium bg-surface-3 text-fg-1 px-3 py-1.5 rounded-lg">
              Accept ownership
            </button>
            <button className="text-[13px] text-fg-3 px-3 py-1.5 rounded-lg bg-surface-2">
              Decline
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── Main section ── */

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * HubHeader — page-level identity line for /home and /topics
 *
 * Design intent:
 *   • Warm, contextual greeting based on time + delta state + hub type
 *   • Hub avatar (48–56px) as the anchor
 *   • Hub name uses the same sans heading family as production memory view
 *   • Thin stats line as trailing context
 *   • Two layout variants: "borderless" (typographic) and "aurora" (glass hero)
 *
 * Honors:
 *   • Hub is ambient context — no toolbar, no actions, no mode change
 *   • No left-border accents anywhere
 *   • No card chrome in borderless variant
 *   • Glass + aurora glow in the hero variant uses signature + dream tints
 *   • Brand voice: warm but not cheesy, contextual over generic
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

type TimeBucket =
  | "deep-night"
  | "morning"
  | "noon"
  | "afternoon"
  | "evening"
  | "late-night";
type HubHeaderState =
  | "morning-dream-delta"
  | "afternoon-clean"
  | "evening-inbox-overflow"
  | "deep-night-quiet"
  | "first-time"
  | "return-after-absence"
  | "team-morning"
  | "team-review-needed";

interface HubHeaderData {
  hubType: "personal" | "team";
  hubName: string;
  hubIcon?: string;
  userName: string;
  userInitial: string;
  stats: {
    memories: number;
    topics: number;
    inbox?: number;
    pendingReview?: number;
    members?: number;
  };
  members?: Array<{ initial: string; bg: string; fg: string }>;
  greeting: string;
  timeBucket: TimeBucket;
}

/** Signature aurora — the static, always-on, memax-branded gradient.
 *  This is the recommended default. It does not drift with time of day,
 *  it does not care about personal schedule, it is simply the ambient
 *  texture of the memax hub. Signature violet (our brand center) +
 *  soft warm magenta. Team hubs use this unconditionally.
 *
 *  The time-driven AURORA_GRADIENTS below remain as an opt-in personal
 *  preference (Settings › Appearance › "Match time of day") for personal
 *  hubs only. Ship defaults use Signature.
 */
const SIGNATURE_AURORA =
  "radial-gradient(80% 140% at 25% 20%, oklch(0.55 0.15 290 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.65 0.14 320 / 0.14), transparent 70%)";

/** Time bucket → soft aurora gradient colors. Used as background glow in the
 *  glass hero variant. The gradient is an ambient signal, not a strong branding
 *  moment — users feel the time of day rather than see it announced. */
const AURORA_GRADIENTS: Record<TimeBucket, string> = {
  "deep-night":
    "radial-gradient(80% 140% at 25% 20%, oklch(0.55 0.15 260 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.55 0.15 290 / 0.14), transparent 70%)",
  morning:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.82 0.14 75 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.78 0.14 35 / 0.16), transparent 70%)",
  noon: "radial-gradient(80% 140% at 25% 20%, oklch(0.82 0.11 195 / 0.2), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.85 0.09 170 / 0.14), transparent 70%)",
  afternoon:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.78 0.14 55 / 0.2), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.72 0.13 25 / 0.14), transparent 70%)",
  evening:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.68 0.16 25 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.6 0.16 340 / 0.16), transparent 70%)",
  "late-night":
    "radial-gradient(80% 140% at 25% 20%, oklch(0.5 0.14 285 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.55 0.14 265 / 0.16), transparent 70%)",
};

/** Canonical state → data matrix. Each entry is one fully-specified mock for
 *  a HubHeader demo variant. In production, the state is derived from live
 *  data (time, dream report, inbox count) — these are frozen examples so the
 *  visual evidence of each variant is stable in kitchen. */
const HUB_HEADER_MOCKS: Record<HubHeaderState, HubHeaderData> = {
  "morning-dream-delta": {
    hubType: "personal",
    hubName: "Personal",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 247, topics: 12, inbox: 3, pendingReview: 1 },
    greeting: "早上好，Derek。memax 昨晚整理了 4 个主题。",
    timeBucket: "morning",
  },
  "afternoon-clean": {
    hubType: "personal",
    hubName: "Personal",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 143, topics: 6 },
    greeting: "下午好，Derek。一切井井有条。",
    timeBucket: "afternoon",
  },
  "evening-inbox-overflow": {
    hubType: "personal",
    hubName: "Personal",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 171, topics: 4, inbox: 28 },
    greeting: "晚上好，Derek。收件箱里有 28 条等你整理。",
    timeBucket: "evening",
  },
  "deep-night-quiet": {
    hubType: "personal",
    hubName: "Personal",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 89, topics: 3 },
    greeting: "深夜了，Derek。深夜的想法最清澈。",
    timeBucket: "deep-night",
  },
  "first-time": {
    hubType: "personal",
    hubName: "Personal",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 0, topics: 0 },
    greeting: "欢迎使用 memax，Derek。",
    timeBucket: "afternoon",
  },
  "return-after-absence": {
    hubType: "personal",
    hubName: "Personal",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 312, topics: 14 },
    greeting: "欢迎回来，Derek。最近都想些什么？",
    timeBucket: "morning",
  },
  "team-morning": {
    hubType: "team",
    hubName: "backend-team",
    hubIcon: "🛠",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 147, topics: 6, inbox: 12, members: 5 },
    members: [
      {
        initial: "D",
        bg: "oklch(0.55 0.15 290 / 0.16)",
        fg: "oklch(0.45 0.15 290)",
      },
      {
        initial: "Z",
        bg: "oklch(0.65 0.15 145 / 0.16)",
        fg: "oklch(0.45 0.14 155)",
      },
      {
        initial: "A",
        bg: "oklch(0.68 0.17 35 / 0.16)",
        fg: "oklch(0.5 0.16 35)",
      },
    ],
    greeting: "早上好，Derek。团队今天多了 6 条新记忆。",
    timeBucket: "morning",
  },
  "team-review-needed": {
    hubType: "team",
    hubName: "backend-team",
    hubIcon: "🛠",
    userName: "Derek",
    userInitial: "D",
    stats: { memories: 147, topics: 6, inbox: 4, pendingReview: 3, members: 5 },
    members: [
      {
        initial: "D",
        bg: "oklch(0.55 0.15 290 / 0.16)",
        fg: "oklch(0.45 0.15 290)",
      },
      {
        initial: "Z",
        bg: "oklch(0.65 0.15 145 / 0.16)",
        fg: "oklch(0.45 0.14 155)",
      },
      {
        initial: "A",
        bg: "oklch(0.68 0.17 35 / 0.16)",
        fg: "oklch(0.5 0.16 35)",
      },
    ],
    greeting: "Derek，有 3 条合并冲突等你决定。",
    timeBucket: "afternoon",
  },
};

/** The hub avatar slot — personal hubs render as a circle (user identity),
 *  team hubs render as a rounded square (collective identity). Size is 52px
 *  for the borderless variant, 60px for the aurora variant. */
function HubAvatar({
  data,
  size,
  haloed = false,
}: {
  data: HubHeaderData;
  size: number;
  haloed?: boolean;
}) {
  const radius = "rounded-2xl";
  const fontSize = Math.round(size * 0.42);

  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      {/* Halo — soft signature glow behind the avatar. Only in aurora variant. */}
      {haloed && (
        <div
          className="absolute pointer-events-none"
          style={{
            inset: -Math.round(size * 0.28),
            borderRadius: "9999px",
            background:
              "radial-gradient(closest-side, oklch(0.55 0.15 290 / 0.24), transparent 70%)",
            filter: "blur(8px)",
          }}
        />
      )}
      <div
        className={`relative ${radius} flex items-center justify-center font-semibold`}
        style={{
          width: size,
          height: size,
          fontSize,
          background:
            data.hubType === "personal"
              ? "linear-gradient(135deg, oklch(0.55 0.15 290 / 0.14), oklch(0.62 0.16 260 / 0.1))"
              : "linear-gradient(135deg, oklch(0.68 0.14 35 / 0.14), oklch(0.72 0.15 55 / 0.1))",
          color:
            data.hubType === "personal"
              ? "oklch(0.45 0.15 290)"
              : "oklch(0.5 0.16 35)",
          boxShadow: haloed
            ? "0 2px 12px oklch(0.55 0.15 290 / 0.12), inset 0 0 0 1px oklch(from var(--foreground) l c h / 0.06)"
            : "inset 0 0 0 1px oklch(from var(--foreground) l c h / 0.08)",
        }}
      >
        {data.hubType === "personal" ? data.userInitial : (data.hubIcon ?? "?")}
      </div>
    </div>
  );
}

/** Overlapping member cluster — up to 3 visible + "+N" pill. Only rendered
 *  for team hubs. Uses the same circle primitive as personal avatars, sized
 *  for inline decoration. */
function TeamMemberCluster({
  members,
  totalCount,
}: {
  members: HubHeaderData["members"];
  totalCount: number;
}) {
  if (!members || members.length === 0) return null;
  const visible = members.slice(0, 3);
  const extra = Math.max(totalCount - visible.length, 0);

  return (
    <div className="flex items-center">
      {visible.map((m, i) => (
        <div
          key={i}
          className="h-5 w-5 rounded-full flex items-center justify-center text-[10px] font-medium"
          style={{
            background: m.bg,
            color: m.fg,
            marginLeft: i === 0 ? 0 : -6,
            border: "1.5px solid var(--card)",
            zIndex: 10 - i,
          }}
        >
          {m.initial}
        </div>
      ))}
      {extra > 0 && (
        <span
          className="ml-1 text-[11px] text-fg-3 tabular-nums"
          style={{ lineHeight: 1 }}
        >
          +{extra}
        </span>
      )}
    </div>
  );
}

/** Stats line — compact, ordered by usefulness, tabular-nums.
 *  Pending review gets the ✦ prefix because it is dream-derived. */
function HubHeaderStats({ data }: { data: HubHeaderData }) {
  const parts: React.ReactNode[] = [];

  if (data.stats.memories > 0) {
    parts.push(
      <span key="m" className="tabular-nums">
        {data.stats.memories} 条记忆
      </span>,
    );
  }
  if (data.stats.topics > 0) {
    parts.push(
      <span key="t" className="tabular-nums">
        {data.stats.topics} 个主题
      </span>,
    );
  }
  if (data.stats.members && data.stats.members > 0) {
    parts.push(
      <span key="mem" className="tabular-nums">
        {data.stats.members} 成员
      </span>,
    );
  }
  if (data.stats.inbox && data.stats.inbox > 0) {
    parts.push(
      <span key="i" className="tabular-nums">
        {data.stats.inbox} 待整理
      </span>,
    );
  }
  if (data.stats.pendingReview && data.stats.pendingReview > 0) {
    parts.push(
      <span
        key="r"
        className="tabular-nums font-medium"
        style={{ color: DREAM_PURPLE }}
      >
        ✦ {data.stats.pendingReview} 条待确认
      </span>,
    );
  }

  if (parts.length === 0) {
    return (
      <span className="text-[13px] text-fg-3">
        开始记录你的第一条吧 <ArrowRight className="inline h-3 w-3" />
      </span>
    );
  }

  return (
    <div className="flex items-center flex-wrap gap-x-2 text-[12px] text-fg-3">
      {parts.map((part, i) => (
        <span key={i} className="flex items-center gap-2">
          {i > 0 && <span className="text-fg-4">·</span>}
          {part}
        </span>
      ))}
    </div>
  );
}

/** The one and only HubHeader. Full-bleed banner, three aurora modes via
 *  the `aurora` prop. Content sits in a constrained container below the
 *  banner, with the avatar overlapping the banner bottom edge.
 *
 *  Three background modes:
 *    - "none"      — plain typographic header on --background. No banner
 *                    layer at all. No halo on the avatar. The minimal
 *                    escape hatch / reduced-motion fallback.
 *    - "signature" — static memax-branded signature gradient (DEFAULT).
 *                    Stable, premium, consistent across team members and
 *                    time of day.
 *    - "time"      — time-of-day driven gradient that shifts through
 *                    6 buckets. Personal hub opt-in preference only.
 */
function HubHeaderBanner({
  data,
  aurora = "signature",
  height = 180,
  layout = "inline-right",
  compact = false,
}: {
  data: HubHeaderData;
  /** Background treatment. Default "signature". */
  aurora?: "none" | "signature" | "time";
  height?: number;
  layout?: "inline-right" | "centered-notion";
  /** Mobile — shrinks banner height, avatar, typography. */
  compact?: boolean;
}) {
  const avatarSize = compact ? 56 : layout === "centered-notion" ? 80 : 72;
  const titleSize = compact ? 20 : layout === "centered-notion" ? 28 : 24;
  const greetingSize = compact ? 13 : 15;

  // Plain mode — no banner layer. Pure typographic header on --background,
  // matching the 20p-f "borderless" treatment. This is the minimal option.
  if (aurora === "none") {
    return (
      <section className="relative w-full">
        <div
          className={`mx-auto max-w-3xl ${compact ? "px-4" : "px-6"} pt-10 pb-6`}
        >
          <div className="flex items-start gap-5">
            <HubAvatar data={data} size={avatarSize} />
            <div className="min-w-0 flex-1 pt-1">
              <div className="flex items-center gap-3 mb-1.5">
                <span
                  className="font-bold text-foreground"
                  style={{
                    fontSize: titleSize,
                    letterSpacing: "-0.02em",
                  }}
                >
                  {data.hubName}
                </span>
                {data.hubType === "team" && (
                  <TeamMemberCluster
                    members={data.members}
                    totalCount={data.stats.members ?? 0}
                  />
                )}
              </div>
              <p
                className="text-fg-2 mb-2 leading-relaxed"
                style={{ fontSize: greetingSize }}
              >
                {data.greeting}
              </p>
              <HubHeaderStats data={data} />
            </div>
          </div>
        </div>
      </section>
    );
  }

  // How much of the avatar sits ABOVE the banner bottom edge.
  //   inline-right → 25% above, 75% below (title sits cleanly below banner)
  //   centered-notion → 50/50 (classic Notion emoji hang)
  const overlapAbovePx =
    layout === "centered-notion"
      ? Math.round(avatarSize * 0.5)
      : Math.round(avatarSize * 0.25);
  const bannerHeight = compact ? 140 : height;

  const gradient =
    aurora === "time" ? AURORA_GRADIENTS[data.timeBucket] : SIGNATURE_AURORA;

  return (
    <section className="relative w-full">
      {/* Aurora banner layer — full width within parent. Parent should set
          the viewport constraint (max-w + center). The banner has no side
          padding so the aurora genuinely bleeds to the parent's edges.
          The app top bar (logo + user menu) renders on top of this layer
          via absolute positioning from the parent shell — we deliberately
          do NOT render any content in the banner's top-right area. */}
      <div
        className="relative w-full overflow-hidden"
        style={{
          height: bannerHeight,
          background: gradient,
        }}
      >
        {/* Subtle noise texture — same treatment as the card aurora */}
        <div
          className="absolute inset-0 pointer-events-none opacity-[0.05]"
          style={{
            backgroundImage:
              "radial-gradient(circle at 1px 1px, var(--foreground) 0.5px, transparent 0)",
            backgroundSize: "16px 16px",
          }}
        />

        {/* Soft fade to --background at the bottom so the banner lands on
            the content area seamlessly — no hard horizontal line. */}
        <div
          className="absolute inset-x-0 bottom-0 pointer-events-none"
          style={{
            height: Math.round(bannerHeight * 0.25),
            background:
              "linear-gradient(to bottom, transparent, var(--background))",
          }}
        />
      </div>

      {/* Content container — constrained max-width, sits on --background.
          Title and body are in the clean area below the banner; only the
          avatar crosses the edge via negative margin. */}
      <div
        className={`relative mx-auto max-w-3xl ${compact ? "px-4" : "px-6"}`}
      >
        {layout === "centered-notion" ? (
          <div className="flex flex-col items-start">
            <div style={{ marginTop: -overlapAbovePx }}>
              <HubAvatar data={data} size={avatarSize} haloed />
            </div>
            <div className="mt-4 w-full pb-6">
              <div className="flex items-center gap-3 mb-1.5">
                <span
                  className="font-bold text-foreground"
                  style={{
                    fontSize: titleSize,
                    letterSpacing: "-0.02em",
                  }}
                >
                  {data.hubName}
                </span>
                {data.hubType === "team" && (
                  <TeamMemberCluster
                    members={data.members}
                    totalCount={data.stats.members ?? 0}
                  />
                )}
              </div>
              <p
                className="text-fg-2 mb-2.5 leading-relaxed"
                style={{ fontSize: greetingSize }}
              >
                {data.greeting}
              </p>
              <HubHeaderStats data={data} />
            </div>
          </div>
        ) : (
          <div className="flex items-start gap-5 pb-6">
            <div style={{ marginTop: -overlapAbovePx }}>
              <HubAvatar data={data} size={avatarSize} haloed />
            </div>
            <div className="min-w-0 flex-1 pt-1">
              <div className="flex items-center gap-3 mb-1.5">
                <span
                  className="font-bold text-foreground"
                  style={{
                    fontSize: titleSize,
                    letterSpacing: "-0.02em",
                  }}
                >
                  {data.hubName}
                </span>
                {data.hubType === "team" && (
                  <TeamMemberCluster
                    members={data.members}
                    totalCount={data.stats.members ?? 0}
                  />
                )}
              </div>
              <p
                className="text-fg-2 mb-2 leading-relaxed"
                style={{ fontSize: greetingSize }}
              >
                {data.greeting}
              </p>
              <HubHeaderStats data={data} />
            </div>
          </div>
        )}
      </div>
    </section>
  );
}

function HubHeaderAurora({ data }: { data: HubHeaderData }) {
  return <HubHeaderBanner data={data} aurora="signature" />;
}

function HubHeaderVariantPair({
  state,
  note,
}: {
  state: HubHeaderState;
  note?: string;
}) {
  const data = HUB_HEADER_MOCKS[state];

  return (
    <div className="space-y-3">
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-2">
          <p className="text-[11px] uppercase tracking-wider text-fg-3">
            Borderless
          </p>
          <div
            className="overflow-hidden rounded-2xl"
            style={{
              background: "var(--background)",
              border: "1px solid var(--border)",
            }}
          >
            <HubHeaderBanner data={data} aurora="none" />
          </div>
        </div>

        <div className="space-y-2">
          <p className="text-[11px] uppercase tracking-wider text-fg-3">
            Aurora
          </p>
          <div
            className="overflow-hidden rounded-2xl"
            style={{
              background: "var(--background)",
              border: "1px solid var(--border)",
            }}
          >
            <HubHeaderBanner data={data} aurora="signature" />
          </div>
        </div>
      </div>

      {note && <p className="text-[12px] text-fg-3">{note}</p>}
    </div>
  );
}

function HubHeaderInContext({ state }: { state: HubHeaderState }) {
  const data = HUB_HEADER_MOCKS[state];

  return (
    <div
      className="overflow-hidden rounded-2xl"
      style={{
        background: "var(--background)",
        border: "1px solid var(--border)",
      }}
    >
      <HubHeaderBanner data={data} aurora="signature" />
      <div className="p-4 pt-0">
        <div
          className="overflow-hidden rounded-xl"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
            <Clock className="h-3.5 w-3.5 shrink-0 text-fg-3" />
            <span className="text-[14px] font-semibold uppercase tracking-wide text-fg-2">
              新鲜记忆
            </span>
            <span className="rounded bg-surface-1 px-1.5 py-0.5 text-[12px] text-fg-3">
              过去 12h
            </span>
          </div>
          {[
            { title: "API key rotation policy", age: "2h" },
            { title: "OAuth refresh token audit", age: "5h" },
            { title: "SAML callback boundary fix", age: "7h" },
          ].map((row, index) => (
            <div
              key={row.title}
              className={`flex items-center gap-3 px-4 py-2.5 ${
                index > 0 ? "border-t border-border/20" : ""
              }`}
            >
              <span className="flex-1 truncate text-[14px] text-fg-1">
                {row.title}
              </span>
              <span className="w-8 text-right text-[12px] tabular-nums text-fg-4">
                {row.age}
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** Mock of the real app's top bar — logo on the left, hub dropdown + user
 *  avatar on the right. This is NOT part of HubHeaderBanner; it's the app
 *  shell. In kitchen we render it as an absolutely positioned overlay on
 *  top of the banner to demonstrate that the aurora bleeds *under* the top
 *  bar. In production the top bar lives in the layout shell, and the
 *  banner is simply its first child in the main content area — the same
 *  visual stacking is achieved via normal flow. */
function MockAppTopBar({
  hubName = "个人",
  userInitial = "J",
  onSignature = true,
}: {
  hubName?: string;
  userInitial?: string;
  /** When true, wrap elements in backdrop-blur pills so they stay legible
   *  on top of the aurora. When the header is plain (no aurora), elements
   *  render without pills so they match the normal --background treatment. */
  onSignature?: boolean;
}) {
  const pillClass = onSignature
    ? "bg-background/55 backdrop-blur-md ring-1 ring-foreground/6"
    : "";
  return (
    <div className="absolute inset-x-0 top-0 z-10 flex items-center justify-between px-6 py-4 pointer-events-none">
      {/* Logo */}
      <div
        className={`pointer-events-auto flex items-center gap-2 rounded-lg px-2 py-1.5 ${pillClass}`}
      >
        <div
          className="h-5 w-5 rounded-md"
          style={{
            background:
              "linear-gradient(135deg, oklch(0.55 0.15 290), oklch(0.62 0.14 320))",
          }}
        />
        <span className="text-[14px] font-semibold text-foreground tracking-tight">
          memax
        </span>
      </div>

      {/* Right cluster — hub dropdown + user avatar */}
      <div className="pointer-events-auto flex items-center gap-2">
        <button
          type="button"
          className={`flex items-center gap-1 rounded-md px-2 py-1.5 text-[12px] text-fg-1 hover:bg-foreground/5 transition-colors ${pillClass}`}
        >
          {hubName}
          <ChevronDown className="h-3 w-3 text-fg-3" strokeWidth={2} />
        </button>
        <div
          className={`rounded-full flex items-center justify-center text-[11px] font-semibold text-fg-1 ${pillClass}`}
          style={{
            width: 30,
            height: 30,
            background: onSignature
              ? undefined
              : "oklch(from var(--foreground) l c h / 0.1)",
          }}
        >
          {userInitial}
        </div>
      </div>
    </div>
  );
}

/** Inlined NEUTRAL control tokens. See §19 "Control color semantics" for
 *  the canonical rule block and `@memaxlabs/ui/tokens/controls` for the
 *  production source. Keep in sync. */
const NEUTRAL_INK = "var(--fg-1)";
const NEUTRAL_BORDER_OFF = "oklch(from var(--foreground) l c h / 0.18)";

/** Mock of the Settings › Account › Appearance section — the northstar
 *  that `settings-dialog.tsx` SelectionOption matches. Follows §19 control
 *  color semantics: radio-row pattern (options need descriptions) +
 *  NEUTRAL_INK active color + `border-2` ring. */
function SettingsAppearanceMock() {
  const [mode, setMode] = useState<"none" | "signature" | "time">("signature");
  const options: Array<{
    value: "none" | "signature" | "time";
    label: string;
    desc: string;
  }> = [
    {
      value: "none",
      label: "Plain",
      desc: "No banner, no aurora. Pure typography. The minimal fallback.",
    },
    {
      value: "signature",
      label: "Signature",
      desc: "Static memax-branded gradient. Stable, premium, the shipping default.",
    },
    {
      value: "time",
      label: "Match time of day",
      desc: "Aurora drifts through 6 time buckets. Personal hubs only — team hubs fall back to Signature.",
    },
  ];

  return (
    <div className="space-y-4">
      {/* APPEARANCE section — matches real Settings › Account tab */}
      <div>
        <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
          Appearance
        </p>
        <Surface variant="subtle" rounded="2xl" className="px-5 py-4">
          <div className="px-1 pb-1">
            <p className="text-[15px] text-fg-2">Hub header style</p>
            <p className="text-[13px] text-fg-3 mt-0.5">
              How the banner at the top of each hub page renders.
            </p>
          </div>
          {/* Radio rows — matches SelectionOption in settings-dialog.tsx */}
          {options.map((opt) => {
            const checked = mode === opt.value;
            return (
              <button
                key={opt.value}
                type="button"
                onClick={() => setMode(opt.value)}
                className="flex w-full items-start gap-3 rounded-lg px-1 py-3 text-left transition-colors hover:bg-surface-1 cursor-pointer"
              >
                <span
                  className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2"
                  style={{
                    borderColor: checked ? NEUTRAL_INK : NEUTRAL_BORDER_OFF,
                  }}
                >
                  {checked && (
                    <span
                      className="h-2 w-2 rounded-full"
                      style={{ background: NEUTRAL_INK }}
                    />
                  )}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-[15px] text-fg-2">{opt.label}</p>
                  <p className="mt-0.5 text-[13px] text-fg-3">{opt.desc}</p>
                </div>
              </button>
            );
          })}
        </Surface>
      </div>

      {/* PREVIEW — live banner rendered from the selected mode. Separate
           Surface so the preview has its own frame and doesn't compete with
           the settings rows above. */}
      <div>
        <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
          Preview
        </p>
        <Surface
          variant="subtle"
          rounded="2xl"
          className="overflow-hidden"
          style={{ background: "var(--background)" }}
        >
          <HubHeaderBanner
            data={HUB_HEADER_MOCKS["morning-dream-delta"]}
            aurora={mode}
            height={160}
          />
        </Surface>
      </div>
    </div>
  );
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * 07q — Hub switch from inside a topic
 * Decision: switching hub while inside a topic navigates to the new hub's
 * memory feed, not its topic grid. Topic detail is content-inspection mode;
 * Recent feed is also content-inspection mode. Continuity of job > continuity
 * of container.
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

function HubSwitchTransitionDemo() {
  const [frame, setFrame] = useState<0 | 1 | 2>(0);

  const labels = ["Old hub", "Transitioning", "Landed — Memories"] as const;

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-[11px] text-fg-3">
        <span>Tap frames:</span>
        {[0, 1, 2].map((i) => (
          <button
            key={i}
            onClick={() => setFrame(i as 0 | 1 | 2)}
            className={`px-2 py-0.5 rounded-md transition-colors cursor-pointer ${
              frame === i
                ? "bg-surface-2 text-fg-1"
                : "bg-surface-1 text-fg-3 hover:text-fg-2"
            }`}
          >
            {labels[i]}
          </button>
        ))}
      </div>

      <div
        className="relative rounded-2xl overflow-hidden"
        style={{
          background: "var(--background)",
          border: "1px solid var(--border)",
          minHeight: 360,
        }}
      >
        {/* Frame 0: old hub content */}
        {frame === 0 && (
          <div className="p-6 animate-fade-up">
            <div className="flex items-center gap-1.5 text-[12px] text-fg-3 mb-4">
              <span>Your Topics</span>
              <ChevronRight className="h-3 w-3 text-fg-4" />
              <span className="text-fg-2">Auth & Security</span>
            </div>
            <span
              className="text-[21px] font-bold text-foreground"
              style={{ letterSpacing: "-0.02em" }}
            >
              Auth & Security
            </span>
            <p className="text-[13px] text-fg-2 mt-1 mb-1">
              JWT tokens, OAuth flows, API key management…
            </p>
            <p className="text-[12px] text-fg-3">23 memories · 3 subtopics</p>
            <div
              className="mt-5 rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              {[
                { t: "API key rotation policy", age: "3h", accent: true },
                { t: "OAuth refresh token audit", age: "5h", accent: true },
                { t: "SAML callback boundary fix", age: "7h", accent: true },
                { t: "JWT validation middleware", age: "1w" },
              ].map((row, i) => (
                <div
                  key={row.t}
                  className={`flex items-center gap-2 px-4 py-2.5 ${i > 0 ? "border-t border-border/20" : ""}`}
                  style={{
                    boxShadow: row.accent
                      ? "inset 2px 0 0 oklch(0.55 0.15 290 / 0.55)"
                      : undefined,
                  }}
                >
                  <span className="text-[14px] text-fg-1 flex-1 truncate">
                    {row.t}
                  </span>
                  <span className="text-[12px] text-fg-3 tabular-nums">
                    {row.age}
                  </span>
                </div>
              ))}
            </div>
            <div
              className="absolute top-4 right-4 flex items-center gap-2 rounded-full px-2 py-1"
              style={{
                background: "oklch(from var(--card) l c h / 0.8)",
                backdropFilter: "blur(8px)",
              }}
            >
              <div
                className="h-5 w-5 rounded-full flex items-center justify-center text-[10px] font-medium"
                style={{
                  background: "oklch(0.55 0.15 290 / 0.16)",
                  color: "oklch(0.45 0.15 290)",
                }}
              >
                D
              </div>
              <span className="text-[11px] text-fg-3">Personal</span>
              <ChevronDown className="h-2.5 w-2.5 text-fg-4" />
            </div>
          </div>
        )}

        {/* Frame 1: transitioning — fade overlay */}
        {frame === 1 && (
          <div className="absolute inset-0 flex items-center justify-center">
            <div
              className="absolute inset-0"
              style={{
                background:
                  "radial-gradient(circle at 50% 50%, oklch(0.55 0.15 290 / 0.08), transparent 70%)",
              }}
            />
            <div className="relative flex items-center gap-3 animate-fade-up">
              <div className="state-slow-breathe">
                <HubIcon kind="team" label="B" accent="emerald" />
              </div>
              <span className="text-[13px] text-fg-2">
                Switching to backend-team…
              </span>
            </div>
          </div>
        )}

        {/* Frame 2: landed — Memories view for the new hub */}
        {frame === 2 && (
          <div className="p-6 animate-fade-up">
            <HubHeaderAurora data={HUB_HEADER_MOCKS["team-morning"]} />
            <div
              className="mt-4 rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
                <Clock className="h-3.5 w-3.5 text-fg-3 shrink-0" />
                <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
                  新鲜记忆
                </span>
                <span className="text-[12px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
                  过去 12h
                </span>
                <div className="flex-1" />
                <span className="text-[12px] text-fg-4">选择</span>
              </div>
              {[
                { t: "Reranker cost analysis", author: "Ziyang", age: "2h" },
                {
                  t: "Worker queue backoff tuning",
                  author: "Derek",
                  age: "4h",
                },
                { t: "Retry idempotency key design", author: "Ava", age: "6h" },
                { t: "Neon connection pool size", author: "Yusuf", age: "8h" },
              ].map((row, i) => (
                <div
                  key={row.t}
                  className={`flex items-center gap-3 px-4 py-2.5 ${i > 0 ? "border-t border-border/20" : ""}`}
                >
                  <span className="text-[14px] text-fg-1 flex-1 truncate">
                    {row.t}
                  </span>
                  <span className="text-[12px] text-fg-3">{row.author}</span>
                  <span className="text-[12px] text-fg-4 tabular-nums w-8 text-right">
                    {row.age}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      <p className="text-[11px] text-fg-3">
        Frame 1 is the fade transition (actual production is an opacity
        crossfade over ~220ms, not a topic remap and not a loading spinner).
        Frame 2 lands the user on the new hub&apos;s memories view so hub
        switches always have one destination and one mental model.
      </p>
    </div>
  );
}

function HubSwitchDropdownHintDemo() {
  return (
    <div className="space-y-3">
      <p className="text-[11px] text-fg-3">
        Subtle inline hint in the hub switcher dropdown when the user is
        currently inside a topic. Tells them what they&apos;ll{" "}
        <span className="text-fg-1">see</span>, not what they&apos;ll lose.
      </p>
      <div className="grid gap-4 md:grid-cols-2">
        <div>
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Without hint (default state)
          </p>
          <div
            className="w-[260px] rounded-xl overflow-hidden"
            style={{
              border: "1px solid var(--border)",
              background: "var(--card)",
              boxShadow: "0 12px 40px oklch(0 0 0 / 0.08)",
            }}
          >
            <div className="text-[11px] uppercase tracking-wider text-fg-3 px-3 pt-2.5 pb-1.5">
              Switch hub
            </div>
            {[
              {
                name: "Personal",
                icon: "D",
                active: true,
                type: "personal" as const,
              },
              {
                name: "backend-team",
                icon: "🛠",
                active: false,
                type: "team" as const,
              },
              {
                name: "design-team",
                icon: "🎨",
                active: false,
                type: "team" as const,
              },
            ].map((h) => (
              <button
                key={h.name}
                className={`flex items-center gap-2.5 w-full px-3 py-2.5 text-left hover:bg-surface-1 transition-colors cursor-pointer ${h.active ? "bg-surface-1" : ""}`}
              >
                <div
                  className="h-5 w-5 flex items-center justify-center rounded-md text-[11px]"
                  style={{
                    background:
                      h.type === "personal"
                        ? "oklch(0.55 0.15 290 / 0.16)"
                        : "oklch(0.68 0.14 35 / 0.14)",
                    color:
                      h.type === "personal"
                        ? "oklch(0.45 0.15 290)"
                        : "oklch(0.5 0.16 35)",
                  }}
                >
                  {h.icon}
                </div>
                <span className="text-[13px] text-fg-1 flex-1">{h.name}</span>
                {h.type === "personal" && <HubTypeTag kind={h.type} />}
                {h.active && (
                  <Check className="h-3.5 w-3.5 text-fg-3" strokeWidth={2} />
                )}
              </button>
            ))}
          </div>
        </div>

        <div>
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Inside a topic — shows what you&apos;ll see
          </p>
          <div
            className="w-[260px] rounded-xl overflow-hidden"
            style={{
              border: "1px solid var(--border)",
              background: "var(--card)",
              boxShadow: "0 12px 40px oklch(0 0 0 / 0.08)",
            }}
          >
            <div className="text-[11px] uppercase tracking-wider text-fg-3 px-3 pt-2.5 pb-1.5">
              Switch hub
            </div>
            {[
              {
                name: "Personal",
                icon: "D",
                active: true,
                type: "personal" as const,
                hint: null,
              },
              {
                name: "backend-team",
                icon: "🛠",
                active: false,
                type: "team" as const,
                hint: "显示 backend-team 最近动态",
              },
              {
                name: "design-team",
                icon: "🎨",
                active: false,
                type: "team" as const,
                hint: "显示 design-team 最近动态",
              },
            ].map((h) => (
              <button
                key={h.name}
                className={`flex items-start gap-2.5 w-full px-3 py-2.5 text-left hover:bg-surface-1 transition-colors cursor-pointer ${h.active ? "bg-surface-1" : ""}`}
              >
                <div
                  className="h-5 w-5 mt-0.5 flex items-center justify-center rounded-md text-[11px]"
                  style={{
                    background:
                      h.type === "personal"
                        ? "oklch(0.55 0.15 290 / 0.16)"
                        : "oklch(0.68 0.14 35 / 0.14)",
                    color:
                      h.type === "personal"
                        ? "oklch(0.45 0.15 290)"
                        : "oklch(0.5 0.16 35)",
                  }}
                >
                  {h.icon}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-[13px] text-fg-1">{h.name}</span>
                    {h.type === "personal" && <HubTypeTag kind={h.type} />}
                    {h.active && (
                      <Check
                        className="h-3.5 w-3.5 text-fg-3 ml-auto"
                        strokeWidth={2}
                      />
                    )}
                  </div>
                  {h.hint && (
                    <p className="text-[11px] text-fg-4 mt-0.5">{h.hint}</p>
                  )}
                </div>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export function HubExperienceSection() {
  return (
    <Section
      title="7. Hub Experience"
      description="Team hub: ambient workspace switching from the top-right chrome beside the avatar, plus attribution, management, and invite. Personal by default, team by intent."
    >
      <DemoCard label="20a. Hub switcher &mdash; top-right anchor">
        <p className="text-[12px] text-fg-2 mb-3">
          Anchored beside the avatar in the top-right chrome. Single-hub users
          see no chip. Multi-hub users get a persistent current-hub chip; the
          avatar dropdown stays account-only. Active hub shown with checkmark.
          Users icon for team hubs. &ldquo;Create team hub&rdquo; link at
          bottom.
        </p>
        <HubSwitcherDemo />
      </DemoCard>

      <DemoCard label="20b. Memory attribution &mdash; hub badge on rows">
        <p className="text-[12px] text-fg-2 mb-3">
          Team memories show lucide Users + hub name (text-[12px] text-fg-4).
          Personal memories show no badge (default = no label). Only visible
          when user has 2+ hubs. Processing row unchanged. Flat dividers, icon
          containers match production.
        </p>
        <MemoryAttributionDemo />
      </DemoCard>

      <DemoCard label="20c. Hub-aware bar behavior">
        <p className="text-[12px] text-fg-2 mb-3">
          Push toast: &ldquo;Sent — memax will organize.&rdquo; (personal) or
          &ldquo;Sent → hub-name — memax will organize.&rdquo; (team). Recall
          results show hub attribution in cross-hub mode. Bar clears immediately
          after submit (chat-style, no blocking confirmation).
        </p>
        <HubAwareBarDemo />
      </DemoCard>

      <DemoCard label="20d. Hub creation (settings Account tab)">
        <p className="text-[12px] text-fg-2 mb-3">
          Inline form in settings dialog Account tab, below existing hub list.
          Name input → auto-generated slug → Create. Hub appears in pill
          dropdown immediately. Also accessible via dropdown &ldquo;Create team
          hub&rdquo; link.
        </p>
        <HubCreationDemo />
      </DemoCard>

      <DemoCard label="20e. Hub management (settings, expandable per hub)">
        <p className="text-[12px] text-fg-2 mb-3">
          Click team hub in settings → expands to show members + invite
          lifecycle. Rename hub (Pencil icon, inline edit). Members sorted by
          role with (you) marker. Owner/admin: hover-to-reveal X for member
          removal with destructive confirmation. Role badge becomes an inline
          editor for Admin/Contributor/Viewer. Invite management: list active
          invites with role badge, expiry, copy, regenerate, revoke. Ownership
          transfer is a dedicated owner-only section with pending state.
        </p>
        <HubManagementDemo />
      </DemoCard>

      <DemoCard label="20f. Invite accept page (/invite/[token])">
        <p className="text-[12px] text-fg-2 mb-3">
          Centered card, same visual language as login. Shows hub name, memory
          count, member count, inviter. Single CTA. Expired tokens show error
          state.
        </p>
        <InviteAcceptDemo />
      </DemoCard>

      <DemoCard label="20g. Hub-scoped inbox states">
        <p className="text-[12px] text-fg-2 mb-3">
          Inbox is hub-scoped. Shows unassigned memories for active hub. Clean
          state uses DREAM_PURPLE ✦. All states match production patterns
          (Clock/Inbox icons, flat dividers, icon containers).
        </p>
        <HubInboxDemo />
      </DemoCard>

      <DemoCard label="20h. Hub creation states">
        <p className="text-[12px] text-fg-2 mb-3">
          All states for hub creation: loading (breathing ✦), error (inline
          message), success (auto-switch + open management). Maps to
          teams-section.tsx HubCreateForm.
        </p>
        <HubCreationStatesDemo />
      </DemoCard>

      <DemoCard label="20i. Empty hub onboarding (just created, alone)">
        <p className="text-[12px] text-fg-2 mb-3">
          When hub has 1 member (owner) and 0 memories, replace member list with
          prominent invite CTA. This is the first thing an owner sees after
          creating a hub.
        </p>
        <EmptyHubOnboardingDemo />
      </DemoCard>

      <DemoCard label="20j. Invite link states + role picker">
        <p className="text-[12px] text-fg-2 mb-3">
          Four states: not generated (CTA button), generating (breathing ✦),
          generated (code + copy + expiry), error (inline message). Plus role
          picker shown before generating — select Contributor/Viewer/Admin. Maps
          to teams-section.tsx InviteCreateButton.
        </p>
        <InviteLinkStatesDemo />
      </DemoCard>

      <DemoCard label="20k. Invite accept page — all states">
        <p className="text-[12px] text-fg-2 mb-3">
          Logged in: single CTA. Not logged in: &ldquo;Sign in to join&rdquo; →
          OAuth → return to page → auto-accept. Expired/used show error.
          Accepted shows redirect.
        </p>
        <InviteAcceptStatesDemo />
      </DemoCard>

      <DemoCard label="20l. Delete hub confirmation">
        <p className="text-[12px] text-fg-2 mb-3">
          Two-step: button → destructive confirmation with memory count + member
          impact. &ldquo;Personal memories are not affected.&rdquo; After
          delete: return to list, auto-switch to personal.
        </p>
        <DeleteHubDemo />
      </DemoCard>

      <DemoCard label="20m. Role-based management views">
        <p className="text-[12px] text-fg-2 mb-3">
          Owner/Admin: full management — rename hub, remove members
          (hover-to-reveal X), invite lifecycle (list/revoke/regenerate/role
          picker), delete hub. Contributor/Viewer: read-only member list + leave
          only. Roles affect ACTIONS not VIEWS — all roles see the same
          memories.
        </p>
        <RoleBasedManagementDemo />
      </DemoCard>

      <DemoCard label="20n. Ownership transfer">
        <p className="text-[12px] text-fg-2 mb-3">
          Two-step transfer only. Owner selects an existing member, the target
          explicitly accepts, and the previous owner is automatically demoted to
          Admin. No same-screen instant owner swap.
        </p>
        <OwnershipTransferDemo />
      </DemoCard>

      <DemoCard label="20o. Hub Scope Filter — view + push">
        <p className="text-[12px] text-fg-2 mb-3">
          Hub switcher = scope filter. Selection controls BOTH what you see
          (read) AND where you push (write). &ldquo;All&rdquo; is read-only
          aggregation — push defaults to personal. Single-hub users see no
          filter UI.
        </p>
        <HubScopeFilterDemo />
      </DemoCard>

      <DemoCard label="20p. Main-view hub identity anchor">
        <p className="text-[12px] text-fg-2 mb-3">
          The &ldquo;you are here&rdquo; anchor for multi-hub browsing. Hub
          identity now lives in the persistent top-right chrome beside the
          avatar, not above each page h2. h2 forks on hub kind: personal →
          &ldquo;Your Topics&rdquo; (warm), team → &ldquo;Topics&rdquo;
          (neutral).
        </p>
        <ViewTitlesDemo />
      </DemoCard>

      <DemoCard label="20q. Push target indicator">
        <p className="text-[12px] text-fg-2 mb-3">
          Bar shows where push goes. Personal scope: no indicator (default).
          Team scope: hub name pill right of input. All scope: &ldquo;→
          Personal&rdquo; default, user can tap to change. Push toast always
          confirms target.
        </p>
        <PushTargetDemo />
      </DemoCard>

      <DemoCard label="20n. Hub switch feedback">
        <p className="text-[12px] text-fg-2 mb-3">
          Uses existing bar notification system. Success type, 2s auto-dismiss.
          Avatar subtitle updates immediately. No new component.
        </p>
        <HubSwitchFeedbackDemo />
      </DemoCard>

      <DemoCard label="7r. Profile — display name edit">
        <p className="text-[12px] text-foreground/40 mb-3">
          In settings Account tab. Click name → inline edit → save on
          blur/Enter. display_name is the single source of truth (seeded from
          OAuth, editable here). Used in attribution rows, settings panel,
          avatar dropdown.
        </p>
        <div
          className="rounded-2xl overflow-hidden max-w-sm"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="px-5 py-4">
            <div className="flex items-center gap-3.5">
              <div
                className="h-10 w-10 rounded-full shrink-0 flex items-center justify-center text-[18px] font-medium text-foreground/55"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                J
              </div>
              <div className="min-w-0 flex-1">
                {/* Editable name — click to edit, blur/Enter to save */}
                <input
                  className="text-[16px] font-semibold text-foreground bg-transparent border-b border-transparent hover:border-foreground/20 focus:border-foreground/40 focus:outline-none w-full transition-colors"
                  defaultValue="Jiahao Ye"
                  placeholder="Display name"
                />
                <p className="text-[14px] text-foreground/55 truncate mt-0.5">
                  yejiahaoderek@gmail.com
                </p>
              </div>
              <span className="text-[13px] font-medium text-foreground/55 bg-foreground/5 px-2.5 py-0.5 rounded-full shrink-0">
                free
              </span>
            </div>
          </div>
        </div>
        <p className="text-[11px] text-foreground/25 mt-2">
          Inline edit: no modal, no save button. Bottom border appears on hover,
          solidifies on focus. Save on blur or Enter. PATCH /v1/auth/me with
          display_name. Optimistic update in useAuth.
        </p>
      </DemoCard>

      <div className="text-[12px] text-fg-4 space-y-0.5 mt-2">
        <p>
          Industry pattern: Slack, Notion, Linear, Vercel, GitHub, Figma all use
          top-left scope selector. 2-click standard. Full context swap on
          switch.
        </p>
        <p>
          Routing: switched hub sets read context only. Push writes require
          explicit targeting (for example X-Hub-ID) or fall back to personal.
          Recall reads across all accessible hubs.
        </p>
        <p>
          New user invite flow: Click link → see hub info → &ldquo;Sign in to
          join&rdquo; → OAuth with returnTo → auto-accept on return → switch to
          hub → land in /home.
        </p>
      </div>

      {/* ═══════════════════════════════════════════════════════════════
          20p — HubHeader: dedicated page-level identity + greeting
          ═══════════════════════════════════════════════════════════════ */}
      <DemoCard label="20p. HubHeader — full-bleed banner, three aurora modes">
        <div className="space-y-2 text-[13px] text-fg-2">
          <p>
            <span className="text-fg-1 font-medium">What it is.</span> A
            page-level identity header that renders on /home, /topics, and any
            hub-scoped landing. Replaces the old &ldquo;你的主题 · 55
            条记忆&rdquo; heading that floated between section cards — that was
            hub metadata mislabelled as a section title.
          </p>
          <p>
            <span className="text-fg-1 font-medium">One component.</span>{" "}
            <span className="font-mono">HubHeaderBanner</span> with an{" "}
            <span className="font-mono">aurora</span> prop. Full-bleed banner
            bleeds under the app top bar (logo + hub dropdown + user avatar).
            Avatar hangs across the banner edge, title/greeting /stats sit on{" "}
            <span className="font-mono">--background</span>.
          </p>
          <p>
            <span className="text-fg-1 font-medium">Three modes.</span>{" "}
            <span className="font-mono">signature</span> (default) — static
            memax-branded gradient, stable across team members and time of day.{" "}
            <span className="font-mono">time</span> — drifts with time of day,
            personal-hub opt-in only, team hubs ignore.{" "}
            <span className="font-mono">none</span> — plain typographic
            fallback, no banner, no aurora. See 20p-f for the Settings ›
            Appearance switcher.
          </p>
          <p className="text-fg-3 text-[12px]">
            Rules honored: no left-colored-borders, no toolbar in the header, no
            mode change, no decorative chrome, no content in the banner
            top-right (that area is reserved for the app shell). Greeting is
            contextual — dream deltas, inbox overflow, review-needed,
            quiet-night. Never generic time-only.
          </p>
        </div>
      </DemoCard>

      {/* ═══════════════════════════════════════════════════════════════
          20p-a..h — Banner + aurora modes (production target)
          ═══════════════════════════════════════════════════════════════ */}
      <DemoCard label="20p-a. Three aurora modes — plain / signature / time">
        <p className="text-[12px] text-fg-2 mb-1">
          One component, <span className="font-mono">HubHeaderBanner</span>,
          with three background treatments via the{" "}
          <span className="font-mono">aurora</span> prop. Shipping default is{" "}
          <span className="text-fg-1 font-medium">signature</span>.
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 space-y-1 pl-4 list-disc">
          <li>
            <span className="text-fg-1 font-medium">Plain</span> (
            <span className="font-mono">aurora=&quot;none&quot;</span>) — no
            banner layer. Pure typography on{" "}
            <span className="font-mono">--background</span>. Same treatment as
            20p-f. Minimal. For users who find any ambient background noisy, or
            as a fallback if the browser doesn&apos;t support gradients/blur.
          </li>
          <li>
            <span className="text-fg-1 font-medium">Signature</span> (
            <span className="font-mono">aurora=&quot;signature&quot;</span>,{" "}
            default) — static memax-branded signature gradient. Violet + soft
            magenta. Stable across time, stable across team members. This is the
            production default.
          </li>
          <li>
            <span className="text-fg-1 font-medium">Time</span> (
            <span className="font-mono">aurora=&quot;time&quot;</span>) —
            time-of-day driven gradient. Personal hub opt-in only (Settings ›
            Appearance › &ldquo;Match time of day&rdquo;). Team hubs cannot
            enable this — the time signal is a personal concept, not a shared
            one.
          </li>
        </ul>
        <div className="space-y-6">
          <div>
            <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
              Plain — no aurora, pure typography
            </p>
            <div className="rounded-2xl overflow-hidden bg-background border border-border/30">
              <HubHeaderBanner
                data={HUB_HEADER_MOCKS["morning-dream-delta"]}
                aurora="none"
              />
            </div>
          </div>
          <div>
            <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
              Signature — static memax gradient (recommended default)
            </p>
            <div className="rounded-2xl overflow-hidden bg-background border border-border/30">
              <HubHeaderBanner
                data={HUB_HEADER_MOCKS["morning-dream-delta"]}
                aurora="signature"
              />
            </div>
          </div>
          <div>
            <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
              Time — shifts with time of day (personal opt-in)
            </p>
            <div className="rounded-2xl overflow-hidden bg-background border border-border/30">
              <HubHeaderBanner
                data={HUB_HEADER_MOCKS["morning-dream-delta"]}
                aurora="time"
              />
            </div>
          </div>
        </div>
      </DemoCard>

      <DemoCard label="20p-b. Centered-notion layout — signature aurora, 200px">
        <p className="text-[12px] text-fg-2 mb-1">
          Direct Notion translation. Avatar centered, hanging 50/50 across the
          banner edge. Title/greeting/stats stack below left-aligned. Title
          bumps to 28px for the hero feel.
        </p>
        <p className="text-[12px] text-fg-3 mb-3">
          Use for hero states (onboarding, return-after-absence) where the hub
          identity should take a full-width moment. Daily workspace uses the
          inline-right variant from 20p-k.
        </p>
        <div className="rounded-2xl overflow-hidden bg-background border border-border/30">
          <HubHeaderBanner
            data={HUB_HEADER_MOCKS["return-after-absence"]}
            aurora="signature"
            layout="centered-notion"
            height={200}
          />
        </div>
      </DemoCard>

      <DemoCard label="20p-c. Mobile compact — all three modes (140px)">
        <p className="text-[12px] text-fg-2 mb-3">
          Mobile: banner drops to 140px, avatar to 56px, title to 20px, greeting
          to 13px. Plain mode has no banner, just a small top padding and the
          same typographic content.
        </p>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {(
            [
              ["none", "Plain"],
              ["signature", "Signature"],
              ["time", "Time"],
            ] as const
          ).map(([mode, label]) => (
            <div key={mode}>
              <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
                {label}
              </p>
              <div className="rounded-2xl overflow-hidden bg-background border border-border/30">
                <HubHeaderBanner
                  data={HUB_HEADER_MOCKS["evening-inbox-overflow"]}
                  aurora={mode}
                  compact
                />
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="20p-d. States gallery — 9 canonical states on signature">
        <p className="text-[12px] text-fg-2 mb-1">
          The same <span className="font-mono">HubHeaderBanner</span> across
          every data state: morning-dream-delta, afternoon-clean,
          evening-inbox-overflow, deep-night-quiet, first-time,
          return-after-absence, team-morning, team-review-needed. All on the
          signature aurora (the shipping default), so you can see how the same
          surface adapts content without changing its skin.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Notice: team-morning and team-review-needed render a square avatar +
          member cluster beside the hub name. Personal hubs render a circle
          avatar and skip the cluster. Stats row adapts: first-time collapses to
          a setup CTA, team states include member count, inbox-heavy states
          include an inbox number, review-needed includes a pending-review
          count.
        </p>
        <div className="space-y-6">
          {(
            [
              ["morning-dream-delta", "Morning — dream deltas"],
              ["afternoon-clean", "Afternoon — clean"],
              ["evening-inbox-overflow", "Evening — inbox overflow"],
              ["deep-night-quiet", "Deep night — contemplative"],
              ["first-time", "First-time / empty"],
              ["return-after-absence", "Return after absence"],
              ["team-morning", "Team — morning + activity"],
              ["team-review-needed", "Team — review needed"],
            ] as const
          ).map(([state, label]) => (
            <div key={state}>
              <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
                {label}
              </p>
              <div className="rounded-2xl overflow-hidden bg-background border border-border/30">
                <HubHeaderBanner data={HUB_HEADER_MOCKS[state]} />
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="20p-e. Full-page context — app top bar + banner + Recent below">
        <p className="text-[12px] text-fg-2 mb-1">
          End-to-end. The real app has a top bar: memax logo top-left, hub
          dropdown + user avatar top-right. The banner bleeds{" "}
          <span className="text-fg-1 font-medium">under</span> this top bar —
          the aurora is the ambient backdrop for the app chrome, not something
          that starts below it.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          On signature/time modes, top-bar elements get frosted backdrop-blur
          pills so they stay legible on the gradient. On plain mode, the pills
          drop away — elements sit on{" "}
          <span className="font-mono">--background</span> normally. Compare the
          three modes below using the same mock state (team hub, morning).
        </p>
        <div className="space-y-6">
          {(
            [
              ["none", "Plain — no aurora under top bar"],
              ["signature", "Signature — default production treatment"],
              ["time", "Time — personal opt-in (morning bucket)"],
            ] as const
          ).map(([mode, label]) => (
            <div key={mode}>
              <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
                {label}
              </p>
              <div className="relative rounded-2xl overflow-hidden bg-background border border-border/30">
                <MockAppTopBar
                  hubName="backend-team"
                  userInitial="D"
                  onSignature={mode !== "none"}
                />
                <HubHeaderBanner
                  data={HUB_HEADER_MOCKS["team-morning"]}
                  aurora={mode}
                />
                <div className="mx-auto max-w-3xl px-6 pb-6">
                  <div
                    className="rounded-xl overflow-hidden opacity-70"
                    style={{
                      border: "1px solid var(--border)",
                      background: "var(--card)",
                    }}
                  >
                    <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
                      <Clock className="h-3.5 w-3.5 text-fg-3 shrink-0" />
                      <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
                        新鲜记忆
                      </span>
                      <span className="text-[12px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
                        过去 12h
                      </span>
                      <div className="flex-1" />
                      <span className="text-[12px] text-fg-4">选择</span>
                    </div>
                    <div className="px-4 py-3 text-[12px] text-fg-3">
                      … memory rows …
                    </div>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="20p-f. Settings › Appearance — the aurora mode switcher">
        <p className="text-[12px] text-fg-2 mb-1">
          How users change aurora mode. The control lives in{" "}
          <span className="font-mono">Settings › Appearance</span>, not on the
          banner itself (the banner has no chrome). Three radio options + live
          preview. Preference is user-global (one setting per user across all
          hubs, not per-hub).
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Team hubs auto-downgrade <span className="font-mono">time</span> →{" "}
          <span className="font-mono">signature</span> at render time. The
          setting stays at <span className="font-mono">time</span> for the user
          — they&apos;ll see the time drift again whenever they view a personal
          hub. No feature flags, no hub-level override in the data model: a
          simple two-line rule in the render path.
        </p>
        <SettingsAppearanceMock />
      </DemoCard>

      <DemoCard label="20p-g. Greeting copy matrix — brand voice decisions">
        <p className="text-[12px] text-fg-2 mb-3">
          Greeting priority when multiple states apply:{" "}
          <span className="text-fg-1">
            first-time → review-needed → dream-deltas → inbox-overflow → clean →
            time-only
          </span>
          . Lower-priority states roll into the stats line instead. Variety
          within a state: 2–3 alternates, deterministically rotated per day.
        </p>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-[12px]">
            <thead className="text-fg-3 border-b border-border/40">
              <tr>
                <th className="py-2 pr-3 text-left font-medium">Situation</th>
                <th className="py-2 pr-3 text-left font-medium">
                  Greeting (zh)
                </th>
                <th className="py-2 text-left font-medium">Greeting (en)</th>
              </tr>
            </thead>
            <tbody className="text-fg-2">
              {[
                [
                  "Morning + dream deltas",
                  "早上好，Derek。memax 昨晚整理了 4 个主题。",
                  "Good morning, Derek. Dreams reorganized 4 topics overnight.",
                ],
                [
                  "Morning + clean",
                  "早上好，Derek。一切井井有条。",
                  "Good morning, Derek. Everything's in order.",
                ],
                [
                  "Afternoon + inbox overflow",
                  "下午好，Derek。收件箱里有 28 条等你整理。",
                  "Good afternoon, Derek. 28 items waiting in your inbox.",
                ],
                [
                  "Evening + review needed",
                  "晚上好，Derek。有 3 条合并冲突等你决定。",
                  "Good evening, Derek. 3 merge conflicts need your call.",
                ],
                [
                  "Deep night + clean",
                  "深夜了，Derek。深夜的想法最清澈。",
                  "Late night, Derek. Quiet thoughts travel far.",
                ],
                [
                  "Return after 7d absence",
                  "欢迎回来，Derek。最近都想些什么？",
                  "Welcome back, Derek. What have you been thinking about?",
                ],
                [
                  "First-time / empty",
                  "欢迎使用 memax，Derek。",
                  "Welcome to memax, Derek.",
                ],
                [
                  "Team + team activity",
                  "早上好，Derek。团队今天多了 6 条新记忆。",
                  "Good morning, Derek. Team added 6 memories today.",
                ],
                [
                  "Team + review needed",
                  "Derek，有 3 条合并冲突等你决定。",
                  "Derek, 3 merge conflicts need your call.",
                ],
              ].map(([situation, zh, en]) => (
                <tr
                  key={situation}
                  className="border-b border-border/20 align-top"
                >
                  <td className="py-2 pr-3 text-fg-3">{situation}</td>
                  <td className="py-2 pr-3">{zh}</td>
                  <td className="py-2 text-fg-2">{en}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="mt-3 text-[11px] text-fg-3 space-y-1">
          <p>
            <span className="text-fg-2">Rules:</span> warm but not cheesy ·
            acknowledge state, don&apos;t hype · comma after name in English ·
            never prefix with Hi / Hello · never include hub name in the
            greeting (it&apos;s already above) · bilingual parity (not
            translations — each language gets its own copy pass).
          </p>
          <p>
            <span className="text-fg-2">i18n namespace (new):</span>{" "}
            <span className="font-mono">hub.greeting.*</span> with ~15 keys
            covering the situation matrix plus 2–3 alternates each.
          </p>
        </div>
      </DemoCard>

      <DemoCard label="20p-h. Banner design rules">
        <div className="space-y-2 text-[12px] text-fg-2">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p>
              <span className="text-fg-1 font-medium">
                One component, three modes.
              </span>{" "}
              <span className="font-mono">HubHeaderBanner</span> owns all three
              aurora treatments via the{" "}
              <span className="font-mono">aurora</span> prop:{" "}
              <span className="font-mono">&quot;none&quot;</span> (plain
              typography),{" "}
              <span className="font-mono">&quot;signature&quot;</span> (static
              memax gradient, default), and{" "}
              <span className="font-mono">&quot;time&quot;</span> (opt-in
              time-of-day drift). No separate components, no feature-flag
              branches in callers.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Signature is the default.
              </span>{" "}
              Stable, memax-branded, consistent across time and team members.
              Time-drift is a personal-hub-only opt-in (Settings › Appearance ›
              &ldquo;Match time of day&rdquo;). Team hubs cannot enable
              time-drift — time-of-day is a personal concept and makes no sense
              for a shared workspace.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Plain is the escape hatch.
              </span>{" "}
              <span className="font-mono">aurora=&quot;none&quot;</span> renders
              pure typography on <span className="font-mono">--background</span>{" "}
              — identical to the 20p-f borderless treatment. No banner layer, no
              halo on the avatar, no negative margin. For users who prefer
              minimal ambient, or as a fallback when gradients/blur don &apos;t
              render (e.g. reduced-motion, old browsers).
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Banner bleeds under the app top bar.
              </span>{" "}
              The app shell&apos;s top bar (memax logo left + hub dropdown and
              user avatar right) is absolutely positioned on top of the banner.
              The aurora is the backdrop for the top bar elements — they are NOT
              in a separate bar above it. On signature/time modes, top-bar
              elements wear frosted backdrop-blur pills to stay legible. On
              plain mode, the pills drop.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p>
              <span className="text-fg-1 font-medium">
                No content in the banner&apos;s top-right.
              </span>{" "}
              That area is reserved for the app shell (hub dropdown + user
              avatar). The banner itself renders no time icon, no Change button,
              no Pin button. Aurora is ambient — the user doesn &apos;t
              customize it and doesn&apos;t need controls.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Shorter than Notion.
              </span>{" "}
              180px desktop default (200px for hero states like onboarding),
              140px mobile. Notion&apos;s 280px assumes a rarely-visited project
              page. memax /hub is a daily workspace — every pixel above the fold
              is a budget.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Fade, don&apos;t cut.
              </span>{" "}
              Bottom 25% of the banner fades to{" "}
              <span className="font-mono">--background</span> via a gradient
              overlay (not <span className="font-mono">mask-image</span> — the
              mask interferes with the avatar halo). No hard horizontal line
              between banner and content.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">8.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Avatar crosses the edge.
              </span>{" "}
              Inline-right: 25% above / 75% below (title lands on clean
              background). Centered-notion: 50/50 (dramatic hang). Negative{" "}
              <span className="font-mono">margin-top</span> on the avatar
              wrapper; everything else in natural flow. Plain mode: no negative
              margin, no halo.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">9.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Title never touches the banner.
              </span>{" "}
              In both layouts the title sits entirely below the banner bottom
              edge, on <span className="font-mono">--background</span>. Aurora
              is ambient context, not a backdrop for typography — placing the
              title on the aurora costs legibility and reduces the semantic
              weight of the name.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">10.</span>
            <p>
              <span className="text-fg-1 font-medium">
                HubHeaderAurora (the card) is deprecated.
              </span>{" "}
              The rounded glass card variant remains in the kitchen for
              comparison only. In production: delete it, use{" "}
              <span className="font-mono">HubHeaderBanner</span> with the
              default{" "}
              <span className="font-mono">aurora=&quot;signature&quot;</span>{" "}
              and{" "}
              <span className="font-mono">layout=&quot;inline-right&quot;</span>
              .
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ═══════════════════════════════════════════════════════════════
          20q — Hub switch from inside a topic
          ═══════════════════════════════════════════════════════════════ */}
      <DemoCard label="20q. Hub switch — always land in memory view">
        <div className="space-y-2 text-[13px] text-fg-2">
          <p>
            <span className="text-fg-1 font-medium">The decision.</span> Topics
            are hub-scoped, memory detail is hub-scoped, and even a plain Recent
            row is still content from the old hub. Leaving the user on whatever
            route happened to be open creates loopholes: some switches re-scope
            in place, others navigate, and memory detail can point at the wrong
            hub context.
          </p>
          <p>
            <span className="text-fg-1 font-medium">The fix.</span> Switching
            hub always navigates to the new hub&apos;s{" "}
            <span className="text-fg-1">/memories view</span>. You were looking
            at memories before; now you&apos;re looking at the new hub&apos;s
            memories. One destination, one rule, no route-specific exceptions.
          </p>
        </div>
      </DemoCard>

      <DemoCard label="20q-a. Transition frames — switch hub → fade → new hub's Memories">
        <HubSwitchTransitionDemo />
      </DemoCard>

      <DemoCard label="20q-b. Dropdown hint — tells you what you'll see, not what you'll lose">
        <HubSwitchDropdownHintDemo />
      </DemoCard>
    </Section>
  );
}
