"use client";

import { Button, Icon } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { lastUsedText, sessionIcon, sessionLabel } from "@/lib/v2/account-copy";
import type { SessionView } from "@/lib/v2/data/account";
import { useAccountCommands, useSessions } from "../../_lib/account";
import { useSource } from "../../_lib/data";
import { useToast } from "../../_components/toasts";
import { usePlace } from "../../_places/place";
import styles from "./account.module.css";

/**
 * Signed in on (Account.png): every live session from /v2/sessions, this
 * one marked; sign any other out, or all of them (both need the person on
 * the web, which this page is). An agent with the CLI login can't sign
 * the person out of the web: the server refuses it.
 */
export function SessionsPanel() {
  const { t, locale } = useLocale();
  const copy = t.ledger.account.sessions;
  const sessions = useSessions();
  const commands = useAccountCommands();
  const toast = useToast();
  const source = useSource();
  const { timeZone } = usePlace();
  const list = sessions.data ?? [];
  const others = list.filter((s) => !s.current);
  // This session first, then the most recently used.
  const ordered = [...list].sort(
    (a, b) =>
      Number(b.current) - Number(a.current) ||
      Date.parse(b.lastUsedAt) - Date.parse(a.lastUsedAt),
  );

  const signOut = async (s: SessionView) => {
    const out = await commands.signOut(s.id);
    toast({
      text: out.ok
        ? interpolate(copy.signedOut, {
            client: sessionLabel(t.ledger.account, s).label,
          })
        : copy.failed,
    });
  };

  const signOutOthers = async () => {
    const out = await commands.signOutOthers();
    if (!out.ok) {
      toast({ text: copy.failed });
      return;
    }
    toast({
      text:
        out.value === 1
          ? copy.signedOutOther
          : out.value === 0
            ? copy.noneOther
            : interpolate(copy.signedOutOthers, { n: out.value }),
    });
  };

  return (
    <section className="mx-panel" aria-labelledby="sessions-title">
      <header className="mx-panel-head">
        <h2 id="sessions-title" className="mx-panel-title">
          {copy.title}
        </h2>
        <Button
          variant="quiet"
          size="sm"
          pending={commands.pending === "sign-out-others"}
          disabled={sessions.isSuccess && others.length === 0}
          disabledReason={copy.noneOther}
          onClick={() => void signOutOthers()}
        >
          {copy.signOutOthers}
        </Button>
      </header>
      {sessions.isError && !sessions.data ? (
        <div className={styles.alert} role="alert">
          <span>{copy.loadFailed}</span>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void sessions.refetch()}
          >
            {t.ledger.account.retry}
          </Button>
        </div>
      ) : null}
      {ordered.map((s) => {
        const { label, meta } = sessionLabel(t.ledger.account, s);
        return (
          <div className={styles.row} key={s.id}>
            <Icon name={sessionIcon(s)} />
            <span className={styles.name}>
              {label}
              {meta ? <span className="mx-meta"> · {meta}</span> : null}
            </span>
            <span className={styles.quiet}>
              {s.current
                ? copy.now
                : lastUsedText(
                    t.ledger.account,
                    s.lastUsedAt,
                    source.now(),
                    timeZone,
                    locale,
                  )}
            </span>
            {s.current ? (
              <span className={styles.quiet}>{copy.thisOne}</span>
            ) : (
              <Button
                variant="quiet"
                size="sm"
                pending={commands.pending === `sign-out:${s.id}`}
                onClick={() => void signOut(s)}
              >
                {copy.signOut}
              </Button>
            )}
          </div>
        );
      })}
    </section>
  );
}
