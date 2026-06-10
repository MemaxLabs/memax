"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import { ChevronRight } from "lucide-react";

/**
 * ContentDisclosure — spring-eased collapsible section.
 *
 * Uses CSS grid-template-rows (0fr → 1fr) for smooth height animation —
 * no max-height hack, works with any content height.
 * Respects prefers-reduced-motion (instant open/close).
 *
 * Kitchen reference: section 02 (Memory Detail) — raw content disclosure.
 * Reusable: memory detail, settings, dream reports, any collapsible section.
 */
export function ContentDisclosure({
  label,
  detail,
  defaultOpen = false,
  children,
}: {
  label: string;
  detail?: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const contentRef = useRef<HTMLDivElement>(null);

  // Respect prefers-reduced-motion
  const [reduceMotion, setReduceMotion] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReduceMotion(mq.matches);
    const handler = (e: MediaQueryListEvent) => setReduceMotion(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  const toggle = useCallback(() => setOpen((prev) => !prev), []);

  return (
    <div>
      <button
        onClick={toggle}
        aria-expanded={open}
        className="flex items-center gap-1.5 text-[13px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors py-2"
      >
        <ChevronRight
          className="h-3 w-3 shrink-0"
          style={{
            transform: open ? "rotate(90deg)" : "rotate(0deg)",
            transition: reduceMotion
              ? "none"
              : "transform 0.25s var(--ease-spring)",
          }}
        />
        <span>{label}</span>
        {detail && (
          <>
            <span className="text-fg-4">·</span>
            <span className="text-fg-4">{detail}</span>
          </>
        )}
      </button>
      {/* Grid-based collapse: 0fr → 1fr animates height smoothly */}
      <div
        className="grid"
        style={{
          gridTemplateRows: open ? "1fr" : "0fr",
          transition: reduceMotion
            ? "none"
            : "grid-template-rows 0.3s var(--ease-spring)",
        }}
      >
        <div ref={contentRef} className="overflow-hidden">
          {children}
        </div>
      </div>
    </div>
  );
}
