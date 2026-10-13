// Agents' JSON settings, edited in place. Several accept comments (Gemini
// CLI reads settings.json through strip-json-comments, OpenCode reads
// opencode.jsonc, VS Code reads mcp.json with comments), so JSON.parse
// refuses them, and a writer that serialises the whole object again drops
// every comment. These change the one value Memax owns and leave every
// other byte as the person wrote it: a new key goes on its own line before
// the closing brace, so a comment after the previous one stays with it.
import {
  findNodeAtLocation,
  getNodeValue,
  parse,
  parseTree,
  type JSONPath,
  type Node,
  type ParseError,
} from "jsonc-parser";

const OPTIONS = { allowTrailingComma: true, disallowComments: false };

/** A settings file Memax won't edit: it isn't JSON, even allowing comments. */
export class SettingsFileError extends Error {
  constructor(
    readonly path: string,
    what: string,
  ) {
    super(
      `${path} isn't valid JSON, so Memax didn't change it. Fix it, or ${what} by hand.`,
    );
    this.name = "SettingsFileError";
  }
}

/** The file's top-level object, or null when it isn't one (comments allowed). */
export function parseJsoncObject(text: string): Record<string, unknown> | null {
  if (text.trim() === "") return {};
  const errors: ParseError[] = [];
  const value: unknown = parse(text, errors, OPTIONS);
  if (errors.length > 0) return null;
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

/** The indentation of the line `offset` is on. */
function lineIndent(text: string, offset: number): string {
  const start = text.lastIndexOf("\n", offset - 1) + 1;
  return /^[ \t]*/.exec(text.slice(start))?.[0] ?? "";
}

function splice(text: string, at: number, length: number, add: string) {
  return text.slice(0, at) + add + text.slice(at + length);
}

/** Text between two places with its comments taken out. */
function code(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*/g, "");
}

/**
 * `text` with the value at `path` (object keys) set, or removed when
 * `value` is undefined. New lines follow the file's own indentation and
 * line endings. Throws when `text` isn't a JSON object or the path runs
 * through something that isn't one.
 */
export function setJsonc(text: string, path: JSONPath, value: unknown): string {
  const base = text.trim() === "" ? "{}\n" : text;
  const eol = base.includes("\r\n") ? "\r\n" : "\n";
  const unit = /^([ \t]+)\S/m.exec(base)?.[1] ?? "  ";
  const end = (s: string) => (s.endsWith("\n") ? s : `${s}${eol}`);
  const render = (v: unknown, indent: string) =>
    JSON.stringify(v, null, unit).split("\n").join(`${eol}${indent}`);

  if (!parseJsoncObject(base)) throw new Error("not a JSON object");
  const tree = parseTree(base, [], OPTIONS) as Node;
  if (value === undefined) return end(remove(base, tree, path));
  let depth = path.length;
  let node = findNodeAtLocation(tree, path);
  while (!node && depth > 0) {
    depth -= 1;
    node = findNodeAtLocation(tree, path.slice(0, depth));
  }
  if (!node) throw new Error("not a JSON object");
  if (depth === path.length) {
    // The key is there: its value is replaced where it stands.
    return end(
      splice(
        base,
        node.offset,
        node.length,
        render(value, lineIndent(base, node.offset)),
      ),
    );
  }
  if (node.type !== "object") {
    throw new Error(`${path.slice(0, depth).join(".")} isn't an object`);
  }
  let nested = value;
  for (let k = path.length - 1; k > depth; k--) nested = { [path[k]]: nested };

  const close = node.offset + node.length - 1;
  const outer = lineIndent(base, node.offset);
  const props = node.children ?? [];
  const inner =
    props.length > 0 ? lineIndent(base, props[0].offset) : outer + unit;
  const entry = `${JSON.stringify(path[depth])}: ${render(nested, inner)}`;
  const after =
    props.length > 0
      ? props[props.length - 1].offset + props[props.length - 1].length
      : node.offset + 1;
  let at = close;
  while (at > after && /\s/.test(base[at - 1])) at -= 1;
  let out = splice(
    base,
    at,
    close - at,
    `${eol}${inner}${entry}${eol}${outer}`,
  );
  if (props.length > 0 && !code(base.slice(after, close)).includes(",")) {
    out = splice(out, after, 0, ",");
  }
  return end(out);
}

/** Where the next `,` is after `from`, past spaces only; -1 when there's none. */
function commaAfter(text: string, from: number): number {
  let i = from;
  while (text[i] === " " || text[i] === "\t") i += 1;
  return text[i] === "," ? i : -1;
}

/**
 * Takes out the property at `path` and one comma: its own, or the one
 * after the property before it when it was the last. Comments stay, and so
 * does its line when anything but space is left on it.
 */
function remove(text: string, tree: Node, path: JSONPath): string {
  const prop = findNodeAtLocation(tree, path)?.parent;
  if (!prop || prop.type !== "property" || !prop.parent) return text;
  const siblings = prop.parent.children ?? [];
  const i = siblings.indexOf(prop);
  let start = prop.offset;
  let stop = prop.offset + prop.length;
  let out = text;
  const own = commaAfter(text, stop);
  if (own >= 0) stop = own + 1;
  else if (i > 0) {
    const prev = siblings[i - 1];
    const theirs = commaAfter(text, prev.offset + prev.length);
    if (theirs >= 0) {
      out = splice(out, theirs, 1, "");
      start -= 1;
      stop -= 1;
    }
  }
  const lineStart = out.lastIndexOf("\n", start - 1) + 1;
  const lineEnd = out.indexOf("\n", stop);
  const before = out.slice(lineStart, start);
  const rest = out.slice(stop, lineEnd < 0 ? out.length : lineEnd);
  if (before.trim() === "" && rest.trim() === "") {
    return splice(
      out,
      lineStart,
      (lineEnd < 0 ? out.length : lineEnd + 1) - lineStart,
      "",
    );
  }
  return splice(out, start, stop - start, "");
}

/** Where the next `,` is after `from`, past spaces, line ends and comments; -1 when there's none. */
function commaPast(text: string, from: number, to: number): number {
  let i = from;
  while (i < to) {
    if (/\s/.test(text[i])) i += 1;
    else if (text.startsWith("//", i)) {
      const nl = text.indexOf("\n", i);
      i = nl < 0 ? to : nl + 1;
    } else if (text.startsWith("/*", i)) {
      const close = text.indexOf("*/", i + 2);
      i = close < 0 ? to : close + 2;
    } else return text[i] === "," ? i : -1;
  }
  return -1;
}

/**
 * `text` with `value` added to the end of the array at `path`, or the
 * array made when it isn't there. The elements already there, their
 * comments and the file's other bytes stay as they are: on its own line
 * after the last element when the array spans lines, else after it on the
 * same one. Throws when the path holds something that isn't an array.
 */
export function appendJsonc(
  text: string,
  path: JSONPath,
  value: unknown,
): string {
  const base = text.trim() === "" ? "{}\n" : text;
  if (!parseJsoncObject(base)) throw new Error("not a JSON object");
  const tree = parseTree(base, [], OPTIONS) as Node;
  const node = findNodeAtLocation(tree, path);
  if (!node) return setJsonc(text, path, [value]);
  if (node.type !== "array") throw new Error(`${path.join(".")} isn't a list`);
  const eol = base.includes("\r\n") ? "\r\n" : "\n";
  const item = JSON.stringify(value);
  const close = node.offset + node.length - 1;
  const items = node.children ?? [];
  const end = (s: string) => (s.endsWith("\n") ? s : `${s}${eol}`);
  if (items.length === 0) return end(splice(base, node.offset + 1, 0, item));
  const last = items[items.length - 1];
  const after = last.offset + last.length;
  const trailing = commaPast(base, after, close);
  const multiline = base.slice(node.offset, close).includes("\n");
  const at = trailing >= 0 ? trailing + 1 : after;
  const lead = trailing >= 0 ? "" : ",";
  return end(
    splice(
      base,
      at,
      0,
      multiline
        ? `${lead}${eol}${lineIndent(base, last.offset)}${item}`
        : `${lead} ${item}`,
    ),
  );
}

/**
 * `text` with every element of the array at `path` that `match` picks
 * taken out, each with one comma (its own, or the one before it when it
 * was the last), and its line when nothing else is on it. Comments stay.
 * Unchanged when there's no such array or no such element.
 */
export function removeFromJsonc(
  text: string,
  path: JSONPath,
  match: (value: unknown) => boolean,
): string {
  let out = text;
  for (;;) {
    if (!parseJsoncObject(out)) return out;
    const tree = parseTree(out, [], OPTIONS) as Node;
    const node = findNodeAtLocation(tree, path);
    if (node?.type !== "array") return out;
    const items = node.children ?? [];
    const i = items.findIndex((n) => match(getNodeValue(n)));
    if (i < 0) return out;
    out = cut(out, items, i, node.offset + node.length - 1);
  }
}

/** Takes element `i` out of `items` (an array's), with one comma. */
function cut(text: string, items: Node[], i: number, close: number): string {
  const el = items[i];
  const elEnd = el.offset + el.length;
  const own = commaPast(text, elEnd, close);
  if (own < 0 && i > 0) {
    // The last one: the comma before it goes, and with nothing but space
    // between, everything from that comma on (`["a", "b"]` → `["a"]`).
    const prev = items[i - 1];
    const theirs = commaPast(text, prev.offset + prev.length, el.offset);
    if (theirs >= 0 && text.slice(theirs + 1, el.offset).trim() === "")
      return splice(text, theirs, elEnd - theirs, "");
    if (theirs >= 0) {
      const out = splice(text, theirs, 1, "");
      return dropSpan(out, el.offset - 1, elEnd - 1);
    }
  }
  return dropSpan(text, el.offset, own >= 0 ? own + 1 : elEnd);
}

/** `text` without [start, stop), and the line when nothing else is on it. */
function dropSpan(text: string, start: number, stop: number): string {
  const lineStart = text.lastIndexOf("\n", start - 1) + 1;
  const lineEnd = text.indexOf("\n", stop);
  const before = text.slice(lineStart, start);
  const rest = text.slice(stop, lineEnd < 0 ? text.length : lineEnd);
  if (before.trim() === "" && rest.trim() === "") {
    return splice(
      text,
      lineStart,
      (lineEnd < 0 ? text.length : lineEnd + 1) - lineStart,
      "",
    );
  }
  // On a line with others: one space after it goes too (`["a", "b"]`).
  const end =
    text[stop] === " " && /[[ ]$/.test(text.slice(0, start)) ? stop + 1 : stop;
  return splice(text, start, end - start, "");
}
