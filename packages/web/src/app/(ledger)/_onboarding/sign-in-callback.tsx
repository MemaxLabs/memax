"use client";

import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { Button } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { safeNext, signInHref } from "@/lib/v2/onboarding/routes";
import { StatusPage } from "../_components/status-page";
import { useLandingRedirect } from "./landing";

/**
 * Where the API sends a sign-in's one-time code (GitHub, Google or the
 * email code): it trades the code for the web app's session (surface
 * web, migration 030) through /api/auth/exchange, then goes on to `next`
 * or the person's landing. A failure says so and starts over; an error
 * the API sent (an account that may not sign in) goes back to SignIn,
 * which words it.
 */
export function SignInCallbackScreen() {
  const { t } = useLocale();
  const copy = t.ledger.onboarding.callback;
  const params = useSearchParams();
  const { completeLogin } = useAuth();
  const landing = useLandingRedirect();
  const [state, setState] = useState<"working" | "landing" | "failed">(
    "working",
  );
  const ran = useRef(false);
  const next = safeNext(params?.get("next"));

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;
    const error = params?.get("error");
    if (error) {
      const back = new URLSearchParams({ error });
      if (next) back.set("next", next);
      window.location.replace(`/signin?${back.toString()}`);
      return;
    }
    const code = params?.get("code");
    if (!code) {
      setState("failed");
      return;
    }
    void (async () => {
      try {
        const res = await fetch("/api/auth/exchange", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ code }),
        });
        const json = (await res.json()) as {
          data?: { access_token?: string; refresh_token?: string };
        };
        const tokens = json.data;
        if (!res.ok || !tokens?.access_token || !tokens.refresh_token) {
          setState("failed");
          return;
        }
        if (!(await completeLogin(tokens.access_token, tokens.refresh_token))) {
          setState("failed");
          return;
        }
        setState("landing");
        landing.go(next);
      } catch {
        setState("failed");
      }
    })();
  }, [params, completeLogin, landing, next]);

  if (state === "failed") {
    return (
      <StatusPage
        role="alert"
        receipt={copy.receipt}
        title={copy.failed}
        description={copy.failedDetail}
        actions={
          <Button variant="primary" href={signInHref(next)}>
            {copy.again}
          </Button>
        }
      />
    );
  }
  return (
    <StatusPage
      receipt={copy.receipt}
      title={state === "landing" ? copy.landing : copy.working}
      description={copy.workingDetail}
      actions={null}
    />
  );
}
