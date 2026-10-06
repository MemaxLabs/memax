"use client";

import { useState } from "react";
import { CommandBar, CommandDialog, type CommandMode } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { count } from "@/lib/v2/copy";
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

  const keepAnswer = async () => {
    if (ask.state.status !== "done" || keepingAnswer) return;
    const statement = answerText(ask.state.parts, false);
    setKeepingAnswer(true);
    try {
      const check = await source.checkRemember({ space, statement });
      const result = await source.remember({
        space,
        statement,
        section: check.section ?? "decisions",
        idempotencyKey: crypto.randomUUID(),
      });
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
          onKeep={() => void keepAnswer()}
          onCopy={() => void copyAnswer()}
          onRemember={switchToRemember}
          onRetry={() => void ask.ask(query)}
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
        if (current === "ask") void ask.ask(text);
        else void keepDraft();
      }}
    >
      {body}
    </CommandBar>
  );
}
