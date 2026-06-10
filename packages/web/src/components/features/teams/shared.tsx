"use client";

import { Surface } from "@memaxlabs/ui";
import {
  NEUTRAL_BORDER_OFF,
  NEUTRAL_INK,
  NEUTRAL_INK_INVERSE,
  NEUTRAL_THUMB_OFF,
  NEUTRAL_TRACK_OFF,
} from "@memaxlabs/ui/tokens/controls";
import type { ContributorDeletePolicy, HubMember } from "memax-sdk";

/** @deprecated Use NEUTRAL_INK from @memaxlabs/ui/tokens/controls instead.
 *  Kept as an alias so `hub-permissions-section.tsx` continues to compile. */
export const TEAM_ACCENT = NEUTRAL_INK;

export type HubRole = HubMember["role"];

export const ROLE_ORDER: HubRole[] = [
  "owner",
  "admin",
  "contributor",
  "viewer",
];
export const INVITE_ROLE_OPTIONS: Exclude<HubRole, "owner">[] = [
  "contributor",
  "viewer",
  "admin",
];
export const MANAGED_ROLE_OPTIONS: Exclude<HubRole, "owner">[] = [
  "admin",
  "contributor",
  "viewer",
];

export const ROLE_COLORS: Record<HubRole, string> = {
  owner: "text-fg-2 bg-surface-3",
  admin: "text-fg-2 bg-surface-2",
  contributor: "text-fg-3 bg-surface-2",
  viewer: "text-fg-3 bg-surface-2",
};

export const DELETE_POLICY_OPTIONS: {
  value: ContributorDeletePolicy;
  labelKey: "deletePolicyNone" | "deletePolicyOwn" | "deletePolicyAny";
  descKey:
    | "deletePolicyNoneDesc"
    | "deletePolicyOwnDesc"
    | "deletePolicyAnyDesc";
}[] = [
  {
    value: "none",
    labelKey: "deletePolicyNone",
    descKey: "deletePolicyNoneDesc",
  },
  {
    value: "own",
    labelKey: "deletePolicyOwn",
    descKey: "deletePolicyOwnDesc",
  },
  {
    value: "any",
    labelKey: "deletePolicyAny",
    descKey: "deletePolicyAnyDesc",
  },
];

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function isExpired(iso: string): boolean {
  return new Date(iso).getTime() < Date.now();
}

export function Section({
  title,
  children,
}: {
  title?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="mb-8">
      {title && (
        <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
          {title}
        </p>
      )}
      <Surface variant="subtle" rounded="2xl" className="px-5 py-4">
        {children}
      </Surface>
    </section>
  );
}

export function ToggleRow({
  label,
  sublabel,
  checked,
  onToggle,
}: {
  label: string;
  sublabel?: string;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      onClick={onToggle}
      className="w-full flex items-center gap-3 px-1 py-3 rounded-lg text-left hover:bg-surface-1 transition-colors cursor-pointer group"
    >
      <div className="flex-1 min-w-0">
        <span className="text-[14px] text-fg-2 group-hover:text-fg-1 transition-colors">
          {label}
        </span>
        {sublabel && <p className="text-[12px] text-fg-3 mt-0.5">{sublabel}</p>}
      </div>
      <div
        className="relative w-10 h-6 rounded-full border transition-colors duration-200 shrink-0"
        style={{
          backgroundColor: checked ? NEUTRAL_INK : NEUTRAL_TRACK_OFF,
          borderColor: checked ? NEUTRAL_INK : NEUTRAL_BORDER_OFF,
        }}
      >
        <div
          className="absolute top-0.5 w-4.5 h-4.5 rounded-full transition-transform duration-200"
          style={{
            transform: checked ? "translateX(18px)" : "translateX(2px)",
            backgroundColor: checked ? NEUTRAL_INK_INVERSE : NEUTRAL_THUMB_OFF,
            opacity: 1,
          }}
        />
      </div>
    </button>
  );
}
