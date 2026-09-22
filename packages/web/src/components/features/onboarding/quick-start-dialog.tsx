"use client";

// =============================================================================
// Quick start — the first-week checklist as a card deck (founder spec
// 2026-09-21: "直接把 onboarding card 变成这个").
//
// One dialog, two layers:
//   1. Six step cards, each with a real action (the same actions the
//      old checklist rows performed) and a product-made illustration.
//      Progress dots are the checklist's own item state, so a finished
//      step is a finished step wherever it was finished.
//   2. A closing 「常见用法」 page: how to talk to your agent.
//
// Opened from: the memories-hero launcher, the notification drawer's
// 入门 rows, 入门与机制 › 快速开始, and once automatically for a brand
// new account. State lives in lib/quick-start-store.ts.
// =============================================================================

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  Copy,
  Moon,
  Sparkles,
  X,
} from "lucide-react";
import type { ChecklistItem, ChecklistPayload, Notification } from "memax-sdk";
import { Surface } from "@memaxlabs/ui";
import { useInterpolate, useLocale } from "@/i18n";
import {
  useCompleteChecklistItem,
  useNotifications,
  useViewChecklistItem,
} from "@/hooks/use-notifications";
import { useDreamReport, useDreamTrigger } from "@/hooks/use-dreams";
import { useBar } from "@/contexts/bar-context";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";
import { CLI_SETUP_CMD } from "@/lib/cli";
import { AGENT_BRAND_MARKS } from "@memaxlabs/ui/tokens/agent-brand-marks";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { HubCreateDialog } from "@/components/features/settings/hub-create-dialog";
import { ConnectAgentsBody } from "@/components/features/connect-agents-section";
import { HubBadge } from "@/components/features/hub/hub-badge";
import {
  closeQuickStart,
  openQuickStart,
  useQuickStartState,
  type QuickStartStep,
} from "@/lib/quick-start-store";

const STEP_ORDER: QuickStartStep[] = [
  "welcome",
  "connect_agent",
  "first_memory",
  "first_ask",
  "first_dream",
  "first_hub_invite",
];

/** Same URL ConnectAgentsBody hands to agents. */
const MCP_URL = "https://api.memax.app/mcp";
/** The one-liner, split into the three steps the card explains. */
const SETUP_STEPS = CLI_SETUP_CMD.split(" && ");
const TERMINAL_AGENTS = [
  "claude-code",
  "cursor",
  "codex",
  "gemini",
  "copilot",
  "windsurf",
  "openclaw",
  "muse",
];

type RememberScene = "agent" | "claude" | "web";
const REMEMBER_SCENES: RememberScene[] = ["agent", "claude", "web"];
const SCENE_MARKS: Record<RememberScene, string[]> = {
  agent: ["claude-code", "cursor", "openclaw"],
  claude: ["claude-ai"],
  web: [],
};

/** Real brand marks (lobe-icons, vendored in @memaxlabs/ui). */
function BrandMarkRow({
  slugs,
  size = "md",
}: {
  slugs: readonly string[];
  size?: "sm" | "md";
}) {
  const cls = size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4";
  return (
    <span className="inline-flex items-center gap-1.5">
      {slugs.map((slug) => {
        const Mark = AGENT_BRAND_MARKS[slug];
        return Mark ? (
          <Mark
            key={slug}
            className={cls}
            aria-label={AGENT_IDENTITIES[slug]?.displayName ?? slug}
            role="img"
          />
        ) : null;
      })}
    </span>
  );
}

function ReplyChip({ children }: { children: ReactNode }) {
  return (
    <span
      className="w-fit shrink-0 whitespace-nowrap rounded-md px-1.5 py-0.5 font-mono text-[10.5px]"
      style={{
        background: "oklch(from var(--signature) l c h / 0.12)",
        color: "var(--signature)",
      }}
    >
      {children}
    </span>
  );
}

/** One "you say → memax answers" pair. */
function Exchange({
  label,
  say,
  reply,
}: {
  label: string;
  say: string;
  reply: string;
}) {
  return (
    <div className="flex items-center gap-2.5">
      <span className="w-[22px] shrink-0 font-mono text-[10px] uppercase tracking-[0.12em] text-fg-4">
        {label}
      </span>
      <span className="min-w-0 rounded-[12px_12px_4px_12px] bg-foreground px-2.5 py-1 text-[12px] leading-snug text-background sm:truncate">
        {say}
      </span>
      <ReplyChip>{reply}</ReplyChip>
    </div>
  );
}

export function isOnboardingChecklist(n: Notification): boolean {
  return (
    n.kind === "checklist" &&
    n.source_kind === "onboarding" &&
    n.status === "pending"
  );
}

export function findOnboardingChecklist(
  rows: readonly Notification[] | undefined,
): Notification | null {
  if (!rows) return null;
  const candidates = rows.filter(isOnboardingChecklist);
  if (candidates.length === 0) return null;
  return candidates.sort(
    (a, b) => Date.parse(b.created_at) - Date.parse(a.created_at),
  )[0];
}

function itemById(payload: ChecklistPayload, id: string) {
  return payload.items.find((it) => it.id === id);
}

/** Which step a fresh open should land on: the first unfinished one. */
export function firstUnfinishedStep(payload: ChecklistPayload): QuickStartStep {
  for (const step of STEP_ORDER) {
    if (!stepDone(payload, step)) return step;
  }
  return "use_cases";
}

function stepDone(payload: ChecklistPayload, step: QuickStartStep): boolean {
  if (step === "use_cases") return false;
  if (step === "first_memory") {
    return (
      !!itemById(payload, "first_memory")?.completed_at &&
      !!itemById(payload, "five_memories")?.completed_at
    );
  }
  return !!itemById(payload, step)?.completed_at;
}

/** Whether a deck can open right now (a pending onboarding checklist
 *  exists). Launchers hide themselves when it is false. */
export function useQuickStartAvailable(): boolean {
  const { data } = useNotifications({ status: "pending" });
  return useMemo(
    () => findOnboardingChecklist(data?.notifications) !== null,
    [data],
  );
}

/**
 * QuickStartHost — mounts once in the app shell. Finds the pending
 * onboarding checklist, renders the dialog when opened, and opens it
 * once by itself for an account that has done nothing yet.
 */
export function QuickStartHost() {
  const { data } = useNotifications({ status: "pending" });
  const checklist = useMemo(
    () => findOnboardingChecklist(data?.notifications),
    [data],
  );
  const { open, step } = useQuickStartState();

  useEffect(() => {
    if (!checklist || typeof window === "undefined") return;
    const key = `memax.quickstart.autoOpened.${checklist.id}`;
    try {
      if (window.localStorage.getItem(key)) return;
      const payload = checklist.payload as unknown as ChecklistPayload;
      const anyDone = payload.items.some((it) => !!it.completed_at);
      window.localStorage.setItem(key, "1");
      if (!anyDone) openQuickStart("welcome");
    } catch {
      // storage unavailable → never auto-open, the launcher still works
    }
  }, [checklist]);

  if (!open || !checklist) return null;
  return (
    <QuickStartDialog
      notification={checklist}
      initialStep={step}
      onClose={closeQuickStart}
    />
  );
}

export function QuickStartDialog({
  notification,
  initialStep,
  onClose,
}: {
  notification: Notification;
  initialStep: QuickStartStep | null;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const isMobile = useIsMobile();
  const copy = t.quickStart;
  const payload = notification.payload as unknown as ChecklistPayload;
  const [step, setStep] = useState<QuickStartStep>(
    () => initialStep ?? firstUnfinishedStep(payload),
  );
  const [showConnectPanel, setShowConnectPanel] = useState(false);
  const [showHubCreate, setShowHubCreate] = useState(false);
  const [copied, setCopied] = useState<"command" | "url" | null>(null);
  const [rememberScene, setRememberScene] = useState<RememberScene>("agent");
  // Set on trigger success until the report shows the run — closes the
  // gap where isPending is already false but the report is still stale.
  const [dreamTriggered, setDreamTriggered] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);

  const viewItem = useViewChecklistItem();
  const completeItem = useCompleteChecklistItem();
  const dreamTrigger = useDreamTrigger();
  const dreamReport = useDreamReport();
  const { openBar } = useBar();
  const router = useRouter();

  const index = STEP_ORDER.indexOf(step);
  const total = STEP_ORDER.length;
  const doneCount = STEP_ORDER.filter((s) => stepDone(payload, s)).length;
  const allDone = !!payload.all_done_at || doneCount === total;

  const go = useCallback(
    (delta: number) => {
      setShowConnectPanel(false);
      if (step === "use_cases") {
        if (delta < 0) setStep(STEP_ORDER[total - 1]);
        return;
      }
      const next = index + delta;
      if (next >= total) {
        setStep("use_cases");
        return;
      }
      if (next < 0) return;
      setStep(STEP_ORDER[next]);
    },
    [index, step, total],
  );

  // Deck keys. A nested form (HubCreateDialog, ConnectAgentsBody
  // inputs) owns its own keys: arrows move the caret, Escape closes
  // the nearest layer, never the whole deck underneath it.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (showHubCreate) {
        if (e.key === "Escape") setShowHubCreate(false);
        return;
      }
      const target = e.target as HTMLElement | null;
      if (target?.closest("input,textarea,select,[contenteditable=true]")) {
        return;
      }
      if (e.key === "Escape") onClose();
      if (e.key === "ArrowRight") go(1);
      if (e.key === "ArrowLeft") go(-1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, onClose, showHubCreate]);

  // Page scroll stays put under the deck; focus moves into the dialog
  // (the container, so no ring flashes on a button) and Esc / Tab work
  // from there.
  useEffect(() => acquireBodyScrollLock(), []);
  useEffect(() => {
    dialogRef.current?.focus();
  }, []);

  // Reading the welcome card is the welcome item. `mutate` is stable
  // (React Query) and the boolean dep only flips once the server has
  // recorded it, so this fires once per unfinished visit.
  const welcomeDone = !!itemById(payload, "welcome")?.completed_at;
  const completeMutate = completeItem.mutate;
  useEffect(() => {
    if (step !== "welcome" || welcomeDone) return;
    completeMutate({ notificationId: notification.id, itemId: "welcome" });
  }, [step, welcomeDone, completeMutate, notification.id]);

  const markViewed = (itemId: string) => {
    const item = itemById(payload, itemId);
    if (item && !item.viewed_at) {
      viewItem.mutate({ notificationId: notification.id, itemId });
    }
  };

  const copyToClipboard = async (text: string, what: "command" | "url") => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(what);
      setTimeout(() => setCopied(null), 1800);
    } catch {
      // clipboard blocked — the text is visible to select by hand
    }
    markViewed("connect_agent");
  };
  const copyCommand = () => copyToClipboard(CLI_SETUP_CMD, "command");
  const copyConnectorUrl = () => copyToClipboard(MCP_URL, "url");

  const reportRunning = dreamReport.data?.run?.status === "running";
  const dreamRunning = reportRunning || dreamTriggered;
  useEffect(() => {
    if (reportRunning) setDreamTriggered(false);
  }, [reportRunning]);
  const dreamItem = itemById(payload, "first_dream");
  const dreamLocked =
    Array.isArray(dreamItem?.locked_by) &&
    dreamItem.locked_by.some((dep) => !itemById(payload, dep)?.completed_at);

  const fiveItem = itemById(payload, "five_memories");
  const fiveProgress = fiveItem?.progress;

  const primaryClass =
    "inline-flex items-center gap-1.5 rounded-[14px] bg-foreground px-4 py-2 text-[13px] font-medium text-background transition-opacity cursor-pointer hover:opacity-85 disabled:cursor-default disabled:opacity-50";
  const secondaryClass =
    "inline-flex items-center gap-1.5 rounded-[14px] border border-border/60 px-4 py-2 text-[13px] text-fg-2 transition-colors cursor-pointer hover:bg-surface-2 hover:text-fg-1";
  const ghostClass =
    "inline-flex items-center gap-1 rounded-[14px] px-3 py-2 text-[13px] text-fg-3 transition-colors cursor-pointer hover:text-fg-1";

  const card = (() => {
    switch (step) {
      case "welcome": {
        const w = t.onboarding.welcome;
        return {
          kicker: copy.kickerWelcome,
          title: w.title,
          body: null,
          art: (
            <div className="flex h-full flex-col justify-center gap-2 px-6 text-[13.5px] leading-relaxed text-fg-2">
              <p className="m-0">{w.paragraph1}</p>
              <p className="m-0">{w.paragraph2}</p>
              <p className="m-0 hidden sm:block">{w.paragraph3}</p>
              <p className="m-0 text-fg-3">{w.signature}</p>
            </div>
          ),
          actions: (
            <button
              type="button"
              className={primaryClass}
              onClick={() => go(1)}
            >
              {copy.start}
              <ArrowRight className="h-3.5 w-3.5" />
            </button>
          ),
        };
      }
      case "connect_agent":
        return {
          kicker: copy.kickerConnect,
          title: copy.connectTitle,
          body: copy.connectBody,
          art: (
            <div className="grid h-full grid-cols-1 gap-2 p-3 sm:grid-cols-[1.15fr_1fr]">
              {/* Pane 1 — agents in the terminal: three steps, one line */}
              <div className="flex min-h-0 flex-col gap-2.5 rounded-xl border border-border/50 bg-card p-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="m-0 font-mono text-[10px] uppercase tracking-[0.12em] text-fg-4">
                    {copy.connectPaneAgents}
                  </p>
                  <BrandMarkRow slugs={TERMINAL_AGENTS} />
                </div>
                <ol className="m-0 flex list-none flex-col gap-2 p-0">
                  {SETUP_STEPS.map((cmd, i) => (
                    <li key={cmd} className="flex items-start gap-2.5">
                      <span className="mt-[3px] grid h-4 w-4 shrink-0 place-items-center rounded-full bg-surface-2 font-mono text-[9.5px] text-fg-3">
                        {i + 1}
                      </span>
                      <div className="min-w-0">
                        <code className="block w-fit max-w-full truncate rounded-md bg-foreground px-2 py-[3px] font-mono text-[11px] text-background">
                          {cmd}
                        </code>
                        <p className="m-0 mt-0.5 text-[11.5px] leading-snug text-fg-3">
                          {copy.connectSteps[i]}
                        </p>
                      </div>
                    </li>
                  ))}
                </ol>
              </div>
              {/* Pane 2 — claude.ai custom connector (phone app inherits) */}
              <div className="flex min-h-0 flex-col rounded-xl border border-border/50 bg-card p-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="m-0 font-mono text-[10px] uppercase tracking-[0.12em] text-fg-4">
                    {copy.connectPaneClaude}
                  </p>
                  <BrandMarkRow slugs={["claude-ai"]} />
                </div>
                <div className="my-auto rounded-lg border border-border/50 bg-surface-1 p-2.5">
                  <p className="m-0 text-[11.5px] font-medium text-fg-2">
                    Add custom connector
                  </p>
                  <div className="mt-1.5 flex items-center gap-1.5 rounded-md bg-card px-2 py-1 text-[11.5px]">
                    <span className="text-fg-4">Name</span>
                    <span className="ml-auto font-medium text-fg-1">memax</span>
                  </div>
                  <div className="mt-1 flex items-center gap-1.5 rounded-md bg-card px-2 py-1 text-[11.5px]">
                    <span className="text-fg-4">URL</span>
                    <span className="ml-auto truncate font-mono text-[10.5px] text-fg-1">
                      {MCP_URL}
                    </span>
                  </div>
                </div>
                <p className="m-0 text-[11.5px] leading-snug text-fg-3">
                  {copy.connectPhoneNote}
                </p>
              </div>
            </div>
          ),
          actions: (
            <>
              <button
                type="button"
                className={primaryClass}
                onClick={copyCommand}
              >
                {copied === "command" ? (
                  <Check className="h-3.5 w-3.5" />
                ) : (
                  <Copy className="h-3.5 w-3.5" />
                )}
                {copied === "command" ? copy.copied : copy.copyOneLiner}
              </button>
              <button
                type="button"
                className={secondaryClass}
                onClick={copyConnectorUrl}
              >
                {copied === "url" ? (
                  <Check className="h-3.5 w-3.5" />
                ) : (
                  <Copy className="h-3.5 w-3.5" />
                )}
                {copied === "url" ? copy.copied : copy.copyConnectorUrl}
              </button>
              <button
                type="button"
                className={ghostClass}
                onClick={() => {
                  markViewed("connect_agent");
                  setShowConnectPanel((v) => !v);
                }}
                aria-expanded={showConnectPanel}
              >
                {copy.connectWeb}
              </button>
            </>
          ),
        };
      case "first_memory": {
        const scene = copy.rememberScenes[rememberScene];
        return {
          kicker: copy.kickerRemember,
          title: copy.rememberTitle,
          body:
            fiveProgress && typeof fiveProgress.current === "number"
              ? interpolate(copy.rememberProgress, {
                  current: fiveProgress.current,
                  target: fiveProgress.target ?? 5,
                })
              : copy.rememberBody,
          art: (
            <div className="flex h-full flex-col gap-3 p-3 sm:gap-2.5">
              {/* Scene tabs — where the user is when they want to remember */}
              <div
                role="tablist"
                aria-label={copy.rememberTitle}
                className="flex flex-wrap items-center gap-1"
              >
                {REMEMBER_SCENES.map((id) => (
                  <button
                    key={id}
                    type="button"
                    role="tab"
                    aria-selected={rememberScene === id}
                    onClick={() => setRememberScene(id)}
                    className={`inline-flex cursor-pointer items-center gap-1.5 rounded-full px-2.5 py-1 text-[11.5px] transition-colors ${
                      rememberScene === id
                        ? "bg-foreground text-background"
                        : "bg-surface-2 text-fg-3 hover:text-fg-1"
                    }`}
                  >
                    <BrandMarkRow slugs={SCENE_MARKS[id]} size="sm" />
                    {copy.rememberScenes[id].label}
                  </button>
                ))}
              </div>
              {/* The scene — a dump and a recall, in the user's words */}
              {rememberScene === "web" ? (
                <div className="flex flex-1 flex-col justify-center gap-2.5">
                  <div className="flex items-center gap-2.5 rounded-[18px] border border-border/50 bg-card px-3.5 py-2.5 shadow-glow">
                    <span style={{ color: "var(--signature)" }}>✦</span>
                    <span className="flex-1 truncate text-[13px] text-fg-3">
                      {t.compose.placeholder}
                    </span>
                    <span className="font-mono text-[10px] text-fg-4">⌘K</span>
                  </div>
                  <ReplyChip>{copy.rememberSaved}</ReplyChip>
                </div>
              ) : (
                <div className="flex flex-1 flex-col justify-center gap-2.5">
                  <Exchange
                    label={copy.rememberDumpLabel}
                    say={scene.dump}
                    reply={scene.dumpReply}
                  />
                  <Exchange
                    label={copy.rememberReferLabel}
                    say={scene.refer}
                    reply={scene.referReply}
                  />
                </div>
              )}
              <p className="m-0 text-[11.5px] leading-snug text-fg-3">
                {scene.hint}
              </p>
            </div>
          ),
          actions: (
            <button
              type="button"
              className={primaryClass}
              onClick={() => {
                markViewed("first_memory");
                onClose();
                openBar();
              }}
            >
              {copy.rememberCta}
            </button>
          ),
        };
      }
      case "first_ask":
        return {
          kicker: copy.kickerAsk,
          title: copy.askTitle,
          body: copy.askBody,
          art: (
            <div className="absolute inset-5 flex flex-col">
              <div className="ml-auto max-w-[62%] rounded-[14px_14px_4px_14px] bg-foreground px-3 py-2 text-[13px] text-background">
                {copy.askExampleQ}
              </div>
              <div className="mt-3 max-w-[78%] rounded-[14px_14px_14px_4px] border border-border/50 bg-card px-3 py-2.5 text-[13px] text-fg-2">
                {copy.askExampleA}
                <span
                  className="mt-1.5 block w-fit rounded-md px-1.5 py-0.5 font-mono text-[10px]"
                  style={{
                    background: "oklch(from var(--signature) l c h / 0.12)",
                    color: "var(--signature)",
                  }}
                >
                  {copy.askExampleCite}
                </span>
              </div>
            </div>
          ),
          actions: (
            <button
              type="button"
              className={primaryClass}
              onClick={() => {
                markViewed("first_ask");
                onClose();
                openBar();
              }}
            >
              {copy.askCta}
            </button>
          ),
        };
      case "first_dream": {
        const done = !!dreamItem?.completed_at;
        return {
          kicker: dreamRunning ? copy.kickerDreaming : copy.kickerDream,
          title: done
            ? copy.dreamDoneTitle
            : dreamRunning
              ? copy.dreamingTitle
              : copy.dreamTitle,
          body: done
            ? copy.dreamDoneBody
            : dreamRunning
              ? copy.dreamingBody
              : dreamLocked
                ? copy.dreamLocked
                : copy.dreamBody,
          art: (
            <div className="relative h-full">
              <div className="absolute inset-0 grid place-items-center">
                <div
                  className={`h-20 w-20 rounded-full ${dreamRunning ? "state-slow-breathe" : ""}`}
                  style={{
                    background:
                      "radial-gradient(circle at 35% 35%, oklch(0.95 0.03 290), var(--signature))",
                    boxShadow:
                      "0 0 60px oklch(from var(--signature) l c h / 0.45)",
                  }}
                />
              </div>
              {[
                { text: copy.dreamCard1, pos: "left-5 top-6" },
                { text: copy.dreamCard2, pos: "right-5 top-14" },
                { text: copy.dreamCard3, pos: "left-9 bottom-6" },
              ].map((c) => (
                <div
                  key={c.pos}
                  className={`absolute ${c.pos} w-[170px] rounded-xl border border-border/50 bg-card px-2.5 py-2 text-[12px] text-fg-2`}
                >
                  {c.text}
                </div>
              ))}
            </div>
          ),
          actions: done ? (
            <button
              type="button"
              className={primaryClass}
              onClick={() => {
                onClose();
                router.push("/pulse");
              }}
            >
              {copy.dreamSeePulse}
              <ArrowRight className="h-3.5 w-3.5" />
            </button>
          ) : dreamRunning ? (
            <>
              <span className={`${secondaryClass} cursor-default opacity-70`}>
                <Moon className="h-3.5 w-3.5" />
                {copy.dreamingChip}
              </span>
              <button type="button" className={ghostClass} onClick={onClose}>
                {copy.dreamingLeave}
              </button>
            </>
          ) : (
            <button
              type="button"
              className={primaryClass}
              disabled={dreamLocked || dreamTrigger.isPending}
              onClick={() => {
                markViewed("first_dream");
                dreamTrigger.mutate(undefined, {
                  onSuccess: () => setDreamTriggered(true),
                });
              }}
            >
              <Sparkles className="h-3.5 w-3.5" />
              {copy.dreamCta}
            </button>
          ),
        };
      }
      case "first_hub_invite":
        return {
          kicker: copy.kickerTeam,
          title: copy.teamTitle,
          body: copy.teamBody,
          art: (
            <div className="absolute inset-0 grid place-items-center">
              <div className="flex items-center gap-4">
                <HubBadge
                  kind="personal"
                  label={copy.teamMeInitial}
                  size="xl"
                />
                <span className="text-fg-4">→</span>
                <HubBadge kind="team" label="T" accent="violet" size="xl" />
                <HubBadge kind="team" label="Z" accent="teal" size="xl" />
              </div>
            </div>
          ),
          actions: (
            <>
              <button
                type="button"
                className={primaryClass}
                onClick={() => {
                  markViewed("first_hub_invite");
                  setShowHubCreate(true);
                }}
              >
                {copy.teamCta}
              </button>
              <button
                type="button"
                className={secondaryClass}
                onClick={() => go(1)}
              >
                {copy.teamSkip}
              </button>
            </>
          ),
        };
      case "use_cases":
        return {
          kicker: copy.kickerUseCases,
          title: allDone ? copy.useCasesTitleDone : copy.useCasesTitle,
          body: null,
          art: (
            <div className="grid h-full grid-cols-1 gap-2 overflow-y-auto p-3 sm:grid-cols-2">
              {copy.useCases.map((u) => (
                <div
                  key={u.say}
                  className="rounded-xl border border-border/50 bg-card px-3 py-2.5"
                >
                  <span className="inline-block rounded-[12px_12px_4px_12px] bg-foreground px-2.5 py-1 text-[12px] text-background">
                    {u.say}
                  </span>
                  <p className="m-0 mt-1.5 text-[12px] leading-snug text-fg-2">
                    {u.then}
                  </p>
                </div>
              ))}
            </div>
          ),
          actions: (
            <button type="button" className={primaryClass} onClick={onClose}>
              {copy.finish}
            </button>
          ),
        };
    }
  })();

  const dots = STEP_ORDER.map((s) => ({
    id: s,
    done: stepDone(payload, s),
    active: s === step,
  }));

  if (typeof document === "undefined") return null;

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
      />
      <div className="fixed inset-0 z-takeover flex items-end justify-center pointer-events-none sm:items-center sm:p-6">
        <div
          ref={dialogRef}
          tabIndex={-1}
          role="dialog"
          aria-modal="true"
          aria-label={copy.title}
          className={`pointer-events-auto w-full animate-fade-up outline-none ${isMobile ? "h-[100dvh]" : "max-w-[720px]"}`}
        >
          <Surface
            variant="subtle"
            rounded="2xl"
            className={`glass-dropdown backdrop-blur-sm flex flex-col overflow-hidden ${isMobile ? "h-full rounded-none!" : "max-h-[88dvh]"}`}
          >
            {/* top: dots + skip */}
            <div className="flex items-center justify-between px-4 pt-3.5 sm:px-5">
              <div
                role="group"
                className="flex items-center gap-2"
                aria-label={interpolate(copy.progress, {
                  done: doneCount,
                  total,
                })}
              >
                {dots.map((d) => (
                  <button
                    key={d.id}
                    type="button"
                    onClick={() => {
                      setShowConnectPanel(false);
                      setStep(d.id);
                    }}
                    aria-label={copy.stepLabel[d.id]}
                    aria-current={d.active ? "step" : undefined}
                    className="h-2 rounded-full transition-all cursor-pointer"
                    style={{
                      width: d.active ? 18 : 7,
                      background: d.active
                        ? "var(--signature)"
                        : d.done
                          ? "oklch(0.62 0.15 150)"
                          : "var(--fg-4)",
                    }}
                  />
                ))}
              </div>
              <button type="button" onClick={onClose} className={ghostClass}>
                {copy.skip}
                <X className="h-3.5 w-3.5" />
              </button>
            </div>

            {/* art */}
            <div
              className={`relative mx-4 mt-3 overflow-hidden rounded-2xl border border-border/50 bg-surface-1 sm:mx-5 ${
                step === "use_cases"
                  ? "min-h-[300px] flex-1 sm:h-[360px] sm:flex-none"
                  : step === "connect_agent"
                    ? "min-h-[420px] flex-1 sm:h-[280px] sm:flex-none"
                    : step === "first_memory"
                      ? "flex-none sm:h-[250px]"
                      : "min-h-[220px] flex-1 sm:h-[250px] sm:flex-none"
              }`}
            >
              {card.art}
            </div>

            {/* copy */}
            <div className="min-h-0 shrink overflow-y-auto px-5 pt-4 sm:flex-1 sm:px-6">
              <p
                className="m-0 font-mono text-[10px] uppercase tracking-[0.14em]"
                style={{ color: "var(--signature)" }}
              >
                {card.kicker}
              </p>
              <h2 className="mb-1.5 mt-1 text-[21px] font-semibold leading-tight tracking-[-0.015em] text-fg-1">
                {card.title}
              </h2>
              {card.body ? (
                <p className="m-0 max-w-[560px] text-[14px] leading-relaxed text-fg-2">
                  {card.body}
                </p>
              ) : null}
              {step === "connect_agent" && showConnectPanel ? (
                <div className="mt-3 rounded-xl border border-border/50 bg-card p-3">
                  <ConnectAgentsBody />
                </div>
              ) : null}
            </div>

            {/* actions */}
            <div className="flex flex-wrap items-center gap-2 px-5 pb-5 pt-3 sm:px-6">
              {card.actions}
              <span className="flex-1" />
              {index > 0 || step === "use_cases" ? (
                <button
                  type="button"
                  className={ghostClass}
                  onClick={() => go(-1)}
                  aria-label={copy.back}
                >
                  <ArrowLeft className="h-3.5 w-3.5" />
                </button>
              ) : null}
              {step !== "use_cases" ? (
                <button
                  type="button"
                  className={ghostClass}
                  onClick={() => go(1)}
                >
                  {copy.next}
                  <ArrowRight className="h-3.5 w-3.5" />
                </button>
              ) : null}
            </div>
          </Surface>
        </div>
      </div>
      {showHubCreate ? (
        <HubCreateDialog
          onCreated={() => {
            setShowHubCreate(false);
            completeItem.mutate({
              notificationId: notification.id,
              itemId: "first_hub_invite",
            });
            go(1);
          }}
          onCancel={() => setShowHubCreate(false)}
        />
      ) : null}
    </>,
    document.body,
  );
}

/** Progress summary for launchers (hero card, drawer row). */
export function quickStartProgress(payload: ChecklistPayload): {
  done: number;
  total: number;
  next: QuickStartStep;
  allDone: boolean;
} {
  const done = STEP_ORDER.filter((s) => stepDone(payload, s)).length;
  return {
    done,
    total: STEP_ORDER.length,
    next: firstUnfinishedStep(payload),
    allDone: !!payload.all_done_at || done === STEP_ORDER.length,
  };
}

export type { ChecklistItem };
