"use client";

import { memo, useCallback, useEffect, useMemo, useRef } from "react";
import type { ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/**
 * AI answer with markdown rendering + clickable [N] citation badges.
 *
 * Streaming-aware split rendering (Perplexity / Claude.ai / Cursor pattern):
 *
 *   text = "…committed paragraphs…\n\ntail paragraph still streaming"
 *          └──────── committed ────┘  └────── tail ──────┘
 *
 *   • Committed part (everything up to the last `\n\n`) is parsed with
 *     ReactMarkdown ONCE per paragraph boundary and memoized, so tokens
 *     arriving mid-stream never re-parse the earlier text. This cuts the
 *     work from O(n²) (re-parse the whole answer on every token) down to
 *     O(n) total.
 *
 *   • Tail (current in-progress paragraph) renders as plain <p> with
 *     inline citation badges applied via a lightweight regex split —
 *     no markdown parser runs on it while tokens arrive. Bold/italic in
 *     the tail becomes rich only when the paragraph closes and rolls
 *     into `committed`.
 *
 * `React.memo` + reference-stable callbacks keep parent re-renders from
 * re-executing the committed parse when nothing visible changed.
 */

const NORMALIZE_PATTERNS: Array<[RegExp, string]> = [
  [/\\\*\\\*([\s\S]+?)\\\*\\\*/g, "**$1**"],
  [/\\_\\_([\s\S]+?)\\_\\_/g, "__$1__"],
  [/\\\*([^\n]+?)\\\*/g, "*$1*"],
  [/\\_([^\n]+?)\\_/g, "_$1_"],
  [/\\`([^`\n]+?)\\`/g, "`$1`"],
];

function normalize(input: string): string {
  let out = input;
  for (const [re, rep] of NORMALIZE_PATTERNS) out = out.replace(re, rep);
  return out;
}

function expandCitationRanges(input: string): string {
  return input
    .replace(/\[(\d+)-(\d+)\]/g, (_, a, b) => {
      const start = parseInt(a, 10);
      const end = parseInt(b, 10);
      return Array.from(
        { length: end - start + 1 },
        (__, i) => `[${start + i}]`,
      ).join("");
    })
    .replace(/\[(\d+)\]/g, "[$1](#cite-$1)");
}

/** True when the prefix ends with an unclosed ```` ``` ```` fence — splitting
 *  inside one would commit a fenced block that doesn't terminate and render
 *  the rest of the answer outside the code block. */
function hasUnclosedCodeFence(prefix: string): boolean {
  const fences = prefix.match(/```/g);
  return fences !== null && fences.length % 2 === 1;
}

/** Split text at the last paragraph break. Committed = parsed once.
 *  Tail = the in-progress paragraph, re-rendered as plain text.
 *
 *  When `isStreaming` is false (stream done OR initial render for a cached
 *  answer), everything goes to `committed` so markdown formatting (bold,
 *  italic, fenced code, etc.) renders even if the answer has no `\n\n`
 *  paragraph break. Without this, a single-paragraph answer would stay in
 *  the plain-text tail forever and show raw `**` markers.
 *
 *  When the committed prefix ends inside an unclosed fenced code block,
 *  fall back to tail-only so we don't render a broken half-fence. */
function splitAtBlockBoundary(
  text: string,
  isStreaming: boolean,
): {
  committed: string;
  tail: string;
} {
  if (!isStreaming) return { committed: text, tail: "" };
  const idx = text.lastIndexOf("\n\n");
  if (idx < 0) return { committed: "", tail: text };
  const candidateCommitted = text.slice(0, idx);
  if (hasUnclosedCodeFence(candidateCommitted)) {
    return { committed: "", tail: text };
  }
  return { committed: candidateCommitted, tail: text.slice(idx + 2) };
}

function CitationBadge({
  index,
  sources,
  onSourceClick,
}: {
  index: number;
  sources: Array<{ id: string }>;
  onSourceClick: (id: string) => void;
}) {
  const src = sources[index - 1];
  if (!src) {
    return (
      <span className="inline-flex items-center justify-center text-[13px] md:text-[12px] text-fg-4 bg-surface-1 rounded px-1.5 md:px-1 py-px mx-0.5 font-mono align-baseline">
        {index}
      </span>
    );
  }
  return (
    <button
      type="button"
      onPointerDown={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onSourceClick(src.id);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSourceClick(src.id);
        }
      }}
      className="relative inline-flex items-center justify-center text-[13px] md:text-[12px] text-fg-3 hover:text-fg-2 bg-surface-2 hover:bg-surface-3 rounded px-1.5 md:px-1 py-px mx-0.5 font-mono cursor-pointer transition-colors align-baseline select-none touch-manipulation no-underline after:absolute after:-inset-1 after:content-[''] after:pointer-events-none pointer-events-auto"
    >
      {index}
    </button>
  );
}

/** Tail renderer — plain paragraph with live citation badges. No markdown
 *  parse runs here; only a split on `[N]` tokens. Cheap per update. */
function TailParagraph({
  text,
  sources,
  onSourceClick,
}: {
  text: string;
  sources: Array<{ id: string }>;
  onSourceClick: (id: string) => void;
}) {
  // Expand ranges first so [1-3] in the tail still behaves like the
  // committed parser — then split on [N] to interleave badges.
  const expanded = text.replace(/\[(\d+)-(\d+)\]/g, (_, a, b) => {
    const start = parseInt(a, 10);
    const end = parseInt(b, 10);
    return Array.from(
      { length: end - start + 1 },
      (__, i) => `[${start + i}]`,
    ).join("");
  });
  const parts = expanded.split(/(\[\d+\])/g);
  const children: ReactNode[] = parts.map((part, i) => {
    const m = part.match(/^\[(\d+)\]$/);
    if (m) {
      return (
        <CitationBadge
          key={`cite-${i}`}
          index={parseInt(m[1], 10)}
          sources={sources}
          onSourceClick={onSourceClick}
        />
      );
    }
    return <span key={`text-${i}`}>{part}</span>;
  });
  return (
    <p
      className="text-[14px] leading-[1.75]"
      style={{ letterSpacing: "-0.006em" }}
    >
      {children}
    </p>
  );
}

/** Committed markdown renderer — full ReactMarkdown pass. Memoized by
 *  the committed string so it only re-parses when a new paragraph lands. */
function CommittedMarkdown({
  text,
  sources,
  onSourceClick,
}: {
  text: string;
  sources: Array<{ id: string }>;
  onSourceClick: (id: string) => void;
}) {
  const processed = useMemo(
    () => expandCitationRanges(normalize(text)),
    [text],
  );
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        p: ({ children }) => (
          <p
            className="text-[14px] leading-[1.75]"
            style={{ letterSpacing: "-0.006em" }}
          >
            {children}
          </p>
        ),
        strong: ({ children }) => (
          <strong className="font-medium text-foreground">{children}</strong>
        ),
        a: ({ href, children }) => {
          const linkText = String(children);
          const match = linkText.match(/^(\d+)$/);
          if (match && href?.startsWith("#cite-")) {
            return (
              <CitationBadge
                index={parseInt(match[1], 10)}
                sources={sources}
                onSourceClick={onSourceClick}
              />
            );
          }
          return (
            <a href={href} className="text-fg-2 underline hover:text-fg-1">
              {children}
            </a>
          );
        },
      }}
    >
      {processed}
    </ReactMarkdown>
  );
}

const CommittedMarkdownMemo = memo(
  CommittedMarkdown,
  (prev, next) =>
    prev.text === next.text &&
    prev.sources === next.sources &&
    prev.onSourceClick === next.onSourceClick,
);

function AISummaryImpl({
  text,
  sources,
  onSourceClick,
  isStreaming = false,
}: {
  text: string;
  sources: Array<{ id: string }>;
  onSourceClick: (id: string) => void;
  /** While true, the in-progress last paragraph renders as plain text to
   *  avoid a full markdown re-parse on every token. When false (stream
   *  done), the entire answer parses as markdown so a single-paragraph
   *  reply still shows proper bold/italic/code formatting. */
  isStreaming?: boolean;
}) {
  // Latest-ref pattern — parents typically pass a fresh inline closure
  // every render, which would defeat CommittedMarkdownMemo's identity
  // compare. Store the latest callback in a ref and expose a stable
  // dispatcher whose identity never changes, so memoized children stay
  // cached across parent re-renders.
  const onSourceClickRef = useRef(onSourceClick);
  useEffect(() => {
    onSourceClickRef.current = onSourceClick;
  }, [onSourceClick]);
  const stableOnSourceClick = useCallback((id: string) => {
    onSourceClickRef.current(id);
  }, []);
  const { committed, tail } = useMemo(
    () => splitAtBlockBoundary(text, isStreaming),
    [text, isStreaming],
  );

  return (
    <>
      {committed ? (
        <CommittedMarkdownMemo
          text={committed}
          sources={sources}
          onSourceClick={stableOnSourceClick}
        />
      ) : null}
      {tail ? (
        <TailParagraph
          text={tail}
          sources={sources}
          onSourceClick={stableOnSourceClick}
        />
      ) : null}
    </>
  );
}

export const AISummary = memo(AISummaryImpl);
