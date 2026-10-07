"use client";

// V1 root-level error boundary. Catches render errors from the (v1)
// root layout's children AND from V1 routes outside the named route
// groups ((v1)/invite/[token], (v1)/dev/*). Segment-level boundaries
// ((app)/error.tsx etc.) take precedence for their own subtrees; this
// file only fires when nothing lower catches.

import { ErrorFallback } from "@/components/features/error/error-fallback";

export default function RootError({
  error,
  reset,
  unstable_retry,
}: {
  error: Error & { digest?: string };
  reset: () => void;
  unstable_retry?: () => void;
}) {
  return (
    <ErrorFallback
      error={error}
      reset={reset}
      unstable_retry={unstable_retry}
      boundary="root"
      homeHref="/"
    />
  );
}
