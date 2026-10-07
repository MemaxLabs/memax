import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { UnsubscribeScreen } from "./unsubscribe-screen";

export const metadata: Metadata = {
  title: en.ledger.dream.unsubscribe.title,
  robots: { index: false },
};

// /unsubscribe?token=… (the morning email's link): open to every
// browser, signed in or not, since the email goes to people who may
// never have opted into the V2 UI.
export default function UnsubscribePage() {
  return (
    <Suspense>
      <UnsubscribeScreen />
    </Suspense>
  );
}
