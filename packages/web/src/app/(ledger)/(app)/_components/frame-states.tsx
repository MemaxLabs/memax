"use client";

import { Button, Logo } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { StatusPage } from "../../_components/status-page";
import { HeaderSkeleton, RailSkeleton, SkeletonRows } from "./skeleton";
import styles from "./app-frame.module.css";

/**
 * The frame before the person's spaces arrive (the SDK source only;
 * the demo has them on first render): the shell's shape in hairlines.
 */
export function FrameSkeleton() {
  const { t } = useLocale();
  return (
    <div className={styles.frame}>
      <div className="mx-shell" aria-busy="true">
        <div className="mx-rail">
          <div className="mx-rail-top">
            <Logo variant="mark" size={20} decorative />
          </div>
          <RailSkeleton />
        </div>
        <main className="mx-sheet">
          <div className="mx-page">
            <p className="mx-sr" role="status">
              {t.ledger.app.frame.loading}
            </p>
            <HeaderSkeleton />
            <div className="mx-panel">
              <SkeletonRows />
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}

/** The spaces didn't load: what happened, what was kept, what to do. */
export function FrameFailed({ onRetry }: { onRetry?: () => void }) {
  const { t } = useLocale();
  const copy = t.ledger.app.frame.failed;
  return (
    <StatusPage
      role="alert"
      title={copy.title}
      description={copy.description}
      actions={
        onRetry ? (
          <Button variant="primary" icon="sync" onClick={onRetry}>
            {copy.retry}
          </Button>
        ) : null
      }
    />
  );
}
