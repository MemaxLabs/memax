/**
 * Error responses, in the style of a JSON Schema validator's output: a
 * stable code, a sentence for people, and for a refused body every issue
 * found, each with the JSON Pointer (RFC 6901) of the offending value.
 *
 *   {"error": {"code": "invalid_input", "message": "…",
 *              "issues": [{"instancePath": "/memories/3/state", "message": "…"}]}}
 */

export type ErrorCode =
  | "invalid_json"
  | "invalid_input"
  | "unsupported_media_type"
  | "unauthorized"
  | "too_large"
  | "not_found"
  | "method_not_allowed"
  | "unavailable"
  | "internal_error";

export interface Issue {
  /** JSON Pointer into the request body; "" is the whole body. */
  instancePath: string;
  message: string;
}

export interface ErrorBody {
  error: { code: ErrorCode; message: string; issues?: Issue[] };
}

/** A failed request: an HTTP status and the body to send. */
export class HttpError extends Error {
  readonly status: number;
  readonly code: ErrorCode;
  readonly issues?: Issue[];

  constructor(
    status: number,
    code: ErrorCode,
    message: string,
    issues?: Issue[],
  ) {
    super(message);
    this.name = "HttpError";
    this.status = status;
    this.code = code;
    this.issues = issues;
  }

  body(): ErrorBody {
    return {
      error: {
        code: this.code,
        message: this.message,
        ...(this.issues ? { issues: this.issues } : {}),
      },
    };
  }
}

/**
 * Turns the compiler's issue paths (`memories[3].state`, `brief.sections[0].items[2].ref`)
 * into JSON Pointers (`/memories/3/state`). `input` is the whole body.
 */
export function toPointer(path: string): string {
  if (path === "" || path === "input") return "";
  const tokens: string[] = [];
  for (const part of path.split(".")) {
    const m = /^([^[\]]*)((?:\[\d+\])*)$/.exec(part);
    if (!m) {
      tokens.push(part);
      continue;
    }
    if (m[1] !== "") tokens.push(m[1]);
    for (const idx of m[2].matchAll(/\[(\d+)\]/g)) tokens.push(idx[1]);
  }
  return tokens
    .map((t) => `/${t.replace(/~/g, "~0").replace(/\//g, "~1")}`)
    .join("");
}
