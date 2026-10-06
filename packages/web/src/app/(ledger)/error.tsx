"use client";

// Error boundary for the (ledger) tree, inside the Ledger root layout.
// Errors in the root layout itself fall through to app/global-error.tsx.

import { useEffect } from "react";
import { interpolate, useLocale } from "@/i18n";
import { reportRenderError } from "@/lib/report-render-error";
import {
  StatusPage,
  statusActionClass,
  statusPrimaryActionClass,
} from "./_components/status-page";

export default function LedgerError({
  error,
  reset,
  unstable_retry,
}: {
  error: Error & { digest?: string };
  reset: () => void;
  unstable_retry?: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.error;

  useEffect(() => {
    reportRenderError(error, {
      digest: error.digest,
      boundary: "ledger",
      pathname: window.location.pathname,
    });
  }, [error]);

  return (
    <StatusPage
      role="alert"
      receipt={
        error.digest
          ? interpolate(copy.digest, { digest: error.digest })
          : undefined
      }
      title={copy.title}
      description={copy.description}
      actions={
        <>
          <button
            type="button"
            className={statusPrimaryActionClass}
            onClick={unstable_retry ?? reset}
          >
            {copy.retry}
          </button>
          <a href="/" className={statusActionClass}>
            {copy.home}
          </a>
        </>
      }
    />
  );
}
