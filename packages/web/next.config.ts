import type { NextConfig } from "next";

// `next build` type-checks the app: tsc --noEmit is clean, so the old
// typescript.ignoreBuildErrors escape hatch is gone (plan §6.1).
const nextConfig: NextConfig = {
  transpilePackages: ["@memaxlabs/ui"],
  experimental: {
    // Two root layouts ((v1) and (ledger)) leave no app/layout.tsx to
    // build a 404 from, so unmatched URLs render app/global-not-found.tsx.
    globalNotFound: true,
  },
};

export default nextConfig;
