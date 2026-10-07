// OpenNext's build of the app for Cloudflare Workers (wrangler.jsonc).
//
// The app has no ISR, no revalidate and no "use cache", so its only cached
// pages are the ones `next build` prerenders. Those are served from the
// Worker's static assets: no R2 bucket, KV namespace, queue or tag cache
// to pay for or provision. If the app ever revalidates, move to the R2
// incremental cache (and a queue for time-based revalidation).
import { defineCloudflareConfig } from "@opennextjs/cloudflare";
import staticAssetsIncrementalCache from "@opennextjs/cloudflare/overrides/incremental-cache/static-assets-incremental-cache";

export default defineCloudflareConfig({
  incrementalCache: staticAssetsIncrementalCache,
  // Answer prerendered pages from the cache before loading Next's server.
  enableCacheInterception: true,
});
