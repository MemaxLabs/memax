import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { MemoryPlace } from "../../../_places/memory";

export const metadata: Metadata = { title: en.ledger.app.titles.memories };

// /[space]/memories/[ref] (Memory.png): one memory under its seal, or
// States2's "Not found" when there's none the person can open.
export default async function MemoryPage({
  params,
}: {
  params: Promise<{ ref: string }>;
}) {
  const { ref } = await params;
  return <MemoryPlace memoryRef={decodeURIComponent(ref)} />;
}
