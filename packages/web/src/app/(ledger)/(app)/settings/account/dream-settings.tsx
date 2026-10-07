"use client";

import { useId, useMemo, useState } from "react";
import { Button, Field, Segmented } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { useToast } from "../../_components/toasts";
import { useDreamSettings, useUpdateDreamSettings } from "../../_lib/dream";
import styles from "../settings.module.css";

/** Every zone this browser knows, for the time zone's suggestions. */
function knownZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
}

/**
 * Settings › Account › Dream: the person's time zone (Dream runs in their
 * night, 03:00 local) and the morning email. The edition page's "Dream
 * settings" and the email's "Change when, or turn it off" link here
 * (#dream).
 */
export function DreamSettingsPanel() {
  const { t } = useLocale();
  const dc = t.ledger.dream.settingsPanel;
  const settings = useDreamSettings();
  const update = useUpdateDreamSettings();
  const toast = useToast();
  const zones = useMemo(knownZones, []);
  const listId = useId();
  const device = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const [draft, setDraft] = useState<string | null>(null);
  const current = settings.data;
  if (!current) return null;

  const change = async (next: {
    timeZone?: string;
    morningEmail?: boolean;
  }) => {
    const outcome = await update(next);
    toast({
      text: outcome.ok ? dc.changed : t.ledger.dream.refused.failed,
    });
  };
  const zone = draft ?? current.timeZone;
  const onZone = (value: string) => {
    setDraft(value);
    if (value !== current.timeZone && zones.includes(value)) {
      setDraft(null);
      void change({ timeZone: value });
    }
  };

  return (
    <section className="mx-panel" id="dream" aria-labelledby="dream-settings">
      <header className="mx-panel-head">
        <h2 id="dream-settings" className="mx-panel-title">
          {dc.title}
        </h2>
        <span className="mx-meta">{dc.meta}</span>
      </header>
      <div className={styles.row}>
        <div className={styles.rowText}>
          <span className={styles.rowLabel}>{dc.timeZone}</span>
          <span className={styles.hint}>
            {current.timeZoneSource === "default"
              ? dc.timeZoneDefault
              : dc.timeZoneHint}
          </span>
        </div>
        <div className={styles.controls}>
          <Field
            aria-label={dc.timeZone}
            value={zone}
            list={listId}
            spellCheck={false}
            autoComplete="off"
            onChange={(event) => onZone(event.target.value)}
          />
          <datalist id={listId}>
            {zones.map((z) => (
              <option key={z} value={z} />
            ))}
          </datalist>
          {device && device !== current.timeZone ? (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void change({ timeZone: device })}
            >
              {interpolate(dc.useDevice, { zone: device })}
            </Button>
          ) : null}
        </div>
      </div>
      <div className={styles.row}>
        <div className={styles.rowText}>
          <span className={styles.rowLabel}>{dc.email}</span>
          <span className={styles.hint}>{dc.emailHint}</span>
        </div>
        <Segmented<"on" | "off">
          size="sm"
          label={dc.email}
          value={current.morningEmail ? "on" : "off"}
          onChange={(value) => void change({ morningEmail: value === "on" })}
          options={[
            { value: "on", label: dc.on },
            { value: "off", label: dc.off },
          ]}
        />
      </div>
    </section>
  );
}
