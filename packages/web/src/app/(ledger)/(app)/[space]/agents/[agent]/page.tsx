import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { AgentDetailPlace } from "../../../_places/agents/agent-detail";

export const metadata: Metadata = { title: en.ledger.app.titles.agents };

// /[space]/agents/[agent]: one agent connection, by its id.
export default function AgentPage() {
  return <AgentDetailPlace />;
}
