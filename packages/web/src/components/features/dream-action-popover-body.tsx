"use client";

// ═══════════════════════════════════════════════
// DreamActionPopoverBody (web adapter).
//
// The pure presentation primitive lives in @memaxlabs/ui; this file
// wires i18n strings through from the locale and forwards the SDK's
// DreamActionRef (structurally compatible with the ui primitive's
// DreamActionDisplayRef view model).
//
// Kitchen imports the @memaxlabs/ui primitive directly with hardcoded
// strings, proving exact visual parity with production.
// ═══════════════════════════════════════════════

import type { DreamActionRef } from "memax-sdk";
import { DreamActionPopoverBody as UIDreamActionPopoverBody } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";

export function DreamActionPopoverBody({ action }: { action: DreamActionRef }) {
  const { t } = useLocale();
  return (
    <UIDreamActionPopoverBody
      action={action}
      headerLabel={t.lifecycle.popoverHeader[action.action_type]}
      unassignedLabel={t.lifecycle.unassigned}
      verbFallbackLabel={t.lifecycle.dreamActionVerb[action.action_type]}
    />
  );
}
