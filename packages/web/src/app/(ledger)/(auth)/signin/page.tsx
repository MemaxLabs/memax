import { Suspense } from "react";
import type { Metadata } from "next";
import { cookies } from "next/headers";
import { en } from "@/i18n/locales/en";
import { SESSION_PRESENCE_COOKIE } from "@/lib/session-presence";
import { SignInScreen } from "../../_onboarding/sign-in";

export const metadata: Metadata = {
  title: en.ledger.onboarding.signIn.heading,
};

// SignIn (SignIn.png). Reached signed out from the app frame, /device and
// the setup screens, each with ?next= to come back to; and signed in from
// the proxy, to read the person's V2 UI flag again (lib/ui-gate.ts), when
// the page lands instead of showing the form.
export default async function SignInPage() {
  const jar = await cookies();
  const restoring = jar.get(SESSION_PRESENCE_COOKIE)?.value === "1";
  return (
    <Suspense>
      <SignInScreen restoring={restoring} />
    </Suspense>
  );
}
