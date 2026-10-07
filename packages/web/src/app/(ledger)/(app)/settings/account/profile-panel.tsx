"use client";

import { useId, useMemo, useState, type FormEvent } from "react";
import { AgentStamp, Button, Field, Segmented } from "@memaxlabs/ledger";
import { interpolate, useLocale, type Locale } from "@/i18n";
import type { AccountView } from "@/lib/v2/data/account";
import { useAccountCommands } from "../../_lib/account";
import { useDreamSettings, useUpdateDreamSettings } from "../../_lib/dream";
import { useToast } from "../../_components/toasts";
import styles from "./account.module.css";

const LOCALES: Locale[] = ["en", "zh"];

/** Every zone this browser knows, for the time zone's suggestions. */
function knownZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
}

/**
 * Profile (Account.png): the stamp receipts show, the name it comes from,
 * the email sign-in codes go to (read only: it is how the person signs
 * in), Dream's time zone, and this device's language. Update sends what
 * changed: the name, and the zone (Dream's setting, the one Notifications'
 * quiet hours read). The language is the device's and changes at once.
 */
export function ProfilePanel({ account }: { account: AccountView }) {
  const { t, locale, setLocale } = useLocale();
  const copy = t.ledger.account.profile;
  const commands = useAccountCommands();
  const dream = useDreamSettings().data;
  const updateDream = useUpdateDreamSettings();
  const toast = useToast();
  const zones = useMemo(knownZones, []);
  const listId = useId();
  const [name, setName] = useState<string | null>(null);
  const [zone, setZone] = useState<string | null>(null);
  const [problem, setProblem] = useState<{
    field: "name" | "zone";
    text: string;
  } | null>(null);
  const [busy, setBusy] = useState(false);

  const currentZone = dream?.timeZone ?? "";
  const nameValue = name ?? account.name;
  const zoneValue = zone ?? currentZone;
  const nameChanged =
    name !== null && name.trim().replace(/\s+/g, " ") !== account.name;
  const zoneChanged = zone !== null && zone.trim() !== currentZone;
  const device = Intl.DateTimeFormat().resolvedOptions().timeZone;

  const update = async (event?: FormEvent) => {
    event?.preventDefault();
    if (busy || (!nameChanged && !zoneChanged)) return;
    const nextName = nameValue.trim().replace(/\s+/g, " ");
    if (nameChanged && (nextName === "" || [...nextName].length > 80)) {
      setProblem({ field: "name", text: copy.nameInvalid });
      return;
    }
    const nextZone = zoneValue.trim();
    if (zoneChanged && zones.length > 0 && !zones.includes(nextZone)) {
      setProblem({ field: "zone", text: copy.timeZoneInvalid });
      return;
    }
    setProblem(null);
    setBusy(true);
    try {
      const named = nameChanged ? await commands.rename(nextName) : null;
      const zoned = zoneChanged
        ? await updateDream({ timeZone: nextZone })
        : null;
      if ((named && !named.ok) || (zoned && !zoned.ok)) {
        toast({ text: copy.failed });
        return;
      }
      if (named) setName(null);
      if (zoned) setZone(null);
      toast({ text: copy.updated });
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="mx-panel" id="dream" aria-labelledby="profile-title">
      <form onSubmit={(e) => void update(e)}>
        <header className="mx-panel-head">
          <h2 id="profile-title" className="mx-panel-title">
            {copy.title}
          </h2>
          <Button
            type="submit"
            variant="primary"
            size="sm"
            pending={busy}
            disabled={!nameChanged && !zoneChanged}
          >
            {copy.update}
          </Button>
        </header>
        <div className={styles.profile}>
          <span className={styles.stamp}>
            <AgentStamp person={account.initials} name={account.name} />
          </span>
          <Field
            label={copy.name}
            value={nameValue}
            autoComplete="name"
            maxLength={120}
            onChange={(e) => setName(e.target.value)}
            hint={
              problem?.field === "name"
                ? undefined
                : interpolate(copy.nameHint, { initials: account.initials })
            }
            error={problem?.field === "name" ? problem.text : undefined}
          />
          <Field
            label={copy.email}
            value={account.email}
            readOnly
            hint={copy.emailHint}
          />
          <span />
          <div>
            <Field
              label={copy.timeZone}
              value={zoneValue}
              list={listId}
              spellCheck={false}
              autoComplete="off"
              onChange={(e) => setZone(e.target.value)}
              hint={problem?.field === "zone" ? undefined : copy.timeZoneHint}
              error={problem?.field === "zone" ? problem.text : undefined}
            />
            <datalist id={listId}>
              {zones.map((z) => (
                <option key={z} value={z} />
              ))}
            </datalist>
            {device && dream && device !== zoneValue ? (
              <Button variant="quiet" size="sm" onClick={() => setZone(device)}>
                {interpolate(copy.useDevice, { zone: device })}
              </Button>
            ) : null}
          </div>
          <div className={styles.language}>
            <span className="mx-field-label">{copy.language}</span>
            <Segmented<Locale>
              size="sm"
              label={copy.language}
              value={locale}
              onChange={setLocale}
              options={LOCALES.map((value) => ({
                value,
                label: (
                  <span lang={value === "zh" ? "zh-CN" : "en"}>
                    {copy.languages[value]}
                  </span>
                ),
              }))}
            />
          </div>
        </div>
      </form>
    </section>
  );
}
