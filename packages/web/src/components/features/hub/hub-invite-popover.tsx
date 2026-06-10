"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Copy, Link2, Mail, UserPlus } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@memaxlabs/ui";
import { MemaxError } from "memax-sdk";
import { useInterpolate, useLocale } from "@/i18n";
import { useBar } from "@/contexts/bar-context";
import { useSettingsDialog } from "@/contexts/settings-dialog-context";
import { useCreateInvite } from "@/hooks/use-hub-management";
import {
  INVITE_ROLE_OPTIONS,
  type HubRole,
} from "@/components/features/teams/shared";
import { isValidEmail } from "@/lib/email-validation";

type Mode = "email" | "link";
type InviteRole = Exclude<HubRole, "owner">;

/**
 * Compact invite entry point rendered in the hub header for team hubs when
 * the viewer is owner/admin. Wraps a Popover over the existing
 * useCreateInvite mutation. Shares the same email/link/role semantics as
 * the Settings → Members → Invites surface but trimmed to the single
 * "send one invite right now" path so the popover stays weightless.
 */
export function HubInvitePopover({
  hubId,
  hubName,
}: {
  hubId: string;
  hubName: string;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { setBarNotification } = useBar();
  const { open: openSettings } = useSettingsDialog();
  const createInvite = useCreateInvite();

  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<Mode>("email");
  const [email, setEmail] = useState("");
  const [selectedRole, setSelectedRole] = useState<InviteRole>("contributor");
  const [error, setError] = useState<string | null>(null);
  const [generatedLink, setGeneratedLink] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Email input ref — passed to PopoverContent's `initialFocus` so Base
  // UI focuses the input after positioning settles. The input's React
  // `autoFocus` was removed because it fires on mount, before the
  // portal is positioned: at focus time the input is still at the
  // portal's default (0,0) coords, and Chrome anchors its native
  // address-autofill dropdown to those stale coords — the dropdown
  // ended up floating at the top-left of the viewport even though the
  // input was eventually positioned under the trigger.
  const emailInputRef = useRef<HTMLInputElement | null>(null);

  // Request-generation guard. Each submit bumps this; settle handlers
  // only apply if their captured gen still matches. The on-close reset
  // effect also bumps it so mutations that resolve after dismissal are
  // ignored (otherwise the next open could flash stale link/error state
  // from a previous session).
  const submitGenRef = useRef(0);

  useEffect(() => {
    if (open) return;
    // Close resets local state so the next open starts clean. Deferred to
    // the closed state so the fade-out animation doesn't flash an empty
    // form during dismiss. Bumping submitGenRef here strands any in-flight
    // mutation's onSuccess/onError so it can't repopulate state after close.
    submitGenRef.current += 1;
    setMode("email");
    setEmail("");
    setError(null);
    setSelectedRole("contributor");
    setGeneratedLink(null);
    setCopied(false);
  }, [open]);

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
    const gen = ++submitGenRef.current;
    createInvite.mutate(
      { hubId, role: selectedRole, invitee: { email: trimmed } },
      {
        onSuccess: () => {
          if (submitGenRef.current !== gen) return;
          setBarNotification({
            type: "success",
            message: interpolate(t.hubs.inviteSentNew, { email: trimmed }),
            autoDismissMs: 2500,
            onDismiss: () => setBarNotification(null),
          });
          setEmail("");
        },
        onError: (err) => {
          if (submitGenRef.current !== gen) return;
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

  const submitLink = () => {
    setError(null);
    const gen = ++submitGenRef.current;
    createInvite.mutate(
      { hubId, role: selectedRole },
      {
        onSuccess: (invite) => {
          if (submitGenRef.current !== gen) return;
          const origin =
            typeof window !== "undefined" ? window.location.origin : "";
          setGeneratedLink(`${origin}/invite/${invite.token}`);
        },
        onError: (err) => {
          if (submitGenRef.current !== gen) return;
          if (err instanceof MemaxError && err.code === "invite_limit") {
            setError(t.hubs.inviteLimit);
            return;
          }
          setError(t.hubs.inviteError);
        },
      },
    );
  };

  const copyLink = async () => {
    if (!generatedLink) return;
    try {
      await navigator.clipboard.writeText(generatedLink);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard write can fail on insecure contexts or denied
      // permissions. Fall back silently — user can still select the
      // link text manually.
    }
  };

  const openManage = () => {
    setOpen(false);
    openSettings({ kind: "hub", hubId, section: "members" });
  };

  const isPending = createInvite.isPending;
  const roleLabel = (role: InviteRole): string =>
    (t.hubs as Record<string, string>)[role] ?? role;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={t.hubs.inviteHeaderTrigger}
        className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[12px] font-medium text-fg-2 hover:text-fg-1 hover:bg-surface-2 transition-colors cursor-pointer"
      >
        <UserPlus className="h-3.5 w-3.5" strokeWidth={1.8} />
        <span>{t.hubs.inviteHeaderTrigger}</span>
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="end"
        sideOffset={8}
        initialFocus={emailInputRef}
      >
        <div className="w-[320px] p-4 space-y-3">
          <p className="text-[13px] font-medium text-fg-1">
            {interpolate(t.hubs.invitePopoverTitle, { name: hubName })}
          </p>

          {mode === "email" && (
            <>
              <input
                ref={emailInputRef}
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
                disabled={isPending}
                className="w-full bg-surface-1 border border-border/60 rounded-chrome px-3 py-2 text-[13px] text-fg-1 placeholder:text-fg-4 focus:outline-none focus:border-border disabled:opacity-50"
              />
              <div className="flex flex-wrap items-center gap-1.5">
                {INVITE_ROLE_OPTIONS.map((role) => (
                  <button
                    key={role}
                    type="button"
                    onClick={() => setSelectedRole(role)}
                    className={`text-[12px] px-2.5 py-1 rounded-chrome transition-colors cursor-pointer ${
                      selectedRole === role
                        ? "bg-foreground text-background font-medium"
                        : "text-fg-2 bg-surface-1 hover:bg-surface-2"
                    }`}
                  >
                    {roleLabel(role)}
                  </button>
                ))}
              </div>
              {error && (
                <p className="text-[12px] text-destructive/80">{error}</p>
              )}
              <div className="flex items-center justify-between gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setMode("link");
                    setError(null);
                  }}
                  disabled={isPending}
                  className="flex shrink items-center gap-1 text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer disabled:opacity-40 whitespace-nowrap"
                >
                  <Link2 className="h-3 w-3 shrink-0" />
                  {t.hubs.inviteUseLinkInstead}
                </button>
                <button
                  type="button"
                  onClick={submitEmail}
                  disabled={isPending || !email.trim()}
                  className="flex shrink-0 items-center gap-1.5 text-[13px] font-medium text-background bg-foreground px-3 py-1.5 rounded-chrome hover:opacity-90 transition-opacity cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed whitespace-nowrap"
                >
                  <Mail className="h-3 w-3 shrink-0" />
                  {isPending ? t.hubs.inviteSending : t.hubs.inviteSend}
                </button>
              </div>
            </>
          )}

          {mode === "link" && !generatedLink && (
            <>
              <p className="text-[12px] text-fg-3">{t.hubs.inviteAs}</p>
              <div className="flex flex-wrap items-center gap-1.5">
                {INVITE_ROLE_OPTIONS.map((role) => (
                  <button
                    key={role}
                    type="button"
                    onClick={() => setSelectedRole(role)}
                    className={`text-[12px] px-2.5 py-1 rounded-chrome transition-colors cursor-pointer ${
                      selectedRole === role
                        ? "bg-foreground text-background font-medium"
                        : "text-fg-2 bg-surface-1 hover:bg-surface-2"
                    }`}
                  >
                    {roleLabel(role)}
                  </button>
                ))}
              </div>
              {error && (
                <p className="text-[12px] text-destructive/80">{error}</p>
              )}
              <div className="flex items-center justify-between gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setMode("email");
                    setError(null);
                  }}
                  disabled={isPending}
                  className="flex shrink items-center gap-1 text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer disabled:opacity-40 whitespace-nowrap"
                >
                  <Mail className="h-3 w-3 shrink-0" />
                  {t.hubs.inviteUseEmailInstead}
                </button>
                <button
                  type="button"
                  onClick={submitLink}
                  disabled={isPending}
                  className="flex shrink-0 items-center gap-1.5 text-[13px] font-medium text-background bg-foreground px-3 py-1.5 rounded-chrome hover:opacity-90 transition-opacity cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed whitespace-nowrap"
                >
                  <Link2 className="h-3 w-3 shrink-0" />
                  {isPending ? t.hubs.generating : t.hubs.generateLink}
                </button>
              </div>
            </>
          )}

          {mode === "link" && generatedLink && (
            <>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-[12px] text-fg-2 bg-surface-1 px-2.5 py-2 rounded-chrome truncate font-mono">
                  {generatedLink}
                </code>
                <button
                  type="button"
                  onClick={copyLink}
                  aria-label={t.hubs.copyLink}
                  className="p-1.5 text-fg-3 hover:text-fg-1 transition-colors cursor-pointer"
                >
                  {copied ? (
                    <Check className="h-3.5 w-3.5 text-emerald-500/70" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <div className="flex items-center justify-between gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setGeneratedLink(null);
                    setCopied(false);
                    setMode("email");
                  }}
                  className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
                >
                  {t.hubs.inviteUseEmailInstead}
                </button>
                <button
                  type="button"
                  onClick={() => setOpen(false)}
                  className="text-[13px] text-fg-2 hover:text-fg-1 font-medium px-3 py-1.5 rounded-chrome hover:bg-surface-2 transition-colors cursor-pointer"
                >
                  {t.bar.dismiss}
                </button>
              </div>
            </>
          )}

          <div className="pt-2 border-t border-border/40">
            <button
              type="button"
              onClick={openManage}
              className="text-[12px] text-fg-3 hover:text-fg-1 transition-colors cursor-pointer"
            >
              {t.hubs.manageInvites}
            </button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
