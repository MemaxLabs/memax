"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, PageHeader, Segmented } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { switchRunning, type SwitchView } from "@/lib/v2/data/switch";
import { joinSentences } from "@/lib/v2/copy";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { reviewImportHref } from "@/lib/v2/onboarding/routes";
import { placeHref } from "@/lib/v2/places";
import { switchRows } from "@/lib/v2/switch-copy";
import { HeaderSkeleton } from "../../_components/skeleton";
import { useToast } from "../../_components/toasts";
import { ledgerQueryKeys, useSource } from "../../_lib/data";
import type { RecordsView } from "../records-view";
import styles from "./today.module.css";

/** While a switch runs in the background, how often its status is read. */
export const SWITCH_POLL_MS = 1000;

export const switchKeys = {
  status: (kind: string, slug: string) =>
    ["v2", kind, "spaces", slug, "switch"] as const,
};

/**
 * Today of a space still on V1 (plan §10, epic 2.8): what switching it
 * to V2 moves, read fresh from V1, and the switch itself, for the
 * space's owner. A switch with an import to judge runs in the
 * background; the page follows it, then opens Review's "From V1" (the
 * person's own V1 memories, to keep in one go), or Today when there are
 * none. A failed switch resumes where it stopped. Nothing in V1
 * changes, and `memax switch --back` switches it back.
 */
export function SwitchToV2({ view }: { view: RecordsView }) {
  const { space, l, locale } = view;
  const s = l.today.switch;
  const source = useSource();
  const queryClient = useQueryClient();
  const router = useRouter();
  const toast = useToast();
  const keys = useRef(new IntentKeys());
  const owner = space.role === "owner";
  const statusKey = switchKeys.status(source.kind, space.slug);
  const status = useQuery<SwitchView>({
    queryKey: statusKey,
    queryFn: ({ signal }) => source.switch.status({ space, signal }),
    refetchInterval: (q) =>
      switchRunning(q.state.data) ? SWITCH_POLL_MS : false,
  });
  const [kind, setKind] = useState<"project" | "team" | null>(null);

  const run = useMutation({
    mutationFn: async () => {
      const intent = `switch:${space.slug}:${kind ?? ""}`;
      const idempotencyKey = keys.current.keyFor(intent);
      const v = await source.switch.toV2({
        space,
        ...(kind ? { kind } : {}),
        idempotencyKey,
      });
      keys.current.settle(intent);
      return v;
    },
    onSuccess: (v) => queryClient.setQueryData(statusKey, v),
    onError: () => toast({ state: "proposed", text: s.failedToast }),
  });

  // Once it lands: the space is on V2 (the spaces list says so), and the
  // person's own V1 memories wait in Review.
  const landed = status.data?.state === "switched" ? status.data : null;
  const done = useRef(false);
  useEffect(() => {
    if (!landed || done.current) return;
    done.current = true;
    toast({ text: interpolate(s.switchedToast, { space: space.name }) });
    void queryClient.invalidateQueries({
      queryKey: ledgerQueryKeys.spaces(source.kind),
    });
    router.push(
      landed.progress.proposed > 0 && landed.importId
        ? reviewImportHref(space.slug, landed.importId)
        : placeHref(space.slug, "today"),
    );
  }, [landed, queryClient, router, s.switchedToast, source.kind, space, toast]);

  const title = interpolate(s.title, { space: space.name });
  if (!status.data) {
    return (
      <div className={`mx-page ${styles.page}`}>
        {status.isError ? (
          <PageHeader
            eyebrow={view.eyebrow}
            title={title}
            lede={s.loadFailed}
            actions={
              <Button onClick={() => void status.refetch()}>{s.retry}</Button>
            }
          />
        ) : (
          <HeaderSkeleton />
        )}
      </div>
    );
  }

  const v = status.data;
  const pv = v.preview;
  const running = v.state === "running" || run.isPending;
  const failed = v.state === "failed";
  const rows = switchRows(s, pv, locale);
  const kinds = pv.kinds.filter(
    (k): k is "project" | "team" => k === "project" || k === "team",
  );
  const chosen = kind ?? (pv.kind === "personal" ? null : pv.kind);

  return (
    <div className={`mx-page ${styles.page}`}>
      {/* Not Today's phone header: the lede and the switch stay on a phone. */}
      <PageHeader
        className={styles.switchHead}
        eyebrow={view.eyebrow}
        title={title}
        lede={s.lede}
        actions={
          owner ? (
            <Button
              variant="primary"
              disabled={running || Boolean(landed)}
              pending={running}
              onClick={() => run.mutate()}
            >
              {failed ? s.resume : s.switchNow}
            </Button>
          ) : null
        }
      />
      {v.state === "running" ? (
        <p className={`mx-meta ${styles.switchNote}`} role="status">
          {joinSentences(
            [interpolate(s.running, { space: space.name }), s.runningDetail],
            locale,
          )}
        </p>
      ) : null}
      {failed ? (
        <p className={styles.switchNote} role="alert">
          {joinSentences(
            [
              interpolate(s.failed, {
                step: (s.steps as Record<string, string>)[v.step] ?? v.step,
              }),
              s.failedDetail,
            ],
            locale,
          )}
        </p>
      ) : null}
      {v.state === "off" ? (
        <p className={`mx-meta ${styles.switchNote}`}>
          {interpolate(s.back, { space: space.name })}
        </p>
      ) : null}
      <div className={styles.emptyGrid}>
        <section className="mx-panel" aria-labelledby="switch-moves">
          <header className="mx-panel-head">
            <h2 className="mx-panel-title" id="switch-moves">
              {s.moves}
            </h2>
          </header>
          <dl className={styles.moves}>
            {rows.map((row) => (
              <div key={row.key} className={styles.move}>
                <dt>{row.label}</dt>
                <dd>{row.text}</dd>
              </div>
            ))}
          </dl>
        </section>
        <aside className={styles.emptySide}>
          {owner && kinds.length > 1 && chosen ? (
            <section className="mx-panel" aria-labelledby="switch-as">
              <header className="mx-panel-head">
                <h2 className="mx-panel-title" id="switch-as">
                  {s.as}
                </h2>
              </header>
              <div className={styles.switchAs}>
                <Segmented
                  size="sm"
                  label={s.as}
                  options={kinds.map((k) => ({
                    value: k,
                    label: s.kinds[k],
                  }))}
                  value={chosen}
                  onChange={(k) => setKind(k as "project" | "team")}
                />
              </div>
            </section>
          ) : null}
          {owner ? null : (
            <p className={`mx-meta ${styles.emptyNote}`}>{s.ownerOnly}</p>
          )}
        </aside>
      </div>
    </div>
  );
}
