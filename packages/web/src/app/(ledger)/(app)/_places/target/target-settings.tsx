"use client";

import { useState } from "react";
import { Field, Segmented } from "@memaxlabs/ledger";
import { formatKb, parseKb } from "@/lib/v2/brief-copy";
import {
  SIZE_BUDGET_MAX,
  SIZE_BUDGET_MIN,
  type Delivery,
  type IncludeMode,
  type StaleMode,
  type TargetView,
} from "@/lib/v2/data/targets";
import type { RecordsView } from "../records-view";
import { useTargetSettings } from "./use-target-settings";
import styles from "./target.module.css";

/** TargetPreview.png's "How this file is written": include, stale facts, size budget, delivery. */
export function TargetSettings({
  view,
  target,
}: {
  view: RecordsView;
  target: TargetView;
}) {
  const { l, space, locale } = view;
  const s = l.brief.target.settings;
  const { settings, pending, change } = useTargetSettings(space, target);
  const readOnly = space.role === "viewer";
  const disabled = readOnly || pending;
  const budgetText = formatKb(settings.sizeBudget, locale, "budget");
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState(false);

  const commitBudget = async () => {
    if (draft === null) return;
    const bytes = parseKb(draft);
    if (bytes === null || bytes < SIZE_BUDGET_MIN || bytes > SIZE_BUDGET_MAX) {
      setError(true);
      return;
    }
    setError(false);
    if (bytes === target.settings.sizeBudget) {
      setDraft(null);
      return;
    }
    await change({ settings: { sizeBudget: bytes } });
    setDraft(null);
  };

  return (
    <section className="mx-panel" aria-labelledby="target-settings">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="target-settings">
          {s.title}
        </h2>
      </header>
      <div className={styles.setting}>
        <span className={styles.settingLabel}>{s.include}</span>
        <Segmented<IncludeMode>
          size="sm"
          label={s.include}
          value={settings.include}
          disabled={disabled}
          onChange={(include) => void change({ settings: { include } })}
          options={[
            { value: "kept_only", label: s.includeKept },
            { value: "kept_and_open", label: s.includeOpen },
          ]}
        />
      </div>
      <div className={styles.setting}>
        <span className={styles.settingLabel}>{s.stale}</span>
        <Segmented<StaleMode>
          size="sm"
          label={s.stale}
          value={settings.stale}
          disabled={disabled}
          onChange={(stale) => void change({ settings: { stale } })}
          options={[
            { value: "mark", label: s.staleMark },
            { value: "omit", label: s.staleOmit },
          ]}
        />
      </div>
      <div className={styles.setting}>
        <Field
          label={s.budget}
          mono
          value={draft ?? budgetText}
          disabled={disabled}
          hint={s.budgetHint}
          error={error ? s.budgetError : undefined}
          onChange={(event) => {
            setDraft(event.currentTarget.value);
            setError(false);
          }}
          onBlur={() => void commitBudget()}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              void commitBudget();
            } else if (event.key === "Escape" && draft !== null) {
              event.preventDefault();
              setDraft(null);
              setError(false);
            }
          }}
        />
      </div>
      <div className={styles.setting}>
        <span className={styles.settingLabel}>{s.delivery}</span>
        {target.delivery === "copy" ? (
          <p className={styles.settingNote}>{s.copyNote}</p>
        ) : (
          <Segmented<Delivery>
            size="sm"
            label={s.delivery}
            value={settings.delivery}
            disabled={disabled}
            onChange={(delivery) => void change({ delivery })}
            options={[
              // Pull requests arrive with the GitHub App (Phase 4).
              {
                value: "pr",
                label: s.deliveryPr,
                disabled: true,
                disabledReason: s.prLater,
              },
              { value: "local", label: s.deliveryLocal },
            ]}
          />
        )}
      </div>
    </section>
  );
}
