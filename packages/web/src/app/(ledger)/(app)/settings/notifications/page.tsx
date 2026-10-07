import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { NotificationsSettings } from "./notifications-settings";

export const metadata: Metadata = {
  title: en.ledger.app.titles.notifications,
};

export default function NotificationsPage() {
  return <NotificationsSettings />;
}
