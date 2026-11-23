"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { AgentConnectionView } from "@/lib/v2/data/agents";
import type { ReviewItem, ReviewQueue } from "@/lib/v2/data/review";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useToast } from "../../_components/toasts";
import { useSource } from "../../_lib/data";
import { recordKeys } from "../../_lib/records";
import {
  agentKeys,
  patchAgent,
  refreshAfterAgentCommand,
  useMyAgents,
} from "../agents/queries";
import type { RecordsView } from "../records-view";
import type { ReviewController } from "./use-review";
import styles from "./review.module.css";

/** A proposal a bulk Keep may take: not a conflict, not a kept memory's flag. */
export function keepableAtOnce(item: ReviewItem): boolean {
  return item.lifecycle === "proposed" && item.state === "proposed";
}

/**
 * The person's own agents that still propose in this space and could
 * keep: active, theirs, at Propose here, with a credential that allows
 * Write. Cloud agents never write, and an API key proposes at most.
 */
export function agentsToRaise(
  agents: readonly AgentConnectionView[],
  space: SpaceSummary,
): AgentConnectionView[] {
  return agents.filter(
    (a) =>
      a.mine &&
      a.state === "active" &&
      a.surface !== "cloud" &&
      a.maxAutonomy === "write" &&
      a.spaces.some((s) => s.slug === space.slug && s.autonomy === "propose"),
  );
}

type Step = "idle" | "keep" | "agents";

/**
 * Review's two shortcuts for someone who doesn't want a queue: keep
 * every proposal waiting, and let their own agents keep what they write
 * from now on (their level here goes to Write). Each asks once before it
 * acts. Both are the ordinary commands (a bulk Keep, an agent's level),
 * so policy still decides each one: a conflict, a proposal the judge is
 * checking or one that needs the person's own look stays in the queue.
 */
export function BulkActions({
  view,
  review,
}: {
  view: RecordsView;
  review: ReviewController;
}) {
  const { space, l } = view;
  const copy = l.review.bulk;
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const mine = useMyAgents();
  const [keys] = useState(() => new IntentKeys());
  const [step, setStep] = useState<Step>("idle");
  const [pending, setPending] = useState(false);

  const { queue } = review;
  const loaded = review.all.filter(keepableAtOnce);
  const raise = agentsToRaise(mine.data ?? [], space);
  if (!review.canDecide || (loaded.length === 0 && raise.length === 0)) {
    return null;
  }

  /** The whole queue, every page, as the server lists it now. */
  const wholeQueue = async (): Promise<ReviewItem[]> => {
    let pages: ReviewQueue[] = queue.data?.pages ?? [];
    let more = queue.hasNextPage;
    while (more) {
      const next = await queue.fetchNextPage();
      if (next.isError) throw next.error;
      pages = next.data?.pages ?? pages;
      more = next.hasNextPage;
    }
    return pages
      .flatMap((page) => page.items)
      .filter((item) => !review.state.done[item.ref])
      .filter(keepableAtOnce);
  };

  const keepAll = async () => {
    setPending(true);
    const intent = `keep-all:${space.slug}`;
    try {
      const items = await wholeQueue();
      const result = await source.review.keepAll({
        space,
        items,
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      const n = result.kept.length;
      toast(
        n === 0
          ? { state: "proposed", text: copy.keptNone }
          : result.left === 0
            ? { state: "kept", text: interpolate(copy.kept, { n }) }
            : {
                state: "kept",
                text: interpolate(copy.keptSome, { n, left: result.left }),
              },
      );
    } catch {
      // The key stays: pressing again is the same command.
      toast({ text: copy.failed });
    } finally {
      // The queue, the rail's count and Memories, as the server has them.
      await queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
      setPending(false);
      setStep("idle");
    }
  };

  const letAgents = async () => {
    setPending(true);
    const results = await Promise.allSettled(
      raise.map((agent) =>
        source.setAutonomy({
          agent: agent.id,
          space,
          autonomy: "write",
          idempotencyKey: keys.keyFor(`raise:${agent.id}:${space.slug}`),
        }),
      ),
    );
    let done = 0;
    results.forEach((result, i) => {
      if (result.status !== "fulfilled") return;
      done += 1;
      keys.settle(`raise:${raise[i]!.id}:${space.slug}`);
      patchAgent(queryClient, source.kind, result.value);
    });
    refreshAfterAgentCommand(queryClient, source.kind);
    void queryClient.invalidateQueries({
      queryKey: agentKeys.all(source.kind),
    });
    const left = raise.length - done;
    toast(
      done === 0
        ? { text: copy.agentsFailed }
        : left === 0
          ? { text: copy.agentsDone }
          : { text: interpolate(copy.agentsSome, { n: done, left }) },
    );
    setPending(false);
    setStep("idle");
  };

  if (step === "keep") {
    return (
      <div className={styles.bulk} role="group" aria-label={copy.label}>
        <Button size="sm" pending={pending} onClick={() => void keepAll()}>
          {interpolate(copy.keepAllConfirm, { n: loaded.length })}
        </Button>
        <Button
          variant="quiet"
          size="sm"
          disabled={pending}
          onClick={() => setStep("idle")}
        >
          {copy.cancel}
        </Button>
      </div>
    );
  }
  if (step === "agents") {
    return (
      <div className={styles.bulk} role="group" aria-label={copy.label}>
        <p className={styles.bulkDetail}>{copy.letAgentsDetail}</p>
        <Button size="sm" pending={pending} onClick={() => void letAgents()}>
          {copy.letAgentsConfirm}
        </Button>
        <Button
          variant="quiet"
          size="sm"
          disabled={pending}
          onClick={() => setStep("idle")}
        >
          {copy.cancel}
        </Button>
      </div>
    );
  }
  return (
    <div className={styles.bulk} role="group" aria-label={copy.label}>
      {loaded.length > 0 ? (
        <Button variant="secondary" size="sm" onClick={() => setStep("keep")}>
          {interpolate(copy.keepAll, { n: loaded.length })}
        </Button>
      ) : null}
      {raise.length > 0 ? (
        <Button variant="quiet" size="sm" onClick={() => setStep("agents")}>
          {copy.letAgents}
        </Button>
      ) : null}
    </div>
  );
}
