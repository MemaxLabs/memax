import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  transpilePackages: ["@memaxlabs/ui"],
  typescript: { ignoreBuildErrors: true },
  experimental: {
    // Two root layouts ((v1) and (ledger)) leave no app/layout.tsx to
    // build a 404 from, so unmatched URLs render app/global-not-found.tsx.
    globalNotFound: true,
  },
};

export default nextConfig;
