import { redirect } from "next/navigation";

/**
 * `/home` is the v1 brain landing URL. Plan 24 phase 4b retires v1
 * across the board — no kill switch, no soak. All visits redirect to
 * `/brain`, which is the canonical v2 path that the LeftRail Brain
 * tab links to.
 *
 * Server-side `redirect()` avoids the client-side flash that a
 * `useEffect` redirect would produce.
 */
export default function HomePage() {
  redirect("/brain");
}
