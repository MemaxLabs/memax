import { V1Document, v1Metadata, v1Viewport } from "./v1-document";
import { v1FontVariables } from "./v1-fonts";

// Root layout of the frozen V1 app. Every V1 route group lives under
// (v1) so the URLs are unchanged; the V2 Ledger UI has its own root
// layout in app/(ledger). Two root layouts mean neither tree loads the
// other's CSS, and navigating between them is a full page load.

export const viewport = v1Viewport;
export const metadata = v1Metadata;

export default function V1RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <V1Document fontVariables={v1FontVariables}>{children}</V1Document>;
}
