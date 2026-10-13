"use client";

import { useRef, useState } from "react";
import { CommandBar, CommandDialog, type CommandMode } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import { IntentKeys } from "@/lib/v2/intent-keys";
import type { KeepResult, SpaceSummary } from "@/lib/v2/data/types";
import { KeyScopeBoundary, useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import type { PlaceRoute } from "@/lib/v2/places";
import { useSource, useViewer } from "../_lib/data";
import { useOverlays } from "../_lib/overlays";
import { useUndo } from "../_lib/undo";
import { answerText, useAsk } from "../_lib/use-ask";
import { useRemember } from "../_lib/use-remember";
import { AskPanel } from "./ask-panel";
import { MobileTabBar } from "./mobile-bars";
import { isOnV1, OnV1Notice } from "./on-v1-notice";
import { RememberPanel } from "./remember-panel";
import { useToast, type ShowToast } from "./toasts";
import styles from "./command-center.module.css";

/** How long the seal shows after a Keep before the overlay closes (the stamp is 280 ms). */
const SEAL_MS = 700;

/**
 * ⌘K: Ask or remember (plan §6.4; Ask.png, Remember.png). One field;
 * Tab switches the mode. Ask streams a cited answer and ⌘↵ keeps it as
 * a memory; Remember keeps on Enter (or ⌘↵) after the near-duplicate
 * check. CommandDialog (Base UI) traps focus, returns it on close and
 * closes on Escape. Opens from the rail, ⌘K and the phone's Ask tab.
 * In a space still on V1 it offers neither: its V2 record takes no new
 * words yet (policy space_on_v1), so it says so and links to Today.
 */
export function CommandCenter({
  space,
  spaces,
  route,
  waiting = 0,
}: {
  space: SpaceSummary;
  spaces: SpaceSummary[];
  /** The page under it, and what waits on the person: for the phone's tab bar. */
  route?: PlaceRoute;
  waiting?: number;
}) {
  const { command, setCommandOpen } = useOverlays();
  const close = () => setCommandOpen(false);
  return (
    <CommandDialog open={command.open} onOpenChange={setCommandOpen}>
      {/* Keyed by session: every open starts clean. */}
      <KeyScopeBoundary name="command" modal>
        <CommandBody
          key={command.session}
          space={space}
          spaces={spaces}
          initialMode={command.mode}
          initialQuery={command.query}
        />
      </KeyScopeBoundary>
      {/* A phone's Ask fills the screen above the tab bar (MobileAsk.png),
          with Ask the current tab; the page's own bar is behind the
          dialog, so this one is in it. Not shown above 640px. */}
      <MobileTabBar
        space={space}
        route={route}
        waiting={waiting}
        asking
        onAsk={close}
        onNavigate={close}
      />
    </CommandDialog>
  );
}

function useKeptToast() {
  const { t } = useLocale();
  const toast = useToast();
  const undo = useUndo();
  const copy = t.ledger.app.toast;
  return {
    /**
     * The toast for a Remember, or for keeping the near-duplicate offer
     * (`keep`). Both can be undone from it (and ⌘Z) while their receipt
     * is fresh: undoing a Remember withdraws the memory, undoing a keep
     * sends the proposal back to Review.
     */
    kept(
      result: KeepResult,
      space: SpaceSummary,
      command: "remember" | "keep",
    ) {
      const toastOptions: ShowToast =
        result.outcome === "proposed"
          ? {
              state: "proposed",
              text: interpolate(copy.proposed, { ref: result.ref }),
            }
          : {
              state: "kept",
              text:
                result.recompiled === null
                  ? interpolate(copy.kept, { ref: result.ref })
                  : count(
                      copy.keptRecompiledOne,
                      copy.keptRecompiled,
                      result.recompiled,
                      { ref: result.ref },
                    ),
            };
      if (result.receipt) {
        const entry = undo.record({
          space,
          command,
          ref: result.ref,
          receipt: result.receipt,
          restore: null,
        });
        toastOptions.undo = () => void undo.run(entry);
      }
      toast(toastOptions);
    },
    /** A command that didn't go through; one refused because `space` is still on V1 says so. */
    failed(err?: unknown, space?: SpaceSummary) {
      const failure = err === undefined ? null : toFailure(err);
      if (
        space &&
        failure?.kind === "refused" &&
        failure.code === "space_on_v1"
      ) {
        toast({ text: interpolate(copy.onV1, { space: space.name }) });
        return;
      }
      toast({ text: copy.failed });
    },
    copied() {
      toast({ text: copy.copied });
    },
  };
}

function CommandBody({
  space,
  spaces,
  initialMode,
  initialQuery,
}: {
  space: SpaceSummary;
  spaces: SpaceSummary[];
  initialMode: CommandMode;
  initialQuery: string;
}) {
  const { setCommandOpen } = useOverlays();
  const { t } = useLocale();
  const viewer = useViewer();
  const source = useSource();
  const onV1 = isOnV1(space);
  const [mode, setMode] = useState<CommandMode>(initialMode);
  const [query, setQuery] = useState(initialQuery);
  const [keepingAnswer, setKeepingAnswer] = useState(false);
  const [keptAnswer, setKeptAnswer] = useState<{
    ref: string;
    at: Date;
  } | null>(null);
  const intents = useRef(new IntentKeys());
  const ask = useAsk(space);
  const draft = useRemember(mode === "remember" && !onV1 ? query : "", space);
  const keepKey = useKeycap("command.keep");
  const commandKey = useKeycap("command.open");
  const notify = useKeptToast();

  const close = () => setCommandOpen(false);

  const keepDraft = async () => {
    try {
      const result = await draft.keep();
      if (!result) return;
      close();
      notify.kept(result, draft.space, "remember");
    } catch (err) {
      notify.failed(err, draft.space);
    }
  };

  const keepDuplicate = async () => {
    try {
      const result = await draft.keepDuplicate();
      if (!result) return;
      close();
      notify.kept(result, draft.space, "keep");
    } catch (err) {
      notify.failed(err, draft.space);
    }
  };

  // ⌘↵ keeps the answer as the person's own memory, citing the memories
  // it came from, so its trust is theirs (plan §5.11). One key per answer,
  // so a retry after a dropped connection keeps it once.
  const keepAnswer = async () => {
    if (ask.state.status !== "done" || keepingAnswer || keptAnswer) return;
    const statement = answerText(ask.state.parts, false);
    const cites = ask.state.sources.flatMap((s) =>
      s.memory ? [s.memory] : [],
    );
    const intent = `ask-keep:${space.id}:${ask.state.question}:${statement}`;
    setKeepingAnswer(true);
    try {
      const section =
        ask.state.sources.find((s) => s.section)?.section ??
        (await source.checkRemember({ space, statement })).section ??
        "decisions";
      const result = await source.remember({
        space,
        statement,
        section,
        idempotencyKey: intents.current.keyFor(intent),
        cites,
      });
      intents.current.settle(intent);
      if (result.outcome === "kept") {
        // The seal stamps where Keep was, then the overlay goes.
        setKeptAnswer({ ref: result.ref, at: source.now() });
        await new Promise((resolve) => setTimeout(resolve, SEAL_MS));
      }
      close();
      notify.kept(result, space, "remember");
    } catch (err) {
      notify.failed(err, space);
    } finally {
      setKeepingAnswer(false);
    }
  };

  const copyAnswer = async () => {
    if (ask.state.status !== "done") return;
    const { parts, sources } = ask.state;
    const text = [
      answerText(parts, true),
      "",
      ...sources.map((s) => `[${s.n}] ${s.statement} (${s.receipt.ref})`),
    ].join("\n");
    try {
      await navigator.clipboard.writeText(text);
      notify.copied();
    } catch {
      notify.failed();
    }
  };

  // ⌘K again closes; ⌘↵ keeps (the answer in Ask, the draft in Remember),
  // never in a space still on V1. The command scope is modal, so the key
  // reaches nothing else.
  useHotkey("command.open", () => close());
  useHotkey(
    "command.keep",
    () => {
      if (mode === "ask") void keepAnswer();
      else void keepDraft();
    },
    { enabled: !onV1 },
  );

  // A new question starts clean: no seal from the last answer's Keep.
  const askFresh = (text: string) => {
    setKeptAnswer(null);
    void ask.ask(text);
  };

  const switchToRemember = () => {
    setMode("remember");
    if (ask.state.status !== "idle") setQuery(ask.state.question);
  };

  const p = t.ledger.app.phone;
  // A phone's full-screen Ask names the space it asks (MobileAsk.png).
  const phoneTitle = (
    <h2 className={styles.phoneTitle}>
      {interpolate(mode === "ask" ? p.askTitle : p.rememberTitle, {
        space: space.name,
      })}
    </h2>
  );

  if (onV1) {
    // Neither Ask nor Remember: the field is read-only, Enter and Tab do
    // nothing, and the one way on is the space's Today.
    return (
      <>
        {phoneTitle}
        <CommandBar
          className={styles.bar}
          query={query}
          onQueryChange={setQuery}
          mode={mode}
          onModeChange={setMode}
          tabSwitchesMode={false}
          footer={false}
          placeholder={interpolate(t.ledger.app.command.onV1.title, {
            space: space.name,
          })}
          inputProps={{ readOnly: true }}
        >
          <OnV1Notice space={space} onOpen={close} />
        </CommandBar>
      </>
    );
  }

  const body =
    mode === "ask" ? (
      ask.state.status === "idle" ? null : (
        <AskPanel
          state={ask.state}
          space={space}
          viewer={viewer}
          keepKey={keepKey}
          keeping={keepingAnswer}
          kept={keptAnswer}
          onKeep={() => void keepAnswer()}
          onCopy={() => void copyAnswer()}
          onRemember={switchToRemember}
          onRetry={() => askFresh(query)}
          onOpen={close}
        />
      )
    ) : (
      <RememberPanel
        draft={draft}
        homeSlug={space.slug}
        // A space still on V1 takes no Remember (policy space_on_v1).
        spaces={spaces.filter((s) => !isOnV1(s))}
        viewer={viewer}
        keepKey={keepKey}
        commandKey={commandKey}
        onKeep={() => void keepDraft()}
        onKeepDuplicate={() => void keepDuplicate()}
      />
    );

  return (
    <>
      {phoneTitle}
      <CommandBar
        className={styles.bar}
        keepShortcut={keepKey}
        query={query}
        onQueryChange={setQuery}
        mode={mode}
        onModeChange={setMode}
        onSubmit={(text, current) => {
          if (current === "ask") askFresh(text);
          else void keepDraft();
        }}
      >
        {body}
      </CommandBar>
    </>
  );
}
