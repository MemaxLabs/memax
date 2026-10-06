/**
 * Structured logs: one JSON object per line on stdout, which Fly ships to
 * Loki. Logs carry the route, status, sizes and timings, never a request's
 * content: compile inputs hold memory statements, and parse-back requests
 * hold whole files.
 */

export type Level = "debug" | "info" | "warn" | "error";

export type Fields = Record<string, string | number | boolean | null>;

export interface Logger {
  log(level: Level, msg: string, fields?: Fields): void;
}

/** Writes JSON lines to a sink (stdout by default). */
export function jsonLogger(
  write: (line: string) => void = (line) => process.stdout.write(line),
  now: () => Date = () => new Date(),
): Logger {
  return {
    log(level, msg, fields = {}) {
      write(
        `${JSON.stringify({ time: now().toISOString(), level, msg, service: "compile", ...fields })}\n`,
      );
    },
  };
}

/** Drops everything (tests). */
export const silentLogger: Logger = { log() {} };
