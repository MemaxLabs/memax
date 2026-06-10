"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import * as adminClient from "@/lib/admin-client";
import { useLocale } from "@/i18n";
import type {
  PlanDefinition,
  AdminUserListParams,
  AdminUserListResponse,
  AdminUserDetail,
  AdminUserUsage,
  AdminConfig,
  SystemStats,
} from "@/lib/admin-client";

// --- Query Keys ---

const adminUsersKey = ["admin", "users"] as const;
const adminStatsKey = ["admin", "stats"] as const;
const adminPlansKey = ["admin", "plans"] as const;

// --- Queries ---

export function useAdminUsers(params: AdminUserListParams = {}) {
  return useQuery<AdminUserListResponse>({
    queryKey: [...adminUsersKey, params],
    queryFn: () => adminClient.listUsers(params),
    staleTime: 30 * 1000,
  });
}

export function useAdminUserDetail(userId: string) {
  return useQuery<AdminUserDetail>({
    queryKey: [...adminUsersKey, userId],
    queryFn: () => adminClient.getUser(userId),
    staleTime: 30 * 1000,
    enabled: !!userId,
  });
}

export function useAdminStats() {
  return useQuery<SystemStats>({
    queryKey: [...adminStatsKey],
    queryFn: () => adminClient.getStats(),
    staleTime: 60 * 1000,
  });
}

export function useAdminPlans() {
  return useQuery<PlanDefinition[]>({
    queryKey: [...adminPlansKey],
    queryFn: () => adminClient.listPlans(),
    staleTime: 60 * 1000,
  });
}

export function useAdminUserUsage(userId: string) {
  return useQuery<AdminUserUsage>({
    queryKey: [...adminUsersKey, userId, "usage"],
    queryFn: () => adminClient.getUserUsage(userId),
    // Usage is dynamic; short stale time keeps the admin view fresh
    // without spamming the endpoint on every tab switch.
    staleTime: 15 * 1000,
    enabled: !!userId,
  });
}

export function useAdminConfig() {
  return useQuery<AdminConfig>({
    queryKey: ["admin", "config"],
    queryFn: () => adminClient.getConfig(),
    // Static infra config — long stale time. TRUSTED_PROXY and
    // ORIGIN_SHARED_SECRET only change on deploy.
    staleTime: 5 * 60 * 1000,
  });
}

// --- Mutations ---

export function useSetUserPlan() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminSetUserPlan,
    },
    mutationFn: ({ userId, planId }: { userId: string; planId: string }) =>
      adminClient.setUserPlan(userId, planId),
    onSuccess: (_data, { userId }) => {
      qc.invalidateQueries({ queryKey: adminUsersKey });
      qc.invalidateQueries({ queryKey: [...adminUsersKey, userId] });
      qc.invalidateQueries({ queryKey: adminStatsKey });
    },
  });
}

export function useSetUserOverrides() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminSetOverrides,
    },
    mutationFn: ({
      userId,
      overrides,
      reason,
    }: {
      userId: string;
      overrides: Record<string, unknown>;
      reason: string;
    }) => adminClient.setUserOverrides(userId, overrides, reason),
    onSuccess: (_data, { userId }) => {
      qc.invalidateQueries({ queryKey: [...adminUsersKey, userId] });
    },
  });
}

export function useDeleteUserOverrides() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminDeleteOverrides,
    },
    mutationFn: (userId: string) => adminClient.deleteUserOverrides(userId),
    onSuccess: (_data, userId) => {
      qc.invalidateQueries({ queryKey: [...adminUsersKey, userId] });
    },
  });
}

export function useUpdatePlan() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminUpdatePlan,
    },
    mutationFn: ({
      planId,
      patch,
    }: {
      planId: string;
      patch: Partial<PlanDefinition>;
    }) => adminClient.updatePlan(planId, patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminPlansKey });
      qc.invalidateQueries({ queryKey: adminUsersKey });
    },
  });
}
