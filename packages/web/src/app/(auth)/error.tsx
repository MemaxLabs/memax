"use client";

import { ErrorFallback } from "@/components/features/error/error-fallback";

export default function AuthError({
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
      boundary="auth"
      homeHref="/login"
      homeLabelKey="backToSignIn"
    />
  );
}
