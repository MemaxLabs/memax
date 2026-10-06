"use client";

import { Fragment } from "react";
import Link from "next/link";
import {
  AgentStamp,
  Button,
  Cite,
  Highlight,
  StateMark,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { marginLine } from "@/lib/v2/brief-copy";
import { blocksOf, type BriefRow, type BriefView } from "@/lib/v2/data/brief";
import { placeHref } from "@/lib/v2/places";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import styles from "./brief.module.css";

const MARK = {
  stale: "stale",
  conflict: "conflict",
  proposed: "proposed",
} as const;

/** A fact's words, with the span the latest version changed highlighted. */
function Words({ text, changed }: { text: string; changed: string | null }) {
  const at = changed ? text.indexOf(changed) : -1;
  if (!changed || at < 0) return <StatementText text={text} />;
  return (
    <>
      <StatementText text={text.slice(0, at)} />
      <Highlight>
        <StatementText text={changed} />
      </Highlight>
      <StatementText text={text.slice(at + changed.length)} />
    </>
  );
}

/**
 * The Brief's sections in Brief order (Brief.png), each fact set in the
 * serif with its [n] cite and its receipt in the ruled margin: kept
 * facts roman, waiting ones italic, stale ones dotted with Verify.
 */
export function BriefLedger({
  view,
  brief,
  numbers,
}: {
  view: RecordsView;
  brief: BriefView;
  /** The [n] of each cited memory (numberSources). */
  numbers: ReadonlyMap<string, number>;
}) {
  return (
    <div className={styles.ledger}>
      {brief.sections.map((section) => (
        <section key={section.key} className={styles.section}>
          <h2 className={styles.heading}>{section.heading}</h2>
          {blocksOf(section.rows).map((block) =>
            block.kind === "row" ? (
              <Fact
                key={block.row.key}
                view={view}
                rows={[block.row]}
                numbers={numbers}
              />
            ) : (
              <Fact
                key={block.rows[0]!.key}
                view={view}
                rows={block.rows}
                numbers={numbers}
              />
            ),
          )}
        </section>
      ))}
    </div>
  );
}

function Fact({
  view,
  rows,
  numbers,
}: {
  view: RecordsView;
  /** One row, or the lines of one paragraph of prose. */
  rows: BriefRow[];
  numbers: ReadonlyMap<string, number>;
}) {
  const { l, space } = view;
  const b = l.brief;
  const first = rows[0]!;
  const memoryHref = (ref: string) =>
    `${placeHref(space.slug, "memories")}/${encodeURIComponent(ref)}`;
  const cite = (ref: string) => {
    const n = numbers.get(ref);
    return n ? (
      <Cite key={ref} n={n} title={ref} href={memoryHref(ref)} />
    ) : null;
  };
  const state = first.kind === "waiting" ? "proposed" : first.state;
  const stamp = view.stamp(first.receipt?.by ?? null);
  const verb = first.receipt
    ? first.receipt.action === "wrote"
      ? b.page.wrote
      : l.records.verbs[first.receipt.action]
    : null;
  const line = marginLine(b, first);

  return (
    <div
      className={`${styles.fact} ${
        state === "stale"
          ? styles.stale
          : state === "conflict"
            ? styles.conflict
            : state === "proposed"
              ? styles.waiting
              : ""
      }`}
      data-ref={first.ref ?? undefined}
    >
      <span className={styles.mark}>
        {state !== "kept" ? (
          <StateMark state={MARK[state]} label={false} />
        ) : null}
      </span>
      <p className={styles.text}>
        {rows.map((row, i) => (
          <Fragment key={row.key}>
            {i > 0 ? " " : null}
            <span className={styles.words}>
              <Words text={row.text} changed={row.changed} />
            </span>
            {row.kind === "prose"
              ? row.cites.map(cite)
              : row.ref && row.state === "kept"
                ? cite(row.ref)
                : null}
          </Fragment>
        ))}
        {first.state === "stale" && first.kind === "memory" ? (
          <Button
            size="sm"
            variant="secondary"
            className={styles.verify}
            disabled
            disabledReason={b.page.verifyLater}
          >
            {b.page.verify}
          </Button>
        ) : null}
        {first.scope.length > 0 ? (
          <span className={styles.scope}>
            {interpolate(b.page.scope, { paths: "" })}
            {first.scope.map((path, i) => (
              <Fragment key={path}>
                {i > 0 ? ", " : null}
                <code>{path}</code>
              </Fragment>
            ))}
          </span>
        ) : null}
      </p>
      <div className={styles.margin}>
        {first.receipt ? (
          <span className="mx-receipt">
            {stamp ? <AgentStamp {...stamp} size="sm" /> : null}
            <span className="mx-receipt-action">{verb}</span>
            <span className="mx-receipt-dot" aria-hidden="true">
              ·
            </span>
            <span>{view.time(first.receipt.at)}</span>
          </span>
        ) : null}
        <span
          className={`mx-receipt ${styles.margin2} ${line.conflict ? styles.vs : ""}`}
        >
          {first.ref ? (
            <Link href={memoryHref(first.ref)}>{line.ids}</Link>
          ) : (
            line.ids
          )}
          {line.rest ? (
            first.kind === "waiting" ? (
              <>
                {" · "}
                <Link href={placeHref(space.slug, "review")}>{line.rest}</Link>
              </>
            ) : (
              ` · ${line.rest}`
            )
          ) : null}
        </span>
      </div>
    </div>
  );
}
