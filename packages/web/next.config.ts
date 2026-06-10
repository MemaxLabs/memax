import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  transpilePackages: ["@memaxlabs/ui"],
  typescript: { ignoreBuildErrors: true },
};

export default nextConfig;
