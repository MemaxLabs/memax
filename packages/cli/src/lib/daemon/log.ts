// The daemon's log: one line per event, rotated at 1 MiB, three files
// kept. It records what happened to which file (paths, run refs, hash
// prefixes), never a file's content or a credential.
import {
  appendFileSync,
  existsSync,
  renameSync,
  statSync,
  unlinkSync,
} from "node:fs";

export type LogFields = Record<string, string | number | boolean | undefined>;

export interface Logger {
  info(msg: string, fields?: LogFields): void;
  warn(msg: string, fields?: LogFields): void;
  error(msg: string, fields?: LogFields): void;
}

/** Field names that may carry memory text or file content. */
const CONTENT_KEYS = new Set([
  "content",
  "body",
  "text",
  "statement",
  "observed",
  "compiled",
]);
const MAX_VALUE = 240;

/** What may go in a log line: no content fields, no tokens, short values. */
export function redact(value: string): string {
  let v = value
    .replace(/Bearer\s+[^\s"']+/gi, "Bearer [redacted]")
    .replace(/\bmxk_[A-Za-z0-9_-]+/g, "mxk_[redacted]")
    .replace(
      /\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g,
      "[jwt redacted]",
    )
    .replace(/[\r\n\t]+/g, " ");
  if (v.length > MAX_VALUE) v = v.slice(0, MAX_VALUE) + "…";
  return v;
}

export function formatLine(
  level: string,
  msg: string,
  fields: LogFields = {},
  now = new Date(),
): string {
  const parts = [now.toISOString(), level.toUpperCase(), redact(msg)];
  for (const [k, v] of Object.entries(fields)) {
    if (v === undefined) continue;
    if (CONTENT_KEYS.has(k.toLowerCase())) {
      parts.push(`${k}=[omitted]`);
      continue;
    }
    const s = redact(String(v));
    parts.push(`${k}=${/[\s"=]/.test(s) || s === "" ? JSON.stringify(s) : s}`);
  }
  return parts.join(" ") + "\n";
}

export interface FileLoggerOptions {
  maxBytes?: number;
  /** Files kept, the live one included. */
  keep?: number;
  /** Also write each line here (a TTY in the foreground). */
  mirror?: NodeJS.WritableStream;
}

export function fileLogger(path: string, opts: FileLoggerOptions = {}): Logger {
  const maxBytes = opts.maxBytes ?? 1024 * 1024;
  const keep = Math.max(1, opts.keep ?? 3);
  let size = 0;
  try {
    size = statSync(path).size;
  } catch {
    size = 0;
  }
  const rotate = () => {
    for (let i = keep - 1; i >= 1; i--) {
      const from = i === 1 ? path : `${path}.${i - 1}`;
      const to = `${path}.${i}`;
      if (!existsSync(from)) continue;
      try {
        if (i === keep - 1 && existsSync(to)) unlinkSync(to);
        renameSync(from, to);
      } catch {
        // A failed rotation only means a longer file.
      }
    }
    size = 0;
  };
  const write = (level: string, msg: string, fields?: LogFields) => {
    const line = formatLine(level, msg, fields);
    if (size + line.length > maxBytes && size > 0) rotate();
    try {
      appendFileSync(path, line, { mode: 0o600 });
      size += Buffer.byteLength(line);
    } catch {
      // Logging must never stop the daemon.
    }
    opts.mirror?.write(line);
  };
  return {
    info: (m, f) => write("info", m, f),
    warn: (m, f) => write("warn", m, f),
    error: (m, f) => write("error", m, f),
  };
}

/** A logger that keeps lines in memory (tests). */
export function memoryLogger(): Logger & { lines: string[] } {
  const lines: string[] = [];
  const push = (level: string) => (m: string, f?: LogFields) => {
    lines.push(formatLine(level, m, f));
  };
  return {
    lines,
    info: push("info"),
    warn: push("warn"),
    error: push("error"),
  };
}
