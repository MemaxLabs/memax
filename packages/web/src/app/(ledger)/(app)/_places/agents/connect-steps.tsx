"use client";

import { useRef, type KeyboardEvent, type RefObject } from "react";
import {
  AgentStamp,
  Icon,
  SyncTarget,
  useLedger,
  type AgentSurface,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import type {
  AgentConnectionView,
  AgentTarget,
  Autonomy,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import styles from "./connect.module.css";

// The steps of Connect an agent (ConnectAgent.png): which agent, what it
// reads and where it runs. Step 2 (autonomy) is a Segmented in the dialog.

/** The agents Memax knows how to connect, in the board's order. */
export const CONNECTABLE = [
  "claude-code",
  "codex",
  "cursor",
  "chatgpt",
  "claude",
  "gemini",
  "copilot",
  "opencode",
] as const;

export type Connectable = (typeof CONNECTABLE)[number];

/** Chat apps connect only as a remote MCP server, with OAuth in the app. */
export function isChatAgent(
  agents: Record<string, { surface: string }>,
  key: string,
) {
  return agents[key]?.surface === "chat";
}

/** The CLI's name for an agent (spec AgentKind): the registry's "gemini" is "gemini-cli". */
export function cliName(key: string): string {
  return key === "gemini" ? "gemini-cli" : key;
}

/**
 * The tiles, as one radio group: arrows move and choose (four to a row,
 * so ↑ and ↓ jump a row), Home and End go to the ends.
 */
export function AgentTiles({
  selected,
  onSelect,
  connected,
  selectedRef,
}: {
  selected: Connectable;
  onSelect: (agent: Connectable) => void;
  connected: AgentConnectionView[];
  /** The chosen tile, for the dialog's first focus. */
  selectedRef: RefObject<HTMLButtonElement | null>;
}) {
  const { t } = useLocale();
  const { agents, strings } = useLedger();
  const copy = t.ledger.agents.connect;
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const move = (event: KeyboardEvent, from: number) => {
    const n = CONNECTABLE.length;
    const step: Partial<Record<string, number>> = {
      ArrowRight: 1,
      ArrowLeft: -1,
      ArrowDown: 4,
      ArrowUp: -4,
    };
    let to: number | undefined;
    if (event.key in step) to = (from + step[event.key]! + n) % n;
    else if (event.key === "Home") to = 0;
    else if (event.key === "End") to = n - 1;
    if (to === undefined || event.altKey || event.metaKey || event.ctrlKey)
      return;
    event.preventDefault();
    buttons.current[to]?.focus();
    onSelect(CONNECTABLE[to]!);
  };
  return (
    <div
      className={styles.tiles}
      role="radiogroup"
      aria-label={copy.whichLabel}
    >
      {CONNECTABLE.map((key, i) => {
        const on = key === selected;
        const conn = connected.find((a) => a.agent === key);
        const surface = (agents[key]?.surface ?? "cli") as AgentSurface;
        const surfaceLabel =
          (strings.surface as Record<string, string>)[surface] ?? surface;
        const note = conn
          ? conn.state === "paused"
            ? copy.paused
            : copy.connected
          : on
            ? interpolate(copy.notConnected, { surface: surfaceLabel })
            : surfaceLabel;
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

/** Step 3: the spaces it reads (at least one), the file it reads, and MCP. */
export function ReadsStep({
  agent,
  autonomy,
  spaces,
  chosen,
  onToggle,
  target,
}: {
  agent: string;
  autonomy: Autonomy;
  spaces: SpaceSummary[];
  chosen: string[];
  onToggle: (slug: string) => void;
  target: AgentTarget | null | undefined;
}) {
  const { t } = useLocale();
  const { strings, agents } = useLedger();
  const copy = t.ledger.agents;
  const kinds = t.ledger.app.frame.spaceKind;
  return (
    <div className={styles.box}>
      <fieldset className={styles.spaces}>
        <legend className="mx-sr">
          {interpolate(copy.connect.readsLabel, { agent })}
        </legend>
        {spaces.map((space) => {
          const on = chosen.includes(space.slug);
          const last = on && chosen.length === 1;
          return (
            <label key={space.slug} className={styles.space}>
              <input
                type="checkbox"
                checked={on}
                aria-disabled={last || undefined}
                aria-describedby={last ? "connect-one-space" : undefined}
                onChange={() => {
                  if (!last) onToggle(space.slug);
                }}
              />
              <span className={styles.spaceName}>{space.name}</span>
              <span className="mx-meta">{kinds[space.kind]}</span>
            </label>
          );
        })}
        <span id="connect-one-space" className="mx-sr">
          {copy.connect.oneSpace}
        </span>
      </fieldset>
      {target ? (
        <SyncTarget
          compact
          path={target.path}
          tool={
            target.sharedWith
              ? interpolate(copy.detail.compiles.sharedWith, {
                  agent: agents[target.sharedWith]?.name ?? target.sharedWith,
                })
              : ""
          }
          status={target.status}
        />
      ) : null}
      <div className={styles.live}>
        <span className={styles.liveIcon}>
          <Icon name="globe" />
        </span>
        <span>{copy.connect.live[autonomy]}</span>
        <span className="mx-meta">{copy.connect.oauth}</span>
      </div>
      {/* The level's word, for anyone reading the live line on its own. */}
      <span className="mx-sr">{strings.autonomy[autonomy]}</span>
    </div>
  );
}

/** One line to copy: the command, or the server's address. */
export function CopyLine({
  text,
  label,
  onCopy,
}: {
  text: string;
  label: string;
  onCopy: () => void;
}) {
  return (
    <div className={styles.command}>
      <span className={styles.prompt} aria-hidden="true">
        ›
      </span>
      <code className={styles.commandText}>{text}</code>
      <button
        type="button"
        className={styles.copy}
        aria-label={label}
        title={label}
        onClick={onCopy}
      >
        <Icon name="copy" size={14} />
      </button>
    </div>
  );
}
