"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Persona, PersonaApplyRequest } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale } from "@/i18n";

const PERSONAS_QUERY_KEY = ["personas"] as const;

/**
 * Personas (Beta) — identity objects derived server-side from synced
 * identity configs (SOUL.md etc.). Read-only list; `useApplyPersona`
 * writes a persona into a target agent's identity config through the
 * config-sync machinery.
 */
export function usePersonas() {
  return useQuery<Persona[]>({
    queryKey: PERSONAS_QUERY_KEY,
    queryFn: async () => {
      const res = await getMemaxClient().personas.list();
      return res.personas ?? [];
    },
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

export function useApplyPersona() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.applyPersona,
      // No successMessage: success feedback is inline in the persona card
      // (staged-for-sync message) — a global toast would double-announce it.
    },
    mutationFn: ({ id, input }: { id: string; input: PersonaApplyRequest }) =>
      getMemaxClient().personas.apply(id, input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: PERSONAS_QUERY_KEY });
      qc.invalidateQueries({ queryKey: ["agent-configs"] });
      qc.invalidateQueries({ queryKey: ["connected-agents"] });
    },
  });
}
