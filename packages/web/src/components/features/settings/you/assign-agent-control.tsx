"use client";

/**
 * AssignAgentControl — the single affordance for the "which agent owns
 * this key?" question. Replaces the prior three-branch classifier
 * (static agent badge / static Standalone label / unassigned dropdown)
 * that made reassignment impossible without revoking the key. Users
 * now go freely between:
 *
 *   Linked(agent X) ↔ Linked(agent Y)
 *   Linked ↔ Standalone
 *   Linked ↔ Unassigned
 *   Standalone ↔ Unassigned
 *
 * The trigger pill always reflects current state (agent name, badge +
 * "Standalone", or "Unassigned"); the same Popover menu drives every
 * transition. Owner-side invariants (no agent_name+standalone=true)
 * are enforced by the PATCH handler, so the client only sends the
 * fields it wants to change.
 *
 * Lives alongside you-api-keys.tsx because it is scoped entirely to
 * that list — the shared Popover primitive handles portal, Floating
 * UI, focus, ARIA, click-away.
 */

import { Bot, Check, ChevronDown, UserPlus, X } from "lucide-react";
import {
  MenuItem,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useState } from "react";

/**
 * Mirror of model.NormalizeAgentSlug on the server (trim → lower →
 * literal U+0020 → "-"). Exposed here because the menu offers a
 * "Create agent: <slug>" shortcut derived from the key's own name.
 * Kept bit-exact so the preview slug matches what the server will
 * actually store.
 */
export function normalizeAgentSlug(value: string): string {
  return value.trim().toLowerCase().replaceAll(" ", "-");
}

export interface AssignAgentControlAgent {
  agent_name: string;
  display_name?: string | null;
}

export interface AssignAgentControlProps {
  keyId: string;
  keyName: string;
  agents: AssignAgentControlAgent[];
  currentAgent: string;
  standalone: boolean;
  busy: boolean;
  /** Assign a specific agent slug (empty string = clear assignment). */
  onAssign: (slug: string) => Promise<unknown>;
  /** Toggle the standalone flag. Clearing standalone returns the key
   *  to "Unassigned"; setting it silences the Assign affordance. */
  onStandalone: (value: boolean) => Promise<unknown>;
  /** Clear both agent and standalone — key goes back to Unassigned. */
  onClear: () => Promise<unknown>;
  t: ReturnType<typeof useLocale>["t"];
}

type MenuState = "linked" | "standalone" | "unassigned";

function resolveState(currentAgent: string, standalone: boolean): MenuState {
  if (currentAgent) return "linked";
  if (standalone) return "standalone";
  return "unassigned";
}

export function AssignAgentControl({
  keyId,
  keyName,
  agents,
  currentAgent,
  standalone,
  busy,
  onAssign,
  onStandalone,
  onClear,
  t,
}: AssignAgentControlProps) {
  const [open, setOpen] = useState(false);
  const state = resolveState(currentAgent, standalone);

  const currentAgentRow = agents.find((a) => a.agent_name === currentAgent);
  const triggerLabel = busy
    ? (t.apiKeys?.assigning ?? "Assigning...")
    : state === "linked"
      ? currentAgentRow?.display_name || currentAgent
      : state === "standalone"
        ? (t.apiKeys?.standalone ?? "Standalone")
        : (t.apiKeys?.unassigned ?? "Unassigned");

  const triggerIcon =
    state === "linked" ? <Bot className="h-3 w-3 text-fg-3 shrink-0" /> : null;

  // "Create agent: <slug>" is offered whenever the key's own name
  // normalizes to a slug that isn't already an existing agent AND
  // isn't the currently-linked agent (avoids a redundant "create the
  // thing you already have" row).
  const suggestedSlug = normalizeAgentSlug(keyName);
  const suggestedSlugExists = agents.some(
    (a) => a.agent_name === suggestedSlug,
  );
  const showCreateSuggestion =
    suggestedSlug !== "" &&
    !suggestedSlugExists &&
    suggestedSlug !== currentAgent;

  const menuLabel = t.apiKeys?.assignAgent ?? "Assign to agent";
  const showMarkStandalone = state !== "standalone";
  const showClear = state === "linked" || state === "standalone";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={busy}
        className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[12px] text-fg-2 hover:text-foreground hover:bg-surface-2 transition-colors cursor-pointer disabled:cursor-wait disabled:opacity-60"
        aria-label={`${triggerLabel} — ${menuLabel}`}
      >
        {triggerIcon}
        <span className="truncate max-w-[160px]">{triggerLabel}</span>
        <ChevronDown className="h-3 w-3" />
      </PopoverTrigger>
      <PopoverContent side="bottom" align="start" sideOffset={6}>
        <div role="menu" className="w-[220px] p-1">
          <div className="px-2.5 py-1.5 text-[11px] uppercase tracking-wider text-fg-4">
            {menuLabel}
          </div>
          {agents.map((a) => {
            const isCurrent = a.agent_name === currentAgent;
            return (
              <MenuItem
                key={a.agent_name}
                role="menuitem"
                onClick={async () => {
                  setOpen(false);
                  if (isCurrent) return; // no-op
                  await onAssign(a.agent_name);
                }}
                aria-current={isCurrent ? "true" : undefined}
                icon={<Bot className="h-3.5 w-3.5 text-fg-3" />}
                keybind={
                  isCurrent ? <Check className="h-3.5 w-3.5 text-fg-3" /> : null
                }
              >
                {a.display_name || a.agent_name}
              </MenuItem>
            );
          })}
          {showCreateSuggestion && (
            <MenuItem
              role="menuitem"
              onClick={async () => {
                setOpen(false);
                await onAssign(suggestedSlug);
              }}
              data-testid={`create-agent-${keyId}`}
              icon={<UserPlus className="h-3.5 w-3.5 text-fg-3" />}
            >
              {(t.apiKeys?.createAgent ?? "Create agent: {slug}").replace(
                "{slug}",
                suggestedSlug,
              )}
            </MenuItem>
          )}
          {(showMarkStandalone || showClear) && (
            <div className="my-1 border-t border-border/30" />
          )}
          {showMarkStandalone && (
            <MenuItem
              role="menuitem"
              onClick={async () => {
                setOpen(false);
                await onStandalone(true);
              }}
              data-testid={`mark-standalone-${keyId}`}
            >
              {t.apiKeys?.markStandalone ?? "Mark as standalone"}
            </MenuItem>
          )}
          {showClear && (
            <MenuItem
              role="menuitem"
              onClick={async () => {
                setOpen(false);
                await onClear();
              }}
              data-testid={`clear-assignment-${keyId}`}
              icon={<X className="h-3.5 w-3.5 text-fg-3" />}
            >
              {t.apiKeys?.clearAssignment ?? "Clear assignment"}
            </MenuItem>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
