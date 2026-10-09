"use client";

import type { Translations } from "@/i18n/locales/en";
import { useLocale } from "@/i18n";
import type { Theme } from "../../../_lib/theme";
import { Button } from "@memaxlabs/ledger";
import { ThemeControl } from "../../../_components/theme-control";
import styles from "./specimen.module.css";

type DevTokensCopy = Translations["ledger"]["devTokens"];
export type TypeStyleName = keyof DevTokensCopy["samples"];
export type StateName = keyof DevTokensCopy["states"];

interface NamedValue {
  name: string;
  value: string;
}

export interface SpecimenData {
  colours: { name: string; light: string; dark: string }[];
  shadows: string[];
  typeStyles: { name: TypeStyleName; metrics: string }[];
  spacing: NamedValue[];
  radius: NamedValue[];
  controls: NamedValue[];
  measures: NamedValue[];
  states: { name: StateName; src: string }[];
}

const THEMES: Theme[] = ["light", "dark"];

/** The specimen body. Every visual comes from a token: `var(--name)`. */
export function TokenSpecimen({ data }: { data: SpecimenData }) {
  const { t } = useLocale();
  const copy = t.ledger.devTokens;

  return (
    <div className={styles.page}>
      <main className={styles.sheet}>
        <header className={styles.header}>
          <div>
            <p className={`label ${styles.breadcrumb}`}>{copy.breadcrumb}</p>
            <h1 className={`title ${styles.title}`}>{copy.title}</h1>
            <p className={`ui ${styles.lede}`}>{copy.lede}</p>
          </div>
          <div className={styles.controls}>
            <ThemeControl size="sm" />
            {/* A plain anchor: /dev/ui is a route handler, not a page. */}
            <Button size="sm" render={<a href="/dev/ui?v=1" />}>
              {copy.switchToV1}
            </Button>
          </div>
        </header>

        <Section id="colour" title={copy.sections.colour}>
          <ThemePair>
            {(theme) => (
              <ul className={styles.swatches}>
                {data.colours.map((colour) => (
                  <li key={colour.name} className={styles.swatchRow}>
                    <span
                      className={styles.swatch}
                      style={{ background: `var(--${colour.name})` }}
                      aria-hidden
                    />
                    <span className={styles.tokenText}>
                      <span className={`receipt-strong ${styles.tokenName}`}>
                        {colour.name}
                      </span>
                      <span className={`receipt ${styles.tokenValue}`}>
                        {colour[theme]}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </ThemePair>
        </Section>

        <Section id="elevation" title={copy.sections.elevation}>
          <ThemePair>
            {() => (
              <div className={styles.shadows}>
                {data.shadows.map((name) => (
                  <div
                    key={name}
                    className={`receipt ${styles.shadowCard}`}
                    style={{ boxShadow: `var(--${name})` }}
                  >
                    {name}
                  </div>
                ))}
              </div>
            )}
          </ThemePair>
        </Section>

        <Section id="type" title={copy.sections.type}>
          <ThemePair>
            {() => (
              <ul className={styles.typeList}>
                {data.typeStyles.map((style) => (
                  <li key={style.name} className={styles.typeRow}>
                    <span className={styles.typeMeta}>
                      <span className={`receipt-strong ${styles.tokenName}`}>
                        {style.name}
                      </span>
                      <span className={`receipt ${styles.metrics}`}>
                        {style.metrics}
                      </span>
                    </span>
                    <p className={`${style.name} ${styles.typeSample}`}>
                      {copy.samples[style.name]}
                    </p>
                  </li>
                ))}
              </ul>
            )}
          </ThemePair>
        </Section>

        <div className={styles.scales}>
          <Section id="space" title={copy.sections.space}>
            <ScaleList
              items={data.spacing}
              render={(name) => (
                <span
                  className={styles.bar}
                  style={{ width: `var(--${name})` }}
                />
              )}
            />
          </Section>
          <Section id="radius" title={copy.sections.radius}>
            <ScaleList
              items={data.radius}
              render={(name) => (
                <span
                  className={styles.radiusBox}
                  style={{ borderRadius: `var(--${name})` }}
                />
              )}
            />
          </Section>
        </div>

        <Section id="size" title={copy.sections.size}>
          <ScaleList
            items={data.controls}
            render={(name) => (
              <span
                className={styles.controlBox}
                style={{ height: `var(--${name})` }}
              />
            )}
          />
          <div className={styles.measures}>
            <ScaleList
              items={data.measures}
              render={(name) => (
                <span
                  className={styles.bar}
                  style={{ width: `var(--${name})` }}
                />
              )}
            />
          </div>
        </Section>

        <Section id="states" title={copy.sections.states}>
          <div className={styles.panel} data-theme="light">
            <ul className={styles.states}>
              {data.states.map((state) => (
                <li key={state.name} className={`ui ${styles.state}`}>
                  {/* eslint-disable-next-line @next/next/no-img-element -- 16px static SVG assets; next/image adds nothing here. */}
                  <img src={state.src} width={16} height={16} alt="" />
                  {copy.states[state.name]}
                </li>
              ))}
            </ul>
            <p className={`ui-sm ${styles.note}`}>{copy.statesNote}</p>
          </div>
        </Section>
      </main>
    </div>
  );
}

function Section({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className={styles.section} aria-labelledby={`specimen-${id}`}>
      <h2 id={`specimen-${id}`} className={`heading ${styles.sectionTitle}`}>
        {title}
      </h2>
      {children}
    </section>
  );
}

/** The same content in a Paper scope and a Carbon scope, side by side. */
function ThemePair({
  children,
}: {
  children: (theme: Theme) => React.ReactNode;
}) {
  const { t } = useLocale();
  return (
    <div className={styles.pair}>
      {THEMES.map((theme) => (
        <div key={theme} className={styles.panel} data-theme={theme}>
          <p className={`label ${styles.panelLabel}`}>
            {t.ledger.theme[theme]}
          </p>
          {children(theme)}
        </div>
      ))}
    </div>
  );
}

function ScaleList({
  items,
  render,
}: {
  items: NamedValue[];
  render: (name: string) => React.ReactNode;
}) {
  return (
    <ul className={styles.scaleList}>
      {items.map((item) => (
        <li key={item.name} className={styles.scaleRow}>
          <span className={styles.typeMeta}>
            <span className={`receipt-strong ${styles.tokenName}`}>
              {item.name}
            </span>
            <span className={`receipt ${styles.tokenValue}`}>{item.value}</span>
          </span>
          {render(item.name)}
        </li>
      ))}
    </ul>
  );
}
