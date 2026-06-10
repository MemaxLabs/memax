import { cn } from "../utils";

/**
 * Surface — the primary container used for collection views and detail pages.
 *
 * Renders a card-background container with the standard Memax border + shadow.
 * Three variants:
 *   - `default` — bar-border + bar-shadow (memory grid, detail pages)
 *   - `subtle`  — lighter border, no prominent shadow (notes list)
 *   - `flat`    — border only, no shadow
 */
export function Surface({
  variant = "default",
  rounded = "xl",
  className,
  children,
  style,
  ...props
}: {
  variant?: "default" | "subtle" | "flat" | "borderless" | "clean";
  /**
   * `xl` and `2xl` both resolve to the unified 14px chrome curve. The prop
   * is kept for API stability but is effectively a no-op between these two
   * values after the repo-wide curve unification — every container in the
   * app shares one radius. `lg` retains the smaller 8px curve for tight
   * inline containers.
   */
  rounded?: "xl" | "2xl" | "lg";
} & React.ComponentProps<"div">) {
  const roundedClass =
    variant === "borderless"
      ? ""
      : rounded === "lg"
        ? "rounded-lg"
        : "rounded-surface";

  const baseStyle =
    variant === "borderless"
      ? {}
      : variant === "clean"
        ? { background: "var(--card)" as string }
        : {
            background: "var(--card)" as string,
            border:
              variant === "subtle" || variant === "flat"
                ? "1px solid var(--border)"
                : "1px solid var(--bar-border)",
            boxShadow: variant === "default" ? "var(--bar-shadow)" : undefined,
          };

  return (
    <div
      className={cn(roundedClass, className)}
      style={{ ...baseStyle, ...style }}
      {...props}
    >
      {children}
    </div>
  );
}
