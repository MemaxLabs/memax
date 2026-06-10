"use client";

import { useState } from "react";
import { useLocale, useInterpolate } from "@/i18n";
import { Surface } from "@memaxlabs/ui";
import {
  Copy,
  Check,
  ChevronDown,
  ChevronRight,
  Zap,
  Brain,
  Share2,
} from "lucide-react";
import { AGENT_IDENTITIES } from "@memaxlabs/ui/tokens/agents";

const SETUP_CMD =
  "npx memax-cli@latest login && npx memax-cli@latest setup --all";
const MCP_URL = "https://api.memax.app/mcp";
const MCP_CONFIG = `"memax": {
  "command": "npx",
  "args": ["-y", "memax-cli@latest", "mcp", "serve"]
}`;

/** Top 6 agents to show in the visual grid */
const FEATURED_AGENTS = [
  "claude-code",
  "cursor",
  "windsurf",
  "copilot",
  "codex",
  "gemini",
] as const;

/**
 * Settings disclosure consumer — wraps `<ConnectAgentsBody>` in the
 * Settings-style "Connect agents" section header + Surface so the
 * existing Settings flow stays byte-identical.
 *
 * The new `/agents/connect` route imports `<ConnectAgentsBody>`
 * directly so it can use page-level chrome without a nested section.
 * Two consumers, one body — keeps the install instructions in lockstep.
 */
export function ConnectAgentsSection() {
  const { t } = useLocale();

  return (
    <section className="mb-8">
      <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
        {t.settings.connectAgents}
      </p>
      <Surface variant="subtle" rounded="2xl" className="px-5 py-5">
        <ConnectAgentsBody />
      </Surface>
    </section>
  );
}

/**
 * Bare body of the connect-agents flow — agent identity grid, the npx
 * setup command, the value-prop chips, and the collapsible alternative
 * methods (MCP prompt + manual config). No outer section header or
 * surface; consumers wrap as needed.
 *
 * Two consumers:
 *   - `<ConnectAgentsSection>` (Settings disclosure, see above) wraps
 *     this in a Surface + section header.
 *   - `/agents/connect` route mounts this directly inside the page
 *     shell; the page's own `<PageHeader>` provides chrome.
 */
export function ConnectAgentsBody() {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  return (
    <>
      {/* Agent identity grid */}
      <div className="grid grid-cols-3 gap-2 mb-5">
        {FEATURED_AGENTS.map((slug) => {
          const agent = AGENT_IDENTITIES[slug];
          if (!agent) return null;
          const Icon = agent.icon;
          return (
            <div
              key={slug}
              className="flex items-center gap-2 rounded-surface px-3 py-2.5 bg-surface-1 hover:bg-surface-2 transition-colors"
            >
              <div
                className="flex items-center justify-center h-7 w-7 rounded-lg shrink-0"
                style={{
                  background: `oklch(from ${agent.color} l c h / 0.12)`,
                }}
              >
                <Icon
                  className="h-3.5 w-3.5"
                  style={{ color: agent.color }}
                  strokeWidth={1.8}
                />
              </div>
              <span className="text-[13px] text-fg-2 font-medium truncate">
                {agent.displayName}
              </span>
            </div>
          );
        })}
      </div>

      {/* One command */}
      <div className="mb-5">
        <p className="text-[14px] font-medium text-fg-2 mb-2">
          {t.settings.setupLabel}
        </p>
        <CopyBlock text={SETUP_CMD} mono />
        <p className="text-[12px] text-fg-3 mt-1.5">{t.settings.setupDesc}</p>
      </div>

      {/* What you get — value props */}
      <div className="grid grid-cols-3 gap-2 mb-4">
        {[
          {
            icon: Zap,
            label: t.settings.valueRecall,
            color: "oklch(0.72 0.19 145)",
          },
          {
            icon: Brain,
            label: t.settings.valuePush,
            color: "oklch(0.65 0.15 30)",
          },
          {
            icon: Share2,
            label: t.settings.valueShared,
            color: "oklch(0.65 0.15 250)",
          },
        ].map((v) => (
          <div
            key={v.label}
            className="flex items-center gap-2 rounded-surface px-3 py-2 bg-surface-1"
          >
            <v.icon
              className="h-3 w-3 shrink-0"
              style={{ color: v.color }}
              strokeWidth={1.8}
            />
            <span className="text-[12px] text-fg-2">{v.label}</span>
          </div>
        ))}
      </div>

      {/* Alternative methods — collapsed */}
      <AltMethodsDisclosure
        label={t.settings.altMethods}
        promptLabel={t.settings.promptLabel}
        promptText={interpolate(t.settings.promptTemplate, { url: MCP_URL })}
        configLabel={t.settings.manualConfig}
        configText={MCP_CONFIG}
        configHint={t.settings.pasteInEditor}
      />
    </>
  );
}

/* ── CopyBlock ─────────────────────────────────────────────────────── */

function CopyBlock({
  label,
  text,
  mono,
  hint,
}: {
  label?: string;
  text: string;
  mono?: boolean;
  hint?: string;
}) {
  const [copied, setCopied] = useState(false);

  return (
    <div>
      {label && <p className="text-[13px] text-fg-3 mb-1.5">{label}</p>}
      <button
        onClick={async () => {
          await navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        }}
        className="relative w-full text-left rounded-lg bg-foreground/3 hover:bg-foreground/5 transition-colors cursor-pointer px-3.5 py-3 group/copy"
      >
        <p
          className={`text-[14px] text-fg-2 leading-relaxed pr-8 ${mono ? "font-mono whitespace-pre-wrap" : ""}`}
        >
          {text}
        </p>
        <span className="absolute top-2.5 right-3 text-[12px] text-fg-3 group-hover/copy:text-fg-3 transition-colors">
          {copied ? (
            <Check className="h-3 w-3 text-emerald-500/70" />
          ) : (
            <Copy className="h-3 w-3" />
          )}
        </span>
        {hint && <p className="text-[12px] text-fg-3 mt-1.5">{hint}</p>}
      </button>
    </div>
  );
}

/* ── AltMethodsDisclosure ──────────────────────────────────────────── */

function AltMethodsDisclosure({
  label,
  promptLabel,
  promptText,
  configLabel,
  configText,
  configHint,
}: {
  label: string;
  promptLabel: string;
  promptText: string;
  configLabel: string;
  configText: string;
  configHint: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="pt-3 border-t border-border/10">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 text-[13px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
      >
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        {label}
      </button>
      {open && (
        <div className="mt-3 space-y-4">
          <CopyBlock label={promptLabel} text={promptText} />
          <CopyBlock
            label={configLabel}
            text={configText}
            mono
            hint={configHint}
          />
        </div>
      )}
    </div>
  );
}
