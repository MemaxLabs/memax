import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { CleanupScreen } from "../../../_onboarding/cleanup";

export const metadata: Metadata = {
  title: en.ledger.onboarding.frame.titles.cleanup,
};

export default function Page() {
  return (
    <Suspense>
      <CleanupScreen />
    </Suspense>
  );
}
