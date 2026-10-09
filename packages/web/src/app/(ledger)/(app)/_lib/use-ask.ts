"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { AnswerPart, AskSource, SpaceSummary } from "@/lib/v2/data/types";
import { useSource } from "./data";

interface Answer {
  question: string;
  sources: AskSource[];
  parts: AnswerPart[];
}

export type AskState =
  | { status: "idle" }
  | ({ status: "streaming" } & Answer)
  | ({ status: "done" } & Answer)
  | { status: "none"; question: string }
  /** Answers are off on the server: what matched, without an answer. */
  | { status: "off"; question: string; sources: AskSource[] }
  /** The plan's asks this month are used up. */
  | { status: "limit"; question: string; limit: number; resetAt: string }
  | { status: "unavailable"; question: string }
  | { status: "failed"; question: string };

/** Joins neighbouring text runs, so the answer renders as few nodes as it can. */
function append(parts: AnswerPart[], part: AnswerPart): AnswerPart[] {
  const last = parts[parts.length - 1];
  if (part.kind === "text" && last?.kind === "text") {
    return [
      ...parts.slice(0, -1),
      { kind: "text", text: last.text + part.text },
    ];
  }
  return [...parts, part];
}

/** The answer as plain text, citations as [n]: what Keep and Copy use. */
export function answerText(parts: AnswerPart[], withCites: boolean): string {
  return parts
    .map((part) =>
      part.kind === "cite" ? (withCites ? ` [${part.n}]` : "") : part.text,
    )
    .join("")
    .replace(/\s+([.,;:])/g, "$1")
    .trim();
}

/** Streams a cited answer from the source; a new question or close aborts the last one. */
export function useAsk(space: SpaceSummary) {
  const source = useSource();
  const [state, setState] = useState<AskState>({ status: "idle" });
  const controller = useRef<AbortController | null>(null);

  const stop = useCallback(() => {
    controller.current?.abort();
    controller.current = null;
  }, []);

  useEffect(() => stop, [stop]);

  const ask = useCallback(
    async (question: string) => {
      const q = question.trim();
      if (!q) return;
      stop();
      const run = new AbortController();
      controller.current = run;
      setState({ status: "streaming", question: q, sources: [], parts: [] });
      try {
        for await (const event of source.ask({
          space,
          question: q,
          signal: run.signal,
        })) {
          if (run.signal.aborted) return;
          setState((prev) => {
            if (prev.status !== "streaming") return prev;
            switch (event.type) {
              case "sources":
                return { ...prev, sources: event.sources };
              case "part":
                return { ...prev, parts: append(prev.parts, event.part) };
              case "done":
                return { ...prev, status: "done" };
              case "none":
              case "unavailable":
                return { status: event.type, question: q };
              case "off":
                return { status: "off", question: q, sources: event.sources };
              case "limit":
                return {
                  status: "limit",
                  question: q,
                  limit: event.limit,
                  resetAt: event.resetAt,
                };
            }
          });
        }
        // A stream that ends without saying how is a failure, not an answer.
        if (!run.signal.aborted) {
          setState((prev) =>
            prev.status === "streaming"
              ? { status: "failed", question: q }
              : prev,
          );
        }
      } catch {
        if (!run.signal.aborted) setState({ status: "failed", question: q });
      }
    },
    [source, space, stop],
  );

  const reset = useCallback(() => {
    stop();
    setState({ status: "idle" });
  }, [stop]);

  return { state, ask, reset };
}
