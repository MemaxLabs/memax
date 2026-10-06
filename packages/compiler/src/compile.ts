/**
 * `compile(input) → result`: a pure function. No I/O, no clock, no
 * randomness; the same input gives the same bytes on every platform.
 */
import { adapters, getAdapter } from "./adapters/index.js";
import type { Adapter, Output } from "./adapters/adapter.js";
import type { Limit } from "./document.js";
import { sha256Hex, byteLength } from "./hash.js";
import type { Target } from "./model.js";
import { byCodeUnit, kib, plural, thousands } from "./text.js";
import type {
  CompiledCopy,
  CompiledFile,
  CompiledText,
  CompileInput,
  CompileResult,
  CompileWarning,
  TargetResult,
} from "./types.js";
import { validate } from "./validate.js";

/** The default budget per file: Codex's cap, so AGENTS.md always fits. */
export const DEFAULT_BUDGET = 32 * 1024;

/** Warn when a file reaches this share of a hard cap. */
const NEAR = 0.9;

/**
 * Compiles a Space's kept record into every target's files.
 *
 * Throws `CompileInputError` (listing every problem) when the input is
 * malformed or carries proposals or quarantined content.
 */
export function compile(input: CompileInput): CompileResult {
  const model = validate(input);
  const warnings: CompileWarning[] = [...model.warnings];
  const files: CompiledFile[] = [];
  const copies: CompiledCopy[] = [];
  const targets: TargetResult[] = [];

  const order = (t: Target) => adapters.findIndex((a) => a.kind === t.kind);
  const sorted = [...model.targets].sort(
    (a, b) => order(a) - order(b) || byCodeUnit(a.path, b.path),
  );

  for (const target of sorted) {
    const adapter = getAdapter(target.kind);
    const limit = limitFor(adapter, target, warnings);
    const outputs = adapter.render(model, target, limit);
    const texts = outputs.map((output) => {
      const text = toText(adapter, output);
      warnings.push(...warningsFor(adapter, output, text, limit));
      if (output.path === null) {
        copies.push({ label: output.label ?? adapter.tool, ...text });
      } else {
        files.push({
          path: output.path,
          ...text,
          drift_sha256: sha256Hex(output.driftText),
          user_owned: output.userOwned,
        });
      }
      return text;
    });
    targets.push({
      kind: target.kind,
      path: target.path === "" ? null : target.path,
      delivery: adapter.role === "copy" ? "copy" : "file",
      reads: adapter.reads(target),
      files: outputs.flatMap((o) => (o.path === null ? [] : [o.path])),
      refs: union(texts.map((t) => t.refs)),
      dropped_for_budget: union(texts.map((t) => t.dropped_for_budget)),
    });
  }

  return {
    version: 1,
    compile_id: model.compileId,
    compiled_at: model.compiledAt,
    brief_id: model.brief.id,
    space: model.space.slug,
    files: files.sort((a, b) => byCodeUnit(a.path, b.path)),
    copies,
    targets,
    warnings: warnings.sort(
      (a, b) =>
        byCodeUnit(a.path ?? "", b.path ?? "") ||
        byCodeUnit(a.code, b.code) ||
        byCodeUnit(a.ref ?? "", b.ref ?? "") ||
        byCodeUnit(a.message, b.message),
    ),
  };
}

function limitFor(
  adapter: Adapter,
  target: Target,
  warnings: CompileWarning[],
): Limit {
  const requested = target.budget ?? DEFAULT_BUDGET;
  const cap = adapter.limits.bytes;
  if (cap && requested > cap.value) {
    warnings.push({
      code: "budget_capped",
      target: target.kind,
      path: target.path,
      message: `The ${kib(requested)} budget for ${target.path} is over ${cap.tool}'s ${kib(cap.value)} cap, so it compiles to ${kib(cap.value)}.`,
    });
  }
  return {
    bytes: cap ? Math.min(requested, cap.value) : requested,
    chars: adapter.limits.chars?.value ?? null,
  };
}

function toText(adapter: Adapter, output: Output): CompiledText {
  const { content, doc } = output;
  return {
    target: adapter.kind,
    content,
    bytes: byteLength(content),
    lines: countLines(content),
    sha256: sha256Hex(content),
    refs: union([doc.included.flatMap((e) => (e.ref ? [e.ref] : []))]),
    cites: union([doc.included.flatMap((e) => e.refs)]),
    dropped_for_budget: union([
      doc.dropped.flatMap((e) => (e.ref ? [e.ref] : [])),
    ]),
  };
}

function warningsFor(
  adapter: Adapter,
  output: Output,
  text: CompiledText,
  limit: Limit,
): CompileWarning[] {
  const where = output.path ?? output.label ?? adapter.tool;
  const base = {
    target: adapter.kind,
    ...(output.path ? { path: output.path } : {}),
  };
  const warnings: CompileWarning[] = [];
  const { bytes, chars, lines } = adapter.limits;

  if (bytes && text.bytes >= NEAR * bytes.value) {
    warnings.push({
      ...base,
      code: "near_limit",
      message: `${where} at ${kib(text.bytes)} is near ${bytes.tool}'s ${kib(bytes.value)} cap.`,
    });
  }
  if (chars && output.content.length >= NEAR * chars.value) {
    warnings.push({
      ...base,
      code: "near_limit",
      message: `${where} at ${thousands(output.content.length)} characters is near ${chars.tool}'s ${thousands(chars.value)}-character cap.`,
    });
  }
  if (lines && text.lines > lines.value) {
    warnings.push({
      ...base,
      code: "over_guidance",
      message: `${where} has ${text.lines} lines; ${lines.tool} suggests under ${lines.value}.`,
    });
  }

  const budget = `${kib(limit.bytes)}${limit.chars ? ` and ${thousands(limit.chars)}-character` : ""} budget`;
  const facts = text.dropped_for_budget.length;
  if (facts > 0) {
    warnings.push({
      ...base,
      code: "dropped_for_budget",
      message: `${plural(facts, "fact")} didn't fit the ${budget} for ${where}. The rest stay live over MCP.`,
    });
  }
  const prose = output.doc.dropped.filter((e) => e.ref === null).length;
  if (prose > 0) {
    warnings.push({
      ...base,
      code: "prose_dropped_for_budget",
      message: `${plural(prose, "line")} of Brief prose didn't fit the ${budget} for ${where}.`,
    });
  }
  return warnings;
}

function countLines(content: string): number {
  if (content === "") return 0;
  const breaks = content.split("\n").length - 1;
  return content.endsWith("\n") ? breaks : breaks + 1;
}

function union(lists: string[][]): string[] {
  return [...new Set(lists.flat())].sort(byCodeUnit);
}
