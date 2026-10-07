import { createMDX } from "fumadocs-mdx/next";

const withMDX = createMDX();

// A fully static site: `next build` writes every page to out/, which
// Cloudflare serves as Workers static assets (wrangler.jsonc) and any
// static host can serve too. Nothing renders per request, so there is no
// server to run.
//
// Static export has no next.config redirects: /benchmarks → its new home
// is public/_redirects on Cloudflare (a 301), and src/app/benchmarks
// everywhere else.
export default withMDX({
  output: "export",
  reactStrictMode: true,
});
