"use client";

import { useRef, useState } from "react";
import { CommandBar, CommandDialog, type CommandMode } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
import { IntentKeys } from "@/lib/v2/intent-keys";
import type { KeepResult, SpaceSummary } from "@/lib/v2/data/types";
import { KeyScopeBoundary, useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { useSource, useViewer } from "../_lib/data";
import { useOverlays } from "../_lib/overlays";
import { useUndo } from "../_lib/undo";
import { answerText, useAsk } from "../_lib/use-ask";
import { useRemember } from "../_lib/use-remember";
import { AskPanel } from "./ask-panel";
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
 */
export function CommandCenter({
  space,
  spaces,
}: {
  space: SpaceSummary;
  spaces: SpaceSummary[];
}) {
  const { command, setCommandOpen } = useOverlays();
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
    </CommandDialog>
  );
}

function useKeptToast() {
  const { t } = useLocale();
  const toast = useToast();
  const undo = useUndo();
  const copy = t.ledger.app.toast;
  return {
    kept(result: KeepResult, space: SpaceSummary) {
      if (result.outcome === "proposed") {
        toast({
          state: "proposed",
          text: interpolate(copy.proposed, { ref: result.ref }),
        });
        return;
      }
      const text =
        result.recompiled === null
          ? interpolate(copy.kept, { ref: result.ref })
          : count(
              copy.keptRecompiledOne,
              copy.keptRecompiled,
              result.recompiled,
              {
                ref: result.ref,
              },
            );
      const toastOptions: ShowToast = { state: "kept", text };
      // Keeping a proposal (the near-duplicate offer) can be undone; a
      // fresh Remember carries no receipt to undo by.
      if (result.receipt) {
        const entry = undo.record({
          space,
          command: "keep",
          ref: result.ref,
          receipt: result.receipt,
          restore: null,
        });
        toastOptions.undo = () => void undo.run(entry);
      }
      toast(toastOptions);
    },
    failed() {
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
  const viewer = useViewer();
  const source = useSource();
  const [mode, setMode] = useState<CommandMode>(initialMode);
  const [query, setQuery] = useState(initialQuery);
  const [keepingAnswer, setKeepingAnswer] = useState(false);
  const [keptAnswer, setKeptAnswer] = useState<{
    ref: string;
    at: Date;
  } | null>(null);
  const intents = useRef(new IntentKeys());
  const ask = useAsk(space);
  const draft = useRemember(mode === "remember" ? query : "", space);
  const keepKey = useKeycap("command.keep");
  const commandKey = useKeycap("command.open");
  const notify = useKeptToast();

  const close = () => setCommandOpen(false);

  const keepDraft = async () => {
    try {
      const result = await draft.keep();
      if (!result) return;
      close();
      notify.kept(result, draft.space);
    } catch {
      notify.failed();
    }
  };

  const keepDuplicate = async () => {
    try {
      const result = await draft.keepDuplicate();
      if (!result) return;
      close();
      notify.kept(result, draft.space);
    } catch {
      notify.failed();
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
      notify.kept(result, space);
    } catch {
      notify.failed();
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

  // ⌘K again closes; ⌘↵ keeps (the answer in Ask, the draft in Remember).
  useHotkey("command.open", () => close());
  useHotkey("command.keep", () => {
    if (mode === "ask") void keepAnswer();
    else void keepDraft();
  });

  // A new question starts clean: no seal from the last answer's Keep.
  const askFresh = (text: string) => {
    setKeptAnswer(null);
    void ask.ask(text);
  };

  const switchToRemember = () => {
    setMode("remember");
    if (ask.state.status !== "idle") setQuery(ask.state.question);
  };

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
        spaces={spaces}
        viewer={viewer}
        keepKey={keepKey}
        commandKey={commandKey}
        onKeep={() => void keepDraft()}
        onKeepDuplicate={() => void keepDuplicate()}
      />
    );

  return (
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
  );
}
