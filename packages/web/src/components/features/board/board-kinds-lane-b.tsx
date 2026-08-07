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
import { BoardKindLabel, BoardMemQuote } from "@memaxlabs/ui";
import { useActiveHub } from "@/lib/auth";
import { buildMemoryDetailPath } from "@/lib/route-helpers";
import { useResolveBoardSlot } from "@/hooks/use-board";
import { useInterpolate, useLocale } from "@/i18n";
import {
  registerBoardKind,
  type BoardKindBodyProps,
} from "./board-kind-registry";
import { boardKindEyebrow } from "./board-kind-visuals";

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

/**
 * 接下来 — the predictive to-do. Items are display + memory links
 * only: the card resolves through the standard ack verb (relabeled
 * "做完了 · 收下"), so there is no per-item state to manage and the
 * registry body needs no resolve access.
 */
function NextUpBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const items = asArray<NextUpItem>(slot.payload?.items);
  return (
    <>
      <BoardKindLabel star>{t.board.kindNextup}</BoardKindLabel>
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
            </div>
          </li>
        ))}
      </ol>
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
  "openq",
  makeWowBody((t) => t.board.kindOpenq),
  {
    purpose: (t) => t.board.openqPurpose,
    strip: (slot, t) => ({ label: t.board.kindOpenq, detail: slot.title }),
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
registerBoardKind(
  "musing",
  makeWowBody((t) => t.board.kindMusing),
  {
    purpose: (t) => t.board.musingPurpose,
    strip: (slot, t) => ({ label: t.board.kindMusing, detail: slot.title }),
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
