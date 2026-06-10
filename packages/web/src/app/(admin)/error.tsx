"use client";

import { useLocale } from "@/i18n";
import { ErrorFallback } from "@/components/features/error/error-fallback";

export default function AdminError({
  error,
  reset,
  unstable_retry,
}: {
  error: Error & { digest?: string };
  reset: () => void;
  unstable_retry?: () => void;
}) {
  const { t } = useLocale();
  return (
    <ErrorFallback
      error={error}
      reset={reset}
      unstable_retry={unstable_retry}
      boundary="admin"
      homeHref="/home"
      hint={t.errors.adminHint}
    />
  );
}
