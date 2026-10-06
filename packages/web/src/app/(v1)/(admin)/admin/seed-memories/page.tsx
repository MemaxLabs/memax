"use client";

/**
 * /admin/seed-memories — onboarding seed templates surface (plan 23 §5.7
 * + plan 28 §C northstar redesign).
 *
 * Founder-facing tool for editing the curriculum new users receive on
 * signup. Migration 005 ships the bootstrap set; everything after is
 * authored here:
 *   - Click an existing card to edit (Tiptap modal, same editor stack
 *     production users get for memory editing).
 *   - Click the "+" tile to create a new seed.
 *   - Card kebab menu archives/restores or hard-deletes a template.
 *   - Header "Sync to my account" replays the seed-copy worker against
 *     the calling admin's hub so the operator can preview live.
 *
 * Plan 23 principle 4 holds: edits / creates / deletes only affect
 * future signups. Existing per-user copies (different owner_id) stay
 * untouched. The Sync-to-self path is the operator's preview, scoped
 * to their own hub only.
 *
 * Editor parity: `<SeedEditModal>` mounts the same `<SeedEditor>`
 * component the page used to render inline — same Tiptap stack, same
 * markdown round-trip, same inline-image upload pipeline (drop/paste/
 * type `![alt](url)`; SVG blocked, 5MB cap, public R2 prefix `public/
 * seed-images/<uuid>.<ext>`). Modal is keyed by `mode-id` so Tiptap
 * fully re-initializes when switching create/edit, which avoids stale
 * editor state codex review (gpt-5.5) flagged.
 *
 * Operator-facing only — no end-user shipping path. Lives under the
 * /admin segment so the auth gate + admin chrome cover it.
 */

import { useCallback, useEffect, useState } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import {
  markdownEditorExtensions,
  getEditorMarkdown,
  consumeBrowserHistoryChords,
} from "@/lib/markdown-editor-extensions";
import { MarkdownBubbleToolbar } from "@/components/features/markdown-bubble-toolbar";
import { useLocale, interpolate } from "@/i18n";
import {
  listSeedMemories,
  updateSeedMemory,
  uploadSeedImage,
  syncSeedMemoriesToSelf,
  createSeedMemory,
  deleteSeedMemory,
} from "@/lib/admin-client";
import type { AdminSeedMemory } from "@/lib/admin-client";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@memaxlabs/ui";
import { MoreHorizontal, Plus, Archive, Trash2, RotateCcw } from "lucide-react";

// Whitelist mirrors the server-side admin_seed_images.go set. SVG is
// intentionally excluded — public-read SVG can carry script payloads.
const SEED_IMAGE_ACCEPTED_TYPES = new Set([
  "image/png",
  "image/jpeg",
  "image/webp",
  "image/gif",
]);
const SEED_IMAGE_MAX_BYTES = 5 * 1024 * 1024;

type SyncState =
  | { kind: "idle" }
  | { kind: "confirming" }
  | { kind: "running" }
  | { kind: "success"; deleted: number; copied: number }
  | { kind: "error"; message: string };

type EditTarget =
  | { mode: "create" }
  | { mode: "edit"; seed: AdminSeedMemory }
  | null;

export default function AdminSeedMemoriesPage() {
  const { t } = useLocale();
  const copy = t.admin.seedMemories;
  const [seeds, setSeeds] = useState<AdminSeedMemory[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [syncState, setSyncState] = useState<SyncState>({ kind: "idle" });
  const [editTarget, setEditTarget] = useState<EditTarget>(null);
  const [pendingDelete, setPendingDelete] = useState<AdminSeedMemory | null>(
    null,
  );
  const [deleteRunning, setDeleteRunning] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const refetchSeeds = useCallback(async () => {
    try {
      const res = await listSeedMemories();
      setSeeds(res.seeds);
      setLoadError(null);
    } catch {
      setLoadError(copy.loadFailed);
    }
  }, [copy.loadFailed]);

  useEffect(() => {
    void refetchSeeds();
  }, [refetchSeeds]);

  const handleSync = async () => {
    setSyncState({ kind: "running" });
    try {
      const res = await syncSeedMemoriesToSelf();
      setSyncState({
        kind: "success",
        deleted: res.deleted,
        copied: res.copied,
      });
    } catch (err) {
      setSyncState({
        kind: "error",
        message: err instanceof Error ? err.message : copy.syncFailed,
      });
    }
  };

  const handleToggleArchive = async (seed: AdminSeedMemory) => {
    const next = seed.state === "active" ? "archived" : "active";
    try {
      const res = await updateSeedMemory(seed.id, { state: next });
      setSeeds((prev) =>
        prev ? prev.map((s) => (s.id === seed.id ? res.seed : s)) : prev,
      );
    } catch {
      // Surface a quiet failure: the kebab menu's optimistic state
      // doesn't update, and the operator can retry.
    }
  };

  const handleConfirmDelete = async () => {
    if (!pendingDelete) return;
    setDeleteRunning(true);
    setDeleteError(null);
    try {
      await deleteSeedMemory(pendingDelete.id);
      setSeeds((prev) =>
        prev ? prev.filter((s) => s.id !== pendingDelete.id) : prev,
      );
      setPendingDelete(null);
    } catch {
      setDeleteError(copy.deleteFailed);
    } finally {
      setDeleteRunning(false);
    }
  };

  const handleSavedFromModal = (next: AdminSeedMemory) => {
    setSeeds((prev) => {
      if (!prev) return [next];
      const existing = prev.findIndex((s) => s.id === next.id);
      if (existing >= 0) {
        const out = [...prev];
        out[existing] = next;
        return out;
      }
      return [...prev, next];
    });
    setEditTarget(null);
  };

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-6xl">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-[20px] font-semibold text-fg-1">{copy.title}</h1>
          <p className="text-[14px] text-fg-3 mt-1">{copy.description}</p>
        </div>
        <SyncToSelfControl
          state={syncState}
          onStart={() => setSyncState({ kind: "confirming" })}
          onConfirm={handleSync}
          onCancel={() => setSyncState({ kind: "idle" })}
          onDismiss={() => setSyncState({ kind: "idle" })}
          copy={{
            label: copy.syncToMineLabel,
            confirm: copy.syncToMineConfirm,
            confirmAction: copy.syncToMineConfirmAction,
            cancel: copy.syncToMineCancel,
            running: copy.syncToMineRunning,
            success: copy.syncToMineSuccess,
            failed: copy.syncFailed,
          }}
        />
      </header>

      {loadError ? (
        <p className="text-[13px] text-destructive">{loadError}</p>
      ) : seeds === null ? (
        <p className="text-[13px] text-fg-3">…</p>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NewSeedTile
            label={copy.createNewLabel}
            ariaLabel={copy.createNewAria}
            onClick={() => setEditTarget({ mode: "create" })}
          />
          {seeds.map((seed) => (
            <SeedMemoryCard
              key={seed.id}
              seed={seed}
              onEdit={() => setEditTarget({ mode: "edit", seed })}
              onToggleArchive={() => void handleToggleArchive(seed)}
              onDelete={() => {
                setDeleteError(null);
                setPendingDelete(seed);
              }}
            />
          ))}
        </div>
      )}

      {editTarget !== null ? (
        <SeedEditModal
          // `key` forces React to unmount + remount the modal (and its
          // Tiptap editor) when switching create→edit or between two
          // edits. Without this, Tiptap's internal doc state would
          // persist across targets — codex review caught this stale-
          // state risk.
          key={editTarget.mode === "edit" ? editTarget.seed.id : "create"}
          target={editTarget}
          onClose={() => setEditTarget(null)}
          onSaved={handleSavedFromModal}
        />
      ) : null}

      <Dialog
        open={pendingDelete !== null}
        onOpenChange={(next) => {
          // Lock the confirm during the in-flight DELETE — backdrop /
          // Escape / X dismissals would orphan the request and leave
          // any error state stranded on a closed dialog. Codex review.
          if (!next && deleteRunning) return;
          if (!next) {
            setPendingDelete(null);
            setDeleteError(null);
          }
        }}
      >
        <DialogContent className="sm:max-w-md" showCloseButton={!deleteRunning}>
          <DialogHeader>
            <DialogTitle>{copy.deleteTitle}</DialogTitle>
            <DialogDescription>
              {pendingDelete
                ? interpolate(copy.deleteConfirm, {
                    title: pendingDelete.title || copy.untitledFallback,
                  })
                : ""}
            </DialogDescription>
          </DialogHeader>
          {deleteError ? (
            <p className="text-[12px] text-destructive">{deleteError}</p>
          ) : null}
          <DialogFooter>
            <button
              type="button"
              onClick={() => setPendingDelete(null)}
              disabled={deleteRunning}
              className="inline-flex items-center justify-center rounded-chrome border border-border/60 px-3 py-2 text-[13px] font-medium text-fg-2 hover:bg-surface-2 hover:text-fg-1 transition-colors cursor-pointer disabled:cursor-default disabled:opacity-50"
            >
              {copy.deleteCancel}
            </button>
            <button
              type="button"
              onClick={() => void handleConfirmDelete()}
              disabled={deleteRunning}
              className="inline-flex items-center justify-center rounded-chrome bg-destructive px-3 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-90 disabled:cursor-wait disabled:opacity-50 cursor-pointer"
            >
              {deleteRunning ? copy.deleteRunning : copy.deleteConfirmAction}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/**
 * Header-anchored "Sync to my account" affordance. Two-stage destructive
 * pattern (matches `<AgentCard>`'s forget/disconnect): first click flips
 * the button into a confirm strip, second click runs the sync. Result
 * (or error) lands inline as a small status row beneath the strip; the
 * operator dismisses to clear and run again.
 *
 * Scoped to the calling admin only — wipes their seed copies and
 * re-runs the copy logic so they preview live templates without making
 * a fresh signup. Other users' onboarding stays untouched.
 */
function SyncToSelfControl({
  state,
  onStart,
  onConfirm,
  onCancel,
  onDismiss,
  copy,
}: {
  state: SyncState;
  onStart: () => void;
  onConfirm: () => void;
  onCancel: () => void;
  onDismiss: () => void;
  copy: {
    label: string;
    confirm: string;
    confirmAction: string;
    cancel: string;
    running: string;
    success: string;
    failed: string;
  };
}) {
  const isRunning = state.kind === "running";

  if (state.kind === "confirming" || isRunning) {
    return (
      <div
        className="flex items-center gap-2.5 rounded-chrome px-3 py-2 text-[12px]"
        style={{
          backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
        }}
      >
        <span className="text-fg-2">{copy.confirm}</span>
        <button
          type="button"
          onClick={onConfirm}
          disabled={isRunning}
          className="text-destructive/70 hover:text-destructive font-medium cursor-pointer disabled:cursor-wait disabled:text-destructive/40"
        >
          {isRunning ? copy.running : copy.confirmAction}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={isRunning}
          className="text-fg-2 hover:text-foreground font-medium cursor-pointer disabled:cursor-default disabled:text-fg-4"
        >
          {copy.cancel}
        </button>
      </div>
    );
  }

  if (state.kind === "success" || state.kind === "error") {
    const isError = state.kind === "error";
    const message =
      state.kind === "success"
        ? interpolate(copy.success, {
            deleted: state.deleted,
            copied: state.copied,
          })
        : state.message || copy.failed;
    return (
      <button
        type="button"
        onClick={onDismiss}
        className={`text-[12px] px-3 py-2 rounded-chrome cursor-pointer transition-colors ${
          isError
            ? "text-destructive hover:text-destructive/80"
            : "text-emerald-700 dark:text-emerald-400 hover:opacity-80"
        }`}
        title={message}
      >
        {message}
      </button>
    );
  }

  return (
    <button
      type="button"
      onClick={onStart}
      className="text-[12px] px-3 py-2 rounded-chrome border border-border/60 text-fg-2 hover:bg-surface-2 hover:text-fg-1 transition-colors cursor-pointer"
    >
      {copy.label}
    </button>
  );
}

/**
 * `<NewSeedTile>` — "+" cell at the start of the seed grid. Mirrors
 * the `<ConnectAgentTile>` visual treatment (dashed border, neutral
 * elevation on hover) so the two admin-tool surfaces read as siblings.
 * Click opens the create-mode modal.
 */
function NewSeedTile({
  label,
  ariaLabel,
  onClick,
}: {
  label: string;
  ariaLabel: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={ariaLabel}
      className="flex h-full min-h-44 flex-col items-center justify-center gap-2 rounded-surface border border-dashed border-border/70 p-4 text-fg-3 transition-colors hover:border-border hover:bg-surface-1 hover:text-fg-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40 cursor-pointer"
    >
      <span className="flex h-9 w-9 items-center justify-center rounded-chrome bg-surface-1">
        <Plus className="h-4 w-4" strokeWidth={2} />
      </span>
      <span className="text-[13px] font-medium">{label}</span>
    </button>
  );
}

/**
 * `<SeedMemoryCard>` — admin card for a single seed template.
 *
 * Codex review (gpt-5.5) pushed back on reusing prod `<MemoryCardLayout>`
 * here: that component makes the whole card root an interactive
 * `article role="button"`, which fights with an in-card kebab menu
 * (nested interactives + double-fire on click). This is a thin admin-
 * only card: visual recipe matches the prod card (glass-card, rounded-
 * surface, p-4, title + summary preview + tag chips) but the root is
 * a non-interactive `<div>`. Edit is triggered by clicking the body
 * region; the kebab menu sits in the corner with its own click handlers
 * that stopPropagation up the tree.
 *
 * State badge (Active / Archived) sits at the top so the founder sees
 * at a glance what's live for new signups.
 */
function SeedMemoryCard({
  seed,
  onEdit,
  onToggleArchive,
  onDelete,
}: {
  seed: AdminSeedMemory;
  onEdit: () => void;
  onToggleArchive: () => void;
  onDelete: () => void;
}) {
  const { t } = useLocale();
  const copy = t.admin.seedMemories;
  const isActive = seed.state === "active";
  const previewText = seed.summary?.trim()
    ? seed.summary
    : seed.content && seed.content !== seed.title
      ? seed.content.slice(0, 240)
      : "";

  return (
    <div className="glass-card relative flex h-full min-h-44 flex-col rounded-surface p-4 transition-colors hover:bg-surface-1">
      <div className="mb-2 flex items-center justify-between gap-2">
        <span
          className={`text-[10px] uppercase tracking-wider font-medium px-1.5 py-0.5 rounded ${
            isActive
              ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400"
              : "bg-foreground/5 text-fg-3"
          }`}
        >
          {isActive ? copy.stateActive : copy.stateArchived}
        </span>
        <SeedCardMenu
          seed={seed}
          isActive={isActive}
          onToggleArchive={onToggleArchive}
          onDelete={onDelete}
          copy={{
            menu: copy.cardMenuLabel,
            archive: copy.menuArchive,
            unarchive: copy.menuUnarchive,
            delete: copy.menuDelete,
          }}
        />
      </div>
      <button
        type="button"
        onClick={onEdit}
        className="flex flex-1 flex-col gap-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40 rounded-chrome -mx-1 px-1 cursor-pointer"
      >
        <p className="text-[14px] font-medium text-fg-1 line-clamp-2">
          {seed.title || copy.untitledFallback}
        </p>
        {previewText ? (
          <p className="text-[12px] text-fg-3 line-clamp-3 leading-relaxed">
            {previewText}
          </p>
        ) : null}
      </button>
      {seed.tags.length > 0 ? (
        <div className="mt-3 flex flex-wrap items-center gap-1">
          {seed.tags.map((tag) => (
            <span
              key={tag}
              className="rounded bg-foreground/5 px-1.5 py-0.5 text-[10px] text-fg-3"
            >
              {tag}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/**
 * Kebab menu attached to a seed card. Implemented as a small detail/
 * summary-style popover via local open state + outside-click close so
 * we can drive the menu without bringing in a Radix DropdownMenu (the
 * codebase uses base-ui everywhere; introducing one Radix dep just
 * here would be drift). Items stop propagation so a card body click
 * doesn't fire underneath.
 */
function SeedCardMenu({
  isActive,
  onToggleArchive,
  onDelete,
  copy,
}: {
  seed: AdminSeedMemory;
  isActive: boolean;
  onToggleArchive: () => void;
  onDelete: () => void;
  copy: {
    menu: string;
    archive: string;
    unarchive: string;
    delete: string;
  };
}) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    // Defer one tick so the click that opened the menu doesn't
    // immediately close it through the document-level listener.
    const id = window.setTimeout(() => {
      window.addEventListener("click", close);
    }, 0);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.clearTimeout(id);
      window.removeEventListener("click", close);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    // No `role="menu"` / `role="menuitem"` here — codex review (gpt-5.5)
    // flagged that those ARIA roles imply a full menu keyboard
    // contract (focus into menu on open, arrow-key navigation, etc.)
    // which we don't implement. Treating the popover as a plain
    // cluster of buttons is correct for screen readers; we get
    // outside-click + Escape close, which is enough for a 2-item
    // popover. Tab order naturally flows through the buttons.
    <div className="relative" onClick={(e) => e.stopPropagation()}>
      <button
        type="button"
        aria-label={copy.menu}
        aria-expanded={open}
        aria-haspopup
        onClick={() => setOpen((v) => !v)}
        className="flex h-7 w-7 items-center justify-center rounded-chrome text-fg-3 hover:bg-surface-2 hover:text-fg-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40 cursor-pointer"
      >
        <MoreHorizontal className="h-3.5 w-3.5" />
      </button>
      {open ? (
        <div className="absolute right-0 top-8 z-modal w-44 rounded-chrome border border-border/50 bg-popover shadow-lg p-1">
          <button
            type="button"
            onClick={() => {
              setOpen(false);
              onToggleArchive();
            }}
            className="flex w-full items-center gap-2 rounded-chrome px-2 py-1.5 text-[13px] text-fg-2 hover:bg-surface-2 hover:text-fg-1 cursor-pointer"
          >
            {isActive ? (
              <Archive className="h-3.5 w-3.5" />
            ) : (
              <RotateCcw className="h-3.5 w-3.5" />
            )}
            <span>{isActive ? copy.archive : copy.unarchive}</span>
          </button>
          <button
            type="button"
            onClick={() => {
              setOpen(false);
              onDelete();
            }}
            className="flex w-full items-center gap-2 rounded-chrome px-2 py-1.5 text-[13px] text-destructive hover:bg-destructive/10 cursor-pointer"
          >
            <Trash2 className="h-3.5 w-3.5" />
            <span>{copy.delete}</span>
          </button>
        </div>
      ) : null}
    </div>
  );
}

/**
 * `<SeedEditModal>` — create/edit form in a dialog. Mounts the same
 * Tiptap editor production users get for memory editing so the founder
 * authors in WYSIWYG markdown. Modal is keyed by mode-id at the parent
 * so this component freshly re-mounts on each target switch — Tiptap
 * doc state never bleeds between create/edit/edit-different-seed.
 *
 * Save path: POST `/v1/admin/seed-memories` (create) or PATCH
 * `/v1/admin/seed-memories/:id` (edit). The parent receives the
 * normalized AdminSeedMemory back via `onSaved` and updates its
 * in-memory list — no React Query in this admin tool yet.
 */
function SeedEditModal({
  target,
  onClose,
  onSaved,
}: {
  target: { mode: "create" } | { mode: "edit"; seed: AdminSeedMemory };
  onClose: () => void;
  onSaved: (next: AdminSeedMemory) => void;
}) {
  const { t } = useLocale();
  const copy = t.admin.seedMemories;
  const isEdit = target.mode === "edit";
  const initial = isEdit ? target.seed : null;

  const [title, setTitle] = useState(initial?.title ?? "");
  const [summary, setSummary] = useState(initial?.summary ?? "");
  const [hint, setHint] = useState(initial?.hint ?? "");
  const [content, setContent] = useState(initial?.content ?? "");
  const [tagsInput, setTagsInput] = useState(
    initial ? initial.tags.join(", ") : "",
  );
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const handleSave = async () => {
    if (!title.trim()) {
      setSaveError(copy.titleRequired);
      return;
    }
    if (!content.trim()) {
      setSaveError(copy.contentRequired);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const tags = tagsInput
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (isEdit) {
        const res = await updateSeedMemory(target.seed.id, {
          title,
          summary,
          hint,
          content,
          tags,
        });
        onSaved(res.seed);
      } else {
        const res = await createSeedMemory({
          title,
          summary,
          hint,
          content,
          tags,
        });
        onSaved(res.seed);
      }
    } catch {
      setSaveError(copy.saveFailed);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        // Lock the modal during the in-flight create/update — backdrop /
        // Escape / X dismissals would orphan the request and either drop
        // the error or apply success against a closed component. Codex
        // review (gpt-5.5).
        if (!next && saving) return;
        if (!next) onClose();
      }}
    >
      <DialogContent
        className="flex max-h-[85vh] flex-col gap-0 sm:max-w-2xl"
        showCloseButton={!saving}
      >
        <DialogHeader className="pr-8">
          <DialogTitle>
            {isEdit ? copy.editTitle : copy.createNewTitle}
          </DialogTitle>
          <DialogDescription>
            {isEdit ? copy.editDescription : copy.createNewDescription}
          </DialogDescription>
        </DialogHeader>
        <div className="-mx-1 mt-4 flex-1 space-y-3 overflow-y-auto px-1">
          <Field label={copy.titleLabel}>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="w-full bg-transparent border border-border/40 rounded-chrome px-2 py-1 text-[14px]"
            />
          </Field>
          <Field label={copy.summaryLabel}>
            <textarea
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              rows={2}
              className="w-full bg-transparent border border-border/40 rounded-chrome px-2 py-1 text-[13px]"
            />
          </Field>
          <Field label={copy.hintLabel}>
            <input
              type="text"
              value={hint}
              onChange={(e) => setHint(e.target.value)}
              className="w-full bg-transparent border border-border/40 rounded-chrome px-2 py-1 text-[13px]"
            />
          </Field>
          {/* Content section is NOT wrapped in <Field> (which renders a
              <label>) — labels around contenteditable elements break
              keyboard input: the label captures focus delegation, so
              typed characters don't reach ProseMirror, AND chord
              shortcuts like ⌘← escape to global handlers (admin tab
              switcher) instead of the editor's own keymap. Manual label
              text + plain <div> wrapper keeps focus inside the editor. */}
          <div>
            <span className="block text-[11px] uppercase tracking-wider text-fg-3 mb-1">
              {copy.contentLabel}
            </span>
            <SeedEditor content={content} onChange={setContent} />
            <p className="mt-1 text-[11px] text-fg-4">
              {copy.contentImageHint}
            </p>
          </div>
          <Field label={copy.tagsLabel}>
            <input
              type="text"
              value={tagsInput}
              onChange={(e) => setTagsInput(e.target.value)}
              className="w-full bg-transparent border border-border/40 rounded-chrome px-2 py-1 text-[13px]"
            />
          </Field>
          {saveError ? (
            <p className="text-[12px] text-destructive">{saveError}</p>
          ) : null}
        </div>
        <DialogFooter className="mt-4">
          <button
            type="button"
            onClick={onClose}
            disabled={saving}
            className="inline-flex items-center justify-center rounded-chrome border border-border/60 px-3 py-2 text-[13px] font-medium text-fg-2 hover:bg-surface-2 hover:text-fg-1 transition-colors cursor-pointer disabled:cursor-default disabled:opacity-50"
          >
            {copy.modalCancel}
          </button>
          <button
            type="button"
            onClick={() => void handleSave()}
            disabled={saving}
            className="inline-flex items-center justify-center rounded-chrome bg-foreground px-3 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-90 disabled:cursor-wait disabled:opacity-50 cursor-pointer"
          >
            {saving
              ? copy.saving
              : isEdit
                ? copy.modalSaveEdit
                : copy.modalSaveCreate}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type ImageUploadStatus =
  | { kind: "idle" }
  | { kind: "uploading"; filename: string }
  | { kind: "success"; filename: string }
  | { kind: "error"; message: string };

/**
 * `<SeedEditor>` — Tiptap editor for a single seed's body. Mirrors
 * `<MemoryDetailEditor>` in production: same `markdownEditorExtensions`
 * + `<MarkdownBubbleToolbar>` + `getEditorMarkdown` triad. The editor
 * IS the live rendering — no separate preview tab needed.
 *
 * Image upload: drop or paste an image into the editor — the wrapper
 * intercepts and routes through `uploadSeedImage`, which presigns a
 * PUT to the public R2 prefix and returns a stable URL the editor
 * inserts as `![alt](url)`. PNG/JPEG/WebP/GIF only, 5MB cap, SVG
 * blocked. See file-level doc above for the full pipeline.
 */
function SeedEditor({
  content,
  onChange,
}: {
  content: string;
  onChange: (next: string) => void;
}) {
  const { t } = useLocale();
  const copy = t.admin.seedMemories;
  const [uploadStatus, setUploadStatus] = useState<ImageUploadStatus>({
    kind: "idle",
  });

  const editor = useEditor({
    extensions: markdownEditorExtensions(copy.contentPlaceholder),
    content,
    immediatelyRender: false,
    editorProps: {
      // Stops ⌘← / ⌘→ from triggering the browser's history back/
      // forward when focus is in the editor. Without this, typing in
      // a seed and pressing ⌘← yanks the user back to the previously
      // visited admin route (e.g., /admin/waitlist) instead of acting
      // as a cursor-motion key.
      handleKeyDown: consumeBrowserHistoryChords,
    },
    onUpdate: ({ editor }) => onChange(getEditorMarkdown(editor)),
  });

  const handleImageUpload = useCallback(
    async (file: File) => {
      if (!editor) return;
      if (!SEED_IMAGE_ACCEPTED_TYPES.has(file.type)) {
        setUploadStatus({
          kind: "error",
          message: copy.imageUploadRejectedType,
        });
        return;
      }
      if (file.size > SEED_IMAGE_MAX_BYTES) {
        setUploadStatus({
          kind: "error",
          message: copy.imageUploadRejectedSize,
        });
        return;
      }
      setUploadStatus({ kind: "uploading", filename: file.name });
      try {
        const { url } = await uploadSeedImage(file);
        editor.chain().focus().setImage({ src: url, alt: file.name }).run();
        setUploadStatus({ kind: "success", filename: file.name });
      } catch {
        setUploadStatus({ kind: "error", message: copy.imageUploadFailed });
      }
    },
    [
      editor,
      copy.imageUploadRejectedType,
      copy.imageUploadRejectedSize,
      copy.imageUploadFailed,
    ],
  );

  if (!editor) {
    return (
      <div className="rounded-chrome border border-border/40 px-3 py-2 text-[13px] text-fg-4 min-h-24" />
    );
  }

  return (
    <div className="space-y-1">
      <div
        className="rounded-chrome border border-border/40 bg-transparent"
        onDrop={(event) => {
          const file = Array.from(event.dataTransfer.files).find((f) =>
            f.type.startsWith("image/"),
          );
          if (!file) return;
          event.preventDefault();
          void handleImageUpload(file);
        }}
        onPaste={(event) => {
          const file = Array.from(event.clipboardData.items)
            .filter((item) => item.kind === "file")
            .map((item) => item.getAsFile())
            .find((f): f is File => f !== null && f.type.startsWith("image/"));
          if (!file) return;
          event.preventDefault();
          void handleImageUpload(file);
        }}
      >
        <MarkdownBubbleToolbar editor={editor} />
        <EditorContent
          editor={editor}
          className="memax-prose px-3 py-2 text-[14px] text-fg-1 min-h-24 focus-within:outline-none"
        />
      </div>
      {uploadStatus.kind !== "idle" ? (
        <ImageUploadStatusStrip status={uploadStatus} t={copy} />
      ) : null}
    </div>
  );
}

function ImageUploadStatusStrip({
  status,
  t,
}: {
  status: Exclude<ImageUploadStatus, { kind: "idle" }>;
  t: ReturnType<typeof useLocale>["t"]["admin"]["seedMemories"];
}) {
  if (status.kind === "uploading") {
    return (
      <p className="text-[11px] text-fg-3">
        {t.imageUploading}{" "}
        <span className="text-fg-4">({status.filename})</span>
      </p>
    );
  }
  if (status.kind === "success") {
    return (
      <p className="text-[11px] text-emerald-600">
        {t.imageUploaded} <span className="text-fg-4">({status.filename})</span>
      </p>
    );
  }
  return <p className="text-[11px] text-destructive">{status.message}</p>;
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="block text-[11px] uppercase tracking-wider text-fg-3 mb-1">
        {label}
      </span>
      {children}
    </label>
  );
}
