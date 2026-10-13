import type { Metadata } from "next";
import { publicMetadata } from "../_site/metadata";
import { PricingScreen } from "../_site/pricing";

export const metadata: Metadata = publicMetadata("pricing", "/pricing");

// Pricing (Pricing.png): the plans from lib/v2/plans.ts (D9). Static and
// open to every browser; nothing here sells anything while billing is
// closed.
export default function PricingPage() {
  return <PricingScreen />;
}
