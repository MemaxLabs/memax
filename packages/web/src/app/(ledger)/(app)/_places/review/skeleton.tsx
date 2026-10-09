import styles from "./review.module.css";

// Review's loading state (States board): hairline bars at the queue's
// real row heights (stacked rows: one or two statement lines, then the
// receipt) and a card of the card's height. No spinner, no shimmer.
const ROWS: string[][] = [["86%", "48%"], ["72%"], ["90%", "40%"], ["64%"]];

function Bar({ width }: { width: string }) {
  return <span className={styles.bar} style={{ width }} />;
}

export function QueueSkeleton({ label }: { label: string }) {
  return (
    <div aria-busy="true" aria-label={label} role="status">
      {ROWS.map((lines, i) => (
        <div key={i} className={styles.skRow} aria-hidden="true">
          <span />
          <span>
            <span className={styles.skLines}>
              {lines.map((width, j) => (
                <Bar key={j} width={width} />
              ))}
            </span>
            <span className={styles.skRail}>
              <Bar width="22px" />
              <Bar width="140px" />
            </span>
          </span>
        </div>
      ))}
    </div>
  );
}

export function CardSkeleton() {
  return (
    <div className={styles.skCard} aria-hidden="true">
      <Bar width="120px" />
      <Bar width="92%" />
      <Bar width="64%" />
      <Bar width="80%" />
    </div>
  );
}
