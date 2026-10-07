"use client";

import { useRef, useState, type FormEvent, type MouseEvent } from "react";
import { AgentStamp, Button, Icon, Logo } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { consentFootnote, type ConsentCopy } from "@/lib/v2/consent-copy";
import { shortName, type ConsentRequestView } from "@/lib/v2/data/consent";
import { Abilities, SpaceChoice } from "./consent-parts";
import styles from "./consent.module.css";

export type ConsentError = "space" | "permission";
type Decision = "approve" | "deny" | "switch";

const CANCEL_FORM = "consent-cancel";
const SWITCH_FORM = "consent-switch";

/**
 * The card: who asks, who is signed in, the one space, what the agent
 * will and won't be able to do there (the server's words for it, from
 * policy), Cancel and Allow. Three plain forms post to the API, which
 * answers with a redirect the browser follows: Allow (`approve`, the
 * chosen space and no more than memax:propose), Cancel (`deny`: the
 * client hears access_denied) and "Not you?" (`switch`: sign in again,
 * back to this request). The request's consent token rides in each.
 */
export function ConsentForm({
  request,
  error,
  demo,
}: {
  request: ConsentRequestView;
  error: ConsentError | null;
  demo: boolean;
}) {
  const { t } = useLocale();
  const copy = t.ledger.consent;
  const { user } = useAuth();
  const client = request.client.name || copy.fallbackClient;
  const short = shortName(client);
  const [chosen, setChosen] = useState<string | null>(
    () => request.spaces.find((s) => !s.disabled)?.id ?? null,
  );
  const [sending, setSending] = useState<Decision | null>(null);
  const switchForm = useRef<HTMLFormElement>(null);
  const space = request.spaces.find((s) => s.id === chosen);

  const onSubmit = (decision: Decision) => (event: FormEvent) => {
    if (sending !== null || demo) event.preventDefault();
    if (sending === null) setSending(decision);
  };
  // "Not you?" signs this browser out of Memax too, then starts over.
  const notYou = (event: MouseEvent) => {
    if (!user || demo || sending !== null) return;
    event.preventDefault();
    setSending("switch");
    void fetch("/api/auth/logout", { method: "POST" })
      .catch(() => undefined)
      .finally(() => switchForm.current?.submit());
  };

  const noSpaces = request.spaces.length === 0;
  return (
    <>
      <form
        className={styles.card}
        method="post"
        action={request.submitUrl}
        onSubmit={onSubmit("approve")}
        aria-labelledby="consent-title"
      >
        <RequestFields request={request} />
        {request.permissions.map((p) => (
          <input key={p} type="hidden" name="permission" value={p} />
        ))}
        {/* The heading says who connects to what; the row draws it. */}
        <div className={styles.stamps}>
          <AgentStamp agent={request.client.agent} name={client} decorative />
          {request.client.host ? (
            <span
              className={`mx-meta ${styles.host}`}
              title={interpolate(copy.servedFromLabel, {
                host: request.client.host,
              })}
            >
              {interpolate(copy.servedFrom, { host: request.client.host })}
            </span>
          ) : null}
          <Icon name="arrow-right" size={14} />
          <span className={styles.mark}>
            <Logo variant="mark" size={28} decorative />
          </span>
        </div>
        <div className={styles.head}>
          <h1 className={styles.title} id="consent-title">
            {noSpaces
              ? copy.noSpaces.title
              : interpolate(copy.title, { client })}
          </h1>
          <p className={`mx-meta ${styles.meta}`}>
            {request.person
              ? `${interpolate(copy.signedInAs, { name: request.person })} · `
              : null}
            <button
              type="submit"
              form={SWITCH_FORM}
              className={styles.notYou}
              aria-disabled={sending !== null || undefined}
              onClick={notYou}
            >
              {copy.notYou}
            </button>
          </p>
        </div>
        {error ? (
          <p className={styles.alert} role="alert">
            {interpolate(copy.errors[error], { client: short })}
          </p>
        ) : null}
        {noSpaces ? (
          <p className={styles.lede}>
            {interpolate(copy.noSpaces.lede, { client: short })}
          </p>
        ) : (
          <>
            <SpaceChoice
              copy={copy}
              client={short}
              spaces={request.spaces}
              chosen={chosen}
              onChoose={setChosen}
            />
            {space?.described ? (
              <Abilities copy={copy} client={short} space={space} />
            ) : null}
          </>
        )}
        <Foot
          copy={copy}
          client={short}
          fullName={client}
          noSpaces={noSpaces}
          footnote={consentFootnote(copy, short, space)}
          canAllow={space !== undefined && !space.disabled}
          sending={sending}
        />
      </form>
      <form
        id={CANCEL_FORM}
        method="post"
        action={request.submitUrl}
        onSubmit={onSubmit("deny")}
        hidden
      >
        <RequestFields request={request} decision="deny" />
      </form>
      <form
        id={SWITCH_FORM}
        ref={switchForm}
        method="post"
        action={request.submitUrl}
        onSubmit={onSubmit("switch")}
        hidden
      >
        <RequestFields request={request} decision="switch" />
      </form>
    </>
  );
}

/** The request, its token, and that the Ledger page sent it. */
function RequestFields({
  request,
  decision,
}: {
  request: ConsentRequestView;
  decision?: Decision;
}) {
  return (
    <>
      <input type="hidden" name="session_id" value={request.requestId} />
      <input type="hidden" name="csrf_token" value={request.token} />
      <input type="hidden" name="ui" value="v2" />
      {decision ? (
        <input type="hidden" name="decision" value={decision} />
      ) : null}
    </>
  );
}

/**
 * Cancel and Allow, and the footnote. With no space yet, Allow becomes
 * the way to set one up; Cancel still answers the agent.
 */
function Foot({
  copy,
  client,
  fullName,
  noSpaces,
  footnote,
  canAllow,
  sending,
}: {
  copy: ConsentCopy;
  client: string;
  /** The whole name, for the tooltip of a shortened Allow. */
  fullName: string;
  noSpaces: boolean;
  footnote: string;
  canAllow: boolean;
  sending: Decision | null;
}) {
  return (
    <div className={styles.foot}>
      <div className={styles.actions}>
        <Button
          type="submit"
          form={CANCEL_FORM}
          size="lg"
          className={styles.cancel}
          pending={sending === "deny"}
          disabled={sending !== null && sending !== "deny"}
        >
          {copy.cancel}
        </Button>
        {noSpaces ? (
          <Button
            variant="primary"
            size="lg"
            className={styles.allow}
            href="/setup"
          >
            {copy.noSpaces.setup}
          </Button>
        ) : (
          <Button
            type="submit"
            name="decision"
            value="approve"
            variant="primary"
            size="lg"
            className={styles.allow}
            title={interpolate(copy.allow, { client: fullName })}
            pending={sending === "approve"}
            disabled={!canAllow || (sending !== null && sending !== "approve")}
          >
            {interpolate(copy.allow, { client })}
          </Button>
        )}
      </div>
      {noSpaces ? null : (
        <p className={`mx-meta ${styles.footnote}`}>{footnote}</p>
      )}
    </div>
  );
}
