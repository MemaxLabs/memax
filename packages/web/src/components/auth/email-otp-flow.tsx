"use client";

import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { useRouter } from "next/navigation";
import { MemaxError } from "memax-sdk";
import type { VerifyEmailOtpResponse } from "memax-sdk";

import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { getPublicMemaxClient } from "@/lib/memax-client";

const OTP_LENGTH = 6;
const RESEND_COOLDOWN_SECONDS = 30;
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

type Step = "email" | "code";

interface EmailOtpFlowProps {
  /** Optional invite token to carry through the verify path. */
  inviteToken?: string;
  /** Pre-fills the email field, e.g. from invite metadata. */
  defaultEmail?: string;
  /** Final destination after successful sign-in. Defaults to /home. */
  redirectAfter?: string;
}

interface FieldErrors {
  email?: string;
  code?: string;
  banner?: string;
}

/**
 * Two-step email OTP login flow:
 *
 *   email → submit → code (6-digit single input) → verify → /home
 *
 * Designed to live inside both /login and /register?invite=... so a
 * customer who only has enterprise email (no GitHub/Google) can sign
 * in. Errors map to localized strings; the resend control runs a
 * countdown to discourage the obvious spam pattern.
 *
 * No raw fetch — every call goes through the public memax SDK, which
 * is required by the codebase's SDK boundary script
 * (`scripts/check-sdk-boundary.mjs`).
 */
export function EmailOtpFlow({
  inviteToken,
  defaultEmail = "",
  redirectAfter = "/home",
}: EmailOtpFlowProps) {
  const { t } = useLocale();
  const copy = t.auth.emailSignIn;
  const router = useRouter();
  const { completeLogin } = useAuth();

  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState(defaultEmail);
  const [code, setCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errors, setErrors] = useState<FieldErrors>({});
  const [resendIn, setResendIn] = useState(0);
  const emailInputRef = useRef<HTMLInputElement | null>(null);
  const codeInputRef = useRef<HTMLInputElement | null>(null);

  const client = useMemo(() => getPublicMemaxClient(), []);

  const emailId = useId();
  const codeId = useId();

  // Auto-focus on step transitions. The codepath fires on initial
  // mount too so the email input is ready for keyboard-only users.
  useEffect(() => {
    if (step === "email") {
      emailInputRef.current?.focus();
    } else {
      codeInputRef.current?.focus();
    }
  }, [step]);

  // Resend cooldown timer.
  useEffect(() => {
    if (resendIn <= 0) return;
    const t = window.setInterval(() => {
      setResendIn((n) => (n > 0 ? n - 1 : 0));
    }, 1000);
    return () => window.clearInterval(t);
  }, [resendIn]);

  const mapError = useCallback(
    (err: unknown): FieldErrors => {
      if (err instanceof MemaxError) {
        switch (err.code) {
          case "invalid_email":
            return { email: copy.errorInvalidEmail };
          case "rate_limited":
            return { banner: copy.errorRateLimited };
          case "email_send_failed":
            return { banner: copy.errorSendFailed };
          case "invalid_code_format":
            return { code: copy.errorCodeFormat };
          case "code_invalid":
            return { code: err.message || copy.errorCodeInvalid };
          case "code_expired":
            return { code: copy.errorCodeExpired };
          case "code_locked":
            return { code: copy.errorCodeLocked };
          case "registration_required":
            return { banner: copy.errorRegistrationRequired };
          case "invalid_invite":
            return { banner: copy.errorInvalidInvite };
          case "invite_email_mismatch":
            return { banner: copy.errorInviteEmailMismatch };
          default:
            return { banner: err.message || copy.errorGeneric };
        }
      }
      return { banner: copy.errorGeneric };
    },
    [copy],
  );

  const handleSendCode = useCallback(
    async (e?: React.FormEvent) => {
      e?.preventDefault();
      setErrors({});
      const trimmed = email.trim();
      if (!EMAIL_PATTERN.test(trimmed)) {
        setErrors({ email: copy.errorInvalidEmail });
        return;
      }
      setSubmitting(true);
      try {
        const resp = await client.auth.requestEmailOtp({
          email: trimmed,
          invite_token: inviteToken,
        });
        // The server canonicalizes — adopt that form so subsequent
        // verify calls don't drift on whitespace/case.
        setEmail(resp.email);
        setStep("code");
        setCode("");
        setResendIn(resp.cooldown ?? RESEND_COOLDOWN_SECONDS);
      } catch (err) {
        setErrors(mapError(err));
      } finally {
        setSubmitting(false);
      }
    },
    [email, inviteToken, client, copy, mapError],
  );

  const handleVerify = useCallback(
    async (e?: React.FormEvent, codeOverride?: string) => {
      e?.preventDefault();
      setErrors({});
      const submitCode = (codeOverride ?? code).trim();
      if (submitCode.length !== OTP_LENGTH || !/^\d+$/.test(submitCode)) {
        setErrors({ code: copy.errorCodeFormat });
        return;
      }
      setSubmitting(true);
      try {
        const resp: VerifyEmailOtpResponse = await client.auth.verifyEmailOtp({
          email,
          code: submitCode,
        });
        // The verify response is a discriminated union — we issued
        // no redirect_uri at request time, so the server must hand
        // back tokens directly. Defensive narrow regardless: a
        // future SDK addition can carry redirect data, and we want
        // a single happy-path branch.
        if ("access_token" in resp && resp.access_token && resp.refresh_token) {
          const ok = await completeLogin(resp.access_token, resp.refresh_token);
          if (!ok) {
            setErrors({ banner: copy.errorGeneric });
            return;
          }
          router.replace(redirectAfter);
          return;
        }
        if ("redirect" in resp && resp.redirect) {
          window.location.assign(resp.redirect);
          return;
        }
        setErrors({ banner: copy.errorGeneric });
      } catch (err) {
        setErrors(mapError(err));
      } finally {
        setSubmitting(false);
      }
    },
    [code, email, client, copy, completeLogin, mapError, redirectAfter, router],
  );

  // Paste-friendly digit input. If the user pastes the full code, we
  // auto-submit so the flow feels instant — a common touch-typer
  // pattern that also helps mobile autofill from "iOS one-time-code"
  // SMS or email scanners.
  const handleCodeChange = useCallback(
    (raw: string) => {
      const digits = raw.replace(/\D/g, "").slice(0, OTP_LENGTH);
      setCode(digits);
      if (digits.length === OTP_LENGTH && !submitting) {
        void handleVerify(undefined, digits);
      }
    },
    [handleVerify, submitting],
  );

  const handleResend = useCallback(async () => {
    if (resendIn > 0 || submitting) return;
    await handleSendCode();
  }, [handleSendCode, resendIn, submitting]);

  if (step === "email") {
    return (
      <form
        onSubmit={handleSendCode}
        className="w-full"
        aria-labelledby={`${emailId}-heading`}
        noValidate
      >
        <div className="mb-5">
          <h2
            id={`${emailId}-heading`}
            className="text-[15px] font-semibold text-fg-1"
          >
            {copy.emailHeading}
          </h2>
          <p className="mt-1.5 text-[12.5px] leading-relaxed text-fg-3">
            {copy.emailSubheading}
          </p>
        </div>

        <label htmlFor={emailId} className="sr-only">
          {copy.emailLabel}
        </label>
        <input
          id={emailId}
          ref={emailInputRef}
          type="email"
          inputMode="email"
          autoComplete="email"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          required
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            if (errors.email)
              setErrors((prev) => ({ ...prev, email: undefined }));
          }}
          placeholder={copy.emailPlaceholder}
          aria-invalid={errors.email ? "true" : undefined}
          aria-describedby={errors.email ? `${emailId}-error` : undefined}
          className="h-12 w-full rounded-chrome border border-border bg-surface-1 px-4 text-[14px] text-fg-1 placeholder:text-fg-4 focus:outline-none focus:border-fg-3 focus:bg-background transition-colors"
        />

        {errors.email && (
          <p
            id={`${emailId}-error`}
            role="alert"
            className="mt-2 text-[12px] text-[var(--signature)]"
          >
            {errors.email}
          </p>
        )}
        {errors.banner && (
          <p role="alert" className="mt-2 text-[12px] text-[var(--signature)]">
            {errors.banner}
          </p>
        )}

        <button
          type="submit"
          disabled={submitting}
          className="mt-3 h-12 w-full rounded-chrome bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center gap-2 cursor-pointer hover:opacity-90 active:opacity-80 disabled:opacity-50 disabled:cursor-not-allowed transition-opacity"
        >
          {submitting ? copy.sending : copy.sendCode}
        </button>
      </form>
    );
  }

  return (
    <form
      onSubmit={handleVerify}
      className="w-full"
      aria-labelledby={`${codeId}-heading`}
      noValidate
    >
      <div className="mb-5">
        <h2
          id={`${codeId}-heading`}
          className="text-[15px] font-semibold text-fg-1"
        >
          {copy.codeHeading}
        </h2>
        <p className="mt-1.5 text-[12.5px] leading-relaxed text-fg-3">
          {copy.codeSubheading.replace("{email}", email)}
        </p>
      </div>

      <label htmlFor={codeId} className="sr-only">
        {copy.codeLabel}
      </label>
      <input
        id={codeId}
        ref={codeInputRef}
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        pattern="[0-9]*"
        maxLength={OTP_LENGTH}
        required
        value={code}
        onChange={(e) => handleCodeChange(e.target.value)}
        placeholder={copy.codePlaceholder}
        aria-invalid={errors.code ? "true" : undefined}
        aria-describedby={errors.code ? `${codeId}-error` : undefined}
        className="h-14 w-full rounded-chrome border border-border bg-surface-1 px-4 font-mono text-[24px] font-semibold tracking-[0.4em] text-center text-fg-1 placeholder:text-fg-4 placeholder:tracking-normal placeholder:font-normal placeholder:text-[14px] focus:outline-none focus:border-fg-3 focus:bg-background transition-colors"
      />

      {errors.code && (
        <p
          id={`${codeId}-error`}
          role="alert"
          className="mt-2 text-[12px] text-[var(--signature)]"
        >
          {errors.code}
        </p>
      )}
      {errors.banner && (
        <p role="alert" className="mt-2 text-[12px] text-[var(--signature)]">
          {errors.banner}
        </p>
      )}

      <button
        type="submit"
        disabled={submitting || code.length !== OTP_LENGTH}
        className="mt-3 h-12 w-full rounded-chrome bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center gap-2 cursor-pointer hover:opacity-90 active:opacity-80 disabled:opacity-50 disabled:cursor-not-allowed transition-opacity"
      >
        {submitting ? copy.verifying : copy.verify}
      </button>

      <div className="mt-4 flex items-center justify-between text-[12px] text-fg-3">
        <button
          type="button"
          onClick={() => {
            setStep("email");
            setCode("");
            setErrors({});
          }}
          className="text-fg-3 hover:text-fg-1 cursor-pointer transition-colors"
        >
          {copy.changeEmail}
        </button>
        <button
          type="button"
          onClick={handleResend}
          disabled={resendIn > 0 || submitting}
          className="text-fg-3 hover:text-fg-1 disabled:hover:text-fg-3 disabled:cursor-not-allowed cursor-pointer transition-colors"
        >
          {resendIn > 0
            ? copy.resendIn.replace("{seconds}", String(resendIn))
            : copy.resend}
        </button>
      </div>

      <p className="mt-4 text-center text-[11px] text-fg-4">
        {copy.didntGetIt}
      </p>
    </form>
  );
}
