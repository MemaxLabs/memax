"use client";

import { Button, Receipt, Seal, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatAgo, formatShortDate } from "@/lib/v2/copy";
import { clashTitle } from "@/lib/v2/records-copy";
import type { MemoryRecord } from "@/lib/v2/data/memories";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { EditClash } from "../../_components/edit-clash";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { StatementEditor } from "../../_components/statement-editor";
import { StatementText } from "../../_components/statement-text";
import { PlaceError } from "../../_components/status";
import { useToast } from "../../_components/toasts";
import { StatusPage } from "../../../_components/status-page";
import { useOverlays } from "../../_lib/overlays";
import { useMemoryRecord } from "../../_lib/records";
import { useRecordsView, type RecordsView } from "../records-view";
import { MemoryHead } from "./memory-head";
import { MemoryLineage } from "./memory-lineage";
import { MemorySide } from "./memory-side";
import { useMemoryEdit } from "./use-memory-edit";
import styles from "./memory.module.css";

/**
 * One memory (Memory.png) at /[space]/memories/[ref]: the statement
 * under its seal, its receipt and reach, Edit (E), Copy citation (⌘⇧C),
 * Move to space and Forget (not yet: Forget arrives with propagation,
 * and never has a key), then its lineage, merged notes, sources, the
 * files it reaches and what keeps it true. Not found is States2's.
 */
export function MemoryPlace({ memoryRef }: { memoryRef: string }) {
  const view = useRecordsView();
  const { l, space } = view;
  const record = useMemoryRecord(space, memoryRef);
  const { openCommand } = useOverlays();

  if (record.data === undefined) {
    if (record.isError) {
      return <PlaceError space={space} onRetry={() => void record.refetch()} />;
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={l.memory.page.lineage}
          status={view.copy.empty.loadingMeta}
          label={l.memory.list.loading}
        />
      </div>
    );
  }
  if (record.data === null) {
    const nf = l.memory.page.notFound;
    return (
      <StatusPage
        variant="sheet"
        receipt={memoryRef}
        title={nf.title}
        description={nf.detail}
        actions={
          <>
            <Button
              variant="secondary"
              size="sm"
              icon="search"
              onClick={() => openCommand("ask")}
            >
              {nf.search}
            </Button>
            <Button
              variant="quiet"
              size="sm"
              href={placeHref(space.slug, "activity")}
            >
              {nf.activity}
            </Button>
          </>
        }
      />
    );
  }
  return <Memory view={view} record={record.data} />;
}

function Memory({ view, record }: { view: RecordsView; record: MemoryRecord }) {
  const { l, rc, space, viewer, timeZone, locale } = view;
  const p = l.memory.page;
  const toast = useToast();
  const edit = useMemoryEdit(space, record);
  const submitKey = useKeycap("command.keep");
  const forgotten = record.forgotten;

  const cite = async () => {
    const link = `${window.location.origin}${placeHref(space.slug, "memories")}/${encodeURIComponent(record.ref)}`;
    try {
      await navigator.clipboard.writeText(`[${record.ref}] ${link}`);
      toast({
        state: "kept",
        text: interpolate(rc.toast.copied, { ref: record.ref }),
      });
    } catch {
      toast({ state: "proposed", text: rc.toast.copyFailed });
    }
  };

  useHotkey("memory.edit", () => edit.start(), {
    enabled: edit.mode.kind === "view" && !forgotten,
  });
  useHotkey("memory.cite", () => void cite(), { enabled: !forgotten });

  const mode = edit.mode;

  let lead;
  if (mode.kind === "editing") {
    lead = (
      <StatementEditor
        stateLabel={interpolate(p.editing, { ref: record.ref })}
        base={mode.base.statement}
        draft={mode.draft}
        reason={mode.reason}
        onDraft={(draft) => edit.change({ draft })}
        onReason={(reason) => edit.change({ reason })}
        onCancel={edit.cancel}
        onSubmit={() => void edit.submit(mode)}
        labels={{
          statement: rc.editor.statement,
          diff: rc.editor.yourChange,
          why: rc.editor.why,
          whyHint: rc.editor.whyHint,
          cancel: rc.editor.cancel,
          submit: rc.editor.keepEdited,
        }}
        receipt={
          <Receipt
            person={viewer?.initials}
            name={rc.actor.you}
            action={rc.verbs.edited}
            id={record.ref}
          />
        }
        pending={edit.pending}
        error={mode.error ? rc.editor.empty : null}
        submitKey={submitKey}
      />
    );
  } else if (mode.kind === "clash") {
    const by = mode.theirs.by;
    lead = (
      <EditClash
        title={clashTitle(
          rc,
          by,
          formatAgo(view.copy, mode.theirs.at, view.now),
          view.agentName,
        )}
        stamp={view.stamp(by)}
        before={mode.base.statement}
        theirs={mode.theirs.statement}
        yours={interpolate(rc.clash.yours, { text: mode.mine })}
        labels={rc.clash}
        onKeepTheirs={edit.keepTheirs}
        onCombine={() => edit.combine(mode)}
        onKeepMine={() => edit.keepMine(mode)}
        pending={edit.pending}
      />
    );
  } else {
    lead = (
      <MemoryHead
        view={view}
        record={record}
        onEdit={() => edit.start()}
        onCite={() => void cite()}
      />
    );
  }

  return (
    <div className={`mx-page ${styles.page}`}>
      <div className={styles.top}>
        <div className={styles.lead}>
          <div className={styles.eyebrow}>
            <span className="mx-page-eyebrow">
              {interpolate(p.eyebrow, {
                space: space.name,
                section: rc.sections[record.section],
              })}
            </span>
            {record.state !== "kept" ? (
              <StateMark state={record.state} />
            ) : null}
          </div>
          {mode.kind === "view" ? null : (
            <h1 className="mx-sr">
              <StatementText text={record.statement} />
            </h1>
          )}
          {lead}
        </div>
        {record.kept ? (
          <div className={styles.seal}>
            <Seal
              date={formatShortDate(new Date(record.kept.at), timeZone, locale)}
              id={record.ref}
              size={112}
            />
          </div>
        ) : null}
      </div>
      <div className={styles.body}>
        <MemoryLineage view={view} record={record} />
        <MemorySide view={view} record={record} />
      </div>
    </div>
  );
}
