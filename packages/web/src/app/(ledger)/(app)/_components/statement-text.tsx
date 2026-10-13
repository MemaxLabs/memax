import { Fragment } from "react";
import { Highlight } from "@memaxlabs/ledger";
import { highlightRuns } from "@/lib/v2/search-view";

/**
 * A statement's words, with `inline code` set as Ledger code
 * (Memories.png: "Never run `npm install` at the root."). Everything
 * else is plain text: statements are never parsed as markup. With
 * `query`, the words it matched wear the highlighter (Search.png).
 */
export function StatementText({
  text,
  query,
}: {
  text: string;
  query?: string;
}) {
  const parts = text.split(/(`[^`\n]+`)/g);
  return (
    <>
      {parts.map((part, i) =>
        part.length > 2 && part.startsWith("`") && part.endsWith("`") ? (
          <code key={i} className="mx-code">
            {part.slice(1, -1)}
          </code>
        ) : query ? (
          <Fragment key={i}>
            {highlightRuns(part, query).map((run, j) =>
              run.hl ? (
                <Highlight key={j}>{run.text}</Highlight>
              ) : (
                <Fragment key={j}>{run.text}</Fragment>
              ),
            )}
          </Fragment>
        ) : (
          <Fragment key={i}>{part}</Fragment>
        ),
      )}
    </>
  );
}
