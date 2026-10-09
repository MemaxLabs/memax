"use client";

import { useMemo } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Button, Terminal } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count, joinList, spell } from "@/lib/v2/copy";
import { autonomyIn } from "@/lib/v2/data/agents";
import {
  agentsOfImport,
  openConflicts,
  waitingMemories,
  type ImportView,
} from "@/lib/v2/data/imports";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import {
  reviewImportHref,
  setupHref,
  skipSpace,
} from "@/lib/v2/onboarding/routes";
import { cliCommand } from "@/lib/v2/cli";
import { AGENT_START, firstRunLines, type TranscriptAgent } from "./transcript";
import { useViewer } from "../(app)/_lib/data";
import { useLedger } from "@memaxlabs/ledger";
import { OnboardingPage } from "./frame";
import { useFunnelStep } from "./funnel";
import { useAgentsOf, useSetupImport, useSetupSpace } from "./queries";
import {
  CommandBox,
  SetupSteps,
  useAppHost,
  type SetupStepItem,
} from "./setup-bits";
import styles from "./first-run.module.css";

/** The agents init would list: connected in the space, else found by their files. */
function useTranscriptAgents(
  space: SpaceSummary | null,
  view: ImportView | null,
): { agents: TranscriptAgent[]; chatgptPending: boolean } {
  const connections = useAgentsOf(space).data;
  return useMemo(() => {
    const out: TranscriptAgent[] = [];
    const live = (connections ?? []).filter((c) => c.state !== "disconnected");
    for (const c of live) {
      if (c.agent === "chatgpt") continue;
      out.push({
        agent: c.agent,
        autonomy: (space && autonomyIn(c, space.slug)?.autonomy) || "read",
      });
    }
    for (const agent of view ? agentsOfImport(view.summary) : []) {
      if (out.some((a) => a.agent === agent)) continue;
      out.push({ agent, autonomy: AGENT_START[agent] ?? "propose" });
    }
    const chatgptPending = !live.some((c) => c.agent === "chatgpt");
    return { agents: out, chatgptPending };
  }, [connections, view, space]);
}

/**
 * FirstRun (FirstRun.png, step 2 of 3) at /setup/import: what to do
 * first, which is `npx memax-cli init` in a repository, and then what it
 * did, as it does it. The page polls for the space init creates, then its
 * import, then the judge's and the conflict check's progress, and draws
 * the terminal from what arrives. A new person (no space on the V2
 * record) lands here after signing in.
 */
export function FirstRunScreen() {
  const { t, locale } = useLocale();
  const copy = t.ledger.onboarding.firstRun;
  const frame = t.ledger.onboarding.frame;
  const app = t.ledger.app;
  const router = useRouter();
  const viewer = useViewer();
  const { agents: registry } = useLedger();
  const setup = useSetupSpace({ poll: true });
  const space = setup.space ?? null;
  // Before init has made a space, a space still on V1 (its Today is the
  // switch to V2): the page never holds someone who'd rather look around.
  const skipTo = space ?? skipSpace(setup.spaces);
  const { imports, view: viewQuery } = useSetupImport(space, { poll: true });
  const view = viewQuery.data ?? null;
  const { agents, chatgptPending } = useTranscriptAgents(space, view);
  const nextKey = useKeycap("setup.next");
  const host = useAppHost();
  useFunnelStep("first_run_reached", true);
  useFunnelStep("first_import_seen", view !== null, {
    files: view?.summary.files.filter((f) => f.statements > 0).length ?? 0,
  });

  const ready = Boolean(view?.progress.ready);
  const conflicts = view ? openConflicts(view).length : 0;
  const waiting = view ? waitingMemories(view).length : 0;
  const primary =
    view && ready && space
      ? conflicts > 0
        ? {
            label: copy.seeDisagree,
            href: setupHref("cleanup", {
              space: space.slug,
              import: view.summary.id,
            }),
          }
        : waiting > 0
          ? {
              label: count(copy.reviewProposal, copy.reviewProposals, waiting),
              href: reviewImportHref(space.slug, view.summary.id),
            }
          : {
              label: app.titles.today,
              href: `/${encodeURIComponent(space.slug)}/today`,
            }
      : null;
  useHotkey(
    "setup.next",
    () => {
      if (primary) router.push(primary.href);
    },
    {
      enabled: Boolean(primary),
    },
  );

  const names = agents.map((a) => registry[a.agent]?.name ?? a.agent);
  const files = view?.summary.files.filter((f) => f.statements > 0).length ?? 0;
  const steps: SetupStepItem[] = view
    ? [
        {
          state: "done",
          title: copy.connect,
          detail: names.length
            ? interpolate(
                chatgptPending ? copy.connectDetailChatgpt : copy.connectDetail,
                { agents: joinList(names, locale) },
              )
            : copy.runDetail,
          extra: space ? (
            <Link
              className={styles.link}
              href={setupHref("agents", { space: space.slug })}
            >
              {copy.connectLink}
            </Link>
          ) : null,
        },
        {
          state: "now",
          title: copy.import,
          detail: ready
            ? [
                interpolate(copy.importReady, {
                  items: view.summary.counts.items,
                  files,
                  proposed: view.summary.counts.proposed,
                }),
                conflicts > 0
                  ? conflicts === 1
                    ? copy.disagreeOne
                    : interpolate(copy.disagreeMany, {
                        n: capitalise(spell(app, conflicts)),
                      })
                  : null,
              ]
                .filter(Boolean)
                .join(locale === "zh" ? "" : " ")
            : interpolate(copy.importChecking, {
                items: view.summary.counts.items,
                files,
              }),
        },
        { state: "later", title: copy.review, detail: copy.reviewDetail },
      ]
    : [
        {
          state: "now",
          title: copy.run,
          detail: copy.runDetail,
          extra: (
            <>
              <CommandBox
                command={cliCommand(
                  space ? `init --space ${space.slug}` : "init",
                )}
              />
              <span className="mx-meta">{copy.follow}</span>
            </>
          ),
        },
        { state: "later", title: copy.import, detail: copy.importWaiting },
        { state: "later", title: copy.review, detail: copy.reviewDetail },
      ];

  const lines = firstRunLines(copy, {
    viewer: viewer?.name ?? null,
    space: space?.slug ?? null,
    agents,
    chatgptPending,
    view,
    host,
  });
  const repo = space?.repository?.split("/").at(-1) ?? space?.slug;

  return (
    <OnboardingPage
      meta={interpolate(frame.step, { n: 2 })}
      align="center"
      fill
    >
      <div className={styles.split}>
        <section aria-labelledby="first-run-title">
          <p className="mx-page-eyebrow">
            {space
              ? interpolate(frame.newSpace, { space: space.slug })
              : frame.firstSpace}
          </p>
          <h1 className={styles.title} id="first-run-title">
            {copy.title}
          </h1>
          <p className={styles.lede}>
            {view
              ? interpolate(copy.lede, {
                  agents:
                    agents.length === 1
                      ? copy.agentsOne
                      : interpolate(copy.agentsMany, {
                          n: spell(app, agents.length),
                        }),
                })
              : copy.ledeWaiting}
          </p>
          <SetupSteps steps={steps} label={copy.steps} />
          <div
            className={`mx-inline ${styles.actions}`}
            aria-busy={(imports.isFetching && !view) || undefined}
          >
            {primary ? (
              <Button
                variant="primary"
                size="lg"
                kbd={nextKey}
                href={primary.href}
              >
                {primary.label}
              </Button>
            ) : null}
            {skipTo ? (
              <Button
                variant="quiet"
                size="lg"
                href={`/${encodeURIComponent(skipTo.slug)}/today`}
              >
                {copy.skip}
              </Button>
            ) : null}
          </div>
        </section>
        <Terminal
          title={
            repo
              ? interpolate(copy.terminalTitle, { repo })
              : copy.terminalWaitingTitle
          }
          lines={lines}
        />
      </div>
    </OnboardingPage>
  );
}

function capitalise(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}
