import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // ui.memaxlabs.com serves the kitchen as static files from Cloudflare
  // (wrangler.jsonc): next build writes them to out/.
  output: "export",
  typescript: { ignoreBuildErrors: true },
};

export default nextConfig;
