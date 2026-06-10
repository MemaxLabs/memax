"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocale } from "@/i18n";
import * as adminClient from "@/lib/admin-client";
import type {
  AdminEmailTemplateDetail,
  AdminEmailTemplatePreview,
  AdminEmailTemplateSendResult,
} from "@/lib/admin-client";

const adminEmailTemplatesKey = ["admin", "email", "templates"] as const;

export function useAdminEmailTemplates() {
  return useQuery({
    queryKey: adminEmailTemplatesKey,
    queryFn: () => adminClient.listEmailTemplates(),
    staleTime: 30_000,
  });
}

export function useAdminEmailTemplate(name?: string) {
  return useQuery<AdminEmailTemplateDetail>({
    queryKey: [...adminEmailTemplatesKey, name],
    queryFn: () => adminClient.getEmailTemplate(name as string),
    enabled: Boolean(name),
    staleTime: 30_000,
  });
}

export function usePreviewAdminEmailTemplate(name?: string) {
  const { t } = useLocale();
  return useMutation<
    AdminEmailTemplatePreview,
    Error,
    {
      subject?: string;
      html?: string;
      text?: string;
      sample_data?: Record<string, string>;
    }
  >({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminLoadEmailTemplate,
    },
    mutationFn: (params) =>
      adminClient.previewEmailTemplate(name as string, params),
  });
}

export function useUpdateAdminEmailTemplate(name?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    AdminEmailTemplateDetail,
    Error,
    {
      subject: string;
      html: string;
      text: string;
      notes?: string;
      editor_kind?: string;
      editor_state?: Record<string, unknown>;
    }
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSaveEmailTemplate,
    },
    mutationFn: (params) =>
      adminClient.updateEmailTemplate(name as string, params),
    onSuccess: (detail) => {
      qc.setQueryData([...adminEmailTemplatesKey, name], detail);
      qc.invalidateQueries({ queryKey: adminEmailTemplatesKey });
    },
  });
}

export function useResetAdminEmailTemplate(name?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminResetEmailTemplate,
    },
    mutationFn: () => adminClient.resetEmailTemplate(name as string),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [...adminEmailTemplatesKey, name] });
      qc.invalidateQueries({ queryKey: adminEmailTemplatesKey });
    },
  });
}

export function useSendAdminEmailTemplate(name?: string) {
  const { t } = useLocale();
  return useMutation<
    AdminEmailTemplateSendResult,
    Error,
    {
      to: string;
      subject: string;
      html: string;
      text: string;
      sample_data?: Record<string, string>;
    }
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSendEmail,
    },
    mutationFn: (params) =>
      adminClient.sendEmailTemplate(name as string, params),
  });
}

export function usePublishAdminEmailTemplate(name?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminEmailTemplateDetail, Error, void>({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminPublishEmailTemplate,
    },
    mutationFn: () => adminClient.publishEmailTemplate(name as string),
    onSuccess: (detail) => {
      qc.setQueryData([...adminEmailTemplatesKey, name], detail);
      qc.invalidateQueries({ queryKey: adminEmailTemplatesKey });
    },
  });
}
