// The agent's side of a hook process: its JSON event on stdin, and what
// the hook prints on stdout. Shared by the local session-start hook
// (hook-main.ts) and the cloud start (cloud-start-main.ts). Node built-ins
// only: this is on the local hook's critical path.
import { fstatSync, read, writeSync } from "node:fs";

/**
 * How long to wait for the agent's stdin event before going without it.
 * MEMAX_HOOK_STDIN_WAIT_MS raises it for tests on a loaded machine, where
 * the timer can fire before the read callback brings an event already
 * written (CI, Oct 8); agents get the 50 ms the budget allows.
 */
const STDIN_WAIT_MS = Number(process.env.MEMAX_HOOK_STDIN_WAIT_MS) || 50;

/**
 * The agent's JSON event on stdin. Agents write it and close the pipe; a
 * complete object ends the wait at once, and a pipe nobody writes to
 * costs at most STDIN_WAIT_MS.
 */
export function readEvent(
  ms = STDIN_WAIT_MS,
): Promise<Record<string, unknown>> {
  // Agents pass a pipe (or a socket pair); a terminal or /dev/null has no
  // event, and reading a terminal would take a person's keystrokes.
  try {
    const st = fstatSync(0);
    if (!st.isFIFO() && !st.isSocket() && !st.isFile())
      return Promise.resolve({});
  } catch {
    return Promise.resolve({});
  }
  return new Promise((resolve) => {
    const chunks: Buffer[] = [];
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      resolve(parseEvent(Buffer.concat(chunks).toString("utf8")) ?? {});
    };
    const timer = setTimeout(finish, ms);
    const buf = Buffer.alloc(64 * 1024);
    const step = () =>
      read(0, buf, 0, buf.length, null, (err, n) => {
        if (done) return;
        if (err && (err as NodeJS.ErrnoException).code === "EAGAIN") {
          setTimeout(step, 2);
          return;
        }
        if (err || n === 0) return finish();
        chunks.push(Buffer.from(buf.subarray(0, n)));
        if (parseEvent(Buffer.concat(chunks).toString("utf8"))) return finish();
        step();
      });
    step();
  });
}

/** One JSON object, or null. */
export function parseEvent(text: string): Record<string, unknown> | null {
  const t = text.trim();
  if (!t.startsWith("{")) return null;
  try {
    const v: unknown = JSON.parse(t);
    return v && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

/** Writes all of text to stdout, synchronously ("" writes nothing). */
export function print(text: string): void {
  if (text === "") return;
  const buf = Buffer.from(text, "utf8");
  let off = 0;
  while (off < buf.length) {
    try {
      off += writeSync(1, buf, off);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EAGAIN") throw err;
    }
  }
}
