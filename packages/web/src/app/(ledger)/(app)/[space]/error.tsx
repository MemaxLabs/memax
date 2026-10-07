"use client";

// A place that fails to render: the States board's error, inside the
// frame (rail, ⌘K and the keys keep working). Errors in the frame
// itself reach (ledger)/error.tsx.

import { use, useEffect } from "react";
import { reportRenderError } from "@/lib/report-render-error";
import { PlaceError } from "../_components/status";
import { SpaceViewContext } from "../_lib/space-context";

export default function SpaceError({
  error,
  reset,
  unstable_retry,
}: {
  error: Error & { digest?: string };
  reset: () => void;
  unstable_retry?: () => void;
}) {
  const view = use(SpaceViewContext);

  useEffect(() => {
    reportRenderError(error, {
      digest: error.digest,
      boundary: "ledger-space",
      pathname: window.location.pathname,
    });
  }, [error]);

  return (
    <PlaceError
      space={view?.space}
      digest={error.digest}
      onRetry={unstable_retry ?? reset}
    />
  );
}
