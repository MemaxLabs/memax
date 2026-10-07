"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button, Field, Logo, MemoryRow } from "@memaxlabs/ledger";
import { MemaxError, type AuthProviderName, type WebUi } from "memax-sdk";
import { interpolate, useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { getPublicMemaxClient } from "@/lib/memax-client";
import { trackFunnelStep } from "@/lib/v2/funnel";
import { safeNext } from "@/lib/v2/onboarding/routes";
import {
  getPasskey,
  PasskeyError,
  type RequestOptionsJSON,
} from "@/lib/v2/passkeys/webauthn";
import { StatusPage } from "../_components/status-page";
import { useLandingRedirect } from "./landing";
import styles from "./sign-in.module.css";

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

type Copy = ReturnType<typeof useLocale>["t"]["ledger"]["onboarding"]["signIn"];

/** The callback the API sends the one-time code to: this app, so the session is the web's. */
export function signInCallback(origin: string, next: string | null): string {
  const url = new URL("/signin/callback", origin);
  if (next) url.searchParams.set("next", next);
  return url.toString();
}

function errorText(copy: Copy, code: string | null | undefined): string {
  switch (code) {
    case "invalid_email":
      return copy.errors.invalidEmail;
    case "invalid_code_format":
      return copy.errors.codeFormat;
    case "code_invalid":
      return copy.errors.codeInvalid;
    case "code_expired":
      return copy.errors.codeExpired;
    case "code_locked":
      return copy.errors.codeLocked;
    case "rate_limited":
      return copy.errors.rateLimited;
    case "registration_required":
    case "invalid_invite":
    case "invite_email_mismatch":
    case "account_not_allowed":
      return copy.errors.notAllowed;
    case "callback":
      return copy.errors.callback;
    default:
      return copy.errors.failed;
  }
}

/**
 * SignIn (SignIn.png): the product on the left, the ways in on the
 * right. GitHub and Google go through the API's OAuth and come back to
 * /signin/callback; email sends a 6-digit code whose redirect comes back
 * the same way. Either way the one-time code is delivered to this app's
 * origin, so the session is issued to the web app (surface web) and the
 * signed proxy can record the person's keeps as human_web. Already
 * signed in, the page lands (routes.ts) or returns to `next`, by the web
 * UI the person sees: the proxy sends a signed-in browser here to read
 * its V2 UI flag again (lib/ui-gate.ts), and the profile read that tells
 * it also sets the hint. `restoring` (the session-presence marker, read
 * by the page's server) shows that instead of the form while the profile
 * loads.
 */
export function SignInScreen({ restoring = false }: { restoring?: boolean }) {
  const { t } = useLocale();
  const copy = t.ledger.onboarding.signIn;
  const { user, ui, loading, login } = useAuth();
  const params = useSearchParams();
  const next = safeNext(params?.get("next"));
  const landing = useLandingRedirect();
  // ?again=1: a fresh session issued to the web app, even if signed in.
  const again = params?.get("again") === "1";
  const signedIn = !again && !loading && Boolean(user);

  useEffect(() => {
    if (signedIn) landing.go(next, ui);
  }, [signedIn, next, landing, ui]);

  if (signedIn || (!again && loading && restoring)) {
    const status = t.ledger.onboarding.callback;
    return (
      <StatusPage
        receipt={status.receipt}
        title={status.landing}
        description={status.workingDetail}
        actions={null}
      />
    );
  }

  const callback = () => signInCallback(window.location.origin, next);
  const start = (provider: AuthProviderName) => login(callback(), provider);

  return (
    <div className={styles.page}>
      <section className={styles.story}>
        <Logo variant="lockup" size={20} />
        <div className={styles.storyBody}>
          <h1 className={styles.title}>{copy.title}</h1>
          <div className="mx-panel" aria-label={copy.sample.label} role="group">
            <MemoryRow
              as="div"
              compact
              person="ZZ"
              action={copy.sample.keptAction}
              time={copy.sample.keptTime}
              id="M-0219"
            >
              {copy.sample.kept}
            </MemoryRow>
            <MemoryRow
              as="div"
              compact
              state="proposed"
              agent="claude-code"
              action={copy.sample.proposedAction}
              time={copy.sample.proposedTime}
              id="M-0430"
            >
              {copy.sample.proposed}
            </MemoryRow>
          </div>
          <p className={styles.lede}>{copy.lede}</p>
        </div>
        <span className="mx-meta">&copy; {copy.copyright}</span>
      </section>

      <section className={styles.form} aria-labelledby="sign-in-heading">
        <div className={styles.formBody}>
          <div className={styles.heading}>
            <h2 id="sign-in-heading">{copy.heading}</h2>
            <p className="mx-meta">{copy.newHere}</p>
          </div>
          {params?.get("error") ? (
            <p className={styles.error} role="alert">
              {errorText(copy, params.get("error"))}
            </p>
          ) : null}
          <div className={styles.buttons}>
            <Button
              size="lg"
              icon="commit"
              className={styles.wide}
              disabled={signedIn}
              onClick={() => start("github")}
            >
              {copy.github}
            </Button>
            <Button
              size="lg"
              icon="globe"
              className={styles.wide}
              disabled={signedIn}
              onClick={() => start("google")}
            >
              {copy.google}
            </Button>
            <PasskeySignIn
              copy={copy}
              disabled={signedIn}
              onSignedIn={(signedInUi) => landing.go(next, signedInUi)}
            />
          </div>
          <div className={styles.or} aria-hidden="true">
            <span />
            {copy.or}
            <span />
          </div>
          <EmailSignIn copy={copy} callback={callback} disabled={signedIn} />
          <p className={`mx-meta ${styles.legal}`}>
            {copy.legalBefore}
            <a href="/terms">{copy.terms}</a>
            {copy.legalAnd}
            <a href="/privacy">{copy.privacy}</a>
            {copy.legalAfter}
          </p>
          {next ? (
            <p className={`mx-meta ${styles.legal}`}>
              {interpolate(copy.next, { where: next.split("?")[0] ?? next })}
            </p>
          ) : null}
        </div>
      </section>
    </div>
  );
}

/**
 * A passkey (plan §5.15): the web app's server asks the API for a
 * challenge naming nobody, the browser asks for any passkey for this site
 * with user verification, and the server trades the verified answer for
 * the web app's session (/api/auth/passkey), as every sign-in here does.
 * The button's press is the gesture the browser's prompt needs.
 */
function PasskeySignIn({
  copy,
  disabled,
  onSignedIn,
}: {
  copy: Copy;
  disabled: boolean;
  /** With the web UI the person sees, as the sign-in answered it. */
  onSignedIn: (ui: WebUi | undefined) => void;
}) {
  const { completeLogin } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const start = async () => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const options = await fetch("/api/auth/passkey/options", {
        method: "POST",
      });
      const begun = (await options.json()) as {
        data?: { options?: RequestOptionsJSON };
      };
      if (!options.ok || !begun.data?.options) {
        setError(copy.errors.passkeyFailed);
        return;
      }
      const credential = await getPasskey(begun.data.options);
      const res = await fetch("/api/auth/passkey", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ credential }),
      });
      const json = (await res.json()) as {
        data?: { signed_in?: boolean; ui?: WebUi };
        error?: { code?: string; details?: { passkey_failure?: string } };
      };
      if (!res.ok || !json.data?.signed_in) {
        setError(
          json.error?.details?.passkey_failure === "no_credential"
            ? copy.errors.passkeyUnknown
            : copy.errors.passkeyFailed,
        );
        return;
      }
      if (!(await completeLogin())) {
        setError(copy.errors.passkeyFailed);
        return;
      }
      trackFunnelStep("signed_in");
      onSignedIn(json.data.ui);
    } catch (err) {
      const problem = err instanceof PasskeyError ? err.problem : "failed";
      setError(
        problem === "cancelled"
          ? copy.errors.passkeyCancelled
          : problem === "unsupported"
            ? copy.errors.passkeyUnsupported
            : copy.errors.passkeyFailed,
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Button
        size="lg"
        icon="shield"
        className={styles.wide}
        disabled={disabled}
        pending={busy}
        onClick={() => void start()}
      >
        {busy ? copy.passkeyWaiting : copy.passkey}
      </Button>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
    </>
  );
}

/**
 * Email: a 6-digit code (the API's email sign-in), sent with this app's
 * callback as its redirect, so verifying it hands back a one-time code for
 * /signin/callback rather than tokens: the session stays the web's.
 */
function EmailSignIn({
  copy,
  callback,
  disabled,
}: {
  copy: Copy;
  callback: () => string;
  disabled: boolean;
}) {
  const client = useMemo(() => getPublicMemaxClient(), []);
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cooldown, setCooldown] = useState(0);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setTimeout(() => setCooldown((n) => n - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);

  const send = async (event?: FormEvent) => {
    event?.preventDefault();
    const trimmed = email.trim();
    if (!EMAIL.test(trimmed)) {
      setError(copy.errors.invalidEmail);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const res = await client.auth.requestEmailOtp({
        email: trimmed,
        redirect_uri: callback(),
      });
      setSentTo(res.email);
      setCode("");
      setCooldown(res.cooldown ?? 30);
    } catch (err) {
      setError(errorText(copy, err instanceof MemaxError ? err.code : null));
    } finally {
      setBusy(false);
    }
  };

  const verify = async (event: FormEvent) => {
    event.preventDefault();
    if (!sentTo) return;
    const digits = code.replace(/\s/g, "");
    if (!/^\d{6}$/.test(digits)) {
      setError(copy.errors.codeFormat);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const res = await client.auth.verifyEmailOtp({
        email: sentTo,
        code: digits,
      });
      if ("redirect" in res && res.redirect) {
        window.location.assign(res.redirect);
        return;
      }
      // Tokens in the response would be a session the web app wasn't
      // issued; ask for a fresh code rather than keep it.
      setError(copy.errors.failed);
      router.refresh();
    } catch (err) {
      setError(errorText(copy, err instanceof MemaxError ? err.code : null));
    } finally {
      setBusy(false);
    }
  };

  if (sentTo) {
    return (
      <form className={styles.email} onSubmit={verify} noValidate>
        <p className={styles.sent}>
          {interpolate(copy.codeSent, { email: sentTo })}
        </p>
        <Field
          label={copy.code}
          placeholder={copy.codePlaceholder}
          inputMode="numeric"
          autoComplete="one-time-code"
          mono
          value={code}
          onChange={(e) => setCode(e.currentTarget.value)}
          error={error ?? undefined}
        />
        <Button
          type="submit"
          variant="primary"
          size="lg"
          className={styles.wide}
          pending={busy}
        >
          {copy.verify}
        </Button>
        <div className={styles.emailLinks}>
          <Button
            variant="quiet"
            size="sm"
            onClick={() => {
              setSentTo(null);
              setError(null);
            }}
          >
            {copy.otherEmail}
          </Button>
          <Button
            variant="quiet"
            size="sm"
            disabled={cooldown > 0 || busy}
            onClick={() => void send()}
          >
            {cooldown > 0
              ? interpolate(copy.resendIn, { n: cooldown })
              : copy.resend}
          </Button>
        </div>
      </form>
    );
  }
  return (
    <form className={styles.email} onSubmit={send} noValidate>
      <Field
        label={copy.email}
        type="email"
        placeholder={copy.emailPlaceholder}
        autoComplete="email"
        value={email}
        onChange={(e) => setEmail(e.currentTarget.value)}
        error={error ?? undefined}
        disabled={disabled}
      />
      <Button
        type="submit"
        variant="primary"
        size="lg"
        className={styles.wide}
        pending={busy}
        disabled={disabled}
      >
        {copy.sendCode}
      </Button>
    </form>
  );
}
