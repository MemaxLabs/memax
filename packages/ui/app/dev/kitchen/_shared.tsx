"use client";

// ─── Constants ───
export const SIGNATURE = "var(--signature)";
export const SIGNATURE_MUTED = "var(--signature-muted)";

export const DOT_SIZE = {
  xs: "h-1 w-1",
  sm: "h-1.5 w-1.5",
  md: "h-2 w-2",
  lg: "h-3 w-3",
} as const;

export const STAR_SIZE = {
  xs: "text-[8px]",
  sm: "text-[10px]",
  md: "text-[13px]",
  lg: "text-[20px]",
} as const;

export const LOGO_PATH =
  "M693.827 1493.738s-16.84 2.831-16.516 20.493c1.067 16.532 15.335 19.167 15.335 19.167s1.425 21.119 27.246 21.269c0 0-7.763-5.005-10.598-14.042-2.699-1.779-4.919-4.636-5.618-9.79l.04-6.555a1.047 1.047 0 0 0-1.045-1.053l-5.271-.007s-8.544-.066-8.948-9.434c.065-8.179 8.036-9.562 8.036-9.562l6.533.07a.93.93 0 0 0 .939-.94l-.075-6.782s2.8-12.189 14.791-11.958c14.484.209 15.19 15.232 15.19 15.232l.134 14.264s-.862 4.118 5.047 4.261c6.154.043 5.34-4.22 5.34-4.22l-.005-17.452S741.535 1474.114 718 1474c-20.435.336-24.173 19.738-24.173 19.738";

// ─── Layout Components ───

export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  // Auto-generate anchor from title: "10. Memory Cards" → "10-memory-cards"
  const id = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
  return (
    <section id={id} className="mb-8 scroll-mt-12">
      <h2 className="text-[18px] font-semibold text-foreground mb-1">
        {title}
      </h2>
      {description && (
        <p className="text-[14px] text-fg-2 mb-4">{description}</p>
      )}
      <div className="space-y-4">{children}</div>
    </section>
  );
}

export function DemoCard({
  label,
  children,
  className = "",
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`border border-border/60 rounded-xl p-4 ${className}`}
      style={{ background: "var(--card)" }}
    >
      <div className="text-[12px] font-medium text-fg-3 uppercase tracking-wider mb-3">
        {label}
      </div>
      {children}
    </div>
  );
}

export function Swatch({
  color,
  label,
  token,
}: {
  color: string;
  label: string;
  token?: string;
}) {
  return (
    <div className="flex items-center gap-3">
      <div
        className="w-8 h-8 rounded-lg border border-border shrink-0"
        style={{ background: color }}
      />
      <div>
        <p className="text-[13px] text-fg-1 font-medium">{label}</p>
        {token && <p className="text-[10px] text-fg-3 font-mono">{token}</p>}
      </div>
    </div>
  );
}

// ─── Indicator Demos ───

export function DotDemo({
  color,
  behavior,
  label,
  size = "sm",
}: {
  color: string;
  behavior: "static" | "fast-pulse" | "slow-breathe" | "fade" | "flash";
  label: string;
  size?: "xs" | "sm" | "md" | "lg";
}) {
  const behaviorClass =
    behavior === "fast-pulse"
      ? "state-fast-pulse"
      : behavior === "slow-breathe"
        ? "state-slow-breathe"
        : behavior === "fade"
          ? "state-fade"
          : behavior === "flash"
            ? "state-flash"
            : "";

  return (
    <div className="flex items-center gap-3">
      <div
        className={`rounded-full shrink-0 ${DOT_SIZE[size]} ${behaviorClass}`}
        style={{ backgroundColor: color }}
      />
      <span className="text-[14px] text-fg-2">{label}</span>
    </div>
  );
}

export function StarDemo({
  behavior,
  label,
  size = "sm",
}: {
  behavior: "static" | "fast-pulse" | "slow-breathe";
  label: string;
  size?: "xs" | "sm" | "md" | "lg";
}) {
  const behaviorClass =
    behavior === "fast-pulse"
      ? "state-fast-pulse"
      : behavior === "slow-breathe"
        ? "state-slow-breathe"
        : "";

  return (
    <div className="flex items-center gap-3">
      <span
        className={`leading-none ${STAR_SIZE[size]} ${behaviorClass}`}
        style={{ color: SIGNATURE }}
      >
        ✦
      </span>
      <span className="text-[14px] text-fg-2">{label}</span>
    </div>
  );
}

// ─── Bar State Mock ───

export function BarStateMock({
  label,
  border,
  shadow,
  animation,
  lift,
}: {
  label: string;
  border: string;
  shadow: string;
  animation?: string;
  lift?: boolean;
}) {
  const style: React.CSSProperties = { border };
  if (!animation) style.boxShadow = shadow;
  return (
    <div className="flex flex-col items-center gap-2">
      <div
        className="w-full max-w-sm rounded-xl p-4 flex items-center justify-center"
        style={{ background: "var(--background)" }}
      >
        <div
          className={`w-full h-14 flex items-center px-5 ${animation ?? ""}`}
          style={{
            ...style,
            background: "var(--bar-bg, var(--card))",
            borderRadius: "16px",
            transform: lift ? "translateY(-3px)" : undefined,
            transition:
              "box-shadow 0.15s ease, border 0.15s ease, transform 0.15s ease",
          }}
        >
          <span className="text-[14px] text-fg-2 truncate">{label}</span>
        </div>
      </div>
      <p className="text-[10px] text-fg-3 text-center">{label}</p>
    </div>
  );
}
