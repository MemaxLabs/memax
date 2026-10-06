import type { ReactElement, ReactNode, RefObject } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { useLedger } from "../i18n/provider";

export interface CommandDialogProps {
  /** Controlled. */
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** A `CommandBar`. */
  children: ReactNode;
  /** The dialog's accessible name. Defaults to the locale's "Ask or remember". */
  label?: string;
  /** What gets focus on open; the first tabbable element (the field) by default. */
  initialFocus?: RefObject<HTMLElement | null>;
  /**
   * Where the portal mounts. Defaults to the body; pass the themed root when
   * `data-theme` is set below <html>, so the dialog keeps the theme.
   */
  container?: HTMLElement | null | RefObject<HTMLElement | null>;
  /** An element that opens it, e.g. the rail's Ask button. */
  trigger?: ReactElement;
}

/**
 * The ⌘K layer: the one floating thing on screen, centred 88px from the top
 * over the scrim. Base UI supplies the behaviour (focus trap and return,
 * Escape, outside press, scroll lock); the look is Ledger's.
 */
export function CommandDialog({
  open,
  defaultOpen,
  onOpenChange,
  children,
  label,
  initialFocus,
  container,
  trigger,
}: CommandDialogProps) {
  const { strings } = useLedger();
  return (
    <Dialog.Root
      open={open}
      defaultOpen={defaultOpen}
      onOpenChange={(next) => onOpenChange?.(next)}
    >
      {trigger ? <Dialog.Trigger render={trigger} /> : null}
      <Dialog.Portal container={container}>
        <Dialog.Backdrop className="mx-cmd-scrim" />
        <Dialog.Popup
          className="mx-cmd-layer"
          aria-label={label ?? strings.command.label}
          initialFocus={initialFocus}
        >
          {children}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
