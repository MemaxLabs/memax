"use client";

import * as React from "react";
import { Popover as PopoverPrimitive } from "@base-ui/react/popover";
import { cn } from "../utils";

/**
 * Popover — positioned floating panel anchored to a trigger.
 *
 * Thin wrapper over @base-ui/react/popover with the shared memax glass
 * material. Handles: positioning (Floating UI), click-outside, focus
 * management, ARIA, portal rendering, viewport collision avoidance.
 *
 * Material: always `.glass-dropdown` (defined in popover-glass.css) —
 * part of the shared glass family on the Topic Explorer tree
 * (`.glass-panel`), the command bar (`.glass-bar`), and the batch
 * toolbar (`.glass-toolbar`). All four recipes compose the same fill
 * + border tokens from globals.css (--glass-fill-*,
 * --glass-border-hairline, --glass-inset-highlight) but carry
 * deliberately distinct blur weights (40px md for dropdown, 36px for
 * panel, 50px lg for bar + toolbar) and shadow curves to match each
 * surface's role. Every popup in the app ships on this dropdown
 * recipe so menus, context popovers, InfoPopovers, pickers, and hub
 * switchers read as one material family.
 *
 * For centered modals, use Dialog instead.
 */

/**
 * Z-layer: popover. Sits above `z-modal` so a popover triggered inside
 * a Settings dialog renders correctly, and below `z-takeover` so a
 * full-screen surface (lightbox, hub-create, mobile compose) can paint
 * above a stray open popover. Keep in sync with the z-scale in
 * globals.css (`--z-popover`).
 */

function Popover({ ...props }: PopoverPrimitive.Root.Props) {
  return <PopoverPrimitive.Root data-slot="popover" {...props} />;
}

function PopoverTrigger({ ...props }: PopoverPrimitive.Trigger.Props) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />;
}

function PopoverPortal({ ...props }: PopoverPrimitive.Portal.Props) {
  return <PopoverPrimitive.Portal data-slot="popover-portal" {...props} />;
}

function PopoverBackdrop({
  className,
  ...props
}: PopoverPrimitive.Backdrop.Props) {
  return (
    <PopoverPrimitive.Backdrop
      data-slot="popover-backdrop"
      className={cn("fixed inset-0 z-popover", className)}
      {...props}
    />
  );
}

function PopoverPositioner({
  className,
  ...props
}: PopoverPrimitive.Positioner.Props) {
  return (
    <PopoverPrimitive.Positioner
      data-slot="popover-positioner"
      className={cn("z-popover outline-none", className)}
      {...props}
    />
  );
}

function PopoverContent({
  className,
  side,
  sideOffset = 6,
  align,
  ...props
}: PopoverPrimitive.Popup.Props &
  Pick<PopoverPrimitive.Positioner.Props, "side" | "sideOffset" | "align">) {
  return (
    <PopoverPortal>
      <PopoverBackdrop />
      <PopoverPositioner side={side} sideOffset={sideOffset} align={align}>
        <PopoverPrimitive.Popup
          data-slot="popover-content"
          className={cn(
            // Glass chrome: .glass-dropdown owns bg + rim + shadow +
            // radius + saturate/contrast (via -webkit-backdrop-filter);
            // `backdrop-blur-sm` adds the standard `backdrop-filter`
            // property so the blur also resolves in browsers that
            // don't accept the -webkit- prefix. Matches the .glass-bar
            // and .glass-panel blur level — one unified softness.
            "glass-dropdown backdrop-blur-sm outline-none",
            "data-open:animate-in data-open:fade-in-0 data-open:duration-100",
            "data-closed:animate-out data-closed:fade-out-0 data-closed:duration-100",
            className,
          )}
          {...props}
        />
      </PopoverPositioner>
    </PopoverPortal>
  );
}

export {
  Popover,
  PopoverTrigger,
  PopoverPortal,
  PopoverBackdrop,
  PopoverPositioner,
  PopoverContent,
};
