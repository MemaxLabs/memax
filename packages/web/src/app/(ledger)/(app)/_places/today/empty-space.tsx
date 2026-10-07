"use client";

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Icon, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { setupHref } from "@/lib/v2/onboarding/routes";
import { placeHref } from "@/lib/v2/places";
import { useImports } from "../../../_onboarding/queries";
import { useToast } from "../../_components/toasts";
import { useSource, useSpaces } from "../../_lib/data";
import { recordKeys } from "../../_lib/records";
import type { RecordsView } from "../records-view";
import styles from "./today.module.css";

/**
 * A new, empty space (EmptySpace.png): three steps to a first compile,
 * the one to do now in ink, and where else it could start from. Those
 * two aren't available yet, and say so. D2: the compile writes AGENTS.md,
 * a CLAUDE.md that imports it and Cursor rules.
 */
export function EmptySpace({
  view,
  dreamAt,
}: {
  view: RecordsView;
  /** When Dream runs, where the source knows it. */
  dreamAt: string | null;
}) {
  const { l, copy, space, overview } = view;
  const spaces = useSpaces().data ?? [];
  // While the space is empty, look for `memax init`'s import; once one
  // lands, the space has proposals and Today shows them.
  const source = useSource();
  const queryClient = useQueryClient();
  const imports = useImports(space, { poll: true });
  const landed = (imports.data?.length ?? 0) > 0;
  useEffect(() => {
    if (!landed) return;
    void queryClient.invalidateQueries({
      queryKey: recordKeys.space(source.kind, space.slug),
    });
  }, [landed, queryClient, source.kind, space.slug]);
  const e = l.today.empty;
  const toast = useToast();
  const agents = overview?.agents?.connected ?? 0;
  // The step to do now: connect, then settle, then keep and compile.
  const now = agents === 0 ? 1 : overview?.waiting ? 2 : 3;
  const command = interpolate(e.command, { space: space.slug });
  const where = space.repository ?? space.name;
  const other = spaces.find((s) => s.id !== space.id && s.kind !== "personal");

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command);
      toast({ text: copy.toast.copiedCommand });
    } catch {
      toast({ state: "proposed", text: l.brief.toast.copyFailed });
    }
  };

  const steps = [
    {
      title: space.repository ? e.connect : e.connectAnywhere,
      detail: space.repository
        ? interpolate(e.connectDetail, { repository: space.repository })
        : e.connectDetailAnywhere,
    },
    { title: e.settle, detail: e.settleDetail },
    { title: e.compile, detail: e.compileDetail },
  ];

  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        eyebrow={view.eyebrow}
        title={e.title}
        lede={interpolate(e.lede, { where })}
      />
      <div className={styles.emptyGrid}>
        <section className="mx-panel" aria-label={e.steps}>
          <ol className={styles.steps}>
            {steps.map((step, i) => {
              const n = i + 1;
              const current = n === now;
              return (
                <li
                  key={n}
                  className={`${styles.step} ${current ? styles.now : ""}`}
                  aria-current={current ? "step" : undefined}
                >
                  <span
                    className={`${styles.stepN} ${current ? styles.current : ""}`}
                    aria-hidden="true"
                  >
                    {n < now ? <Icon name="check" size={14} /> : n}
                  </span>
                  <div className={styles.stepBody}>
                    <div>
                      <h3>{step.title}</h3>
                      <p>{step.detail}</p>
                    </div>
                    {n === 1 && current ? (
                      <>
                        <div className={styles.command}>
                          <span className={styles.prompt} aria-hidden="true">
                            ›
                          </span>
                          <code>{command}</code>
                          <button
                            type="button"
                            className={styles.copyCommand}
                            aria-label={e.copy}
                            title={e.copy}
                            onClick={() => void copyCommand()}
                          >
                            <Icon name="copy" size={14} />
                          </button>
                        </div>
                        <span className={styles.emptyActions}>
                          <Button
                            variant="secondary"
                            size="sm"
                            icon="plus"
                            href={`${placeHref(space.slug, "agents")}?overlay=connect`}
                          >
                            {e.connectHere}
                          </Button>
                          <Button
                            variant="quiet"
                            size="sm"
                            href={setupHref("import", { space: space.slug })}
                          >
                            {l.onboarding.emptySpace.follow}
                          </Button>
                        </span>
                      </>
                    ) : null}
                    {n === 2 && current ? (
                      <span>
                        <Button
                          variant="secondary"
                          size="sm"
                          href={placeHref(space.slug, "review")}
                        >
                          {l.today.waiting.open}
                        </Button>
                      </span>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ol>
        </section>
        <aside className={styles.emptySide}>
          <section className="mx-panel" aria-labelledby="start-from">
            <header className="mx-panel-head">
              <h2 className="mx-panel-title" id="start-from">
                {e.startFrom}
              </h2>
            </header>
            <div className={styles.startFrom}>
              {other ? (
                <Button
                  variant="secondary"
                  size="sm"
                  icon="copy"
                  disabled
                  disabledReason={e.conventionsLater}
                >
                  {interpolate(e.conventionsFrom, { space: other.name })}
                </Button>
              ) : null}
              <div
                className={styles.drop}
                aria-disabled="true"
                title={e.dropLater}
              >
                <span className={styles.dropIcon}>
                  <Icon name="file" />
                </span>
                <span className={styles.dropTitle}>{e.drop}</span>
                <span className="mx-meta">{e.dropMeta}</span>
              </div>
            </div>
          </section>
          {dreamAt ? (
            <p className={`mx-meta ${styles.emptyNote}`}>
              {interpolate(e.dreamTonight, { time: dreamAt })}
            </p>
          ) : null}
        </aside>
      </div>
    </div>
  );
}
