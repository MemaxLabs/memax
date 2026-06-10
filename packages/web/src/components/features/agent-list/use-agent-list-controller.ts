"use client";

/**
 * `useAgentListController` — single source of truth for the agent
 * list's data plumbing and grouping. Plan 24 §4.2 Appendix A.
 *
 * Both Settings (`<AgentConfigsSection>` via `<AgentList expandable
 * showConnectNew />`) and the `/agents` route (`<AgentList />`) read
 * from this hook so they stay in lockstep — same loading semantics,
 * same error retry, same hub→name resolution, same per-agent config
 * + key grouping. The list components become pure presentation.
 *
 * Returned shape mirrors what `<AgentList>` consumes directly:
 *   - `agents`, `isLoading`, `hasPrimaryError` for top-level state
 *   - `configsByAgent` + `keysByAgent` Maps so `<AgentCard>` can
 *     receive a flat slice without re-grouping per render
 *   - `extractionCounts`, `hubNames` lookup tables
 *   - mutation hooks already paired (`onForgetConfigs`,
 *     `onRevokeKey`, `onUpdateAgent`, `onDisconnect`) + their
 *     feedback objects
 *   - `refetchAll` for the section's error retry
 *
 * Pure hook — no JSX. Consumers wire each agent slice into
 * `<AgentCard>`.
 */

import { useMemo } from "react";
import type { AgentConfig, ApiKeyListItem } from "memax-sdk";
import { useAuth } from "@/lib/auth";
import { useAgentConfigs } from "@/hooks/use-agent-configs";
import { useAgentConfigForget } from "@/hooks/use-agent-config-forget";
import {
  useConnectedAgents,
  useUpdateAgent,
  useDisconnectAgent,
} from "@/hooks/use-connected-agents";
import { useApiKeys, useRevokeApiKey } from "@/hooks/use-api-keys";

export interface AgentListControllerValue {
  agents: ReturnType<typeof useConnectedAgents>["data"];
  isLoading: boolean;
  hasPrimaryError: boolean;
  configsByAgent: Map<string, AgentConfig[]>;
  keysByAgent: Map<string, ApiKeyListItem[]>;
  extractionCounts: Record<string, number>;
  hubNames: Map<string, string>;

  // Mutation handlers + feedback (paired)
  onForgetConfigs: (ids: string[]) => Promise<boolean>;
  forgetFeedback: ReturnType<typeof useAgentConfigForget>["feedback"];
  clearForgetFeedback: () => void;

  onRevokeKey: (id: string) => Promise<boolean>;
  revokeFeedbackRaw: ReturnType<typeof useRevokeApiKey>["feedback"];
  clearRevokeFeedback: () => void;

  onUpdateAgent: (slug: string, displayName?: string, icon?: string) => void;

  onDisconnect: (slug: string) => Promise<boolean>;
  disconnectFeedback: ReturnType<typeof useDisconnectAgent>["feedback"];
  clearDisconnectFeedback: () => void;

  refetchAll: () => void;
}

export function useAgentListController(): AgentListControllerValue {
  const { hubs } = useAuth();
  const {
    data: agents,
    isLoading: agentsLoading,
    isError: agentsError,
    refetch: refetchAgents,
  } = useConnectedAgents();
  const {
    data: configData,
    isLoading: configsLoading,
    isError: configsError,
    refetch: refetchConfigs,
  } = useAgentConfigs();
  const { data: allKeys, refetch: refetchApiKeys } = useApiKeys();
  const configForget = useAgentConfigForget();
  const revokeKey = useRevokeApiKey();
  const updateAgent = useUpdateAgent();
  const disconnectAgent = useDisconnectAgent();

  const hubNames = useMemo(() => {
    const map = new Map<string, string>();
    for (const { hub } of hubs) map.set(hub.id, hub.name);
    return map;
  }, [hubs]);

  // useMemo wraps the `?? []` fallback so the dep is stable across
  // renders when `configData?.configs` doesn't change identity.
  // Bare `?? []` produces a new array literal each render, which
  // would invalidate the configsByAgent memo every time.
  const configs = useMemo<AgentConfig[]>(
    () => configData?.configs ?? [],
    [configData?.configs],
  );
  const extractionCounts = configData?.extraction_counts ?? {};

  const configsByAgent = useMemo(() => {
    const map = new Map<string, AgentConfig[]>();
    for (const c of configs) {
      const group = map.get(c.agent) ?? [];
      group.push(c);
      map.set(c.agent, group);
    }
    return map;
  }, [configs]);

  // Unassigned keys live on the You > API Keys surface — they don't
  // belong under any agent card.
  const keysByAgent = useMemo(() => {
    const map = new Map<string, ApiKeyListItem[]>();
    for (const k of allKeys ?? []) {
      if (!k.agent_name) continue;
      const group = map.get(k.agent_name) ?? [];
      group.push(k);
      map.set(k.agent_name, group);
    }
    return map;
  }, [allKeys]);

  return {
    agents,
    isLoading: agentsLoading || configsLoading,
    hasPrimaryError: Boolean(agentsError || configsError),
    configsByAgent,
    keysByAgent,
    extractionCounts,
    hubNames,
    onForgetConfigs: (ids) => configForget.forgetConfigs(ids),
    forgetFeedback: configForget.feedback,
    clearForgetFeedback: configForget.clearFeedback,
    onRevokeKey: (id) => revokeKey.revokeKey(id),
    revokeFeedbackRaw: revokeKey.feedback,
    clearRevokeFeedback: revokeKey.clearFeedback,
    onUpdateAgent: (slug, displayName, icon) =>
      updateAgent.mutate({ slug, displayName, icon }),
    onDisconnect: (slug) => disconnectAgent.disconnectAgent(slug),
    disconnectFeedback: disconnectAgent.feedback,
    clearDisconnectFeedback: disconnectAgent.clearFeedback,
    refetchAll: () => {
      void refetchAgents();
      void refetchConfigs();
      void refetchApiKeys();
    },
  };
}
