"use client";

import type { CSSProperties } from "react";
import { PREVIEWS, type PreviewGroup } from "@memaxlabs/ledger/previews";
import { interpolate, useLocale } from "@/i18n";
import { ThemeControl } from "../../../_components/theme-control";
import styles from "./gallery.module.css";

const GROUPS: PreviewGroup[] = [
  "Brand",
  "Actions",
  "Provenance",
  "Memory",
  "Agents",
  "Surfaces",
];
const THEMES = ["light", "dark"] as const;

/**
 * Each preview sits in a box of exactly its artboard's size, so a 2×
 * screenshot of the box lines up pixel for pixel with the handoff's
 * `<Name>-<theme>.png`. The frame around the box is outside it.
 */
export function ComponentGallery() {
  const { t, locale } = useLocale();
  const copy = t.ledger.app.devComponents;
  return (
    <div className={styles.page}>
      <main className={styles.sheet}>
        <header className={styles.header}>
          <div>
            <p className={`label ${styles.breadcrumb}`}>{copy.breadcrumb}</p>
            <h1 className={`title ${styles.title}`}>{copy.title}</h1>
            <p className={`ui ${styles.lede}`}>{copy.lede}</p>
          </div>
          <ThemeControl size="sm" />
        </header>
        {GROUPS.map((group) => (
          <section
            key={group}
            className={styles.group}
            aria-labelledby={`group-${group}`}
          >
            <h2
              id={`group-${group}`}
              className={`heading ${styles.groupTitle}`}
            >
              {copy.groups[group]}
            </h2>
            {PREVIEWS.filter((p) => p.group === group).map((preview) => (
              <article
                key={preview.name}
                className={styles.preview}
                aria-labelledby={`preview-${preview.name}`}
              >
                <h3 id={`preview-${preview.name}`} className={styles.name}>
                  <span className="ui-strong">{preview.name}</span>
                  <span className="receipt">
                    {interpolate(copy.size, {
                      width: preview.width,
                      height: preview.height,
                    })}
                  </span>
                </h3>
                {THEMES.map((theme) => (
                  <div key={theme} className={styles.frame}>
                    <div
                      className={styles.artboard}
                      data-preview={preview.name}
                      data-preview-theme={theme}
                      data-theme={theme}
                      style={
                        {
                          "--artboard-w": `${preview.width}px`,
                          "--artboard-h": `${preview.height}px`,
                        } as CSSProperties
                      }
                    >
                      <preview.Component theme={theme} locale={locale} />
                    </div>
                  </div>
                ))}
              </article>
            ))}
          </section>
        ))}
      </main>
    </div>
  );
}
