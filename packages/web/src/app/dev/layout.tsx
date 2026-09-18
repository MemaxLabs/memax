import type { Metadata } from "next";
import { notFound } from "next/navigation";

export const metadata: Metadata = {
  robots: { index: false, follow: false },
};

export default function DevLayout({ children }: { children: React.ReactNode }) {
  // Production hides /dev/* — except when a build opts in (screenshot
  // runs of the fixture pages need a hydrated production bundle).
  if (
    process.env.NODE_ENV === "production" &&
    process.env.NEXT_PUBLIC_DEV_FIXTURES !== "1"
  ) {
    notFound();
  }
  return children;
}
