import { Fragment } from "react";

/**
 * A statement's words, with `inline code` set as Ledger code
 * (Memories.png: "Never run `npm install` at the root."). Everything
 * else is plain text: statements are never parsed as markup.
 */
export function StatementText({ text }: { text: string }) {
  const parts = text.split(/(`[^`\n]+`)/g);
  return (
    <>
      {parts.map((part, i) =>
        part.length > 2 && part.startsWith("`") && part.endsWith("`") ? (
          <code key={i} className="mx-code">
            {part.slice(1, -1)}
          </code>
        ) : (
          <Fragment key={i}>{part}</Fragment>
        ),
      )}
    </>
  );
}
