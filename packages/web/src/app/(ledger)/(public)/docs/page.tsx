import type { Metadata } from "next";
import { DocsScreen } from "../_site/docs";
import { publicMetadata } from "../_site/metadata";

export const metadata: Metadata = publicMetadata("docs", "/docs");

// Docs · quickstart (Docs.png): four steps and the MCP tools, with the
// docs site's pages (docs.memax.app) in the tree. Static and open to
// every browser.
export default function DocsPage() {
  return <DocsScreen />;
}
