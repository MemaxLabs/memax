"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPatch, apiPost, apiDelete } from "@/lib/api";
import { useLocale } from "@/i18n";

// --- Types ---

export interface WaitlistEntry {
  id: string;
  email: string;
  use_case: string;
  ai_tools: string[];
  role: string;
  status: string;
  wave: number | null;
  notes: string;
  approved_by: string | null;
  approved_at: string | null;
  created_at: string;
  updated_at: string;
  invite_status?: string | null; // active, used, expired
  invite_used_at?: string | null;
  invite_used_by?: string | null;
  invite_used_by_email?: string | null;
  invite_used_by_name?: string | null;
}

export interface WaitlistStats {
  total: number;
  pending: number;
  approved: number;
  registered: number;
  rejected: number;
  capacity_cap: number;
  capacity_used: number;
  waves: Array<{
    wave: number;
    approved: number;
    registered: number;
    started_at: string | null;
  }>;
}

interface WaitlistListResponse {
  entries: WaitlistEntry[];
  next_cursor: string;
  has_more: boolean;
  total_count: number;
}

interface WaitlistListParams {
  status?: string;
  use_case?: string;
  q?: string;
  sort?: string;
  order?: string;
  limit?: number;
  cursor?: string;
}

// --- Query Keys ---

const adminWaitlistKey = ["admin", "waitlist"] as const;
const adminWaitlistStatsKey = ["admin", "waitlist", "stats"] as const;

// --- Queries ---

export function useAdminWaitlist(params: WaitlistListParams = {}) {
  const searchParams = new URLSearchParams();
  if (params.status) searchParams.set("status", params.status);
  if (params.use_case) searchParams.set("use_case", params.use_case);
  if (params.q) searchParams.set("q", params.q);
  if (params.sort) searchParams.set("sort", params.sort);
  if (params.order) searchParams.set("order", params.order);
  if (params.limit) searchParams.set("limit", String(params.limit));
  if (params.cursor) searchParams.set("cursor", params.cursor);

  const qs = searchParams.toString();
  const path = `/v1/admin/waitlist${qs ? `?${qs}` : ""}`;

  return useQuery<WaitlistListResponse>({
    queryKey: [...adminWaitlistKey, params],
    queryFn: () => apiGet<WaitlistListResponse>(path),
    staleTime: 30 * 1000,
  });
}

export function useAdminWaitlistStats() {
  return useQuery<WaitlistStats>({
    queryKey: [...adminWaitlistStatsKey],
    queryFn: () => apiGet<WaitlistStats>("/v1/admin/waitlist/stats"),
    staleTime: 30 * 1000,
  });
}

// --- Mutations ---

export function useApproveWaitlistEntry() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminApprove,
    },
    mutationFn: ({ id, wave }: { id: string; wave?: number }) =>
      apiPatch<WaitlistEntry>(`/v1/admin/waitlist/${id}`, {
        status: "approved",
        wave,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminWaitlistKey });
      qc.invalidateQueries({ queryKey: adminWaitlistStatsKey });
    },
  });
}

export function useRejectWaitlistEntry() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminReject,
    },
    mutationFn: ({ id, notes }: { id: string; notes?: string }) =>
      apiPatch<WaitlistEntry>(`/v1/admin/waitlist/${id}`, {
        status: "rejected",
        notes,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminWaitlistKey });
      qc.invalidateQueries({ queryKey: adminWaitlistStatsKey });
    },
  });
}

export function useRestoreWaitlistEntry() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminRestore,
    },
    mutationFn: (id: string) =>
      apiPatch<WaitlistEntry>(`/v1/admin/waitlist/${id}`, {
        status: "pending",
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminWaitlistKey });
      qc.invalidateQueries({ queryKey: adminWaitlistStatsKey });
    },
  });
}

interface BatchApproveResponse {
  approved_count: number;
  invite_count: number;
  failed: string[];
}

export function useBatchApproveWaitlist() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminBatchApprove,
    },
    mutationFn: ({
      ids,
      wave,
      notes,
    }: {
      ids: string[];
      wave?: number;
      notes?: string;
    }) =>
      apiPost<BatchApproveResponse>("/v1/admin/waitlist/batch-approve", {
        ids,
        wave: wave ?? 1,
        notes,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminWaitlistKey });
      qc.invalidateQueries({ queryKey: adminWaitlistStatsKey });
    },
  });
}

export function useInviteByEmail() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminInvite,
    },
    mutationFn: ({
      email,
      wave,
      notes,
    }: {
      email: string;
      wave?: number;
      notes?: string;
    }) =>
      apiPost<{ status: string; email: string }>("/v1/admin/waitlist/invite", {
        email,
        wave,
        notes,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminWaitlistKey });
      qc.invalidateQueries({ queryKey: adminWaitlistStatsKey });
    },
  });
}

export function useRevokeInvite() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminRevokeInvite,
    },
    mutationFn: (entryId: string) =>
      apiDelete(`/v1/admin/waitlist/${entryId}/invite`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminWaitlistKey });
      qc.invalidateQueries({ queryKey: adminWaitlistStatsKey });
    },
  });
}
