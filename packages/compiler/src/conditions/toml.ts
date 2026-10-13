// A small TOML reader, enough for the manifests dep_present reads
// (pyproject.toml and Cargo.toml; the stale-detection design §1.1): tables
// and arrays of tables, dotted and quoted keys, basic, literal and
// multi-line strings, numbers, booleans, dates, arrays and inline tables,
// comments anywhere TOML allows them. Whatever it can't read is null, so a
// check says unknown, never that a dependency is gone. It isn't a
// validator: a document TOML refuses may still read (a key defined twice
// keeps the last one), never the other way round.

export type TomlValue = string | number | boolean | TomlValue[] | TomlTable;
export interface TomlTable {
  [key: string]: TomlValue;
}

/** The document as a table, or null when it can't be read. */
export function parseToml(text: string): TomlTable | null {
  try {
    return new Reader(text).document();
  } catch {
    return null;
  }
}

/** A value at a dotted path of keys, or undefined. */
export function tomlAt(t: TomlTable, ...keys: string[]): TomlValue | undefined {
  let v: TomlValue | undefined = t;
  for (const k of keys) {
    if (!isTable(v)) return undefined;
    v = Object.prototype.hasOwnProperty.call(v, k) ? v[k] : undefined;
  }
  return v;
}

export function isTable(v: TomlValue | undefined): v is TomlTable {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

class Unreadable extends Error {}

/** A table with no prototype, so a key like __proto__ is only a key. */
function newTable(): TomlTable {
  return Object.create(null) as TomlTable;
}

const BARE = /[A-Za-z0-9_-]/;
const TOKEN = /[A-Za-z0-9_+\-.:]/;

class Reader {
  private i = 0;
  private readonly s: string;

  constructor(text: string) {
    this.s = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
  }

  private fail(): never {
    throw new Unreadable();
  }

  private ch(): string {
    return this.s[this.i] ?? "";
  }

  private startsWith(t: string): boolean {
    return this.s.startsWith(t, this.i);
  }

  /** Spaces and tabs. */
  private spaces(): void {
    while (this.ch() === " " || this.ch() === "\t") this.i++;
  }

  private comment(): void {
    if (this.ch() !== "#") return;
    while (this.i < this.s.length && this.ch() !== "\n") this.i++;
  }

  /** Spaces, comments and newlines (between statements, inside arrays). */
  private blank(): void {
    for (;;) {
      this.spaces();
      this.comment();
      if (this.ch() === "\n") this.i++;
      else if (this.startsWith("\r\n")) this.i += 2;
      else return;
    }
  }

  /** The rest of the line holds nothing but a comment. */
  private lineEnd(): void {
    this.spaces();
    this.comment();
    if (this.i >= this.s.length) return;
    if (this.ch() === "\n") this.i++;
    else if (this.startsWith("\r\n")) this.i += 2;
    else this.fail();
  }

  document(): TomlTable {
    const root = newTable();
    let current = root;
    for (;;) {
      this.blank();
      if (this.i >= this.s.length) return root;
      if (this.startsWith("[[")) {
        this.i += 2;
        const keys = this.key();
        if (!this.startsWith("]]")) this.fail();
        this.i += 2;
        current = arrayTable(root, keys);
      } else if (this.ch() === "[") {
        this.i++;
        const keys = this.key();
        if (this.ch() !== "]") this.fail();
        this.i++;
        current = table(root, keys);
      } else {
        const keys = this.key();
        if (this.ch() !== "=") this.fail();
        this.i++;
        this.spaces();
        assign(current, keys, this.value());
      }
      this.lineEnd();
    }
  }

  /** A dotted key, with the spaces around it. */
  private key(): string[] {
    const keys: string[] = [];
    for (;;) {
      this.spaces();
      if (this.ch() === '"') keys.push(this.basic());
      else if (this.ch() === "'") keys.push(this.literal());
      else {
        const start = this.i;
        while (BARE.test(this.ch())) this.i++;
        if (this.i === start) this.fail();
        keys.push(this.s.slice(start, this.i));
      }
      this.spaces();
      if (this.ch() !== ".") return keys;
      this.i++;
    }
  }

  private value(): TomlValue {
    if (this.startsWith('"""')) return this.multiBasic();
    if (this.startsWith("'''")) return this.multiLiteral();
    if (this.ch() === '"') return this.basic();
    if (this.ch() === "'") return this.literal();
    if (this.ch() === "[") return this.array();
    if (this.ch() === "{") return this.inline();
    if (this.startsWith("true") && !TOKEN.test(this.s[this.i + 4] ?? "")) {
      this.i += 4;
      return true;
    }
    if (this.startsWith("false") && !TOKEN.test(this.s[this.i + 5] ?? "")) {
      this.i += 5;
      return false;
    }
    return this.scalar();
  }

  /** A number, or a date or time (kept as its text). */
  private scalar(): TomlValue {
    const start = this.i;
    while (TOKEN.test(this.ch())) this.i++;
    let text = this.s.slice(start, this.i);
    // A date and a time apart by one space: 1979-05-27 07:32:00Z.
    if (
      /^\d{4}-\d{2}-\d{2}$/.test(text) &&
      /^ \d{2}:/.test(this.s.slice(this.i, this.i + 4))
    ) {
      this.i++;
      while (TOKEN.test(this.ch())) this.i++;
      text = this.s.slice(start, this.i);
    }
    if (text === "") this.fail();
    const n = text.replace(/_/g, "");
    if (/^[+-]?(\d+)$/.test(n) || /^[+-]?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(n))
      return Number(n);
    if (/^0x[0-9a-fA-F]+$/.test(n)) return parseInt(n.slice(2), 16);
    if (/^0o[0-7]+$/.test(n)) return parseInt(n.slice(2), 8);
    if (/^0b[01]+$/.test(n)) return parseInt(n.slice(2), 2);
    if (/^[+-]?(inf|nan)$/.test(n))
      return n.endsWith("inf")
        ? n.startsWith("-")
          ? -Infinity
          : Infinity
        : NaN;
    if (/^\d{4}-\d{2}-\d{2}|^\d{2}:\d{2}/.test(text)) return text;
    return this.fail();
  }

  private basic(): string {
    this.i++; // "
    let out = "";
    for (;;) {
      const ch = this.ch();
      if (ch === "" || ch === "\n") this.fail();
      this.i++;
      if (ch === '"') return out;
      out += ch === "\\" ? this.escape() : ch;
    }
  }

  private literal(): string {
    this.i++; // '
    const end = this.s.indexOf("'", this.i);
    const nl = this.s.indexOf("\n", this.i);
    if (end < 0 || (nl >= 0 && nl < end)) this.fail();
    const out = this.s.slice(this.i, end);
    this.i = end + 1;
    return out;
  }

  private multiBasic(): string {
    this.i += 3;
    if (this.startsWith("\r\n")) this.i += 2;
    else if (this.ch() === "\n") this.i++;
    let out = "";
    for (;;) {
      if (this.i >= this.s.length) this.fail();
      if (this.startsWith('"""')) {
        // Up to two quotes may close the string's content.
        let n = 3;
        while (n < 5 && this.s[this.i + n] === '"') n++;
        out += '"'.repeat(n - 3);
        this.i += n;
        return out;
      }
      const ch = this.ch();
      this.i++;
      if (ch !== "\\") {
        out += ch;
        continue;
      }
      // A backslash at a line's end trims the whitespace after it.
      const rest = /^[ \t]*\r?\n/.exec(this.s.slice(this.i, this.i + 256));
      if (rest) {
        this.i += rest[0].length;
        while (/[ \t\r\n]/.test(this.ch())) this.i++;
        continue;
      }
      out += this.escape();
    }
  }

  private multiLiteral(): string {
    this.i += 3;
    if (this.startsWith("\r\n")) this.i += 2;
    else if (this.ch() === "\n") this.i++;
    const end = this.s.indexOf("'''", this.i);
    if (end < 0) this.fail();
    let n = 3;
    while (n < 5 && this.s[end + n] === "'") n++;
    const out = this.s.slice(this.i, end + n - 3);
    this.i = end + n;
    return out;
  }

  /** The escape after a backslash (already read). */
  private escape(): string {
    const ch = this.ch();
    this.i++;
    switch (ch) {
      case "b":
        return "\b";
      case "t":
        return "\t";
      case "n":
        return "\n";
      case "f":
        return "\f";
      case "r":
        return "\r";
      case "e":
        return "\x1b";
      case '"':
        return '"';
      case "\\":
        return "\\";
      case "u":
      case "U": {
        const len = ch === "u" ? 4 : 8;
        const hex = this.s.slice(this.i, this.i + len);
        if (!new RegExp(`^[0-9a-fA-F]{${len}}$`).test(hex)) this.fail();
        this.i += len;
        const cp = parseInt(hex, 16);
        if (cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff)) this.fail();
        return String.fromCodePoint(cp);
      }
      default:
        return this.fail();
    }
  }

  private array(): TomlValue[] {
    this.i++; // [
    const out: TomlValue[] = [];
    for (;;) {
      this.blank();
      if (this.ch() === "]") {
        this.i++;
        return out;
      }
      out.push(this.value());
      this.blank();
      if (this.ch() === ",") this.i++;
      else if (this.ch() !== "]") this.fail();
    }
  }

  /** An inline table (newlines and a trailing comma allowed, as TOML 1.1 does). */
  private inline(): TomlTable {
    this.i++; // {
    const out = newTable();
    for (;;) {
      this.blank();
      if (this.ch() === "}") {
        this.i++;
        return out;
      }
      const keys = this.key();
      if (this.ch() !== "=") this.fail();
      this.i++;
      this.spaces();
      assign(out, keys, this.value());
      this.blank();
      if (this.ch() === ",") this.i++;
      else if (this.ch() !== "}") this.fail();
    }
  }
}

/** The table at keys under t, made as needed (an array of tables: its last). */
function descend(t: TomlTable, keys: string[]): TomlTable {
  let cur = t;
  for (const k of keys) {
    let next: TomlValue | undefined = Object.prototype.hasOwnProperty.call(
      cur,
      k,
    )
      ? cur[k]
      : undefined;
    if (next === undefined) {
      next = newTable();
      cur[k] = next;
    } else if (Array.isArray(next)) {
      const last: TomlValue | undefined = next[next.length - 1];
      if (!isTable(last)) throw new Unreadable();
      next = last;
    }
    if (!isTable(next)) throw new Unreadable();
    cur = next;
  }
  return cur;
}

function table(root: TomlTable, keys: string[]): TomlTable {
  return descend(root, keys);
}

function arrayTable(root: TomlTable, keys: string[]): TomlTable {
  const parent = descend(root, keys.slice(0, -1));
  const k = keys[keys.length - 1];
  const existing: TomlValue | undefined = Object.prototype.hasOwnProperty.call(
    parent,
    k,
  )
    ? parent[k]
    : undefined;
  const arr: TomlValue[] =
    existing === undefined ? [] : Array.isArray(existing) ? existing : [];
  if (existing !== undefined && !Array.isArray(existing))
    throw new Unreadable();
  const t = newTable();
  arr.push(t);
  parent[k] = arr;
  return t;
}

function assign(t: TomlTable, keys: string[], v: TomlValue): void {
  const parent = descend(t, keys.slice(0, -1));
  parent[keys[keys.length - 1]] = v;
}
