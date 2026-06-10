"use client";

import { useState } from "react";
import {
  Check,
  Copy,
  Link2,
  Mail,
  RefreshCw,
  Send,
  Shield,
} from "lucide-react";
import { Surface } from "@memaxlabs/ui";
import type { Translations } from "@/i18n/locales/en";
import {
  INVITE_ROLE_OPTIONS,
  formatDate,
  type HubRole,
} from "@/components/features/teams/shared";
import {
  useCreateInvite,
  useRegenerateInvite,
  useResendInvite,
  useRevokeInvite,
} from "@/hooks/use-hub-management";
import { useDestructiveAction } from "@/hooks/use-destructive-action";
import { MemaxError, type HubInvite } from "memax-sdk";
import { isValidEmail } from "@/lib/email-validation";

export function HubInvitesSection({
  hubId,
  invites,
  t,
  interpolate,
  onCreateInvite,
  onRevokeInvite,
  onRegenerateInvite,
}: {
  hubId: string;
  invites: HubInvite[];
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
  onCreateInvite: ReturnType<typeof useCreateInvite>;
  onRevokeInvite: ({
    hubId,
    inviteId,
  }: {
    hubId: string;
    inviteId: string;
  }) => Promise<unknown>;
  onRegenerateInvite: ReturnType<typeof useRegenerateInvite>;
}) {
  const destructiveAction = useDestructiveAction<string>();
  const [regeneratingId, setRegeneratingId] = useState<string | null>(null);
  const resendInvite = useResendInvite();

  return (
    <section className="mb-8">
      <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
        {t.hubs.inviteLink}
      </p>

      {invites.length > 0 && (
        <Surface
          variant="subtle"
          rounded="2xl"
          className="overflow-hidden mb-3"
        >
          {invites.map((invite, index) => (
            <InviteRow
              key={invite.id}
              index={index}
              hubId={hubId}
              invite={invite}
              isRevoking={destructiveAction.isConfirming(`revoke:${invite.id}`)}
              isRevokingPending={destructiveAction.isRunning(
                `revoke:${invite.id}`,
              )}
              isRegenerating={
                regeneratingId === invite.id && onRegenerateInvite.isPending
              }
              resendInvite={resendInvite}
              onStartRevoke={() =>
                destructiveAction.start(`revoke:${invite.id}`)
              }
              onCancelRevoke={() =>
                destructiveAction.cancel(`revoke:${invite.id}`)
              }
              onConfirmRevoke={() => {
                return destructiveAction.run(`revoke:${invite.id}`, () =>
                  onRevokeInvite({ hubId, inviteId: invite.id }).then(
                    () => undefined,
                  ),
                );
              }}
              onRegenerate={() => {
                setRegeneratingId(invite.id);
                onRegenerateInvite.mutate(
                  { hubId, inviteId: invite.id },
                  { onSettled: () => setRegeneratingId(null) },
                );
              }}
              t={t}
              interpolate={interpolate}
            />
          ))}
        </Surface>
      )}

      <InviteCreateButton hubId={hubId} onCreateInvite={onCreateInvite} t={t} />
    </section>
  );
}

function InviteRow({
  index,
  hubId,
  invite,
  isRevoking,
  isRevokingPending,
  isRegenerating,
  resendInvite,
  onStartRevoke,
  onCancelRevoke,
  onConfirmRevoke,
  onRegenerate,
  t,
  interpolate,
}: {
  index: number;
  hubId: string;
  invite: HubInvite;
  isRevoking: boolean;
  isRevokingPending: boolean;
  isRegenerating: boolean;
  resendInvite: ReturnType<typeof useResendInvite>;
  onStartRevoke: () => void;
  onCancelRevoke: () => void;
  onConfirmRevoke: () => Promise<boolean>;
  onRegenerate: () => void;
  t: Translations;
  interpolate: (s: string, vars: Record<string, string | number>) => string;
}) {
  const [copied, setCopied] = useState(false);
  const origin = typeof window !== "undefined" ? window.location.origin : "";
  const inviteUrl = `${origin}/invite/${invite.token}`;
  const isEmailInvite = !!invite.invitee_email;
  const isResending =
    resendInvite.isPending && resendInvite.variables?.inviteId === invite.id;
  const dividerClass = index > 0 ? "border-t border-border/30" : "";

  const copy = () => {
    navigator.clipboard.writeText(inviteUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  if (isRevoking) {
    return (
      <div
        className={`flex flex-col gap-2 px-4 py-3 transition-colors sm:flex-row sm:items-center sm:justify-between ${dividerClass}`}
        style={{
          backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
        }}
      >
        <span className="text-[13px] text-fg-2">
          {t.hubs.revokeInviteConfirm}
        </span>
        <div className="flex items-center gap-2.5 shrink-0">
          <button
            onClick={() => void onConfirmRevoke()}
            disabled={isRevokingPending}
            className="text-[13px] text-destructive/60 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
          >
            {isRevokingPending ? t.hubs.revoking : t.hubs.revoke}
          </button>
          <button
            onClick={onCancelRevoke}
            disabled={isRevokingPending}
            className="text-[13px] text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
          >
            {t.forget.keep}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className={`px-4 py-3 ${dividerClass}`}>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:gap-3">
        {/* Left: recipient + meta chips. min-w-0 so long emails/urls can truncate
            instead of pushing the action column off-screen. */}
        <div className="min-w-0 flex-1">
          {isEmailInvite ? (
            <div className="flex items-center gap-2 min-w-0">
              <Mail className="h-3.5 w-3.5 text-fg-3 shrink-0" />
              <span className="text-[13px] text-fg-1 truncate flex-1">
                {invite.invitee_email}
              </span>
              <button
                onClick={copy}
                title={t.hubs.copyLink}
                className="text-fg-4 hover:text-fg-2 transition-colors cursor-pointer p-1 shrink-0"
              >
                {copied ? (
                  <Check className="h-3 w-3 text-emerald-500/70" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-2 min-w-0">
              <code className="text-[13px] text-fg-2 bg-surface-2 px-2.5 py-1.5 rounded-chrome flex-1 min-w-0 truncate font-mono">
                {inviteUrl}
              </code>
              <button
                onClick={copy}
                className="text-fg-3 hover:text-fg-2 transition-colors cursor-pointer p-1.5 shrink-0"
              >
                {copied ? (
                  <Check className="h-3.5 w-3.5 text-emerald-500/70" />
                ) : (
                  <Copy className="h-3.5 w-3.5" />
                )}
              </button>
            </div>
          )}

          {/* Meta chips — flex-wrap container. Each "separator + chip" is
              bound into one flex item so the `·` can't orphan at the start
              or end of a wrapped line. First chip has no leading separator. */}
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 mt-1.5 text-[12px] text-fg-3">
            <span className="flex items-center gap-1 whitespace-nowrap">
              <Shield className="h-2.5 w-2.5" />
              {(t.hubs as Record<string, string>)[invite.role] ?? invite.role}
            </span>
            {isEmailInvite && invite.email_enqueued_at ? (
              <span className="flex items-center gap-2 whitespace-nowrap">
                <span className="text-fg-4">·</span>
                <span className="flex items-center gap-1">
                  <Send className="h-2.5 w-2.5" />
                  {interpolate(t.hubs.emailQueuedAt, {
                    date: formatDate(invite.email_enqueued_at),
                  })}
                </span>
              </span>
            ) : !isEmailInvite ? (
              <span className="flex items-center gap-2 whitespace-nowrap">
                <span className="text-fg-4">·</span>
                <span className="flex items-center gap-1 text-fg-4">
                  <Link2 className="h-2.5 w-2.5" />
                  {t.hubs.anyoneWithLink}
                </span>
              </span>
            ) : null}
            <span className="flex items-center gap-2 whitespace-nowrap">
              <span className="text-fg-4">·</span>
              <span>
                {interpolate(t.hubs.expiresOn, {
                  date: formatDate(invite.expires_at),
                })}
              </span>
            </span>
          </div>
        </div>

        {/* Right: actions. Stacks under content on mobile (flex-col parent),
            inline on ≥sm. shrink-0 keeps buttons from being squeezed. */}
        <div className="flex items-center gap-3 text-[12px] text-fg-3 sm:shrink-0 sm:pt-0.5">
          {isEmailInvite && (
            <button
              onClick={() =>
                resendInvite.mutate({ hubId, inviteId: invite.id })
              }
              disabled={isResending}
              className="flex items-center gap-1 whitespace-nowrap hover:text-fg-2 transition-colors cursor-pointer disabled:opacity-40"
            >
              <Send className="h-2.5 w-2.5" />
              {isResending ? t.hubs.resending : t.hubs.resend}
            </button>
          )}
          <button
            onClick={onRegenerate}
            disabled={isRegenerating}
            className="flex items-center gap-1 whitespace-nowrap hover:text-fg-2 transition-colors cursor-pointer disabled:opacity-40"
          >
            <RefreshCw
              className={`h-2.5 w-2.5 ${isRegenerating ? "animate-spin" : ""}`}
            />
            {t.hubs.regenerate}
          </button>
          <button
            onClick={onStartRevoke}
            className="whitespace-nowrap hover:text-destructive/60 transition-colors cursor-pointer"
          >
            {t.hubs.revoke}
          </button>
        </div>
      </div>
    </div>
  );
}

type InviteMode = "closed" | "email" | "link";

function InviteCreateButton({
  hubId,
  onCreateInvite,
  t,
}: {
  hubId: string;
  onCreateInvite: ReturnType<typeof useCreateInvite>;
  t: Translations;
}) {
  const [mode, setMode] = useState<InviteMode>("closed");
  const [selectedRole, setSelectedRole] =
    useState<Exclude<HubRole, "owner">>("contributor");
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setMode("closed");
    setEmail("");
    setError(null);
    setSelectedRole("contributor");
  };

  const submitEmail = () => {
    const trimmed = email.trim();
    if (!trimmed) {
      setError(t.hubs.inviteEmailRequired);
      return;
    }
    if (!isValidEmail(trimmed)) {
      setError(t.hubs.inviteEmailInvalid);
      return;
    }
    setError(null);
    onCreateInvite.mutate(
      {
        hubId,
        role: selectedRole,
        invitee: { email: trimmed },
      },
      {
        onSuccess: () => reset(),
        onError: (err) => {
          if (err instanceof MemaxError) {
            if (err.code === "invitee_already_member") {
              setError(t.hubs.inviteAlreadyMember);
              return;
            }
            if (err.code === "invite_limit") {
              setError(t.hubs.inviteLimit);
              return;
            }
            if (err.code === "invalid_email") {
              setError(t.hubs.inviteEmailInvalid);
              return;
            }
          }
          setError(t.hubs.inviteError);
        },
      },
    );
  };

  const submitLinkOnly = () => {
    setError(null);
    onCreateInvite.mutate(
      { hubId, role: selectedRole },
      {
        onSuccess: () => reset(),
        onError: (err) => {
          if (err instanceof MemaxError && err.code === "invite_limit") {
            setError(t.hubs.inviteLimit);
            return;
          }
          setError(t.hubs.inviteError);
        },
      },
    );
  };

  if (mode === "email") {
    return (
      <div className="rounded-surface border border-border/60 px-4 py-3 space-y-3">
        <div className="space-y-1">
          <p className="text-[13px] text-fg-2 font-medium">
            {t.hubs.inviteByEmailTitle}
          </p>
          <p className="text-[12px] text-fg-4">{t.hubs.inviteByEmailHint}</p>
        </div>
        <input
          type="email"
          name="invitee-email"
          autoComplete="off"
          autoCorrect="off"
          spellCheck={false}
          data-1p-ignore
          data-lpignore="true"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            if (error) setError(null);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submitEmail();
            }
          }}
          placeholder={t.hubs.inviteEmailPlaceholder}
          disabled={onCreateInvite.isPending}
          className="w-full bg-surface-1 border border-border/60 rounded-chrome px-3 py-2 text-[13px] text-fg-1 placeholder:text-fg-4 focus:outline-none focus:border-border disabled:opacity-50"
          autoFocus
        />
        <div>
          <p className="text-[12px] text-fg-4 mb-1.5">{t.hubs.inviteAs}</p>
          <div className="flex gap-1.5 flex-wrap">
            {INVITE_ROLE_OPTIONS.map((role) => (
              <button
                key={role}
                onClick={() => setSelectedRole(role)}
                className={`text-[13px] px-3 py-1.5 rounded-chrome transition-colors cursor-pointer ${
                  selectedRole === role
                    ? "bg-foreground text-background font-medium"
                    : "text-fg-2 bg-surface-1 hover:bg-surface-2"
                }`}
              >
                {(t.hubs as Record<string, string>)[role] ?? role}
              </button>
            ))}
          </div>
        </div>
        {error && <p className="text-[12px] text-destructive/80">{error}</p>}
        <div className="flex items-center justify-between gap-2">
          <button
            onClick={() => setMode("link")}
            disabled={onCreateInvite.isPending}
            className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer disabled:opacity-40"
          >
            {t.hubs.inviteUseLinkInstead}
          </button>
          <div className="flex gap-2">
            <button
              onClick={reset}
              disabled={onCreateInvite.isPending}
              className="text-[13px] text-fg-3 px-3 py-1.5 rounded-chrome hover:bg-surface-2 transition-colors cursor-pointer disabled:opacity-40"
            >
              {t.bar.dismiss}
            </button>
            <button
              onClick={submitEmail}
              disabled={onCreateInvite.isPending || !email.trim()}
              className="text-[13px] text-foreground font-medium bg-surface-3 px-3 py-1.5 rounded-chrome transition-colors cursor-pointer disabled:opacity-40"
            >
              {onCreateInvite.isPending
                ? t.hubs.inviteSending
                : t.hubs.inviteSend}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (mode === "link") {
    return (
      <div className="rounded-surface border border-border/60 px-4 py-3 space-y-3">
        <p className="text-[13px] text-fg-3 font-medium">{t.hubs.inviteAs}</p>
        <div className="flex gap-1.5 flex-wrap">
          {INVITE_ROLE_OPTIONS.map((role) => (
            <button
              key={role}
              onClick={() => setSelectedRole(role)}
              className={`text-[13px] px-3 py-1.5 rounded-chrome transition-colors cursor-pointer ${
                selectedRole === role
                  ? "bg-foreground text-background font-medium"
                  : "text-fg-2 bg-surface-1 hover:bg-surface-2"
              }`}
            >
              {(t.hubs as Record<string, string>)[role] ?? role}
            </button>
          ))}
        </div>
        {error && <p className="text-[12px] text-destructive/80">{error}</p>}
        <div className="flex items-center justify-between gap-2">
          <button
            onClick={() => setMode("email")}
            disabled={onCreateInvite.isPending}
            className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer disabled:opacity-40"
          >
            {t.hubs.inviteUseEmailInstead}
          </button>
          <div className="flex gap-2">
            <button
              onClick={reset}
              disabled={onCreateInvite.isPending}
              className="text-[13px] text-fg-3 px-3 py-1.5 rounded-chrome hover:bg-surface-2 transition-colors cursor-pointer disabled:opacity-40"
            >
              {t.bar.dismiss}
            </button>
            <button
              onClick={submitLinkOnly}
              disabled={onCreateInvite.isPending}
              className="text-[13px] text-foreground font-medium bg-surface-3 px-3 py-1.5 rounded-chrome transition-colors cursor-pointer disabled:opacity-40"
            >
              {onCreateInvite.isPending
                ? t.hubs.generating
                : t.hubs.generateLink}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // mode === "closed" — dual entry: email (primary) + link (secondary).
  return (
    <div className="grid grid-cols-2 gap-2">
      <button
        onClick={() => setMode("email")}
        disabled={onCreateInvite.isPending}
        className="flex items-center justify-center gap-2 py-2.5 rounded-chrome text-[13px] font-medium text-background bg-foreground hover:bg-foreground/90 transition-colors cursor-pointer disabled:opacity-30"
      >
        <Mail className="h-3.5 w-3.5" strokeWidth={2} />
        {t.hubs.inviteByEmail}
      </button>
      <button
        onClick={() => setMode("link")}
        disabled={onCreateInvite.isPending}
        className="flex items-center justify-center gap-2 py-2.5 rounded-chrome text-[13px] font-medium text-fg-2 hover:text-fg-1 bg-surface-1 hover:bg-surface-2 transition-colors cursor-pointer disabled:opacity-30"
      >
        <Link2 className="h-3.5 w-3.5" strokeWidth={2} />
        {t.hubs.generateLink}
      </button>
    </div>
  );
}
