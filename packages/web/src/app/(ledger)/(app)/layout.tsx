import { cookies } from "next/headers";
import { devRoutesEnabled } from "@/lib/dev-routes";
import { SESSION_PRESENCE_COOKIE } from "@/lib/session-presence";
import { DEMO_DATA_COOKIE, pickDataMode } from "@/lib/v2/data/mode";
import { AppFrame } from "./_components/app-frame";

// The V2 app frame around every place (/[space]/…) and settings page:
// Shell, rail, ⌘K, the keyboard sheet and toasts. Reading the session
// cookie here picks the data source once (lib/v2/data/mode.ts); the
// (ledger) root layout above stays static.

export default async function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const jar = await cookies();
  const mode = pickDataMode({
    hasSession: jar.get(SESSION_PRESENCE_COOKIE)?.value === "1",
    dataCookie: jar.get(DEMO_DATA_COOKIE)?.value,
    devFixtures: devRoutesEnabled(),
  });
  return <AppFrame mode={mode}>{children}</AppFrame>;
}
