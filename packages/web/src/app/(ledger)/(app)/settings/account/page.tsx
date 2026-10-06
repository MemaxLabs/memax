import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { AccountSettings } from "./account-settings";

export const metadata: Metadata = { title: en.ledger.app.titles.account };

export default function AccountPage() {
  return <AccountSettings />;
}
