import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { devRoutesEnabled } from "@/lib/dev-routes";

export const metadata: Metadata = {
  robots: { index: false, follow: false },
};

export default function DevLayout({ children }: { children: React.ReactNode }) {
  // Production hides /dev/* — except when a build opts in (screenshot
  // runs of the fixture pages need a hydrated production bundle).
  if (!devRoutesEnabled()) {
    notFound();
  }
  return children;
}
