"use client";

import { useEffect, useRef, useState } from "react";
import { useParams } from "next/navigation";
import { AgentStamp, Button, Icon, Kbd, useLedger } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { joinSentences } from "@/lib/v2/copy";
import { autonomyIn, type AgentDetailView } from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { placeHref } from "@/lib/v2/places";
import { StatusPage } from "../../../_components/status-page";
import { PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { PlaceColumn, usePlace } from "../place";
import {
  CompilesPanel,
  ConnectionPanel,
  MayPanel,
  SessionsPanel,
  WeekPanel,
  WritesPanel,
} from "./agent-panels";
import { useAgent } from "./queries";
import { useAgentCommand } from "./use-agent-commands";
import styles from "./agent-detail.module.css";

/** The agent's page (AgentDetail.png): /[space]/agents/[agent], by connection id. */
export function AgentDetailPlace() {
  const params = useParams<{ agent: string }>();
  const id = decodeURIComponent(params?.agent ?? "");
  const place = usePlace();
  const { space, copy } = place;
  const { t } = useLocale();
  const query = useAgent(id);
  if (query.data === undefined) {
    return (
      <PlaceColumn>
        {query.isError ? (
          <PlaceError space={space} onRetry={() => void query.refetch()} />
        ) : (
          <PlaceSkeleton
            title={copy.agents.title}
            status={copy.empty.loadingMeta}
            label={interpolate(copy.empty.loading, {
              place: copy.agents.title,
            })}
          />
        )}
      </PlaceColumn>
    );
  }
  if (query.data === null) {
    const nf = t.ledger.agents.detail.notFound;
    return (
      <StatusPage
        variant="sheet"
        receipt={id}
        title={nf.title}
        description={nf.description}
        actions={
          <Button variant="secondary" href={placeHref(space.slug, "agents")}>
            {nf.back}
          </Button>
        }
      />
    );
  }
  return <AgentDetail detail={query.data} space={space} />;
}

function AgentDetail({
  detail,
  space,
}: {
  detail: AgentDetailView;
  space: SpaceSummary;
}) {
  const { t, locale } = useLocale();
  const { agents } = useLedger();
  const copy = t.ledger.agents.detail;
  const agent = detail.connection;
  const vars = { agent: agent.name, space: space.name };
  const here = autonomyIn(agent, space.slug);
  const run = useAgentCommand(space);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const disconnectRef = useRef<HTMLButtonElement | null>(null);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const disconnected = agent.state === "disconnected";

  // The confirmation takes focus when it opens; Cancel gives it back.
  useEffect(() => {
    if (confirming) cancelRef.current?.focus();
  }, [confirming]);

  const mono =
    agents[agent.agent]?.mono ?? agent.agent.slice(0, 2).toUpperCase();
  const lede = disconnected
    ? copy.lede.disconnected
    : joinSentences(
        [
          copy.surface[agent.surface],
          interpolate(
            agent.state === "paused"
              ? copy.lede.paused
              : copy.lede[here?.autonomy ?? "read"],
            { mono },
          ),
        ],
        locale,
      );

  const unavailable = !agent.mine
    ? interpolate(copy.notMine, vars)
    : disconnected
      ? interpolate(copy.disconnectedAlready, vars)
      : undefined;
  // A revoked credential can't be paused, only disconnected (cleared).
  const pauseUnavailable =
    unavailable ??
    (agent.credential.active ? undefined : t.ledger.agents.unavailable.revoked);

  const command = async (name: "pause" | "resume" | "disconnect") => {
    setBusy(true);
    const ok = await run(name, agent);
    setBusy(false);
    if (name === "disconnect") {
      setConfirming(false);
      if (!ok) disconnectRef.current?.focus();
    }
  };
  const cancel = () => {
    setConfirming(false);
    disconnectRef.current?.focus();
  };

  const paused = agent.state === "paused";
  return (
    <PlaceColumn>
      {/* PageHeader's markup, with the stamp beside the title as drawn
          (AgentDetail.dc.html), not inside the heading. */}
      <header className={`mx-page-head ${styles.head}`}>
        <div className="mx-page-head-text">
          <div className="mx-page-eyebrow">
            {interpolate(copy.eyebrow, { space: space.name })}
          </div>
          <div className={styles.titleRow}>
            <AgentStamp agent={agent.agent} name={agent.name} decorative />
            <h1 className="mx-page-title">{agent.name}</h1>
          </div>
          <p className="mx-page-lede">{lede}</p>
        </div>
        <div className="mx-page-actions">
          <Button
            variant="secondary"
            disabled={Boolean(pauseUnavailable)}
            disabledReason={pauseUnavailable}
            pending={busy && !confirming}
            onClick={() => void command(paused ? "resume" : "pause")}
          >
            {paused ? copy.resume : copy.pause}
          </Button>
          <Button
            ref={disconnectRef}
            variant="danger"
            disabled={Boolean(unavailable)}
            disabledReason={unavailable}
            aria-expanded={confirming}
            aria-controls="agent-disconnect"
            onClick={() => setConfirming(true)}
          >
            {copy.disconnect}
          </Button>
        </div>
      </header>

      {confirming ? (
        <section
          id="agent-disconnect"
          className={styles.confirm}
          aria-labelledby="agent-disconnect-title"
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              event.stopPropagation();
              cancel();
            }
          }}
        >
          <h2 id="agent-disconnect-title" className={styles.confirmTitle}>
            {interpolate(copy.confirm.title, vars)}
          </h2>
          <p className={styles.confirmBody}>
            {agent.credential.kind === "oauth_grant"
              ? copy.confirm.oauth
              : copy.confirm.key}
          </p>
          <div className={styles.confirmActions}>
            <Button
              ref={cancelRef}
              variant="quiet"
              size="sm"
              onClick={cancel}
              aria-keyshortcuts="Escape"
            >
              {copy.confirm.cancel}
              <Kbd className="mx-btn-kbd" aria-hidden="true">
                Esc
              </Kbd>
            </Button>
            <Button
              variant="danger"
              size="sm"
              pending={busy}
              onClick={() => void command("disconnect")}
            >
              {interpolate(copy.confirm.confirm, vars)}
            </Button>
          </div>
        </section>
      ) : null}

      {!agent.credential.active && !disconnected ? (
        <section className={styles.revoked} role="status">
          <span className={styles.revokedIcon}>
            <Icon name="alert" />
          </span>
          <span>
            <strong className={styles.revokedTitle}>
              {copy.revoked.title}
            </strong>{" "}
            {interpolate(copy.revoked.body, vars)}
          </span>
        </section>
      ) : null}

      <div className={styles.grid}>
        <div className={styles.main}>
          <MayPanel detail={detail} space={space} />
          <WritesPanel detail={detail} space={space} />
          <SessionsPanel detail={detail} />
        </div>
        <aside className={styles.aside}>
          <ConnectionPanel detail={detail} space={space} />
          <CompilesPanel detail={detail} />
          <WeekPanel detail={detail} />
        </aside>
      </div>
    </PlaceColumn>
  );
}
