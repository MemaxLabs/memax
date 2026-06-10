"use client";

import * as React from "react";
import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip";
import { cn } from "../utils";

/**
 * Tooltip — hover-or-tap contextual hint anchored to a trigger.
 *
 * Thin wrapper over @base-ui/react/tooltip with memax styling. Handles
 * hover delay, touch handling, focus, ARIA, portal rendering, and
 * viewport collision avoidance automatically. Max width keeps copy
 * readable; use for 1-2 sentence explanations, not long-form content
 * (reach for Popover for that).
 *
 * Usage:
 *   <Tooltip>
 *     <TooltipTrigger>
 *       <button><Info /></button>
 *     </TooltipTrigger>
 *     <TooltipContent>
 *       memax dreams about your memories and groups them into topics.
 *     </TooltipContent>
 *   </Tooltip>
 */

function Tooltip({ ...props }: TooltipPrimitive.Root.Props) {
  return <TooltipPrimitive.Root data-slot="tooltip" {...props} />;
}

function TooltipProvider({ ...props }: TooltipPrimitive.Provider.Props) {
  return <TooltipPrimitive.Provider data-slot="tooltip-provider" {...props} />;
}

function TooltipTrigger({ ...props }: TooltipPrimitive.Trigger.Props) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />;
}

function TooltipPortal({ ...props }: TooltipPrimitive.Portal.Props) {
  return <TooltipPrimitive.Portal data-slot="tooltip-portal" {...props} />;
}

function TooltipPositioner({
  className,
  ...props
}: TooltipPrimitive.Positioner.Props) {
  return (
    <TooltipPrimitive.Positioner
      data-slot="tooltip-positioner"
      className={cn("z-popover outline-none", className)}
      {...props}
    />
  );
}

function TooltipContent({
  className,
  side = "top",
  sideOffset = 8,
  align = "center",
  children,
  ...props
}: TooltipPrimitive.Popup.Props &
  Pick<TooltipPrimitive.Positioner.Props, "side" | "sideOffset" | "align">) {
  return (
    <TooltipPortal>
      <TooltipPositioner side={side} sideOffset={sideOffset} align={align}>
        <TooltipPrimitive.Popup
          data-slot="tooltip-content"
          className={cn(
            // Solid card surface — same compositing philosophy as Popover
            // (no backdrop-filter; GPU stays on paint, not compositing).
            "max-w-[260px] rounded-lg border border-border bg-card px-3 py-2",
            "text-[12px] leading-[1.5] text-fg-2 outline-none",
            "shadow-[0_8px_32px_oklch(0_0_0/0.08),0_2px_8px_oklch(0_0_0/0.04)]",
            "data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-open:duration-150",
            "data-closed:animate-out data-closed:fade-out-0 data-closed:duration-100",
            className,
          )}
          {...props}
        >
          {children}
        </TooltipPrimitive.Popup>
      </TooltipPositioner>
    </TooltipPortal>
  );
}

export {
  Tooltip,
  TooltipProvider,
  TooltipTrigger,
  TooltipPortal,
  TooltipPositioner,
  TooltipContent,
};
