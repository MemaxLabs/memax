import type { Metadata } from "next";
import { LANDING_HREF } from "@/lib/ui-gate";
import { LandingScreen } from "../_site/landing";
import { publicMetadata } from "../_site/metadata";

export const metadata: Metadata = publicMetadata("landing", LANDING_HREF);

// Landing (Landing.png), the public site's front page: static, open to
// every browser. It lives at /welcome while V1's landing keeps "/";
// lib/ui-gate.ts LANDING_AT_ROOT moves it to "/" for signed-out visitors.
export default function WelcomePage() {
  return <LandingScreen />;
}
