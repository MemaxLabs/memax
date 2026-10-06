"use client";

import { useEffect, useMemo, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Menu } from "@base-ui/react/menu";
import { Shell } from "@memaxlabs/ledger";
import { useAuth } from "@/lib/auth";
import { isSpaceSlug } from "@/lib/ui-gate";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import {
  LedgerDataProvider,
  useOverview,
  useViewer,
  type DataMode,
} from "../_lib/data";
import { useCurrentSpace, useFrameLocation } from "../_lib/location";
import { OverlayProvider, useOverlays } from "../_lib/overlays";
import { SpaceViewContext, type SpaceView } from "../_lib/space-context";
import { CommandCenter } from "./command-center";
import { FrameKeys } from "./frame-keys";
import { FrameFailed, FrameSkeleton } from "./frame-states";
import { MobileTabBar, MobileTopBar } from "./mobile-bars";
import { useRailProps } from "./rail-props";
import { ShortcutsSheet } from "./shortcuts-sheet";
import { SpaceMenuPopup } from "./space-menu";
import { SpaceNotFound } from "./status";
import { ToastProvider, ToastViewport } from "./toasts";
import styles from "./app-frame.module.css";

/**
 * The V2 app frame (plan §6.4): Shell with the rail, the space
 * switcher, ⌘K, the `?` sheet, toasts and the keymap, around every
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
  return () =>
    router.replace(`/login?returnTo=${encodeURIComponent(pathname ?? "/")}`);
}

/** No session and no dev fixtures: sign in (V1's page until V2's lands). */
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
  const { route, inSettings } = useFrameLocation();
  const viewer = useViewer();
  const overview = useOverview(space);
  const { openCommand } = useOverlays();
  const missing = missingSlug !== undefined;
  const nav = useRailProps({
    space,
    overview: overview.data,
    route: missing ? undefined : route,
    inSettings,
    viewer,
  });
  const { data, isError, refetch } = overview;
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
      <MobileTopBar
        space={space}
        spaces={spaces}
        route={route}
        viewer={viewer}
      />
      <Menu.Root>
        <Shell mainId="main" nav={nav}>
          {missing ? (
            <SpaceNotFound slug={missingSlug} fallback={space} />
          ) : (
            <SpaceViewContext value={view}>{children}</SpaceViewContext>
          )}
        </Shell>
        <SpaceMenuPopup
          spaces={spaces}
          current={missing ? undefined : space}
          route={route}
          // Lines the card up with the rail's edge, as drawn.
          anchorOffset={{ side: 8, align: -32 }}
        />
      </Menu.Root>
      <MobileTabBar
        space={space}
        route={route}
        waiting={overview.data?.waiting ?? 0}
        onAsk={() => openCommand("ask")}
      />
      <CommandCenter space={space} spaces={spaces} />
      <ShortcutsSheet />
    </div>
  );
}
