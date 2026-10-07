"use client";

import { useState } from "react";
import Link from "next/link";
import { Button, Field, PageHeader } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import {
  isClock,
  type NotificationChange,
  type NotificationChoiceView,
  type NotificationSettingsView,
  type QuietHoursView,
} from "@/lib/v2/data/settings";
import {
  deliveryNote,
  eventText,
  quietZoneText,
  type SettingsCopy,
} from "@/lib/v2/settings-copy";
import { PlaceSkeleton } from "../../_components/skeleton";
import { useToast } from "../../_components/toasts";
import {
  useNotificationSettings,
  useUpdateNotificationSettings,
} from "../../_lib/settings";
import styles from "./notifications.module.css";

type Copy = SettingsCopy["notifications"];

/** One event's row: in app (never a choice), email, and the channels to come. */
function EventRow({
  copy,
  choice,
  settings,
  onEmail,
}: {
  copy: Copy;
  choice: NotificationChoiceView;
  settings: NotificationSettingsView;
  onEmail: (on: boolean) => void;
}) {
  const { title, meta } = eventText(copy, choice, settings);
  const cell = (channel: string) =>
    interpolate(copy.cell, { event: title, channel });
  const later = (channel: string) => (
    <span role="cell" className={`${styles.cell} ${styles.later}`}>
      <label title={copy.notYet}>
        <input
          type="checkbox"
          checked={false}
          disabled
          aria-label={cell(`${channel}, ${copy.notYet}`)}
          readOnly
        />
      </label>
    </span>
  );
  return (
    <div role="row" className={styles.row}>
      <span role="rowheader" className={styles.event}>
        <span className={styles.title}>{title}</span>
        <span className={styles.meta}>{meta}</span>
      </span>
      <span role="cell" className={styles.cell}>
        <label>
          <input
            type="checkbox"
            checked={choice.inApp}
            disabled
            readOnly
            aria-label={cell(choice.inApp ? copy.inAppOn : copy.columns.inApp)}
          />
        </label>
      </span>
      <span role="cell" className={styles.cell}>
        <label>
          <input
            type="checkbox"
            checked={choice.email}
            aria-label={cell(copy.columns.email)}
            onChange={(event) => onEmail(event.target.checked)}
          />
        </label>
      </span>
      {later(copy.columns.phone)}
      {later(copy.columns.slack)}
    </div>
  );
}

/**
 * The quiet hours, in the person's zone: two times (24-hour, saved when
 * a field is left or on Enter) and whether decision gates still come
 * through. Clearing both turns them off.
 */
function QuietHours({
  copy,
  settings,
  onChange,
}: {
  copy: Copy;
  settings: NotificationSettingsView;
  onChange: (change: NotificationChange) => void;
}) {
  const q = settings.quietHours;
  const shown = (v: QuietHoursView) => ({
    from: v.on ? v.from : "",
    until: v.on ? v.until : "",
  });
  const [from, setFrom] = useState(shown(q).from);
  const [until, setUntil] = useState(shown(q).until);
  const [error, setError] = useState<{
    field: "from" | "until";
    text: string;
  } | null>(null);
  // When the times change underneath (a change saved, refused or made
  // elsewhere), the fields follow; focus stays where it is.
  const [seen, setSeen] = useState(q);
  if (seen.on !== q.on || seen.from !== q.from || seen.until !== q.until) {
    setSeen(q);
    setFrom(shown(q).from);
    setUntil(shown(q).until);
  }

  // Leaving a field saves; with the other one still empty it waits for
  // it (Enter says so at once).
  const commit = (strict: boolean) => {
    const f = from.trim();
    const u = until.trim();
    if (f === "" && u === "") {
      setError(null);
      if (q.on) onChange({ quietHours: { on: false } });
      return;
    }
    if (!strict && (f === "" || u === "")) return;
    if (!isClock(f) || !isClock(u)) {
      setError({
        field: isClock(f) ? "until" : "from",
        text: copy.quiet.invalid,
      });
      return;
    }
    if (f === u) {
      setError({ field: "until", text: copy.quiet.same });
      return;
    }
    setError(null);
    const delta: Partial<QuietHoursView> = {};
    if (!q.on) delta.on = true;
    if (f !== q.from) delta.from = f;
    if (u !== q.until) delta.until = u;
    if (Object.keys(delta).length > 0) onChange({ quietHours: delta });
  };
  const onKey = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      commit(true);
    }
  };
  const zone = quietZoneText(copy, settings);

  return (
    <section className="mx-panel" aria-labelledby="quiet-hours">
      <header className="mx-panel-head">
        <h2 id="quiet-hours" className="mx-panel-title">
          {copy.quiet.title}
        </h2>
        <Link
          href="/settings/account#dream"
          className={styles.zone}
          aria-label={`${zone}. ${copy.quiet.changeZone}`}
          title={copy.quiet.changeZone}
        >
          {zone}
        </Link>
      </header>
      <div className={styles.quiet}>
        <Field
          label={copy.quiet.from}
          mono
          inputMode="numeric"
          autoComplete="off"
          spellCheck={false}
          placeholder={q.from}
          value={from}
          error={error?.field === "from" ? error.text : undefined}
          onChange={(event) => setFrom(event.target.value)}
          onBlur={() => commit(false)}
          onKeyDown={onKey}
        />
        <Field
          label={copy.quiet.until}
          mono
          inputMode="numeric"
          autoComplete="off"
          spellCheck={false}
          placeholder={q.until}
          value={until}
          error={error?.field === "until" ? error.text : undefined}
          onChange={(event) => setUntil(event.target.value)}
          onBlur={() => commit(false)}
          onKeyDown={onKey}
        />
        <label className={styles.gates}>
          <input
            type="checkbox"
            checked={q.gatesThrough}
            onChange={(event) =>
              onChange({ quietHours: { gatesThrough: event.target.checked } })
            }
          />
          {copy.quiet.gates}
        </label>
        {!q.on ? <p className={styles.quietNote}>{copy.quiet.off}</p> : null}
      </div>
    </section>
  );
}

/**
 * Settings › Notifications (Notifications.png): for each event, where it
 * reaches the person (in the app always, by email if they choose; phone
 * and Slack aren't channels yet) and the quiet hours email waits through.
 * The morning edition's email is Dream's setting, the one its unsubscribe
 * link turns off. Every change goes to the server from the version last
 * read, so one made elsewhere since is reloaded, never undone.
 */
export function NotificationsSettings() {
  const { t, locale } = useLocale();
  const copy = t.ledger.settings.notifications;
  const settings = useNotificationSettings();
  const update = useUpdateNotificationSettings();
  const toast = useToast();
  const s = settings.data;

  const change = async (c: NotificationChange) => {
    const out = await update(c);
    if (!out.ok) {
      toast({ text: out.failure.kind === "clash" ? copy.clash : copy.failed });
    }
  };

  if (!s) {
    return (
      <>
        <PageHeader title={copy.title} lede={copy.lede} />
        {settings.isError ? (
          <section className="mx-panel" role="alert">
            <header className="mx-panel-head">
              <h2 className="mx-panel-title">{copy.loadFailed}</h2>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void settings.refetch()}
              >
                {copy.retry}
              </Button>
            </header>
          </section>
        ) : (
          <PlaceSkeleton
            title={copy.title}
            status={copy.loading}
            label={copy.loading}
          />
        )}
      </>
    );
  }

  return (
    <>
      <PageHeader title={copy.title} lede={copy.lede} />
      <section className="mx-panel" aria-label={copy.label}>
        <div role="table" aria-label={copy.label}>
          <div role="row" className={`${styles.row} ${styles.head}`}>
            <span role="columnheader">{copy.columns.when}</span>
            <span role="columnheader">{copy.columns.inApp}</span>
            <span role="columnheader">{copy.columns.email}</span>
            <span role="columnheader" className={styles.later}>
              {copy.columns.phone}
            </span>
            <span role="columnheader" className={styles.later}>
              {copy.columns.slack}
            </span>
          </div>
          {s.events.map((choice) => (
            <EventRow
              key={choice.event}
              copy={copy}
              choice={choice}
              settings={s}
              onEmail={(on) => void change({ email: { [choice.event]: on } })}
            />
          ))}
        </div>
      </section>
      <p className={styles.note}>{deliveryNote(copy, s, locale)}</p>
      <QuietHours copy={copy} settings={s} onChange={(c) => void change(c)} />
    </>
  );
}
