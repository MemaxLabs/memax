import {
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from "react";
import { Icon } from "../brand/icon";
import { Seal } from "../brand/seal";
import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";
import { format, formatNodes } from "../lib/format";
import { mergeRefs } from "../lib/merge-refs";
import { Button } from "../primitives/button";
import { AgentStamp } from "../provenance/agent-stamp";
import { Receipt } from "../provenance/receipt";
import { Diff } from "./diff";
import { StateMark } from "./state-mark";

export interface ReviewCardProps {
  /** The claim, italic until kept. */
  statement: string;
  /** Who proposed it. */
  agent: string;
  time?: string;
  /** The memory's ID ("M-0430"). */
  id?: string;
  /** The space it goes into. */
  space?: string;
  /** Where it came from ("session 3e1a"). */
  source?: string;
  /** The words the agent read, quoted. */
  evidence?: ReactNode;
  evidenceSource?: ReactNode;
  /** For an update to a kept memory: its current statement. Renders a word diff. */
  before?: string;
  beforeId?: string;
  /**
   * Turns on the quarantine notice. Set it whenever the claim came from text
   * the agent did not write (a web page, an email, an issue comment).
   */
  external?: ReactNode;
  /** The statement of the kept memory this contradicts. */
  conflictWith?: ReactNode;
  /** The card in focus in Review gets the stronger edge. */
  focused?: boolean;
  /**
   * Controlled: whether a person has kept it. Set it optimistically when Keep
   * is pressed and back to false on rollback. The seal stamps on the change to
   * kept, and the type settles back on the way out.
   */
  kept: boolean;
  /**
   * A command is in flight. When omitted, the card tracks the promise returned
   * by `onKeep` itself.
   */
  pending?: boolean;
  /**
   * Keep was pressed. May return a promise: the card stays pending until it
   * settles, and if it rejects the card stays unkept. The card never marks
   * itself kept; the caller does, through `kept`.
   */
  onKeep?: () => void | Promise<unknown>;
  onEdit?: () => void;
  onReject?: () => void;
  /** Undo a Keep (⌘Z). The Undo button only shows when this is given. */
  onUndo?: () => void;
  /** Initials of the person who kept it, for the receipt stamp. */
  keptBy?: string;
  /** Their name when it isn't the reader; the header then reads "Kept by {name}". */
  keptByName?: string;
  /** The date on the seal ("Oct 5"). */
  keptDate?: string;
  /** The kept memory's ID, when it differs from `id`. */
  keptId?: string;
  /** The receipt's time. Defaults to the locale's "just now". */
  keptTime?: string;
  /** The statement's language, when it differs from the page's. */
  lang?: string;
  className?: string;
  ref?: Ref<HTMLElement>;
}

function isThenable(value: unknown): value is PromiseLike<unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { then?: unknown }).then === "function"
  );
}

/**
 * A proposed memory waiting on a person: the claim, why the agent made it, what
 * it changes, and Keep · Edit · Reject. Keep stamps the seal and settles the
 * text from italic to roman; the footer becomes the person's receipt.
 */
export function ReviewCard({
  statement,
  agent,
  time,
  id,
  space,
  source,
  evidence,
  evidenceSource,
  before,
  beforeId,
  external,
  conflictWith,
  focused = true,
  kept,
  pending: pendingProp,
  onKeep,
  onEdit,
  onReject,
  onUndo,
  keptBy,
  keptByName,
  keptDate,
  keptId,
  keptTime,
  lang,
  className,
  ref,
}: ReviewCardProps) {
  const { strings, agents } = useLedger();
  const s = strings.review;
  const proposer = resolveAgent(agents, { agent }, strings.agent.fallback);
  const stateId = useId();

  // The seal stamps only on a change to kept while mounted, never when the
  // card is first drawn already kept.
  const [prevKept, setPrevKept] = useState(kept);
  const [stamping, setStamping] = useState(false);
  if (prevKept !== kept) {
    setPrevKept(kept);
    setStamping(kept);
  }

  const [keeping, setKeeping] = useState(false);
  const pending = pendingProp ?? keeping;

  // Keep and Undo replace each other, so the focused button disappears on every
  // change. If focus was in the card's actions and fell to the page, move it to
  // whatever replaced the button (or to the card itself).
  const articleRef = useRef<HTMLElement>(null);
  const keepRef = useRef<HTMLElement>(null);
  const undoRef = useRef<HTMLElement>(null);
  const focusInActions = useRef(false);
  useEffect(() => {
    if (!focusInActions.current) return;
    const active = document.activeElement;
    // Some engines keep pointing at the removed button; treat that as lost too.
    if (active && active !== document.body && active.isConnected) return;
    const next =
      (kept ? undoRef.current : keepRef.current) ?? articleRef.current;
    next?.focus();
  }, [kept]);

  const handleKeep = () => {
    if (kept || pending || !onKeep) return;
    const result: unknown = onKeep();
    if (isThenable(result)) {
      setKeeping(true);
      result.then(
        () => setKeeping(false),
        // The command failed and the caller left `kept` false, so nothing
        // changes here. The caller surfaces the error: its own promise rejected.
        () => setKeeping(false),
      );
    }
  };

  const headLabel = kept
    ? keptByName
      ? format(s.keptBy, { name: keptByName })
      : s.keptByYou
    : conflictWith
      ? s.conflict
      : strings.state.proposed;
  const pendingReceipt = [id, space ? format(s.into, { space }) : null, source]
    .filter(Boolean)
    .join(" · ");

  return (
    <article
      ref={mergeRefs(ref, articleRef)}
      className={cx(
        "mx-review",
        focused && "is-focused",
        kept && "is-kept",
        className,
      )}
      aria-busy={pending || undefined}
      tabIndex={-1}
    >
      <header className="mx-review-head">
        <StateMark
          id={stateId}
          state={kept ? "kept" : conflictWith ? "conflict" : "proposed"}
          label={headLabel}
          aria-live="polite"
        />
        <span className="mx-review-by">
          <AgentStamp agent={agent} size="sm" decorative />
          <span>{proposer.name}</span>
          {time ? <span className="mx-meta">· {time}</span> : null}
        </span>
      </header>
      {kept ? (
        <Seal
          date={keptDate ?? ""}
          id={keptId ?? id}
          size={76}
          animate={stamping}
          className="mx-review-seal"
        />
      ) : null}
      {external != null && !kept ? (
        <div className="mx-quarantine" role="note">
          <Icon name="shield" size={16} />
          <div>
            <strong>{s.external} </strong>
            {external}
          </div>
        </div>
      ) : null}
      <div className="mx-review-stmt">
        <p
          className="mx-review-statement is-proposed"
          lang={lang}
          aria-hidden={kept ? true : undefined}
          aria-describedby={kept ? undefined : stateId}
        >
          {before ? <Diff before={before} after={statement} /> : statement}
          <span className="mx-sr">
            {" "}
            {format(s.proposedBy, { name: proposer.name })}
          </span>
        </p>
        <p
          className="mx-review-statement is-kept"
          lang={lang}
          aria-hidden={kept ? undefined : true}
        >
          {statement}
        </p>
      </div>
      {before ? (
        <p className="mx-review-updates">
          {formatNodes(kept ? s.replaced : s.updates, {
            id: (
              <span className="mx-receipt-id">{beforeId ?? s.aKeptMemory}</span>
            ),
          })}
        </p>
      ) : null}
      {conflictWith != null && !kept ? (
        <div className="mx-review-conflict">
          <span className="mx-review-conflict-label">{s.keptNow}</span>
          <span className="mx-review-conflict-text">{conflictWith}</span>
        </div>
      ) : null}
      {evidence != null && !kept ? (
        <figure className="mx-evidence">
          <blockquote>{evidence}</blockquote>
          {evidenceSource != null ? (
            <figcaption className="mx-receipt">{evidenceSource}</figcaption>
          ) : null}
        </figure>
      ) : null}
      <footer className="mx-review-foot">
        {kept ? (
          <Receipt
            person={keptBy}
            name={keptByName ?? (keptBy ? strings.agent.you : undefined)}
            action={strings.receiptVerb.kept}
            time={keptTime ?? strings.time.justNow}
            id={keptId ?? id}
          />
        ) : (
          <span className="mx-receipt">{pendingReceipt}</span>
        )}
        <div
          className="mx-review-actions"
          onFocus={() => {
            focusInActions.current = true;
          }}
          onBlur={(event) => {
            // Focus moved somewhere on purpose. A removed button blurs to nowhere,
            // and then the effect above takes over.
            if (event.relatedTarget) focusInActions.current = false;
          }}
        >
          {/* Keyed, so React never reuses the focused Undo button as Reject. */}
          {kept ? (
            onUndo ? (
              <Button
                key="undo"
                ref={undoRef}
                variant="quiet"
                size="sm"
                kbd="⌘Z"
                onClick={onUndo}
              >
                {s.undo}
              </Button>
            ) : null
          ) : (
            <>
              <Button
                key="reject"
                variant="quiet"
                size="sm"
                kbd="X"
                onClick={onReject}
                disabled={pending}
              >
                {s.reject}
              </Button>
              <Button
                key="edit"
                variant="secondary"
                size="sm"
                kbd="E"
                onClick={onEdit}
                disabled={pending}
              >
                {s.edit}
              </Button>
              <Button
                key="keep"
                ref={keepRef}
                variant="keep"
                size="sm"
                kbd="K"
                onClick={handleKeep}
                pending={pending}
              >
                {conflictWith ? s.keepReplace : s.keep}
              </Button>
            </>
          )}
        </div>
      </footer>
    </article>
  );
}
