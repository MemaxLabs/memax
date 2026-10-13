/**
 * Memax's plans: the one table the Pricing page reads (plan 25 D9,
 * accepted Oct 6, 2026, and internal-docs 01 "Plans", which spells D9 out
 * and adds Pro's Ask allowance). Prices are US dollars. Words live in the
 * catalogue (t.ledger.public.pricing), keyed by these ids, with the
 * numbers below filled in, so a number is written once.
 *
 * Billing isn't built yet (epic 3.4's Stripe work waits on a founder): no
 * plan is sold and no limit here is enforced (policy.Decide counts Ask
 * and Dream's cadence, but the alpha is free). A feature that isn't built
 * yet is `coming`, and the page says so beside it: nothing unbuilt reads
 * as available.
 */

export type PlanId = "free" | "pro" | "team" | "enterprise";

/** Built and in the product today, or planned for the plan. */
export type Availability = "now" | "coming";

export interface PlanPrice {
  /** Dollars a month, billed monthly. */
  monthly: number;
  /** Dollars a year, billed yearly. */
  yearly: number;
  /** Who pays it: the account, or each member who keeps (viewers are free). */
  per: "account" | "seat";
  /** How the yearly price is put (01: Pro "2 months free", Team "20% off"). */
  yearlySaving: "months" | "percent";
}

/**
 * What a plan includes. Each id has its sentence in the catalogue; the
 * values are its numbers ({n}, {personal}, {project}, {spaces}).
 */
export type PlanFeatureId =
  | "adapters"
  | "spaces"
  | "agentKinds"
  | "review"
  | "dreamWeekly"
  | "askMonthly"
  | "activityDays"
  | "unlimited"
  | "cloudAgents"
  | "dreamNightly"
  | "staleWatched"
  | "handoffsGates"
  | "viewers"
  | "everythingInPro"
  | "teamSpaces"
  | "decisionLedger"
  | "routing"
  | "integrations"
  | "auditYear"
  | "sso"
  | "euResidency"
  | "selfHosting";

export interface PlanFeature {
  id: PlanFeatureId;
  availability: Availability;
  values?: Readonly<Record<string, number>>;
}

/**
 * What the plan's button does while billing is closed: sign in (Free is
 * what every account has), wait (a paid plan can't be bought yet), or
 * write to us (Enterprise).
 */
export type PlanAction = "sign_in" | "coming" | "contact";

export interface Plan {
  id: PlanId;
  /** null: priced per contract (Enterprise). */
  price: PlanPrice | null;
  features: readonly PlanFeature[];
  action: PlanAction;
  /** Drawn with the ink edge (Pricing.png). A layout choice, not a claim. */
  outlined?: boolean;
}

const now = (id: PlanFeatureId, values?: Record<string, number>) =>
  ({ id, availability: "now", ...(values ? { values } : {}) }) as const;
const coming = (id: PlanFeatureId, values?: Record<string, number>) =>
  ({ id, availability: "coming", ...(values ? { values } : {}) }) as const;

export const PLANS: readonly Plan[] = [
  {
    id: "free",
    price: { monthly: 0, yearly: 0, per: "account", yearlySaving: "months" },
    features: [
      now("adapters"),
      now("spaces", { personal: 1, project: 1 }),
      now("agentKinds", { n: 3 }),
      now("review"),
      now("dreamWeekly"),
      now("askMonthly", { n: 50 }),
      now("activityDays", { n: 30 }),
    ],
    action: "sign_in",
  },
  {
    id: "pro",
    price: { monthly: 12, yearly: 120, per: "account", yearlySaving: "months" },
    features: [
      now("unlimited"),
      now("cloudAgents"),
      now("dreamNightly", { spaces: 5 }),
      now("staleWatched"),
      coming("handoffsGates"),
      coming("viewers", { n: 3 }),
      now("askMonthly", { n: 1000 }),
      now("activityDays", { n: 90 }),
    ],
    action: "coming",
    outlined: true,
  },
  {
    id: "team",
    price: { monthly: 20, yearly: 192, per: "seat", yearlySaving: "percent" },
    features: [
      now("everythingInPro"),
      coming("teamSpaces"),
      coming("decisionLedger"),
      coming("routing"),
      coming("integrations"),
      coming("auditYear"),
    ],
    action: "coming",
  },
  {
    id: "enterprise",
    price: null,
    features: [coming("sso"), coming("euResidency"), coming("selfHosting")],
    action: "contact",
  },
];

/** What every plan has (D9: receipts, export and Forget on every plan). */
export const EVERY_PLAN = ["receipts", "export", "forget"] as const;
export type EveryPlanId = (typeof EVERY_PLAN)[number];

/** The plans' currency. */
export const PLAN_CURRENCY = "USD";

/** A yearly price as a month of it, rounded to the dollar ($120 → $10). */
export function monthlyOfYearly(price: PlanPrice): number {
  return Math.round(price.yearly / 12);
}

/** Months a yearly price gives free against twelve monthly ones ($120 vs $144 → 2). */
export function monthsFree(price: PlanPrice): number {
  if (price.monthly === 0) return 0;
  return Math.round((price.monthly * 12 - price.yearly) / price.monthly);
}

/** The yearly discount in whole percent ($192 vs $240 → 20). */
export function yearlyDiscount(price: PlanPrice): number {
  if (price.monthly === 0) return 0;
  return Math.round((1 - price.yearly / (price.monthly * 12)) * 100);
}
