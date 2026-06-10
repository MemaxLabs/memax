import { AppShellClient } from "@/components/shell-v2/app-shell-client";

/**
 * Server-component (app) layout. Mounts the shell. Plan 24 phase 4b
 * removed the `memax_shell` cookie dispatch — pre-launch, no external
 * users — so v2 chrome is the only path. Mobile chrome that still
 * lives under the legacy "v1 mobile" code paths in
 * `AppShellClient` stays mounted unconditionally on `isMobile` until
 * plan 22 ships v2 mobile chrome.
 */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  return <AppShellClient>{children}</AppShellClient>;
}
