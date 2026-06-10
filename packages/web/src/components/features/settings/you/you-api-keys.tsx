"use client";

import { useState } from "react";
import { Key, Plus, Copy, Check } from "lucide-react";
import { Surface } from "@memaxlabs/ui";
import { DOCS_URL } from "@/lib/urls";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { useAuth } from "@/lib/auth";
import {
  useApiKeys,
  useRevokeApiKey,
  useUpdateApiKey,
} from "@/hooks/use-api-keys";
import { useConnectedAgents } from "@/hooks/use-connected-agents";
import { useDestructiveAction } from "@/hooks/use-destructive-action";
import { getMemaxClient } from "@/lib/memax-client";
import { getHubDisplayInitial } from "@/lib/hub-display";
import { getHubAccentStyle } from "@memaxlabs/ui";
import { Section } from "../shared/section";
import { AssignAgentControl } from "./assign-agent-control";

export function YouApiKeys() {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { hubs } = useAuth();
  const { data: keys } = useApiKeys();
  const { data: connectedAgents } = useConnectedAgents();
  const { revokeKey, feedback, clearFeedback } = useRevokeApiKey();
  const { updateKey, pendingKeyId: updatePendingKeyId } = useUpdateApiKey();
  const revokeAction = useDestructiveAction<string>();
  const [showCreate, setShowCreate] = useState(false);

  return (
    <>
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-baseline gap-2 px-1">
          <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider">
            {t.apiKeys?.title ?? "API Keys"}
          </p>
          <a
            href={`${DOCS_URL}/api/authentication`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-[11px] font-medium text-fg-4 transition-colors hover:text-fg-2"
          >
            {t.apiKeys?.learnMore ?? "Learn how to use API keys →"}
          </a>
        </div>
        <button
          onClick={() => setShowCreate(!showCreate)}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-[13px] text-fg-2 hover:text-foreground hover:bg-surface-2 transition-colors cursor-pointer"
        >
          <Plus className="h-3.5 w-3.5" />
          {t.apiKeys?.create ?? "Create Key"}
        </button>
      </div>

      {showCreate && (
        <CreateKeyForm
          hubs={hubs}
          agents={connectedAgents ?? []}
          t={t}
          onCreated={() => {
            // Key will appear in list via query invalidation
          }}
          onClose={() => setShowCreate(false)}
        />
      )}

      {(!keys || keys.length === 0) && !showCreate ? (
        <Section>
          <div className="flex flex-col items-center py-8 text-center">
            <div
              className="mb-4 flex h-11 w-11 items-center justify-center rounded-surface"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.05)",
              }}
            >
              <Key className="h-5 w-5 text-fg-3" />
            </div>
            <p className="text-[15px] text-fg-2 mb-1">
              {t.apiKeys?.emptyTitle ?? "No API keys"}
            </p>
            <p className="text-[14px] text-fg-3 max-w-[28rem]">
              {t.apiKeys?.emptyDesc ??
                "API keys authenticate agents and scripts to push and recall memories."}
            </p>
            <a
              href={`${DOCS_URL}/api/authentication`}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-4 text-[13px] font-medium text-fg-2 transition-colors hover:text-fg-1"
            >
              {t.apiKeys?.learnMore ?? "Learn how to use API keys →"}
            </a>
          </div>
        </Section>
      ) : (
        <Surface variant="subtle" rounded="2xl" className="overflow-hidden">
          {(keys ?? []).map((key, index) => {
            const hubName = key.hub_id
              ? hubs.find((h) => h.hub.id === key.hub_id)?.hub.name
              : null;
            const isRevoking = revokeAction.isConfirming(key.id);
            const isRunning = revokeAction.isRunning(key.id);
            const revokeFeedback = feedback?.id === key.id ? feedback : null;

            return (
              <div
                key={key.id}
                className={`flex items-center gap-3 px-4 py-3 ${
                  index > 0 ? "border-t border-border/30" : ""
                }`}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <code className="text-[13px] text-fg-2 font-mono">
                      {key.prefix}...
                    </code>
                    {key.name && (
                      <span className="text-[12px] text-fg-3 truncate">
                        {key.name}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-2 mt-0.5 text-[12px] text-fg-3">
                    {/* Always-interactive attribution pill. The same
                     * control renders for every state — linked to an
                     * agent, explicitly standalone, or unassigned —
                     * so reassignment is never a one-shot operation.
                     * The menu inside adapts to current state. */}
                    <AssignAgentControl
                      keyId={key.id}
                      keyName={key.name}
                      agents={connectedAgents ?? []}
                      currentAgent={key.agent_name ?? ""}
                      standalone={Boolean(key.standalone)}
                      busy={updatePendingKeyId === key.id}
                      onAssign={(slug) =>
                        updateKey(key.id, { agent_name: slug })
                      }
                      onStandalone={(value) =>
                        // Setting standalone=true on a linked key must
                        // also clear agent_name so the classifier flips
                        // correctly (it otherwise shows the agent badge
                        // because "linked" wins first-branch). Server
                        // auto-clears too, but sending both fields makes
                        // the optimistic update honest.
                        updateKey(
                          key.id,
                          value
                            ? { agent_name: "", standalone: true }
                            : { standalone: false },
                        )
                      }
                      onClear={() =>
                        updateKey(key.id, {
                          agent_name: "",
                          standalone: false,
                        })
                      }
                      t={t}
                    />
                    <span className="text-fg-4">·</span>
                    <span>
                      {hubName ? hubName : (t.apiKeys?.global ?? "Global")}
                    </span>
                    {key.last_used && (
                      <>
                        <span className="text-fg-4">·</span>
                        <span>{formatAge(key.last_used, t, interpolate)}</span>
                      </>
                    )}
                  </div>
                </div>

                {revokeFeedback ? (
                  <span
                    className={`text-[12px] ${
                      revokeFeedback.kind === "success"
                        ? "text-emerald-500/70"
                        : revokeFeedback.kind === "not_found"
                          ? "text-fg-3"
                          : "text-destructive/60"
                    }`}
                  >
                    {revokeFeedback.kind === "success"
                      ? (t.agentConfigs?.revoked ?? "Revoked")
                      : revokeFeedback.kind === "not_found"
                        ? (t.agentConfigs?.revokedAlready ?? "Already revoked")
                        : (t.agentConfigs?.revokeFailed ?? "Failed")}
                  </span>
                ) : isRevoking ? (
                  <div className="flex items-center gap-2">
                    <button
                      onClick={async () => {
                        await revokeAction.run(key.id, () => revokeKey(key.id));
                      }}
                      disabled={isRunning}
                      className="text-[12px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait"
                    >
                      {t.agentConfigs?.revokeConfirm ?? "Confirm"}
                    </button>
                    <button
                      onClick={() => revokeAction.cancel(key.id)}
                      className="text-[12px] text-fg-2 hover:text-foreground font-medium cursor-pointer"
                    >
                      {t.forget?.keep ?? "Keep"}
                    </button>
                  </div>
                ) : (
                  <button
                    onClick={() => revokeAction.start(key.id)}
                    className="text-[12px] text-fg-3 hover:text-destructive/70 transition-colors cursor-pointer"
                  >
                    {t.agentConfigs?.revoke ?? "Revoke"}
                  </button>
                )}
              </div>
            );
          })}
        </Surface>
      )}

      {keys && keys.length > 0 && (
        <p className="mt-2 text-[12px] text-fg-4 px-1">
          {(
            t.apiKeys?.summaryWithStandalone ??
            "{total} keys · {linked} linked · {standalone} standalone · {unassigned} unassigned"
          )
            .replace("{total}", String(keys.length))
            .replace(
              "{linked}",
              String(keys.filter((k) => k.agent_name).length),
            )
            .replace(
              "{standalone}",
              String(keys.filter((k) => !k.agent_name && k.standalone).length),
            )
            .replace(
              "{unassigned}",
              String(keys.filter((k) => !k.agent_name && !k.standalone).length),
            )}
        </p>
      )}
    </>
  );
}

function CreateKeyForm({
  hubs,
  agents,
  t,
  onCreated,
  onClose,
}: {
  hubs: ReturnType<typeof useAuth>["hubs"];
  agents: Array<{ agent_name: string; display_name?: string | null }>;
  t: ReturnType<typeof useLocale>["t"];
  onCreated: () => void;
  onClose: () => void;
}) {
  const [label, setLabel] = useState("");
  const [hubId, setHubId] = useState<string>("");
  const [agentName, setAgentName] = useState("");
  const [creating, setCreating] = useState(false);
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const handleCreate = async () => {
    if (!label.trim() || creating) return;
    setCreating(true);
    try {
      const result = await getMemaxClient().auth.createKey({
        name: label.trim(),
        hubId: hubId || undefined,
        agentName: agentName || undefined,
      });
      setCreatedKey(result.key);
      // Invalidate key list
      const { queryClient } = await import("@/lib/query-client");
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      onCreated();
    } catch (err) {
      console.error("Failed to create API key:", err);
    } finally {
      setCreating(false);
    }
  };

  const handleCopy = () => {
    if (createdKey) {
      navigator.clipboard.writeText(createdKey);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  if (createdKey) {
    return (
      <Surface variant="subtle" rounded="2xl" className="px-5 py-4 mb-4">
        <div className="flex items-center gap-2 mb-3">
          <Check className="h-4 w-4 text-emerald-500" />
          <p className="text-[14px] font-medium text-fg-1">
            {t.apiKeys?.created ?? "Key created"}
          </p>
        </div>
        <div className="flex items-center gap-2 mb-3">
          <code className="flex-1 text-[13px] font-mono text-fg-2 bg-surface-1 px-3 py-2 rounded-lg truncate">
            {createdKey}
          </code>
          <button
            onClick={handleCopy}
            className="shrink-0 p-2 rounded-lg hover:bg-surface-2 transition-colors cursor-pointer"
          >
            {copied ? (
              <Check className="h-4 w-4 text-emerald-500" />
            ) : (
              <Copy className="h-4 w-4 text-fg-3" />
            )}
          </button>
        </div>
        <p className="text-[12px] text-fg-3 mb-3">
          {t.apiKeys?.revealOnce ??
            "This key will only be shown once. Store it securely."}
        </p>
        <button
          onClick={onClose}
          className="text-[13px] text-fg-2 hover:text-foreground transition-colors cursor-pointer"
        >
          {t.apiKeys?.done ?? "Done"}
        </button>
      </Surface>
    );
  }

  return (
    <Surface variant="subtle" rounded="2xl" className="px-5 py-4 mb-4">
      <p className="text-[14px] font-medium text-fg-1 mb-3">
        {t.apiKeys?.create ?? "Create API Key"}
      </p>
      <div className="space-y-3">
        <div>
          <label className="text-[13px] text-fg-3 mb-1 block">
            {t.apiKeys?.label ?? "Label"}
          </label>
          <input
            type="text"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder={
              t.apiKeys?.placeholder ?? "Claude Code on laptop, CI deploy..."
            }
            className="w-full rounded-lg bg-surface-1 border border-border/60 px-3 py-2 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-foreground/20 transition-colors"
            onKeyDown={(e) => {
              if (e.key === "Enter") handleCreate();
            }}
          />
        </div>
        <div>
          <label className="text-[13px] text-fg-3 mb-1 block">
            {t.apiKeys?.scope ?? "Scope"}
          </label>
          <select
            value={hubId}
            onChange={(e) => setHubId(e.target.value)}
            className="w-full rounded-lg bg-surface-1 border border-border/60 px-3 py-2 text-[14px] text-fg-1 outline-none focus:border-foreground/20 transition-colors cursor-pointer"
          >
            <option value="">{t.apiKeys?.global ?? "Global"}</option>
            {hubs.map((h) => (
              <option key={h.hub.id} value={h.hub.id}>
                {h.hub.name}
              </option>
            ))}
          </select>
        </div>
        {agents.length > 0 && (
          <div>
            <label className="text-[13px] text-fg-3 mb-1 block">
              {t.apiKeys?.agentLabel ?? "Agent (optional)"}
            </label>
            <select
              value={agentName}
              onChange={(e) => setAgentName(e.target.value)}
              className="w-full rounded-lg bg-surface-1 border border-border/60 px-3 py-2 text-[14px] text-fg-1 outline-none focus:border-foreground/20 transition-colors cursor-pointer"
            >
              <option value="">
                {t.apiKeys?.agentPlaceholderNone ?? "No agent"}
              </option>
              {agents.map((a) => (
                <option key={a.agent_name} value={a.agent_name}>
                  {a.display_name || a.agent_name}
                </option>
              ))}
            </select>
          </div>
        )}
        <div className="flex items-center gap-2 pt-1">
          <button
            onClick={handleCreate}
            disabled={!label.trim() || creating}
            className="px-4 py-2 rounded-lg text-[13px] font-medium bg-foreground text-background hover:opacity-90 transition-opacity cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {creating
              ? (t.apiKeys?.creating ?? "Creating...")
              : (t.apiKeys?.create ?? "Create")}
          </button>
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg text-[13px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {t.apiKeys?.cancel ?? "Cancel"}
          </button>
        </div>
      </div>
    </Surface>
  );
}
