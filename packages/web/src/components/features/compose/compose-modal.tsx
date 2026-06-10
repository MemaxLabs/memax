"use client";

/**
 * ComposeModal — centered glass-panel surface for "considered capture".
 *
 * Mounted once at app shell root via `<ComposeProvider>` in
 * AppShellClient. Visibility driven by `useCompose().open`.
 *
 * Editor: Tiptap v3 with StarterKit + Placeholder + Link + Image +
 * Typography + Markdown serializer (deps in `packages/web/package.json`).
 * Markdown shortcuts (`# `, `- `, `**`, `>`, ```, `[text](url)`,
 * `![alt](url)`) render inline as the user types — no edit/preview
 * toggle, Notion / Bear convention. StarterKit's bundled Link is
 * disabled (`StarterKit.configure({ link: false })`) so the explicit
 * Link config wins.
 *
 * Hotkeys (when modal open):
 *   ⌘↵ / Ctrl↵   save (calls useCreateMemory; closes on success)
 *   Esc          close (draft preserved by ComposeProvider)
 *
 * Hotkey for OPENING (⌘⇧↵) is a global handler in AppShellClient — not
 * scoped to this modal.
 *
 * The save flow uses the existing `useCreateMemory()` mutation, which
 * does optimistic insert into `["recent-memories", ...]` so the new
 * memory appears in the grid instantly. Active hub (from useAuth) is
 * the implicit target; hub picker UI lands in plan 20 phase 1B.
 *
 * A11y: focus trap via base-ui `<Dialog>`, focus restore on close,
 * `aria-modal`, dialog role. Esc handled by base-ui internals; click-
 * outside dismisses (via `Backdrop` press).
 */

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { useRouter } from "next/navigation";
import { Dialog } from "@base-ui/react/dialog";
import { useEditor, EditorContent } from "@tiptap/react";
import { MarkdownBubbleToolbar } from "@/components/features/markdown-bubble-toolbar";
import {
  markdownEditorExtensions,
  getEditorMarkdown,
} from "@/lib/markdown-editor-extensions";
import {
  ChevronDown,
  X,
  Loader2,
  Check,
  Paperclip,
  AlertCircle,
} from "lucide-react";
import { Pill, Popover, PopoverTrigger, PopoverContent } from "@memaxlabs/ui";
import { useCompose } from "@/contexts/compose-context";
import { useCreateMemory } from "@/hooks/use-memories";
import { useAuth, useActiveHub } from "@/lib/auth";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { HubBadge } from "@/components/features/hub/hub-badge";
import { useLocale, useInterpolate } from "@/i18n";
import { classifyMutationError } from "@/lib/error-copy";
import {
  processDroppedFiles,
  isBinaryFileName,
} from "@/lib/process-dropped-files";
import { uploadMemoryObject, detectUploadMimeType } from "@/lib/memory-upload";
import { deriveTitleFromContent } from "./compose-title";

// `useBarToast` was used in 1b for save-failure feedback, but bar
// notifications render at z-bar-notif (30), which is BEHIND the modal
// at z-modal (60). Codex 5.5 caught the layering bug; we now show
// inline error UI inside the modal body instead.

interface ComposeModalProps {
  /**
   * - `"modal"` (default): centered glass-panel dialog. Visibility
   *   driven by `useCompose().open`. close() calls compose.close().
   * - `"route"`: full-screen surface mounted by a route page (plan 22
   *   §5.3). Always visible while mounted; close() calls
   *   router.back(). The `useCompose().open` flag is ignored — the
   *   route IS the open state.
   */
  variant?: "modal" | "route";
  /**
   * When set on the route variant, pre-populates the target hub if
   * the draft doesn't already carry a targetHubId. Resolves the slug
   * via the auth-context hubs list. No-op for the modal variant
   * (active hub is the implicit default there).
   */
  defaultHubSlug?: string;
}

export function ComposeModal({
  variant = "modal",
  defaultHubSlug,
}: ComposeModalProps = {}) {
  const { open, draft, close: composeClose, clear, setDraft } = useCompose();
  const createMemory = useCreateMemory();
  const { activeHub } = useActiveHub();
  const { hubs, user } = useAuth();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const [saveError, setSaveError] = useState<string | null>(null);
  const isRoute = variant === "route";

  // Route variant: close = navigate back. Modal variant: close =
  // compose.close() (preserves draft per ComposeProvider contract).
  // Both still flow through the same handlers below; only the
  // bottom-of-stack action differs.
  const close = useCallback(() => {
    if (isRoute) {
      router.back();
      return;
    }
    composeClose();
  }, [isRoute, router, composeClose]);

  // Pre-populate targetHubId from defaultHubSlug on first mount of
  // the route variant. Skipped if the draft already carries a hub
  // pick (user navigated away mid-compose, came back).
  useEffect(() => {
    if (!isRoute) return;
    if (draft.targetHubId) return;
    if (!defaultHubSlug) return;
    const match = hubs.find((h) => h.hub.slug === defaultHubSlug);
    if (!match) return;
    setDraft({ targetHubId: match.hub.id });
    // intentionally bound only to defaultHubSlug + isRoute — the
    // pre-populate fires once per route mount; subsequent draft.
    // targetHubId mutations are user-driven via the picker.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isRoute, defaultHubSlug]);
  // Picker open state — controlled so selecting a hub closes the
  // popover automatically. Codex 5.5 L-finding: an uncontrolled
  // popover stays open after selection, requiring an extra outside
  // click/Esc to dismiss.
  const [pickerOpen, setPickerOpen] = useState(false);

  // Target hub: explicit pick from the picker (in draft so it
  // survives close), falling back to the active hub when the user
  // hasn't picked. The picker shows the resolved hub regardless of
  // which path it came from. Coerces a stale pick (hub deleted while
  // the modal was closed) back to the active hub. Codex 5.5 M-finding:
  // without this, the chip falls back to active but `mutateAsync`
  // would still submit the dead `targetHubId`.
  const viewerIdentity = user
    ? { name: user.name, displayName: user.display_name }
    : undefined;
  const targetHubEntry =
    (draft.targetHubId && hubs.find((h) => h.hub.id === draft.targetHubId)) ||
    activeHub ||
    null;
  const targetHubName = targetHubEntry
    ? getHubDisplayName(targetHubEntry.hub, t, viewerIdentity)
    : "";
  const targetHubInitial = targetHubEntry
    ? getHubDisplayInitial(targetHubEntry.hub, t, viewerIdentity)
    : "";
  // Local tag-input state — only the COMMITTED tag list lives in
  // `draft.tags` (so it persists across modal close like the rest of
  // the draft). The text the user is currently typing into the input
  // is uncommitted; Enter/comma promotes it into the draft list.
  // Reset on close (effect below) so a half-typed tag doesn't survive
  // dismissal — the contract is "only committed tags persist".
  const [tagDraft, setTagDraft] = useState("");

  /**
   * Normalize tag input: trim outer whitespace, strip any leading
   * `#` plus its trailing whitespace, trim again. So `"# tag"`,
   * `"#tag"`, `"  tag  "` all collapse to `"tag"`. Centralized so
   * commit + save-auto-promote agree on the same shape.
   */
  const normalizeTag = (raw: string): string =>
    raw
      .trim()
      .replace(/^#+\s*/, "")
      .trim();

  const commitTag = (raw: string) => {
    const tag = normalizeTag(raw);
    if (!tag) return;
    if (draft.tags.includes(tag)) {
      // Already in the list — clear the input but don't duplicate.
      setTagDraft("");
      return;
    }
    setDraft({ tags: [...draft.tags, tag] });
    setTagDraft("");
  };

  const removeTag = (tag: string) => {
    setDraft({ tags: draft.tags.filter((t) => t !== tag) });
  };

  // Discard half-typed tag input on modal close. The committed tag
  // list (`draft.tags`) survives via ComposeProvider as designed; only
  // the uncommitted in-flight string resets. Codex 5.5 M-finding.
  useEffect(() => {
    if (!open) setTagDraft("");
  }, [open]);

  // Live draft mirror — `handleSave` reads this on resolve so the
  // race-fix comparison sees the user's CURRENT title, not the value
  // captured in the closure when the mutation was kicked off. Codex 5.5
  // caught: comparing against `draft.title` from the closure scope
  // would let a title-only edit during pending-save still wipe the new
  // title. Body comparison reads `getEditorMarkdown(editor)` directly which
  // is already live; only title needs the ref.
  //
  // `useLayoutEffect` (not `useEffect`) — the await continuation in
  // `handleSave` resumes as a microtask. Passive effects flush after
  // paint; layout effects flush synchronously after the commit. Using
  // a passive effect here can leave `draftRef.current` lagging by one
  // render between a state change and the post-await read. Codex 5.5
  // caught the timing gap.
  const draftRef = useRef(draft);
  useLayoutEffect(() => {
    draftRef.current = draft;
  }, [draft]);

  // Same useLayoutEffect mirror for the in-flight tag input, so the
  // race-check post-await can detect "user opened the modal mid-save
  // and is currently typing a new tag" without that tag being lost.
  // Codex 5.5: the prior race check covered title + content + tags
  // but not tagDraft, so an uncommitted-but-being-typed tag would be
  // wiped when the original save's success branch fired.
  const tagDraftRef = useRef(tagDraft);
  useLayoutEffect(() => {
    tagDraftRef.current = tagDraft;
  }, [tagDraft]);

  // Clear any stale save-error banner whenever the draft mutates (any
  // path: typing in editor, title input change, paste/drop, programmatic
  // setDraft) AND when the modal opens fresh. Reopening the modal after
  // a previous failed save shouldn't show a banner from the prior
  // session unless the user hasn't touched the draft since. Calling
  // `setSaveError(null)` unconditionally is fine — React bails on
  // identical values, so the effect doesn't trigger an extra render
  // when the banner was already empty.
  useEffect(() => {
    setSaveError(null);
  }, [draft, open]);

  const editor = useEditor({
    extensions: markdownEditorExtensions(t.composeModal.bodyPlaceholder),
    content: draft.content,
    immediatelyRender: false,
    onUpdate: ({ editor }) => {
      setDraft({ content: getEditorMarkdown(editor) });
    },
  });

  // Auto-focus the editor when the modal opens so the user can start
  // typing immediately (Cmd/Ctrl+Shift+Enter → modal up, caret blinks
  // in body — Notion/Linear/Things compose convention). Don't steal
  // focus when the modal opens with the title input already focused
  // (title field is right above the editor and can grab default
  // focus from base-ui's focus restore on some paths). The body
  // editor wins by default for empty drafts; for preserved drafts
  // we still focus body because the user is most likely resuming
  // mid-paragraph.
  useEffect(() => {
    if (!open || !editor) return;
    // Defer one frame so base-ui Dialog's mount + focus-trap setup
    // completes before we override focus. Without this, base-ui's
    // initial focus pass can land focus elsewhere AFTER our call.
    const handle = window.setTimeout(() => editor.commands.focus("end"), 0);
    return () => window.clearTimeout(handle);
  }, [open, editor]);

  // Sync draft → editor when the modal reopens with a preserved draft.
  // Mounting/unmounting the editor here would lose Tiptap selection +
  // history, so we keep it mounted and reset content imperatively.
  useEffect(() => {
    if (!open || !editor) return;
    if (getEditorMarkdown(editor) === draft.content) return;
    editor.commands.setContent(draft.content);
    // The dependency list intentionally omits `draft.content` — the
    // user's own typing flows draft.content through onUpdate, and we
    // don't want to fight the editor's current selection. We only
    // sync on open to seed the preserved draft from a prior session.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editor]);

  const handleSave = async () => {
    if (!editor) return;
    // Pending guard: the Save button is disabled while a previous
    // mutation runs, but the ⌘↵ hotkey fires regardless of button
    // state — without this early return, two quick presses would
    // create duplicate memories.
    if (createMemory.isPending) return;
    // Attachment guard: same reason. The Save button disables for
    // `uploading | error` attachment states, but `Cmd+Enter` doesn't
    // route through the button — without this guard, a hotkey press
    // mid-upload would ship a memory with `file_ref: undefined`
    // (status uploading) or with a stale errored attachment that the
    // user hasn't acknowledged. Codex 5.5 H-finding.
    if (
      draft.attachment &&
      (draft.attachment.status === "uploading" ||
        draft.attachment.status === "error")
    )
      return;
    const content = getEditorMarkdown(editor).trim();
    if (!content) return;
    // Title required by the SDK contract. Fall back to the first line of
    // content (trimmed of markdown punctuation, ≤80 chars) when the user
    // doesn't supply one — mirrors the auto-titling behavior of the
    // bar's quick-capture path.
    const explicitTitle = draft.title.trim();
    const title = explicitTitle || deriveTitleFromContent(content);
    // Promote any in-flight tag input to a committed tag before
    // saving. Otherwise a user who typed "draft" + clicked Save (no
    // Enter) would see their tag silently dropped.
    //
    // Codex 5.5 caught: we used to add `pendingTag` only to the
    // SUBMITTED snapshot, leaving `draft.tags` without the new tag.
    // The live comparison post-await then failed and the success
    // branch never cleared/closed the modal — opening a duplicate-
    // save path. Fix: commit the pending tag into draft state BEFORE
    // snapshotting so the live ref will reflect it by the time the
    // await continuation runs (setDraft → React commit →
    // useLayoutEffect updates draftRef synchronously before the
    // microtask resumes).
    const pendingTag = normalizeTag(tagDraft);
    const tags =
      pendingTag && !draft.tags.includes(pendingTag)
        ? [...draft.tags, pendingTag]
        : draft.tags;
    // Always clear tagDraft on save — even when the raw input
    // normalized to empty (e.g., "#", "##  ") and didn't promote.
    // Otherwise a non-empty `tagDraftRef` post-await would fail the
    // race check's `tagDraftMatch` and the success branch wouldn't
    // clear/close — opening a duplicate-save path. React bails on
    // identical-value setStates so the no-op case is cheap. Codex
    // 5.5 caught two flavors of this: duplicate-pending and the
    // normalize-to-empty case.
    setTagDraft("");
    if (tags !== draft.tags) {
      setDraft({ tags });
    }
    // Resolve `targetHubId` against the current `hubs` list — coerce
    // a stale pick (hub deleted while modal was closed, draft still
    // has the old id) back to undefined so the save uses the active
    // hub. Without this the chip would fall back to active in the UI
    // but `mutateAsync` would still send the dead id. Codex 5.5
    // M-finding.
    //
    // Resolution applies ONLY to the save call. The race-fix snapshot
    // compares RAW `draft.targetHubId` values — the user's intent is
    // "did they touch the picker?", not "is the resolved hub still
    // valid?". Codex 5.5 caught the asymmetric-resolve trap: if a hub
    // is deleted mid-save, raw stays the same but the resolved value
    // flips to undefined → false mismatch → modal stays open after a
    // successful save. Raw comparison sidesteps that AND still
    // catches "user picked a newly-available hub mid-save" (raw goes
    // undefined → "C", mismatch, no clear).
    const resolvedTargetHubId =
      draft.targetHubId && hubs.some((h) => h.hub.id === draft.targetHubId)
        ? draft.targetHubId
        : undefined;
    // Snapshot the submitted content + title + tags + targetHubId +
    // attachment object_key. If the user closes the modal mid-save,
    // reopens it, and changes ANY of these before the original
    // mutation resolves, the resolved success branch must not wipe
    // their NEW draft. We compare against the live draft on resolve
    // and only reset state when every field still matches.
    //
    // Attachment is identified by `fileRef.object_key` (server-
    // generated, stable across renders). `undefined` means "no
    // attachment". The user replacing the attachment mid-save bumps
    // `uploadGenRef`; the submitted fileRef stays at its captured
    // value while the live draft.attachment.fileRef points at the
    // new upload's key — so the comparison correctly fails. Codex
    // 5.5 H-finding.
    const submitted = {
      title,
      content,
      tags,
      targetHubId: draft.targetHubId,
      attachmentKey: draft.attachment?.fileRef?.object_key,
    };
    setSaveError(null);
    try {
      await createMemory.mutateAsync({
        content,
        title,
        content_type: "markdown",
        tags: tags.length > 0 ? tags : undefined,
        // Picker selection wins over active hub. Falls back to active
        // hub when the user hasn't explicitly picked or when the pick
        // is stale (resolved to undefined above).
        hubId: resolvedTargetHubId ?? activeHub?.hub.id,
        // Attachment fileRef from the staged upload, if any. The save
        // button is disabled while `attachment.status === "uploading"
        // | "error"`, so by the time we reach this branch either no
        // attachment is staged or it's `uploaded` with a populated
        // fileRef.
        file_ref: draft.attachment?.fileRef,
      });
      // Read draft from the refs (always current) so an edit landing
      // while mutateAsync was in flight is visible here. Title / tags
      // / targetHubId come from draftRef; tagDraft has its own ref
      // because it lives in local state, not the shared compose
      // context.
      const live = getEditorMarkdown(editor).trim();
      const liveTitle = draftRef.current.title.trim();
      const liveTags = draftRef.current.tags;
      const liveTargetHubId = draftRef.current.targetHubId;
      const liveAttachmentKey =
        draftRef.current.attachment?.fileRef?.object_key;
      const tagsMatch =
        liveTags.length === submitted.tags.length &&
        liveTags.every((tag, i) => tag === submitted.tags[i]);
      // After save-time auto-promote, tagDraft was reset to "". If
      // the user typed something new in the input post-submit
      // (close → reopen → type without committing), tagDraftRef
      // would be non-empty — catches a state the {title,content,tags}
      // snapshot otherwise misses. Codex 5.5 M-finding.
      const tagDraftMatch = tagDraftRef.current === "";
      const stillMatches =
        live === submitted.content &&
        liveTitle === explicitTitle &&
        tagsMatch &&
        tagDraftMatch &&
        liveTargetHubId === submitted.targetHubId &&
        liveAttachmentKey === submitted.attachmentKey;
      if (stillMatches) {
        clear();
        editor.commands.clearContent();
        setTagDraft("");
        close();
      }
    } catch (error) {
      // `useCreateMemory` opts out of the global toast via
      // `meta.skipGlobalToast`. Surface inline error UI inside the
      // modal — bar notifications would render BEHIND the modal at
      // z-bar-notif (30) vs the modal's z-modal (60). Reach for the
      // shared `classifyMutationError` so quota / rate-limit /
      // offline / forbidden cases get specific copy; fall back to the
      // generic `saveFailed` string.
      const classified = classifyMutationError(error, {
        action: t.errors.action.createMemory,
      });
      setSaveError(classified?.message ?? t.composeModal.saveFailed);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.shiftKey) {
      e.preventDefault();
      void handleSave();
    }
  };

  // Upload generation token — bumped every time the user stages a new
  // attachment OR removes one. The async upload promise checks its
  // captured generation against the current ref before writing back
  // to the draft; if they differ, the user has moved on and the result
  // is silently dropped. Without this, a slow upload from file A
  // resolving AFTER the user replaced it with file B would clobber B's
  // state with A's fileRef. Codex 5.5 H-finding.
  const uploadGenRef = useRef(0);

  // Stage a single binary file as the compose attachment and start the
  // R2 upload in the background. The chip UI shows progress; save is
  // gated on `attachment.status === "uploaded"` so a click during
  // upload doesn't ship a memory with an unset fileRef.
  //
  // Compose accepts ONE attachment per memory (matches SDK's
  // `memories.push({ fileRef })` shape). If the user already has an
  // attachment staged and drops/picks another, the new one replaces
  // it — same convention as bar's single-pasted-image flow. The old
  // upload's bytes are abandoned on the server (R2 cleans up
  // unreferenced objects via lifecycle policy) and any in-flight
  // result is gated by `uploadGenRef`.
  const stageAttachment = (file: File) => {
    if (!file) return;
    const gen = ++uploadGenRef.current;
    const contentType = detectUploadMimeType(file.name, true);
    setDraft({
      attachment: {
        fileName: file.name,
        fileSize: file.size,
        contentType,
        status: "uploading",
      },
    });
    void file
      .arrayBuffer()
      .then((buffer) =>
        uploadMemoryObject({
          name: file.name,
          bytes: new Uint8Array(buffer),
          contentType,
        }),
      )
      .then((fileRef) => {
        if (uploadGenRef.current !== gen) return;
        setDraft({
          attachment: {
            fileName: file.name,
            fileSize: file.size,
            contentType,
            status: "uploaded",
            fileRef,
          },
        });
      })
      .catch((err: unknown) => {
        if (uploadGenRef.current !== gen) return;
        const message = err instanceof Error ? err.message : String(err);
        setDraft({
          attachment: {
            fileName: file.name,
            fileSize: file.size,
            contentType,
            status: "error",
            errorMessage: message,
          },
        });
      });
  };

  // Drop handler — when user drops files onto the modal (popup OR
  // backdrop while modal is open):
  //
  //   - Text files: read inline and insert at the editor's cursor.
  //   - Binary files: stage the FIRST as the compose attachment
  //     (subsequent binary files in the same drop are ignored — compose
  //     is single-attachment by design).
  //
  // Gating: only fires on file drops (`types.includes("Files")`) so a
  // non-file drag (e.g., text drag-within the modal) flows through
  // to the editor's native drop handler instead of being canceled.
  //
  // `stopImmediatePropagation` on the native event prevents the bar's
  // window-level drop listener from also firing and staging the same
  // files in the bar — avoids a "drop one file, see it in two places"
  // UX bug. Backdrop wires the same handler so drops near-but-not-on
  // the popup also route to compose instead of leaking to the bar.
  const handleDrop = (e: React.DragEvent) => {
    if (!e.dataTransfer?.types.includes("Files")) return;
    e.preventDefault();
    e.nativeEvent.stopImmediatePropagation();
    if (!editor) return;
    const files = e.dataTransfer.files;
    if (!files || files.length === 0) return;
    // Stage the first binary File directly (no need to base64-roundtrip
    // it through processDroppedFiles when stageAttachment will re-read
    // its bytes anyway — extra base64 + decode is wasted work for
    // multi-MB images).
    const firstBinary = Array.from(files).find((f) => isBinaryFileName(f.name));
    if (firstBinary) stageAttachment(firstBinary);
    // Text files: still go through processDroppedFiles for the
    // folder-filter + read pipeline; insert at cursor. `skipBinary`
    // ensures binaries don't get base64-encoded inside the helper for
    // a result we'd just throw away. Codex 5.5 M-finding.
    void processDroppedFiles(files, { skipBinary: true }).then((result) => {
      const textItems = result.items.filter((item) => !item.binary);
      if (textItems.length === 0) return;
      const joined = textItems
        .map((item) => item.content.trimEnd())
        .join("\n\n");
      editor.commands.insertContent(joined);
    });
  };

  // File picker entry — clicked Paperclip in the footer triggers the
  // hidden <input type="file">. onChange stages the chosen file via
  // the same path as drop. Reset the input value after staging so the
  // user can re-pick the same filename if they remove and retry.
  const fileInputRef = useRef<HTMLInputElement>(null);
  const onFilePickerChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) stageAttachment(file);
    e.target.value = "";
  };
  const removeAttachment = () => {
    // Bump the upload generation so any in-flight resolve from the
    // current attachment becomes stale and is dropped (otherwise it
    // would re-introduce the chip moments after the user removed it).
    uploadGenRef.current++;
    setDraft({ attachment: undefined });
  };

  const handleDragOver = (e: React.DragEvent) => {
    // Required for drop events to fire — the browser default rejects
    // most drops unless dragover preventDefault'd at the target.
    if (e.dataTransfer?.types.includes("Files")) {
      e.preventDefault();
    }
  };

  return (
    // Route variant keeps Dialog.Root for focus-trap + Esc handling
    // but the Popup spans the full viewport with no centered glass
    // chrome, no backdrop scrim. open is hardcoded `true` because
    // the route IS the open state (mount = open). onOpenChange still
    // routes through close() so Esc + outside-click both navigate
    // back via router.
    <Dialog.Root
      open={isRoute ? true : open}
      onOpenChange={(next) => (next ? null : close())}
    >
      <Dialog.Portal>
        {isRoute ? null : (
          <Dialog.Backdrop
            className="fixed inset-0 z-modal bg-foreground/12 backdrop-blur-sm"
            onDragOver={handleDragOver}
            onDrop={handleDrop}
          />
        )}
        <Dialog.Popup
          aria-label={t.composeModal.title}
          className={
            isRoute
              ? "z-modal fixed inset-0 overflow-y-auto bg-background px-4 sm:px-6 pt-16 pb-32 outline-none"
              : "glass-panel z-modal fixed left-1/2 top-1/2 w-[min(640px,90vw)] -translate-x-1/2 -translate-y-1/2 rounded-surface p-5 outline-none"
          }
          onKeyDown={handleKeyDown}
          onDragOver={handleDragOver}
          onDrop={handleDrop}
        >
          <div className={isRoute ? "mx-auto max-w-2xl" : ""}>
            <header className="mb-3 flex items-center justify-between gap-2">
              <Dialog.Title className="text-[14px] font-medium text-fg-2 shrink-0">
                {t.composeModal.title}
              </Dialog.Title>
              <div className="flex items-center gap-2">
                {/* Hub picker — show the resolved target hub; click to
                  switch. Defaults to active hub; selection persists in
                  draft so users tied to a specific hub keep that intent
                  across modal close. Only render when 2+ hubs exist —
                  with one hub the choice is implicit. */}
                {hubs.length >= 2 && targetHubEntry ? (
                  <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
                    <PopoverTrigger
                      aria-label={interpolate(t.composeModal.targetHubAria, {
                        hub: targetHubName,
                      })}
                      // Mobile-friendly touch target: min-h-9 + larger
                      // padding + bigger badge/text so the chip is
                      // tappable without precision. Plan 26 follow-up
                      // ("hub drop down ... too small for mobile").
                      className="inline-flex min-h-9 items-center gap-2 rounded-full border border-border/60 bg-surface-1 px-3 py-1.5 text-[13px] text-fg-1 transition-colors hover:bg-surface-2 cursor-pointer"
                    >
                      <HubBadge
                        kind={
                          targetHubEntry.hub.hub_type === "team"
                            ? "team"
                            : "personal"
                        }
                        label={targetHubInitial}
                        accent={targetHubEntry.hub.accent}
                        size="md"
                      />
                      <span className="truncate max-w-35">{targetHubName}</span>
                      <ChevronDown
                        className="h-3.5 w-3.5 text-fg-3 shrink-0"
                        strokeWidth={2}
                      />
                    </PopoverTrigger>
                    <PopoverContent
                      side="bottom"
                      align="end"
                      sideOffset={6}
                      className="w-56 p-1"
                    >
                      {hubs.map(({ hub }) => {
                        const label = getHubDisplayName(hub, t, viewerIdentity);
                        const initial = getHubDisplayInitial(
                          hub,
                          t,
                          viewerIdentity,
                        );
                        const isSelected = hub.id === targetHubEntry.hub.id;
                        return (
                          <button
                            key={hub.id}
                            type="button"
                            onClick={() => {
                              setDraft({ targetHubId: hub.id });
                              setPickerOpen(false);
                            }}
                            className={`flex w-full items-center gap-2 rounded-chrome px-2 py-1.5 text-left text-[13px] transition-colors cursor-pointer ${
                              isSelected
                                ? "bg-surface-2 text-fg-1"
                                : "text-fg-2 hover:bg-surface-1"
                            }`}
                          >
                            <HubBadge
                              kind={
                                hub.hub_type === "team" ? "team" : "personal"
                              }
                              label={initial}
                              accent={hub.accent}
                              size="sm"
                            />
                            <span className="flex-1 truncate">{label}</span>
                            {isSelected && (
                              <Check
                                className="h-3.5 w-3.5 text-fg-2 shrink-0"
                                strokeWidth={2}
                              />
                            )}
                          </button>
                        );
                      })}
                    </PopoverContent>
                  </Popover>
                ) : null}
                <button
                  type="button"
                  onClick={close}
                  aria-label={t.composeModal.closeAria}
                  // 44px touch target (Apple HIG / WCAG 2.5.5) so the
                  // close X is reliably tappable on mobile. Plan 26
                  // follow-up.
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-fg-3 hover:bg-surface-2 hover:text-fg-1 transition-colors cursor-pointer"
                >
                  <X className="h-4 w-4" strokeWidth={2} />
                </button>
              </div>
            </header>

            <input
              type="text"
              value={draft.title}
              onChange={(e) => setDraft({ title: e.target.value })}
              placeholder={t.composeModal.titlePlaceholder}
              className="w-full bg-transparent text-[20px] font-semibold text-foreground placeholder:text-fg-4 outline-none mb-3"
              style={{ letterSpacing: "-0.015em" }}
              aria-label={t.composeModal.titleAria}
            />

            <div className="memax-prose min-h-[180px] max-h-[50vh] overflow-y-auto text-fg-1 text-[15px]">
              <EditorContent editor={editor} />
            </div>
            <MarkdownBubbleToolbar editor={editor} />

            {/* Tags row — chip cluster + free-text input. Enter or comma
              commits the typed value to the draft list; backspace on
              empty input pops the last tag (small QoL convention). */}
            <div className="mt-3 flex flex-wrap items-center gap-1.5">
              {draft.tags.map((tag) => (
                <Pill
                  key={tag}
                  variant="remove"
                  size="sm"
                  removeLabel={interpolate(t.composeModal.removeTagLabel, {
                    tag,
                  })}
                  onRemove={() => removeTag(tag)}
                >
                  {tag}
                </Pill>
              ))}
              <input
                type="text"
                value={tagDraft}
                onChange={(e) => setTagDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === ",") {
                    e.preventDefault();
                    commitTag(tagDraft);
                    return;
                  }
                  if (
                    e.key === "Backspace" &&
                    tagDraft === "" &&
                    draft.tags.length > 0
                  ) {
                    removeTag(draft.tags[draft.tags.length - 1]);
                  }
                }}
                placeholder={
                  draft.tags.length === 0
                    ? t.composeModal.tagsPlaceholder
                    : t.composeModal.tagsPlaceholderSubsequent
                }
                aria-label={t.composeModal.tagsAria}
                className="h-6 min-w-24 flex-1 bg-transparent text-[12px] text-fg-2 placeholder:text-fg-4 outline-none"
              />
            </div>

            {/* Attachment chip — shows the staged file's name + status.
              Click X to remove (cancels the in-flight upload's effect:
              the bytes may finish uploading to R2, but they won't be
              referenced by any memory and R2 lifecycle will reap). */}
            {draft.attachment ? (
              <div className="mt-3 inline-flex max-w-full items-center gap-2 rounded-full border border-border/60 bg-surface-1 px-2.5 py-1 text-[12px] text-fg-2">
                {draft.attachment.status === "uploading" ? (
                  <Loader2 className="h-3 w-3 shrink-0 animate-spin text-fg-3" />
                ) : draft.attachment.status === "error" ? (
                  <AlertCircle className="h-3 w-3 shrink-0 text-destructive" />
                ) : (
                  <Paperclip className="h-3 w-3 shrink-0 text-fg-3" />
                )}
                <span className="truncate" title={draft.attachment.fileName}>
                  {draft.attachment.fileName}
                </span>
                <span className="shrink-0 text-fg-4">
                  {draft.attachment.status === "uploading"
                    ? t.composeModal.attachmentUploading
                    : draft.attachment.status === "error"
                      ? t.composeModal.attachmentError
                      : null}
                </span>
                <button
                  type="button"
                  onClick={removeAttachment}
                  aria-label={interpolate(
                    t.composeModal.attachmentRemoveLabel,
                    {
                      file: draft.attachment.fileName,
                    },
                  )}
                  className="shrink-0 rounded-full p-0.5 text-fg-3 hover:bg-foreground/5 hover:text-fg-2 transition-colors cursor-pointer"
                >
                  <X className="h-3 w-3" strokeWidth={2.5} />
                </button>
              </div>
            ) : null}

            {saveError ? (
              <div
                role="alert"
                aria-live="polite"
                className="mt-3 rounded-chrome bg-destructive/10 px-3 py-2 text-[13px] text-destructive"
              >
                {saveError}
              </div>
            ) : null}

            {/* Hidden file input — clicked-through by the Paperclip
              button in the footer. No `accept` attribute (omitted
              rather than wildcarded) so any binary type the bar
              accepts is allowed; the actual MIME is detected from
              filename via `detectUploadMimeType`. */}
            <input
              ref={fileInputRef}
              type="file"
              className="hidden"
              onChange={onFilePickerChange}
              aria-hidden
              tabIndex={-1}
            />

            <footer className="mt-4 flex items-center justify-between gap-3 text-[12px] text-fg-3">
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  aria-label={t.composeModal.attachLabel}
                  className="rounded-chrome p-1.5 text-fg-3 hover:bg-foreground/5 hover:text-fg-2 transition-colors cursor-pointer"
                >
                  <Paperclip className="h-4 w-4" />
                </button>
                <span aria-hidden>{t.composeModal.hotkeyHint}</span>
              </div>
              <button
                type="button"
                onClick={() => void handleSave()}
                disabled={
                  createMemory.isPending ||
                  !draft.content.trim() ||
                  draft.attachment?.status === "uploading" ||
                  draft.attachment?.status === "error"
                }
                className="inline-flex items-center gap-1.5 rounded-chrome bg-foreground px-3 py-1.5 text-[13px] font-medium text-background transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40 cursor-pointer"
              >
                {createMemory.isPending ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : null}
                {t.composeModal.save}
              </button>
            </footer>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
