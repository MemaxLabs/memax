import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { SignInScreen } from "../../_onboarding/sign-in";

export const metadata: Metadata = {
  title: en.ledger.onboarding.signIn.heading,
};

// SignIn (SignIn.png). Reached signed out from the app frame, /device and
// the setup screens, each with ?next= to come back to.
export default function SignInPage() {
  return (
    <Suspense>
      <SignInScreen />
    </Suspense>
  );
}
