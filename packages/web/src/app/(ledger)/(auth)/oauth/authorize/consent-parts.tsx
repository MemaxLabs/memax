"use client";

import { Icon } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { spaceMetaLine, type ConsentCopy } from "@/lib/v2/consent-copy";
import type { ConsentSpaceView } from "@/lib/v2/data/consent";
import styles from "./consent.module.css";

/** "Which space": one of the person's spaces, as radios named hub_id. */
export function SpaceChoice({
  copy,
  client,
  spaces,
  chosen,
  onChoose,
}: {
  copy: ConsentCopy;
  client: string;
  spaces: ConsentSpaceView[];
  chosen: string | null;
  onChoose: (id: string) => void;
}) {
  return (
    <fieldset className={styles.spaces}>
      <legend className={`mx-section-label ${styles.legend}`}>
        {copy.which}
      </legend>
      {spaces.map((space) => {
        const on = space.id === chosen;
        return (
          <label
            key={space.id}
            className={[
              styles.space,
              on ? styles.on : "",
              space.disabled ? styles.unavailable : "",
            ].join(" ")}
          >
            <input
              type="radio"
              name="hub_id"
              value={space.id}
              checked={on}
              disabled={space.disabled}
              onChange={() => onChoose(space.id)}
            />
            <span className={styles.spaceText}>
              <span className={styles.spaceName}>{space.name}</span>
              <span className="mx-meta">{spaceMetaLine(copy, space)}</span>
              {space.disabled ? (
                <span className={`mx-meta ${styles.detail}`}>
                  {interpolate(copy.disabled, { client })}
                </span>
              ) : null}
            </span>
            <span className={`mx-meta ${styles.detail}`}>
              {space.compiles && !space.disabled
                ? interpolate(copy.compiles, { file: space.compiles })
                : null}
            </span>
          </label>
        );
      })}
    </fieldset>
  );
}

/**
 * What the agent will and won't be able to do in the chosen space: the
 * server's verdicts (policy, for the level consent connects it at), in
 * the catalogue's words. Nothing here is assumed.
 */
export function Abilities({
  copy,
  client,
  space,
}: {
  copy: ConsentCopy;
  client: string;
  space: ConsentSpaceView;
}) {
  return (
    <div className={styles.lists}>
      <div className={styles.column}>
        <p className={`mx-section-label ${styles.listLabel}`} id="consent-can">
          {interpolate(copy.will, { client })}
        </p>
        <ul className={styles.list} aria-labelledby="consent-can">
          {space.can.map((ability) => (
            <li key={ability} className={styles.item}>
              <Icon name="check" />
              <span>{copy.can[ability]}</span>
            </li>
          ))}
        </ul>
      </div>
      {space.cannot.length > 0 ? (
        <div className={styles.column}>
          <p
            className={`mx-section-label ${styles.listLabel}`}
            id="consent-cannot"
          >
            {copy.wont}
          </p>
          <ul className={styles.list} aria-labelledby="consent-cannot">
            {space.cannot.map((ability) => (
              <li key={ability} className={`${styles.item} ${styles.wont}`}>
                <Icon name="x" />
                <span>{copy.cannot[ability]}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
