/**
 * StateIndicator — unified dot/star rendering with state animations.
 *
 * Visual vocab (kitchen 02):
 *   ● dot  = content exists
 *   ✦ star = memax intelligence is acting
 *
 * Animation classes:
 *   state-slow-breathe = processing / loading (2.5s pulse)
 *   state-fast-pulse   = active / running (0.8s pulse)
 *
 * Usage:
 *   <StateIndicator state="processing" />           // ✦ slow breathe, signature color
 *   <StateIndicator state="active" />                // ✦ fast pulse, signature color
 *   <StateIndicator state="idle" />                  // ✦ static, muted
 *   <StateIndicator state="processing" color={c} />  // ✦ slow breathe, custom color
 */

interface StateIndicatorProps {
  state: "processing" | "active" | "idle";
  color?: string;
  className?: string;
  size?: "sm" | "md";
}

const ANIMATION: Record<StateIndicatorProps["state"], string> = {
  processing: "state-slow-breathe",
  active: "state-fast-pulse",
  idle: "",
};

export function StateIndicator({
  state,
  color = "var(--signature)",
  className = "",
  size = "sm",
}: StateIndicatorProps) {
  const fontSize = size === "md" ? "14px" : "11px";

  return (
    <span
      className={`leading-none shrink-0 ${ANIMATION[state]} ${className}`}
      style={{
        color,
        fontSize,
        opacity: state === "idle" ? 0.4 : 1,
      }}
    >
      ✦
    </span>
  );
}
