import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { devRoutesEnabled } from "@/lib/dev-routes";

export const metadata: Metadata = {
  robots: { index: false, follow: false },
};

// /dev/ledger/* fixtures. Same gate as the V1 /dev pages: hidden in
// production unless the build sets NEXT_PUBLIC_DEV_FIXTURES=1.
export default function LedgerDevLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  if (!devRoutesEnabled()) {
    notFound();
  }
  return children;
}
