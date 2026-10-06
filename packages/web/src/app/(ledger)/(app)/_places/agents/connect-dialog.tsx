"use client";

import { useRef, useState, type KeyboardEvent } from "react";
import { Dialog } from "@base-ui/react/dialog";
import {
  AgentStamp,
  Button,
  Segmented,
  formatNodes,
  useLedger,
  type Autonomy,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { isComposing } from "@/lib/v2/keymap/keymap";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import { isRaise, type AgentConnectionView } from "@/lib/v2/data/agents";
import { useSource, useSpaces } from "../../_lib/data";
import { useSpaceView } from "../../_lib/space-context";
import { useToast } from "../../_components/toasts";
import { usePlace } from "../place";
import {
  AgentTiles,
  CONNECTABLE,
  CopyLine,
  ReadsStep,
  cliName,
  isChatAgent,
  type Connectable,
} from "./connect-steps";
import styles from "./connect.module.css";

/** The remote MCP server agents add by hand (HANDOFF §6, MCP). */
export const MCP_URL = "https://mcp.memax.app/mcp";

/**
 * The first agent to offer: one that isn't connected yet and runs in a
 * terminal (the command is the quickest way in), else any that isn't.
 */
function firstChoice(
  connected: AgentConnectionView[],
  surfaceOf: (key: string) => string,
): Connectable {
  const free = CONNECTABLE.filter(
    (key) => !connected.some((a) => a.agent === key),
  );
  return (
    free.find((key) => surfaceOf(key) === "cli") ?? free[0] ?? CONNECTABLE[0]
  );
}

/**
 * Connect an agent (ConnectAgent.png), a layer over Agents.
 *
 * There is no HTTP endpoint that creates a connection, by design: a
 * connection is made by the agent itself, through OAuth consent (remote
 * MCP) or `memax-cli connect` (device authorization), so the credential
 * and the person's consent travel together. This overlay guides the
 * person to that step and calls no create API: its primary action copies
 * the command (or the server's address) and closes. The new agent then
 * shows up in Agents on its own.
 *
 * Base UI's Dialog brings the focus trap, Escape, outside press, scroll
 * lock and focus return; a modal key scope silences the page's keys.
 */
export function ConnectAgentDialog({
  open,
  onOpenChange,
  connected,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  connected: AgentConnectionView[];
}) {
  const { t } = useLocale();
  const copy = t.ledger.agents.connect;
  return (
    <Dialog.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        {/* Keyed by opening, so each open starts from the defaults. */}
        {open ? (
          <ConnectBody
            connected={connected}
            onClose={() => onOpenChange(false)}
            title={copy.title}
          />
        ) : null}
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function ConnectBody({
  connected,
  onClose,
  title,
}: {
  connected: AgentConnectionView[];
  onClose: () => void;
  title: string;
}) {
  const { t } = useLocale();
  const copy = t.ledger.agents.connect;
  const { agents, strings } = useLedger();
  const source = useSource();
  const toast = useToast();
  const { space } = useSpaceView();
  const place = usePlace();
  const spaces = useSpaces().data ?? [space];
  const surfaceOf = (key: string) => agents[key]?.surface ?? "cli";
  const [agent, setAgent] = useState<Connectable>(() =>
    firstChoice(connected, surfaceOf),
  );
  const start = source.newAgentAutonomy(space);
  const [autonomy, setAutonomy] = useState<Autonomy>(start);
  const [chosen, setChosen] = useState<string[]>([space.slug]);
  const selectedRef = useRef<HTMLButtonElement | null>(null);

  const name = agents[agent]?.name ?? agent;
  const chat = isChatAgent(agents, agent);
  const level = strings.autonomy[autonomy];
  // From the CLI an agent connects at most at the space's default; a
  // lower level is a flag, a higher one is raised here afterwards.
  const command = [
    `npx memax-cli connect ${cliName(agent)}`,
    ...chosen.map((slug) => `--space ${slug}`),
    ...(isRaise(autonomy, start) ? [`--autonomy ${autonomy}`] : []),
  ].join(" ");

  const copyText = async (text: string, done: string) => {
    try {
      await navigator.clipboard.writeText(text);
      // Waiting on the agent to connect: the neutral working mark.
      toast({ state: "working", text: done });
      return true;
    } catch {
      toast({ text: copy.copyFailed });
      return false;
    }
  };
  const connect = async () => {
    const ok = chat
      ? await copyText(MCP_URL, interpolate(copy.copiedUrl, { agent: name }))
      : await copyText(command, interpolate(copy.copied, { agent: name }));
    if (ok) onClose();
  };
  // ↵ connects from the choices (the tiles, the levels, the spaces);
  // buttons keep their own Enter.
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "Enter" || isComposing(event.nativeEvent)) return;
    if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)
      return;
    const target = event.target as HTMLElement;
    const role = target.getAttribute("role");
    const choice =
      role === "radio" ||
      (target instanceof HTMLInputElement && target.type === "checkbox");
    if (!choice) return;
    event.preventDefault();
    void connect();
  };

  return (
    <Dialog.Popup
      className={styles.layer}
      initialFocus={selectedRef}
      aria-labelledby="connect-title"
    >
      <KeyScopeBoundary name="connect-agent" modal>
        <section className={styles.dialog} onKeyDown={onKeyDown}>
          <header className={styles.head}>
            <div className={styles.titles}>
              <Dialog.Title id="connect-title" className={styles.title}>
                {title}
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
              <AgentTiles
                selected={agent}
                onSelect={setAgent}
                connected={connected}
                selectedRef={selectedRef}
              />
            </div>

            <div className={styles.step}>
              <p className="mx-section-label">{copy.may}</p>
              <Segmented<Autonomy>
                label={interpolate(copy.mayLabel, { agent: name })}
                value={autonomy}
                onChange={setAutonomy}
                options={(["read", "propose", "write"] as const).map((v) => ({
                  value: v,
                  label: strings.autonomy[v],
                  hint: strings.autonomy[`${v}Hint`],
                }))}
              />
              <p className={styles.levelText}>
                <strong className={styles.levelName}>{`${level}.`}</strong>{" "}
                {interpolate(copy.level[autonomy], { agent: name })}
                {isRaise(start, autonomy)
                  ? ` ${interpolate(copy.connectsAt, {
                      level: strings.autonomy[start],
                    })}`
                  : null}
              </p>
            </div>

            <div className={styles.step}>
              <p className="mx-section-label">{copy.reads}</p>
              <ReadsStep
                agent={name}
                autonomy={autonomy}
                spaces={spaces}
                chosen={chosen}
                onToggle={(slug) =>
                  setChosen((prev) =>
                    prev.includes(slug)
                      ? prev.filter((s) => s !== slug)
                      : [...prev, slug],
                  )
                }
                target={source.targetFor(space, agent)}
              />
            </div>

            <div className={styles.step}>
              <p className="mx-section-label">
                {interpolate(chat ? copy.runsChat : copy.runs, { agent: name })}
              </p>
              {chat ? (
                <>
                  <CopyLine
                    text={MCP_URL}
                    label={copy.copyUrl}
                    onCopy={() =>
                      void copyText(
                        MCP_URL,
                        interpolate(copy.copiedUrl, { agent: name }),
                      )
                    }
                  />
                  <p className={`mx-meta ${styles.plain}`}>
                    {interpolate(copy.chat, { agent: name })}
                  </p>
                </>
              ) : (
                <>
                  <CopyLine
                    text={command}
                    label={copy.copyCommand}
                    onCopy={() =>
                      void copyText(
                        command,
                        interpolate(copy.copied, { agent: name }),
                      )
                    }
                  />
                  <p className={`mx-meta ${styles.plain}`}>
                    {formatNodes(copy.byHand, {
                      url: <code className="mx-code">{MCP_URL}</code>,
                    })}
                  </p>
                </>
              )}
            </div>
          </div>

          <footer className={styles.foot}>
            <span className={`mx-meta ${styles.receipts}`}>
              {formatNodes(copy.receiptsAs, {
                stamp: <AgentStamp agent={agent} size="sm" />,
              })}
            </span>
            <Dialog.Close render={<Button variant="quiet" />}>
              {copy.cancel}
            </Dialog.Close>
            <Button variant="primary" kbd="↵" onClick={() => void connect()}>
              {interpolate(copy.go, { agent: name })}
            </Button>
          </footer>
        </section>
      </KeyScopeBoundary>
    </Dialog.Popup>
  );
}
