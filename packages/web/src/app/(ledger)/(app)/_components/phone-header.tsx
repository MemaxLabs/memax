"use client";

import type { ReactNode } from "react";
import Link from "next/link";
import { Icon } from "@memaxlabs/ledger";
import styles from "./phone-header.module.css";

/**
 * A page's own header on a phone (MobileReview.png, MobileGate.png,
 * MobileMemory.png), in place of the space's top bar (the frame leaves
 * it out on these pages, _lib/phone.ts ownsPhoneHeader): a 44px back
 * link, the title in the middle and an end slot, with anything below
 * (Review's progress). Above 640px it isn't shown; the page's own
 * heading is.
 */
export function PhoneHeader({
  back,
  title,
  heading = true,
  end,
  children,
}: {
  back: { href: string; label: string };
  title: ReactNode;
  /** The title is the page's h1 on a phone; a memory's page has its statement for that. */
  heading?: boolean;
  end?: ReactNode;
  children?: ReactNode;
}) {
  const Title = heading ? "h1" : "span";
  return (
    <header className={styles.header}>
      <div className={styles.row}>
        <Link href={back.href} className={styles.back} aria-label={back.label}>
          <Icon name="chevron-right" size={20} className={styles.backIcon} />
        </Link>
        <Title className={heading ? styles.title : styles.label}>{title}</Title>
        <span className={styles.end}>{end}</span>
      </div>
      {children}
    </header>
  );
}

/**
 * Where you are in a deck of cards (MobileReview.png): one segment a card
 * while they fit, else one bar that fills. Drawn for the eye; the count
 * beside the title says it in words.
 */
export function PhoneProgress({ n, total }: { n: number; total: number }) {
  if (total <= 0) return null;
  if (total <= 12) {
    return (
      <div className={styles.segments} aria-hidden="true">
        {Array.from({ length: total }, (_, i) => (
          <span key={i} className={i < n ? styles.segmentOn : styles.segment} />
        ))}
      </div>
    );
  }
  return (
    <div className={styles.bar} aria-hidden="true">
      <span style={{ width: `${(Math.min(n, total) / total) * 100}%` }} />
    </div>
  );
}
