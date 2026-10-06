import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { MemoriesPlace } from "../../_places/memories";

export const metadata: Metadata = { title: en.ledger.app.titles.memories };

export default function MemoriesPage() {
  return <MemoriesPlace />;
}
