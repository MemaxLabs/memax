import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { SearchPlace } from "../../_places/search";

export const metadata: Metadata = { title: en.ledger.app.titles.search };

export default function SearchPage() {
  return <SearchPlace />;
}
