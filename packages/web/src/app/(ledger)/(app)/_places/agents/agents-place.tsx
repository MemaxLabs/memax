"use client";

import { useCallback } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  AgentListHead,
  Button,
  Icon,
  PageHeader,
  Terminal,
  useLedger,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { agentsLede } from "@/lib/v2/copy";
import type { AgentConnectionView } from "@/lib/v2/data/agents";
import { EmptyState } from "../../_components/empty-state";
import { PlaceError } from "../../_components/status";
import { PlaceSkeleton } from "../../_components/skeleton";
import { useToast } from "../../_components/toasts";
import { NotYetButton, PlaceColumn, usePlace } from "../place";
import placeStyles from "../place.module.css";
import { AgentRowControl } from "./agent-row-control";
import { ConnectAgentDialog } from "./connect-dialog";
import { useSpaceAgents } from "./queries";
import styles from "./agents.module.css";

/** `?overlay=connect` opens Connect an agent (plan §6.3: overlays deep-link). */
const OVERLAY = "connect";

/**
 * Agents (Agents.png, epic 1.8): every agent connected to the space,
 * with its autonomy, this week's reads and writes, when it was last seen
 * and the file it compiles to; the trust rules; and how to connect one
 * from the terminal.
 */
export function AgentsPlace() {
  const place = usePlace();
  const { space, overview, copy, locale } = place;
  const { t } = useLocale();
  const agentsCopy = t.ledger.agents;
  const { strings } = useLedger();
  const agents = useSpaceAgents(space);
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const connectOpen = params?.get("overlay") === OVERLAY;
  const setConnectOpen = useCallback(
    (open: boolean) => {
      const next = new URLSearchParams(params?.toString());
      if (open) next.set("overlay", OVERLAY);
      else next.delete("overlay");
      const query = next.toString();
      router.replace(query ? `${pathname}?${query}` : (pathname ?? ""), {
        scroll: false,
      });
    },
    [params, pathname, router],
  );

  const list = agents.data;
  const counts = list
    ? {
        connected: list.length,
        active: list.filter((a) => a.state === "active").length,
      }
    : null;
  const lede =
    overview && counts
      ? (agentsLede(copy, locale, { ...overview, agents: counts }) ?? undefined)
      : undefined;

  return (
    <PlaceColumn>
      <PageHeader
        className={styles.head}
        eyebrow={place.eyebrow}
        title={copy.agents.title}
        lede={lede}
        actions={
          <>
            {/* PLACEHOLDER: compile targets are built on another branch. */}
            <NotYetButton variant="secondary" icon="sync">
              {copy.agents.compileAll}
            </NotYetButton>
            <Button
              variant="primary"
              icon="plus"
              onClick={() => setConnectOpen(true)}
            >
              {copy.agents.connect}
            </Button>
          </>
        }
      />
      {!list ? (
        agents.isError ? (
          <PlaceError space={space} onRetry={() => void agents.refetch()} />
        ) : (
          <PlaceSkeleton
            title={strings.nav.places.agents}
            status={copy.empty.loadingMeta}
            label={interpolate(copy.empty.loading, {
              place: strings.nav.places.agents,
            })}
          />
        )
      ) : list.length === 0 ? (
        <NoAgents slug={space.slug} />
      ) : (
        <>
          <section
            className={`mx-panel ${styles.table}`}
            aria-label={interpolate(agentsCopy.tableLabel, {
              space: space.name,
            })}
          >
            <AgentListHead />
            {list.map((agent: AgentConnectionView) => (
              <AgentRowControl key={agent.id} agent={agent} space={space} />
            ))}
          </section>
          <div className={styles.lower}>
            <TrustRules />
            <Terminal
              title={agentsCopy.terminal.title}
              lines={[
                { kind: "cmd", text: agentsCopy.terminal.command },
                { kind: "dim", text: agentsCopy.terminal.found },
                { kind: "ok", text: agentsCopy.terminal.added },
                {
                  kind: "kept",
                  text: interpolate(agentsCopy.terminal.compiled, {
                    space: space.slug,
                  }),
                },
                { kind: "dim", text: agentsCopy.terminal.receipts },
              ]}
            />
          </div>
        </>
      )}
      <ConnectAgentDialog
        open={connectOpen}
        onOpenChange={setConnectOpen}
        connected={list ?? []}
      />
    </PlaceColumn>
  );
}

/** The trust rules every agent works under (Agents.png), as written on the board. */
export function TrustRules() {
  const { t } = useLocale();
  const rules = t.ledger.agents.trust;
  const items = [
    { icon: "shield", ...rules.external },
    { icon: "receipt", ...rules.receipt },
    { icon: "forget", ...rules.forget },
  ] as const;
  return (
    <section className="mx-panel" aria-labelledby="trust-rules">
      <header className="mx-panel-head">
        <h2 id="trust-rules" className="mx-panel-title">
          {rules.title}
        </h2>
        <span className="mx-meta">{rules.meta}</span>
      </header>
      <ul className={styles.rules}>
        {items.map((rule) => (
          <li key={rule.icon} className={styles.rule}>
            <span className={styles.ruleIcon}>
              <Icon name={rule.icon} />
            </span>
            <span className={styles.ruleText}>
              <strong className={styles.ruleTitle}>{rule.title}</strong>
              <span className={styles.ruleBody}>{rule.body}</span>
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** No agents yet (States board): one serif sentence, the command, one action. */
function NoAgents({ slug }: { slug: string }) {
  const { copy } = usePlace();
  const toast = useToast();
  const command = interpolate(copy.empty.agents.command, { space: slug });
  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command);
      toast({ text: copy.toast.copiedCommand });
    } catch {
      toast({ text: copy.toast.failed });
    }
  };
  return (
    <EmptyState
      title={copy.empty.agents.title}
      detail={copy.empty.agents.detail}
      action={
        <Button
          variant="secondary"
          icon="copy"
          onClick={() => void copyCommand()}
        >
          {copy.empty.agents.copy}
        </Button>
      }
    >
      <code className={placeStyles.command}>
        <span className={placeStyles.prompt} aria-hidden="true">
          ›
        </span>
        {command}
      </code>
    </EmptyState>
  );
}
