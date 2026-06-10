"use client";

export function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd
      className="inline-flex min-w-[18px] items-center justify-center rounded bg-surface-2 px-1 text-[10px] leading-none text-fg-3"
      style={{
        height: 18,
        fontFamily:
          "var(--font-mono), ui-monospace, SFMono-Regular, Menlo, monospace",
      }}
    >
      {children}
    </kbd>
  );
}
