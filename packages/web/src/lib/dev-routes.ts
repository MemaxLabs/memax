/**
 * Whether the /dev/* fixture routes (V1 and Ledger) and the /dev/ui
 * toggle are served. Production hides them, except when a build opts
 * in with NEXT_PUBLIC_DEV_FIXTURES=1 (screenshot runs need a hydrated
 * production bundle). Never set that for a real deploy.
 */
export function devRoutesEnabled(): boolean {
  return (
    process.env.NODE_ENV !== "production" ||
    process.env.NEXT_PUBLIC_DEV_FIXTURES === "1"
  );
}
