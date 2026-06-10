"use client";

import * as React from "react";
import { Select as SelectPrimitive } from "@base-ui/react/select";
import { cn } from "../utils";
import { ChevronDown, Check } from "lucide-react";

/**
 * Select — styled dropdown built on @base-ui/react/select.
 *
 * Replaces native <select> with memax design language: card background,
 * border tokens, spring entrance animation, check indicator.
 *
 * Usage:
 *   <Select value={value} onValueChange={setValue}>
 *     <SelectTrigger placeholder="Choose...">
 *       <SelectValue />
 *     </SelectTrigger>
 *     <SelectContent>
 *       <SelectItem value="a">Alpha</SelectItem>
 *       <SelectItem value="b">Beta</SelectItem>
 *     </SelectContent>
 *   </Select>
 */

// --- Root ---

interface SelectProps<V = string> {
  children: React.ReactNode;
  value?: V;
  defaultValue?: V;
  onValueChange?: (value: V) => void;
  /** Map of value→label so SelectValue can display labels even before the popup opens. */
  items?: Record<string, React.ReactNode>;
  name?: string;
  required?: boolean;
  disabled?: boolean;
}

function Select<V = string>({
  children,
  value,
  defaultValue,
  onValueChange,
  items,
  name,
  required,
  disabled,
}: SelectProps<V>) {
  return (
    <SelectPrimitive.Root
      data-slot="select"
      value={value}
      defaultValue={defaultValue}
      onValueChange={(val, _details) => {
        onValueChange?.(val as V);
      }}
      items={items}
      name={name}
      required={required}
      disabled={disabled}
    >
      {children}
    </SelectPrimitive.Root>
  );
}

// --- Trigger ---

interface SelectTriggerProps {
  children?: React.ReactNode;
  className?: string;
  placeholder?: string;
}

function SelectTrigger({
  children,
  className,
  placeholder,
  ...props
}: SelectTriggerProps) {
  return (
    <SelectPrimitive.Trigger
      data-slot="select-trigger"
      className={cn(
        "inline-flex w-full items-center justify-between gap-2",
        "rounded-surface border border-border/50 bg-card",
        "px-4 py-2.5 text-[14px] text-fg-1",
        "outline-none transition-colors cursor-pointer",
        "hover:border-fg-4 focus:border-fg-3",
        "disabled:opacity-40 disabled:cursor-not-allowed",
        className,
      )}
      {...props}
    >
      {children ?? (
        <SelectPrimitive.Value
          data-slot="select-value"
          placeholder={placeholder}
          className="truncate data-[placeholder]:text-fg-4"
        />
      )}
      <SelectPrimitive.Icon
        data-slot="select-icon"
        className="shrink-0 text-fg-3"
      >
        <ChevronDown className="h-4 w-4" />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

// --- Value ---

function SelectValue({
  className,
  placeholder,
  ...props
}: {
  className?: string;
  placeholder?: string;
}) {
  return (
    <SelectPrimitive.Value
      data-slot="select-value"
      placeholder={placeholder}
      className={cn("truncate data-[placeholder]:text-fg-4", className)}
      {...props}
    />
  );
}

// --- Content (portal + positioner + popup) ---

interface SelectContentProps {
  children: React.ReactNode;
  className?: string;
  side?: "top" | "bottom";
  sideOffset?: number;
  align?: "start" | "center" | "end";
}

function SelectContent({
  children,
  className,
  side = "bottom",
  sideOffset = 6,
  align = "start",
}: SelectContentProps) {
  return (
    <SelectPrimitive.Portal data-slot="select-portal">
      <SelectPrimitive.Positioner
        data-slot="select-positioner"
        side={side}
        sideOffset={sideOffset}
        align={align}
        className="z-popover outline-none"
      >
        <SelectPrimitive.Popup
          data-slot="select-popup"
          className={cn(
            "rounded-surface border border-border bg-card outline-none",
            "shadow-[0_8px_32px_oklch(0_0_0/0.08),0_2px_8px_oklch(0_0_0/0.04)]",
            "min-w-[var(--anchor-width)] max-h-72 overflow-y-auto p-1",
            "data-open:animate-in data-open:fade-in-0 data-open:duration-100",
            "data-closed:animate-out data-closed:fade-out-0 data-closed:duration-100",
            className,
          )}
        >
          {children}
        </SelectPrimitive.Popup>
      </SelectPrimitive.Positioner>
    </SelectPrimitive.Portal>
  );
}

// --- Item ---

interface SelectItemProps {
  children: React.ReactNode;
  value: string;
  className?: string;
  disabled?: boolean;
}

function SelectItem({ children, value, className, disabled }: SelectItemProps) {
  return (
    <SelectPrimitive.Item
      data-slot="select-item"
      value={value}
      disabled={disabled}
      className={cn(
        "flex items-center gap-2 rounded-lg px-3 py-2 text-[14px] text-fg-2 outline-none cursor-pointer",
        "transition-colors select-none",
        "data-highlighted:bg-surface-2 data-highlighted:text-fg-1",
        "data-disabled:opacity-40 data-disabled:cursor-not-allowed",
        className,
      )}
    >
      <SelectPrimitive.ItemIndicator
        data-slot="select-item-indicator"
        className="shrink-0 w-3.5 text-fg-1 data-[state=unchecked]:opacity-0"
      >
        <Check className="h-3.5 w-3.5" />
      </SelectPrimitive.ItemIndicator>
      <SelectPrimitive.ItemText data-slot="select-item-text">
        {children}
      </SelectPrimitive.ItemText>
    </SelectPrimitive.Item>
  );
}

export { Select, SelectTrigger, SelectValue, SelectContent, SelectItem };
