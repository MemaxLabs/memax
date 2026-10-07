"use client";

import { useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { Button, Logo } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { getPublicMemaxClient } from "@/lib/memax-client";
import { SETTINGS_HREF } from "@/lib/v2/places";
import styles from "./unsubscribe.module.css";

type State = "ready" | "working" | "done" | "failed";

/**
 * /unsubscribe?token=…: the morning email's visible unsubscribe link,
 * no sign-in needed. It asks for one press rather than acting on load,
 * so a mail scanner opening the link turns nothing off; the email's
 * one-click header (RFC 8058) goes straight to the API.
 */
export function UnsubscribeScreen() {
  const { t } = useLocale();
  const copy = t.ledger.dream.unsubscribe;
  const token = useSearchParams()?.get("token")?.trim() ?? "";
  const client = useMemo(() => getPublicMemaxClient(), []);
  const [state, setState] = useState<State>("ready");

  const turnOff = async () => {
    setState("working");
    try {
      await client.v2.dream.unsubscribe(token);
      setState("done");
    } catch {
      setState("failed");
    }
  };

  return (
    <main className={styles.page}>
      <div className={styles.card}>
        <Logo variant="lockup" size={20} />
        <h1 className={styles.title}>
          {state === "done" ? copy.done : copy.title}
        </h1>
        {state === "done" ? (
          <p className={styles.lede}>{copy.doneDetail}</p>
        ) : !token ? (
          <p className={styles.lede}>{copy.missing}</p>
        ) : (
          <>
            <p className={styles.lede}>{copy.lede}</p>
            {state === "failed" ? (
              <p className={styles.error} role="alert">
                {copy.failed}
              </p>
            ) : null}
          </>
        )}
        <div className={styles.actions}>
          {token && state !== "done" ? (
            <Button
              variant="primary"
              disabled={state === "working"}
              onClick={() => void turnOff()}
            >
              {copy.confirm}
            </Button>
          ) : null}
          <Button variant="secondary" href={`${SETTINGS_HREF}#dream`}>
            {copy.settings}
          </Button>
        </div>
      </div>
    </main>
  );
}
