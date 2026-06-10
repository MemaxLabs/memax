"use client";

import { useState } from "react";
import { Button } from "@memaxlabs/ui";
import {
  Send,
  CalendarClock,
  X,
  Trash2,
  Pencil,
  Mail,
  Check,
} from "lucide-react";
import { useLocale } from "@/i18n";
import type { AdminCampaign } from "@/lib/admin-client";

export function CampaignLifecycleActions({
  campaign,
  onSendNow,
  onSchedule,
  onCancel,
  onDelete,
  onEdit,
  onTestSend,
  pending,
}: {
  campaign: AdminCampaign;
  onSendNow: () => void;
  onSchedule: (scheduledAt: string) => void;
  onCancel: () => void;
  onDelete: () => void;
  onEdit?: () => void;
  // onTestSend is only meaningful for channel=email draft campaigns.
  // The parent wires the mutation; this component just collects the
  // address and signals success/error via the returned promise.
  onTestSend?: (to: string) => Promise<void>;
  pending: boolean;
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.detail;
  const [confirm, setConfirm] = useState<"send" | "cancel" | "delete" | null>(
    null,
  );
  const [showSchedule, setShowSchedule] = useState(false);
  const [scheduleInput, setScheduleInput] = useState("");
  const [showTestSend, setShowTestSend] = useState(false);
  const [testSendEmail, setTestSendEmail] = useState("");
  const [testSendPending, setTestSendPending] = useState(false);
  const [testSendResult, setTestSendResult] = useState<
    { kind: "success"; to: string } | { kind: "error"; message: string } | null
  >(null);

  const canTestSend = Boolean(onTestSend) && campaign.channel === "email";
  const status = campaign.status;

  if (status === "sending") {
    return (
      <div className="rounded-lg border border-amber-500/30 bg-amber-500/8 px-3 py-2 text-[13px] text-amber-700 dark:text-amber-300">
        {copy.states.sending}
      </div>
    );
  }

  if (status === "sent" || status === "failed") {
    return <p className="text-[13px] text-fg-2">{copy.states.terminal}</p>;
  }

  return (
    <div className="space-y-3">
      {status === "draft" ? (
        <div className="flex flex-wrap gap-2">
          {onEdit ? (
            <Button
              variant="outline"
              onClick={onEdit}
              disabled={pending}
              className="cursor-pointer"
            >
              <Pencil className="mr-1 h-3.5 w-3.5" />
              {copy.actions.edit}
            </Button>
          ) : null}
          {canTestSend ? (
            <Button
              variant="outline"
              onClick={() => {
                setShowTestSend((v) => !v);
                setTestSendResult(null);
              }}
              disabled={pending || testSendPending}
              className="cursor-pointer"
            >
              <Mail className="mr-1 h-3.5 w-3.5" />
              {copy.actions.testSend}
            </Button>
          ) : null}
          <Button
            onClick={() => setConfirm("send")}
            disabled={pending}
            className="cursor-pointer"
          >
            <Send className="mr-1 h-3.5 w-3.5" />
            {copy.actions.send}
          </Button>
          <Button
            variant="outline"
            onClick={() => setShowSchedule(true)}
            disabled={pending}
            className="cursor-pointer"
          >
            <CalendarClock className="mr-1 h-3.5 w-3.5" />
            {copy.actions.schedule}
          </Button>
          <Button
            variant="outline"
            onClick={() => setConfirm("delete")}
            disabled={pending}
            className="cursor-pointer"
          >
            <Trash2 className="mr-1 h-3.5 w-3.5" />
            {copy.actions.delete}
          </Button>
        </div>
      ) : null}

      {showTestSend && canTestSend && onTestSend ? (
        <div className="rounded-surface border border-border/60 bg-surface-1 p-4">
          <p className="text-[13px] font-medium text-fg-1">
            {copy.testSend.title}
          </p>
          <p className="mt-0.5 text-[12.5px] text-fg-2">
            {copy.testSend.description}
          </p>
          <form
            className="mt-3 flex gap-2"
            onSubmit={async (event) => {
              event.preventDefault();
              const trimmed = testSendEmail.trim();
              if (!trimmed || !trimmed.includes("@")) {
                setTestSendResult({
                  kind: "error",
                  message: copy.testSend.invalidEmail,
                });
                return;
              }
              setTestSendPending(true);
              setTestSendResult(null);
              try {
                await onTestSend(trimmed);
                setTestSendResult({ kind: "success", to: trimmed });
              } catch (err) {
                setTestSendResult({
                  kind: "error",
                  message: err instanceof Error ? err.message : String(err),
                });
              } finally {
                setTestSendPending(false);
              }
            }}
          >
            <input
              type="email"
              value={testSendEmail}
              onChange={(e) => setTestSendEmail(e.target.value)}
              placeholder={copy.testSend.placeholder}
              className="flex-1 rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
              required
              disabled={testSendPending}
            />
            <Button
              type="submit"
              disabled={testSendPending || pending}
              className="cursor-pointer"
            >
              {testSendPending ? copy.testSend.sending : copy.testSend.submit}
            </Button>
          </form>
          {testSendResult?.kind === "success" ? (
            <p className="mt-2 inline-flex items-center gap-1.5 text-[12.5px] text-emerald-700 dark:text-emerald-400">
              <Check className="h-3 w-3" />
              {copy.testSend.successPrefix} {testSendResult.to}
            </p>
          ) : null}
          {testSendResult?.kind === "error" ? (
            <p className="mt-2 text-[12.5px] text-destructive">
              {testSendResult.message}
            </p>
          ) : null}
        </div>
      ) : null}

      {status === "scheduled" ? (
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => setConfirm("cancel")}
            disabled={pending}
            className="cursor-pointer"
          >
            <X className="mr-1 h-3.5 w-3.5" />
            {copy.actions.cancel}
          </Button>
        </div>
      ) : null}

      {status === "cancelled" ? (
        <Button
          variant="outline"
          onClick={() => setConfirm("delete")}
          disabled={pending}
          className="cursor-pointer"
        >
          <Trash2 className="mr-1 h-3.5 w-3.5" />
          {copy.actions.delete}
        </Button>
      ) : null}

      {showSchedule ? (
        <div className="rounded-surface border border-border/60 bg-surface-1 p-4">
          <p className="text-[13px] font-medium text-fg-1">
            {copy.schedule.title}
          </p>
          <p className="mt-0.5 text-[12.5px] text-fg-2">
            {copy.schedule.description}
          </p>
          <input
            type="datetime-local"
            value={scheduleInput}
            onChange={(e) => setScheduleInput(e.target.value)}
            className="mt-3 w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
          />
          <div className="mt-3 flex gap-2">
            <Button
              onClick={() => {
                if (!scheduleInput) return;
                onSchedule(new Date(scheduleInput).toISOString());
                setShowSchedule(false);
                setScheduleInput("");
              }}
              disabled={pending || !scheduleInput}
              className="cursor-pointer"
            >
              {copy.schedule.submit}
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setShowSchedule(false);
                setScheduleInput("");
              }}
              disabled={pending}
              className="cursor-pointer"
            >
              {copy.schedule.cancel}
            </Button>
          </div>
        </div>
      ) : null}

      {confirm ? (
        <ConfirmBlock
          message={copy.confirm[confirm]}
          confirmLabel={copy.actions[confirm]}
          // Broadcast sends (audience rule_type=all) require typing the
          // literal string "SEND" to confirm — thumb-slip protection.
          typedConfirmPhrase={
            confirm === "send" && isBroadcast(campaign)
              ? copy.confirm.typedPhrase
              : null
          }
          onConfirm={() => {
            if (confirm === "send") onSendNow();
            if (confirm === "cancel") onCancel();
            if (confirm === "delete") onDelete();
            setConfirm(null);
          }}
          onDismiss={() => setConfirm(null)}
          pending={pending}
        />
      ) : null}
    </div>
  );
}

function isBroadcast(campaign: AdminCampaign): boolean {
  return campaign.audience_snapshot?.rule_type === "all";
}

function ConfirmBlock({
  message,
  confirmLabel,
  typedConfirmPhrase,
  onConfirm,
  onDismiss,
  pending,
}: {
  message: string;
  confirmLabel: string;
  typedConfirmPhrase: string | null;
  onConfirm: () => void;
  onDismiss: () => void;
  pending: boolean;
}) {
  const { t } = useLocale();
  const [typed, setTyped] = useState("");
  const requiresTyping = typedConfirmPhrase !== null;
  const canConfirm = !requiresTyping || typed.trim() === typedConfirmPhrase;

  return (
    <div className="rounded-surface border border-amber-500/30 bg-amber-500/8 p-4">
      <p className="text-[13px] text-amber-700 dark:text-amber-300">
        {message}
      </p>
      {requiresTyping ? (
        <div className="mt-3">
          <label className="block text-[12px] font-medium text-amber-800 dark:text-amber-200">
            {t.admin.communications.campaigns.detail.confirm.typedPrompt.replace(
              "{phrase}",
              typedConfirmPhrase,
            )}
          </label>
          <input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={typedConfirmPhrase}
            className="mt-1 w-full rounded-lg border border-amber-500/40 bg-background px-3 py-2 font-mono text-sm text-fg-1 placeholder:text-fg-3 focus:border-amber-500/60 focus:outline-none focus:ring-2 focus:ring-amber-500/20"
          />
        </div>
      ) : null}
      <div className="mt-3 flex gap-2">
        <Button
          onClick={onConfirm}
          disabled={pending || !canConfirm}
          className="cursor-pointer"
        >
          {confirmLabel}
        </Button>
        <Button
          variant="outline"
          onClick={onDismiss}
          disabled={pending}
          className="cursor-pointer"
        >
          {t.admin.communications.campaigns.form.cancel}
        </Button>
      </div>
    </div>
  );
}
