import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { KeysSettings } from "./keys-settings";

export const metadata: Metadata = { title: en.ledger.agents.keys.title };

export default function KeysPage() {
  return <KeysSettings />;
}
