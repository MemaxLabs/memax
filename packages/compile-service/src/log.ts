/**
 * Structured logs: one JSON object per line, on stdout under Node (which
 * Fly or any container runtime ships on) and through console.log on
 * Workers (which Workers Logs indexes field by field). Logs carry the
 * route, status, sizes and timings, never a request's content or headers:
 * compile inputs hold memory statements, parse-back requests hold whole
 * files, and the Authorization header holds the service token.
 */

export type Level = "debug" | "info" | "warn" | "error";

export type Fields = Record<string, string | number | boolean | null>;

export interface Logger {
  log(level: Level, msg: string, fields?: Fields): void;
}

/** One log line as JSON. */
export function logLine(
  time: Date,
  level: Level,
  msg: string,
  fields: Fields = {},
): string {
  return JSON.stringify({
    time: time.toISOString(),
    level,
    msg,
    service: "compile",
    ...fields,
  });
}

/** Writes JSON lines to a sink (stdout by default). */
export function jsonLogger(
  write: (line: string) => void = (line) => process.stdout.write(line),
  now: () => Date = () => new Date(),
): Logger {
  return {
    log(level, msg, fields = {}) {
      write(`${logLine(now(), level, msg, fields)}\n`);
    },
  };
}

/** Writes JSON lines with console.log (Workers). */
export function consoleLogger(now: () => Date = () => new Date()): Logger {
  return {
    log(level, msg, fields = {}) {
      // eslint-disable-next-line no-console -- Workers Logs reads console output.
      console.log(logLine(now(), level, msg, fields));
    },
  };
}

/** Drops everything (tests). */
export const silentLogger: Logger = { log() {} };
