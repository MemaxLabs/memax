"use client";

/**
 * OnboardingMechanismModal — the rail's 入门与机制 entry (C1/C3).
 *
 * Two tabs in one centered glass overlay:
 *   - 快速开始: the connect-agents flow (ConnectAgentsBody), moved
 *     here from the avatar menu — connecting an agent is onboarding,
 *     not an account action.
 *   - 机制: "how memax remembers you" — the layer diagram (hub =
 *     ownership boundary → topic = context → memory with source
 *     attribution → nightly dreams) with LIVE numbers. Every figure
 *     is fetched from the same APIs the product runs on (hub summary,
 *     connected agents, dream report) — never hardcoded — so this
 *     page cannot drift from reality; when data hasn't loaded it
 *     shows an em-dash, never a fake number.
 */

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { Surface } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { useAuth, useActiveHub } from "@/lib/auth";
import { useHubSummary } from "@/hooks/use-hub-management";
import { useConnectedAgents } from "@/hooks/use-connected-agents";
import { useDreamReport } from "@/hooks/use-dreams";
import { formatAge } from "@/lib/format-age";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";
import { ConnectAgentsBody } from "./connect-agents-section";

type MechanismTab = "quickstart" | "mechanism";

export function OnboardingMechanismModal({
  onClose,
  initialTab = "quickstart",
}: {
  onClose: () => void;
  initialTab?: MechanismTab;
}) {
  const { t } = useLocale();
  const [tab, setTab] = useState<MechanismTab>(initialTab);

  useEffect(() => acquireBodyScrollLock(), []);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [onClose]);

  if (typeof document === "undefined") return null;

  const tabs: { id: MechanismTab; label: string }[] = [
    { id: "quickstart", label: t.mechanism.tabQuickstart },
    { id: "mechanism", label: t.mechanism.tabMechanism },
  ];

  return createPortal(
    <>
      <div
        className="fixed inset-0 z-takeover"
        style={{
          background: "rgba(0,0,0,0.4)",
          touchAction: "none",
          overscrollBehavior: "contain",
        }}
        onClick={onClose}
        onTouchMove={(e) => e.preventDefault()}
      />
      <div className="fixed inset-0 z-takeover flex items-center justify-center pointer-events-none p-4 sm:p-6">
        <div
          role="dialog"
          aria-modal="true"
          aria-label={t.mechanism.title}
          className="pointer-events-auto w-full max-w-xl animate-fade-up"
        >
          <Surface
            variant="subtle"
            rounded="2xl"
            className="glass-dropdown backdrop-blur-sm max-h-[85dvh] overflow-y-auto px-5 py-5"
          >
            <div className="mb-4 flex items-start justify-between gap-3">
              <div
                role="tablist"
                aria-label={t.mechanism.title}
                className="flex gap-1 rounded-xl bg-surface-1 p-1"
              >
                {tabs.map((entry) => (
                  <button
                    key={entry.id}
                    role="tab"
                    aria-selected={tab === entry.id}
                    onClick={() => setTab(entry.id)}
                    className={`cursor-pointer rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors ${
                      tab === entry.id
                        ? "bg-card text-foreground shadow-sm"
                        : "text-fg-3 hover:text-fg-2"
                    }`}
                  >
                    {entry.label}
                  </button>
                ))}
              </div>
              <button
                type="button"
                onClick={onClose}
                aria-label={t.personas.close}
                className="shrink-0 rounded-lg p-1.5 text-fg-3 transition-colors cursor-pointer hover:bg-surface-2 hover:text-fg-1"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            {tab === "quickstart" ? <ConnectAgentsBody /> : <MechanismPanel />}
          </Surface>
        </div>
      </div>
    </>,
    document.body,
  );
}

/** A live figure — em-dash until the API answers, never a fake number. */
function LiveStat({ label, value }: { label: string; value: string | null }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-xl bg-surface-1 px-3 py-2">
      <span className="text-[16px] font-semibold tabular-nums text-foreground">
        {value ?? "—"}
      </span>
      <span className="text-[11.5px] text-fg-3">{label}</span>
    </div>
  );
}

function MechanismPanel() {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { hubs } = useAuth();
  const { activeHub } = useActiveHub();
  const { data: summary } = useHubSummary(activeHub?.hub.id ?? null);
  const { data: agents } = useConnectedAgents();
  const { data: dreamReport } = useDreamReport();

  const memoriesCount = summary?.stats?.memories;
  const topicsCount = summary?.stats?.topics;
  const agentCount = agents?.length;
  const lastDreamAt = dreamReport?.has_run
    ? dreamReport.run?.finished_at
    : undefined;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h2 className="text-[15px] font-semibold text-foreground">
          {t.mechanism.title}
        </h2>
        <p className="mt-0.5 text-[13px] text-fg-3">{t.mechanism.subtitle}</p>
      </div>

      {/* Live figures — the "numbers come from the running product"
          doctrine. Four stats, all from existing endpoints. */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <LiveStat
          label={t.mechanism.statHubs}
          value={hubs.length > 0 ? String(hubs.length) : null}
        />
        <LiveStat
          label={t.mechanism.statMemories}
          value={memoriesCount != null ? String(memoriesCount) : null}
        />
        <LiveStat
          label={t.mechanism.statTopics}
          value={topicsCount != null ? String(topicsCount) : null}
        />
        <LiveStat
          label={t.mechanism.statAgents}
          value={agentCount != null ? String(agentCount) : null}
        />
      </div>

      {/* Layer diagram — OUR truth, drawn in code (glass, two themes):
          hub is the ownership/permission boundary, topics organize
          within it, memories carry source attribution, dreams tidy
          nightly. */}
      <div className="flex flex-col gap-2 rounded-xl border border-border/40 p-3">
        <MechanismLayer
          index="01"
          title={t.mechanism.layerHubTitle}
          body={t.mechanism.layerHubBody}
        />
        <div className="ml-4 border-l border-border/40 pl-3">
          <MechanismLayer
            index="02"
            title={t.mechanism.layerTopicTitle}
            body={t.mechanism.layerTopicBody}
          />
          <div className="ml-4 mt-2 border-l border-border/40 pl-3">
            <MechanismLayer
              index="03"
              title={t.mechanism.layerMemoryTitle}
              body={t.mechanism.layerMemoryBody}
            />
          </div>
        </div>
      </div>

      <div className="flex items-baseline justify-between gap-3 rounded-xl bg-surface-1 px-3 py-2.5">
        <div className="min-w-0">
          <p className="text-[13px] font-medium text-foreground">
            {t.mechanism.dreamsTitle}
          </p>
          <p className="mt-0.5 text-[12.5px] text-fg-3">
            {t.mechanism.dreamsBody}
          </p>
        </div>
        <span className="shrink-0 text-[12px] tabular-nums text-fg-3">
          {lastDreamAt
            ? interpolate(t.mechanism.lastDream, {
                age: formatAge(lastDreamAt, t, interpolate),
              })
            : "—"}
        </span>
      </div>

      <p className="text-[12px] text-fg-4">{t.mechanism.liveNote}</p>
    </div>
  );
}

function MechanismLayer({
  index,
  title,
  body,
}: {
  index: string;
  title: string;
  body: string;
}) {
  return (
    <div className="flex gap-2.5">
      <span className="mt-0.5 shrink-0 font-mono text-[10px] tracking-widest text-fg-4">
        {index}
      </span>
      <div className="min-w-0">
        <p className="text-[13px] font-medium text-foreground">{title}</p>
        <p className="mt-0.5 text-[12.5px] leading-relaxed text-fg-3">{body}</p>
      </div>
    </div>
  );
}
