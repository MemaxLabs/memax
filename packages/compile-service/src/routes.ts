/**
 * The routes, as plain functions from a parsed body to a status and a
 * response body, so they are tested without a socket.
 *
 *   POST /compile     CompileInput → CompileResult
 *   POST /parse-back  {last, current} → ChangeSet + {drifted, drift_sha256}
 *   GET  /health      liveness, the contract version and the adapters
 */
import {
  CONTRACT_VERSION,
  CompileInputError,
  DEFAULT_TARGET_KINDS,
  adapters,
  compile,
  driftHash,
  isDrifted,
  parseBack,
  type ChangeSet,
  type CompileResult,
} from "@memaxlabs/compiler";

import { HttpError, toPointer, type Issue } from "./errors.js";

/** Compiles an input, or refuses it with every issue the compiler found. */
export function compileRoute(body: unknown): CompileResult {
  try {
    return compile(body as Parameters<typeof compile>[0]);
  } catch (err) {
    if (err instanceof CompileInputError) {
      throw new HttpError(
        422,
        "invalid_input",
        "The compile input is invalid. Fix every issue and send it again.",
        err.issues.map((i) => ({
          instancePath: toPointer(i.path),
          message: i.message,
        })),
      );
    }
    throw err;
  }
}

export interface ParseBackResponse extends ChangeSet {
  /**
   * Whether the current content differs from the last delivered file: by
   * its drift hash when `last.drift_sha256` is given, otherwise by
   * comparing both contents' drift hashes.
   */
  drifted: boolean;
  /** The current content's drift hash: what to record as observed. */
  drift_sha256: string;
}

const SHA256 = /^[0-9a-f]{64}$/;

/**
 * Reads a hand edit back. `last` is the last compiled (or accepted) file:
 * its content as a string, or `{content, drift_sha256?}`.
 */
export function parseBackRoute(body: unknown): ParseBackResponse {
  const issues: Issue[] = [];
  const req = isObject(body) ? body : {};
  if (!isObject(body))
    issues.push({ instancePath: "", message: "must be an object" });

  let lastContent = "";
  let lastSha: string | null = null;
  if (typeof req.last === "string") {
    lastContent = req.last;
  } else if (isObject(req.last)) {
    if (typeof req.last.content === "string") lastContent = req.last.content;
    else
      issues.push({
        instancePath: "/last/content",
        message: "must be a string",
      });
    if (req.last.drift_sha256 !== undefined) {
      if (
        typeof req.last.drift_sha256 === "string" &&
        SHA256.test(req.last.drift_sha256)
      ) {
        lastSha = req.last.drift_sha256;
      } else {
        issues.push({
          instancePath: "/last/drift_sha256",
          message: "must be a lowercase hex sha256",
        });
      }
    }
  } else if (isObject(body)) {
    issues.push({
      instancePath: "/last",
      message: "must be a string or an object with content",
    });
  }
  if (isObject(body) && typeof req.current !== "string") {
    issues.push({ instancePath: "/current", message: "must be a string" });
  }
  if (issues.length > 0) {
    throw new HttpError(
      422,
      "invalid_input",
      "The parse-back request is invalid.",
      issues,
    );
  }

  const current = req.current as string;
  const changes = parseBack(lastContent, current);
  const drifted =
    lastSha !== null
      ? isDrifted(lastSha, current)
      : driftHash(lastContent) !== driftHash(current);
  return { ...changes, drifted, drift_sha256: driftHash(current) };
}

export interface HealthResponse {
  status: "ok" | "draining";
  contract_version: number;
  adapters: {
    kind: string;
    version: number;
    role: string;
    default_path: string | null;
    default: boolean;
  }[];
  uptime_seconds: number;
}

/** What the service compiles with. */
export function healthRoute(
  draining: boolean,
  startedAt: number,
  now: number,
): HealthResponse {
  return {
    status: draining ? "draining" : "ok",
    contract_version: CONTRACT_VERSION,
    adapters: adapters.map((a) => ({
      kind: a.kind,
      version: a.version,
      role: a.role,
      default_path: a.defaultPath,
      default: DEFAULT_TARGET_KINDS.includes(a.kind),
    })),
    uptime_seconds: Math.floor((now - startedAt) / 1000),
  };
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
