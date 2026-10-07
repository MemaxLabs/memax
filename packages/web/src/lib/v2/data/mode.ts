/**
 * Which data source the V2 frame uses, decided once per request in
 * (ledger)/(app)/layout.tsx from cookies the server can read:
 *
 * - A browser with a session (the session-presence cookie) reads its
 *   own record through memax.v2: `sdk`.
 * - Where dev fixtures are on (dev, and the Playwright build), a browser
 *   without one sees the handoff's demo dataset: `demo`. Screenshots
 *   stay deterministic and need no server.
 * - Anywhere else, a browser without a session signs in: `signin`.
 *
 * With dev fixtures on, `memax_v2_data=demo` forces the demo even when
 * signed in, to compare the frame with the boards.
 */
export const DEMO_DATA_COOKIE = "memax_v2_data";

export type FrameDataMode = "sdk" | "demo" | "signin";

export function pickDataMode({
  hasSession,
  dataCookie,
  devFixtures,
}: {
  hasSession: boolean;
  dataCookie: string | undefined;
  devFixtures: boolean;
}): FrameDataMode {
  if (devFixtures && dataCookie === "demo") return "demo";
  if (hasSession) return "sdk";
  return devFixtures ? "demo" : "signin";
}
