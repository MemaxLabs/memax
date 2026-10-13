import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { LANDING_AT_ROOT } from "@/lib/ui-gate";
import { APP_URL } from "@/lib/urls";

export type PublicPage = "landing" | "pricing" | "security" | "docs";

/**
 * The social card every public page shares: a static PNG drawn from the
 * Ledger tokens and fonts (scripts/og-image.mjs), never a handoff board.
 */
export const OG_IMAGE = {
  url: "/images/og-memax.png",
  width: 1200,
  height: 630,
} as const;

/**
 * Title, description, canonical address and the Open Graph and Twitter
 * cards for a public page, in English (what the server renders and
 * crawlers read; the page follows the device's language once it loads).
 *
 * Search engines index the public site only once the Landing serves "/"
 * (LANDING_AT_ROOT): until a founder makes that switch, the pages answer
 * but ask not to be indexed, so nothing unreviewed is published.
 */
export function publicMetadata(page: PublicPage, path: string): Metadata {
  const meta = en.ledger.public.meta;
  const { title, description } = meta[page];
  // The Landing's title stands alone; the others take the layout's
  // "%s · Memax".
  const full = page === "landing" ? title : `${title} · Memax`;
  return {
    metadataBase: new URL(APP_URL),
    title: page === "landing" ? { absolute: title } : title,
    description,
    alternates: { canonical: path },
    robots: LANDING_AT_ROOT
      ? { index: true, follow: true }
      : { index: false, follow: true },
    openGraph: {
      type: "website",
      siteName: "Memax",
      locale: "en_US",
      url: path,
      title: full,
      description,
      images: [{ ...OG_IMAGE, alt: meta.imageAlt }],
    },
    twitter: {
      card: "summary_large_image",
      title: full,
      description,
      images: [{ url: OG_IMAGE.url, alt: meta.imageAlt }],
    },
  };
}
