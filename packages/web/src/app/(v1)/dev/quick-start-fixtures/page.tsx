"use client";

import { use } from "react";
import type { Notification } from "memax-sdk";
import { IsMobileProvider } from "@/hooks/use-is-mobile";
import { BarProvider } from "@/contexts/bar-context";
import { InboxSurfaceProvider } from "@/contexts/inbox-surface-context";
import { LocaleProvider, type Locale } from "@/i18n";
import { QuickStartDialog } from "@/components/features/onboarding/quick-start-dialog";
import {
  QuickStartDrawerRow,
  QuickStartHeroCard,
} from "@/components/features/onboarding/quick-start-launchers";
import type { QuickStartStep } from "@/lib/quick-start-store";

// Dev fixture for the quick-start deck (2026-09-21): mounts the real
// dialog + launchers on a fixture checklist so the cards can be looked
// at without a populated database. `?step=` picks the card, `?state=`
// the checklist progress (fresh | mid | done), `?lang=` the locale.

const STEPS: QuickStartStep[] = [
  "welcome",
  "connect_agent",
  "first_memory",
  "first_ask",
  "first_dream",
  "first_hub_invite",
  "use_cases",
];

function checklist(state: string): Notification {
  const done = (id: string) =>
    state === "done" ||
    (state === "mid" &&
      ["welcome", "connect_agent", "first_memory"].includes(id));
  const at = "2026-09-20T08:00:00Z";
  const items = [
    "welcome",
    "connect_agent",
    "first_memory",
    "first_ask",
    "five_memories",
    "first_hub_invite",
    "first_dream",
  ].map((id) => ({
    id,
    title: id,
    completed_at: done(id) ? at : undefined,
    progress:
      id === "five_memories"
        ? { current: state === "mid" ? 3 : state === "done" ? 5 : 0, target: 5 }
        : undefined,
    locked_by: id === "first_dream" ? ["five_memories"] : undefined,
  }));
  return {
    id: "checklist-dev",
    audience: "user",
    kind: "checklist",
    status: "pending",
    priority: 0,
    source_kind: "onboarding",
    created_at: at,
    payload: {
      items,
      required_ids: [
        "connect_agent",
        "first_memory",
        "first_ask",
        "five_memories",
        "first_dream",
      ],
      pin_context: "memories_hero",
      all_done_at: state === "done" ? at : undefined,
    },
  } as unknown as Notification;
}

export default function QuickStartFixturesPage({
  searchParams,
}: {
  searchParams: Promise<{ lang?: string; step?: string; state?: string }>;
}) {
  const params = use(searchParams);
  const locale: Locale = params.lang === "en" ? "en" : "zh";
  const step = STEPS.includes(params.step as QuickStartStep)
    ? (params.step as QuickStartStep)
    : "welcome";
  const n = checklist(params.state ?? "fresh");
  return (
    <LocaleProvider initialLocale={locale}>
      <IsMobileProvider>
        <InboxSurfaceProvider>
          <BarProvider>
            <div className="mx-auto max-w-[760px] space-y-4 p-6">
              <QuickStartHeroCard notification={n} />
              <div className="w-[340px] overflow-hidden rounded-xl border border-border/40 bg-card">
                <QuickStartDrawerRow n={n} />
              </div>
            </div>
            <QuickStartDialog
              notification={n}
              initialStep={step}
              onClose={() => {}}
            />
          </BarProvider>
        </InboxSurfaceProvider>
      </IsMobileProvider>
    </LocaleProvider>
  );
}
