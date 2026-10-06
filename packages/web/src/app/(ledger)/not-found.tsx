"use client";

// notFound() anywhere in the (ledger) tree renders this inside the
// Ledger root layout. URLs that match no route at all render
// app/global-not-found.tsx (the V1 404) because V1 is still the default
// UI; that moves here at cutover.

import { usePathname } from "next/navigation";
import { useLocale } from "@/i18n";
import { StatusPage, statusActionClass } from "./_components/status-page";

export default function LedgerNotFound() {
  const { t } = useLocale();
  const pathname = usePathname();
  const copy = t.ledger.notFound;
  return (
    <StatusPage
      receipt={pathname ?? undefined}
      title={copy.title}
      description={copy.description}
      actions={
        <a href="/" className={statusActionClass}>
          {copy.home}
        </a>
      }
    />
  );
}
