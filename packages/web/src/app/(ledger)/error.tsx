"use client";

// Error boundary for the (ledger) tree, inside the Ledger root layout.
// Errors in the root layout itself fall through to app/global-error.tsx.

import { useEffect } from "react";
import { Button } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { reportRenderError } from "@/lib/report-render-error";
import { StatusPage } from "./_components/status-page";

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
          <Button variant="primary" onClick={unstable_retry ?? reset}>
            {copy.retry}
          </Button>
          <Button variant="quiet" href="/">
            {copy.home}
          </Button>
        </>
      }
    />
  );
}
