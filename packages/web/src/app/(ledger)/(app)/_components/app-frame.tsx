"use client";

import { useEffect, useMemo, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Shell } from "@memaxlabs/ledger";
import { useAuth } from "@/lib/auth";
import { isSpaceSlug } from "@/lib/ui-gate";
import { signInHref } from "@/lib/v2/onboarding/routes";
import { waitingOnYou, type SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import {
  LedgerDataProvider,
  useOverview,
  useViewer,
  type DataMode,
} from "../_lib/data";
import { useCurrentSpace, useFrameLocation } from "../_lib/location";
import { OverlayProvider, useOverlays } from "../_lib/overlays";
import { ownsPhoneHeader } from "../_lib/phone";
import { SpaceViewContext, type SpaceView } from "../_lib/space-context";
import { CommandCenter } from "./command-center";
import { FrameKeys } from "./frame-keys";
import { FrameFailed, FrameSkeleton } from "./frame-states";
import { MobileTabBar, MobileTopBar } from "./mobile-bars";
import { NewSpaceDialog } from "./new-space";
import { PasskeyCheckHost } from "./passkey-check";
import { useRailProps } from "./rail-props";
import { ShortcutsSheet } from "./shortcuts-sheet";
import { SpaceMenu } from "./space-menu";
import { SpaceNotFound } from "./status";
import { ToastProvider, ToastViewport } from "./toasts";
import { VerifyStale } from "./verify-stale";
import styles from "./app-frame.module.css";

/**
 * The V2 app frame (plan §6.4): Shell with the rail, the space
 * switcher and New space, ⌘K, the `?` sheet, toasts and the keymap, around every
 * place and settings page. `mode` says where the data comes from; the
 * layout decides it.
 */
export function AppFrame({
  mode,
  children,
}: {
  mode: DataMode | "signin";
  children: ReactNode;
}) {
  if (mode === "signin") return <SignInRedirect />;
  return (
    <LedgerDataProvider mode={mode}>
      <KeymapProvider>
        <ToastProvider>
          <OverlayProvider>
            <FrameBody mode={mode}>{children}</FrameBody>
            <PasskeyCheckHost />
            <ToastViewport />
          </OverlayProvider>
        </ToastProvider>
      </KeymapProvider>
    </LedgerDataProvider>
  );
}

function useSignIn() {
  const router = useRouter();
  const pathname = usePathname();
  return () => router.replace(signInHref(pathname ?? "/"));
}

/** No session and no dev fixtures: sign in, then come back. */
function SignInRedirect() {
  const signIn = useSignIn();
  useEffect(() => {
    signIn();
  });
  return <FrameSkeleton />;
}

function FrameBody({
  mode,
  children,
}: {
  mode: DataMode;
  children: ReactNode;
}) {
  const { current, spaces } = useCurrentSpace();
  const { slug } = useFrameLocation();
  const { user, loading } = useAuth();
  const signIn = useSignIn();
  // A session that ends mid-visit (expired, signed out elsewhere) goes
  // back to sign in.
  const signedOut = mode === "sdk" && !loading && !user;
  useEffect(() => {
    if (signedOut) signIn();
  });

  // A reserved word in the space position (/home/today) is no space:
  // render the route, whose layout answers 404 for the whole page.
  if (slug !== undefined && !isSpaceSlug(slug)) return <>{children}</>;
  if (signedOut || current.status === "loading") return <FrameSkeleton />;
  if (current.status === "error")
    return <FrameFailed onRetry={current.retry} />;
  const space = current.status === "ready" ? current.space : current.fallback;
  if (!space) return <FrameFailed />;
  return (
    <FrameReady
      space={space}
      spaces={spaces}
      missingSlug={current.status === "missing" ? current.slug : undefined}
    >
      {children}
    </FrameReady>
  );
}

function FrameReady({
  space,
  spaces,
  missingSlug,
  children,
}: {
  space: SpaceSummary;
  spaces: SpaceSummary[];
  /** The URL names a space the person can't open; the rail shows `space`. */
  missingSlug: string | undefined;
  children: ReactNode;
}) {
  const { route, detail, inSettings } = useFrameLocation();
  const viewer = useViewer();
  const overview = useOverview(space);
  const { openCommand } = useOverlays();
  const missing = missingSlug !== undefined;
  const nav = useRailProps({
    space,
    overview: overview.data,
    overviewFailed: overview.isError,
    route: missing ? undefined : route,
    inSettings,
    viewer,
  });
  const { data, isError, refetch } = overview;
  const waiting = data ? waitingOnYou(data) : 0;
  const view = useMemo<SpaceView>(
    () => ({
      space,
      overview: data,
      overviewFailed: isError,
      retryOverview: () => void refetch(),
    }),
    [space, data, isError, refetch],
  );

  return (
    <div className={styles.frame}>
      <FrameKeys space={space} spaces={spaces} route={route} />
      {/* On a phone, Review and a memory's page draw their own header
          (a back link and their title) where the space's bar would be. */}
      {missing || !ownsPhoneHeader(route, detail) ? (
        <MobileTopBar
          space={space}
          spaces={spaces}
          route={route}
          viewer={viewer}
        />
      ) : null}
      {/* The rail's space name is the menu's trigger (NavRail spaceRender). */}
      <SpaceMenu
        spaces={spaces}
        current={missing ? undefined : space}
        route={route}
        // Lines the card up with the rail's edge, as drawn.
        anchorOffset={{ side: 8, align: -32 }}
      >
        <Shell mainId="main" nav={nav}>
          {missing ? (
            <SpaceNotFound slug={missingSlug} fallback={space} />
          ) : (
            <SpaceViewContext value={view}>
              {children}
              {/* Verify reads the place's view (its space, clock and zone). */}
              <VerifyStale />
            </SpaceViewContext>
          )}
        </Shell>
      </SpaceMenu>
      <MobileTabBar
        space={space}
        route={route}
        waiting={waiting}
        onAsk={() => openCommand("ask")}
      />
      <CommandCenter
        space={space}
        spaces={spaces}
        route={route}
        waiting={waiting}
      />
      <ShortcutsSheet />
      <NewSpaceDialog />
    </div>
  );
}
