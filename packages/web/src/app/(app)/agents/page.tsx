"use client";

/**
 * /agents — plan 28 §B grid mount.
 *
 * Replaces the original plan-24 `<AgentList>` mount with `<AgentGrid>`,
 * a memory-card-style responsive grid (1/2/3 cols across breakpoints)
 * with a `<ConnectAgentTile>` "+" cell as the first item and an
 * `<AgentGridCard>` per connected agent. Click drills into
 * `/agents/<slug>` (existing detail route, unchanged).
 *
 * Codex review (gpt-5.5) flagged that the previous mount stacked
 * `<PageHeader title="Agents">` immediately above `<DataSectionCard
 * icon=Bot label="Agents">` — duplicate icon + title — and hid the
 * connect-an-agent affordance behind a Settings-only disclosure. Both
 * problems disappear here: the grid has no nested chrome, and the
 * "+" tile is the first cell.
 *
 * Settings (`<AgentConfigsSection>`) continues to use `<AgentList>`
 * with disclosure rows — both surfaces share data plumbing via
 * `useAgentListController` so they stay in lockstep on auth, retry,
 * and grouping semantics.
 */

import { useLocale } from "@/i18n";
import { AgentGrid } from "@/components/features/agent-list/agent-grid";
import { PersonaShelf } from "@/components/features/personas/persona-shelf";
import { PageShell, PageHeader } from "@/components/layout";

export default function AgentsPage() {
  const { t } = useLocale();
  return (
    <PageShell>
      <PageHeader
        title={t.agentConfigs.title}
        subtitle={t.agentConfigs.subtitle}
      />
      <PersonaShelf />
      <AgentGrid />
    </PageShell>
  );
}
