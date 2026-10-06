"use client";

import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { Check } from "lucide-react";
import { cn } from "@memaxlabs/ui";

/**
 * Consent checkbox with a spring-scaled check mark. Mirrors the kitchen
 * §39 PermissionRow / HubRow animation — FAST duration, EASE curve,
 * reduced-motion safe.
 */
export function ConsentCheck(
  props: React.InputHTMLAttributes<HTMLInputElement>,
) {
  const { checked, className, disabled, ...rest } = props;
  const reduced = useReducedMotion();
  return (
    <span
      className={cn(
        "relative mt-0.5 flex size-4.5 shrink-0 items-center justify-center",
        className,
      )}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        className="peer absolute inset-0 size-full cursor-pointer appearance-none disabled:cursor-default"
        {...rest}
      />
      <motion.span
        className={cn(
          "flex size-4.5 items-center justify-center rounded-md",
          checked
            ? "bg-foreground text-background"
            : "ring-1 ring-foreground/25 peer-hover:ring-foreground/40",
          "peer-focus-visible:ring-2 peer-focus-visible:ring-foreground/50 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-background",
        )}
        animate={{ scale: checked ? 1 : 0.94 }}
        transition={{ duration: reduced ? 0 : 0.15, ease: [0.2, 0.8, 0.2, 1] }}
      >
        <AnimatePresence>
          {checked ? (
            <motion.span
              key="check"
              initial={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.5 }}
              animate={reduced ? { opacity: 1 } : { opacity: 1, scale: 1 }}
              exit={reduced ? { opacity: 0 } : { opacity: 0, scale: 0.5 }}
              transition={{
                duration: reduced ? 0 : 0.15,
                ease: [0.2, 0.8, 0.2, 1],
              }}
              className="flex"
            >
              <Check className="size-3 stroke-3" aria-hidden="true" />
            </motion.span>
          ) : null}
        </AnimatePresence>
      </motion.span>
    </span>
  );
}
