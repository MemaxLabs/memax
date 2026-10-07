// V2 (Ledger) catalogue: English, the source of truth. Mounted as
// `t.ledger` in ../en.ts; ./zh.ts must match it key for key.
//
// Ledger voice (handoff design-system README): sentence case; no
// "AI", "magic", "smart", "delete", "save" or "approve"; no
// exclamation marks or emoji. voice.test.ts checks both locales.
import { ledgerActivityEn } from "./activity-en";
import { ledgerAgentsEn } from "./agents-en";
import { ledgerAppEn } from "./app-en";
import { ledgerBriefEn } from "./brief-en";
import { ledgerDreamEn } from "./dream-en";
import { ledgerMemoryEn } from "./memory-en";
import { ledgerOnboardingEn } from "./onboarding-en";
import { ledgerRecordsEn } from "./records-en";
import { ledgerReviewEn } from "./review-en";
import { ledgerTodayEn } from "./today-en";

export const ledgerEn = {
  meta: {
    description:
      "The context layer you own. Nothing is kept without a receipt.",
  },
  theme: {
    label: "Theme",
    light: "Paper",
    dark: "Carbon",
    system: "System",
  },
  notFound: {
    title: "There's no page at this address.",
    description:
      "Check the link. If someone shared it with you, it may point to a space you're not part of.",
    home: "Go to Memax",
  },
  error: {
    title: "This page didn't load.",
    description:
      "Nothing you kept was lost. Try again, and if it keeps happening, reload the page.",
    retry: "Try again",
    home: "Go to Memax",
    digest: "Error {digest}",
  },
  app: ledgerAppEn,
  activity: ledgerActivityEn,
  agents: ledgerAgentsEn,
  records: ledgerRecordsEn,
  review: ledgerReviewEn,
  memory: ledgerMemoryEn,
  brief: ledgerBriefEn,
  today: ledgerTodayEn,
  dream: ledgerDreamEn,
  onboarding: ledgerOnboardingEn,
  devTokens: {
    breadcrumb: "Ledger · Dev fixture",
    title: "Tokens and type",
    lede: "Every Ledger token and type style, in Paper and Carbon side by side. This page is the first visual regression fixture.",
    switchToV1: "Switch to V1",
    sections: {
      colour: "Colour",
      type: "Type",
      space: "Space",
      radius: "Radius",
      size: "Sizes",
      elevation: "Elevation",
      states: "State marks",
    },
    statesNote:
      "The SVG assets, drawn in Paper's colours for email and docs. In the app, StateMark follows the theme.",
    states: {
      proposed: "Proposed",
      kept: "Kept",
      merged: "Merged",
      stale: "Stale",
      faded: "Faded",
      conflict: "Conflict",
      forgotten: "Forgotten",
    },
    // One sample per type style in type.css, from the handoff copy.
    samples: {
      wordmark: "Memax",
      display: "One context. Every agent.",
      title: "Monday, October 5",
      heading: "Decisions",
      "memory-lg":
        "Background jobs run on River, Postgres‑backed. We do not use Temporal.",
      memory: "API errors use RFC 9457 problem+json.",
      "memory-proposed": "Deploy the v2 API to Fly.io in iad and ams.",
      prose:
        "Memax V2 is the context layer every agent on the team reads before it writes code.",
      "ui-title": "Waiting on you",
      ui: "Codex proposed 3 memories in memax-v2",
      "ui-strong": "Keep",
      "ui-sm": "Last read 14 min ago by Cursor",
      label: "Autonomy",
      receipt: "CX proposed · 14:02 · session 8f2c · M-0412",
      "receipt-strong": "M-0412",
      code: "npx memax-cli init",
    },
  },
} as const;
