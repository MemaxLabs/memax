"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AgentStamp, Button, Icon, Kbd, StateMark } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count, joinList } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import {
  openConflicts,
  skippedSecrets,
  waitingMemories,
  type ImportConflictView,
  type ImportView,
  type SettleChoice,
} from "@/lib/v2/data/imports";
import type { TargetView } from "@/lib/v2/data/targets";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { reviewImportHref } from "@/lib/v2/onboarding/routes";
import { useSource } from "../(app)/_lib/data";
import { useFunnelStep } from "./funnel";
import { useTargets } from "../(app)/_lib/compile";
import { recordKeys } from "../(app)/_lib/records";
import { StatementText } from "../(app)/_components/statement-text";
import { OnboardingPage } from "./frame";
import { importKeys, useSetupImport, useSetupSpace } from "./queries";
import styles from "./cleanup.module.css";

type Copy = ReturnType<
  typeof useLocale
>["t"]["ledger"]["onboarding"]["cleanup"];

/** One choice in a disagreement, in the order it's numbered. */
export interface CleanupOption {
  choice: SettleChoice;
  statement: string;
  /** Where it was said; "suggested" for the check's own words; none for leaving it open. */
  source: { ref: string; agent: string | null } | "suggested" | null;
}

/** A disagreement's numbered choices: each member, the suggestion, then leaving it open. */
export function cleanupOptions(
  conflict: ImportConflictView,
  leaveOpen: string,
): CleanupOption[] {
  const options: CleanupOption[] = conflict.members.map((m) => ({
    choice: { choice: "keep_one", keep: m.ref },
    statement: m.statement,
    source: { ref: m.refs[0] ?? m.ref, agent: m.agent },
  }));
  if (conflict.suggestion) {
    options.push({
      choice: { choice: "keep_suggestion" },
      statement: conflict.suggestion,
      source: "suggested",
    });
  }
  options.push({
    choice: { choice: "leave_open" },
    statement: leaveOpen,
    source: null,
  });
  return options;
}

/**
 * Cleanup (Cleanup.png, step 2 of 3) at /setup/cleanup: the disagreements
 * the import's check found among what the agents' files say, each
 * settled once, as a group, through `conflicts/{n}:settle`: keep one
 * statement, the check's suggestion, or leave it open (and, when the
 * check was wrong, keep them all). The web is the person on the web
 * (human_web), so decisions in a team space settle here, where the CLI
 * would be refused. Beside it: the files read, what init already
 * handled (folded repeats, secrets kept on the machine), and the files
 * Memax writes once the proposals are reviewed.
 */
export function CleanupScreen() {
  const { t, locale } = useLocale();
  const copy = t.ledger.onboarding.cleanup;
  const frame = t.ledger.onboarding.frame;
  const router = useRouter();
  const setup = useSetupSpace();
  const space = setup.space ?? null;
  const { view: viewQuery } = useSetupImport(space);
  const view = viewQuery.data ?? null;
  const reviewKey = useKeycap("cleanup.review");
  const waiting = view ? waitingMemories(view).length : 0;
  const disagreements = view?.conflicts.length ?? 0;
  useFunnelStep(
    "cleanup_settled",
    view !== null && disagreements > 0 && openConflicts(view).length === 0,
    { conflicts: disagreements },
  );
  const reviewHref =
    space && view ? reviewImportHref(space.slug, view.summary.id) : null;
  useHotkey(
    "cleanup.review",
    () => {
      if (reviewHref) router.push(reviewHref);
    },
    {
      enabled: Boolean(reviewHref),
    },
  );

  const meta = (
    <>
      <span>{interpolate(frame.step, { n: 2 })}</span>
      {space ? (
        <Button
          variant="quiet"
          size="sm"
          href={`/${encodeURIComponent(space.slug)}/today`}
        >
          {frame.finishLater}
        </Button>
      ) : null}
    </>
  );

  if (!space || !view) {
    const loading = setup.loading || (space !== null && viewQuery.isPending);
    return (
      <OnboardingPage meta={meta}>
        {loading ? (
          <p className="mx-sr" role="status">
            {frame.loading}
          </p>
        ) : (
          <section className={`mx-panel ${styles.none}`}>
            <h1 className={styles.noneTitle}>{copy.none.title}</h1>
            <p className="mx-meta">{copy.none.detail}</p>
          </section>
        )}
      </OnboardingPage>
    );
  }

  const files = view.summary.files.filter((f) => f.statements > 0);
  const total = view.conflicts.length;
  const open = openConflicts(view).length;
  const conflicts = [...view.conflicts].sort((a, b) => a.n - b.n);
  const active = conflicts.find((c) => c.state === "open")?.n ?? null;

  return (
    <OnboardingPage meta={meta}>
      <div className={styles.layout}>
        <section className={styles.main}>
          <div>
            <p className="mx-page-eyebrow">
              {interpolate(copy.eyebrow, { space: space.slug })}
            </p>
            <h1 className={styles.title}>
              {interpolate(total === 1 ? copy.titleOne : copy.title, {
                files: files.length,
                conflicts: total,
              })}
            </h1>
            <p className={styles.lede}>
              {open === 0
                ? copy.ledeNone
                : open === 1
                  ? copy.ledeOne
                  : interpolate(copy.lede, {
                      n: t.ledger.app.numbers[open] ?? String(open),
                    })}
            </p>
          </div>
          {conflicts.map((conflict) => (
            <ConflictCard
              key={conflict.id}
              copy={copy}
              conflict={conflict}
              total={total}
              space={space}
              view={view}
              active={conflict.n === active}
            />
          ))}
        </section>
        <aside className={styles.side}>
          <FoundPanel copy={copy} view={view} />
          <HandledPanel copy={copy} view={view} locale={locale} />
          <WritesPanel copy={copy} space={space} view={view} />
          <div className={styles.review}>
            {reviewHref ? (
              <Button
                variant="primary"
                size="lg"
                kbd={reviewKey}
                href={reviewHref}
                className={styles.wide}
              >
                {count(copy.reviewOne, copy.review, waiting)}
              </Button>
            ) : null}
            <span className="mx-meta">{copy.nothingYet}</span>
          </div>
        </aside>
      </div>
    </OnboardingPage>
  );
}

function ConflictCard({
  copy,
  conflict,
  total,
  space,
  view,
  active,
}: {
  copy: Copy;
  conflict: ImportConflictView;
  total: number;
  space: SpaceSummary;
  view: ImportView;
  active: boolean;
}) {
  const source = useSource();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const keys = useRef(new IntentKeys());
  const keepKey = useKeycap("cleanup.keep");
  const [chosen, setChosen] = useState<number | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const options = cleanupOptions(conflict, copy.leaveOpen);
  const settle = useMutation({
    mutationFn: async (choice: SettleChoice) => {
      const intent = `settle:${view.summary.id}:${conflict.n}:${JSON.stringify(choice)}`;
      const idempotencyKey = keys.current.keyFor(intent);
      const result = await source.imports.settle({
        space,
        importId: view.summary.id,
        n: conflict.n,
        choice,
        idempotencyKey,
      });
      keys.current.settle(intent);
      return result;
    },
    onSuccess: () => {
      setRefusal(null);
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
      void queryClient.invalidateQueries({
        queryKey: importKeys.view(source.kind, space.slug, view.summary.id),
      });
    },
    onError: (err) => {
      const failure = toFailure(err);
      const code =
        failure.kind === "refused"
          ? failure.code
          : failure.kind === "in-conflict"
            ? "in_conflict"
            : null;
      setRefusal(
        (code && (copy.refused as Record<string, string>)[code]) ||
          copy.refused.other,
      );
    },
  });
  const keep = () => {
    if (chosen === null || settle.isPending) return;
    settle.mutate(options[chosen]!.choice);
  };
  const open = conflict.state === "open";
  useHotkey(
    "cleanup.choose",
    (_event, match) => {
      if (match.index < options.length) setChosen(match.index);
    },
    { enabled: open && active },
  );
  useHotkey("cleanup.keep", keep, {
    enabled: open && active && chosen !== null,
  });

  const files = new Set(
    conflict.members.flatMap((m) => m.refs.map((r) => r.replace(/:\d+$/, ""))),
  ).size;
  const title = conflict.subject ?? copy.untitled;
  const head = (
    <div className={styles.cardHead}>
      <StateMark
        state={open ? "conflict" : "kept"}
        label={interpolate(copy.ofN, { n: conflict.n, total })}
      />
      <span className={styles.topic}>{title}</span>
      <span className={`mx-meta ${styles.right}`}>
        {interpolate(copy.filesDisagree, { n: Math.max(files, 2) })}
      </span>
    </div>
  );

  if (!open) {
    const kept =
      conflict.choice === "keep_all"
        ? copy.settled.all
        : conflict.choice === "leave_open"
          ? copy.settled.open
          : interpolate(
              conflict.choice === "keep_suggestion"
                ? copy.settled.suggestion
                : copy.settled.one,
              { ref: conflict.chosen ?? "" },
            );
    return (
      <article className={`${styles.card} ${styles.settled}`}>
        {head}
        <p className={styles.settledLine}>{kept}</p>
      </article>
    );
  }

  const choice = chosen === null ? null : options[chosen]!;
  const keys1 = options.map((_, i) => String(i + 1));
  const footer =
    choice === null
      ? interpolate(copy.chooseWith, {
          keys: joinOr(keys1, copy.or, locale),
        })
      : choice.choice.choice === "leave_open"
        ? copy.willLeaveOpen
        : choice.choice.choice === "keep_suggestion"
          ? copy.willKeepSuggestion
          : copy.willKeepOne;

  return (
    <article className={styles.card} aria-labelledby={`conflict-${conflict.n}`}>
      <span className="mx-sr" id={`conflict-${conflict.n}`}>
        {title}
      </span>
      {head}
      <div className={styles.options} role="radiogroup" aria-label={title}>
        {options.map((option, i) => (
          <button
            key={i}
            type="button"
            role="radio"
            aria-checked={chosen === i}
            className={styles.option}
            onClick={() => setChosen(i)}
          >
            <Kbd>{i + 1}</Kbd>
            <span
              className={`${styles.optionText} ${
                option.source === null ? styles.quiet : ""
              }`}
            >
              <StatementText text={option.statement} />
            </span>
            <span className={styles.optionSource}>
              {option.source === "suggested" ? (
                copy.suggested
              ) : option.source ? (
                <>
                  {option.source.agent ? (
                    <AgentStamp agent={option.source.agent} size="sm" />
                  ) : null}
                  {shortRef(option.source.ref)}
                </>
              ) : null}
            </span>
          </button>
        ))}
      </div>
      {refusal ? (
        <p className={styles.refusal} role="alert">
          {refusal}
        </p>
      ) : null}
      <div className={styles.cardFoot}>
        <span className="mx-meta">{footer}</span>
        <Button
          variant="quiet"
          size="sm"
          disabled={settle.isPending}
          onClick={() => settle.mutate({ choice: "keep_all" })}
        >
          {copy.allHold}
        </Button>
        <span className={styles.right}>
          <Button
            variant="keep"
            size="sm"
            kbd={keepKey}
            disabled={chosen === null}
            pending={settle.isPending}
            onClick={keep}
          >
            {chosen === null
              ? copy.keep
              : interpolate(copy.keepN, { n: chosen + 1 })}
          </Button>
        </span>
      </div>
    </article>
  );
}

/** "1, 2 or 3" / "1、2 或 3". */
export function joinOr(items: string[], or: string, locale: string): string {
  if (items.length < 2) return items.join("");
  const head = items.slice(0, -1).join(locale === "zh" ? "、" : ", ");
  return `${head} ${or} ${items.at(-1)}`;
}

/** A ref as the cleanup shows it: the file's own name and the line. */
export function shortRef(ref: string): string {
  const m = /^(.*?)(:\d+)?$/.exec(ref);
  const path = m?.[1] ?? ref;
  const base = path.split("/").at(-1) ?? path;
  return `${base}${m?.[2] ?? ""}`;
}

function FoundPanel({ copy, view }: { copy: Copy; view: ImportView }) {
  const files = view.summary.files.filter((f) => f.statements > 0);
  return (
    <section className="mx-panel" aria-labelledby="found-title">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="found-title">
          {copy.found}
        </h2>
        <span className="mx-meta">
          {interpolate(copy.filesCount, { n: files.length })}
        </span>
      </header>
      {files.map((f) => (
        <div key={f.path} className={styles.fileRow}>
          <code>{f.path}</code>
          {f.agent ? <AgentStamp agent={f.agent} size="sm" /> : <span />}
          <span className={styles.n}>{f.statements}</span>
        </div>
      ))}
      <div className={styles.totals}>
        <span>
          {interpolate(copy.statements, { n: view.summary.counts.items })}
        </span>
        <span>
          {interpolate(copy.proposals, { n: view.summary.counts.proposed })}
        </span>
      </div>
    </section>
  );
}

function HandledPanel({
  copy,
  view,
  locale,
}: {
  copy: Copy;
  view: ImportView;
  locale: "en" | "zh";
}) {
  const secrets = skippedSecrets(view.summary);
  const folded = view.summary.counts.folded;
  if (folded === 0 && secrets.length === 0) return null;
  return (
    <section className="mx-panel" aria-labelledby="handled-title">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="handled-title">
          {copy.handled}
        </h2>
      </header>
      {folded > 0 ? (
        <div className={styles.handled}>
          <span className={styles.handledMark}>
            <StateMark state="merged" label={false} />
          </span>
          <div>
            <strong>{interpolate(copy.folded, { n: folded })}</strong>
            <span>{copy.foldedDetail}</span>
          </div>
        </div>
      ) : null}
      {secrets.length > 0 ? (
        <div className={styles.handled}>
          <span className={styles.handledIcon}>
            <Icon name="shield" />
          </span>
          <div>
            <strong>
              {count(copy.secretOne, copy.secrets, secrets.length)}
            </strong>
            <span>
              {interpolate(copy.secretDetail, {
                list: joinList(
                  secrets.map((s) =>
                    s.detail
                      ? interpolate(copy.secretItem, {
                          ref: s.ref,
                          rule: s.detail,
                        })
                      : s.ref,
                  ),
                  locale,
                ),
              })}
            </span>
          </div>
        </div>
      ) : null}
    </section>
  );
}

/** What Memax writes once the proposals are reviewed: the space's targets, or the ones init adds. */
export function plannedWrites(
  targets: readonly TargetView[],
  view: ImportView,
): Array<{ label: string; word: "rewritten" | "beside" | "new"; n?: number }> {
  const read = new Set(view.summary.files.map((f) => f.path));
  const rules = view.summary.files.filter(
    (f) => f.kind === "cursor_rule",
  ).length;
  const list =
    targets.length > 0
      ? targets
          .filter((t) => t.syncState !== "off")
          .map((t) => ({ label: t.label, kind: t.kind, path: t.path }))
      : [
          { label: "AGENTS.md", kind: "agents_md", path: "AGENTS.md" },
          { label: "CLAUDE.md", kind: "claude_md", path: "CLAUDE.md" },
          { label: ".cursor/rules", kind: "cursor_mdc", path: ".cursor/rules" },
          { label: "ChatGPT project", kind: "chatgpt", path: null },
        ];
  return list.map((t) => {
    if (t.kind === "cursor_mdc" && rules > 0) {
      return { label: t.label, word: "beside", n: rules };
    }
    if (t.path && read.has(t.path))
      return { label: t.label, word: "rewritten" };
    return { label: t.label, word: "new" };
  });
}

function WritesPanel({
  copy,
  space,
  view,
}: {
  copy: Copy;
  space: SpaceSummary;
  view: ImportView;
}) {
  const targets = useTargets(space).data ?? [];
  const writes = plannedWrites(targets, view);
  return (
    <section className="mx-panel" aria-labelledby="writes-title">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="writes-title">
          {copy.writes}
        </h2>
      </header>
      {writes.map((w) => (
        <div key={w.label} className={`${styles.fileRow} ${styles.writeRow}`}>
          <code>{w.label}</code>
          <span className="mx-meta">
            {w.word === "rewritten"
              ? copy.rewritten
              : w.word === "beside"
                ? interpolate(copy.beside, { n: w.n ?? 0 })
                : copy.newFile}
          </span>
        </div>
      ))}
    </section>
  );
}
