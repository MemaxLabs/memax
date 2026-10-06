"use client";

/**
 * /agents/connect — full-page connect-an-agent surface (plan 28 §B-2).
 *
 * Destination of `<ConnectAgentTile>`'s "+" tile from the `/agents`
 * grid. `<PageHeader>` provides the page title; the install
 * instructions render inside a single `<Surface>` directly below
 * (no nested Settings-style section header — that's the chrome
 * consolidation the redesign is about).
 *
 * Why a route, not a modal: codex review (gpt-5.5) flagged that the
 * design system says "Routes, not state" and "Never overlay when the
 * surface can transform". `<ConnectAgentsSection>` is itself a full
 * section with heading, margin, and Surface — dropping it into a
 * centered modal would feel nested and heavy. A route also gives the
 * URL shareability (founder can drop `/agents/connect` in onboarding
 * docs) and back-button semantics.
 *
 * The single Surface around `<ConnectAgentsBody>` matches the
 * Settings consumer's outer container exactly — same material, same
 * radius — so the connect-flow visual treatment doesn't drift between
 * the two entry points.
 */

import { useLocale } from "@/i18n";
import { PageShell, PageHeader } from "@/components/layout";
import { Surface } from "@memaxlabs/ui";
import { ConnectAgentsBody } from "@/components/features/connect-agents-section";

export default function AgentsConnectPage() {
  const { t } = useLocale();
  return (
    <PageShell>
      <PageHeader
        title={t.agentConfigs.connectPageTitle}
        subtitle={t.agentConfigs.connectPageSubtitle}
      />
      <Surface variant="subtle" rounded="2xl" className="px-5 py-5">
        <ConnectAgentsBody />
      </Surface>
    </PageShell>
  );
}
