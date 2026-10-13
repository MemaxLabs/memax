"use client";

import { useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Dialog } from "@base-ui/react/dialog";
import {
  AgentStamp,
  Button,
  formatNodes,
  Icon,
  useLedger,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { formatShortDate } from "@/lib/v2/copy";
import { refusalText } from "@/lib/v2/agents/copy";
import {
  apiHost,
  cloudHookFile,
  connectCloudCommand,
  nonDefaultApi,
} from "@/lib/v2/agents/cloud";
import {
  AgentCommandError,
  CLOUD_AGENTS,
  hasCloudStart,
  isStartKeyAgent,
  type AgentConnectionView,
  type CloudAgent,
  type CloudAgentResultView,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { isComposing } from "@/lib/v2/keymap/keymap";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import { useSource, useSpaces } from "../../_lib/data";
import { useSpaceView } from "../../_lib/space-context";
import { useToast } from "../../_components/toasts";
import { usePlace } from "../place";
import { CopyLine } from "./connect-steps";
import { agentKeys, refreshAfterAgentCommand, useMyAgents } from "./queries";
import styles from "./connect.module.css";

/** The cloud agent a person already connected to a space, whose key a new one replaces. */
export function cloudConnectionOf(
  mine: AgentConnectionView[],
  agent: CloudAgent,
  slug: string,
): AgentConnectionView | undefined {
  return mine.find(
    (c) =>
      isStartKeyAgent(c) &&
      c.mine &&
      c.agent === agent &&
      c.state !== "disconnected" &&
      c.credential.active &&
      c.spaces.some((s) => s.slug === slug),
  );
}

/**
 * Connect a cloud agent (cloud warm-start design §4.3), a layer over
 * Agents: from Connect an agent, or `?overlay=connect-cloud`. No board
 * draws it, so it follows ConnectAgent's card and parts.
 *
 * Unlike Connect an agent, it makes the connection itself: a cloud
 * sandbox can't sign in, so the person makes its start key here
 * (POST /v2/cloud-agents, at Read, for one space) and puts it where the
 * sandbox reads it. The key lives in this layer only, shown once: never
 * in the URL, a cache or a toast. A connected agent gets a new key
 * instead, which stops the old one at once.
 */
export function ConnectCloudDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        {/* Always rendered, as Connect an agent's: each opening starts afresh. */}
        <CloudBody onClose={() => onOpenChange(false)} />
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function newKey(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function CloudBody({ onClose }: { onClose: () => void }) {
  const { t, locale } = useLocale();
  const copy = t.ledger.agents.cloud;
  const { agents: registry } = useLedger();
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const { space } = useSpaceView();
  const place = usePlace();
  // A cloud agent starts in one project or team space on the V2 record.
  const spaces = (useSpaces().data ?? [space]).filter(
    (s) => s.onV2 !== false && s.kind !== "personal",
  );
  const mine = useMyAgents().data ?? [];
  const [agent, setAgent] = useState<CloudAgent>("claude-code");
  const [slug, setSlug] = useState<string | null>(
    () =>
      spaces.find((s) => s.slug === space.slug)?.slug ??
      spaces[0]?.slug ??
      null,
  );
  const [made, setMade] = useState<{
    agent: CloudAgent;
    slug: string;
    result: CloudAgentResultView;
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const intents = useRef(new IntentKeys(newKey));
  const selectedRef = useRef<HTMLButtonElement | null>(null);

  const chosen: SpaceSummary | null =
    spaces.find((s) => s.slug === slug) ?? null;
  const name = registry[agent]?.name ?? agent;
  const existing = chosen
    ? cloudConnectionOf(mine, agent, chosen.slug)
    : undefined;
  const starts = hasCloudStart(agent);
  const shown =
    made && made.agent === agent && made.slug === slug ? made : null;
  const vars = { agent: name, space: chosen?.name ?? "" };

  const copyText = async (text: string, done: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast({ text: done });
    } catch {
      toast({ text: t.ledger.agents.connect.copyFailed });
    }
  };

  const create = async () => {
    if (!chosen || !starts || busy || shown) return;
    const intent = existing
      ? `rekey:${existing.id}`
      : `cloud:${agent}:${chosen.slug}`;
    const idempotencyKey = intents.current.keyFor(intent);
    setBusy(true);
    setError(null);
    try {
      const result = existing
        ? await source.rekeyAgent({ agent: existing.id, idempotencyKey })
        : await source.connectCloudAgent({
            agent,
            space: chosen,
            idempotencyKey,
          });
      intents.current.settle(intent);
      setMade({ agent, slug: chosen.slug, result });
      void queryClient.invalidateQueries({
        queryKey: agentKeys.all(source.kind),
      });
      refreshAfterAgentCommand(queryClient, source.kind);
    } catch (err) {
      const refusal = err instanceof AgentCommandError ? err.refusal : "failed";
      // A refusal a retry would only repeat ends the intent.
      if (refusal !== "failed") intents.current.settle(intent);
      setError(
        refusal === "failed"
          ? copy.failed
          : refusalText(t.ledger.agents, refusal, {
              ...vars,
              level: "",
              role: chosen.role,
              command: "disconnect",
            }).text,
      );
    } finally {
      setBusy(false);
    }
  };

  // ↵ on a choice (a tile, a space) makes the key; buttons keep their own Enter.
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "Enter" || isComposing(event.nativeEvent)) return;
    if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)
      return;
    const target = event.target as HTMLElement;
    const choice =
      target.getAttribute("role") === "radio" ||
      (target instanceof HTMLInputElement && target.type === "radio");
    if (!choice) return;
    event.preventDefault();
    void create();
  };

  const hook = cloudHookFile(agent);
  const api = nonDefaultApi();
  const host = apiHost();
  const key = shown?.result.key ?? null;

  return (
    <Dialog.Popup
      className={styles.layer}
      initialFocus={selectedRef}
      aria-labelledby="connect-cloud-title"
    >
      <KeyScopeBoundary name="connect-cloud-agent" modal>
        <section className={styles.dialog} onKeyDown={onKeyDown}>
          <header className={styles.head}>
            <div className={styles.titles}>
              <Dialog.Title id="connect-cloud-title" className={styles.title}>
                {copy.title}
              </Dialog.Title>
              <span className="mx-meta">
                {interpolate(copy.to, { where: place.eyebrow })}
              </span>
            </div>
            <Dialog.Close
              render={
                <Button
                  variant="quiet"
                  size="sm"
                  icon="x"
                  aria-label={copy.close}
                />
              }
            />
          </header>

          <div className={styles.body}>
            <div className={styles.step}>
              <p className="mx-section-label">{copy.which}</p>
              <CloudTiles
                selected={agent}
                onSelect={(next) => {
                  setAgent(next);
                  setError(null);
                }}
                connected={(a) =>
                  !!chosen && !!cloudConnectionOf(mine, a, chosen.slug)
                }
                selectedRef={selectedRef}
              />
            </div>

            <div className={styles.step}>
              <p className="mx-section-label">{copy.where}</p>
              {spaces.length === 0 ? (
                <div className={styles.box}>
                  <p className={styles.note}>{copy.noSpaces}</p>
                </div>
              ) : (
                <div className={styles.box}>
                  <fieldset className={styles.spaces}>
                    <legend className="mx-sr">
                      {interpolate(copy.whereLabel, { agent: name })}
                    </legend>
                    {spaces.map((s) => (
                      <label key={s.slug} className={styles.radio}>
                        <input
                          type="radio"
                          name="cloud-space"
                          checked={slug === s.slug}
                          onChange={() => {
                            setSlug(s.slug);
                            setError(null);
                          }}
                        />
                        <span className={styles.spaceName}>{s.name}</span>
                      </label>
                    ))}
                  </fieldset>
                  <p className={`${styles.note} ${styles.plain}`}>
                    {copy.whereHint}
                  </p>
                </div>
              )}
            </div>

            {starts ? (
              <>
                <div className={styles.step}>
                  <p className="mx-section-label">{copy.key}</p>
                  <div className={styles.box} role={key ? "status" : undefined}>
                    <p className={styles.note}>
                      {key
                        ? copy.keyShown
                        : shown
                          ? copy.keyReplayed
                          : interpolate(
                              existing ? copy.keyConnected : copy.keyIntro,
                              vars,
                            )}
                    </p>
                    {key ? (
                      <div className={styles.secret}>
                        <code className={styles.secretText}>{key.secret}</code>
                        <Button
                          variant="secondary"
                          size="sm"
                          icon="copy"
                          onClick={() =>
                            void copyText(key.secret, copy.copiedKey)
                          }
                        >
                          {copy.copyKey}
                        </Button>
                      </div>
                    ) : null}
                    {key ? (
                      <p className={`${styles.note} ${styles.plain}`}>
                        {interpolate(copy.keyExpires, {
                          date: formatShortDate(
                            new Date(key.expiresAt),
                            place.timeZone,
                            locale,
                          ),
                        })}
                      </p>
                    ) : null}
                    {error ? (
                      <p className={styles.note} role="alert">
                        {error}
                      </p>
                    ) : null}
                  </div>
                </div>

                <div className={styles.step}>
                  <p className="mx-section-label">{copy.put}</p>
                  <ol className={styles.howto}>
                    {agent === "claude-code" ? (
                      <>
                        <li>{interpolate(copy.claudeNetwork, { host })}</li>
                        <li>{copy.claudeEnv}</li>
                      </>
                    ) : (
                      <>
                        <li>{copy.copilotSecret}</li>
                        <li>{interpolate(copy.copilotAllow, { host })}</li>
                      </>
                    )}
                    {api ? (
                      <li>{interpolate(copy.apiUrl, { url: api })}</li>
                    ) : null}
                  </ol>
                </div>

                {hook ? (
                  <div className={styles.step}>
                    <p className="mx-section-label">{copy.commit}</p>
                    <p className={`mx-meta ${styles.plain}`}>
                      {formatNodes(copy.commitIntro, {
                        path: <code className="mx-code">{hook.path}</code>,
                      })}{" "}
                      {agent === "claude-code"
                        ? copy.claudeGuard
                        : copy.copilotGuard}
                    </p>
                    <div className={styles.file}>
                      <pre className={styles.fileText}>{hook.json}</pre>
                      <button
                        type="button"
                        className={styles.fileCopy}
                        aria-label={copy.copyFile}
                        title={copy.copyFile}
                        onClick={() =>
                          void copyText(
                            hook.json,
                            interpolate(copy.copiedFile, { path: hook.path }),
                          )
                        }
                      >
                        <Icon name="copy" size={14} />
                      </button>
                    </div>
                    <p className={`mx-meta ${styles.plain}`}>{copy.merge}</p>
                    {chosen ? (
                      <CopyLine
                        text={connectCloudCommand(agent, chosen.slug)}
                        label={copy.copyCommand}
                        onCopy={() =>
                          void copyText(
                            connectCloudCommand(agent, chosen.slug),
                            copy.copiedCommand,
                          )
                        }
                      />
                    ) : null}
                    <p className={`mx-meta ${styles.plain}`}>
                      {copy.agentsMd}{" "}
                      {interpolate(copy.codeowners, { path: hook.path })}
                    </p>
                  </div>
                ) : null}
              </>
            ) : (
              <div className={styles.step}>
                <p className="mx-section-label">{copy.loads}</p>
                <div className={styles.box}>
                  <p className={styles.note}>{copy.codex}</p>
                </div>
              </div>
            )}
          </div>

          <footer className={styles.foot}>
            <span className={`mx-meta ${styles.receipts}`}>
              {formatNodes(copy.receiptsAs, {
                stamp: <AgentStamp agent={agent} size="sm" />,
              })}
            </span>
            {starts && !shown ? (
              <>
                <Dialog.Close render={<Button variant="quiet" />}>
                  {copy.cancel}
                </Dialog.Close>
                <Button
                  variant="primary"
                  kbd="↵"
                  pending={busy}
                  disabled={!chosen}
                  disabledReason={chosen ? undefined : copy.noSpaces}
                  onClick={() => void create()}
                >
                  {existing ? copy.rekey : copy.create}
                </Button>
              </>
            ) : (
              <Button variant="primary" onClick={onClose}>
                {copy.done}
              </Button>
            )}
          </footer>
        </section>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}

/** The cloud agents, as one radio group (arrows choose, Home and End go to the ends). */
function CloudTiles({
  selected,
  onSelect,
  connected,
  selectedRef,
}: {
  selected: CloudAgent;
  onSelect: (agent: CloudAgent) => void;
  connected: (agent: CloudAgent) => boolean;
  selectedRef: RefObject<HTMLButtonElement | null>;
}) {
  const { t } = useLocale();
  const copy = t.ledger.agents.cloud;
  const { agents } = useLedger();
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const move = (event: KeyboardEvent, from: number) => {
    const n = CLOUD_AGENTS.length;
    const step: Partial<Record<string, number>> = {
      ArrowRight: 1,
      ArrowDown: 1,
      ArrowLeft: -1,
      ArrowUp: -1,
    };
    let to: number | undefined;
    if (event.key in step) to = (from + step[event.key]! + n) % n;
    else if (event.key === "Home") to = 0;
    else if (event.key === "End") to = n - 1;
    if (to === undefined || event.altKey || event.metaKey || event.ctrlKey)
      return;
    event.preventDefault();
    buttons.current[to]?.focus();
    onSelect(CLOUD_AGENTS[to]!);
  };
  return (
    <div
      className={`${styles.tiles} ${styles.tiles3}`}
      role="radiogroup"
      aria-label={copy.whichLabel}
    >
      {CLOUD_AGENTS.map((key, i) => {
        const on = key === selected;
        const note = !hasCloudStart(key)
          ? copy.filesOnly
          : connected(key)
            ? copy.connected
            : copy.notConnected;
        return (
          <button
            key={key}
            ref={(el) => {
              buttons.current[i] = el;
              if (on) selectedRef.current = el;
            }}
            type="button"
            role="radio"
            aria-checked={on}
            tabIndex={on ? 0 : -1}
            className={on ? `${styles.tile} ${styles.tileOn}` : styles.tile}
            onClick={() => onSelect(key)}
            onKeyDown={(e) => move(e, i)}
          >
            <AgentStamp agent={key} decorative />
            <span className={styles.tileText}>
              <span className={styles.tileName}>
                {agents[key]?.name ?? key}
              </span>
              <span className="mx-meta">{note}</span>
            </span>
          </button>
        );
      })}
    </div>
  );
}
