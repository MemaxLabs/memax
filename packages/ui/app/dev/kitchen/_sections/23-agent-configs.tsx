// Maps to: settings-dialog.tsx > Agents tab
// Unified agent management — identity, API keys, configs, activity, rename, revoke
// Production: agent-configs-section.tsx, connect-agents-section.tsx
"use client";

import { useState } from "react";
import { Section, DemoCard } from "../_shared";
import { InfoPopover } from "@/components/info-popover";
import {
  ChevronDown,
  ChevronRight,
  X,
  Copy,
  Globe,
  FolderGit2,
  Brain,
  Settings2,
  Bot,
  Terminal,
  Key,
  Pencil,
  Shield,
  Activity,
  Clock,
  Hash,
} from "lucide-react";
import {
  AGENT_IDENTITIES,
  getAgentIdentity,
  type AgentIdentity,
} from "@/tokens/agents";

/** Kitchen uses AGENT_IDENTITIES from shared system */
const AGENTS = AGENT_IDENTITIES;

/* ── Mock data ── */

interface MockAPIKey {
  id: string;
  prefix: string;
  agent_name: string;
  hub_id: string | null;
  hub_name: string | null;
  last_used: string | null; // relative
  created_at: string;
}

interface MockConfig {
  id: string;
  agent: string;
  file_path: string;
  scope: string;
  version: number;
  updated_at: string;
  extracted: number;
}

interface MockAgentData {
  identity: AgentIdentity;
  keys: MockAPIKey[];
  configs: MockConfig[];
  stats: { memories_pushed: number; last_active: string | null };
}

const MOCK_DATA: MockAgentData[] = [
  {
    identity: AGENTS["claude-code"],
    keys: [
      {
        id: "k1",
        prefix: "mxk_a1b2",
        agent_name: "claude-code",
        hub_id: null,
        hub_name: null,
        last_used: "2m ago",
        created_at: "2025-12-01",
      },
      {
        id: "k2",
        prefix: "mxk_c3d4",
        agent_name: "claude-code",
        hub_id: "hub-1",
        hub_name: "memax-team",
        last_used: "1h ago",
        created_at: "2026-01-15",
      },
    ],
    configs: [
      {
        id: "c1",
        agent: "claude-code",
        file_path: "CLAUDE.md",
        scope: "global",
        version: 1,
        updated_at: "4h",
        extracted: 0,
      },
      {
        id: "c2",
        agent: "claude-code",
        file_path: "CLAUDE.md",
        scope: "project:memax",
        version: 3,
        updated_at: "4h",
        extracted: 5,
      },
      {
        id: "c3",
        agent: "claude-code",
        file_path: "memory/MEMORY.md",
        scope: "project:memax",
        version: 1,
        updated_at: "4h",
        extracted: 8,
      },
      {
        id: "c4",
        agent: "claude-code",
        file_path: "memory/feedback_design_patterns.md",
        scope: "project:memax",
        version: 1,
        updated_at: "4h",
        extracted: 3,
      },
      {
        id: "c5",
        agent: "claude-code",
        file_path: "memory/feedback_container_morphing.md",
        scope: "project:memax",
        version: 1,
        updated_at: "4h",
        extracted: 2,
      },
      {
        id: "c6",
        agent: "claude-code",
        file_path: "CLAUDE.md",
        scope: "project:meta-subscriptions",
        version: 2,
        updated_at: "1d",
        extracted: 12,
      },
    ],
    stats: { memories_pushed: 147, last_active: "2m ago" },
  },
  {
    identity: AGENTS["cursor"],
    keys: [
      {
        id: "k3",
        prefix: "mxk_e5f6",
        agent_name: "cursor",
        hub_id: null,
        hub_name: null,
        last_used: "3d ago",
        created_at: "2026-02-10",
      },
    ],
    configs: [
      {
        id: "c7",
        agent: "cursor",
        file_path: ".cursorrules",
        scope: "global",
        version: 1,
        updated_at: "3d",
        extracted: 2,
      },
    ],
    stats: { memories_pushed: 23, last_active: "3d ago" },
  },
  {
    identity: AGENTS["gemini"],
    keys: [
      {
        id: "k4",
        prefix: "mxk_g7h8",
        agent_name: "gemini",
        hub_id: null,
        hub_name: null,
        last_used: null,
        created_at: "2026-03-20",
      },
    ],
    configs: [],
    stats: { memories_pushed: 0, last_active: null },
  },
];

/* ── Helpers ── */

type ActivityStatus = "active" | "idle" | "inactive";

function getActivityStatus(lastActive: string | null): ActivityStatus {
  if (!lastActive) return "inactive";
  if (lastActive.includes("m ago") || lastActive.includes("h ago"))
    return "active";
  if (lastActive.includes("1d") || lastActive.includes("2d")) return "idle";
  return "inactive";
}

function statusDot(status: ActivityStatus) {
  const colors: Record<ActivityStatus, string> = {
    active: "oklch(0.72 0.19 145)", // green
    idle: "oklch(0.75 0.15 85)", // amber
    inactive: "var(--fg-4)", // muted
  };
  return (
    <span className="relative flex h-2 w-2">
      {status === "active" && (
        <span
          className="absolute inline-flex h-full w-full rounded-full opacity-50 state-slow-breathe"
          style={{ backgroundColor: colors[status] }}
        />
      )}
      <span
        className="relative inline-flex h-2 w-2 rounded-full"
        style={{ backgroundColor: colors[status] }}
      />
    </span>
  );
}

function statusLabel(status: ActivityStatus): string {
  return status === "active"
    ? "Just active"
    : status === "idle"
      ? "Active recently"
      : "Connected";
}

function credentialClassPill(label: string) {
  return (
    <span className="text-[10px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded">
      {label}
    </span>
  );
}

function parseScope(scope: string): string {
  if (scope === "global") return "Global";
  return scope.replace("project:", "");
}

function fileIcon(path: string) {
  if (path.startsWith("memory/")) return Brain;
  return Settings2;
}

function fileLabel(path: string): string {
  if (path.startsWith("memory/")) {
    const name = path
      .replace("memory/", "")
      .replace(".md", "")
      .replace(/^(feedback|project|reference|user)_/, "");
    return name
      .split("_")
      .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
      .join(" ");
  }
  return path;
}

function fileKind(path: string): string | null {
  if (path.startsWith("memory/feedback_")) return "feedback";
  if (path.startsWith("memory/project_")) return "project";
  if (path.startsWith("memory/reference_")) return "reference";
  if (path.startsWith("memory/user_")) return "user";
  if (path === "memory/MEMORY.md") return "index";
  return null;
}

/* ── Agent Card (Northstar) ── */

function AgentCard({
  data,
  defaultExpanded = false,
}: {
  data: MockAgentData;
  defaultExpanded?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [renaming, setRenaming] = useState(false);
  const [displayName, setDisplayName] = useState(data.identity.displayName);
  const [revokeKeyId, setRevokeKeyId] = useState<string | null>(null);

  const { identity, keys, configs, stats } = data;
  const Icon = identity.icon;
  const status = getActivityStatus(stats.last_active);

  // Group configs by scope
  const configsByScope = new Map<string, MockConfig[]>();
  for (const c of configs) {
    const group = configsByScope.get(c.scope) ?? [];
    group.push(c);
    configsByScope.set(c.scope, group);
  }
  const totalExtracted = configs.reduce((s, c) => s + c.extracted, 0);

  return (
    <div
      className="rounded-2xl overflow-hidden transition-shadow duration-200"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
        boxShadow: expanded
          ? "0 4px 24px oklch(0 0 0 / 0.06)"
          : "0 1px 4px oklch(0 0 0 / 0.03)",
      }}
    >
      {/* ── Header ── */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-5 py-4 cursor-pointer group"
      >
        {/* Agent icon */}
        <div
          className="flex items-center justify-center h-9 w-9 rounded-xl shrink-0"
          style={{
            background: `oklch(from ${identity.color} l c h / 0.12)`,
          }}
        >
          <Icon
            className="h-4.5 w-4.5"
            style={{ color: identity.color }}
            strokeWidth={1.8}
          />
        </div>

        {/* Name + status */}
        <div className="flex-1 min-w-0 text-left">
          <div className="flex items-center gap-2">
            <span className="text-[15px] font-medium text-fg-1 group-hover:text-foreground transition-colors">
              {displayName}
            </span>
            {statusDot(status)}
            <span className="text-[11px] text-fg-3">{statusLabel(status)}</span>
          </div>
          <div className="flex items-center gap-3 mt-0.5">
            {stats.memories_pushed > 0 && (
              <span className="text-[12px] text-fg-3 tabular-nums">
                {stats.memories_pushed} pushed
              </span>
            )}
            {configs.length > 0 && (
              <>
                {stats.memories_pushed > 0 && (
                  <span className="text-fg-4 text-[10px]">·</span>
                )}
                <span className="text-[12px] text-fg-3 tabular-nums">
                  {configs.length} configs
                </span>
              </>
            )}
            {keys.length > 0 && (
              <>
                <span className="text-fg-4 text-[10px]">·</span>
                <span className="text-[12px] text-fg-3 tabular-nums">
                  {keys.length} {keys.length === 1 ? "key" : "keys"}
                </span>
              </>
            )}
          </div>
        </div>

        {/* Expand chevron */}
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        )}
      </button>

      {/* ── Expanded Detail ── */}
      {expanded && (
        <div className="px-5 pb-5 space-y-4">
          {/* ── Identity & Rename ── */}
          <div className="flex items-center gap-3 px-3 py-2.5 rounded-xl bg-surface-1">
            {renaming ? (
              <div className="flex items-center gap-2 flex-1">
                <Pencil className="h-3 w-3 text-fg-3 shrink-0" />
                <input
                  type="text"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  onBlur={() => setRenaming(false)}
                  onKeyDown={(e) => e.key === "Enter" && setRenaming(false)}
                  className="flex-1 text-[14px] text-fg-1 bg-transparent border-none outline-none"
                  autoFocus
                />
                <span className="text-[11px] text-fg-3">Enter to save</span>
              </div>
            ) : (
              <>
                <Hash className="h-3 w-3 text-fg-3 shrink-0" />
                <span className="text-[13px] text-fg-2 font-mono flex-1">
                  {identity.slug}
                </span>
                {credentialClassPill(
                  keys.length > 0 ? "via API key" : "via OAuth",
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    setRenaming(true);
                  }}
                  className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer flex items-center gap-1"
                >
                  <Pencil className="h-2.5 w-2.5" />
                  Rename
                </button>
              </>
            )}
          </div>

          {/* ── API Keys ── */}
          {keys.length > 0 && (
            <div>
              <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-2 px-1 flex items-center gap-1.5">
                <Key className="h-3 w-3" />
                API Keys
              </p>
              <div className="space-y-1.5">
                {keys.map((k) => (
                  <div key={k.id}>
                    {revokeKeyId === k.id ? (
                      /* Revoke confirmation */
                      <div
                        className="flex items-center justify-between rounded-xl px-3 py-2.5 transition-colors"
                        style={{
                          backgroundColor:
                            "oklch(from var(--destructive) l c h / 0.08)",
                        }}
                      >
                        <span className="text-[12px] text-fg-2">
                          Revoke this key? Agent will lose access.
                        </span>
                        <div className="flex items-center gap-2.5 shrink-0">
                          <button
                            onClick={() => setRevokeKeyId(null)}
                            className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer"
                          >
                            Revoke
                          </button>
                          <button
                            onClick={() => setRevokeKeyId(null)}
                            className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer"
                          >
                            Keep
                          </button>
                        </div>
                      </div>
                    ) : (
                      <div className="flex items-center gap-2.5 rounded-xl px-3 py-2.5 bg-surface-1 hover:bg-surface-2 transition-colors">
                        <span className="text-[13px] text-fg-2 font-mono tabular-nums">
                          {k.prefix}····
                        </span>
                        {k.hub_name ? (
                          <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded flex items-center gap-1">
                            <Shield className="h-2.5 w-2.5" />
                            {k.hub_name}
                          </span>
                        ) : (
                          <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded flex items-center gap-1">
                            <Globe className="h-2.5 w-2.5" />
                            All hubs
                          </span>
                        )}
                        <span className="flex-1" />
                        {k.last_used && (
                          <span className="text-[11px] text-fg-3 tabular-nums flex items-center gap-1">
                            <Activity className="h-2.5 w-2.5" />
                            {k.last_used}
                          </span>
                        )}
                        <button
                          onClick={() => setRevokeKeyId(k.id)}
                          className="p-0.5 text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer shrink-0"
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* ── Config Files ── */}
          {configs.length > 0 && (
            <div>
              <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-2 px-1 flex items-center gap-1.5">
                <Settings2 className="h-3 w-3" />
                Synced Configs
                {totalExtracted > 0 && (
                  <span className="text-fg-4 font-normal">
                    · {totalExtracted} extracted
                  </span>
                )}
              </p>
              <div className="space-y-1">
                {[...configsByScope.entries()].map(([scope, scopeConfigs]) => {
                  const project = parseScope(scope);
                  const isGlobal = scope === "global";
                  const ScopeIcon = isGlobal ? Globe : FolderGit2;

                  return (
                    <ScopeGroup
                      key={scope}
                      label={project}
                      icon={ScopeIcon}
                      configs={scopeConfigs}
                    />
                  );
                })}
              </div>
            </div>
          )}

          {/* ── Activity Summary ── */}
          <div className="flex items-center gap-4 px-3 py-2 rounded-xl bg-surface-1 text-[12px] text-fg-3">
            <span className="flex items-center gap-1.5">
              <Activity className="h-3 w-3" />
              {stats.memories_pushed} memories pushed
            </span>
            {stats.last_active && (
              <span className="flex items-center gap-1.5">
                <Clock className="h-3 w-3" />
                Last active {stats.last_active}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

/* ── Scope Group (config files) ── */

function ScopeGroup({
  label,
  icon: ScopeIcon,
  configs,
}: {
  label: string;
  icon: typeof Globe;
  configs: MockConfig[];
}) {
  const [open, setOpen] = useState(true);
  const scopeExtracted = configs.reduce((s, c) => s + c.extracted, 0);

  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-1.5 rounded-lg hover:bg-surface-2 cursor-pointer transition-colors"
      >
        <ScopeIcon className="h-3 w-3 text-fg-3 shrink-0" />
        <span className="text-[12px] font-medium text-fg-2 flex-1 text-left">
          {label}
        </span>
        <span className="text-[11px] text-fg-4 tabular-nums">
          {configs.length}
        </span>
        {scopeExtracted > 0 && (
          <>
            <span className="text-fg-4 text-[9px]">·</span>
            <span className="text-[11px] text-fg-4 tabular-nums">
              {scopeExtracted}
            </span>
          </>
        )}
        {open ? (
          <ChevronDown className="h-2.5 w-2.5 text-fg-4 shrink-0" />
        ) : (
          <ChevronRight className="h-2.5 w-2.5 text-fg-4 shrink-0" />
        )}
      </button>

      {open && (
        <div className="ml-4 border-l border-border/15">
          {configs.map((c) => {
            const FileIcon = fileIcon(c.file_path);
            const label = fileLabel(c.file_path);
            const group = fileKind(c.file_path);

            return (
              <div
                key={c.id}
                className="flex items-center gap-2 pl-3 pr-3 py-1 text-[12px] text-fg-2 hover:bg-surface-1 rounded-r-lg transition-colors"
              >
                <FileIcon className="h-2.5 w-2.5 text-fg-3 shrink-0" />
                <span className="flex-1 truncate">{label}</span>
                {group && (
                  <span className="text-[9px] text-fg-3 bg-surface-1 px-1 py-0.5 rounded">
                    {group}
                  </span>
                )}
                {c.extracted > 0 && (
                  <span className="text-[10px] text-fg-4 tabular-nums">
                    {c.extracted}
                  </span>
                )}
                <span className="text-[10px] text-fg-4 tabular-nums">
                  v{c.version}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

/* ── Rename Demo (isolated interaction) ── */

function RenameDemoCard() {
  const [name, setName] = useState("Claude Code");
  const [editing, setEditing] = useState(false);

  return (
    <div className="space-y-3">
      {/* Before: display mode */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
          Display mode
        </p>
        <div className="flex items-center gap-3 rounded-xl bg-surface-1 px-3 py-2.5">
          <div
            className="flex items-center justify-center h-8 w-8 rounded-lg shrink-0"
            style={{
              background: `oklch(from ${AGENTS["claude-code"].color} l c h / 0.12)`,
            }}
          >
            <Terminal
              className="h-4 w-4"
              style={{ color: AGENTS["claude-code"].color }}
              strokeWidth={1.8}
            />
          </div>
          <div className="flex-1">
            <span className="text-[14px] font-medium text-fg-1">{name}</span>
            <p className="text-[11px] text-fg-3 font-mono">claude-code</p>
          </div>
          <button
            onClick={() => setEditing(true)}
            className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer flex items-center gap-1"
          >
            <Pencil className="h-2.5 w-2.5" />
            Rename
          </button>
        </div>
      </div>

      {/* After: editing mode */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
          Editing mode
        </p>
        <div className="flex items-center gap-3 rounded-xl bg-surface-1 px-3 py-2.5">
          <div
            className="flex items-center justify-center h-8 w-8 rounded-lg shrink-0"
            style={{
              background: `oklch(from ${AGENTS["claude-code"].color} l c h / 0.12)`,
            }}
          >
            <Terminal
              className="h-4 w-4"
              style={{ color: AGENTS["claude-code"].color }}
              strokeWidth={1.8}
            />
          </div>
          <div className="flex-1">
            <input
              type="text"
              value="My Claude (Work)"
              readOnly
              className="text-[14px] text-fg-1 bg-transparent border-none outline-none w-full font-medium"
              style={{
                borderBottom: "1px solid var(--signature)",
              }}
            />
            <p className="text-[11px] text-fg-3 font-mono mt-0.5">
              claude-code
            </p>
          </div>
          <span className="text-[11px] text-fg-3">Enter to save</span>
        </div>
      </div>

      <p className="text-[11px] text-fg-3 px-1">
        Display name is user-customizable. Slug (claude-code) is immutable,
        auto-set from agent type during{" "}
        <span className="font-mono text-fg-2">memax setup</span>. Display name
        flows to memory attribution.
      </p>
    </div>
  );
}

/* ── Revoke Demo ── */

function RevokeDemoCard() {
  return (
    <div className="space-y-3">
      {/* Normal key row */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
          Normal state
        </p>
        <div className="flex items-center gap-2.5 rounded-xl px-3 py-2.5 bg-surface-1">
          <span className="text-[13px] text-fg-2 font-mono tabular-nums">
            mxk_a1b2····
          </span>
          <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded flex items-center gap-1">
            <Globe className="h-2.5 w-2.5" />
            All hubs
          </span>
          <span className="flex-1" />
          <span className="text-[11px] text-fg-3 tabular-nums flex items-center gap-1">
            <Activity className="h-2.5 w-2.5" />
            2m ago
          </span>
          <button className="p-0.5 text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer shrink-0">
            <X className="h-3 w-3" />
          </button>
        </div>
      </div>

      {/* Revoke confirmation */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
          Revoke confirmation
        </p>
        <div
          className="flex items-center justify-between rounded-xl px-3 py-2.5 transition-colors"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
          }}
        >
          <span className="text-[12px] text-fg-2">
            Revoke <span className="font-mono text-fg-3">mxk_a1b2</span>? Agent
            will lose access.
          </span>
          <div className="flex items-center gap-2.5 shrink-0">
            <button className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer">
              Revoke
            </button>
            <button className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer">
              Keep
            </button>
          </div>
        </div>
      </div>

      {/* Disconnect agent (all keys) */}
      <div>
        <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
          Disconnect agent (revoke all keys + delete configs)
        </p>
        <div
          className="flex items-center justify-between rounded-xl px-3 py-2.5 transition-colors"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
          }}
        >
          <div className="flex items-center gap-2">
            <Terminal
              className="h-3.5 w-3.5"
              style={{ color: AGENTS["claude-code"].color }}
            />
            <span className="text-[12px] text-fg-2">
              Disconnect Claude Code? 2 keys and 6 configs will be removed.
              Memories are preserved.
            </span>
          </div>
          <div className="flex items-center gap-2.5 shrink-0 ml-3">
            <button className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer">
              Disconnect
            </button>
            <button className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer">
              Keep
            </button>
          </div>
        </div>
      </div>

      <p className="text-[11px] text-fg-3 px-1">
        Two-stage confirmation pattern. Revoking a single key is granular.
        &ldquo;Disconnect agent&rdquo; revokes all keys + deletes all configs
        for that agent. Memories pushed by the agent are always preserved (they
        belong to the user, not the agent).
      </p>
    </div>
  );
}

/* ── Main section ── */

export function AgentConfigsSection() {
  return (
    <Section
      title="23. Agent Management"
      description="Unified agent identity, API keys, config sync, activity stats. Settings > Agents tab northstar."
    >
      {/* 23a. Northstar — multi-agent overview */}
      <DemoCard label="23a. Northstar &mdash; Agent Overview">
        <p className="text-[13px] text-fg-2 mb-4">
          Each connected agent gets a unified card. Identity icon with accent
          color, status dot (active/idle/inactive), stats summary. Expand for
          API keys, config files, rename, revoke.
        </p>
        <div className="space-y-3">
          {MOCK_DATA.map((agent, i) => (
            <AgentCard
              key={agent.identity.slug}
              data={agent}
              defaultExpanded={i === 0}
            />
          ))}
        </div>
      </DemoCard>

      {/* 23b. Rename interaction */}
      <DemoCard label="23b. Rename &mdash; inline edit">
        <p className="text-[13px] text-fg-2 mb-4">
          User can customize the display name shown in memory attribution and
          across the app. The agent slug (claude-code) is immutable &mdash; set
          during <span className="font-mono text-fg-3">memax setup</span>.
          Priority: user custom &rarr; AGENT_NAMES map &rarr; raw slug.
        </p>
        <RenameDemoCard />
      </DemoCard>

      {/* 23c. Revoke & disconnect */}
      <DemoCard label="23c. Revoke &amp; Disconnect">
        <p className="text-[13px] text-fg-2 mb-4">
          Three levels of removal: revoke single key, revoke all keys for an
          agent, or full disconnect (keys + configs). Memories are always
          preserved &mdash; they belong to the user, not the agent.
        </p>
        <RevokeDemoCard />
      </DemoCard>

      {/* 23d. Status indicators */}
      <DemoCard label="23d. Activity Status">
        <p className="text-[13px] text-fg-2 mb-4">
          Status derived from{" "}
          <span className="font-mono text-fg-3">last_used</span> on API keys. No
          MCP polling needed &mdash; every API call updates the timestamp.
        </p>
        <div className="space-y-2">
          {(
            [
              ["active", "2m ago", "Observed in the last 5 minutes"],
              ["idle", "3d ago", "Observed recently, not necessarily live"],
              ["inactive", null, "Connected, but no recent observed activity"],
            ] as [ActivityStatus, string | null, string][]
          ).map(([s, time, desc]) => (
            <div
              key={s}
              className="flex items-center gap-3 rounded-xl bg-surface-1 px-3 py-2.5"
            >
              {statusDot(s)}
              <span className="text-[13px] text-fg-1 font-medium w-20">
                {statusLabel(s)}
              </span>
              {time && (
                <span className="text-[12px] text-fg-3 tabular-nums">
                  {time}
                </span>
              )}
              <span className="text-[12px] text-fg-3 flex-1 text-right">
                {desc}
              </span>
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="23d2. Keys without agent identity">
        <p className="mb-4 text-[13px] text-fg-2">
          Orphan keys stay visible and explain why pushes look human until the
          user reissues them as an agent key.
        </p>
        <div
          className="overflow-hidden rounded-xl"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="flex items-center justify-between border-b border-border/30 px-4 py-3">
            <div className="flex items-center gap-2">
              <span className="text-[13px] font-medium text-fg-1">
                Keys without agent identity
              </span>
              <InfoPopover
                ariaLabel="About keys without agent identity"
                title="Why these show as you"
                body="Re-issue as an agent key to attribute memories correctly. Or revoke if no longer needed."
              />
            </div>
          </div>
          <div className="space-y-3 p-4">
            <p className="text-[12px] text-fg-3">
              These authenticate as you. Memories pushed with them appear as
              your own.
            </p>
            <div className="flex items-center gap-2.5 rounded-xl bg-surface-1 px-3 py-2.5">
              <span className="font-mono text-[13px] text-fg-2">
                mxk_abcd····
              </span>
              <span className="text-[11px] text-fg-3">hatch-agent-v3</span>
              <span className="flex-1" />
              <button className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors">
                Reissue as agent key
              </button>
              <button className="p-0.5 text-fg-3 hover:text-destructive/70 transition-colors">
                <X className="h-3 w-3" />
              </button>
            </div>
          </div>
        </div>
      </DemoCard>

      <DemoCard label="23d3. Credential class pill">
        <div className="grid gap-2 sm:grid-cols-3">
          {["via API key", "via OAuth", "via session"].map((label) => (
            <div
              key={label}
              className="flex items-center gap-2 rounded-xl bg-surface-1 px-3 py-2.5"
            >
              <span className="font-mono text-[12px] text-fg-2">
                claude-code
              </span>
              {credentialClassPill(label)}
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="23d4. Unnamed agent remediation">
        <p className="mb-4 text-[13px] text-fg-2">
          Unknown-slug OAuth grants stay usable, but the card becomes clearly
          remediable instead of pretending it has a stable identity.
        </p>
        <div
          className="rounded-2xl border border-border bg-card px-5 py-4"
          style={{ boxShadow: "0 1px 4px oklch(0 0 0 / 0.03)" }}
        >
          <div className="flex items-start gap-3">
            <div
              className="flex h-9 w-9 items-center justify-center rounded-xl"
              style={{
                background: `oklch(from ${AGENTS["gemini"].color} l c h / 0.12)`,
              }}
            >
              <Bot
                className="h-4.5 w-4.5"
                style={{ color: AGENTS["gemini"].color }}
                strokeWidth={1.8}
              />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="text-[14px] font-medium text-fg-1">
                  Unnamed agent
                </span>
                {statusDot("idle")}
                <span className="text-[11px] text-fg-3">Active recently</span>
              </div>
              <div className="mt-1 flex items-center gap-2 text-[12px] text-fg-3">
                <span className="font-mono text-fg-2">unknown</span>
                {credentialClassPill("via OAuth")}
              </div>
            </div>
            <button className="rounded-lg border border-border bg-surface-1 px-2 py-1 text-[12px] text-fg-2">
              Reconnect
            </button>
          </div>
          <div className="mt-3 rounded-xl bg-surface-1 px-3 py-3 text-[12px] text-fg-2">
            This connection was created without a stable agent identity.
            Disconnect this credential and have the agent reconnect through MCP
            OAuth — its registration will be validated.
            <div className="mt-3 flex justify-end">
              <button className="rounded-lg bg-foreground px-3 py-2 text-[13px] font-medium text-background">
                Disconnect this credential
              </button>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* 23e. Agent icons & identity */}
      <DemoCard label="23e. Agent Identity System">
        <p className="text-[13px] text-fg-2 mb-4">
          Each agent type gets a unique icon + accent color. Consistent across
          attribution, settings, and management surfaces. Icon is mapped from
          agent_name slug, not user-configurable (keeps visual consistency).
        </p>
        <div className="grid grid-cols-3 gap-2">
          {Object.values(AGENTS).map((agent) => {
            const Icon = agent.icon;
            return (
              <div
                key={agent.slug}
                className="flex items-center gap-2.5 rounded-xl bg-surface-1 px-3 py-2.5"
              >
                <div
                  className="flex items-center justify-center h-8 w-8 rounded-lg shrink-0"
                  style={{
                    background: `oklch(from ${agent.color} l c h / 0.12)`,
                  }}
                >
                  <Icon
                    className="h-4 w-4"
                    style={{ color: agent.color }}
                    strokeWidth={1.8}
                  />
                </div>
                <div>
                  <span className="text-[13px] text-fg-1 font-medium">
                    {agent.displayName}
                  </span>
                  <p className="text-[10px] text-fg-3 font-mono">
                    {agent.slug}
                  </p>
                </div>
              </div>
            );
          })}
        </div>
      </DemoCard>

      {/* 23f. Empty state */}
      <DemoCard label="23f. Empty State &mdash; no agents connected">
        <div className="grid grid-cols-2 gap-4">
          {/* Loading */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Loading
            </p>
            <div
              className="rounded-2xl px-5 py-4"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="space-y-3">
                {[1, 2].map((i) => (
                  <div key={i} className="flex items-center gap-3">
                    <div className="h-9 w-9 rounded-xl bg-surface-3 animate-pulse" />
                    <div className="flex-1 space-y-1.5">
                      <div className="h-3.5 w-28 bg-surface-3 rounded animate-pulse" />
                      <div className="h-2.5 w-40 bg-surface-2 rounded animate-pulse" />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* Empty */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              No agents
            </p>
            <div
              className="rounded-2xl px-5 py-5 text-center"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <Bot className="h-8 w-8 text-fg-4 mx-auto mb-3" />
              <p className="text-[14px] text-fg-2 font-medium mb-1">
                Connect your first agent
              </p>
              <p className="text-[12px] text-fg-3 mb-4">
                Claude Code, Cursor, Windsurf, Copilot, Codex, Gemini
              </p>
              <div className="rounded-lg bg-surface-1 px-3.5 py-2.5 font-mono text-[12px] text-fg-2 text-left relative mx-auto max-w-xs">
                npm i -g memax-cli && memax setup
                <Copy className="h-3 w-3 text-fg-3 absolute top-2.5 right-3" />
              </div>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* Data model notes */}
      <div className="text-[12px] text-fg-3 space-y-1 mt-2">
        <p className="font-medium text-fg-2">Data model</p>
        <p>
          Agent identity is derived from{" "}
          <span className="font-mono">api_keys.agent_name</span> — set during{" "}
          <span className="font-mono">memax setup</span>. Each agent type can
          have multiple keys (per-hub, per-device). Activity status from{" "}
          <span className="font-mono">api_keys.last_used</span>.
        </p>
        <p>
          Display name priority: user custom → AGENT_NAMES map → raw slug.
          Stored as <span className="font-mono">agent_display_name</span> on
          api_keys (future migration). Unified constant shared across
          memory-row, settings, and attribution.
        </p>
        <p>
          New endpoint needed:{" "}
          <span className="font-mono">GET /v1/agents/summary</span> — aggregates
          api_keys + configs + memory counts per agent_name.
        </p>
        <p>
          Production files: agent-configs-section.tsx, settings-dialog.tsx,
          memory-row.tsx. API: GET /v1/auth/api-keys, GET /v1/configs.
        </p>
      </div>
    </Section>
  );
}
