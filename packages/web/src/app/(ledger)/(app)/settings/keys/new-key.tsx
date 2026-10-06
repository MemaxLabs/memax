"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Field, Segmented } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useSource } from "../../_lib/data";
import { useToast } from "../../_components/toasts";
import { agentKeys } from "../../_places/agents/queries";
import styles from "./keys.module.css";

type May = "read" | "propose";

/**
 * A new API key, inline in the API keys panel: a name, read or propose
 * (never keep or forget), for the space the rail is on. The key is shown
 * once, with a copy button, then only its masked form.
 */
export function NewKey({
  space,
  onDone,
}: {
  space: SpaceSummary;
  onDone: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.agents.keys.form;
  const source = useSource();
  const queryClient = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [may, setMay] = useState<May>("read");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [secret, setSecret] = useState<{ name: string; value: string } | null>(
    null,
  );

  const create = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError(copy.nameRequired);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const created = await source.createApiKey({ name: trimmed, may, space });
      setSecret({ name: trimmed, value: created.secret });
      void queryClient.invalidateQueries({
        queryKey: agentKeys.keys(source.kind),
      });
    } catch {
      setError(copy.failed);
    } finally {
      setBusy(false);
    }
  };

  if (secret) {
    return (
      <div className={styles.form} role="status">
        <p className={styles.formTitle}>
          {interpolate(copy.createdTitle, { name: secret.name })}
        </p>
        <p className={styles.formHint}>{copy.createdBody}</p>
        <div className={styles.secret}>
          <code className={styles.secretText}>{secret.value}</code>
          <Button
            variant="secondary"
            size="sm"
            icon="copy"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(secret.value);
                toast({ text: copy.copied });
              } catch {
                toast({
                  text: t.ledger.agents.connect.copyFailed,
                });
              }
            }}
          >
            {copy.copy}
          </Button>
          <Button variant="primary" size="sm" onClick={onDone}>
            {copy.done}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form
      className={styles.form}
      aria-label={copy.title}
      onSubmit={(event) => {
        event.preventDefault();
        void create();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onDone();
        }
      }}
    >
      <div className={styles.formRow}>
        <Field
          className={styles.formName}
          label={copy.name}
          hint={copy.nameHint}
          error={error ?? undefined}
          value={name}
          autoFocus
          onChange={(event) => setName(event.target.value)}
        />
        <div className={styles.formMay}>
          <span className="mx-field-label">{copy.may}</span>
          <Segmented<May>
            size="sm"
            label={copy.may}
            value={may}
            onChange={setMay}
            options={[
              { value: "read", label: copy.read },
              { value: "propose", label: copy.propose },
            ]}
          />
        </div>
      </div>
      <div className={styles.formFoot}>
        <span className={`mx-meta ${styles.plain}`}>
          {interpolate(copy.space, { space: space.name })}
        </span>
        <span className={styles.grow} />
        <Button variant="quiet" size="sm" onClick={onDone}>
          {copy.cancel}
        </Button>
        <Button variant="primary" size="sm" type="submit" pending={busy}>
          {copy.create}
        </Button>
      </div>
    </form>
  );
}
