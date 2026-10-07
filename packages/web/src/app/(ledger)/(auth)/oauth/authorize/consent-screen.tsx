"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { Button, Logo } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { devRoutesEnabled } from "@/lib/dev-routes";
import { getMemaxClient } from "@/lib/memax-client";
import {
  ConsentLoadError,
  shortName,
  type ConsentEnding,
  type ConsentRequestView,
} from "@/lib/v2/data/consent";
import { DEMO_CONSENT_REQUEST, demoConsent } from "@/lib/v2/data/consent-demo";
import { createSdkConsent } from "@/lib/v2/data/consent-sdk";
import { signInHref } from "@/lib/v2/onboarding/routes";
import { ConsentForm } from "./consent-form";
import styles from "./consent.module.css";

/** The page for a request, as a sign-in's `next`. */
export function consentHref(requestId: string): string {
  return `/oauth/authorize?request=${encodeURIComponent(requestId)}`;
}

/**
 * OAuthConsent (OAuthConsent.png) at /oauth/authorize?request=<id>: the
 * API sends a person here when an agent's OAuth request (MCP) asks for one
 * of their spaces. The web session says who they are, whichever way they
 * signed in: without one, they sign in first (SignIn, every method) and
 * come back. The first person to open a request is bound to it. A link
 * from V1's page made before the move (`request_id`) opens the same way.
 */
export function ConsentScreen() {
  const params = useSearchParams();
  const router = useRouter();
  const requestId = (
    params?.get("request") ??
    params?.get("request_id") ??
    ""
  ).trim();
  const demo = devRoutesEnabled() && requestId === DEMO_CONSENT_REQUEST;
  const { user, loading } = useAuth();
  const signedOut = !demo && requestId !== "" && !loading && !user;
  useEffect(() => {
    if (signedOut) router.replace(signInHref(consentHref(requestId)));
  }, [signedOut, router, requestId]);

  const source = useMemo(
    () => (demo ? demoConsent : createSdkConsent(getMemaxClient())),
    [demo],
  );
  const request = useQuery<ConsentRequestView, ConsentLoadError>({
    queryKey: ["v2", "oauth-consent", requestId, demo ? "demo" : user?.id],
    queryFn: () => source.load(requestId),
    enabled: requestId !== "" && (demo || Boolean(user)),
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const expired = useExpiry(request.data);
  const [ended, setEnded] = useState<ConsentEnding | null>(null);

  let ending: ConsentEnding | null = ended;
  if (!ending) {
    if (requestId === "") ending = "missing";
    else if (request.isError) ending = request.error.ending;
    else if (expired) ending = "expired";
  }

  return (
    <main className={styles.page}>
      <Logo variant="lockup" size={18} />
      {ending ? (
        <Ending
          ending={ending}
          client={request.data?.client.name}
          onRetry={() => void request.refetch()}
        />
      ) : request.data ? (
        <ConsentForm
          request={request.data}
          source={source}
          onEnded={setEnded}
          signInAgain={() => signInHref(consentHref(requestId))}
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
  client,
  onRetry,
}: {
  ending: ConsentEnding;
  /** The agent the request was from, once it loaded. */
  client?: string;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.consent[ending];
  const name = shortName(client || t.ledger.consent.fallbackClient);
  return (
    <section className={styles.card} aria-labelledby="consent-ending">
      <div className={styles.head}>
        <h1 className={styles.title} id="consent-ending">
          {interpolate(copy.title, { client: name })}
        </h1>
        <p className={styles.lede}>
          {interpolate(copy.lede, { client: name })}
        </p>
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
