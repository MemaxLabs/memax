"use client";

import type { KeyboardEvent, Ref } from "react";
import Link from "next/link";
import { AgentStamp, Button, Icon, formatNodes } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { formatClock } from "@/lib/v2/copy";
import type { ActivityEntry } from "@/lib/v2/data/activity";
import { isGateRef } from "@/lib/v2/data/gates";
import { gateHref } from "@/lib/v2/places";
import {
  activitySentences,
  viaText,
  type Names,
} from "@/lib/v2/activity/sentence";
import styles from "./activity.module.css";

/**
 * Where Enter takes a row: a memory's page (or its tombstone), an agent's
 * page, or a decision gate's card in Review (asked, answered, withdrawn;
 * the card says how it ended). A gate is linked by its G- ref.
 */
export function entryHref(space: string, entry: ActivityEntry): string | null {
  const s = encodeURIComponent(space);
  if (entry.object.kind === "memory") {
    return `/${s}/memories/${encodeURIComponent(entry.object.ref)}`;
  }
  if (entry.object.kind === "agent") {
    return `/${s}/agents/${encodeURIComponent(entry.object.id)}`;
  }
  if (entry.object.kind === "gate" && isGateRef(entry.object.ref)) {
    return gateHref(space, entry.object.ref);
  }
  return null;
}

function Actor({ entry }: { entry: ActivityEntry }) {
  const a = entry.actor;
  switch (a.kind) {
    case "you":
      return <AgentStamp person={a.initials} size="sm" decorative />;
    case "person":
      return <AgentStamp person={a.initials ?? "—"} size="sm" decorative />;
    case "agent":
      return <AgentStamp agent={a.agent} size="sm" decorative />;
    case "dream":
      return <AgentStamp agent="dream" size="sm" decorative />;
    case "memax":
    case "repository":
      return (
        <span className={styles.sys} aria-hidden="true">
          <Icon name={a.kind === "memax" ? "sync" : "file"} size={12} />
        </span>
      );
  }
}

/**
 * One receipt: time, who, what happened, via what, and the object's ID
 * (Activity.dc.html's `sx-log`). A row of the log's roving focus: the
 * log moves focus between rows, and Enter opens what it points at.
 */
export function ActivityRow({
  entry,
  space,
  names,
  timeZone,
  focusable,
  onFocus,
  onKeyDown,
  onUnfold,
  ref,
}: {
  entry: ActivityEntry;
  space: string;
  names: Names;
  timeZone: string;
  focusable: boolean;
  onFocus: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLLIElement>, href: string | null) => void;
  /** One of the judge's folds, still undoable: its Undo. */
  onUnfold?: () => void;
  ref?: Ref<HTMLLIElement>;
}) {
  const { t, locale } = useLocale();
  const copy = t.ledger.activity;
  const href = entryHref(space, entry);
  const sentences = activitySentences(copy, entry, names);
  const full = new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
    timeZone,
    dateStyle: "full",
    timeStyle: "short",
  }).format(new Date(entry.at));
  const ref_ = entry.object.kind === "agent" ? null : entry.object.ref;
  return (
    <li
      ref={ref}
      className={href ? `${styles.row} ${styles.opens}` : styles.row}
      tabIndex={focusable ? 0 : -1}
      onFocus={onFocus}
      onKeyDown={(event) => onKeyDown(event, href)}
    >
      <time className={styles.time} dateTime={entry.at} title={full}>
        {formatClock(entry.at, timeZone, locale)}
      </time>
      <Actor entry={entry} />
      <span className={styles.what}>
        {sentences.map((s, i) => (
          <span key={i}>
            {i > 0 && locale !== "zh" ? " " : null}
            {formatNodes(
              s.template,
              Object.fromEntries(
                Object.entries(s.values).map(([key, token]) => [
                  key,
                  token.kind === "actor" ? (
                    <b>{token.text}</b>
                  ) : token.kind === "quote" ? (
                    <q>{token.text}</q>
                  ) : (
                    token.text
                  ),
                ]),
              ),
            )}
          </span>
        ))}
        {href ? <span className="mx-sr"> {copy.opens}</span> : null}
        {onUnfold ? (
          <>
            {" "}
            <Button
              className={styles.unfold}
              variant="quiet"
              size="sm"
              tabIndex={focusable ? 0 : -1}
              aria-label={interpolate(copy.unfoldFor, {
                ref: entry.object.ref,
              })}
              onClick={onUnfold}
            >
              {copy.unfold}
            </Button>
          </>
        ) : null}
      </span>
      <span className={styles.via}>{viaText(copy, entry.via, names)}</span>
      {ref_ && href ? (
        <Link
          className={`mx-receipt-id ${styles.ref}`}
          href={href}
          tabIndex={-1}
        >
          {ref_}
        </Link>
      ) : (
        <span className={`mx-receipt-id ${styles.ref}`}>{ref_}</span>
      )}
    </li>
  );
}
