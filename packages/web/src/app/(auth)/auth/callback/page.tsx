"use client";

import { Suspense, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { MemaxLoader } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { DREAM_AMBIENT } from "@memaxlabs/ui/tokens/dream";

type ErrorKind = "account_not_allowed" | "generic";

function CallbackError({ kind }: { kind: ErrorKind }) {
  const { t } = useLocale();

  const title =
    kind === "account_not_allowed"
      ? t.auth.accountNotAllowedTitle
      : t.auth.callbackError;
  const desc =
    kind === "account_not_allowed"
      ? t.auth.accountNotAllowedDesc
      : t.auth.callbackErrorDesc;

  return (
    <main className="relative flex min-h-dvh items-center justify-center px-4 bg-background overflow-hidden">
      <div
        className="pointer-events-none fixed inset-0"
        style={{ background: DREAM_AMBIENT }}
      />

      <div className="relative z-10 flex flex-col items-center w-full max-w-sm">
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

        <div className="mt-10 text-center">
          <p className="text-[17px] font-medium text-foreground">{title}</p>
          <p className="mt-2 text-[14px] leading-relaxed text-foreground/55">
            {desc}
          </p>
        </div>

        <a
          href="/login"
          className="mt-8 h-12 w-full rounded-chrome bg-foreground text-background font-semibold text-[14px] inline-flex items-center justify-center cursor-pointer hover:opacity-90 active:opacity-80 transition-opacity"
        >
          {t.auth.backToSignIn}
        </a>
      </div>
    </main>
  );
}

function CallbackHandler() {
  const params = useSearchParams();
  const { completeLogin } = useAuth();
  const { t } = useLocale();
  const [status, setStatus] = useState<"loading" | "success" | "error">(
    "loading",
  );
  const [errorKind, setErrorKind] = useState<ErrorKind>("generic");
  const flowRef = useRef<{
    key: string;
    promise: Promise<boolean>;
  } | null>(null);

  useEffect(() => {
    const errorCode = params.get("error");
    if (errorCode === "registration_required") {
      window.location.replace("/waitlist?status=not_registered");
      return;
    }
    if (
      errorCode === "invalid_invite" ||
      errorCode === "invite_email_mismatch"
    ) {
      const invite = params.get("invite") ?? "";
      const dest = invite
        ? `/register?invite=${invite}&error=${errorCode}`
        : `/waitlist?status=${errorCode}`;
      window.location.replace(dest);
      return;
    }
    if (errorCode === "account_not_allowed") {
      setErrorKind("account_not_allowed");
      setStatus("error");
      return;
    }
    if (errorCode) {
      setErrorKind("generic");
      setStatus("error");
      return;
    }

    const code = params.get("code");
    const accessToken = params.get("access_token");
    const refreshToken = params.get("refresh_token");
    const inlineCredentialsKey =
      accessToken && refreshToken ? `${accessToken}:${refreshToken}` : null;
    let active = true;

    if (inlineCredentialsKey) {
      const nextAccessToken = accessToken;
      const nextRefreshToken = refreshToken;
      if (!nextAccessToken || !nextRefreshToken) {
        setStatus("error");
        return;
      }
      if (flowRef.current?.key !== inlineCredentialsKey) {
        flowRef.current = {
          key: inlineCredentialsKey,
          promise: completeLogin(nextAccessToken, nextRefreshToken),
        };
      }

      void flowRef.current.promise
        .then((ok) => {
          if (!active || !ok) {
            if (active) setStatus("error");
            return;
          }
          setStatus("success");
          const returnTo = localStorage.getItem("memax_return_to");
          if (returnTo) localStorage.removeItem("memax_return_to");
          window.location.replace(returnTo || "/home");
        })
        .catch(() => {
          if (!active) return;
          setStatus("error");
        });
      return () => {
        active = false;
      };
    }

    if (!code) {
      setStatus("error");
      return;
    }

    const flowKey = `code:${code}`;
    if (flowRef.current?.key !== flowKey) {
      flowRef.current = {
        key: flowKey,
        promise: (async () => {
          const res = await fetch("/api/auth/exchange", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ code }),
          });
          const json = (await res.json()) as {
            data?: { access_token?: string; refresh_token?: string };
            error?: { code: string; message: string };
          };
          if (!res.ok || json.error || !json.data) {
            throw new Error(json.error?.code ?? "auth_exchange_failed");
          }
          if (!json.data.access_token || !json.data.refresh_token) {
            throw new Error("auth_exchange_incomplete");
          }
          return completeLogin(json.data.access_token, json.data.refresh_token);
        })(),
      };
    }

    void flowRef.current.promise
      .then((ok) => {
        if (!active || !ok) {
          if (active) setStatus("error");
          return;
        }
        setStatus("success");
        const returnTo = localStorage.getItem("memax_return_to");
        if (returnTo) localStorage.removeItem("memax_return_to");
        window.location.replace(returnTo || "/home");
      })
      .catch(() => {
        if (!active) return;
        setStatus("error");
      });

    return () => {
      active = false;
    };
  }, [completeLogin, params]);

  if (status === "error") {
    return <CallbackError kind={errorKind} />;
  }

  return (
    <MemaxLoader
      label={
        status === "success" ? t.auth.callbackSuccess : t.auth.callbackLoading
      }
    />
  );
}

export default function AuthCallbackPage() {
  return (
    <Suspense fallback={<MemaxLoader />}>
      <CallbackHandler />
    </Suspense>
  );
}
