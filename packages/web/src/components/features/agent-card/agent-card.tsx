"use client";

/**
 * `<AgentCard>` — single connected agent surface.
 *
 * Plan 24 phase 0: extracted from `agent-configs-section.tsx` so
 * `<AgentList>` and any future agent-detail surface render the same
 * card without re-implementing the section's data plumbing. The
 * component is presentation + interaction-callbacks; data fetching
 * (`useConnectedAgents`, `useAgentConfigs`, `useApiKeys`, mutations,
 * feedback hooks) lives in the consumer (`<AgentList>` / Settings'
 * `<AgentConfigsSection>`).
 *
 * Co-located helpers (file-private):
 *   - `formatScope`, `fileLabel`, `fileGroup`, `FileIcon`,
 *     `groupByScope` — config-file labelling for `<ScopeGroup>`
 *   - `formatRecentActivity` — bottom-line copy for the agent header
 *   - `ForgetFeedbackRow`, `RevokeFeedbackRow`, `DisconnectFeedbackRow`
 *     — inline structured feedback rows under their respective actions
 *   - `ScopeGroup`, `CopyButton` — building blocks of the expanded
 *     "Synced configs" panel
 *
 * Public exports (used by other surfaces and the dom test):
 *   - `AgentCard` — the main component
 *   - `getCredentialClassKind`, `CredentialClassPill`, `CredentialClassKind`
 *     — credential summary chip + kind classifier; settings + future
 *     agent surfaces share this contract
 */

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Surface,
} from "@memaxlabs/ui";
import {
  ChevronDown,
  ChevronRight,
  X,
  Copy,
  Check,
  Globe,
  FolderGit2,
  Brain,
  Settings2,
  Pencil,
  Activity,
  Clock,
  Key,
  Shield,
} from "lucide-react";
import type {
  AgentConfig,
  ApiKeyListItem,
  ConnectedAgentWithStats,
} from "memax-sdk";
import { resolveAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { pluralize } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { formatAge } from "@/lib/format-age";
import { useDestructiveAction } from "@/hooks/use-destructive-action";
import type { AgentConfigForgetFeedback } from "@/hooks/use-agent-config-forget";
import type { AgentDisconnectFeedback } from "@/hooks/use-connected-agents";
import type { ApiKeyRevokeFeedback } from "@/hooks/use-api-keys";
import { getAgentStatus } from "@/components/features/settings/shared/agent-status";
import { StatusDot, statusLabelKey } from "./agent-status-dot";

type DestructiveKey = string;

/* ── Helpers ── */

function formatScope(scope: string): string {
  if (scope === "global") return "Global";
  const match = scope.match(/\/([^/]+)$/);
  return match?.[1] ?? scope.replace("project:", "");
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

function fileGroup(path: string): string | null {
  if (path.startsWith("memory/feedback_")) return "feedback";
  if (path.startsWith("memory/project_")) return "project";
  if (path.startsWith("memory/reference_")) return "reference";
  if (path.startsWith("memory/user_")) return "user";
  if (path === "memory/MEMORY.md") return "index";
  return null;
}

function FileIcon({ path }: { path: string }) {
  if (path.startsWith("memory/"))
    return <Brain className="h-3 w-3 text-fg-3 shrink-0" />;
  return <Settings2 className="h-3 w-3 text-fg-3 shrink-0" />;
}

function groupByScope(configs: AgentConfig[]): [string, AgentConfig[]][] {
  const map = new Map<string, AgentConfig[]>();
  for (const c of configs) {
    const group = map.get(c.scope) ?? [];
    group.push(c);
    map.set(c.scope, group);
  }
  return [...map.entries()].sort(([a], [b]) => {
    if (a === "global") return -1;
    if (b === "global") return 1;
    return a.localeCompare(b);
  });
}

export type CredentialClassKind = "api_key" | "oauth" | "session";

export function getCredentialClassKind({
  hasAgentKey,
  isConnected,
}: {
  hasAgentKey: boolean;
  isConnected: boolean;
}): CredentialClassKind {
  if (hasAgentKey) return "api_key";
  if (isConnected) return "oauth";
  return "session";
}

export function CredentialClassPill({
  kind,
  t,
}: {
  kind: CredentialClassKind;
  t: Translations;
}) {
  const label =
    kind === "api_key"
      ? t.agentConfigs.credentialClassApiKey
      : kind === "oauth"
        ? t.agentConfigs.credentialClassOAuth
        : t.agentConfigs.credentialClassSession;

  return (
    <span className="text-[10px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded">
      {label}
    </span>
  );
}

function formatRecentActivity(
  agent: ConnectedAgentWithStats,
  t: Translations,
  interpolate: (s: string, vars: Record<string, string | number>) => string,
): string | null {
  if (!agent.last_activity_summary || !agent.last_operation) return null;
  switch (agent.last_operation) {
    case "ask":
      return interpolate(t.agentConfigs.lastActionAsk, {
        summary: agent.last_activity_summary,
      });
    case "recall":
      return interpolate(t.agentConfigs.lastActionRecall, {
        summary: agent.last_activity_summary,
      });
    case "push":
      return interpolate(t.agentConfigs.lastActionPush, {
        summary: agent.last_activity_summary,
      });
    default:
      return interpolate(t.agentConfigs.lastActionGeneric, {
        summary: agent.last_activity_summary,
      });
  }
}

/**
 * Inline feedback row beneath the Forget-all button. Owns the i18n copy
 * and visual treatment for the three feedback kinds the forget hook
 * produces (success / partial / error). Feedback stays inline — no
 * bar-toast routing for settings-style CRUD.
 */
function ForgetFeedbackRow({
  feedback,
  clearFeedback,
  t,
  interpolate,
}: {
  feedback: AgentConfigForgetFeedback | null;
  clearFeedback: () => void;
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
}) {
  if (!feedback) return null;

  const isError = feedback.kind === "error";
  const isPartial = feedback.kind === "partial";

  const message =
    feedback.kind === "success"
      ? feedback.deleted === 1
        ? t.agentConfigs.forgotConfig
        : interpolate(t.agentConfigs.forgotConfigs, { n: feedback.deleted })
      : feedback.kind === "partial"
        ? interpolate(t.agentConfigs.forgetPartial, {
            deleted: feedback.deleted,
            failed: feedback.failed,
          })
        : t.agentConfigs.forgetFailed;

  return (
    <div
      className="flex items-center justify-between gap-2 px-3 py-1 mt-1 text-[12px]"
      style={{
        color: isError || isPartial ? "var(--destructive)" : "var(--fg-3)",
      }}
    >
      <span className="truncate">{message}</span>
      <button
        onClick={clearFeedback}
        className="text-fg-3 hover:text-foreground cursor-pointer shrink-0"
        aria-label="Dismiss"
      >
        <X className="h-3 w-3" />
      </button>
    </div>
  );
}

/**
 * Inline feedback row beneath an API key revoke row. Same pattern as
 * ForgetFeedbackRow / DisconnectFeedbackRow — structured hook state
 * + local i18n copy.
 */
function RevokeFeedbackRow({
  feedback,
  clearFeedback,
  t,
}: {
  feedback: ApiKeyRevokeFeedback | null;
  clearFeedback: () => void;
  t: Translations;
}) {
  if (!feedback) return null;

  const isError = feedback.kind === "error";
  const message =
    feedback.kind === "success"
      ? t.agentConfigs.revoked
      : feedback.kind === "not_found"
        ? t.agentConfigs.revokedAlready
        : t.agentConfigs.revokeFailed;

  return (
    <div
      className="flex items-center justify-between gap-2 px-3 py-1 mt-1 text-[12px]"
      style={{
        color: isError ? "var(--destructive)" : "var(--fg-3)",
      }}
    >
      <span className="truncate">{message}</span>
      <button
        onClick={clearFeedback}
        className="text-fg-3 hover:text-foreground cursor-pointer shrink-0"
        aria-label="Dismiss"
      >
        <X className="h-3 w-3" />
      </button>
    </div>
  );
}

/**
 * Inline feedback row beneath the Disconnect button. Same pattern as
 * ForgetFeedbackRow.
 */
function DisconnectFeedbackRow({
  feedback,
  clearFeedback,
  agentDisplayName,
  t,
  interpolate,
}: {
  feedback: AgentDisconnectFeedback | null;
  clearFeedback: () => void;
  agentDisplayName: string;
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
}) {
  if (!feedback) return null;

  const isError = feedback.kind === "error";
  const message =
    feedback.kind === "success"
      ? [
          interpolate(t.agentConfigs.disconnectedWithCounts, {
            agent: agentDisplayName,
            keys: feedback.keysRevoked,
            configs: feedback.configsTombstoned,
          }),
          t.agentConfigs.disconnectMemoriesStay,
        ].join(" ")
      : feedback.kind === "not_found"
        ? interpolate(t.agentConfigs.disconnectedAlready, {
            agent: agentDisplayName,
          })
        : t.agentConfigs.disconnectFailed;

  return (
    <div
      className="flex items-center justify-between gap-2 px-3 py-1 mt-1 text-[12px]"
      style={{
        color: isError ? "var(--destructive)" : "var(--fg-3)",
      }}
    >
      <span className="truncate">{message}</span>
      <button
        onClick={clearFeedback}
        className="text-fg-3 hover:text-foreground cursor-pointer shrink-0"
        aria-label="Dismiss"
      >
        <X className="h-3 w-3" />
      </button>
    </div>
  );
}

/* ── Copy Button (used by ScopeGroup config preview) ── */

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={async (e) => {
        e.stopPropagation();
        await navigator.clipboard.writeText(text);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      }}
      className="absolute top-2 right-2 p-1 rounded text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
    >
      {copied ? (
        <Check className="h-3 w-3 text-emerald-500/70" />
      ) : (
        <Copy className="h-3 w-3" />
      )}
    </button>
  );
}

/* ── Scope Group ── */

function ScopeGroup({
  scope,
  configs,
  extractionCounts,
  onForgetConfigs,
  destructiveAction,
  previewId,
  setPreviewId,
  t,
  interpolate,
}: {
  scope: string;
  configs: AgentConfig[];
  extractionCounts: Record<string, number>;
  onForgetConfigs: (ids: string[]) => Promise<boolean>;
  destructiveAction: ReturnType<typeof useDestructiveAction<DestructiveKey>>;
  previewId: string | null;
  setPreviewId: (id: string | null) => void;
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
}) {
  const [open, setOpen] = useState(true);
  const project = formatScope(scope);
  const isGlobal = scope === "global";
  const ScopeIcon = isGlobal ? Globe : FolderGit2;
  const scopeExtracted = configs.reduce(
    (s, c) => s + (extractionCounts[c.id] ?? 0),
    0,
  );

  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-1.5 rounded-lg hover:bg-surface-2 touch-no-hover cursor-pointer transition-colors"
      >
        <ScopeIcon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        <span className="text-[13px] font-medium text-fg-1 flex-1 text-left truncate min-w-0">
          {project}
        </span>
        <span className="text-[11px] text-fg-3 tabular-nums shrink-0">
          {configs.length}
        </span>
        {scopeExtracted > 0 && (
          <>
            <span className="text-fg-3 text-[10px] shrink-0">·</span>
            <span className="text-[11px] text-fg-3 tabular-nums shrink-0 whitespace-nowrap">
              {scopeExtracted} extracted
            </span>
          </>
        )}
        {open ? (
          <ChevronDown className="h-3 w-3 text-fg-3 shrink-0" />
        ) : (
          <ChevronRight className="h-3 w-3 text-fg-3 shrink-0" />
        )}
      </button>

      {open && (
        <div className="ml-5 border-l border-border/20">
          {configs.map((c) => {
            const deleteKey = `delete-config:${c.id}`;
            const isDeleting = destructiveAction.isConfirming(deleteKey);
            const isDeletePending = destructiveAction.isRunning(deleteKey);
            const isPreviewing = previewId === c.id;
            const label = fileLabel(c.file_path);
            const group = fileGroup(c.file_path);
            const extracted = extractionCounts[c.id] ?? 0;

            if (isDeleting) {
              return (
                <div
                  key={c.id}
                  className="flex flex-col items-start gap-2 rounded-r-lg pl-3 pr-3 py-1.5 ml-0 sm:flex-row sm:items-center sm:justify-between"
                  style={{
                    backgroundColor:
                      "oklch(from var(--destructive) l c h / 0.08)",
                  }}
                >
                  <span className="text-[12px] text-fg-2">
                    {t.agentConfigs.forgetConfigConfirm}
                  </span>
                  <div className="flex items-center gap-2 shrink-0 ml-auto sm:ml-0">
                    <button
                      onClick={async () => {
                        await destructiveAction.run(deleteKey, () =>
                          onForgetConfigs([c.id]),
                        );
                      }}
                      disabled={isDeletePending}
                      className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
                    >
                      {isDeletePending
                        ? t.agentConfigs.forgetting
                        : t.agentConfigs.forgetConfig}
                    </button>
                    <button
                      onClick={() => destructiveAction.cancel(deleteKey)}
                      disabled={isDeletePending}
                      className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
                    >
                      {t.forget.keep}
                    </button>
                  </div>
                </div>
              );
            }

            return (
              <div key={c.id}>
                <div
                  className="flex items-center gap-2 pl-3 pr-3 py-1.5 rounded-r-lg text-[13px] hover:bg-surface-1 touch-no-hover cursor-pointer transition-colors"
                  onClick={() => setPreviewId(isPreviewing ? null : c.id)}
                >
                  <FileIcon path={c.file_path} />
                  <span className="text-fg-2 flex-1 truncate">{label}</span>
                  {/* group pill: hidden on mobile — scope is already
                      shown in the parent ScopeGroup header, and it eats
                      the narrow viewport's reserved trailing space. */}
                  {group && (
                    <span className="hidden sm:inline text-[10px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
                      {group}
                    </span>
                  )}
                  {extracted > 0 && (
                    <span className="text-[11px] text-fg-3 tabular-nums">
                      {extracted}
                    </span>
                  )}
                  <span className="text-[11px] text-fg-3 tabular-nums shrink-0">
                    v{c.version}
                  </span>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      destructiveAction.start(deleteKey);
                    }}
                    className="text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer shrink-0 flex items-center justify-center min-h-[44px] min-w-[44px] -my-2 -mr-2 sm:min-h-0 sm:min-w-0 sm:m-0 sm:p-0.5"
                    aria-label={t.agentConfigs.forgetConfig}
                  >
                    <X className="h-2.5 w-2.5" />
                  </button>
                </div>

                {isPreviewing && c.content && (
                  <div className="ml-3 mr-3 mb-1 rounded-lg bg-surface-1 border border-border/30 overflow-hidden relative">
                    <CopyButton text={c.content} />
                    <pre className="text-[12px] text-fg-2 font-mono leading-relaxed p-3 pr-9 max-h-48 overflow-y-auto whitespace-pre-wrap break-words">
                      {c.content}
                    </pre>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

/* ── Agent Card ── */

export interface AgentCardProps {
  agent: ConnectedAgentWithStats;
  configs: AgentConfig[];
  keys: ApiKeyListItem[];
  hubNames: Map<string, string>;
  extractionCounts: Record<string, number>;
  /**
   * When set, clicking the card header invokes this handler instead
   * of toggling the inline expanded view. Used by `<AgentList
   * expandable={false}>` so the `/agents` route navigates to
   * `/agents/<slug>` on tap. When unset, the card behaves as the
   * Settings disclosure (click to expand/collapse).
   */
  onCardClick?: () => void;
  /**
   * Force the expanded body open from the consumer. Defaults to
   * `false`; the card manages an internal toggle when this prop is
   * unset. `<AgentDetail>` passes `forceExpanded` so the detail
   * page renders a card that's always open.
   */
  forceExpanded?: boolean;
  onForgetConfigs: (ids: string[]) => Promise<boolean>;
  forgetFeedback: AgentConfigForgetFeedback | null;
  clearForgetFeedback: () => void;
  onRevokeKey: (id: string) => Promise<boolean>;
  revokeFeedback: ApiKeyRevokeFeedback | null;
  clearRevokeFeedback: () => void;
  onUpdateAgent: (slug: string, displayName?: string, icon?: string) => void;
  onDisconnect: (slug: string) => Promise<boolean>;
  disconnectFeedback: AgentDisconnectFeedback | null;
  clearDisconnectFeedback: () => void;
  hideKeyManagement?: boolean;
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
}

export function AgentCard({
  agent,
  configs,
  keys,
  hubNames,
  extractionCounts,
  onCardClick,
  forceExpanded,
  onForgetConfigs,
  forgetFeedback,
  clearForgetFeedback,
  onRevokeKey,
  revokeFeedback,
  clearRevokeFeedback,
  onUpdateAgent,
  onDisconnect,
  disconnectFeedback,
  clearDisconnectFeedback,
  hideKeyManagement,
  t,
  interpolate,
}: AgentCardProps) {
  const identity = resolveAgentIdentity(agent.agent_name, agent);
  const AgentIcon = identity.icon;
  // Memax is the always-on platform agent — synthesized server-side
  // (postgres_agent_sync: ListConnectedAgentsWithStats), never stored
  // in connected_agents. Hide the mutate affordances (rename, icon
  // edit, key management, disconnect) so the UI doesn't offer
  // actions the server would 400 on (builtin_agent_immutable).
  const isBuiltIn = agent.agent_name === "memax";
  // Treat "hide keys" + "hide disconnect" as one flag rather than
  // sprinkling `isBuiltIn` checks throughout the JSX.
  const hideMutations = isBuiltIn;
  const effectiveHideKeyManagement = hideKeyManagement || hideMutations;

  // Internal toggle state — only matters when neither `onCardClick`
  // nor `forceExpanded` is set. With `forceExpanded`, the body is
  // always open. With `onCardClick`, the body is always closed and
  // the click navigates instead.
  const [internalExpanded, setInternalExpanded] = useState(false);
  const expanded = forceExpanded
    ? true
    : onCardClick
      ? false
      : internalExpanded;
  const setExpanded = setInternalExpanded;
  const [renaming, setRenaming] = useState(false);
  const [renameDraft, setRenameDraft] = useState(identity.displayName);
  const [editingIcon, setEditingIcon] = useState(false);
  const [iconDraft, setIconDraft] = useState(agent.icon || "");
  const [reconnectDialogOpen, setReconnectDialogOpen] = useState(false);
  const destructiveAction = useDestructiveAction<DestructiveKey>();
  const [previewId, setPreviewId] = useState<string | null>(null);

  const lastObservedAt = agent.last_observed_at ?? agent.last_active_at;
  const status = getAgentStatus(agent);
  const recentActivity = formatRecentActivity(agent, t, interpolate);
  const displayName = agent.needs_reconnect
    ? t.agentConfigs.unnamedAgent
    : identity.displayName;
  const credentialClassKind = getCredentialClassKind({
    hasAgentKey: keys.length > 0,
    isConnected: true,
  });

  const totalDistilled = configs.reduce(
    (sum, c) => sum + (extractionCounts[c.id] ?? 0),
    0,
  );

  const handleRename = () => {
    const trimmed = renameDraft.trim();
    if (trimmed && trimmed !== identity.displayName) {
      onUpdateAgent(agent.agent_name, trimmed, undefined);
    }
    setRenaming(false);
  };

  const handleIcon = () => {
    const trimmed = iconDraft.trim();
    if (trimmed !== (agent.icon || "")) {
      onUpdateAgent(agent.agent_name, undefined, trimmed);
    }
    setEditingIcon(false);
  };

  const forgetAllKey = `forget-all:${agent.agent_name}`;
  const disconnectKey = `disconnect:${agent.agent_name}`;

  return (
    <Surface variant="subtle" rounded="2xl" className="px-5 py-4">
      {/* Header */}
      <button
        onClick={() => {
          if (onCardClick) {
            onCardClick();
            return;
          }
          if (forceExpanded) return; // detail page: body always open
          setExpanded(!expanded);
        }}
        className="w-full flex items-center gap-3 cursor-pointer group"
      >
        {/* Agent icon — emoji override or Lucide */}
        {editingIcon ? (
          <div
            className="flex items-center justify-center h-9 w-9 rounded-chrome shrink-0"
            style={{
              background: `oklch(from ${identity.color} l c h / 0.12)`,
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <input
              type="text"
              value={iconDraft}
              onChange={(e) => setIconDraft(e.target.value.slice(0, 2))}
              onBlur={handleIcon}
              onKeyDown={(e) => e.key === "Enter" && handleIcon()}
              className="w-6 text-center text-[16px] bg-transparent border-none outline-none"
              autoFocus
              placeholder="🤖"
            />
          </div>
        ) : (
          <div
            className={`flex items-center justify-center h-9 w-9 rounded-chrome shrink-0 ${
              onCardClick ? "" : "cursor-pointer"
            }`}
            style={{
              background: `oklch(from ${identity.color} l c h / 0.12)`,
            }}
            onClick={
              onCardClick || hideMutations
                ? undefined
                : (e) => {
                    // In navigation mode (onCardClick set) the icon
                    // shouldn't intercept — the whole row drills to
                    // /agents/<slug>. Inline edit only fires when the
                    // card is in disclosure mode (Settings). Built-in
                    // agents skip icon edit (identity is owned by
                    // AGENT_IDENTITIES).
                    e.stopPropagation();
                    setEditingIcon(true);
                    setIconDraft(agent.icon || "");
                  }
            }
            title={
              onCardClick || hideMutations
                ? undefined
                : t.agentConfigs.changeIcon
            }
          >
            {agent.icon ? (
              <span className="text-[16px]">{agent.icon}</span>
            ) : (
              <AgentIcon
                className="h-4.5 w-4.5"
                style={{ color: identity.color }}
                strokeWidth={1.8}
              />
            )}
          </div>
        )}

        <div className="flex-1 min-w-0 text-left">
          {/* Name + status: name truncates, status stays fully visible.
              Without min-w-0 flex-1 truncate on the name, a long display
              name (zh-CN display + a slug suffix) would push the status
              label and chevron into overflow on mobile. */}
          <div className="flex items-center gap-2 min-w-0">
            {renaming ? (
              <input
                type="text"
                value={renameDraft}
                onChange={(e) => setRenameDraft(e.target.value)}
                onBlur={handleRename}
                onKeyDown={(e) => e.key === "Enter" && handleRename()}
                className="text-[14px] font-medium text-fg-1 bg-transparent border-none outline-none min-w-0 flex-1"
                style={{ borderBottom: "1px solid var(--signature)" }}
                autoFocus
                onClick={(e) => e.stopPropagation()}
              />
            ) : (
              <span
                className={`text-[14px] font-medium text-fg-1 group-hover:text-foreground transition-colors min-w-0 flex-1 truncate ${
                  onCardClick || hideMutations ? "" : "cursor-text"
                }`}
                onClick={
                  onCardClick || hideMutations
                    ? undefined
                    : (e) => {
                        // Navigation-mode (onCardClick set): the row
                        // drills to /agents/<slug>; inline rename only
                        // fires from disclosure mode (Settings).
                        // Built-in agents (memax) skip rename entirely —
                        // identity is owned by AGENT_IDENTITIES.
                        if (agent.needs_reconnect) return;
                        e.stopPropagation();
                        setRenaming(true);
                        setRenameDraft(displayName);
                      }
                }
                title={displayName}
              >
                {displayName}
              </span>
            )}
            <span className="shrink-0">
              <StatusDot status={status} />
            </span>
            <span className="text-[12px] sm:text-[11px] text-fg-3 shrink-0 whitespace-nowrap">
              {t.agentConfigs[statusLabelKey(status)]}
            </span>
          </div>
          {/* Metrics row: wraps on mobile. gap-y-1 keeps wrapped lines
              readable. Desktop unchanged (all four chips fit one line). */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mt-0.5 text-[12px] text-fg-3">
            {agent.memory_count > 0 && (
              <span className="tabular-nums flex items-center gap-1">
                <Activity className="h-2.5 w-2.5" />
                {interpolate(t.agentConfigs.memoriesPushed, {
                  n: String(agent.memory_count),
                })}
              </span>
            )}
            {agent.key_count > 0 && (
              <span className="tabular-nums">
                {agent.key_count} {agent.key_count === 1 ? "key" : "keys"}
              </span>
            )}
            {agent.config_count > 0 && (
              <span className="tabular-nums">
                {agent.config_count === 1
                  ? t.agentConfigs.file
                  : interpolate(t.agentConfigs.files, {
                      n: String(agent.config_count),
                    })}
              </span>
            )}
            {lastObservedAt && (
              <span className="flex items-center gap-1">
                <Clock className="h-2.5 w-2.5" />
                {interpolate(t.agentConfigs.lastActivity, {
                  time: formatAge(lastObservedAt, t, interpolate),
                })}
              </span>
            )}
          </div>
          {recentActivity && (
            <div
              className="mt-1 text-[12px] text-fg-2 line-clamp-1 sm:line-clamp-2"
              title={recentActivity}
            >
              {recentActivity}
            </div>
          )}
        </div>

        {/* Trailing affordance: detail-page (forceExpanded) hides the
            chevron entirely (the body IS the page); navigation cards
            (onCardClick) show a right-chevron as a "tap to drill"
            hint; default disclosure cards toggle down ↔ right. */}
        {forceExpanded ? null : onCardClick || !expanded ? (
          <ChevronRight className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        ) : (
          <ChevronDown className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        )}
      </button>

      {/* Expanded detail */}
      {expanded && (
        <div className="mt-3 space-y-3">
          {/* ── Agent identity ── */}
          <div className="flex flex-col gap-2 px-3 py-2 rounded-surface bg-surface-1 text-[12px] text-fg-3 sm:flex-row sm:items-center sm:gap-3">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0 flex-1">
              <span className="text-fg-4">
                {t.agentConfigs.displayNameLabel}
              </span>
              <span className="font-medium text-fg-2">{displayName}</span>
              <span className="text-fg-4 hidden sm:inline">·</span>
              <span className="text-fg-4">{t.agentConfigs.agentSlugLabel}</span>
              <span
                className="font-mono text-fg-2 truncate min-w-0"
                title={agent.agent_name}
              >
                {agent.agent_name}
              </span>
              <CredentialClassPill kind={credentialClassKind} t={t} />
            </div>
            {agent.needs_reconnect ? (
              <>
                <button
                  onClick={() => setReconnectDialogOpen(true)}
                  className="flex items-center gap-1 rounded-lg border border-border bg-surface-1 px-2 py-1 text-[12px] text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
                >
                  {t.agentConfigs.reconnect}
                </button>
                <Dialog
                  open={reconnectDialogOpen}
                  onOpenChange={setReconnectDialogOpen}
                >
                  <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                      <DialogTitle>{t.agentConfigs.reconnect}</DialogTitle>
                      <DialogDescription>
                        {t.agentConfigs.reconnectExplanation}
                      </DialogDescription>
                    </DialogHeader>
                    <DialogFooter className="mt-2">
                      <button
                        onClick={() => setReconnectDialogOpen(false)}
                        className="inline-flex items-center justify-center rounded-lg border border-border bg-background px-3 py-2 text-[13px] font-medium text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1 cursor-pointer"
                      >
                        {t.forget.keep}
                      </button>
                      <button
                        onClick={async () => {
                          clearDisconnectFeedback();
                          const ok = await onDisconnect(agent.agent_name);
                          if (ok) setReconnectDialogOpen(false);
                        }}
                        className="inline-flex items-center justify-center rounded-lg bg-foreground px-3 py-2 text-[13px] font-medium text-background transition-colors hover:opacity-90 cursor-pointer"
                      >
                        {t.agentConfigs.disconnectCredential}
                      </button>
                    </DialogFooter>
                  </DialogContent>
                </Dialog>
              </>
            ) : hideMutations ? null : (
              <button
                onClick={() => {
                  setRenaming(true);
                  setRenameDraft(displayName);
                }}
                className="flex items-center gap-1 text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
              >
                <Pencil className="h-2.5 w-2.5" />
                {t.agentConfigs.editName}
              </button>
            )}
          </div>

          {/* ── API Keys (hidden when key management is separate or
              when the row is the synthetic built-in Memax actor) ── */}
          {!effectiveHideKeyManagement && (
            <div>
              <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-2 px-1 flex items-center gap-1.5">
                <Key className="h-3 w-3" />
                {t.agentConfigs.apiKeys}
              </p>
              {keys.length === 0 ? (
                <p className="text-[12px] text-fg-4 px-1">
                  {t.agentConfigs.noKeys}
                </p>
              ) : (
                <div className="space-y-1.5">
                  {keys.map((k) => {
                    const rowFeedback =
                      revokeFeedback?.id === k.id ? revokeFeedback : null;
                    return (
                      <div key={k.id}>
                        {destructiveAction.isConfirming(`revoke:${k.id}`) ? (
                          <div
                            className="flex flex-col items-start gap-2 rounded-surface px-3 py-2.5 transition-colors sm:flex-row sm:items-center sm:justify-between"
                            style={{
                              backgroundColor:
                                "oklch(from var(--destructive) l c h / 0.08)",
                            }}
                          >
                            <span className="text-[12px] text-fg-2">
                              {t.agentConfigs.revokeConfirm}
                            </span>
                            <div className="flex items-center gap-2.5 shrink-0 ml-auto sm:ml-0">
                              <button
                                onClick={() => {
                                  clearRevokeFeedback();
                                  void destructiveAction.run(
                                    `revoke:${k.id}`,
                                    () => onRevokeKey(k.id),
                                  );
                                }}
                                disabled={destructiveAction.isRunning(
                                  `revoke:${k.id}`,
                                )}
                                className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
                              >
                                {destructiveAction.isRunning(`revoke:${k.id}`)
                                  ? t.agentConfigs.revoking
                                  : t.agentConfigs.revoke}
                              </button>
                              <button
                                onClick={() =>
                                  destructiveAction.cancel(`revoke:${k.id}`)
                                }
                                disabled={destructiveAction.isRunning(
                                  `revoke:${k.id}`,
                                )}
                                className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
                              >
                                {t.forget.keep}
                              </button>
                            </div>
                          </div>
                        ) : (
                          <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-surface px-3 py-2.5 bg-surface-1 hover:bg-surface-2 transition-colors touch-no-hover">
                            <span className="text-[13px] text-fg-2 font-mono tabular-nums">
                              {k.prefix}····
                            </span>
                            {k.hub_id ? (
                              <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded flex items-center gap-1 min-w-0">
                                <Shield className="h-2.5 w-2.5 shrink-0" />
                                <span className="truncate">
                                  {hubNames.get(k.hub_id) ?? k.hub_id}
                                </span>
                              </span>
                            ) : (
                              <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded flex items-center gap-1">
                                <Globe className="h-2.5 w-2.5" />
                                {t.agentConfigs.allHubs}
                              </span>
                            )}
                            <div className="ml-auto flex items-center gap-2.5">
                              {k.last_used && (
                                <span className="text-[11px] text-fg-3 tabular-nums flex items-center gap-1">
                                  <Activity className="h-2.5 w-2.5" />
                                  {formatAge(k.last_used, t, interpolate)}
                                </span>
                              )}
                              <button
                                onClick={() =>
                                  destructiveAction.start(`revoke:${k.id}`)
                                }
                                className="text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer shrink-0 flex items-center justify-center min-h-[44px] min-w-[44px] -my-2 -mr-2 sm:min-h-0 sm:min-w-0 sm:m-0 sm:p-0.5"
                                aria-label={t.agentConfigs.revoke}
                              >
                                <X className="h-3 w-3" />
                              </button>
                            </div>
                          </div>
                        )}
                        <RevokeFeedbackRow
                          feedback={rowFeedback}
                          clearFeedback={clearRevokeFeedback}
                          t={t}
                        />
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          )}

          {/* ── Config Files ── */}
          {configs.length > 0 && (
            <div>
              <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium mb-1.5 px-1 flex items-center gap-1.5">
                <Settings2 className="h-3 w-3" />
                {t.agentConfigs.syncedConfigs}
                {totalDistilled > 0 && (
                  <span className="text-fg-4 font-normal">
                    ·{" "}
                    {totalDistilled === 1
                      ? t.agentConfigs.distilledOne
                      : interpolate(t.agentConfigs.distilled, {
                          n: String(totalDistilled),
                        })}
                  </span>
                )}
              </p>
              <div className="space-y-1">
                {groupByScope(configs).map(([scope, scopeConfigs]) => (
                  <ScopeGroup
                    key={scope}
                    scope={scope}
                    configs={scopeConfigs}
                    extractionCounts={extractionCounts}
                    onForgetConfigs={onForgetConfigs}
                    destructiveAction={destructiveAction}
                    previewId={previewId}
                    setPreviewId={setPreviewId}
                    t={t}
                    interpolate={interpolate}
                  />
                ))}
              </div>
            </div>
          )}

          {/* Forget all configs */}
          {configs.length > 1 && (
            <div className="pt-2 mt-1 border-t border-border/30">
              {destructiveAction.isConfirming(forgetAllKey) ? (
                <div
                  className="flex flex-col items-start gap-2 rounded-lg px-3 py-2.5 transition-colors sm:flex-row sm:items-center sm:justify-between"
                  style={{
                    backgroundColor:
                      "oklch(from var(--destructive) l c h / 0.08)",
                  }}
                >
                  <span className="text-[13px] text-fg-2">
                    {interpolate(t.agentConfigs.forgetAllConfirm, {
                      n: String(configs.length),
                      agent: displayName,
                    })}
                  </span>
                  <div className="flex items-center gap-2.5 shrink-0 ml-auto sm:ml-0">
                    <button
                      onClick={() => {
                        clearForgetFeedback();
                        void destructiveAction.run(forgetAllKey, () =>
                          onForgetConfigs(configs.map((c) => c.id)),
                        );
                      }}
                      disabled={destructiveAction.isRunning(forgetAllKey)}
                      className="text-[13px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
                    >
                      {destructiveAction.isRunning(forgetAllKey)
                        ? t.agentConfigs.forgetting
                        : t.agentConfigs.forgetConfig}
                    </button>
                    <button
                      onClick={() => destructiveAction.cancel(forgetAllKey)}
                      disabled={destructiveAction.isRunning(forgetAllKey)}
                      className="text-[13px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
                    >
                      {t.forget.keep}
                    </button>
                  </div>
                </div>
              ) : (
                <button
                  onClick={() => destructiveAction.start(forgetAllKey)}
                  className="text-[13px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer px-3 py-1"
                >
                  {t.agentConfigs.forgetAll}
                </button>
              )}
              <ForgetFeedbackRow
                feedback={forgetFeedback}
                clearFeedback={clearForgetFeedback}
                t={t}
                interpolate={interpolate}
              />
            </div>
          )}

          {/* Disconnect agent — two-stage. Built-in agents (memax)
              have nothing to disconnect, and the server rejects the
              mutate with `builtin_agent_immutable`, so we suppress
              the whole footer for them. */}
          {!hideMutations && (
            <div className="pt-2 mt-1 border-t border-border/30">
              {destructiveAction.isConfirming(disconnectKey) ? (
                <div
                  className="flex flex-col items-start gap-2 rounded-lg px-3 py-2.5 transition-colors sm:flex-row sm:items-center sm:justify-between"
                  style={{
                    backgroundColor:
                      "oklch(from var(--destructive) l c h / 0.08)",
                  }}
                >
                  <span className="text-[13px] text-fg-2">
                    {interpolate(t.agentConfigs.disconnectConfirm, {
                      agent: displayName,
                    })}
                  </span>
                  <div className="flex items-center gap-2.5 shrink-0 ml-auto sm:ml-0">
                    <button
                      onClick={() => {
                        clearDisconnectFeedback();
                        void destructiveAction.run(disconnectKey, () =>
                          onDisconnect(agent.agent_name),
                        );
                      }}
                      disabled={destructiveAction.isRunning(disconnectKey)}
                      className="text-[13px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
                    >
                      {destructiveAction.isRunning(disconnectKey)
                        ? t.agentConfigs.disconnecting
                        : t.agentConfigs.disconnect}
                    </button>
                    <button
                      onClick={() => destructiveAction.cancel(disconnectKey)}
                      disabled={destructiveAction.isRunning(disconnectKey)}
                      className="text-[13px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
                    >
                      {t.forget.keep}
                    </button>
                  </div>
                </div>
              ) : (
                <button
                  onClick={() => destructiveAction.start(disconnectKey)}
                  className="text-[13px] text-fg-3 hover:text-destructive/60 transition-colors cursor-pointer px-3 py-1"
                >
                  {t.agentConfigs.disconnect}
                </button>
              )}
              <DisconnectFeedbackRow
                feedback={disconnectFeedback}
                clearFeedback={clearDisconnectFeedback}
                agentDisplayName={displayName}
                t={t}
                interpolate={interpolate}
              />
            </div>
          )}

          {/* ── Activity Summary ──
              Desktop only. The collapsed header already shows memory
              count + last-activity + recentActivity line; on mobile
              this block is pure duplication. */}
          {(agent.memory_count > 0 || lastObservedAt || recentActivity) && (
            <div className="hidden sm:flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 rounded-surface bg-surface-1 text-[12px] text-fg-3">
              {agent.memory_count > 0 && (
                <span className="flex items-center gap-1.5">
                  <Activity className="h-3 w-3" />
                  {pluralize(
                    t.agentConfigs.memoriesPushedLongOne,
                    t.agentConfigs.memoriesPushedLong,
                    agent.memory_count,
                  )}
                </span>
              )}
              {lastObservedAt && (
                <span className="flex items-center gap-1.5">
                  <Clock className="h-3 w-3" />
                  {interpolate(t.agentConfigs.lastActivity, {
                    time: formatAge(lastObservedAt, t, interpolate),
                  })}
                </span>
              )}
              {recentActivity && (
                <span className="text-fg-2 min-w-0 truncate">
                  {recentActivity}
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </Surface>
  );
}
