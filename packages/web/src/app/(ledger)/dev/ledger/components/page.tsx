import type { Metadata } from "next";
// Load order from @memaxlabs/ledger's README: the tokens and ledger.css
// come from the (ledger) root layout, then the preview helpers.
import "@memaxlabs/ledger/previews.css";
import { en } from "@/i18n/locales/en";
import { ComponentGallery } from "./gallery";

// /dev/ledger/components: every @memaxlabs/ledger preview at its
// artboard size, in Paper and Carbon. The Playwright tests screenshot
// each one and compare it with the handoff's design-system/previews/
// PNGs (e2e/ledger-components.e2e.ts).

export const metadata: Metadata = { title: en.ledger.app.titles.components };

export default function LedgerComponentsPage() {
  return <ComponentGallery />;
}
