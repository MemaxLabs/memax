"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { MemaxLoader } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { DREAM_AMBIENT } from "@memaxlabs/ui/tokens/dream";
import { EmailOtpFlow } from "@/components/auth/email-otp-flow";

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 18 18" aria-hidden="true">
      <path
        fill="#4285F4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.33-1.58-5.04-3.72H.94v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#FBBC05"
        d="M3.96 10.7A5.41 5.41 0 0 1 3.68 9c0-.59.1-1.16.28-1.7V4.97H.94A9 9 0 0 0 0 9c0 1.45.34 2.82.94 4.03l3.02-2.33Z"
      />
      <path
        fill="#EA4335"
        d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58A8.68 8.68 0 0 0 9 0 9 9 0 0 0 .94 4.97L3.96 7.3C4.67 5.16 6.66 3.58 9 3.58Z"
      />
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

export default function LoginPage() {
  const { user, loading, login } = useAuth();
  const router = useRouter();
  const { t } = useLocale();
  const [showEmailFlow, setShowEmailFlow] = useState(false);

  useEffect(() => {
    if (!loading && user) {
      router.replace("/home");
    }
  }, [user, loading, router]);

  // While the auth provider is still resolving the session, render the
  // loader instead of the OAuth UI. Otherwise an authenticated user
  // hitting /login (or returning via bfcache) sees a flash of the
  // GitHub/Google buttons before the redirect-effect fires — the auth
  // state is briefly `loading=true, user=null` before settling to
  // `loading=false, user=<User>`. Only show the OAuth surface when we
  // know there is no user.
  if (loading || user) return <MemaxLoader />;

  return (
    <main className="relative flex min-h-dvh items-center justify-center px-4 bg-background overflow-hidden">
      {/* Ambient dream glow — subtle, alive */}
      <div
        className="pointer-events-none fixed inset-0"
        style={{
          background: DREAM_AMBIENT,
        }}
      />

      <div className="relative z-10 flex flex-col items-center w-full max-w-sm">
        {/* Wordmark */}
        <div
          className="h-8 w-40"
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

        {/* Tagline with breathing star */}
        <div className="mt-4 flex items-center gap-2">
          <span
            className="text-[14px] leading-none animate-pulse"
            style={{ color: "var(--signature)" }}
          >
            ✦
          </span>
          <p className="text-[15px] text-foreground/45">{t.auth.tagline}</p>
        </div>

        {/* CTA */}
        {showEmailFlow ? (
          <div className="mt-10 w-full animate-fade-up">
            <EmailOtpFlow />
            <button
              type="button"
              onClick={() => setShowEmailFlow(false)}
              className="mt-4 w-full text-center text-[12px] text-foreground/45 hover:text-foreground/70 cursor-pointer transition-colors"
            >
              {t.auth.emailSignIn.back}
            </button>
          </div>
        ) : (
          <>
            <button
              onClick={() => login(undefined, "github")}
              className="mt-12 h-12 w-full rounded-chrome bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center gap-2.5 cursor-pointer hover:opacity-90 active:opacity-80 transition-opacity"
            >
              <GitHubIcon className="size-4.5" />
              {t.auth.continueWithGithub}
            </button>

            <button
              onClick={() => login(undefined, "google")}
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
        <p className="mt-6 text-center text-[11px] leading-relaxed text-foreground/35">
          {t.auth.legal}{" "}
          <a
            href="/terms"
            className="text-foreground/40 underline underline-offset-2 hover:text-foreground/60 transition-colors"
          >
            {t.auth.terms}
          </a>
          {" & "}
          <a
            href="/privacy"
            className="text-foreground/40 underline underline-offset-2 hover:text-foreground/60 transition-colors"
          >
            {t.auth.privacy}
          </a>
        </p>
      </div>
    </main>
  );
}
