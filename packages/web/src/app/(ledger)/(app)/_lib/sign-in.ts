"use client";

import { useCallback } from "react";
import { usePathname, useRouter } from "next/navigation";
import { signInHref } from "@/lib/v2/onboarding/routes";

/**
 * Sends the person to sign in again and back here: a fresh session
 * issued to the web app, which is what a change needing a person on the
 * web (D15: raising an agent, answering a team's decision) asks for.
 */
export function useSignInAgain() {
  const router = useRouter();
  const pathname = usePathname();
  return useCallback(
    () => router.push(signInHref(pathname ?? "/", { again: true })),
    [router, pathname],
  );
}
