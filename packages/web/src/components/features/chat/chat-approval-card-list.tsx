"use client";

/**
 * ChatApprovalCardList — inline approval prompts addressed to the
 * user. Plan 24 §"Approval flow" pins this as the surface where
 * mutating tools (push_memory, forget_memory, propose_topic_merge)
 * pause and ask for explicit consent before execution.
 *
 * The card mirrors the standard memax destructive-action pattern:
 * approve action on the right (where the cursor is after reading),
 * deny on the left, both buttons distinct hit targets — never
 * same-button-twice. Args render as compact JSON for transparency;
 * the user's mental model is "memax wants to do X with these
 * inputs," so the inputs ARE the consent context.
 */

import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Shield, X as XIcon } from "lucide-react";
import { cn } from "@memaxlabs/ui";
import type { ChatApprovalRecord } from "@/hooks/use-chat-stream";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";

export function ChatApprovalCardList({
  approvals,
  onDecide,
}: {
  approvals: ChatApprovalRecord[];
  onDecide: (
    approval: ChatApprovalRecord,
    decision: "approve" | "deny",
  ) => void;
}) {
  return (
    <div className="flex flex-col gap-2 w-full">
      <AnimatePresence initial={false}>
        {approvals.map((approval) => (
          <motion.div
            key={approval.approval_id}
            // Subtle drop-in: opacity + a tiny scale-up so the
            // card "settles" into place instead of flashing.
            // 4px translate replaces the prior height-collapse
            // (which clipped the card mid-render) and stays
            // small enough not to compound with neighbor
            // reflow. No `layout` — neighbors reflow naturally
            // when an approval leaves.
            initial={{ opacity: 0, y: 6, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{
              opacity: 0,
              scale: 0.98,
              transition: { duration: 0.14, ease: [0.16, 1, 0.3, 1] },
            }}
            transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
            // Animation transforms can clip child shadows on some
            // browsers — explicit padding via the parent gap
            // gives the card breathing room without using
            // overflow-hidden (which clipped earlier).
            style={{ transformOrigin: "top center" }}
          >
            <ChatApprovalCard approval={approval} onDecide={onDecide} />
          </motion.div>
        ))}
      </AnimatePresence>
    </div>
  );
}

function ChatApprovalCard({
  approval,
  onDecide,
}: {
  approval: ChatApprovalRecord;
  onDecide: (
    approval: ChatApprovalRecord,
    decision: "approve" | "deny",
  ) => void;
}) {
  const { t } = useLocale();
  const [decided, setDecided] = useState<"approve" | "deny" | null>(null);
  const [secondsRemaining, setSecondsRemaining] = useState<number | null>(null);

  useEffect(() => {
    if (!approval.expires_at) {
      setSecondsRemaining(null);
      return undefined;
    }
    const expiresAt = Date.parse(approval.expires_at);
    if (!Number.isFinite(expiresAt)) {
      setSecondsRemaining(null);
      return undefined;
    }
    const tick = () => {
      const remaining = Math.max(
        0,
        Math.floor((expiresAt - Date.now()) / 1000),
      );
      setSecondsRemaining(remaining);
    };
    tick();
    const handle = setInterval(tick, 1000);
    return () => clearInterval(handle);
  }, [approval.expires_at]);

  const argsText = approval.args ? compactJSON(approval.args) : null;
  const expired = secondsRemaining !== null && secondsRemaining <= 0;

  return (
    <div
      className={cn(
        "rounded-2xl border bg-surface-1 px-4 py-3 transition-[box-shadow,border-color,opacity] duration-300",
        // First-paint signature glow draws the eye — the agent
        // is asking for explicit consent, this isn't background
        // chrome. Once decided/expired the glow fades to neutral.
        decided || expired
          ? "border-border/60 shadow-none"
          : "border-[oklch(from_var(--signature)_l_c_h_/_0.35)] shadow-[0_0_0_3px_oklch(from_var(--signature)_l_c_h_/_0.1)]",
        decided && "opacity-70",
        expired && "opacity-50",
      )}
      role="region"
      aria-label={interpolate(t.chat.approval.title, {
        tool: approval.tool_name,
      })}
    >
      <div className="flex items-start gap-2">
        <Shield
          className="mt-0.5 h-4 w-4 shrink-0 text-signature"
          aria-hidden
        />
        <div className="flex-1 min-w-0">
          <div className="text-[14px] font-medium text-foreground">
            {interpolate(t.chat.approval.title, { tool: approval.tool_name })}
          </div>
          <div className="mt-0.5 text-[12px] text-fg-3">
            {interpolate(t.chat.approval.description, {
              tool: approval.tool_name,
            })}
          </div>
          {argsText && (
            <pre className="mt-2 max-h-32 overflow-auto rounded-md bg-surface-2 px-2.5 py-1.5 text-[11px] font-mono text-fg-2 break-all whitespace-pre-wrap">
              {argsText}
            </pre>
          )}
          {secondsRemaining !== null && !decided && (
            <div className="mt-1.5 text-[11px] text-fg-4">
              {expired
                ? t.chat.approval.expired
                : interpolate(t.chat.approval.expiresIn, {
                    seconds: secondsRemaining,
                  })}
            </div>
          )}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-end gap-2">
        <button
          type="button"
          disabled={Boolean(decided) || expired}
          onClick={() => {
            setDecided("deny");
            onDecide(approval, "deny");
          }}
          className={cn(
            "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-[13px] text-fg-2 transition-colors",
            "hover:bg-surface-2",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          <AnimatePresence initial={false}>
            {decided === "deny" && (
              <motion.span
                key="deny-icon"
                initial={{ opacity: 0, scale: 0.6 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{
                  type: "spring",
                  stiffness: 600,
                  damping: 28,
                }}
                className="inline-flex"
              >
                <XIcon className="h-3.5 w-3.5" aria-hidden />
              </motion.span>
            )}
          </AnimatePresence>
          {t.chat.approval.deny}
        </button>
        <button
          type="button"
          disabled={Boolean(decided) || expired}
          onClick={() => {
            setDecided("approve");
            onDecide(approval, "approve");
          }}
          className={cn(
            "inline-flex items-center gap-1.5 rounded-lg bg-foreground px-3 py-1.5 text-[13px] text-background transition-opacity",
            "hover:opacity-90",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          <AnimatePresence initial={false}>
            {decided === "approve" && (
              <motion.span
                key="approve-icon"
                initial={{ opacity: 0, scale: 0.6 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{
                  type: "spring",
                  stiffness: 600,
                  damping: 28,
                }}
                className="inline-flex"
              >
                <Check className="h-3.5 w-3.5" strokeWidth={3} aria-hidden />
              </motion.span>
            )}
          </AnimatePresence>
          {t.chat.approval.approve}
        </button>
      </div>
    </div>
  );
}

function compactJSON(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
