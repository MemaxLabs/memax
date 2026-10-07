"use client";

import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { Button, Logo } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { devRoutesEnabled } from "@/lib/dev-routes";
import { getPublicMemaxClient } from "@/lib/memax-client";
import {
  ConsentLoadError,
  type ConsentEnding,
  type ConsentRequestView,
} from "@/lib/v2/data/consent";
import { DEMO_CONSENT_REQUEST, demoConsent } from "@/lib/v2/data/consent-demo";
import { createSdkConsent } from "@/lib/v2/data/consent-sdk";
import { ConsentForm } from "./consent-form";
import styles from "./consent.module.css";

/**
 * OAuthConsent (OAuthConsent.png) at /oauth/authorize?request_id=…&
 * consent_token=…: the API sends a person here once they have signed in
 * for an agent's OAuth request (MCP), to choose the one space it may use.
 * The request's token reads it, so no session of this browser's is
 * needed: the person it is signed in as is the request's. The API sends
 * a post that can't go on back here with `ended` (expired, gone) or
 * `error` (space, permission).
 */
export function ConsentScreen() {
  const params = useSearchParams();
  const requestId = params?.get("request_id")?.trim() ?? "";
  const token = params?.get("consent_token")?.trim() ?? "";
  const ended = params?.get("ended");
  const error = params?.get("error");
  const demo = devRoutesEnabled() && requestId === DEMO_CONSENT_REQUEST;
  const source = useMemo(
    () => (demo ? demoConsent : createSdkConsent(getPublicMemaxClient())),
    [demo],
  );
  const asked = Boolean(requestId && token) && !ended;
  const request = useQuery<ConsentRequestView, ConsentLoadError>({
    queryKey: ["v2", "oauth-consent", requestId, token],
    queryFn: ({ signal }) => source.load({ requestId, token, signal }),
    enabled: asked,
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const expired = useExpiry(request.data);

  let ending: ConsentEnding | null = null;
  if (ended) ending = ended === "expired" ? "expired" : "gone";
  else if (!asked) ending = "missing";
  else if (request.isError) ending = request.error.ending;
  else if (expired) ending = "expired";

  return (
    <main className={styles.page}>
      <Logo variant="lockup" size={18} />
      {ending ? (
        <Ending ending={ending} onRetry={() => void request.refetch()} />
      ) : request.data ? (
        <ConsentForm
          request={request.data}
          error={error === "space" || error === "permission" ? error : null}
          demo={demo}
        />
      ) : (
        <Loading />
      )}
    </main>
  );
}

/** True once the request's time is up, counted on this page's clock. */
function useExpiry(request: ConsentRequestView | undefined): boolean {
  const [expired, setExpired] = useState(false);
  useEffect(() => {
    if (!request) return;
    const id = window.setTimeout(
      () => setExpired(true),
      Math.max(0, request.expiresIn) * 1000,
    );
    return () => window.clearTimeout(id);
  }, [request]);
  return expired;
}

function Loading() {
  const { t } = useLocale();
  return (
    <p className="mx-sr" role="status">
      {t.ledger.consent.loading}
    </p>
  );
}

function Ending({
  ending,
  onRetry,
}: {
  ending: ConsentEnding;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.consent[ending];
  return (
    <section className={styles.card} aria-labelledby="consent-ending">
      <div className={styles.head}>
        <h1 className={styles.title} id="consent-ending">
          {copy.title}
        </h1>
        <p className={styles.lede}>{copy.lede}</p>
      </div>
      {ending === "failed" ? (
        <div className={styles.endActions}>
          <Button variant="primary" onClick={onRetry}>
            {t.ledger.consent.failed.retry}
          </Button>
        </div>
      ) : null}
    </section>
  );
}
