import NotFound from "./(v1)/not-found";
import { V1Document, v1Metadata, v1Viewport } from "./(v1)/v1-document";

// 404 for URLs that match no route at all (experimental.globalNotFound).
//
// With two root layouts, (v1) and (ledger), there is no app/layout.tsx
// to compose an app/not-found.tsx with, so Next renders this file as a
// whole document instead. It keeps today's V1 404 for every unmatched
// URL: the same document, providers, metadata and copy as
// (v1)/not-found.tsx, which still handles notFound() inside V1.
// notFound() inside V2 renders (ledger)/not-found.tsx.

export const viewport = v1Viewport;
export const metadata = v1Metadata;

export default function GlobalNotFound() {
  return (
    <V1Document>
      <NotFound />
    </V1Document>
  );
}
