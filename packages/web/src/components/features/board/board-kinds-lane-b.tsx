"use client";

/**
 * Lane B kind renderers (plan 25 P2): 梦记 dreamlog, 接下来 nextup, 回声
 * echo, the rotating wow kinds (暗线 thread, 被遗忘的问题 openq,
 * 未观察的模式 pattern, memax 随想 musing) and 等你 decision_gate. Server
 * payloads
 * are structured data (see Go model.Board*Payload); all user-facing
 * copy is composed here through i18n. Every Lane B kind speaks in
 * memax's first person, so all labels carry the VoiceStar.
 */

import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { BoardKindLabel, BoardMemQuote } from "@memaxlabs/ui";
import { useActiveHub } from "@/lib/auth";
import { buildMemoryDetailPath } from "@/lib/route-helpers";
import { useResolveBoardSlot } from "@/hooks/use-board";
import { useInterpolate, useLocale } from "@/i18n";
import { trackEvent } from "@/lib/posthog";
import {
  buildAgentHandoffBundle,
  buildAgentHandoffPrompt,
  type HandoffCardMeta,
} from "@/lib/agent-handoff";
import {
  registerBoardKind,
  slotContentTime,
  type BoardKindBodyProps,
} from "./board-kind-registry";
import { boardKindEyebrow, boardKindVisual } from "./board-kind-visuals";

// Payload guards duplicated from board-kinds.tsx on purpose: the Lane A
// module side-effect-imports this one, so importing helpers back from it
// would create a cycle for three one-liners.
function asArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : [];
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

interface QuoteRef {
  memory_id?: string;
  when?: string;
  excerpt?: string;
}

function asQuote(value: unknown): QuoteRef {
  return typeof value === "object" && value !== null ? (value as QuoteRef) : {};
}

/**
 * Validity-guarded date label for a quote eyebrow — Intl.format throws
 * RangeError on an Invalid Date, which would take down the whole page
 * for one bad payload. Returns "" when the timestamp is unusable.
 */
function formatQuoteWhen(when: string, locale: string): string {
  if (!when) return "";
  const date = new Date(when);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(date);
}

/** Hook shared by every quote-rendering Lane B body: click → memory detail. */
function useOpenMemory() {
  const router = useRouter();
  const { activeHub } = useActiveHub();
  return (memoryId: string) =>
    router.push(buildMemoryDetailPath(activeHub?.hub.slug ?? null, memoryId));
}

function LaneBQuote({
  quote,
  suffix,
}: {
  quote: QuoteRef;
  /** Optional eyebrow suffix appended after the date ("你问自己"). */
  suffix?: string;
}) {
  const { locale } = useLocale();
  const openMemory = useOpenMemory();
  const memoryId = asString(quote.memory_id);
  const whenLabel = formatQuoteWhen(asString(quote.when), locale);
  const when = [whenLabel, suffix].filter(Boolean).join(" · ");
  return (
    <BoardMemQuote
      when={when || undefined}
      onClick={memoryId ? () => openMemory(memoryId) : undefined}
    >
      “{asString(quote.excerpt)}”
    </BoardMemQuote>
  );
}

function DreamlogBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("dreamlog")}>
        {t.board.kindDreamlog}
      </BoardKindLabel>
      <p className="m-0 text-[14px] text-fg-1">
        {asString(slot.payload?.body)}
      </p>
    </>
  );
}

function EchoBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const body = asString(slot.payload?.body);
  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("echo")}>
        {t.board.kindEcho}
      </BoardKindLabel>
      {body ? <p className="m-0 mb-2 text-[14px] text-fg-1">{body}</p> : null}
      <LaneBQuote
        quote={asQuote(slot.payload?.then)}
        suffix={t.board.echoThen}
      />
      <div
        className="my-1 text-center text-[12px] leading-none"
        style={{ color: "var(--signature)" }}
        aria-hidden="true"
      >
        <span>✦</span>
      </div>
      <LaneBQuote quote={asQuote(slot.payload?.now)} suffix={t.board.echoNow} />
    </>
  );
}

/**
 * Shared renderer for the wow rotation (thread/openq/pattern/musing):
 * first-person body + the quoted receipts behind it. Only the label
 * differs per kind.
 */
function makeWowBody(
  labelOf: (t: { board: Record<string, string> }) => string,
) {
  return function WowBody({ slot }: BoardKindBodyProps) {
    const { t } = useLocale();
    const quotes = asArray<QuoteRef>(slot.payload?.quotes);
    return (
      <>
        <BoardKindLabel star {...boardKindEyebrow(slot.kind)}>
          {labelOf(t)}
        </BoardKindLabel>
        <p className="m-0 text-[14px] text-fg-1">
          {asString(slot.payload?.body)}
        </p>
        {quotes.length > 0 ? (
          <div className="mt-2 flex flex-col gap-1.5">
            {quotes.map((quote, index) => (
              <LaneBQuote
                key={asString(quote.memory_id) || index}
                quote={quote}
              />
            ))}
          </div>
        ) : null}
      </>
    );
  };
}

interface NextUpItem {
  title?: string;
  why?: string;
  quotes?: unknown;
}

/** Quiet per-item / per-card handoff verb — never competes with content. */
function HandoffButton({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="text-[11.5px] font-medium text-fg-4 transition-colors hover:text-fg-2"
    >
      {label}
    </button>
  );
}

/**
 * 接下来 — the predictive to-do. Items are display + memory links
 * only: the card resolves through the standard ack verb (relabeled
 * "做完了 · 收下"), so there is no per-item state to manage and the
 * registry body needs no resolve access.
 *
 * Each item also carries an agent handoff: the item IS a task with
 * receipts, so it can be handed to a coding agent as a written brief
 * instead of the user retyping the context. The prompt is assembled
 * client-side from the payload already on the card (see
 * `@/lib/agent-handoff`) — no extra request, no second LLM pass.
 */
function NextUpBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { activeHub } = useActiveHub();
  const items = asArray<NextUpItem>(slot.payload?.items);
  const handoffMeta: HandoffCardMeta = {
    kind: slot.kind,
    hubName: activeHub?.hub.name,
    generatedAt: slotContentTime(slot),
  };

  // itemIndex is the 0-based item, or -1 for the whole-card bundle.
  const copyHandoff = async (prompt: string | null, itemIndex: number) => {
    if (!prompt) return;
    trackEvent("board_nextup_handoff_copied", {
      kind: slot.kind,
      item_index: itemIndex,
    });
    try {
      await navigator.clipboard.writeText(prompt);
      toast.success(t.board.copied);
    } catch {
      toast.error(t.board.copyFailed);
    }
  };

  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("nextup")}>
        {t.board.kindNextup}
      </BoardKindLabel>
      <ol className="m-0 flex list-none flex-col gap-2.5 p-0">
        {items.map((item, index) => (
          <li key={index} className="flex gap-2">
            <span
              className="mt-px shrink-0 text-[12px] font-semibold tabular-nums text-fg-4"
              aria-hidden="true"
            >
              {index + 1}
            </span>
            <div className="min-w-0 flex-1">
              <p className="m-0 text-[14px] font-semibold text-fg-1">
                {asString(item.title)}
              </p>
              {asString(item.why) ? (
                <p className="m-0 mt-0.5 text-[12.5px] text-fg-3">
                  {asString(item.why)}
                </p>
              ) : null}
              {asArray<QuoteRef>(item.quotes).length > 0 ? (
                <div className="mt-1.5 flex flex-col gap-1.5">
                  {asArray<QuoteRef>(item.quotes).map((quote, quoteIndex) => (
                    <LaneBQuote
                      key={asString(quote.memory_id) || quoteIndex}
                      quote={quote}
                    />
                  ))}
                </div>
              ) : null}
              <div className="mt-1 flex justify-end">
                <HandoffButton
                  label={t.board.nextupHandoff}
                  onClick={() =>
                    void copyHandoff(
                      buildAgentHandoffPrompt(item, handoffMeta, t),
                      index,
                    )
                  }
                />
              </div>
            </div>
          </li>
        ))}
      </ol>
      {items.length > 1 ? (
        <div className="mt-1.5 flex justify-end">
          <HandoffButton
            label={interpolate(t.board.nextupHandoffAll, { n: items.length })}
            onClick={() =>
              void copyHandoff(
                buildAgentHandoffBundle(items, handoffMeta, t),
                -1,
              )
            }
          />
        </div>
      ) : null}
    </>
  );
}

interface GateOption {
  id?: string;
  label?: string;
}

function DecisionGateBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { hubFilter } = useActiveHub();
  const resolve = useResolveBoardSlot(hubFilter ?? undefined);
  const context = asString(slot.payload?.context);
  const sourceAgent = asString(slot.payload?.source_agent);
  const options = asArray<GateOption>(slot.payload?.options);
  // Once the gate is settled (someone chose, or it was dismissed) the
  // options stay visible as the record of what was on the table, but
  // they can't be re-fired — the receipt line is the card's live zone.
  const terminal = slot.state === "resolved" || slot.state === "dismissed";
  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("decision_gate")}>
        {t.board.kindGate}
      </BoardKindLabel>
      <p className="m-0 text-[14px] font-semibold text-fg-1">
        {asString(slot.payload?.question)}
      </p>
      {context ? (
        <p className="m-0 mt-1 text-[12.5px] text-fg-3">{context}</p>
      ) : null}
      {sourceAgent ? (
        <p className="m-0 mt-1 text-[11px] text-fg-4">
          {interpolate(t.board.gateFrom, { agent: sourceAgent })}
        </p>
      ) : null}
      <div className="mt-2.5 flex flex-col gap-1.5">
        {options.map((option, index) => {
          const id = asString(option.id);
          return (
            <button
              key={id || index}
              type="button"
              disabled={terminal || !id}
              onClick={() =>
                resolve.mutate({
                  slotKey: slot.slot_key,
                  action: "choose",
                  choice: id,
                })
              }
              className="w-full rounded-xl border border-border/40 px-3 py-2 text-left text-[13px] font-medium text-fg-1 transition-colors hover:bg-surface-1 disabled:cursor-default disabled:opacity-60 disabled:hover:bg-transparent"
            >
              {asString(option.label)}
            </button>
          );
        })}
      </div>
    </>
  );
}

/**
 * Team-native kinds (共识缺口 / 团队回声 / 谁知道这个). These only ever
 * reach a team hub's board: the server rotates them into the wow slot
 * for team hubs only, and drops any card whose cited memories don't
 * have the authorship its claim implies (two different members for a
 * gap or a team echo, one member for who-knows). So the renderer can
 * treat attribution as trustworthy — `author` is the roster name of
 * whoever wrote the quoted memory, never the model's guess.
 */
interface TeamQuoteRef extends QuoteRef {
  author?: string;
}

/** Author attribution, falling back to a generic role label. */
function teamSuffix(quote: TeamQuoteRef, fallback: string): string {
  const author = asString(quote.author);
  if (!author) return fallback;
  return fallback ? `${author} · ${fallback}` : author;
}

/**
 * Separator between the two sides of a team card. Carries the team
 * hue rather than signature violet — these cards are about people,
 * not about memax's own voice.
 */
function TeamDivider({ kind, mark }: { kind: string; mark: string }) {
  return (
    <div
      className="my-1 text-center text-[12px] leading-none"
      style={{ color: boardKindVisual(kind).dot }}
      aria-hidden="true"
    >
      <span>{mark}</span>
    </div>
  );
}

/**
 * 共识缺口 — two members, one subject, two incompatible readings. Reads
 * as a quote pair like 回声, but the axis is people instead of time, so
 * each side is labelled with who said it.
 */
function ConsensusGapBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const body = asString(slot.payload?.body);
  const sides = asArray<TeamQuoteRef>(slot.payload?.sides);
  const fallbacks = [t.board.consensusSideA, t.board.consensusSideB];
  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("consensus_gap")}>
        {t.board.kindConsensus}
      </BoardKindLabel>
      {body ? <p className="m-0 mb-2 text-[14px] text-fg-1">{body}</p> : null}
      {sides.map((side, index) => (
        <div key={asString(side.memory_id) || index}>
          {index > 0 ? <TeamDivider kind="consensus_gap" mark="↔" /> : null}
          <LaneBQuote
            quote={side}
            suffix={teamSuffix(side, fallbacks[index] ?? "")}
          />
        </div>
      ))}
    </>
  );
}

/**
 * 团队回声 — A asked, B answered months later, nobody connected them.
 * Same then/now payload as 回声 (the server reuses BoardEchoPayload),
 * but the eyebrow suffixes name the two members instead of addressing
 * one reader.
 */
function TeamEchoBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const body = asString(slot.payload?.body);
  const then = asQuote(slot.payload?.then) as TeamQuoteRef;
  const now = asQuote(slot.payload?.now) as TeamQuoteRef;
  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("team_echo")}>
        {t.board.kindTeamEcho}
      </BoardKindLabel>
      {body ? <p className="m-0 mb-2 text-[14px] text-fg-1">{body}</p> : null}
      <LaneBQuote
        quote={then}
        suffix={teamSuffix(then, t.board.teamEchoThen)}
      />
      <TeamDivider kind="team_echo" mark="✦" />
      <LaneBQuote quote={now} suffix={teamSuffix(now, t.board.teamEchoNow)} />
    </>
  );
}

/**
 * 谁知道这个 — routing, not insight. The holder line is the payload:
 * everything below it is the evidence that this person is the one to
 * ask. All quotes belong to that one member by server contract.
 */
function WhoKnowsBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const holder = asString(slot.payload?.holder);
  const quotes = asArray<QuoteRef>(slot.payload?.quotes);
  return (
    <>
      <BoardKindLabel star {...boardKindEyebrow("who_knows")}>
        {t.board.kindWhoKnows}
      </BoardKindLabel>
      {holder ? (
        <p className="m-0 mb-1 text-[14px] font-semibold text-fg-1">
          {interpolate(t.board.whoKnowsAsk, { name: holder })}
        </p>
      ) : null}
      <p className="m-0 text-[14px] text-fg-1">
        {asString(slot.payload?.body)}
      </p>
      {quotes.length > 0 ? (
        <div className="mt-2 flex flex-col gap-1.5">
          {quotes.map((quote, index) => (
            <LaneBQuote
              key={asString(quote.memory_id) || index}
              quote={quote}
            />
          ))}
        </div>
      ) : null}
    </>
  );
}

registerBoardKind("dreamlog", DreamlogBody, {
  purpose: (t) => t.board.dreamlogPurpose,
  strip: (_slot, t) => ({ label: t.board.kindDreamlog }),
});
registerBoardKind("echo", EchoBody, {
  purpose: (t) => t.board.echoPurpose,
  strip: (slot, t) => ({ label: t.board.kindEcho, detail: slot.title }),
  feedback: true,
});
registerBoardKind(
  "thread",
  makeWowBody((t) => t.board.kindThread),
  {
    purpose: (t) => t.board.threadPurpose,
    strip: (slot, t) => ({ label: t.board.kindThread, detail: slot.title }),
    feedback: true,
  },
);
registerBoardKind(
  "pattern",
  makeWowBody((t) => t.board.kindPattern),
  {
    purpose: (t) => t.board.patternPurpose,
    strip: (slot, t) => ({ label: t.board.kindPattern, detail: slot.title }),
    feedback: true,
  },
);
registerBoardKind("nextup", NextUpBody, {
  purpose: (t) => t.board.nextupPurpose,
  // slot.title IS the first item's title (server contract), so the
  // collapsed strip shows the top prediction.
  strip: (slot, t) => ({ label: t.board.kindNextup, detail: slot.title }),
  actions: { ack: (t) => t.board.nextupAck },
  feedback: true,
});
registerBoardKind("decision_gate", DecisionGateBody, {
  purpose: (t) => t.board.gatePurpose,
  strip: (slot, t) => ({ label: t.board.kindGate, detail: slot.title }),
  hideDefaultActions: true,
});

// Team-native kinds. Registered last because they arrived last; they
// share the wow slot with the personal rotation and behave the same
// way in the action row — a claim about the team can be wrong, so all
// three carry the 准/不准 verbs.
registerBoardKind("consensus_gap", ConsensusGapBody, {
  purpose: (t) => t.board.consensusPurpose,
  strip: (slot, t) => ({ label: t.board.kindConsensus, detail: slot.title }),
  feedback: true,
});
registerBoardKind("team_echo", TeamEchoBody, {
  purpose: (t) => t.board.teamEchoPurpose,
  strip: (slot, t) => ({ label: t.board.kindTeamEcho, detail: slot.title }),
  feedback: true,
});
registerBoardKind("who_knows", WhoKnowsBody, {
  purpose: (t) => t.board.whoKnowsPurpose,
  strip: (slot, t) => ({ label: t.board.kindWhoKnows, detail: slot.title }),
  feedback: true,
});
