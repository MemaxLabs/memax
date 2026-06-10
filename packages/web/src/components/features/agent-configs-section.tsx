"use client";

/**
 * `<AgentConfigsSection>` — Settings → You → Agents wrapper.
 *
 * Plan 24 phase 1: now a thin alias over `<AgentList>` with
 * `expandable=true` + `showConnectNew=true` — Settings keeps its
 * inline disclosure behavior, the `/agents` route uses
 * `<AgentList>` directly with its own props. Single source of
 * truth: data + render flow through one component.
 *
 * `CredentialClassPill` / `getCredentialClassKind` /
 * `CredentialClassKind` are re-exported here so the existing
 * dom test (and any future consumer that imports them through
 * the settings module) keeps resolving.
 */

import { AgentList } from "@/components/features/agent-list/agent-list";

export {
  CredentialClassPill,
  getCredentialClassKind,
  type CredentialClassKind,
} from "@/components/features/agent-card/agent-card";

export function AgentConfigsSection({
  hideKeyManagement,
}: {
  /** When true, per-agent key lists and orphaned keys section are hidden.
   *  Use when API key management lives in a separate dedicated surface. */
  hideKeyManagement?: boolean;
} = {}) {
  return (
    <AgentList
      expandable
      showConnectNew
      hideKeyManagement={hideKeyManagement}
    />
  );
}
