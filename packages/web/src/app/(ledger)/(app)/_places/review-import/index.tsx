"use client";

import { useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Icon,
  PageHeader,
  Segmented,
  StateMark,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, joinList } from "@/lib/v2/copy";
import {
  bulkMemories,
  importFileOf,
  openConflicts,
  sourceGroups,
  waitingMemories,
  type ImportMemoryView,
  type ImportView,
} from "@/lib/v2/data/imports";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { setupHref } from "@/lib/v2/onboarding/routes";
import { cliCommand } from "@/lib/v2/cli";
import { EmptyState } from "../../_components/empty-state";
import { StatementText } from "../../_components/statement-text";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { recordKeys } from "../../_lib/records";
import {
  importKeys,
  useImports,
  useImportView,
} from "../../../_onboarding/queries";
import { shortRef } from "../../../_onboarding/cleanup";
import { useRecordsView, type RecordsView } from "../records-view";
import styles from "./review-import.module.css";

/** Rows shown before "Show N more". */
const FIRST_ROWS = 12;

/**
 * ReviewImport (ReviewImport.png) at /[space]/review?filter=import: what
 * `memax init` imported, kept or rejected in bulk. Only the proposals
 * the server marks `bulk` can be selected (from the person's own files or
 * the repository, judged, in no disagreement, no hidden characters);
 * the rest say why they wait for one-by-one review, and a disagreement
 * links to the cleanup. Keep (K) and Reject (X) act on the selection,
 * each memory by Keep's rules, with one idempotency key per action.
 */
export function ReviewImportPlace() {
  const view = useRecordsView();
  const params = useSearchParams();
  const asked = params?.get("import") ?? null;
  const imports = useImports(view.space);
  const id = asked ?? imports.data?.[0]?.id ?? null;
  const importView = useImportView(view.space, id);
  const r = view.l.onboarding.reviewImport;

  if (importView.data) {
    return <ReviewImport view={view} data={importView.data} />;
  }
  const loading = imports.isPending || (Boolean(id) && importView.isPending);
  if (loading) {
    return (
      <div className={`mx-page ${styles.page}`}>
        <p className="mx-sr" role="status">
          {view.l.onboarding.frame.loading}
        </p>
      </div>
    );
  }
  return (
    <div className={`mx-page ${styles.page}`}>
      <h1 className="mx-sr">{r.noneTitle}</h1>
      <EmptyState title={r.noneTitle} detail={r.noneDetail} />
    </div>
  );
}

function fileLabel(
  r: RecordsView["l"]["onboarding"]["reviewImport"],
  key: string,
): string {
  return (r.labels as Record<string, string>)[key] ?? r.labels.other;
}

function ReviewImport({ view, data }: { view: RecordsView; data: ImportView }) {
  const { space, l, locale, timeZone, agentName } = view;
  const r = l.onboarding.reviewImport;
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const keys = useRef(new IntentKeys());
  const keepKey = useKeycap("review.keep");
  const rejectKey = useKeycap("review.reject");
  const [group, setGroup] = useState("all");
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [expanded, setExpanded] = useState(false);

  // The person's own V1 memories, offered when the space switched to V2
  // (ReviewImport "From V1"): each cites its note (N-), not a file.
  const fromV1 = data.summary.origin === "v1";
  const groups = sourceGroups(data);
  const filesOf = (key: string) =>
    key === "all" ? null : (groups.find((g) => g.key === key)?.files ?? null);
  const inGroup = (m: ImportMemoryView) => {
    const files = filesOf(group);
    return !files || m.files.some((f) => importFileOf(f, files) !== null);
  };
  const waiting = waitingMemories(data).filter(inGroup);
  const bulk = bulkMemories(data).filter(inGroup);
  const held = waiting.filter((m) => !m.bulk);
  const rows = [...bulk, ...held];
  const shown = expanded ? rows : rows.slice(0, FIRST_ROWS);
  const picked = rows.filter((m) => selected.has(m.ref));
  const allPicked = bulk.length > 0 && bulk.every((m) => selected.has(m.ref));
  const conflictsLeft = openConflicts(data).length;
  const heldForReview = waitingMemories(data).filter(
    (m) => !m.bulk && m.held !== "conflict",
  ).length;
  const fileCount = data.summary.files.filter((f) => f.statements > 0).length;

  const decide = useMutation({
    mutationFn: async (to: "keep" | "reject") => {
      const items = picked.map((m) => ({ ref: m.ref, version: m.version }));
      const intent = `${to}:${data.summary.id}:${items
        .map((i) => `${i.ref}@${i.version}`)
        .join(",")}`;
      const idempotencyKey = keys.current.keyFor(intent);
      const run = to === "keep" ? source.imports.keep : source.imports.reject;
      const outcome = await run({ space, items, idempotencyKey });
      keys.current.settle(intent);
      return { to, outcome };
    },
    onSuccess: ({ to, outcome }) => {
      setSelected(new Set());
      const n = outcome.applied.length;
      const text =
        to === "keep"
          ? count(r.keptToastOne, r.keptToast, n)
          : count(r.rejectedToastOne, r.rejectedToast, n);
      toast({ state: to === "keep" ? "kept" : undefined, text });
      if (outcome.refused.length > 0) {
        toast({
          state: "proposed",
          text: interpolate(r.refusedToast, { n: outcome.refused.length }),
        });
      }
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
      void queryClient.invalidateQueries({
        queryKey: importKeys.view(source.kind, space.slug, data.summary.id),
      });
    },
    onError: () => toast({ state: "proposed", text: r.failedToast }),
  });
  const act = (to: "keep" | "reject") => {
    if (picked.length === 0 || decide.isPending) return;
    decide.mutate(to);
  };
  useHotkey("review.keep", () => act("keep"), { enabled: picked.length > 0 });
  useHotkey("review.reject", () => act("reject"), {
    enabled: picked.length > 0,
  });

  const toggle = (ref: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });
  const toggleAll = () =>
    setSelected(allPicked ? new Set() : new Set(bulk.map((m) => m.ref)));

  const options = groups.map((g) => ({
    value: g.key,
    label:
      g.key === "all"
        ? interpolate(r.all, { n: g.count })
        : interpolate(r.group, { label: fileLabel(r, g.key), n: g.count }),
  }));
  const pickedFiles = useMemo(() => {
    const kinds: string[] = [];
    if (fromV1) return kinds;
    for (const m of picked) {
      for (const f of m.files) {
        const path = importFileOf(
          f,
          data.summary.files.map((x) => x.path),
        );
        const kind = data.summary.files.find((x) => x.path === path)?.kind;
        const label =
          kind === "codex_memory"
            ? r.codexMemory
            : kind
              ? fileLabel(r, kind)
              : f;
        if (!kinds.includes(label)) kinds.push(label);
      }
    }
    return kinds;
  }, [picked, data, r, fromV1]);

  const client = data.summary.client?.startsWith("memax-cli")
    ? cliCommand("init")
    : (data.summary.client ?? cliCommand("init"));
  const when = interpolate(l.app.time.on, {
    date: new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
      timeZone,
      month: "short",
      day: "numeric",
    }).format(new Date(data.summary.createdAt)),
    time: new Intl.DateTimeFormat("en-GB", {
      timeZone,
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(new Date(data.summary.createdAt)),
  });

  if (waitingMemories(data).length === 0) {
    const doneTitle = fromV1 ? r.v1.doneTitle : r.doneTitle;
    return (
      <div className={`mx-page ${styles.page}`}>
        <h1 className="mx-sr">{doneTitle}</h1>
        <EmptyState
          title={doneTitle}
          detail={fromV1 ? r.v1.doneDetail : r.doneDetail}
          action={
            <Button
              variant="primary"
              href={
                fromV1
                  ? placeHref(space.slug, "today")
                  : setupHref("done", { space: space.slug })
              }
            >
              {fromV1 ? r.v1.openToday : r.seeFiles}
            </Button>
          }
        />
      </div>
    );
  }
  const proposed = data.summary.counts.proposed;
  const title = fromV1
    ? interpolate(proposed === 1 ? r.v1.titleOne : r.v1.title, {
        n: proposed,
      })
    : interpolate(proposed === 1 ? r.titleOne : r.title, {
        n: proposed,
        files: fileCount,
      });

  return (
    <div className={styles.frame}>
      <div className={styles.scroll}>
        <div className={`mx-page ${styles.page}`}>
          <PageHeader
            className={styles.head}
            eyebrow={
              fromV1
                ? interpolate(r.v1.eyebrow, { when })
                : interpolate(r.eyebrow, { client, when })
            }
            title={title}
            lede={fromV1 ? r.v1.lede : r.lede}
            actions={
              <Button href={placeHref(space.slug, "review")}>
                {r.oneByOne}
              </Button>
            }
          />
          <div className={styles.filters}>
            {options.length > 1 ? (
              <Segmented
                size="sm"
                label={r.filterLabel}
                options={options}
                value={group}
                onChange={(v) => {
                  setGroup(v);
                  setExpanded(false);
                }}
              />
            ) : null}
            <span className={styles.grow} />
            <span className="mx-meta">
              {interpolate(r.left, {
                conflicts: count(
                  r.conflictLeft,
                  r.conflictsLeft,
                  conflictsLeft,
                ),
                held: interpolate(r.heldForReview, { n: heldForReview }),
              })}
            </span>
          </div>
          <section className="mx-panel" aria-label={r.filterLabel}>
            <div className={styles.headRow}>
              <input
                type="checkbox"
                checked={allPicked}
                disabled={bulk.length === 0}
                onChange={toggleAll}
                aria-label={count(r.selectAllOne, r.selectAll, bulk.length)}
              />
              <span className={styles.headLabel}>
                {count(r.selectAllOne, r.selectAll, bulk.length)}
              </span>
              <span className="mx-meta">{fromV1 ? r.v1.sorted : r.sorted}</span>
            </div>
            {shown.map((m, i) => (
              <ImportRow
                key={m.ref}
                memory={m}
                data={data}
                view={view}
                selected={selected.has(m.ref)}
                onToggle={() => toggle(m.ref)}
                last={i === shown.length - 1}
                agentName={agentName}
              />
            ))}
          </section>
          {rows.length > shown.length ? (
            <div className={styles.more}>
              <Button
                variant="quiet"
                size="sm"
                icon="chevron-down"
                onClick={() => setExpanded(true)}
              >
                {interpolate(r.showMore, { n: rows.length - shown.length })}
              </Button>
            </div>
          ) : null}
        </div>
      </div>
      <div className={styles.bar}>
        <span className={styles.count}>
          {interpolate(r.selected, { n: picked.length })}
        </span>
        {pickedFiles.length > 0 ? (
          <span className="mx-meta">
            {interpolate(r.selectedFrom, {
              files: joinList(pickedFiles, locale),
            })}
          </span>
        ) : null}
        <span className={styles.grow} />
        <Button
          variant="quiet"
          size="sm"
          disabled={picked.length === 0}
          onClick={() => setSelected(new Set())}
        >
          {r.clear}
        </Button>
        <Button
          size="sm"
          kbd={rejectKey}
          disabled={picked.length === 0}
          pending={decide.isPending && decide.variables === "reject"}
          onClick={() => act("reject")}
        >
          {interpolate(r.rejectN, { n: picked.length })}
        </Button>
        <Button
          variant="keep"
          size="sm"
          kbd={keepKey}
          disabled={picked.length === 0}
          pending={decide.isPending && decide.variables === "keep"}
          onClick={() => act("keep")}
        >
          {interpolate(r.keepN, { n: picked.length })}
        </Button>
      </div>
    </div>
  );
}

function ImportRow({
  memory,
  data,
  view,
  selected,
  onToggle,
  last,
  agentName,
}: {
  memory: ImportMemoryView;
  data: ImportView;
  view: RecordsView;
  selected: boolean;
  onToggle: () => void;
  last: boolean;
  agentName: (agent: string) => string;
}) {
  const r = view.l.onboarding.reviewImport;
  const note = rowNote(r, memory, data, agentName, view.space.slug);
  const className = `${styles.row} ${
    memory.bulk ? (selected ? styles.on : "") : styles.held
  } ${last ? styles.last : ""}`;
  const text = (
    <span className={styles.text}>
      <StatementText text={memory.statement} />
      {note ? (
        <span
          className={`${styles.note} ${
            memory.held === "conflict" ? styles.vermilion : ""
          }`}
        >
          {note}
        </span>
      ) : null}
    </span>
  );
  const sources = (
    <span className={styles.sources}>
      {memory.refs.map((ref) => (
        <span key={ref}>{shortRef(ref)}</span>
      ))}
      {memory.external ? (
        <span>{interpolate(r.from, { host: memory.external })}</span>
      ) : null}
    </span>
  );
  if (!memory.bulk) {
    return (
      <div className={className}>
        <input type="checkbox" disabled aria-label={r.cannot} />
        <span className={styles.mark}>
          {memory.held === "conflict" ? (
            <StateMark state="conflict" label={false} />
          ) : memory.held === "quarantined" ? (
            <span className={styles.shield}>
              <Icon name="shield" size={14} />
            </span>
          ) : (
            <StateMark state="proposed" label={false} />
          )}
        </span>
        {text}
        {sources}
      </div>
    );
  }
  return (
    <label className={className}>
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggle}
        aria-label={r.select}
      />
      <span />
      {text}
      {sources}
    </label>
  );
}

/** The line under a row: how many files agree, or why it waits. */
function rowNote(
  r: RecordsView["l"]["onboarding"]["reviewImport"],
  memory: ImportMemoryView,
  data: ImportView,
  agentName: (agent: string) => string,
  space: string,
) {
  if (memory.bulk) {
    if (data.summary.origin === "v1") {
      return memory.refs.length > 1
        ? interpolate(r.v1.repeated, { n: memory.refs.length })
        : null;
    }
    const files = memory.files.length;
    if (files >= 3) return interpolate(r.agreeMany, { n: files });
    if (files === 2) return r.agreeTwo;
    return null;
  }
  switch (memory.held) {
    case "conflict": {
      const conflict = data.conflicts.find((c) => c.n === memory.conflict);
      const others = (conflict?.members ?? [])
        .filter((m) => m.ref !== memory.ref)
        .map((m) => shortRef(m.refs[0] ?? m.ref));
      return (
        <Link
          href={setupHref("cleanup", { space, import: data.summary.id })}
          className={styles.inlineLink}
        >
          {interpolate(r.held.conflict, { refs: others.join(", ") })}
        </Link>
      );
    }
    case "quarantined":
      return memory.agent
        ? interpolate(r.held.quarantined, { agent: agentName(memory.agent) })
        : r.held.quarantinedNoAgent;
    case "checking":
    case "hidden_characters":
    case "unchecked":
    case "stale":
    case "decided":
      return r.held[memory.held];
    default:
      return null;
  }
}
