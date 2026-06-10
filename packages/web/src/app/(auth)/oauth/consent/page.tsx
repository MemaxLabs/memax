"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { AlertCircle } from "lucide-react";

import { MemaxLoader } from "@memaxlabs/ui";
import { DREAM_AMBIENT } from "@memaxlabs/ui/tokens/dream";
import { useLocale } from "@/i18n";
import { getPublicMemaxClient } from "@/lib/memax-client";
import type { ConsentLoadState } from "./consent-labels";
import { ConsentWordmark } from "./consent-wordmark";
import { ConsentForm } from "./consent-form";

function OAuthConsentContent() {
  const searchParams = useSearchParams();
  const { t } = useLocale();
  const [state, setState] = useState<ConsentLoadState>({ status: "loading" });

  useEffect(() => {
    const requestId = searchParams.get("request_id");
    const consentToken = searchParams.get("consent_token");
    if (!requestId || !consentToken) {
      setState({
        status: "error",
        message: t.auth.oauthConsent.missingRequest,
      });
      return;
    }

    let cancelled = false;
    setState({ status: "loading" });
    getPublicMemaxClient()
      .auth.getOAuthConsentRequest(requestId, consentToken)
      .then((data) => {
        if (cancelled) return;
        setState({ status: "ready", data });
      })
      .catch(() => {
        if (cancelled) return;
        setState({ status: "error" });
      });

    return () => {
      cancelled = true;
    };
  }, [searchParams, t.auth.oauthConsent.missingRequest]);

  return (
    <main className="relative flex min-h-dvh items-center justify-center overflow-hidden bg-background px-4 py-12">
      <div
        className="pointer-events-none fixed inset-0"
        style={{ background: DREAM_AMBIENT }}
      />

      <div className="relative z-10 w-full max-w-lg">
        <div className="mb-10 flex justify-center">
          <ConsentWordmark />
        </div>

        {state.status === "loading" ? (
          <div className="flex min-h-72 flex-col items-center justify-center gap-4 text-center">
            <MemaxLoader inline />
            <p className="text-[14px] text-fg-3">
              {t.auth.oauthConsent.loading}
            </p>
          </div>
        ) : state.status === "error" ? (
          <div className="animate-fade-up flex min-h-72 flex-col items-center justify-center gap-5 text-center">
            <div className="flex size-10 items-center justify-center rounded-chrome bg-surface-1 text-fg-3">
              <AlertCircle className="size-5" aria-hidden="true" />
            </div>
            <div>
              <h1 className="font-heading text-lg font-semibold text-fg-1">
                {t.auth.oauthConsent.errorTitle}
              </h1>
              <p className="mt-2 max-w-sm text-[14px] leading-relaxed text-fg-3">
                {state.message ?? t.auth.oauthConsent.errorDescription}
              </p>
            </div>
          </div>
        ) : (
          <ConsentForm data={state.data} />
        )}
      </div>
    </main>
  );
}

export default function OAuthConsentPage() {
  return (
    <Suspense fallback={<MemaxLoader />}>
      <OAuthConsentContent />
    </Suspense>
  );
}
