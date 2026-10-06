"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { MemaxLoader } from "@memaxlabs/ui";
import { DREAM_AMBIENT } from "@memaxlabs/ui/tokens/dream";
import { apiGet } from "@/lib/api";
import { EmailOtpFlow } from "@/components/auth/email-otp-flow";

interface InviteInfo {
  email: string;
  status: string;
  expires_at: string;
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="currentColor">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 18 18" fill="currentColor">
      <path d="M9 7.636v2.91h4.098c-.176 1.148-1.32 3.367-4.098 3.367A4.544 4.544 0 014.455 9.37 4.544 4.544 0 019 4.826c1.308 0 2.184.557 2.684 1.04l1.828-1.762A7.308 7.308 0 009 1.636 7.364 7.364 0 001.636 9 7.364 7.364 0 009 16.364c4.252 0 7.071-2.987 7.071-7.198 0-.484-.052-.852-.116-1.22H9v-.31z" />
    </svg>
  );
}

function EmailIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="2" y="3.5" width="12" height="9" rx="1.5" />
      <path d="M2.5 4.5l5.5 4 5.5-4" />
    </svg>
  );
}

function RegisterHandler() {
  const params = useSearchParams();
  const { user, loading: authLoading, login } = useAuth();
  const { t } = useLocale();
  const router = useRouter();
  const token = params.get("invite") ?? "";
  const errorParam = params.get("error");
  const [showEmailFlow, setShowEmailFlow] = useState(false);

  const [inviteInfo, setInviteInfo] = useState<InviteInfo | null>(null);
  const [inviteError, setInviteError] = useState<"expired" | "invalid" | null>(
    null,
  );
  const [validating, setValidating] = useState(true);

  // If user is already logged in, redirect to home
  useEffect(() => {
    if (!authLoading && user) {
      router.replace("/home");
    }
  }, [authLoading, user, router]);

  // Validate invite token
  useEffect(() => {
    if (!token) {
      setInviteError("invalid");
      setValidating(false);
      return;
    }

    apiGet<InviteInfo>(`/v1/waitlist/invites/${token}`)
      .then((info) => {
        setInviteInfo(info);
        setValidating(false);
      })
      .catch((err) => {
        setInviteError(err?.status === 410 ? "expired" : "invalid");
        setValidating(false);
      });
  }, [token]);

  if (authLoading || user) return <MemaxLoader />;

  if (validating) {
    return <MemaxLoader label={t.auth.callbackLoading} />;
  }

  // Handle OAuth redirect errors
  if (errorParam) {
    const errorMessages: Record<string, string> = {
      registration_required: t.auth.registrationRequired,
      invalid_invite: t.auth.invalidInvite,
      invite_email_mismatch: t.auth.inviteEmailMismatch,
    };
    const message = errorMessages[errorParam] ?? t.auth.callbackErrorDesc;

    return (
      <div className="animate-fade-up text-center max-w-sm">
        <h1 className="text-[20px] font-semibold tracking-tight text-fg-1 mb-3">
          {t.auth.callbackError}
        </h1>
        <p className="text-[14px] leading-relaxed text-fg-3 mb-6">{message}</p>
        <a
          href="/waitlist"
          className="inline-flex h-10 items-center px-5 rounded-chrome border border-border bg-surface-1 text-fg-2 text-[13px] font-medium hover:bg-surface-2 hover:text-fg-1 transition-colors"
        >
          {t.auth.joinWaitlist}
        </a>
      </div>
    );
  }

  // Invalid or expired invite
  if (inviteError) {
    return (
      <div className="animate-fade-up text-center max-w-sm">
        <h1 className="text-[20px] font-semibold tracking-tight text-fg-1 mb-3">
          {inviteError === "expired"
            ? t.registerPage.expired
            : t.registerPage.invalid}
        </h1>
        <div className="flex flex-col items-center gap-3 mt-6">
          <a
            href="/waitlist"
            className="inline-flex h-10 items-center px-5 rounded-chrome border border-border bg-surface-1 text-fg-2 text-[13px] font-medium hover:bg-surface-2 hover:text-fg-1 transition-colors"
          >
            {t.registerPage.backToWaitlist}
          </a>
        </div>
      </div>
    );
  }

  // Valid invite — show login buttons
  const expiresDate = inviteInfo?.expires_at
    ? new Date(inviteInfo.expires_at).toLocaleDateString(undefined, {
        month: "long",
        day: "numeric",
        year: "numeric",
      })
    : "";

  // Build redirect with invite token so it flows through OAuth
  const redirectBase =
    typeof window !== "undefined" ? window.location.origin : "";
  const callbackWithInvite = `${redirectBase}/auth/callback?invite=${token}`;

  return (
    <div className="animate-fade-up w-full max-w-sm">
      {/* Header */}
      <div className="text-center mb-10">
        <div className="mx-auto mb-5 flex h-14 w-14 items-center justify-center rounded-chrome bg-surface-1">
          <span
            className="text-[24px] animate-pulse"
            style={{ color: "var(--signature)" }}
          >
            ✦
          </span>
        </div>
        <h1 className="text-[22px] font-semibold tracking-tight text-fg-1">
          {t.registerPage.headline}
        </h1>
        <p className="mt-2 text-[14px] leading-relaxed text-fg-3">
          {t.registerPage.subtitle}
        </p>
        {expiresDate && (
          <p className="mt-2 text-[12px] text-fg-4">
            {t.registerPage.expiry.replace("{date}", expiresDate)}
          </p>
        )}
      </div>

      {/* Sign-in surfaces */}
      {showEmailFlow ? (
        <div className="animate-fade-up">
          <EmailOtpFlow
            inviteToken={token}
            defaultEmail={inviteInfo?.email ?? ""}
          />
          <button
            type="button"
            onClick={() => setShowEmailFlow(false)}
            className="mt-4 w-full text-center text-[12px] text-fg-4 hover:text-fg-2 cursor-pointer transition-colors"
          >
            {t.auth.emailSignIn.back}
          </button>
        </div>
      ) : (
        <>
          <button
            onClick={() => login(callbackWithInvite, "github")}
            className="h-12 w-full rounded-chrome bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center gap-2.5 cursor-pointer hover:opacity-90 active:opacity-80 transition-opacity"
          >
            <GitHubIcon className="size-4.5" />
            {t.auth.continueWithGithub}
          </button>

          <button
            onClick={() => login(callbackWithInvite, "google")}
            className="mt-3 h-12 w-full rounded-chrome border border-border bg-surface-1 text-fg-2 font-semibold text-[14px] inline-flex items-center justify-center gap-2.5 cursor-pointer hover:bg-surface-2 hover:text-fg-1 active:opacity-80 transition-colors"
          >
            <GoogleIcon className="size-4.5" />
            {t.auth.continueWithGoogle}
          </button>

          <button
            onClick={() => setShowEmailFlow(true)}
            className="mt-3 h-12 w-full rounded-chrome border border-border bg-surface-1 text-fg-2 font-semibold text-[14px] inline-flex items-center justify-center gap-2.5 cursor-pointer hover:bg-surface-2 hover:text-fg-1 active:opacity-80 transition-colors"
          >
            <EmailIcon className="size-4.5" />
            {t.auth.continueWithEmail}
          </button>
        </>
      )}

      {/* Legal */}
      <p className="mt-6 text-center text-[11px] leading-relaxed text-fg-4">
        {t.auth.legal}{" "}
        <a
          href="/terms"
          className="text-fg-3 underline underline-offset-2 hover:text-fg-2 transition-colors"
        >
          {t.auth.terms}
        </a>
        {" & "}
        <a
          href="/privacy"
          className="text-fg-3 underline underline-offset-2 hover:text-fg-2 transition-colors"
        >
          {t.auth.privacy}
        </a>
      </p>
    </div>
  );
}

export default function RegisterPage() {
  return (
    <main className="relative flex min-h-dvh items-center justify-center px-4 bg-background overflow-hidden">
      {/* Ambient dream glow */}
      <div
        className="pointer-events-none fixed inset-0"
        style={{ background: DREAM_AMBIENT }}
      />

      <div className="relative z-10 flex flex-col items-center w-full">
        {/* Wordmark */}
        <div className="mb-10">
          <div
            className="h-7 w-36"
            style={{
              backgroundColor: "var(--foreground)",
              opacity: 0.85,
              maskImage: "url(/images/memax-wordmark.svg)",
              maskSize: "contain",
              maskRepeat: "no-repeat",
              maskPosition: "center",
              WebkitMaskImage: "url(/images/memax-wordmark.svg)",
              WebkitMaskSize: "contain",
              WebkitMaskRepeat: "no-repeat",
              WebkitMaskPosition: "center",
            }}
          />
        </div>

        <Suspense fallback={<MemaxLoader />}>
          <RegisterHandler />
        </Suspense>
      </div>
    </main>
  );
}
