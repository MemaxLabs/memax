import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { CompileDoneScreen } from "../../../_onboarding/compile-done";

export const metadata: Metadata = {
  title: en.ledger.onboarding.frame.titles.done,
};

export default function Page() {
  return (
    <Suspense>
      <CompileDoneScreen />
    </Suspense>
  );
}
