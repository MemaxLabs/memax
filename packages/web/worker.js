// The web app's Worker: OpenNext's handler, after one redirect of our own.
//
// www.<host> answers a permanent redirect to <host>, path and query kept,
// as Vercel did. The app has one origin: its session cookies are __Host-
// (one host each), passkeys are bound to APP_BASE_URL's host, and the API
// decides which sign-ins are the web's by that same host.
//
// .open-next/worker.js is what `opennextjs-cloudflare build` writes; this
// file is plain JavaScript so linting and type checks never need a build.
import openNext from "./.open-next/worker.js";

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (url.hostname.startsWith("www.")) {
      url.hostname = url.hostname.slice("www.".length);
      return Response.redirect(url.toString(), 308);
    }
    return openNext.fetch(request, env, ctx);
  },
};
