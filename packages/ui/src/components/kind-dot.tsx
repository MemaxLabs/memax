import { cn } from "../utils";

const KIND_COLORS: Record<string, string> = {
  episodic: "#06b6d4",
  semantic: "#8b5cf6",
  procedural: "#10b981",
  rationale: "#f59e0b",
};

const SIZES = {
  xs: "h-1.5 w-1.5",
  sm: "h-2 w-2",
  md: "h-3 w-3",
} as const;

export function KindDot({
  kind,
  size = "sm",
  className,
}: {
  kind: string;
  size?: keyof typeof SIZES;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-block rounded-full shrink-0",
        SIZES[size],
        className,
      )}
      style={{ backgroundColor: KIND_COLORS[kind] ?? "#94a3b8" }}
      aria-hidden="true"
    />
  );
}
