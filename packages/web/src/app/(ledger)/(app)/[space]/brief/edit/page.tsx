import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { BriefEditPlace } from "../../../_places/brief-edit";

export const metadata: Metadata = { title: en.ledger.app.titles.brief };

// /[space]/brief/edit (BriefEdit.png): reorder, regroup, edit and cite
// the Brief's facts; Done keeps it all as a new version.
export default function BriefEditPage() {
  return <BriefEditPlace />;
}
