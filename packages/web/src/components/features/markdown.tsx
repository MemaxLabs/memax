"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@memaxlabs/ui";

interface MarkdownProps {
  content: string;
  className?: string;
  /** When provided, [N] citations become interactive elements */
  onCitationClick?: (index: number) => void;
  onCitationHover?: (index: number | null) => void;
}

export function Markdown({
  content,
  className,
  onCitationClick,
  onCitationHover,
}: MarkdownProps) {
  // If citation handlers are provided, pre-process the content to wrap [N] in markers
  const processed = onCitationClick || onCitationHover ? content : content;

  // Pre-process content: if citation handlers are provided, convert [N] to
  // special markers that survive markdown parsing, then we handle them in components
  let processedContent = content;
  if (onCitationClick || onCitationHover) {
    // Replace [N] with a custom marker that won't be parsed as a markdown link
    processedContent = content.replace(/\[(\d+)\]/g, "{{cite:$1}}");
  }

  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      className={cn("memax-prose", className)}
      components={{
        p: ({ children }) => {
          if (!onCitationClick && !onCitationHover) {
            return <p>{children}</p>;
          }
          return (
            <p>
              {processCitations(children, onCitationClick, onCitationHover)}
            </p>
          );
        },
        li: ({ children }) => {
          if (!onCitationClick && !onCitationHover) {
            return <li>{children}</li>;
          }
          return (
            <li>
              {processCitations(children, onCitationClick, onCitationHover)}
            </li>
          );
        },
        code: ({ className: codeClassName, children, ...props }) => {
          const isInline = !codeClassName;
          if (isInline) {
            return (
              <code
                className="px-1.5 py-0.5 rounded-md bg-muted/60 text-[0.85em] font-mono"
                {...props}
              >
                {children}
              </code>
            );
          }
          return (
            <code className={cn("block", codeClassName)} {...props}>
              {children}
            </code>
          );
        },
        pre: ({ children }) => (
          <pre className="rounded-surface bg-muted/30 border border-border/20 p-4 overflow-x-auto text-[15px] font-mono leading-relaxed">
            {children}
          </pre>
        ),
        table: ({ children }) => (
          <div className="overflow-x-auto rounded-lg border border-border/20">
            <table className="w-full text-caption">{children}</table>
          </div>
        ),
        th: ({ children }) => (
          <th className="px-3 py-2 text-left text-micro font-semibold bg-muted/20 border-b border-border/20">
            {children}
          </th>
        ),
        td: ({ children }) => (
          <td className="px-3 py-2 border-b border-border/10">{children}</td>
        ),
        a: ({ href, children }) => (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:underline underline-offset-2"
          >
            {children}
          </a>
        ),
        blockquote: ({ children }) => (
          <blockquote className="border-l-2 border-primary/30 pl-4 text-muted-foreground italic">
            {children}
          </blockquote>
        ),
        hr: () => <hr className="border-border/30 my-6" />,
      }}
    >
      {processedContent}
    </ReactMarkdown>
  );
}

/**
 * Process React children to find {{cite:N}} markers and render them as
 * interactive citation buttons. Only processes string children.
 */
function processCitations(
  children: React.ReactNode,
  onCitationClick?: (index: number) => void,
  onCitationHover?: (index: number | null) => void,
): React.ReactNode {
  if (!children) return children;

  const childArray = Array.isArray(children) ? children : [children];

  return childArray.map((child, i) => {
    if (typeof child !== "string") return child;

    const parts = child.split(/(\{\{cite:\d+\}\})/g);
    if (parts.length === 1) return child;

    return parts.map((part, j) => {
      const match = part.match(/^\{\{cite:(\d+)\}\}$/);
      if (!match) return part;

      const citationIndex = parseInt(match[1], 10);
      return (
        <button
          key={`${i}-${j}`}
          onClick={() => onCitationClick?.(citationIndex)}
          onMouseEnter={() => onCitationHover?.(citationIndex)}
          onMouseLeave={() => onCitationHover?.(null)}
          className="inline-flex items-center justify-center h-5 min-w-5 px-1 rounded-md bg-primary/10 text-primary text-[13px] font-semibold tabular-nums cursor-pointer hover:bg-primary/20 transition-colors align-baseline mx-0.5"
        >
          {citationIndex}
        </button>
      );
    });
  });
}
