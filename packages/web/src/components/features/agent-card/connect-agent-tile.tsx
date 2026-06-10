"use client";

/**
 * `<ConnectAgentTile>` — "+" tile in the `/agents` grid (plan 28 §B-4).
 *
 * Visual: a small row of AI-agent identity icons (Claude, Cursor,
 * Codex, Gemini …) sits beneath the plus glyph so the affordance
 * reads as "connect AI assistants" at a glance — not a generic empty
 * slot. The label + sublabel tells the user what tapping does.
 *
 * Why a modal (and not a route): founder direction overrides the
 * earlier codex-review pushback ("Routes, not state") for THIS surface
 * specifically — connecting an agent is a transient setup step, not a
 * destination users dwell on, and the modal keeps the operator
 * grounded on `/agents` so they see their list of cards behind it
 * while running the install command. The `/agents/connect` route is
 * preserved as a deep-link fallback (someone who lands at that URL
 * directly still gets the full-page version).
 *
 * Modal content: the same `<ConnectAgentsBody>` the route uses, so
 * the install instructions never drift between the two entry points.
 */

import { useState } from "react";
import { Plus } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@memaxlabs/ui";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";
import { useLocale } from "@/i18n";
import { ConnectAgentsBody } from "@/components/features/connect-agents-section";

const PREVIEW_AGENTS = [
  "claude-code",
  "cursor",
  "windsurf",
  "copilot",
  "codex",
  "gemini",
] as const;

export function ConnectAgentTile() {
  const { t } = useLocale();
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={t.agentConfigs.connectNewAria}
        // Dashed-border + dimmed icon-ring vocabulary mirrors
        // <ComposePlusCell> and the kitchen 43 PlusCard canonical
        // (var(--glass-border), bg-surface-2 → bg-surface-3 on hover).
        // Height stays 140px / rounded-surface to match neighboring
        // <AgentGridCard> peers; only the *placeholder* vocabulary is
        // shared, not the card dimensions.
        className="group flex h-full min-h-35 flex-col items-center justify-center gap-3 rounded-surface bg-transparent p-4 transition-colors hover:bg-surface-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40 cursor-pointer"
        style={{ border: "1px dashed var(--glass-border)" }}
      >
        <span className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-2 transition-colors group-hover:bg-surface-3">
          <Plus
            className="h-4 w-4 text-fg-3 transition-colors group-hover:text-foreground"
            strokeWidth={2.2}
          />
        </span>
        <div className="flex flex-col items-center gap-1.5">
          <span className="text-[13px] font-medium text-foreground">
            {t.agentConfigs.connectNew}
          </span>
          {/* Identity row — six tinted circles so the user sees at a
              glance what kinds of agents this connects. The row is
              decorative; alt text lives on the parent button label. */}
          <div className="flex items-center gap-1" aria-hidden>
            {PREVIEW_AGENTS.map((slug) => {
              const agent = AGENT_IDENTITIES[slug];
              if (!agent) return null;
              const Icon = agent.icon;
              return (
                <span
                  key={slug}
                  className="flex h-5 w-5 items-center justify-center rounded-full"
                  style={{
                    background: `oklch(from ${agent.color} l c h / 0.14)`,
                  }}
                >
                  <Icon
                    className="h-2.5 w-2.5"
                    style={{ color: agent.color }}
                    strokeWidth={2}
                  />
                </span>
              );
            })}
          </div>
        </div>
      </button>

      {/* `max-h-[85vh]` + `flex flex-col` caps the modal at the viewport
          so the install command, value chips, and the alt-methods
          disclosure don't push the close (X) button off-screen on short
          windows. The body scrolls; the header (with the X via
          `showCloseButton`) stays fixed at the top, always reachable.
          `pr-8` on the title row reserves room for the absolutely-
          positioned close button so the title text never sits under it. */}
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 sm:max-w-lg">
        <DialogHeader className="pr-8">
          <DialogTitle>{t.agentConfigs.connectPageTitle}</DialogTitle>
          <DialogDescription>
            {t.agentConfigs.connectPageSubtitle}
          </DialogDescription>
        </DialogHeader>
        <div className="-mx-1 mt-4 flex-1 overflow-y-auto px-1">
          <ConnectAgentsBody />
        </div>
      </DialogContent>
    </Dialog>
  );
}
