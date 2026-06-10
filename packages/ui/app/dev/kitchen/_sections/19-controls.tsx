// Maps to: ui/button.tsx, ui/badge.tsx, ui/pill.tsx, ui/input.tsx
// Source of truth for all interactive controls in the product
// Every button, badge, pill, and input variant is rendered here
"use client";

import { useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import { Button } from "@/components/button";
import { Badge } from "@/components/badge";
import { InfoPopover } from "@/components/info-popover";
import { Pill } from "@/components/pill";
import {
  Trash2,
  Plus,
  ArrowRight,
  Loader2,
  Search,
  Star,
  Lightbulb,
  Hash,
} from "lucide-react";

/* ── Control color tokens (inlined — mirrors @memaxlabs/ui/tokens/controls) ──
 *
 * Design rule (see "Control color semantics" at bottom of this section):
 *   purple = Intelligence tab only; NEUTRAL_INK = every other active state.
 * Keep in sync with packages/ui/src/tokens/controls.ts.
 */
const NEUTRAL_INK = "var(--fg-1)";
const NEUTRAL_INK_INVERSE = "var(--background)";
const NEUTRAL_TRACK_OFF = "oklch(from var(--foreground) l c h / 0.08)";
const NEUTRAL_BORDER_OFF = "oklch(from var(--foreground) l c h / 0.18)";
const NEUTRAL_THUMB_OFF = "oklch(from var(--foreground) l c h / 0.40)";

/* ── Toggle Switch (local, not extracted yet) ──
 *
 * Accepts `variant="neutral" | "intelligence"`. Default is `neutral`.
 * Use `intelligence` ONLY for toggles on the Intelligence tab (dreams /
 * merge / archive / organize). Everywhere else (Account, Teams,
 * Appearance, Security, Dev) uses the neutral variant.
 */
function ToggleSwitch({
  label,
  description,
  defaultOn = false,
  async: isAsync = false,
  variant = "neutral",
}: {
  label: string;
  description?: string;
  defaultOn?: boolean;
  async?: boolean;
  variant?: "neutral" | "intelligence";
}) {
  const [on, setOn] = useState(defaultOn);
  const [pending, setPending] = useState(false);
  const isIntel = variant === "intelligence";

  const handleToggle = () => {
    const next = !on;
    setOn(next);
    if (isAsync) {
      setPending(true);
      setTimeout(() => setPending(false), 600);
    }
  };

  return (
    <button
      onClick={handleToggle}
      className="flex items-center justify-between w-full py-3 cursor-pointer group"
    >
      <div className="flex flex-col items-start gap-0.5">
        <span className="text-[14px] text-fg-2 group-hover:text-fg-1 transition-colors">
          {label}
        </span>
        {description && (
          <span className="text-[12px] text-fg-3">{description}</span>
        )}
      </div>
      <div
        className="relative w-10 h-6 rounded-full border transition-colors duration-200 shrink-0"
        style={{
          backgroundColor: on
            ? isIntel
              ? "var(--signature)"
              : NEUTRAL_INK
            : NEUTRAL_TRACK_OFF,
          borderColor: on
            ? isIntel
              ? "var(--signature)"
              : NEUTRAL_INK
            : NEUTRAL_BORDER_OFF,
        }}
      >
        <div
          className="absolute top-0.5 w-4.5 h-4.5 rounded-full transition-transform duration-200"
          style={{
            transform: on ? "translateX(18px)" : "translateX(2px)",
            backgroundColor: on
              ? isIntel
                ? "white"
                : NEUTRAL_INK_INVERSE
              : NEUTRAL_THUMB_OFF,
            opacity: 1,
          }}
        />
        {pending && (
          <div className="absolute inset-0 rounded-full animate-pulse border border-foreground/10" />
        )}
      </div>
    </button>
  );
}

/* ── Radio row (local) ──
 *
 * border-2 outer ring + NEUTRAL_INK dot. Use this pattern when each option
 * needs an inline description ("Plain / Signature / Time" where the user
 * has to LEARN what each means). Do NOT cargo-cult this to self-explanatory
 * options — use the pill picker below for those.
 */
function RadioRow({
  label,
  description,
  checked,
  onSelect,
}: {
  label: string;
  description: string;
  checked: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      onClick={onSelect}
      className="flex w-full items-start gap-3 rounded-lg px-1 py-3 text-left transition-colors hover:bg-surface-1 cursor-pointer"
    >
      <span
        className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2"
        style={{
          borderColor: checked ? NEUTRAL_INK : NEUTRAL_BORDER_OFF,
        }}
      >
        {checked && (
          <span
            className="h-2 w-2 rounded-full"
            style={{ background: NEUTRAL_INK }}
          />
        )}
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-[14px] text-fg-2">{label}</p>
        <p className="mt-0.5 text-[12px] text-fg-3">{description}</p>
      </div>
    </button>
  );
}

/* ── Pill picker (local) — canonical pattern for self-explanatory options ──
 *
 * Mirror of hub-invites-section.tsx:204-217. Use when option labels are
 * short and obvious (role names, plan tiers, filter chips). Do NOT use
 * when options need descriptions — use RadioRow instead.
 */
function PillPicker<T extends string>({
  options,
  value,
  onChange,
}: {
  options: Array<{ value: T; label: string }>;
  value: T;
  onChange: (next: T) => void;
}) {
  return (
    <div className="flex gap-1.5 flex-wrap">
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => onChange(opt.value)}
          className={`text-[13px] px-3 py-1.5 rounded-lg transition-colors cursor-pointer ${
            value === opt.value
              ? "bg-foreground text-background font-medium"
              : "text-fg-2 bg-surface-1 hover:bg-surface-2"
          }`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

function RadioRowDemo() {
  const [value, setValue] = useState<"none" | "signature" | "time">(
    "signature",
  );
  const options = [
    {
      value: "none" as const,
      label: "Plain",
      desc: "No banner, no aurora. Pure typography. The minimal fallback.",
    },
    {
      value: "signature" as const,
      label: "Signature",
      desc: "Static memax-branded gradient. Stable, premium, the shipping default.",
    },
    {
      value: "time" as const,
      label: "Match time of day",
      desc: "Aurora drifts through 6 time buckets. Personal hubs only — team hubs fall back to Signature.",
    },
  ];
  return (
    <div className="max-w-md">
      {options.map((opt) => (
        <RadioRow
          key={opt.value}
          label={opt.label}
          description={opt.desc}
          checked={value === opt.value}
          onSelect={() => setValue(opt.value)}
        />
      ))}
    </div>
  );
}

function PillPickerDemo() {
  const [role, setRole] = useState<"contributor" | "viewer" | "admin">(
    "contributor",
  );
  return (
    <div className="max-w-md space-y-3">
      <p className="text-[13px] text-fg-3 font-medium">Invite as</p>
      <PillPicker
        value={role}
        onChange={setRole}
        options={[
          { value: "contributor", label: "Contributor" },
          { value: "viewer", label: "Viewer" },
          { value: "admin", label: "Admin" },
        ]}
      />
    </div>
  );
}

/* ── Button variant/size data ── */

const BUTTON_VARIANTS = [
  { variant: "default" as const, label: "Default" },
  { variant: "outline" as const, label: "Outline" },
  { variant: "secondary" as const, label: "Secondary" },
  { variant: "ghost" as const, label: "Ghost" },
  { variant: "destructive" as const, label: "Destructive" },
  { variant: "link" as const, label: "Link" },
];

const BUTTON_SIZES = [
  { size: "xs" as const, label: "xs", h: "h-6" },
  { size: "sm" as const, label: "sm", h: "h-7" },
  { size: "default" as const, label: "default", h: "h-8" },
  { size: "lg" as const, label: "lg", h: "h-9" },
];

const ICON_SIZES = [
  { size: "icon-xs" as const, label: "icon-xs", dim: "24px" },
  { size: "icon-sm" as const, label: "icon-sm", dim: "28px" },
  { size: "icon" as const, label: "icon", dim: "32px" },
  { size: "icon-lg" as const, label: "icon-lg", dim: "36px" },
];

/* ── Badge variant data ── */

const BADGE_VARIANTS = [
  { variant: "default" as const, label: "Default" },
  { variant: "secondary" as const, label: "Secondary" },
  { variant: "outline" as const, label: "Outline" },
  { variant: "destructive" as const, label: "Destructive" },
  { variant: "ghost" as const, label: "Ghost" },
];

export function ControlsSection() {
  return (
    <Section
      title="19. Controls"
      description="Complete interactive control gallery. All variants rendered from production components (ui/button.tsx, ui/badge.tsx, ui/pill.tsx)."
    >
      {/* ── Button Variants ── */}
      <DemoCard label="Button — variants">
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            {BUTTON_VARIANTS.map((v) => (
              <Button key={v.variant} variant={v.variant}>
                {v.label}
              </Button>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {BUTTON_VARIANTS.filter((v) => v.variant !== "link").map((v) => (
              <Button
                key={v.variant + "-disabled"}
                variant={v.variant}
                disabled
              >
                {v.label}
              </Button>
            ))}
          </div>
        </div>
        <div className="mt-3 space-y-0.5 text-[10px] text-fg-4 font-mono">
          <p>
            {
              '<Button variant="default|outline|secondary|ghost|destructive|link">'
            }
          </p>
          <p>Source: ui/button.tsx — CVA variants, @base-ui/react primitive</p>
        </div>
      </DemoCard>

      {/* ── Button Sizes ── */}
      <DemoCard label="Button — sizes">
        <div className="flex flex-wrap items-end gap-2">
          {BUTTON_SIZES.map((s) => (
            <div key={s.size} className="flex flex-col items-center gap-1">
              <Button size={s.size}>Label</Button>
              <span className="text-[10px] text-fg-4 font-mono">{s.label}</span>
              <span className="text-[9px] text-fg-4">{s.h}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Button with Icons ── */}
      <DemoCard label="Button — with icons">
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm">
            <Plus data-icon="inline-start" className="size-3.5" />
            Add memory
          </Button>
          <Button variant="outline" size="sm">
            <Search data-icon="inline-start" className="size-3.5" />
            Search
          </Button>
          <Button variant="destructive" size="sm">
            <Trash2 data-icon="inline-start" className="size-3.5" />
            Delete
          </Button>
          <Button variant="secondary" size="sm">
            Continue
            <ArrowRight data-icon="inline-end" className="size-3.5" />
          </Button>
          <Button disabled size="sm">
            <Loader2
              data-icon="inline-start"
              className="size-3.5 animate-spin"
            />
            Saving...
          </Button>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Use data-icon=&quot;inline-start|inline-end&quot; for padding
          adjustment
        </p>
      </DemoCard>

      {/* ── Icon Buttons ── */}
      <DemoCard label="Button — icon-only sizes">
        <div className="flex flex-wrap items-end gap-3">
          {ICON_SIZES.map((s) => (
            <div key={s.size} className="flex flex-col items-center gap-1">
              <Button size={s.size} variant="outline">
                <Star className="size-4" />
              </Button>
              <span className="text-[10px] text-fg-4 font-mono">{s.label}</span>
              <span className="text-[9px] text-fg-4">{s.dim}</span>
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2 mt-3 pt-3 border-t border-border/20">
          {(
            ["default", "outline", "secondary", "ghost", "destructive"] as const
          ).map((v) => (
            <Button key={v + "-icon"} size="icon-sm" variant={v}>
              <Plus className="size-4" />
            </Button>
          ))}
        </div>
      </DemoCard>

      {/* ── Badge Variants ── */}
      <DemoCard label="Badge — variants">
        <div className="flex flex-wrap items-center gap-2">
          {BADGE_VARIANTS.map((v) => (
            <Badge key={v.variant} variant={v.variant}>
              {v.label}
            </Badge>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2 mt-3 pt-3 border-t border-border/20">
          <Badge variant="secondary">core</Badge>
          <Badge variant="secondary">process</Badge>
          <Badge variant="outline">3 sources</Badge>
          <Badge variant="destructive">Error</Badge>
          <Badge variant="default">Pro</Badge>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          {'<Badge variant="default|secondary|outline|destructive|ghost">'}
          <br />
          Source: ui/badge.tsx — h-5, rounded-4xl, text-xs
        </p>
      </DemoCard>

      {/* ══════════════════════════════════════════════════════════════════
          Pill — CANONICAL chip primitive (read this first)
          ══════════════════════════════════════════════════════════════════ */}
      <DemoCard label="Pill — canonical chip (select / remove / add / static)">
        <p className="text-[12px] text-fg-2 mb-3">
          <span className="text-fg-1 font-medium">
            One pill to rule them all.
          </span>{" "}
          Any chip-shaped element in the product — hub switcher, topic selector,
          memory tag, row metadata label — uses this primitive. Four variants
          cover every case; two sizes cover every density. If you find yourself
          writing{" "}
          <span className="font-mono text-[11px]">
            rounded-full border bg-...
          </span>{" "}
          inline, you&apos;re reinventing this. Stop and use{" "}
          <span className="font-mono text-[11px]">&lt;Pill&gt;</span> or{" "}
          <span className="font-mono text-[11px]">pillClass()</span> instead.
        </p>

        {/* ── Variants ── */}
        <div className="mb-4">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Variants (md size)
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <Pill variant="static" icon={<Hash className="size-3.5" />}>
              static label
            </Pill>
            <Pill
              variant="select"
              icon={<Lightbulb className="size-3.5 text-fg-3" />}
              onClick={() => {}}
            >
              select — opens menu
            </Pill>
            <Pill
              variant="remove"
              icon={<Hash className="size-3.5 text-fg-3" />}
              onRemove={() => {}}
            >
              remove — inline ×
            </Pill>
            <Pill variant="add" onClick={() => {}}>
              add topic
            </Pill>
          </div>
          <p className="text-[10px] text-fg-4 mt-2">
            <span className="font-mono">select</span>: chevron trailing, click
            opens popover. <span className="font-mono">remove</span>: × inside,
            for applied tags. <span className="font-mono">add</span>: dashed
            border empty state. <span className="font-mono">static</span>:
            read-only label.
          </p>
        </div>

        {/* ── Sizes ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Sizes — pick by the weight of surrounding text
          </p>
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-col items-start gap-1">
              <Pill
                size="lg"
                variant="select"
                icon={<Lightbulb className="size-4 text-fg-3" />}
                onClick={() => {}}
              >
                lg — comfortable
              </Pill>
              <span className="text-[10px] text-fg-4 font-mono">
                h-9, text-[14px]
              </span>
            </div>
            <div className="flex flex-col items-start gap-1">
              <Pill
                size="md"
                variant="select"
                icon={<Lightbulb className="size-3.5 text-fg-3" />}
                onClick={() => {}}
              >
                md — default
              </Pill>
              <span className="text-[10px] text-fg-4 font-mono">
                h-7, text-[12px]
              </span>
            </div>
            <div className="flex flex-col items-start gap-1">
              <Pill size="sm" variant="static">
                sm — metadata
              </Pill>
              <span className="text-[10px] text-fg-4 font-mono">
                h-5, text-[11px]
              </span>
            </div>
          </div>
          <p className="text-[10px] text-fg-4 mt-3">
            <span className="font-mono">lg</span>: comfortable weight — use when
            the pill is a primary element sitting next to 14px list or body
            content (e.g. the top-bar hub switcher whose dropdown items are 14px
            too). Matching weights keeps the trigger from feeling small next to
            its own menu. <span className="font-mono">md</span>: default chip —
            topic selectors, inline content chips, filter chips, role pickers.{" "}
            <span className="font-mono">sm</span>: ultra-dense metadata in
            memory rows where space is scarce.
          </p>
        </div>

        {/* ── Weight match (trigger vs list) ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Weight match — trigger must feel as substantial as its menu
          </p>
          <div className="flex items-start gap-6">
            {/* Wrong: md trigger next to 14px list */}
            <div className="flex flex-col items-start gap-2">
              <Pill
                size="md"
                variant="select"
                icon={
                  <span className="flex h-4 w-4 items-center justify-center rounded-full bg-signature/15 text-[9px] font-medium text-signature">
                    小
                  </span>
                }
                onClick={() => {}}
              >
                小宝记忆空间
              </Pill>
              <div className="w-52 rounded-xl border border-border bg-card p-1">
                <div className="flex items-center gap-2.5 rounded-lg bg-surface-2 px-2.5 py-2 text-[14px] font-medium text-foreground">
                  <span className="flex h-5 w-5 items-center justify-center rounded-full bg-signature/15 text-[10px] font-medium text-signature shrink-0">
                    小
                  </span>
                  <span className="flex-1 truncate">小宝记忆空间</span>
                  <span className="shrink-0 text-[12px] tabular-nums text-fg-4">
                    330
                  </span>
                </div>
              </div>
              <span className="text-[10px] text-fg-4 font-mono">
                ✗ md trigger + 14px list = trigger feels small
              </span>
            </div>
            {/* Right: lg trigger next to 14px list */}
            <div className="flex flex-col items-start gap-2">
              <Pill
                size="lg"
                variant="select"
                icon={
                  <span className="flex h-6 w-6 items-center justify-center rounded-full bg-signature/15 text-[11px] font-medium text-signature">
                    小
                  </span>
                }
                onClick={() => {}}
              >
                小宝记忆空间
              </Pill>
              <div className="w-52 rounded-xl border border-border bg-card p-1">
                <div className="flex items-center gap-2.5 rounded-lg bg-surface-2 px-2.5 py-2 text-[14px] font-medium text-foreground">
                  <span className="flex h-5 w-5 items-center justify-center rounded-full bg-signature/15 text-[10px] font-medium text-signature shrink-0">
                    小
                  </span>
                  <span className="flex-1 truncate">小宝记忆空间</span>
                  <span className="shrink-0 text-[12px] tabular-nums text-fg-4">
                    330
                  </span>
                </div>
              </div>
              <span className="text-[10px] text-fg-4 font-mono">
                ✓ lg trigger + 14px list = matching weight
              </span>
            </div>
          </div>
          <p className="text-[10px] text-fg-4 mt-3">
            Rule of thumb: if the pill&apos;s own menu uses{" "}
            <span className="font-mono">text-[14px]</span>, use{" "}
            <span className="font-mono">size=&quot;lg&quot;</span> on the
            trigger so it doesn&apos;t feel 小气 next to its list.
          </p>
        </div>

        {/* ── Real product examples ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            In the wild — these are the four canonical uses
          </p>
          <div className="space-y-3">
            {/* Hub switcher — lg size to match 14px dropdown list items */}
            <div className="flex items-center gap-3">
              <Pill
                size="lg"
                variant="select"
                icon={
                  <span className="flex h-6 w-6 items-center justify-center rounded-full bg-signature/15 text-[11px] font-medium text-signature">
                    小
                  </span>
                }
                onClick={() => {}}
              >
                小宝记忆空间
              </Pill>
              <span className="text-[10px] text-fg-4 font-mono">
                hub switcher (top bar) — size=&quot;lg&quot;
              </span>
            </div>

            {/* Topic selector */}
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1">
                <Pill
                  variant="select"
                  icon={<Lightbulb className="size-3.5 text-fg-3" />}
                  onClick={() => {}}
                >
                  Opportunity Research
                </Pill>
                <button
                  className="min-h-11 min-w-8 flex items-center justify-center text-fg-4 hover:text-fg-2 cursor-pointer transition-colors rounded-lg"
                  aria-label="Clear topic"
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </div>
              <span className="text-[10px] text-fg-4 font-mono">
                topic selector (memory detail)
              </span>
            </div>

            {/* Memory tag */}
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1.5">
                <Pill variant="remove" onRemove={() => {}}>
                  react
                </Pill>
                <Pill variant="remove" onRemove={() => {}}>
                  deployment
                </Pill>
              </div>
              <span className="text-[10px] text-fg-4 font-mono">
                memory tags
              </span>
            </div>

            {/* Memory row metadata */}
            <div className="flex items-center gap-3">
              <Pill size="sm" variant="static">
                personal
              </Pill>
              <span className="text-[10px] text-fg-4 font-mono">
                memory row metadata (sm)
              </span>
            </div>
          </div>
        </div>

        {/* ── Compose with PopoverTrigger via pillClass ── */}
        <div className="pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Composing with PopoverTrigger / Link / custom wrappers
          </p>
          <pre className="text-[10px] font-mono text-fg-2 bg-surface-1 rounded-md p-2.5 overflow-x-auto">
            {`import { Pill, pillClass, Popover, PopoverTrigger } from "@memaxlabs/ui";

// Option 1 — <Pill> component (preferred for plain cases)
<Pill variant="select" icon={<Icon />} onClick={open}>Hub name</Pill>

// Option 2 — pillClass() helper (when another element owns the interaction,
// e.g. Base UI PopoverTrigger needs to BE the button itself)
<PopoverTrigger className={pillClass({ variant: "select" })}>
  <Icon />
  <span>Hub name</span>
  <ChevronDown className="size-3.5 shrink-0 text-fg-4" />
</PopoverTrigger>`}
          </pre>
        </div>

        {/* ── Migration map for agents replacing legacy inline styles ── */}
        <div className="mt-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Migration map — legacy call sites to replace
          </p>
          <div className="space-y-2 text-[11px] text-fg-2">
            <div>
              <span className="font-mono text-fg-1">
                web/components/features/hub-identity-chip.tsx
              </span>
              <br />
              <span className="text-fg-3">
                standalone variant →{" "}
                <span className="font-mono">
                  pillClass(&#123; variant: &quot;select&quot; &#125;)
                </span>{" "}
                on the PopoverTrigger. Keep HubBadge as the icon slot, keep
                HubRoleTag, drop the hand-rolled{" "}
                <span className="font-mono">
                  border border-border/70 bg-card...
                </span>{" "}
                and inherit the canonical{" "}
                <span className="font-mono">rounded-lg</span> arc.
              </span>
            </div>
            <div>
              <span className="font-mono text-fg-1">
                web/components/features/topic-pills.tsx (TopicLocation)
              </span>
              <br />
              <span className="text-fg-3">
                filled state →{" "}
                <span className="font-mono">
                  pillClass(&#123; variant: &quot;select&quot; &#125;)
                </span>
                . Empty state →{" "}
                <span className="font-mono">
                  pillClass(&#123; variant: &quot;add&quot; &#125;)
                </span>
                . Keep the external × button as-is (separate 44px clear target).
                Canonical radius is{" "}
                <span className="font-mono">rounded-lg</span> — the crisp 8px
                arc that matches section 2a&apos;s{" "}
                <span className="font-mono">TopicPill</span> reference.
              </span>
            </div>
            <div>
              <span className="font-mono text-fg-1">
                web/components/features/memory-row.tsx (hub label)
              </span>
              <br />
              <span className="text-fg-3">
                →{" "}
                <span className="font-mono">
                  &lt;Pill size=&quot;sm&quot; variant=&quot;static&quot;&gt;
                </span>
                . Drop the inline{" "}
                <span className="font-mono">
                  text-[10px] bg-surface-1 px-1.5 py-0.5 rounded
                </span>
                .
              </span>
            </div>
          </div>
        </div>

        <p className="text-[10px] text-fg-4 font-mono mt-3 pt-3 border-t border-border/20">
          Source: ui/pill.tsx — rounded-lg, bg-surface-1, border-border/60,
          text-[14px] (lg) / text-[12px] (md) / text-[11px] (sm). Variants:
          select | remove | add | static. 8px arc matches section 2a&apos;s
          TopicPill reference — crisp, not overly round.
        </p>
      </DemoCard>

      <DemoCard label="Pill — removable tag">
        <div className="flex flex-wrap items-center gap-2">
          <Pill variant="static">react</Pill>
          <Pill variant="static">deployment</Pill>
          <Pill variant="remove" onRemove={() => {}}>
            removable
          </Pill>
          <Pill variant="remove" onRemove={() => {}}>
            with clear ×
          </Pill>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          {'<Pill variant="remove" onRemove={fn}>label</Pill>'} — Source:
          ui/pill.tsx.{" "}
          <span className="text-fg-3">
            Use the same `Pill` primitive for removable tags instead of a
            separate tag component. This keeps chip semantics and tokens aligned
            across the product.
          </span>
        </p>
      </DemoCard>

      {/* ── Send Button (bar-specific) ── */}
      <DemoCard label="Send button — bar-specific control">
        <div className="flex items-center gap-4">
          {/* Push mode */}
          <div className="flex flex-col items-center gap-1.5">
            <div
              className="flex items-center justify-center h-8 w-8 rounded-lg"
              style={{
                background: "var(--foreground)",
                color: "var(--background)",
              }}
            >
              <ArrowRight className="h-4 w-4 -rotate-90" />
            </div>
            <span className="text-[10px] text-fg-3">Push</span>
          </div>
          {/* Recall mode */}
          <div className="flex flex-col items-center gap-1.5">
            <div
              className="flex items-center justify-center h-8 w-8 rounded-lg"
              style={{ background: SIGNATURE, color: "#fff" }}
            >
              <span className="text-[13px]">✦</span>
            </div>
            <span className="text-[10px] text-fg-3">Recall</span>
          </div>
          {/* Loading */}
          <div className="flex flex-col items-center gap-1.5">
            <div
              className="flex items-center justify-center h-8 w-8 rounded-lg"
              style={{ background: SIGNATURE, color: "#fff" }}
            >
              <Loader2 className="h-4 w-4 animate-spin" />
            </div>
            <span className="text-[10px] text-fg-3">Loading</span>
          </div>
          {/* Disabled */}
          <div className="flex flex-col items-center gap-1.5">
            <div
              className="flex items-center justify-center h-8 w-8 rounded-lg opacity-40"
              style={{ color: "var(--fg-4)" }}
            >
              <ArrowRight className="h-4 w-4 -rotate-90" />
            </div>
            <span className="text-[10px] text-fg-3">Disabled</span>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          h-8 w-8 rounded-lg. Push: foreground fill. Recall: signature fill.
          Source: bar-right-portal.tsx
        </p>
      </DemoCard>

      {/* ── Toggle Switches ── */}
      <DemoCard label="Toggle switch — two variants (neutral default, intelligence opt-in)">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
              Neutral (Account / Teams / Appearance / everywhere else)
            </p>
            <div className="max-w-80 divide-y divide-border/20">
              <ToggleSwitch
                label="Allow topic management"
                description="Contributors can create and organize topics"
                defaultOn
              />
              <ToggleSwitch
                label="Match time of day"
                description="Aurora shifts through 6 time buckets"
              />
            </div>
          </div>
          <div>
            <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
              Intelligence (Intelligence tab only — AI behavior)
            </p>
            <div className="max-w-80 divide-y divide-border/20">
              <ToggleSwitch
                label="Memory Dreams"
                description="Run nightly consolidation"
                defaultOn
                variant="intelligence"
              />
              <ToggleSwitch
                label="Auto-merge duplicates"
                description="Merge notes with >90% similarity"
                variant="intelligence"
              />
            </div>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-3">
          Neutral: NEUTRAL_INK (var(--fg-1)) track when on, NEUTRAL_TRACK_OFF
          when off. Intelligence: var(--signature) track when on. Both: same
          geometry — w-10 h-6 rounded-full track, w-4.5 h-4.5 thumb.
        </p>
      </DemoCard>

      {/* ── Async Toggle ── */}
      <DemoCard label="Toggle — async (optimistic + pending)">
        <div className="max-w-80 divide-y divide-border/20">
          <ToggleSwitch
            label="Auto-synthesis"
            description="Skip Ask button for question queries"
            async
          />
          <ToggleSwitch
            label="Push notifications"
            description="Dream reports and team activity"
            async
            defaultOn
          />
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Optimistic: switch moves immediately. Pending: border pulses until
          server confirms (600ms simulated).
        </p>
      </DemoCard>

      {/* ── Radio rows — options with descriptions ── */}
      <DemoCard label="Radio row — option with sublabel (border-2)">
        <RadioRowDemo />
        <p className="text-[10px] text-fg-4 font-mono mt-3">
          border-2 outer ring. Ring color = NEUTRAL_INK when selected,
          NEUTRAL_BORDER_OFF otherwise. Inner dot = NEUTRAL_INK. NO background
          fill on the ring — radios are border+dot, not filled pills. See real
          impl in SelectionOption (settings-dialog.tsx) and
          hub-permissions-section.tsx.
        </p>
      </DemoCard>

      {/* ── Pill picker — self-explanatory short labels ── */}
      <DemoCard label="Pill picker — self-explanatory short labels (Role Picker pattern)">
        <PillPickerDemo />
        <p className="text-[10px] text-fg-4 font-mono mt-3">
          flex gap-1.5 flex-wrap. Selected: bg-foreground text-background
          font-medium. Unselected: bg-surface-1 text-fg-2 hover:bg-surface-2.
          Use when option labels are self-explanatory (role names, plan tiers,
          filter chips). Canonical impl: hub-invites-section.tsx:204-217. NOT
          for options that need descriptions — use RadioRow above for those.
        </p>
      </DemoCard>

      {/* ── Input Fields ── */}
      <DemoCard label="Input field">
        <div className="max-w-sm space-y-3">
          <div>
            <label className="text-[12px] text-fg-3 mb-1 block">
              Default input
            </label>
            <input
              type="text"
              placeholder="Enter value..."
              className="w-full h-8 px-3 rounded-lg border border-border bg-background text-[14px] text-foreground placeholder:text-fg-3 outline-none focus:border-ring focus:ring-2 focus:ring-ring/50 transition-colors"
            />
          </div>
          <div>
            <label className="text-[12px] text-fg-3 mb-1 block">Disabled</label>
            <input
              type="text"
              placeholder="Disabled..."
              disabled
              className="w-full h-8 px-3 rounded-lg border border-border bg-background text-[14px] text-foreground placeholder:text-fg-3 outline-none opacity-50 cursor-not-allowed"
            />
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          h-8, rounded-lg, border-border, focus: border-ring + ring-2
          ring-ring/50
        </p>
      </DemoCard>

      {/* ── Pill Chips (mode toggle) ── */}
      <DemoCard label="Pill chips — mode toggle">
        <div className="flex items-center gap-4">
          {/* Desktop: text links */}
          <div className="flex items-center gap-0.5 text-[13px]">
            <span className="text-fg-1 font-medium">remember</span>
            <span className="text-fg-4 px-1">&middot;</span>
            <span className="text-fg-3 cursor-pointer hover:text-fg-2">
              recall
            </span>
          </div>
          {/* Mobile: pill chips */}
          <div className="flex items-center gap-1">
            <span className="px-3 py-1 rounded-full text-[13px] font-medium bg-foreground text-background">
              remember
            </span>
            <span className="px-3 py-1 rounded-full text-[13px] text-fg-3 cursor-pointer hover:bg-surface-2">
              recall
            </span>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Desktop: text &quot;remember · recall&quot; (dot separator). Mobile:
          pill chips with 44px min touch target. Source: layout.tsx mode toggle
        </p>
      </DemoCard>

      {/* ═══════════════════════════════════════════════════════════════
          Control color semantics — THE canonical rule for LLM agents
          ═══════════════════════════════════════════════════════════════ */}
      <DemoCard label="Control color semantics — when to use which color (canonical)">
        <p className="text-[12px] text-fg-2 mb-3">
          <span className="text-fg-1 font-medium">
            One rule rules them all:
          </span>{" "}
          purple is reserved for Intelligence, everything else uses neutral ink.
          This card is the single source of truth — if you&apos;re building a
          new control and wondering &ldquo;what color does active use?&rdquo;,
          the answer is here.
        </p>
        <div className="space-y-3 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">
                Purple (var(--signature)) = Intelligence tab ONLY.
              </span>{" "}
              Dreams, merge, archive, organize — AI behavior toggles. Using
              signature purple signals &ldquo;this is the AI&rdquo;. Nowhere
              else: not in Account, Teams, Appearance, Security, Dev. If a new
              setting is purple and it isn&apos;t AI behavior, it&apos;s wrong.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">
                NEUTRAL_INK = everywhere else.
              </span>{" "}
              <span className="font-mono">var(--fg-1)</span> = foreground at 0.9
              opacity = body-text ink weight. One active color unifies every
              non-Intelligence control: toggle track on, toggle border on, radio
              ring on, radio dot. Visible, authoritative, not pure black.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">
                Toggles are iOS-solid.
              </span>{" "}
              When on, the track is filled with the active color (ink or
              signature), border matches fill, thumb uses{" "}
              <span className="font-mono">NEUTRAL_INK_INVERSE</span>{" "}
              (var(--background)) for contrast. When off, transparent track,
              subtle border, muted thumb. Geometry: w-10 h-6 rounded-full track,
              w-4.5 h-4.5 thumb.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">
                Radios are border-2 + dot, never filled.
              </span>{" "}
              Outer ring uses <span className="font-mono">border-2</span> (2px),
              not <span className="font-mono">border</span> — 1px at 0.18
              opacity is invisible. Ring color = NEUTRAL_INK when selected,
              NEUTRAL_BORDER_OFF when not. Inner dot = NEUTRAL_INK. No
              background fill on the ring — that makes the radio look like a
              filled pill, which is wrong.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">
                Radio rows vs pills — the decision tree.
              </span>{" "}
              <span className="text-fg-1">Radio rows</span>: each option needs a
              description because the label alone isn&apos;t self-explanatory
              (&ldquo;Plain / Signature / Time&rdquo;, &ldquo;Delete policy:
              none / own / any&rdquo;). Show all descriptions at once — no
              tooltips. <span className="text-fg-1">Pills</span>: the labels are
              self-explanatory (Contributor / Viewer / Admin, Free / Pro /
              Team). No descriptions needed; density matters. Cargo-culting
              pills onto options that need descriptions is a common mistake —
              don&apos;t.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">Token source.</span>{" "}
              Import from{" "}
              <span className="font-mono">@memaxlabs/ui/tokens/controls</span>:{" "}
              <span className="font-mono">
                NEUTRAL_INK, NEUTRAL_INK_INVERSE, NEUTRAL_TRACK_OFF,
                NEUTRAL_BORDER_OFF, NEUTRAL_THUMB_OFF
              </span>
              . For the Intelligence variant, use{" "}
              <span className="font-mono">var(--signature)</span> directly.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p className="text-fg-2">
              <span className="text-fg-1 font-medium">
                Canonical production references.
              </span>{" "}
              Toggle:{" "}
              <span className="font-mono">settings-dialog.tsx ToggleRow</span> +{" "}
              <span className="font-mono">teams/shared.tsx ToggleRow</span>.
              Radio:{" "}
              <span className="font-mono">
                settings-dialog.tsx SelectionOption
              </span>{" "}
              + <span className="font-mono">hub-permissions-section.tsx</span>.
              Pills:{" "}
              <span className="font-mono">hub-invites-section.tsx:204-217</span>
              . All four match — they are the reference.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Composition: Settings Row ── */}
      <DemoCard label="Composition — settings row">
        <div
          className="max-w-md rounded-xl border border-border p-4 space-y-0"
          style={{ background: "var(--card)" }}
        >
          <div className="flex items-center justify-between py-2">
            <div>
              <span className="text-[14px] text-fg-1">Hub name</span>
              <p className="text-[12px] text-fg-3">
                Visible to all team members
              </p>
            </div>
            <Button variant="outline" size="sm">
              Edit
            </Button>
          </div>
          <div className="border-t border-border/20" />
          <div className="flex items-center justify-between py-2">
            <div>
              <span className="text-[14px] text-fg-1">Danger zone</span>
              <p className="text-[12px] text-fg-3">
                This action cannot be undone
              </p>
            </div>
            <Button variant="destructive" size="sm">
              <Trash2 data-icon="inline-start" className="size-3.5" />
              Delete hub
            </Button>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Pattern: label+description left, action button right. Separator:
          border-t border-border/20.
        </p>
      </DemoCard>

      {/* ── InfoPopover — section-header inline help ── */}
      <DemoCard label="InfoPopover — inline help (hover + click + tap + keyboard)">
        <div className="space-y-5">
          {/* Branded variant — brand voice education (uses MemaxStar brandMark) */}
          <div>
            <div className="mb-2 text-[11px] text-fg-4">
              Branded — brand voice education
            </div>
            <div className="flex items-center gap-2 rounded-lg bg-surface-1 px-3 py-2">
              <Hash className="h-3.5 w-3.5 shrink-0 text-fg-3" />
              <span className="text-[14px] font-semibold text-fg-2">
                Your Topics
              </span>
              <InfoPopover
                ariaLabel="How topics work"
                title="How topics work"
                body="memax dreams about your memories and groups them into topics. Drag any memory between topics to refit it anywhere."
                brandMark={
                  <span
                    aria-hidden
                    className="state-slow-breathe text-[14px] leading-none"
                    style={{ color: "var(--signature)" }}
                  >
                    ✦
                  </span>
                }
              />
            </div>
            <p className="mt-1.5 text-[10px] text-fg-4">
              Pass <code className="font-mono">brandMark</code> as a slot when
              the explanation references brand vocabulary (dream, remember,
              recall, forget) — the ✦ visual ties copy to brand symbol in one
              teaching moment.
            </p>
          </div>

          {/* Unbranded variant — utility help (no brand voice) */}
          <div>
            <div className="mb-2 text-[11px] text-fg-4">
              Unbranded — utility help (no brand voice)
            </div>
            <div className="flex items-center gap-2 rounded-lg bg-surface-1 px-3 py-2">
              <Hash className="h-3.5 w-3.5 shrink-0 text-fg-3" />
              <span className="text-[14px] font-semibold text-fg-2">
                API keys
              </span>
              <InfoPopover
                ariaLabel="How API keys work"
                title="How API keys work"
                body="Rotate keys anytime. Revoked keys stop working immediately and can't be restored."
              />
            </div>
            <p className="mt-1.5 text-[10px] text-fg-4">
              Skip <code className="font-mono">brandMark</code> for utility help
              (hub management, team settings, API docs).
            </p>
          </div>
        </div>

        {/* ── Behavior + defaults reference ── */}
        <div className="mt-4 space-y-0.5 border-t border-border/20 pt-3 text-[10px] text-fg-4 font-mono">
          <p>Hover (desktop): 150 ms open delay, 100 ms close delay</p>
          <p>Click / Enter / Space: instant open, click outside to close</p>
          <p>Tap (touch): instant open, tap outside to close</p>
          <p>Esc: close (auto-handled by @base-ui)</p>
          <p>Position: side=bottom, align=start, sideOffset=8, max-w=320px</p>
        </div>

        {/* ── Usage rules — when to pick this vs Tooltip vs Popover ── */}
        <div className="mt-3 space-y-1.5 text-[10px] text-fg-3 leading-[1.6]">
          <p className="font-medium text-fg-2">When to reach for this</p>
          <p>
            ✓ 1–3 sentence explanations of what a section does and how it works
            — section-header inline help.
          </p>
          <p>
            ✓ Brand voice teaching moments (use{" "}
            <code className="font-mono text-[9px]">brandMark</code> prop).
          </p>
          <p className="pt-1 font-medium text-fg-2">When NOT to use it</p>
          <p>
            ✗ Short hover-only labels (keyboard shortcuts, button names) — use{" "}
            <code className="font-mono text-[9px]">Tooltip</code>.
          </p>
          <p>
            ✗ Rich content with lists, buttons, links, long-form help — compose
            with <code className="font-mono text-[9px]">Popover</code> directly.
          </p>
          <p>
            ✗ Error messages or confirmations — use{" "}
            <code className="font-mono text-[9px]">Dialog</code> or the bar
            toast.
          </p>
        </div>

        <p className="mt-3 text-[10px] text-fg-4 font-mono">
          First consumer: packages/web/src/components/features/topic-card.tsx —
          &quot;Your Topics&quot; header.
        </p>
      </DemoCard>
    </Section>
  );
}
