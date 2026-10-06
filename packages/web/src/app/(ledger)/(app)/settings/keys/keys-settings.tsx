"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  AgentStamp,
  Button,
  Icon,
  PageHeader,
  StateMark,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { agoText } from "@/lib/v2/agents/copy";
import { formatShortDate } from "@/lib/v2/copy";
import {
  AUTONOMY_LEVELS,
  capabilities,
  type AgentConnectionView,
  type Autonomy,
} from "@/lib/v2/data/agents";
import { useSource, useSpaces } from "../../_lib/data";
import { useSpaceView } from "../../_lib/space-context";
import { useToast } from "../../_components/toasts";
import {
  agentKeys,
  useApiKeys,
  useMyAgents,
} from "../../_places/agents/queries";
import { useAgentCommand } from "../../_places/agents/use-agent-commands";
import { usePlace } from "../../_places/place";
import { NewKey } from "./new-key";
import styles from "./keys.module.css";

/** The most an agent may do in any of its spaces, for the one "May" column. */
function highest(agent: AgentConnectionView): Autonomy {
  let top: Autonomy = "read";
  for (const s of agent.spaces) {
    if (AUTONOMY_LEVELS.indexOf(s.autonomy) > AUTONOMY_LEVELS.indexOf(top)) {
      top = s.autonomy;
    }
  }
  return top;
}

/** A row's Revoke, confirmed inline: the consequence named, then Revoke. */
function Revoke({
  name,
  confirm,
  onRevoke,
}: {
  name: string;
  confirm: string;
  onRevoke: () => Promise<void>;
}) {
  const { t } = useLocale();
  const copy = t.ledger.agents.keys;
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  if (!asking) {
    return (
      <Button
        variant="quiet"
        size="sm"
        aria-label={interpolate(copy.revokeLabel, { name })}
        onClick={() => setAsking(true)}
      >
        {copy.revoke}
      </Button>
    );
  }
  return (
    <div
      className={styles.confirm}
      role="group"
      aria-label={interpolate(copy.revokeLabel, { name })}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          setAsking(false);
        }
      }}
    >
      <span className={styles.confirmText}>{confirm}</span>
      <Button
        variant="quiet"
        size="sm"
        autoFocus
        onClick={() => setAsking(false)}
      >
        {copy.cancel}
      </Button>
      <Button
        variant="danger"
        size="sm"
        pending={busy}
        onClick={async () => {
          setBusy(true);
          await onRevoke();
          setBusy(false);
          setAsking(false);
        }}
      >
        {copy.revoke}
      </Button>
    </div>
  );
}

/**
 * Settings › Agents and keys (Keys.png): every agent connection across
 * the person's spaces, and their API keys (V1's auth keys), which read
 * or propose and never keep or forget.
 */
export function KeysSettings() {
  const { t, locale } = useLocale();
  const copy = t.ledger.agents.keys;
  const caps = t.ledger.agents.capabilities;
  const { space } = useSpaceView();
  const { now, timeZone } = usePlace();
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const agents = useMyAgents();
  const keys = useApiKeys();
  const spaces = useSpaces().data ?? [];
  const run = useAgentCommand(space);
  const [creating, setCreating] = useState(false);
  const spaceName = (id: string) => spaces.find((s) => s.id === id)?.name ?? id;
  const ago = (iso: string | null) =>
    agoText(t.ledger.agents, iso, now, timeZone, locale, { lower: true });

  return (
    <>
      <PageHeader title={copy.title} lede={copy.lede} className={styles.head} />

      <section className="mx-panel" aria-labelledby="keys-connections">
        <header className="mx-panel-head">
          <h2 id="keys-connections" className="mx-panel-title">
            {copy.connections.title}
          </h2>
          <span className={`mx-meta ${styles.plain}`}>
            {copy.connections.meta}
          </span>
        </header>
        <div className={`${styles.row} ${styles.th}`} aria-hidden="true">
          <span>{copy.connections.agent}</span>
          <span>{copy.connections.spaces}</span>
          <span>{copy.connections.may}</span>
          <span>{copy.connections.lastUsed}</span>
          <span />
        </div>
        {agents.data?.length === 0 ? (
          <p className={styles.none}>{copy.connections.none}</p>
        ) : (
          <ul className={styles.list} aria-label={copy.connections.label}>
            {(agents.data ?? []).map((agent) => (
              <li key={agent.id} className={styles.row}>
                <AgentStamp agent={agent.agent} name={agent.name} showName />
                <span className={styles.c}>
                  <span className="mx-sr">{copy.connections.spaces}: </span>
                  {agent.spaces.map((s) => s.name).join(", ")}
                </span>
                <span className={styles.c}>
                  <span className="mx-sr">{copy.connections.may}: </span>
                  {capabilities(agent, highest(agent))
                    .map((c) => caps[c])
                    .join(locale === "zh" ? "、" : ", ")}
                </span>
                {agent.state === "paused" ? (
                  <StateMark
                    state="off"
                    label={t.ledger.agents.connect.paused}
                  />
                ) : (
                  <span className="mx-meta">
                    <span className="mx-sr">{copy.connections.lastUsed}: </span>
                    {ago(agent.lastSeenAt)}
                  </span>
                )}
                <Revoke
                  name={agent.name}
                  confirm={interpolate(copy.confirmAgent, { name: agent.name })}
                  onRevoke={async () => {
                    await run("disconnect", agent);
                  }}
                />
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="mx-panel" aria-labelledby="keys-api">
        <header className="mx-panel-head">
          <h2 id="keys-api" className="mx-panel-title">
            {copy.apiKeys.title}
          </h2>
          <Button
            variant="secondary"
            size="sm"
            icon="plus"
            aria-expanded={creating}
            onClick={() => setCreating(true)}
          >
            {copy.apiKeys.new}
          </Button>
        </header>
        {creating ? (
          <NewKey space={space} onDone={() => setCreating(false)} />
        ) : null}
        <div className={`${styles.row} ${styles.th}`} aria-hidden="true">
          <span>{copy.apiKeys.name}</span>
          <span>{copy.apiKeys.key}</span>
          <span>{copy.apiKeys.may}</span>
          <span>{copy.apiKeys.lastUsed}</span>
          <span />
        </div>
        {keys.data?.length === 0 ? (
          <p className={styles.none}>{copy.apiKeys.none}</p>
        ) : (
          <ul className={styles.list} aria-label={copy.apiKeys.label}>
            {(keys.data ?? []).map((key) => (
              <li key={key.id} className={styles.row}>
                <span className={styles.keyName}>
                  <span className={styles.name}>{key.name}</span>
                  <span className="mx-meta">
                    {interpolate(copy.apiKeys.created, {
                      spaces: key.spaceIds
                        ? key.spaceIds.map(spaceName).join(", ")
                        : copy.apiKeys.allSpaces,
                      date: formatShortDate(
                        new Date(key.createdAt),
                        timeZone,
                        locale,
                      ),
                    })}
                  </span>
                </span>
                <code className={`mx-code ${styles.key}`}>{key.masked}</code>
                <span className={styles.c}>
                  <span className="mx-sr">{copy.apiKeys.may}: </span>
                  {copy.apiKeys[key.may]}
                </span>
                <span className="mx-meta">
                  <span className="mx-sr">{copy.apiKeys.lastUsed}: </span>
                  {ago(key.lastUsedAt)}
                </span>
                <Revoke
                  name={key.name}
                  confirm={interpolate(copy.confirmKey, { name: key.name })}
                  onRevoke={async () => {
                    try {
                      await source.revokeApiKey(key.id);
                      queryClient.setQueryData(
                        agentKeys.keys(source.kind),
                        (list: typeof keys.data) =>
                          list?.filter((k) => k.id !== key.id),
                      );
                      void queryClient.invalidateQueries({
                        queryKey: agentKeys.all(source.kind),
                      });
                      toast({
                        state: "off",
                        text: interpolate(copy.revoked, { name: key.name }),
                      });
                    } catch {
                      toast({
                        state: "proposed",
                        text: interpolate(copy.revokeFailed, {
                          name: key.name,
                        }),
                      });
                    }
                  }}
                />
              </li>
            ))}
          </ul>
        )}
      </section>

      <p className={styles.notice}>
        <span className={styles.noticeIcon}>
          <Icon name="shield" />
        </span>
        <span>{copy.notice}</span>
      </p>
    </>
  );
}
