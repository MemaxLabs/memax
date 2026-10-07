/**
 * The Cloudflare Workers entry: the same handler as the Node server
 * (handler.ts), so the two answer every request identically. See
 * wrangler.jsonc.
 *
 *   COMPILE_SERVICE_TOKEN  secret; every route but GET /health needs it as
 *                          `Authorization: Bearer <token>`. A Worker has a
 *                          public URL, so without it every other route
 *                          answers 503 rather than serving anyone.
 *   MAX_BODY_BYTES         largest accepted body (default 4 MiB)
 *
 * There is no drain: the runtime finishes in-flight requests on a deploy.
 */
import { createHandler, DEFAULT_MAX_BODY, type Handler } from "./handler.js";
import { consoleLogger } from "./log.js";

export interface Env {
  COMPILE_SERVICE_TOKEN?: string;
  MAX_BODY_BYTES?: string;
}

const log = consoleLogger();

// One handler per isolate, rebuilt only if the settings change (a secret
// rotation reaches new isolates with the new deployment anyway).
let current: { key: string; handler: Handler } | null = null;

function handlerFor(env: Env): Handler {
  const token = env.COMPILE_SERVICE_TOKEN ?? "";
  const maxBody = Number(env.MAX_BODY_BYTES ?? "");
  const key = `${token}\u0000${env.MAX_BODY_BYTES ?? ""}`;
  if (current?.key !== key) {
    current = {
      key,
      handler: createHandler({
        token,
        requireToken: true,
        maxBodyBytes:
          Number.isInteger(maxBody) && maxBody > 0 ? maxBody : DEFAULT_MAX_BODY,
        log,
      }),
    };
  }
  return current.handler;
}

export default {
  fetch(request: Request, env: Env): Promise<Response> {
    return handlerFor(env)(request);
  },
};
