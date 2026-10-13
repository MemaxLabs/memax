"use client";

import { Button, Icon, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { targetMeta } from "@/lib/v2/brief-copy";
import {
  findTarget,
  isScoped,
  targetName,
  TARGET_READERS,
  type CompiledFileView,
  type TargetPreviewView,
  type TargetView,
} from "@/lib/v2/data/targets";
import { isBlock, siblingsOf } from "@/lib/v2/data/home-files";
import { useHotkey } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { TargetList } from "../../_components/target-list";
import { TargetStatus, targetHref } from "../../_components/target-row";
import { useToast } from "../../_components/toasts";
import { StatusPage } from "../../../_components/status-page";
import { useBrief, useTargetPreview, useTargets } from "../../_lib/compile";
import { briefHref } from "../brief";
import { useCompile } from "../brief/use-compile";
import { useRecordsView, type RecordsView } from "../records-view";
import { CodeView } from "./code-view";
import {
  HomeStopped,
  homeLede,
  MachineSwitcher,
  useMachineOf,
  useStopHere,
} from "./home-target";
import { TargetSettings } from "./target-settings";
import { useTargetSettings } from "./use-target-settings";
import styles from "./target.module.css";

/**
 * A compiled file (TargetPreview.png, epic 1.5) at
 * /[space]/brief/targets/[target]: the exact content Memax writes, how
 * it's written (include, stale facts, size budget, delivery), the other
 * targets, Copy and Compile now (⌘⇧S). D2: AGENTS.md is canonical and
 * CLAUDE.md a shim that imports it, shown exactly as compiled; Cursor's
 * rule holds scoped facts only; ChatGPT is copy-out text.
 */
export function TargetPlace({ segment }: { segment: string }) {
  const view = useRecordsView();
  const { space, l, copy } = view;
  const targets = useTargets(space);
  const target = targets.data ? findTarget(targets.data, segment) : undefined;

  if (targets.data === undefined) {
    if (targets.isError) {
      return (
        <PlaceError space={space} onRetry={() => void targets.refetch()} />
      );
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={l.brief.target.settings.title}
          status={copy.empty.loadingMeta}
          label={interpolate(copy.empty.loading, { place: segment })}
        />
      </div>
    );
  }
  if (!target) {
    const nf = l.brief.target.notFound;
    return (
      <StatusPage
        variant="sheet"
        receipt={segment}
        title={interpolate(nf.title, { target: segment, space: space.name })}
        description={nf.detail}
        actions={
          <Button variant="secondary" size="sm" href={briefHref(space.slug)}>
            {nf.open}
          </Button>
        }
      />
    );
  }
  return <Target view={view} target={target} all={targets.data} />;
}

function Target({
  view,
  target,
  all,
}: {
  view: RecordsView;
  target: TargetView;
  all: TargetView[];
}) {
  const { space, l, now, timeZone, locale, agentName } = view;
  const t = l.brief.target;
  const preview = useTargetPreview(space, target);
  const brief = useBrief(space);
  const compile = useCompile(space);
  const toast = useToast();
  // A Personal space's home file is one machine's copy (home-target.tsx).
  const home = useMachineOf(view, target);
  const stopHere = useStopHere(view, target, home.name ?? "");
  const siblings = siblingsOf(target, all);
  // The other files: a home file's other machines are its switcher's.
  const others = all.filter((o) => !siblings.some((s) => s.id === o.id));
  const canCompile = space.role !== "viewer" && target.syncState !== "off";
  const tool = agentName(TARGET_READERS[target.kind][0] ?? target.kind);
  const name = targetName(target);
  const shown: TargetPreviewView | undefined = preview.data;
  const outputs = shown ? [...shown.files, ...shown.copies] : [];
  const noFile =
    isScoped(target.kind) && shown !== undefined && outputs.length === 0;

  const compileNow = () => void compile.run([target]);
  useHotkey("memory.compile", compileNow, { enabled: canCompile });

  const copyOut = async () => {
    const text = outputs.map((o) => o.content).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      toast({
        text:
          target.kind === "chatgpt"
            ? l.brief.toast.copiedChatgpt
            : interpolate(l.brief.toast.copied, { file: name }),
      });
    } catch {
      toast({ state: "proposed", text: l.brief.toast.copyFailed });
    }
  };

  // Open questions in the file: the Brief's open section, when it compiles.
  const open =
    target.settings.include === "kept_and_open" &&
    (target.kind === "agents_md" || target.kind === "chatgpt")
      ? (brief.data?.sections.find((s) => s.key === "open")?.rows.length ?? 0)
      : 0;
  const meta = targetMeta(
    l.brief,
    {
      target,
      compiledAt: shown?.compile?.at ?? target.lastCompile?.at ?? null,
      bytes: outputs.reduce((sum, o) => sum + o.bytes, 0),
      lines: outputs.reduce((sum, o) => sum + o.lines, 0),
      facts: new Set(outputs.flatMap((o) => o.refs)).size,
      open,
      dropped: new Set(outputs.flatMap((o) => o.dropped)).size,
    },
    { now, timeZone, locale },
  );
  const where = space.repository ?? space.name;
  const lede = home.name
    ? homeLede(l.machines, target, home.name)
    : target.kind === "cursor_mdc" && noFile
      ? t.lede.cursorNone
      : interpolate(
          target.kind in t.lede
            ? t.lede[target.kind as keyof typeof t.lede]
            : t.lede.other,
          { where, tool },
        );

  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        className={styles.head}
        eyebrow={interpolate(t.eyebrow, { space: space.name })}
        title={name}
        lede={lede}
        actions={
          <>
            {home.name &&
            target.syncState !== "off" &&
            space.role === "owner" ? (
              <Button
                variant="quiet"
                pending={stopHere.pending}
                onClick={() => void stopHere.stop()}
              >
                {l.machines.target.stop}
              </Button>
            ) : null}
            <Button
              variant="quiet"
              icon="copy"
              disabled={outputs.length === 0}
              onClick={() => void copyOut()}
            >
              {target.kind === "chatgpt" ? t.copyChatgpt : t.copy}
            </Button>
            <Button
              variant="primary"
              icon="sync"
              onClick={compileNow}
              pending={compile.pending}
              disabled={!canCompile}
              disabledReason={
                target.syncState === "off" ? t.off : l.brief.page.editViewer
              }
            >
              {t.compileNow}
            </Button>
          </>
        }
      />
      <ul className={styles.meta}>
        {home.name ? (
          <li>
            <MachineSwitcher
              view={view}
              target={target}
              all={all}
              machines={home.machines}
            />
          </li>
        ) : null}
        <li>
          <TargetStatus target={target} />
        </li>
        {target.syncState === "drifted" ? (
          <li>
            <Button
              variant="quiet"
              size="sm"
              href={`${targetHref(space.slug, target)}/drift`}
            >
              {l.brief.drift.compare}
            </Button>
          </li>
        ) : null}
        {meta.map((part) => (
          <li key={part}>{part}</li>
        ))}
      </ul>
      <div className={styles.grid}>
        <div className={styles.files}>
          {target.syncState === "off" ? (
            home.name ? (
              <HomeStopped
                view={view}
                target={target}
                machine={home.machine}
                name={home.name}
              />
            ) : (
              <Stopped view={view} target={target} />
            )
          ) : null}
          {target.syncState === "held" ? (
            <Held view={view} target={target} />
          ) : null}
          {noFile ? (
            <section className="mx-panel">
              <p className={styles.note}>{t.noFile}</p>
              <div className={styles.noteActions}>
                <CanonicalLink view={view} target={target} all={all} />
              </div>
            </section>
          ) : shown ? (
            outputs.map((output) => (
              <FilePanel
                key={output.label}
                view={view}
                output={output}
                copyOut={target.kind === "chatgpt"}
                machine={home.name}
                block={home.name !== null && isBlock(target)}
              />
            ))
          ) : (
            <PlaceSkeleton
              title={name}
              status={view.copy.empty.loadingMeta}
              label={interpolate(view.copy.empty.loading, { place: name })}
            />
          )}
        </div>
        <aside className={styles.side}>
          <TargetSettings view={view} target={target} machine={home.name} />
          {others.length > 0 ? (
            <section className="mx-panel" aria-labelledby="other-targets">
              <header className="mx-panel-head">
                <h2 className="mx-panel-title" id="other-targets">
                  {t.otherTargets}
                </h2>
              </header>
              <TargetList space={space} targets={others} />
            </section>
          ) : null}
        </aside>
      </div>
    </div>
  );
}

function FilePanel({
  view,
  output,
  copyOut,
  machine,
  block,
}: {
  view: RecordsView;
  output: CompiledFileView;
  copyOut: boolean;
  /** A home file's machine, which heads it in place of the repository. */
  machine: string | null;
  /** Memax's block in a file the person owns: say the rest stays on the machine. */
  block: boolean;
}) {
  const { space, l } = view;
  const t = l.brief.target;
  const where = space.repository ?? space.name;
  const head = machine
    ? interpolate(l.machines.target.fileHead, { machine, file: output.label })
    : output.path
      ? `${where} / ${output.path}`
      : output.label;
  return (
    <section className="mx-panel" aria-label={output.label}>
      <header className="mx-panel-head">
        <span className={styles.fileHead}>
          <Icon name="file" />
          <span>{head}</span>
        </span>
        <span className="mx-meta">{copyOut ? t.text : t.markdown}</span>
      </header>
      <CodeView
        content={output.content}
        label={interpolate(t.code, { file: output.label })}
      />
      {block && machine ? (
        <p className={styles.blockNote}>
          {interpolate(l.machines.target.block, { machine })}
        </p>
      ) : null}
    </section>
  );
}

/** Cursor with nothing scoped: the canonical file it reads instead. */
function CanonicalLink({
  view,
  target,
  all,
}: {
  view: RecordsView;
  target: TargetView;
  all: TargetView[];
}) {
  const canonical = all.find((t) => t.kind === "agents_md");
  if (!canonical) return null;
  return (
    <Button
      variant="secondary"
      size="sm"
      href={targetHref(view.space.slug, canonical)}
    >
      {interpolate(view.l.brief.target.openCanonical, {
        file: target.reads ?? canonical.label,
      })}
    </Button>
  );
}

/** A pulled hand edit stays in the file until Review decides its proposals. */
function Held({ view, target }: { view: RecordsView; target: TargetView }) {
  const t = view.l.brief.target;
  return (
    <section className="mx-panel">
      <p className={styles.note}>
        {interpolate(target.holding.length === 1 ? t.heldOne : t.held, {
          file: targetName(target),
          refs: target.holding.join(", "),
        })}
      </p>
      <div className={styles.noteActions}>
        <Button
          variant="secondary"
          size="sm"
          href={placeHref(view.space.slug, "review")}
        >
          {t.openReview}
        </Button>
      </div>
    </section>
  );
}

/** A stopped target: agents read over MCP; Compile it again turns it back on. */
function Stopped({ view, target }: { view: RecordsView; target: TargetView }) {
  const t = view.l.brief.target;
  const settings = useTargetSettings(view.space, target);
  return (
    <section className="mx-panel">
      <p className={styles.note}>{t.off}</p>
      {view.space.role !== "viewer" ? (
        <div className={styles.noteActions}>
          <Button
            variant="secondary"
            size="sm"
            icon="sync"
            pending={settings.pending}
            onClick={() => void settings.change({ enabled: true })}
          >
            {t.restart}
          </Button>
        </div>
      ) : null}
    </section>
  );
}
