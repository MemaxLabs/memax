"use client";

/**
 * ChatSessionRowMenu — per-session ⋯ overflow with Rename / Pin /
 * Archive / Delete actions. Used by both the desktop sidebar and
 * the mobile sessions list so the menu surface and behavior match
 * across viewports.
 *
 * Reuses ActionMenu (popover + destructive sub-panel pattern) so
 * Delete shows an inline confirm rather than a separate modal —
 * matches the memax destructive grammar shipped on memory-detail
 * and topic surfaces.
 *
 * Rename uses a centered Dialog rather than inline-edit on the row,
 * because the menu lives next to two row consumers (desktop sidebar
 * + mobile list) and a centered dialog avoids plumbing edit state
 * into both. The dialog input is capped at 200 runes (server
 * `chatMaxTitleLen`) and trims on submit; the auto-titler at
 * `postgres_chat.go:633` only fills empty titles, so a manual rename
 * is preserved across future turns.
 *
 * Optimistic Delete: on success, if the deleted session was the
 * one currently open, the parent navigates back to /brain. Hooks
 * already invalidate the list; no extra cache surgery required.
 */

import {
  Archive,
  ArchiveRestore,
  Pencil,
  Pin,
  PinOff,
  Trash2,
} from "lucide-react";
import { useRouter, usePathname } from "next/navigation";
import { useState } from "react";
import type { ChatSession } from "memax-sdk";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@memaxlabs/ui";
import { useDeleteChatSession, usePatchChatSession } from "@/hooks/use-chat";
import { useLocale } from "@/i18n";
import {
  ActionMenu,
  actionMenuEntries,
  type ActionMenuEntry,
} from "@/components/features/action-menu";

export interface ChatSessionRowMenuProps {
  session: ChatSession;
  /**
   * `desktop`: trigger fades in on the row's group-hover (stays
   * visible while the popover is open). `mobile`: trigger always
   * visible (no hover state on touch surfaces).
   */
  variant?: "desktop" | "mobile";
}

const CHAT_TITLE_MAX = 200;

/**
 * Clamp by Unicode code points so we match the server's `utf8RuneCount`
 * cap (Go counts runes, JS string length counts UTF-16 code units).
 * `Array.from(s)` iterates code points, so a 2-unit emoji like 🤖 takes
 * one slot here the same way it does in Go — no over-truncation.
 */
function clampToRunes(value: string, max: number): string {
  const points = Array.from(value);
  if (points.length <= max) return value;
  return points.slice(0, max).join("");
}

export function ChatSessionRowMenu({
  session,
  variant = "desktop",
}: ChatSessionRowMenuProps) {
  const { t } = useLocale();
  const router = useRouter();
  const pathname = usePathname();
  const patch = usePatchChatSession(session.id);
  const del = useDeleteChatSession();

  const isPinned = Boolean(session.pinned_at);
  const isArchived = Boolean(session.archived_at);

  const [renameOpen, setRenameOpen] = useState(false);
  const [draftTitle, setDraftTitle] = useState(session.title ?? "");
  // Tracked separately from `patch.isPending` so a concurrent
  // Pin/Archive mutation on the same `patch` instance doesn't
  // disable the Rename button or flip its label to "Saving…".
  const [renamePending, setRenamePending] = useState(false);

  const trimmedDraft = draftTitle.trim();
  const renameDisabled = renamePending || trimmedDraft.length === 0;

  const openRename = () => {
    setDraftTitle(session.title ?? "");
    setRenameOpen(true);
  };

  const submitRename = () => {
    if (renameDisabled) return;
    if (trimmedDraft === (session.title ?? "").trim()) {
      // No-op — close without firing a mutation.
      setRenameOpen(false);
      return;
    }
    setRenamePending(true);
    patch.mutate(
      { title: trimmedDraft },
      {
        onSuccess: () => setRenameOpen(false),
        // On error, keep the dialog open; the mutation hook surfaces
        // the toast via `meta.errorMessage`. User can retry.
        onSettled: () => setRenamePending(false),
      },
    );
  };

  const entries: ActionMenuEntry[] = actionMenuEntries(
    {
      id: "rename",
      label: t.chat.rowMenu.rename,
      icon: Pencil,
      onSelect: openRename,
    },
    {
      id: "pin",
      label: isPinned ? t.chat.rowMenu.unpin : t.chat.rowMenu.pin,
      icon: isPinned ? PinOff : Pin,
      onSelect: () => {
        patch.mutate({ pinned: !isPinned });
      },
    },
    {
      id: "archive",
      label: isArchived ? t.chat.rowMenu.unarchive : t.chat.rowMenu.archive,
      icon: isArchived ? ArchiveRestore : Archive,
      onSelect: () => {
        patch.mutate({ archived: !isArchived });
      },
    },
    "divider",
    {
      id: "delete",
      label: t.chat.rowMenu.delete,
      icon: Trash2,
      destructive: true,
      confirmPrompt: t.chat.rowMenu.deletePrompt,
      confirmLabel: t.chat.rowMenu.deleteConfirm,
      cancelLabel: t.chat.rowMenu.deleteCancel,
      pendingLabel: t.chat.rowMenu.deletePending,
      onConfirm: async () => {
        try {
          await del.mutateAsync(session.id);
          // If the deleted session was open, return to the index.
          if (pathname?.startsWith(`/brain/sessions/${session.id}`)) {
            router.push("/brain");
          }
          return true;
        } catch {
          // Mutation toast surfaces the error copy; keep the
          // sub-panel mounted so the user can retry.
          return false;
        }
      },
    },
  );

  // Stop click propagation so the trigger tap doesn't bubble up
  // to the row's onClick (which would navigate into the session).
  const triggerClassName =
    variant === "mobile"
      ? "flex h-8 w-8 items-center justify-center rounded-md text-fg-3 cursor-pointer transition-colors hover:bg-foreground/6 hover:text-fg-2 outline-none focus-visible:text-fg-2"
      : "flex h-7 w-7 items-center justify-center rounded-md text-fg-3 opacity-0 group-hover:opacity-100 group-data-[active=true]:opacity-100 cursor-pointer transition-all hover:bg-foreground/6 hover:text-fg-2 outline-none focus-visible:opacity-100 focus-visible:text-fg-2";

  return (
    <span
      // Stop the parent row's click propagation. Without this,
      // tapping ⋯ on a row would also fire the row's onSelect
      // and navigate into the session before the menu opens.
      onClick={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
    >
      <ActionMenu
        items={entries}
        triggerAriaLabel={t.chat.rowMenu.open}
        triggerClassName={triggerClassName}
      />
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t.chat.rowMenu.renameTitle}</DialogTitle>
          </DialogHeader>
          <input
            type="text"
            value={draftTitle}
            onChange={(e) =>
              setDraftTitle(clampToRunes(e.target.value, CHAT_TITLE_MAX))
            }
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submitRename();
              }
            }}
            placeholder={t.chat.sidebar.untitled}
            className="w-full rounded-md border border-border bg-surface-1 px-3 py-2 text-sm text-fg-1 outline-none focus:border-signature/40"
            autoFocus
          />
          <DialogFooter className="mt-2">
            <button
              type="button"
              onClick={() => setRenameOpen(false)}
              className="px-3 py-1.5 text-sm text-fg-2 hover:text-fg-1"
            >
              {t.chat.rowMenu.renameCancel}
            </button>
            <button
              type="button"
              onClick={submitRename}
              disabled={renameDisabled}
              className="rounded-md bg-signature px-3 py-1.5 text-sm font-medium text-on-signature disabled:opacity-50"
            >
              {renamePending
                ? t.chat.rowMenu.renamePending
                : t.chat.rowMenu.renameConfirm}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </span>
  );
}
