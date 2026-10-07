import { cookies } from "next/headers";
import { devRoutesEnabled } from "@/lib/dev-routes";
import { SESSION_PRESENCE_COOKIE } from "@/lib/session-presence";
import {
  DEMO_DATA_COOKIE,
  pickDataMode,
  type FrameDataMode,
} from "@/lib/v2/data/mode";

/**
 * The data source of the first session's pages, decided per request the
 * way the app frame's is ((app)/layout.tsx, lib/v2/data/mode.ts): the
 * person's record with a session, the demo with dev fixtures and none,
 * else sign in first.
 */
export async function onboardingMode(): Promise<FrameDataMode> {
  const jar = await cookies();
  return pickDataMode({
    hasSession: jar.get(SESSION_PRESENCE_COOKIE)?.value === "1",
    dataCookie: jar.get(DEMO_DATA_COOKIE)?.value,
    devFixtures: devRoutesEnabled(),
  });
}
