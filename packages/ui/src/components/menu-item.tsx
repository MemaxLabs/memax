"use client";

import * as React from "react";
import { cn } from "../utils";

/**
 * MenuItem — the single row primitive for every popover/dropdown list.
 *
 * Scope: visual contract only. Padding, radius, typography, hover, and
 * focus-visible are locked so every menu in the app reads as the same
 * component. Semantics (role, tabIndex, roving focus, keyboard nav)
 * belong to the parent container — the primitive stays a real `<button>`
 * so Enter/Space/`disabled` work natively without extra wiring.
 *
 * Variants:
 *   - default: neutral foreground.
 *   - destructive: danger-tinted foreground + hover (Forget, Delete…).
 *
 * Slots:
 *   - icon: optional leading node (typically a Lucide icon at h-4 w-4).
 *   - keybind: optional trailing node for keyboard hints (⌘K etc.).
 */
export type MenuItemVariant = "default" | "destructive";

export interface MenuItemProps extends Omit<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  "type"
> {
  icon?: React.ReactNode;
  keybind?: React.ReactNode;
  variant?: MenuItemVariant;
}

const base =
  "flex w-full items-center gap-2.5 cursor-pointer rounded-lg px-2.5 py-1.5 " +
  "text-[13px] text-left transition-colors outline-none " +
  "disabled:cursor-not-allowed disabled:opacity-50";

const variantClasses: Record<MenuItemVariant, string> = {
  default: "text-fg-2 hover:bg-surface-1 focus-visible:bg-surface-1",
  destructive:
    "text-destructive hover:bg-destructive/10 focus-visible:bg-destructive/10",
};

export const MenuItem = React.forwardRef<HTMLButtonElement, MenuItemProps>(
  function MenuItem(
    { className, icon, keybind, variant = "default", children, ...props },
    ref,
  ) {
    return (
      <button
        ref={ref}
        type="button"
        className={cn(base, variantClasses[variant], className)}
        {...props}
      >
        {icon ? (
          <span className="flex h-4 w-4 shrink-0 items-center justify-center">
            {icon}
          </span>
        ) : null}
        <span className="min-w-0 flex-1 truncate">{children}</span>
        {keybind ? (
          <span className="ml-2 shrink-0 text-fg-4 text-[12px] tabular-nums">
            {keybind}
          </span>
        ) : null}
      </button>
    );
  },
);
