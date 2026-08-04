/**
 * stabilizeStreamingMarkdown — make a PARTIAL markdown string safe to
 * render mid-stream.
 *
 * While the model streams, the visible text routinely ends inside an
 * unclosed construct: a ``` fence that hasn't closed yet, or an inline
 * `code` span missing its closing backtick. Rendering that raw makes the
 * whole tail of the message flip layout on every frame (everything after
 * the open fence renders as code, then snaps back when the fence closes)
 * — the "flash reflow" every streaming chat UI has to solve.
 *
 * The stabilizer appends the minimal closing syntax so the partial
 * document parses the way the FINAL document will:
 *   - odd number of fence lines → append a closing fence
 *   - unclosed inline code span (outside fences) → append a backtick
 *
 * It never modifies the visible characters — only appends closers — so
 * the smooth-stream cursor position is unaffected. Apply ONLY while
 * streaming; terminal text renders verbatim.
 */
export function stabilizeStreamingMarkdown(text: string): string {
  if (!text) return text;

  let inFence = false;
  let backtickBalance = 0;

  for (const line of text.split("\n")) {
    const trimmed = line.trimStart();
    if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
      inFence = !inFence;
      // A fence line resets inline-code state — spans can't cross
      // block boundaries.
      backtickBalance = 0;
      continue;
    }
    if (inFence) continue;
    // Count single backticks on this line (inline code). Escaped
    // backticks (\`) don't open spans. Inline spans can technically
    // cross single newlines, but per-line reset matches how models
    // actually emit code spans and avoids false positives from stray
    // backticks in prose paragraphs far apart.
    let lineTicks = 0;
    for (let i = 0; i < line.length; i++) {
      if (line[i] === "`" && line[i - 1] !== "\\") lineTicks++;
    }
    backtickBalance = lineTicks % 2;
  }

  let out = text;
  if (backtickBalance === 1 && !inFence) {
    out += "`";
  }
  if (inFence) {
    // Close the fence on its own line so the partial code block
    // renders as a code block instead of swallowing the document tail.
    out += out.endsWith("\n") ? "```" : "\n```";
  }
  return out;
}
