import { permanentRedirect } from "next/navigation";

// The old /benchmarks URL. A static export has no next.config redirects,
// so this page sends readers on (Cloudflare answers it with a 301 from
// public/_redirects before it gets here).
export default function Benchmarks() {
  permanentRedirect("/quickstart/benchmarks");
}
