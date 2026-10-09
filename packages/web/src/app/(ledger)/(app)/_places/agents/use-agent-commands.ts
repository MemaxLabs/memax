"use client";

import { useCallback, useEffect, useReducer, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useLedger } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import {
  autonomyReducer,
  hasUnsent,
  initialAutonomy,
  isRaising,
} from "@/lib/v2/agents/autonomy";
import { changedText, refusalText } from "@/lib/v2/agents/copy";
import {
  AgentCommandError,
  autonomyIn,
  type AgentConnectionView,
  type Autonomy,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useSource } from "../../_lib/data";
import { useSignInAgain } from "../../_lib/sign-in";
import { useToast } from "../../_components/toasts";
import { patchAgent, refreshAfterAgentCommand } from "./queries";

/** How long the control waits for the choices to settle before sending (arrowing through levels). */
export const AUTONOMY_SETTLE_MS = 350;

const DEV = process.env.NODE_ENV !== "production";

function newKey(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/**
 * The autonomy control for one agent in one space (plan §5.6):
 *
 * - The control moves the moment the person chooses, and the command
 *   goes once the choices settle (AUTONOMY_SETTLE_MS), one at a time.
 * - Lowering is optimistic: the caches and the toast say so at once,
 *   because the server always lets you lower your own agent (and an
 *   owner anyone's). A failure rolls it back.
 * - Raising waits for the server, which needs the person on the web
 *   (human_web). A refusal rolls the control back and says why, with
 *   "Sign in again" when it's the web assurance that's missing (a
 *   session from before migration 030, or a web app without
 *   WEB_SURFACE_SECRET in local development).
 */
export function useAutonomy({
  agent,
  space,
  settleMs = AUTONOMY_SETTLE_MS,
}: {
  agent: AgentConnectionView;
  space: SpaceSummary;
  settleMs?: number;
}) {
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const signInAgain = useSignInAgain();
  const { t } = useLocale();
  const { strings } = useLedger();
  const copy = t.ledger.agents;
  const confirmed = autonomyIn(agent, space.slug)?.autonomy ?? "read";
  const [state, dispatch] = useReducer(
    autonomyReducer,
    confirmed,
    initialAutonomy,
  );
  const stateRef = useRef(state);
  const agentRef = useRef(agent);
  useEffect(() => {
    stateRef.current = state;
    agentRef.current = agent;
  });

  // The server's level changed underneath (a refetch, another tab).
  useEffect(() => {
    dispatch({ type: "sync", confirmed });
  }, [confirmed]);

  const send = useCallback(() => {
    const s = stateRef.current;
    if (!hasUnsent(s)) return;
    const key = newKey();
    const to = s.shown;
    const from = s.confirmed;
    const raising = isRaising(s);
    const before = agentRef.current;
    const level = (a: Autonomy) => strings.autonomy[a];
    const vars = { agent: before.name, space: space.name };
    dispatch({ type: "send", key });

    const withLevel = (a: Autonomy): AgentConnectionView => ({
      ...before,
      spaces: before.spaces.map((x) =>
        x.slug === space.slug ? { ...x, autonomy: a } : x,
      ),
    });
    if (!raising) {
      patchAgent(queryClient, source.kind, withLevel(to));
      toast({ text: changedText(copy, to, vars) });
    }

    source
      .setAutonomy({
        agent: before.id,
        space,
        autonomy: to,
        idempotencyKey: key,
      })
      .then((next) => {
        const now = autonomyIn(next, space.slug)?.autonomy ?? to;
        dispatch({ type: "settled", key, autonomy: now });
        patchAgent(queryClient, source.kind, next);
        refreshAfterAgentCommand(queryClient, source.kind);
        if (raising) {
          toast({ text: changedText(copy, now, vars) });
        }
      })
      .catch((err: unknown) => {
        dispatch({ type: "rolledBack", key });
        patchAgent(queryClient, source.kind, withLevel(from));
        const refusal =
          err instanceof AgentCommandError ? err.refusal : "failed";
        const { text, signIn } = refusalText(copy, refusal, {
          agent: before.name,
          level: level(from),
          space: space.name,
          role: space.role,
          dev: DEV,
        });
        toast({
          // Ochre only when it waits on the person (sign in again).
          state: signIn ? "proposed" : undefined,
          text,
          action: signIn
            ? { label: copy.refused.signInAgain, onClick: signInAgain }
            : undefined,
        });
      });
  }, [copy, queryClient, signInAgain, source, space, strings, toast]);

  // Send once the person stops choosing, and again after a command if
  // they chose something else meanwhile.
  useEffect(() => {
    if (!hasUnsent(state)) return;
    const timer = setTimeout(send, settleMs);
    return () => clearTimeout(timer);
  }, [state, send, settleMs]);

  const choose = useCallback(
    (to: Autonomy) => dispatch({ type: "choose", to }),
    [],
  );
  return { shown: state.shown, raising: isRaising(state), choose };
}

type Command = "pause" | "resume" | "disconnect";

/** Pause, resume and disconnect, with the same toasts and refusals. */
export function useAgentCommand(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const signInAgain = useSignInAgain();
  const { t } = useLocale();
  const copy = t.ledger.agents;

  return useCallback(
    async (command: Command, agent: AgentConnectionView): Promise<boolean> => {
      const input = { agent: agent.id, idempotencyKey: newKey() };
      const vars = { agent: agent.name, space: space.name };
      try {
        const next =
          command === "pause"
            ? await source.pauseAgent(input)
            : command === "resume"
              ? await source.resumeAgent(input)
              : await source.disconnectAgent(input);
        patchAgent(queryClient, source.kind, next);
        refreshAfterAgentCommand(queryClient, source.kind);
        const said = {
          pause: copy.changed.paused,
          resume: copy.changed.resumed,
          disconnect: copy.changed.disconnected,
        }[command];
        toast({
          // Paused and disconnected are "off"; resuming is neutral news.
          state: command === "resume" ? undefined : "off",
          text: interpolate(said, vars),
        });
        return true;
      } catch (err) {
        const refusal =
          err instanceof AgentCommandError ? err.refusal : "failed";
        const { text, signIn } = refusalText(copy, refusal, {
          ...vars,
          level: "",
          role: space.role,
          command,
          dev: DEV,
        });
        toast({
          // Ochre only when it waits on the person (sign in again).
          state: signIn ? "proposed" : undefined,
          text,
          action: signIn
            ? { label: copy.refused.signInAgain, onClick: signInAgain }
            : undefined,
        });
        return false;
      }
    },
    [copy, queryClient, signInAgain, source, space, toast],
  );
}
