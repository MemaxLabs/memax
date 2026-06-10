// ═══════════════════════════════════════════════
// DEPRECATED — Kitchen has moved to packages/ui
//
// The canonical kitchen lives at:
//   packages/ui/app/dev/kitchen/page.tsx (port 3003)
//
// This page redirects to the UI kitchen.
// Do NOT add sections, imports, or nav entries here.
// All kitchen work goes in packages/ui/.
// ═══════════════════════════════════════════════

export default function DeprecatedKitchenPage() {
  return (
    <div className="flex flex-col items-center justify-center min-h-screen gap-4 p-8 text-center">
      <h1 className="text-[21px] font-bold">Kitchen moved</h1>
      <p className="text-[15px] text-fg-2 max-w-md">
        The design system kitchen now lives in{" "}
        <code className="text-[14px] bg-surface-2 px-1.5 py-0.5 rounded">
          packages/ui
        </code>{" "}
        and runs on{" "}
        <a
          href="http://localhost:3003/dev/kitchen"
          className="underline text-fg-1"
        >
          localhost:3003
        </a>
        .
      </p>
      <a
        href="http://localhost:3003/dev/kitchen"
        className="text-[14px] font-medium px-4 py-2 rounded-surface"
        style={{ background: "var(--foreground)", color: "var(--background)" }}
      >
        Go to Kitchen
      </a>
    </div>
  );
}
