// A condition's glob (path_exists with `glob`, the scope glob syntax the
// server validates: letters, digits, . _ - / and the wildcards * ** ? [..]
// with ! negating a class) against a repository's paths.
//
//   *     any characters within one path segment
//   **    any number of whole segments (as a segment of its own)
//   ?     one character within a segment
//   [..]  one character of a class; [!..] one not in it

/**
 * The literal directory a glob's matches all sit under ("packages/web" for
 * `packages/web/**`), or "" when its first segment already has a wildcard:
 * what `git ls-tree` lists, so a glob never reads the whole tree when it
 * needn't.
 */
export function globPrefix(glob: string): string {
  const segments = glob.split("/");
  const literal: string[] = [];
  for (const s of segments.slice(0, -1)) {
    if (/[*?[]/.test(s)) break;
    literal.push(s);
  }
  return literal.join("/");
}

/** A matcher for one glob, against paths relative to the repository's root. */
export function globMatcher(glob: string): (path: string) => boolean {
  const re = new RegExp(`^${globSource(glob)}$`);
  return (path) => re.test(path);
}

function globSource(glob: string): string {
  const segments = glob.split("/");
  let out = "";
  segments.forEach((seg, i) => {
    const last = i === segments.length - 1;
    if (seg === "**") {
      // Any number of whole segments, this one included.
      out += last ? ".*" : "(?:[^/]+/)*";
      return;
    }
    out += segmentSource(seg) + (last ? "" : "/");
  });
  return out;
}

function segmentSource(seg: string): string {
  let out = "";
  for (let i = 0; i < seg.length; i++) {
    const c = seg[i];
    if (c === "*") {
      out += "[^/]*";
    } else if (c === "?") {
      out += "[^/]";
    } else if (c === "[") {
      const end = seg.indexOf("]", i + 2);
      if (end < 0) {
        out += "\\[";
        continue;
      }
      let body = seg.slice(i + 1, end);
      const negate = body.startsWith("!");
      if (negate) body = body.slice(1);
      out += `[${negate ? "^/" : ""}${body.replace(/[\\\]^]/g, "\\$&")}]`;
      i = end;
    } else {
      out += c.replace(/[.+^${}()|\\]/g, "\\$&");
    }
  }
  return out;
}
