import type {
  AnchorHTMLAttributes,
  ButtonHTMLAttributes,
  MouseEvent,
  ReactNode,
  Ref,
} from "react";
import { mergeProps } from "@base-ui/react/merge-props";
import { useRender } from "@base-ui/react/use-render";
import { Icon, type IconName } from "../brand/icon";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { toAriaKeyshortcuts } from "../lib/keyshortcuts";
import { Kbd } from "./kbd";

export type ButtonVariant =
  | "primary"
  | "keep"
  | "secondary"
  | "quiet"
  | "danger";
export type ButtonSize = "sm" | "md" | "lg";

export interface ButtonProps
  extends
    Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children">,
    Pick<
      AnchorHTMLAttributes<HTMLAnchorElement>,
      "href" | "target" | "rel" | "download"
    > {
  /**
   * `primary` (ink) moves the page on, once per view. `keep` (seal green) is only for
   * Keep, Keep all, Keep as memory and Answer. `danger` is for Forget and Overwrite.
   */
  variant?: ButtonVariant;
  /** `sm` in rows and cards, `md` by default, `lg` on onboarding and the landing page. */
  size?: ButtonSize;
  icon?: IconName;
  /** The keycap shown after the label ("K", "⌘↵"). Also announced as aria-keyshortcuts. */
  kbd?: string;
  /** A verb in sentence case. Icon-only buttons need `aria-label`. */
  children?: ReactNode;
  /**
   * Why the button is unavailable. With `disabled`, the button stays focusable
   * (aria-disabled) and shows this as its tooltip, so the state doesn't rely on
   * colour alone (design review §2).
   */
  disabledReason?: string;
  /** A command is in flight: the button stays focusable and ignores presses. No spinner. */
  pending?: boolean;
  /**
   * Renders the button as another element or component, e.g.
   * `render={<Menu.Trigger />}`. With `href`, it renders the provider's link.
   */
  render?: useRender.RenderProp;
  ref?: Ref<HTMLElement>;
}

/** The one button. */
export function Button({
  variant = "secondary",
  size = "md",
  icon,
  kbd,
  children,
  className,
  disabled,
  disabledReason,
  pending = false,
  render,
  href,
  type = "button",
  title,
  onClick,
  ref,
  ...rest
}: ButtonProps) {
  const { Link } = useLedger();
  const isLink = href !== undefined;
  // Native `disabled` only when nothing needs the control to stay focusable:
  // a reason to show, a command in flight, a link, or a custom element.
  const nativeDisabled =
    Boolean(disabled) && !pending && !disabledReason && !isLink && !render;
  const ariaDisabled = pending || (Boolean(disabled) && !nativeDisabled);
  const blocked = pending || Boolean(disabled);
  const content = (
    <>
      {icon ? <Icon name={icon} size={size === "lg" ? 18 : 16} /> : null}
      {children != null ? (
        <span className="mx-btn-label">{children}</span>
      ) : null}
      {kbd ? (
        <Kbd className="mx-btn-kbd" aria-hidden="true">
          {kbd}
        </Kbd>
      ) : null}
    </>
  );
  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    if (blocked) {
      event.preventDefault();
      return;
    }
    onClick?.(event);
  };
  return useRender({
    defaultTagName: "button",
    render: render ?? (isLink ? <Link href={href} /> : undefined),
    ref,
    props: mergeProps<"button">(
      {
        className: cx(
          "mx-btn",
          `mx-btn--${variant}`,
          `mx-btn--${size}`,
          children == null && "mx-btn--icon",
          className,
        ),
        type: isLink || render ? undefined : type,
        disabled: nativeDisabled || undefined,
        "aria-disabled": ariaDisabled || undefined,
        "aria-busy": pending || undefined,
        "aria-keyshortcuts": kbd ? toAriaKeyshortcuts(kbd) : undefined,
        title: disabled && disabledReason ? disabledReason : title,
        children: content,
      },
      rest as ButtonHTMLAttributes<HTMLButtonElement>,
      { onClick: handleClick },
    ),
  });
}
