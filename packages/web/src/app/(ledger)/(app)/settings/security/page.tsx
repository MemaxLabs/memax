import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { SecuritySettings } from "./security-settings";

export const metadata: Metadata = { title: en.ledger.app.titles.security };

export default function SecurityPage() {
  return <SecuritySettings />;
}
