import styles from "./skeleton.module.css";

// Bar lengths from the States board's loading panel: statements of one
// and two lines, receipts of two.
const ROWS: { text: string[]; rail: [string, string] }[] = [
  { text: ["88%", "52%"], rail: ["70%", "46%"] },
  { text: ["74%"], rail: ["64%", "40%"] },
  { text: ["92%", "38%"], rail: ["72%", "50%"] },
  { text: ["66%"], rail: ["60%", "44%"] },
];

function Bar({ width, className }: { width?: string; className?: string }) {
  return (
    <span
      className={className ? `${styles.bar} ${className}` : styles.bar}
      style={width ? { width } : undefined}
    />
  );
}

/** Memory rows at their real height, for a list that's loading. */
export function SkeletonRows({ rows = ROWS.length }: { rows?: number }) {
  return (
    <>
      {ROWS.slice(0, rows).map((row, i) => (
        <div key={i} className={styles.row} aria-hidden="true">
          <span />
          <span className={styles.lines}>
            {row.text.map((width, j) => (
              <Bar key={j} width={width} />
            ))}
          </span>
          <span className={styles.rail}>
            <Bar width={row.rail[0]} />
            <Bar width={row.rail[1]} />
          </span>
        </div>
      ))}
    </>
  );
}

/**
 * A place that's loading: its panel with the title and "Loading", over
 * hairline rows (States board). `label` names it for assistive technology.
 */
export function PlaceSkeleton({
  title,
  status,
  label,
}: {
  title: string;
  status: string;
  label: string;
}) {
  return (
    <section
      className={`mx-panel ${styles.panel}`}
      aria-busy="true"
      aria-label={label}
    >
      <header className="mx-panel-head">
        <h2 className="mx-panel-title">{title}</h2>
        <span className="mx-meta">{status}</span>
      </header>
      <SkeletonRows />
    </section>
  );
}

/** The page header while it loads: eyebrow, title and lede as bars. */
export function HeaderSkeleton() {
  return (
    <div className={styles.header} aria-hidden="true">
      <Bar width="160px" />
      <Bar className={styles.titleBar} />
      <Bar width="420px" />
    </div>
  );
}

/** The rail's places as bars, before the spaces arrive. */
export function RailSkeleton() {
  return (
    <div className={styles.railBars} aria-hidden="true">
      {["70%", "54%", "62%", "48%", "66%", "58%"].map((width, i) => (
        <Bar key={i} width={width} />
      ))}
    </div>
  );
}
