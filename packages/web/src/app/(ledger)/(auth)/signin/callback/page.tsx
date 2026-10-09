import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { SignInCallbackScreen } from "../../../_onboarding/sign-in-callback";

export const metadata: Metadata = {
  title: en.ledger.onboarding.signIn.heading,
};

export default function SignInCallbackPage() {
  return (
    <Suspense>
      <SignInCallbackScreen />
    </Suspense>
  );
}
