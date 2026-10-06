import type { NextConfig } from "next";

// `next build` type-checks the app: tsc --noEmit is clean, so the old
// typescript.ignoreBuildErrors escape hatch is gone (plan §6.1).
const nextConfig: NextConfig = {
  // Source-first workspace packages with no build step: @memaxlabs/ui
  // (V1) and @memaxlabs/ledger (V2, TSX and CSS straight from src/).
  transpilePackages: ["@memaxlabs/ui", "@memaxlabs/ledger"],
  experimental: {
    // Two root layouts ((v1) and (ledger)) leave no app/layout.tsx to
    // build a 404 from, so unmatched URLs render app/global-not-found.tsx.
    globalNotFound: true,
  },
};

export default nextConfig;
