import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { AgentsPlace } from "../../_places/agents/agents-place";

export const metadata: Metadata = { title: en.ledger.app.titles.agents };

export default function AgentsPage() {
  return <AgentsPlace />;
}
