"use client";

import { useState, type FormEvent } from "react";
import { AgentStamp, Button, Icon, Logo } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { opensInBrowser } from "@/lib/oauth-redirects";
import { consentFootnote, type ConsentCopy } from "@/lib/v2/consent-copy";
import {
  ConsentDecisionError,
  followable,
  shortName,
  type ConsentEnding,
  type ConsentRequestView,
  type ConsentSource,
} from "@/lib/v2/data/consent";
import { Abilities, SpaceChoice } from "./consent-parts";
import styles from "./consent.module.css";

type Doing = "approve" | "deny" | "switch";

/**
 * The card: who asks, who is signed in, the one space, what the agent
 * will and won't be able to do there (the server's words for it, from
 * policy), Cancel and Allow. Allow and Cancel go to the API through the
 * web app's proxy as this person; the API answers with the URL to send
 * the browser to (the client's registered redirect_uri, with a code or
 * access_denied), which the page follows and never builds. "Not you?"
 * lets go of the request, signs this browser out and goes to sign in,
 * back to the same request.
 */
export function ConsentForm({
  request,
  source,
  onEnded,
  signInAgain,
}: {
  request: ConsentRequestView;
  source: ConsentSource;
  onEnded: (ending: ConsentEnding) => void;
  signInAgain: () => string;
}) {
  const { t } = useLocale();
  const copy = t.ledger.consent;
  const client = request.client.name || copy.fallbackClient;
  const short = shortName(client);
  const [chosen, setChosen] = useState<string | null>(
    () => request.spaces.find((s) => !s.disabled)?.id ?? null,
  );
  const [doing, setDoing] = useState<Doing | null>(null);
  const [error, setError] = useState<"space" | "failed" | null>(null);
  const space = request.spaces.find((s) => s.id === chosen);

  const failed = (err: unknown) => {
    setDoing(null);
    if (err instanceof ConsentDecisionError && err.refusal === "ended") {
      onEnded(err.ending ?? "gone");
      return;
    }
    setError(
      err instanceof ConsentDecisionError && err.refusal === "space"
        ? "space"
        : "failed",
    );
  };
  const decide = (decision: "approve" | "deny") => {
    if (doing) return;
    if (decision === "approve" && !space) return;
    setDoing(decision);
    setError(null);
    source
      .decide(
        request.requestId,
        decision === "approve" && space
          ? { decision, spaceId: space.id }
          : { decision: "deny" },
      )
      .then((to) => {
        if (!followable(to)) throw new ConsentDecisionError("failed");
        window.location.assign(to);
        // A native app's scheme (cursor://) leaves this page where it is:
        // say the answer went to the app.
        if (!opensInBrowser(to)) onEnded("handed");
      })
      .catch(failed);
  };
  const notYou = () => {
    if (doing) return;
    setDoing("switch");
    setError(null);
    source
      .release(request.requestId)
      .catch((err: unknown) => {
        // A request that ended meanwhile is let go of anyway.
        if (!(err instanceof ConsentDecisionError) || err.refusal !== "ended") {
          throw err;
        }
      })
      .then(() => fetch("/api/auth/logout", { method: "POST" }))
      .then(() => window.location.assign(signInAgain()))
      .catch(failed);
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    decide("approve");
  };

  const noSpaces = request.spaces.length === 0;
  return (
    <form
      className={styles.card}
      onSubmit={submit}
      aria-labelledby="consent-title"
    >
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
          {noSpaces ? copy.noSpaces.title : interpolate(copy.title, { client })}
        </h1>
        <p className={`mx-meta ${styles.meta}`}>
          {`${interpolate(copy.signedInAs, { name: request.person })} · `}
          <button
            type="button"
            className={styles.notYou}
            aria-disabled={doing !== null || undefined}
            onClick={notYou}
          >
            {copy.notYou}
          </button>
        </p>
      </div>
      {error ? (
        <p className={styles.alert} role="alert">
          {copy.errors[error]}
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
          {space ? (
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
        doing={doing}
        onCancel={() => decide("deny")}
      />
    </form>
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
  doing,
  onCancel,
}: {
  copy: ConsentCopy;
  client: string;
  /** The whole name, for the tooltip of a shortened Allow. */
  fullName: string;
  noSpaces: boolean;
  footnote: string;
  canAllow: boolean;
  doing: Doing | null;
  onCancel: () => void;
}) {
  return (
    <div className={styles.foot}>
      <div className={styles.actions}>
        <Button
          size="lg"
          className={styles.cancel}
          pending={doing === "deny"}
          disabled={doing !== null && doing !== "deny"}
          onClick={onCancel}
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
            variant="primary"
            size="lg"
            className={styles.allow}
            title={interpolate(copy.allow, { client: fullName })}
            pending={doing === "approve"}
            disabled={!canAllow || (doing !== null && doing !== "approve")}
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
