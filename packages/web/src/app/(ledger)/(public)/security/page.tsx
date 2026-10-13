import type { Metadata } from "next";
import { publicMetadata } from "../_site/metadata";
import { SecurityScreen } from "../_site/security";

export const metadata: Metadata = publicMetadata("security", "/security");

// Trust and security (Security.png): the rules, where data lives, who
// else sees a memory's words, what Forget can't reach, and how to check
// it all. Static and open to every browser; its facts are the server's
// defaults (lib/v2/cloud-posture.json).
export default function SecurityPage() {
  return <SecurityScreen />;
}
