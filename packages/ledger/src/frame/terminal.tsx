import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import type { TerminalLineKind } from "../lib/types";

export interface TerminalLine {
  /** `cmd` ›, `ok` ✓, `kept` ● (green), `proposed` and `warn` ○ (ochre), `forgotten` ▬. */
  kind?: TerminalLineKind;
  /** Exactly as the CLI prints it; spacing is kept and lines never wrap. */
  text?: string;
}

export interface TerminalProps {
  /** The window title ("memax", "~/code/memax · zsh"). */
  title?: string;
  lines: TerminalLine[];
  className?: string;
}

const GLYPHS: Partial<Record<TerminalLineKind, string>> = {
  cmd: "›",
  ok: "✓",
  kept: "●",
  proposed: "○",
  warn: "○",
  forgotten: "▬",
};

/**
 * The CLI as a first-class surface, on `night`. Its lines carry the same marks
 * as the app. Only kept or in-sync lines are green. The body scrolls sideways
 * instead of wrapping, and is focusable so it scrolls from the keyboard.
 */
export function Terminal({ title = "memax", lines, className }: TerminalProps) {
  const { strings } = useLedger();
  const words: Partial<Record<TerminalLineKind, string>> = {
    ok: strings.terminal.ok,
    kept: strings.terminal.kept,
    proposed: strings.terminal.proposed,
    warn: strings.terminal.warn,
    forgotten: strings.terminal.forgotten,
  };
  return (
    <div className={cx("mx-term", className)}>
      <div className="mx-term-bar">
        <span className="mx-term-dots" aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
        <span className="mx-term-title">{title}</span>
      </div>
      <pre
        className="mx-term-body"
        tabIndex={0}
        role="region"
        aria-label={format(strings.terminal.region, { title })}
      >
        {lines.map((line, i) => {
          const kind = line.kind ?? "out";
          const glyph = GLYPHS[kind];
          const word = words[kind];
          return (
            <span key={i} className={`mx-term-line is-${kind}`}>
              {glyph ? (
                <span className="mx-term-glyph" aria-hidden="true">
                  {`${glyph} `}
                </span>
              ) : null}
              {word ? <span className="mx-sr">{word} </span> : null}
              {line.text || " "}
            </span>
          );
        })}
      </pre>
    </div>
  );
}
