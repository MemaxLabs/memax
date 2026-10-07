import type { CaptureResult } from "posthog-js";

/**
 * What PostHog may hear about where a person was, in both trees: never a
 * token. Several pages carry one in their URL: the consent request
 * (/oauth/authorize?request=), a sign-in's one-time code
 * (/signin/callback?code=, V1's /auth/callback?code=), the CLI's device
 * code (/device?code=), the morning email's unsubscribe token
 * (/unsubscribe?token=), an invite (/register?invite=, V1's
 * /invite/<token>, V2's /join/<invite>). So every URL is sent without its
 * query string or fragment, and a path segment that is a token is
 * replaced by its name. Runs as posthog-js's before_send (lib/posthog.ts).
 */

/** Path segments that are followed by a secret, and the name it becomes. */
const SECRET_AFTER: Readonly<Record<string, string>> = {
  invite: ":token", // V1 /invite/<token>
  invites: ":token", // the API's /v1/invites/<token>
  join: ":invite", // V2 /join/<invite>
};

const ABSOLUTE_URL = /^[a-z][a-z0-9+.-]*:\/\//i;

function redactPath(path: string): string {
  const segments = path.split("/");
  for (let i = 0; i < segments.length - 1; i++) {
    const name = SECRET_AFTER[segments[i]!];
    if (name && segments[i + 1]) segments[i + 1] = name;
  }
  return segments.join("/");
}

/** A URL or path without its query, fragment or secret segments; anything else as it is. */
export function scrubURL(value: string): string {
  if (ABSOLUTE_URL.test(value)) {
    try {
      const url = new URL(value);
      const origin = url.origin === "null" ? `${url.protocol}//` : url.origin;
      return origin + redactPath(url.pathname);
    } catch {
      return value.replace(/[?#][\s\S]*$/, "");
    }
  }
  if (value.startsWith("/"))
    return redactPath(value.replace(/[?#][\s\S]*$/, ""));
  return value;
}

/** A value worth scrubbing: a URL anywhere, or a path in one of PostHog's own `$` properties. */
function isURLish(key: string, value: unknown): value is string {
  return (
    typeof value === "string" &&
    (ABSOLUTE_URL.test(value) || (key.startsWith("$") && value.startsWith("/")))
  );
}

const HREF_IN_CHAIN = /(attr__href=")([^"]*)(")/g;

function scrubProperties(
  props: Record<string, unknown> | undefined,
): Record<string, unknown> | undefined {
  if (!props) return props;
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(props)) {
    if (isURLish(key, value)) {
      out[key] = scrubURL(value);
    } else if (key === "$elements" && Array.isArray(value)) {
      // Autocapture: a clicked link's href.
      out[key] = value.map((el: unknown) =>
        el && typeof el === "object"
          ? scrubProperties(el as Record<string, unknown>)
          : el,
      );
    } else if (key === "$elements_chain" && typeof value === "string") {
      out[key] = value.replace(
        HREF_IN_CHAIN,
        (_m, a: string, href: string, b: string) => a + scrubURL(href) + b,
      );
    } else if (key === "attr__href" && typeof value === "string") {
      out[key] = scrubURL(value);
    } else {
      out[key] = value;
    }
  }
  return out;
}

/** posthog-js's before_send: the event with every URL scrubbed. */
export function scrubEvent(event: CaptureResult | null): CaptureResult | null {
  if (!event) return event;
  type Props = CaptureResult["properties"];
  const out: CaptureResult = {
    ...event,
    properties: scrubProperties(event.properties) as Props,
  };
  if (event.$set) out.$set = scrubProperties(event.$set) as Props;
  if (event.$set_once)
    out.$set_once = scrubProperties(event.$set_once) as Props;
  return out;
}
