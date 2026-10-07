"use client";

import { useEffect, type ReactNode } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Logo } from "@memaxlabs/ledger";
import { useAuth } from "@/lib/auth";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { signInHref } from "@/lib/v2/onboarding/routes";
import { LedgerDataProvider, type DataMode } from "../(app)/_lib/data";
import { ToastProvider, ToastViewport } from "../(app)/_components/toasts";
import styles from "./frame.module.css";

/**
 * The frame of the first session's own pages (CliAuth and the setup
 * screens), which sit outside a space: no rail, the wordmark and a line
 * on the right, and the same data source, keymap and toasts as the app
 * frame. `mode` comes from the layout (lib/v2/data/mode.ts); without a
 * session these pages sign in first and come back.
 */
export function OnboardingFrame({
  mode,
  children,
}: {
  mode: DataMode | "signin";
  children: ReactNode;
}) {
  if (mode === "signin") return <SignInFirst />;
  return (
    <LedgerDataProvider mode={mode}>
      <KeymapProvider>
        <ToastProvider>
          <SessionGuard mode={mode}>{children}</SessionGuard>
          <ToastViewport />
        </ToastProvider>
      </KeymapProvider>
    </LedgerDataProvider>
  );
}

function useHereHref(): string {
  const pathname = usePathname() ?? "/";
  const params = useSearchParams();
  const query = params?.toString();
  return query ? `${pathname}?${query}` : pathname;
}

/** No session: sign in, then come back here. */
function SignInFirst() {
  const router = useRouter();
  const here = useHereHref();
  useEffect(() => {
    router.replace(signInHref(here));
  }, [router, here]);
  return null;
}

/** A session that ends mid-visit goes back to sign in. */
function SessionGuard({
  mode,
  children,
}: {
  mode: DataMode;
  children: ReactNode;
}) {
  const { user, loading } = useAuth();
  const router = useRouter();
  const here = useHereHref();
  const signedOut = mode === "sdk" && !loading && !user;
  useEffect(() => {
    if (signedOut) router.replace(signInHref(here));
  }, [signedOut, router, here]);
  if (mode === "sdk" && !user) return null;
  return <>{children}</>;
}

/**
 * One page of the first session: the wordmark on the left, `meta` on the
 * right ("Set up · step 2 of 3"), and the page below, centred to `width`.
 */
export function OnboardingPage({
  meta,
  children,
  width = "wide",
  align = "start",
  fill = false,
}: {
  meta?: ReactNode;
  children: ReactNode;
  /** `wide` 1280px (setup), `narrow` 1080px (CompileDone), `split` 1180px (CliAuth). */
  width?: "wide" | "narrow" | "split";
  /** `center` puts the page in the middle of the window (CliAuth, FirstRun). */
  align?: "start" | "center";
  /** The page fills the window's height (FirstRun's two columns). */
  fill?: boolean;
}) {
  return (
    <div className={`${styles.page} ${fill ? styles.fill : ""}`}>
      <header className={styles.head}>
        <Logo variant="lockup" size={18} />
        {meta ? <div className={styles.meta}>{meta}</div> : null}
      </header>
      <main
        className={`${styles.main} ${styles[width]} ${
          align === "center" ? styles.center : ""
        }`}
      >
        {children}
      </main>
    </div>
  );
}
