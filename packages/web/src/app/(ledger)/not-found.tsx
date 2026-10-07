"use client";

// notFound() anywhere in the (ledger) tree outside a space renders this
// inside the Ledger root layout (a space's own pages use
// (app)/[space]/not-found.tsx, inside the frame). URLs that match no
// route at all render app/global-not-found.tsx (the V1 404) because V1
// is still the default UI; that moves here at cutover.

import { usePathname } from "next/navigation";
import { Button } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { StatusPage } from "./_components/status-page";

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
        <Button variant="secondary" href="/">
          {copy.home}
        </Button>
      }
    />
  );
}
