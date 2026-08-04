"use client";

import { Fingerprint } from "lucide-react";
import type { ChatSession } from "memax-sdk";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { usePersonas } from "@/hooks/use-personas";
import { useSettings } from "@/hooks/use-settings";
import { usePatchChatSession } from "@/hooks/use-chat";

/**
 * Persona switcher for the active chat session (Beta). Three-way binding
 * mirroring the server semantics: inherit the account default ("") /
 * explicitly none ("none") / a specific persona id. Patching persona is
 * allowed mid-session — it only shapes FUTURE turns' system prompts.
 * Hidden entirely while the user has no personas.
 */
export function ChatPersonaPicker({ session }: { session: ChatSession }) {
  const { t } = useLocale();
  const { data: personas } = usePersonas();
  const { data: settings } = useSettings();
  const patchSession = usePatchChatSession(session.id);

  if (!personas || personas.length === 0) return null;

  const defaultPersonaId = settings?.chat_default_persona_id ?? "";
  const defaultPersona = personas.find((p) => p.id === defaultPersonaId);

  // "" = inherit account default — label shows what that resolves to.
  const inheritLabel = defaultPersona
    ? `${t.personas.pickerInherit} (${defaultPersona.name})`
    : `${t.personas.pickerInherit} (${t.personas.pickerNone})`;

  const items: Record<string, string> = {
    "": inheritLabel,
    none: t.personas.pickerNone,
  };
  for (const p of personas) {
    items[p.id] = p.name;
  }

  return (
    <div className="flex items-center gap-1.5 pb-1.5">
      <Fingerprint className="h-3 w-3 text-fg-4 shrink-0" aria-hidden />
      <span className="text-[11px] text-fg-4">{t.personas.pickerLabel}</span>
      <span className="text-[10px] font-medium uppercase tracking-wide text-fg-4 bg-surface-2 px-1 py-px rounded">
        {t.personas.beta}
      </span>
      <Select
        value={session.persona_id ?? ""}
        onValueChange={(v: string) => patchSession.mutate({ personaId: v })}
        items={items}
      >
        <SelectTrigger
          aria-label={t.personas.pickerLabel}
          className="h-6 rounded-chrome bg-transparent border-none px-1.5 text-[12px] text-fg-3 hover:text-fg-1"
        />
        <SelectContent>
          <SelectItem value="">{inheritLabel}</SelectItem>
          <SelectItem value="none">{t.personas.pickerNone}</SelectItem>
          {personas.map((p) => (
            <SelectItem key={p.id} value={p.id}>
              {p.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
