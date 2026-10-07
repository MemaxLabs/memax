// A small, fast string hash for the session-start hook: whether a line
// changed between two sessions, and short file names. Not a security
// boundary (a collision only leaves a changed line unmentioned, and the
// agent loads the file anyway), and it keeps node:crypto, about 5 ms to
// load, off the hook's critical path.

/** cyrb53 (public domain), both 32-bit halves: 16 hex characters. */
export function shortHash(s: string): string {
  let h1 = 0xdeadbeef;
  let h2 = 0x41c6ce57;
  for (let i = 0; i < s.length; i++) {
    const ch = s.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
  h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
  h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  return (
    (h2 >>> 0).toString(16).padStart(8, "0") +
    (h1 >>> 0).toString(16).padStart(8, "0")
  );
}

/** A file name no other write, in this process or another, will pick. */
export function uniqueName(): string {
  return `${Date.now()}-${process.pid}-${Math.random().toString(16).slice(2, 10)}`;
}
