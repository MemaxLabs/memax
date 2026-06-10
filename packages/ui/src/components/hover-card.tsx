"use client";

import * as React from "react";
import { PreviewCard as PreviewCardPrimitive } from "@base-ui/react/preview-card";
import { cn } from "../utils";

/**
 * HoverCard — non-modal floating info surface anchored to a trigger.
 *
 * For hover/focus-revealed informational content (e.g. "what does this
 * tinted chip mean?"). Unlike Popover, HoverCard intentionally does NOT
 * render a backdrop — there is no modal layer, no pointer-event capture
 * outside the trigger + popup, and no focus trap. Click-through to the
 * trigger's own onClick still works normally (e.g. chip navigates to the
 * target topic while the card is open; the card closes on navigate).
 *
 * Use for: explanatory hover surfaces, dream-action reasons, inline
 * topic previews, keyboard-focusable help text.
 * Do NOT use for: menus (use Popover or Menu), dialogs (use Dialog),
 * anything that needs outside-click dismissal with modal semantics.
 *
 * Delays default to 150ms open / 200ms close so the card doesn't chase
 * a rapidly moving pointer — tuned by design review for memax's row
 * breadcrumb dream-action popover.
 *
 * Built on base-ui's preview-card primitive. The backdrop slot is
 * deliberately never rendered; if you find yourself wanting one, you
 * want Popover, not HoverCard.
 */

function HoverCard({ ...props }: PreviewCardPrimitive.Root.Props) {
  return <PreviewCardPrimitive.Root data-slot="hover-card" {...props} />;
}

function HoverCardTrigger({
  delay = 150,
  closeDelay = 200,
  ...props
}: PreviewCardPrimitive.Trigger.Props) {
  return (
    <PreviewCardPrimitive.Trigger
      data-slot="hover-card-trigger"
      delay={delay}
      closeDelay={closeDelay}
      {...props}
    />
  );
}

function HoverCardPortal({ ...props }: PreviewCardPrimitive.Portal.Props) {
  return (
    <PreviewCardPrimitive.Portal data-slot="hover-card-portal" {...props} />
  );
}

function HoverCardPositioner({
  className,
  ...props
}: PreviewCardPrimitive.Positioner.Props) {
  return (
    <PreviewCardPrimitive.Positioner
      data-slot="hover-card-positioner"
      className={cn("z-popover outline-none", className)}
      {...props}
    />
  );
}

function HoverCardContent({
  className,
  side,
  sideOffset = 6,
  align,
  onPointerDown,
  onPointerUp,
  onMouseDown,
  onMouseUp,
  onClick,
  ...props
}: PreviewCardPrimitive.Popup.Props &
  Pick<
    PreviewCardPrimitive.Positioner.Props,
    "side" | "sideOffset" | "align"
  >) {
  return (
    <HoverCardPortal>
      <HoverCardPositioner side={side} sideOffset={sideOffset} align={align}>
        <PreviewCardPrimitive.Popup
          data-slot="hover-card-content"
          className={cn(
            // Shared glass material — same .glass-dropdown recipe as
            // Popover + InfoPopover. Paired with `backdrop-blur-sm` so
            // the standard backdrop-filter property emits (Lightning
            // CSS strips it from raw custom-CSS in some cases).
            "glass-dropdown backdrop-blur-sm p-3 outline-none select-text",
            "data-open:animate-in data-open:fade-in-0 data-open:duration-100",
            "data-closed:animate-out data-closed:fade-out-0 data-closed:duration-100",
            className,
          )}
          // Stop every pointer / mouse / click event from bubbling up
          // the React tree. The popover is portalled out of the DOM
          // tree but React's synthetic events still propagate along
          // the React owner chain — so pressing or clicking inside a
          // hover card opened from inside a memory card would
          // otherwise trigger BOTH dnd-kit's drag listener AND the
          // memory card's onClick (which navigates to the detail
          // page). Stopping at the popover root keeps text selection
          // working, the card calm, and the card's onClick suppressed
          // when the user is interacting with the popover content.
          //
          // We stop pointerdown/pointerup AND mousedown/mouseup AND
          // click because the card's click handler fires on the
          // synthesized "click" after a pointerdown+pointerup pair —
          // stopping just pointerdown lets the click sequence still
          // synthesize and bubble.
          onPointerDown={(e) => {
            e.stopPropagation();
            onPointerDown?.(e);
          }}
          onPointerUp={(e) => {
            e.stopPropagation();
            onPointerUp?.(e);
          }}
          onMouseDown={(e) => {
            e.stopPropagation();
            onMouseDown?.(e);
          }}
          onMouseUp={(e) => {
            e.stopPropagation();
            onMouseUp?.(e);
          }}
          onClick={(e) => {
            e.stopPropagation();
            onClick?.(e);
          }}
          {...props}
        />
      </HoverCardPositioner>
    </HoverCardPortal>
  );
}

export {
  HoverCard,
  HoverCardTrigger,
  HoverCardPortal,
  HoverCardPositioner,
  HoverCardContent,
};
