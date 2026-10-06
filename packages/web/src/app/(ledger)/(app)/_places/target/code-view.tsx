import styles from "./target.module.css";

/** How a line of a compiled file is set: the header comment, headings, frontmatter, a stale fact, or plain. */
export type LineKind = "comment" | "heading" | "front" | "stale" | "plain";

export interface CodeLine {
  kind: LineKind;
  /** The words before the trailing cites. */
  text: string;
  /** "[M-0219]" or "[M-0431, M-0174]", set quieter. */
  cites: string | null;
  /** The line number, as the file has it. */
  n: number;
}

const CITES = /^(.*?)(\s\[M-\d+(?:, M-\d+)*\])$/;

/**
 * A compiled file's lines as the code view sets them: the file's own
 * line numbers, frontmatter and the header comment quiet, headings
 * medium, stale facts on ochre (TargetPreview.png). Nothing is wrapped
 * or reflowed: the view shows the bytes Memax writes.
 */
export function codeLines(content: string): CodeLine[] {
  const lines = content.split("\n");
  // A file ends with a newline; the empty string after it isn't a line.
  if (lines[lines.length - 1] === "") lines.pop();
  let front = lines[0] === "---";
  return lines.map((line, i) => {
    let kind: LineKind = "plain";
    if (front) {
      kind = "front";
      if (i > 0 && line === "---") front = false;
    } else if (line.startsWith("<!--")) {
      kind = "comment";
    } else if (/^#{1,6}\s/.test(line)) {
      kind = "heading";
    } else if (/^\s*- \(Being verified\)/.test(line)) {
      kind = "stale";
    }
    const match =
      kind === "plain" || kind === "stale" ? CITES.exec(line) : null;
    return {
      kind,
      text: match ? match[1]! : line,
      cites: match ? match[2]! : null,
      n: i + 1,
    };
  });
}

/**
 * The exact compiled content with line numbers: `white-space: pre`, so
 * a long line scrolls sideways and never breaks mid-token (design review
 * §2). Line numbers are CSS counters, so copying takes the file only.
 * The view is a focusable region, so the keyboard can scroll it.
 */
export function CodeView({
  content,
  label,
}: {
  content: string;
  /** The region's name: "Contents of AGENTS.md". */
  label: string;
}) {
  return (
    <pre className={styles.code} tabIndex={0} role="region" aria-label={label}>
      <code className={styles.codeLines}>
        {codeLines(content).map((line) => (
          <span key={line.n} className={`${styles.line} ${styles[line.kind]}`}>
            {line.text}
            {line.cites ? <i className={styles.cites}>{line.cites}</i> : null}
          </span>
        ))}
      </code>
    </pre>
  );
}
