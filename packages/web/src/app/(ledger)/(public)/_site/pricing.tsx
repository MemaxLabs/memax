"use client";

import { useState } from "react";
import { Button, Icon, Segmented, type IconName } from "@memaxlabs/ledger";
import { useLocale, type Locale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import {
  EVERY_PLAN,
  monthlyOfYearly,
  monthsFree,
  PLAN_CURRENCY,
  PLANS,
  yearlyDiscount,
  type EveryPlanId,
  type Plan,
  type PlanFeature,
} from "@/lib/v2/plans";
import { CONTACT, LINKS } from "./links";
import { SiteFrame } from "./site-frame";
import site from "./site.module.css";
import styles from "./pricing.module.css";

type Period = "monthly" | "yearly";

const EVERY_ICON: Record<EveryPlanId, IconName> = {
  receipts: "receipt",
  export: "file",
  forget: "forget",
};

function intlLocale(locale: Locale): string {
  return locale === "zh" ? "zh-CN" : "en-US";
}

/** "$12": whole dollars, the symbol alone in either language. */
function money(amount: number, locale: Locale): string {
  return new Intl.NumberFormat(intlLocale(locale), {
    style: "currency",
    currency: PLAN_CURRENCY,
    currencyDisplay: "narrowSymbol",
    maximumFractionDigits: 0,
  }).format(amount);
}

/**
 * Pricing (Pricing.png): the four plans from lib/v2/plans.ts, a monthly
 * or yearly price (D9's yearly toggle), what every plan has, and the
 * questions. Billing is closed during the alpha, and the page says so:
 * Free starts at sign-in, Pro and Team say they're coming, Enterprise is
 * an email. A feature that isn't built carries "Coming".
 */
export function PricingScreen() {
  const { t } = useLocale();
  const p = t.ledger.public.pricing;
  const [period, setPeriod] = useState<Period>("monthly");
  return (
    <SiteFrame current="pricing">
      <section className={styles.hero} aria-labelledby="pricing-title">
        <p className={site.eyebrow}>{p.eyebrow}</p>
        <h1 id="pricing-title" className={site.title}>
          <span className={site.line}>{p.titleLine1}</span>{" "}
          <span className={site.line}>{p.titleLine2}</span>
        </h1>
        <p className={site.lede}>{p.lede}</p>
      </section>

      <div className={styles.bar}>
        <p className={styles.alpha}>
          <Icon name="clock" size={16} />
          <span>{p.alpha}</span>
        </p>
        <Segmented<Period>
          label={p.period.label}
          size="sm"
          value={period}
          onChange={setPeriod}
          options={[
            { value: "monthly", label: p.period.monthly },
            { value: "yearly", label: p.period.yearly },
          ]}
        />
      </div>

      <div className={styles.plans}>
        {PLANS.map((plan) => (
          <PlanCard key={plan.id} plan={plan} period={period} />
        ))}
      </div>

      <section className={styles.every} aria-label={p.every.label}>
        {EVERY_PLAN.map((id) => (
          <div key={id} className={styles.everyItem}>
            <span className={styles.everyIcon}>
              <Icon name={EVERY_ICON[id]} size={16} />
            </span>
            <div>
              <strong className={styles.everyTitle}>{p.every[id].title}</strong>
              <span className={styles.everyBody}>{p.every[id].body}</span>
            </div>
          </div>
        ))}
      </section>

      <section className={styles.faq} aria-labelledby="faq-title">
        <h2 id="faq-title" className={styles.faqTitle}>
          {p.faq.title}
        </h2>
        <div className={styles.faqGrid}>
          {(["replace", "agent", "store", "leave"] as const).map((key) => (
            <div key={key} className={styles.faqItem}>
              <h3 className={styles.question}>{p.faq[key].q}</h3>
              <p className={styles.answer}>{p.faq[key].a}</p>
            </div>
          ))}
        </div>
      </section>
    </SiteFrame>
  );
}

function PlanCard({ plan, period }: { plan: Plan; period: Period }) {
  const { t } = useLocale();
  const p = t.ledger.public.pricing;
  const names = p.plans[plan.id];
  const titleId = `plan-${plan.id}`;
  return (
    <article
      className={[
        styles.plan,
        plan.outlined ? styles.outlined : "",
        plan.price === null ? styles.open : "",
      ].join(" ")}
      aria-labelledby={titleId}
    >
      <div className={styles.planHead}>
        <h2 id={titleId} className={styles.planName}>
          {names.name}
        </h2>
        <span className="mx-meta">{names.audience}</span>
      </div>
      <Price plan={plan} period={period} />
      <ul className={styles.features}>
        {plan.features.map((feature) => (
          <Feature key={feature.id} feature={feature} />
        ))}
      </ul>
      <div className={styles.action}>
        <PlanActionFor plan={plan} />
      </div>
    </article>
  );
}

function Price({ plan, period }: { plan: Plan; period: Period }) {
  const { t, locale } = useLocale();
  const p = t.ledger.public.pricing.price;
  const price = plan.price;
  if (price === null) {
    return <p className={styles.price}>{p.custom}</p>;
  }
  const yearly = period === "yearly";
  const amount = yearly ? price.yearly : price.monthly;
  const unit =
    price.per === "seat"
      ? yearly
        ? p.seatYear
        : p.seatMonth
      : yearly
        ? p.year
        : p.month;
  const saving =
    yearly && amount > 0
      ? price.yearlySaving === "months"
        ? interpolate(p.monthsFree, {
            price: money(monthlyOfYearly(price), locale),
            n: monthsFree(price),
          })
        : interpolate(p.discount, {
            price: money(monthlyOfYearly(price), locale),
            n: yearlyDiscount(price),
          })
      : null;
  return (
    <div className={styles.priceBlock}>
      <p className={styles.price}>
        {money(amount, locale)}
        {amount > 0 ? (
          <>
            {" "}
            <span className={styles.unit}>{unit}</span>
          </>
        ) : null}
      </p>
      {saving ? <p className="mx-meta">{saving}</p> : null}
      {price.per === "seat" ? <p className="mx-meta">{p.seats}</p> : null}
    </div>
  );
}

function Feature({ feature }: { feature: PlanFeature }) {
  const { t, locale } = useLocale();
  const p = t.ledger.public.pricing;
  const values = Object.fromEntries(
    Object.entries(feature.values ?? {}).map(([key, n]) => [
      key,
      new Intl.NumberFormat(intlLocale(locale)).format(n),
    ]),
  );
  const text = interpolate(p.features[feature.id], values);
  const coming = feature.availability === "coming";
  return (
    <li>
      <Icon name={coming ? "clock" : "check"} size={16} />
      <span>
        {text}
        {coming ? (
          <>
            {" "}
            <span className={styles.soon}>{p.coming}</span>
          </>
        ) : null}
      </span>
    </li>
  );
}

function PlanActionFor({ plan }: { plan: Plan }) {
  const { t } = useLocale();
  const p = t.ledger.public.pricing;
  switch (plan.action) {
    case "sign_in":
      return (
        <Button
          variant={plan.outlined ? "primary" : "secondary"}
          href={LINKS.signIn}
        >
          {p.actions.sign_in}
        </Button>
      );
    case "contact":
      return (
        <Button variant="quiet" render={<a href={`mailto:${CONTACT.team}`} />}>
          {p.actions.contact}
        </Button>
      );
    case "coming":
      // Not a button: there is nothing to press until billing opens.
      return (
        <p className={styles.closed}>
          <span className={styles.closedWord}>{p.actions.coming}</span>
          <span className="mx-meta">
            {plan.id === "team" ? p.closed.team : p.closed.pro}
          </span>
        </p>
      );
  }
}
