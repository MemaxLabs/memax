"use client";

import {
  Button,
  Receipt,
  Segmented,
  StateMark,
  useLedger,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { formatAgo } from "@/lib/v2/copy";
import type {
  RememberCheck,
  Section,
  SpaceSummary,
  Viewer,
} from "@/lib/v2/data/types";
import { useSource } from "../_lib/data";
import type { useRemember } from "../_lib/use-remember";
import styles from "./command-center.module.css";

const SECTIONS: Section[] = ["decisions", "conventions", "preferences"];
/** Segmented holds two to four options. */
const MAX_SPACES = 4;

/**
 * Remember's draft (Remember.png): the statement in the serif, where it
 * goes, the near-duplicate offer, and the receipt it will carry.
 */
export function RememberPanel({
  draft,
  homeSlug,
  spaces,
  viewer,
  keepKey,
  commandKey,
  onKeep,
  onKeepDuplicate,
}: {
  draft: ReturnType<typeof useRemember>;
  /** The space ⌘K was opened in. */
  homeSlug: string;
  spaces: SpaceSummary[];
  viewer: Viewer | null;
  keepKey: string;
  commandKey: string;
  onKeep: () => void;
  onKeepDuplicate: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.app.remember;
  const { agents } = useLedger();
  const source = useSource();

  if (!draft.text) {
    return <p className={styles.hint}>{copy.write}</p>;
  }

  // What the draft repeats: an agent's proposal (as drawn), a person's
  // proposal, or a memory that's already kept.
  const duplicateText = (d: NonNullable<RememberCheck["duplicate"]>) => {
    const age = formatAgo(t.ledger.app, d.writtenAt, source.now());
    if (d.lifecycle === "kept") {
      return interpolate(copy.duplicateKept, { age, ref: d.ref });
    }
    if (!d.agent) {
      return interpolate(copy.duplicateByPerson, { age, ref: d.ref });
    }
    return interpolate(copy.duplicate, {
      agent: agents[d.agent]?.name ?? d.agent,
      age,
      ref: d.ref,
    });
  };

  // The space ⌘K opened in comes first, as drawn; then the others.
  const home = spaces.find((s) => s.slug === homeSlug) ?? draft.space;
  const targets = [home, ...spaces.filter((s) => s.slug !== home.slug)].slice(
    0,
    MAX_SPACES,
  );
  const viewerRole = draft.space.role === "viewer";

  return (
    <>
      <p className="mx-section-label">
        {viewerRole ? copy.labelViewer : copy.label}
      </p>
      <p className={styles.statement}>{draft.text}</p>
      <div className={styles.fields}>
        <span className="mx-meta">{copy.space}</span>
        <Segmented
          size="sm"
          label={copy.space}
          value={draft.space.slug}
          onChange={(slug) => {
            const next = spaces.find((s) => s.slug === slug);
            if (next) draft.setSpace(next);
          }}
          options={targets.map((s) => ({ value: s.slug, label: s.name }))}
        />
        <span className="mx-meta">{copy.section}</span>
        <Segmented<Section>
          size="sm"
          label={copy.sectionLabel}
          value={draft.section}
          onChange={draft.setSection}
          options={SECTIONS.map((value) => ({
            value,
            label: copy.sections[value],
          }))}
        />
        {draft.condition ? (
          <>
            <span className="mx-meta">{copy.staysTrue}</span>
            <span className={styles.condition}>
              <code className="mx-code">{draft.condition.subject}</code>{" "}
              {draft.condition.rest}
            </span>
          </>
        ) : null}
      </div>
      {draft.duplicate ? (
        <div className={styles.duplicate} role="status">
          <span className={styles.duplicateMark}>
            <StateMark state="merged" label={false} />
          </span>
          <span className={styles.duplicateText}>
            {duplicateText(draft.duplicate)}
          </span>
          {/* A proposal can be kept instead; a kept memory already is. */}
          {draft.duplicate.lifecycle === "proposed" ? (
            <Button
              variant="secondary"
              size="sm"
              pending={draft.pending}
              onClick={onKeepDuplicate}
            >
              {interpolate(copy.keepRef, { ref: draft.duplicate.ref })}
            </Button>
          ) : null}
        </div>
      ) : null}
      <div className={styles.keepRow}>
        {viewer ? (
          <Receipt
            person={viewer.initials}
            name={viewer.name}
            action={t.ledger.app.receipt.kept}
            time={t.ledger.app.receipt.now}
            source={interpolate(t.ledger.app.receipt.via, { key: commandKey })}
          />
        ) : null}
        <span className={styles.spacer} />
        <Button
          variant="keep"
          size="sm"
          kbd={keepKey}
          pending={draft.pending}
          onClick={onKeep}
        >
          {copy.keep}
        </Button>
      </div>
    </>
  );
}
