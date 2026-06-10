"use client";

/**
 * Compose surface — global modal for "considered capture" (titled,
 * multi-paragraph, markdown-formatted memories).
 *
 * The bar still owns "quick capture" (single-line, `⌘↵` saves what's
 * typed). Compose is the surface for "I want to write more than one
 * line" — opened via the global `⌘⇧↵` hotkey or by clicking the `+`
 * card in the v2 memories grid (plan 20 phase 2 wires the latter).
 *
 * Provider mounts once at the app shell root. Both shell-v1 and
 * shell-v2 see the same surface — the modal is shell-agnostic
 * (centered glass-panel, Esc / click-outside to dismiss).
 *
 * State held here:
 *   - `open`     — boolean, drives modal visibility
 *   - `draft`    — `{ title, content }`, persisted across open/close
 *                  within a session so accidental dismiss doesn't
 *                  destroy work. Reset on successful save.
 *
 * Actions:
 *   - `open()`        — show modal
 *   - `close()`       — hide modal, KEEP the draft
 *   - `clear()`       — wipe the draft (called after save)
 *   - `setDraft()`    — patch draft fields
 */

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { FileRef } from "@/hooks/use-memories";

/**
 * Single binary attachment staged on the compose draft. The SDK's
 * `memories.push` takes ONE `fileRef` per memory (the bar's pattern of
 * "each staged file becomes its own memory" doesn't apply to compose
 * since compose is one considered memory, not a stream of dumps), so
 * this is a single-attachment slot.
 *
 * The upload runs in the background as soon as the file is staged
 * (drop or file-picker). `status` tracks the lifecycle:
 *
 *   - `uploading` — uploads create + R2 PUT in flight
 *   - `uploaded`  — `fileRef` is populated; safe to save
 *   - `error`     — `errorMessage` set; user must remove and retry
 *
 * Save is gated on `status === "uploaded"` so a save click mid-upload
 * doesn't ship a memory with an unset fileRef.
 */
export interface ComposeAttachment {
  fileName: string;
  fileSize: number;
  contentType: string;
  status: "uploading" | "uploaded" | "error";
  fileRef?: FileRef;
  errorMessage?: string;
}

export interface ComposeDraft {
  title: string;
  content: string;
  /**
   * User-supplied tags. Stored as a string array so consumer code
   * matches the existing `Memory.tags: string[]` shape on the SDK.
   * Order is the order the user entered them in.
   */
  tags: string[];
  /**
   * Target hub for the save. Undefined means "use the active hub at
   * save time" (default behavior). Set when the user explicitly
   * picks a non-active hub from the modal's hub picker; persists
   * across modal close so a draft tied to a specific hub keeps that
   * intent.
   */
  targetHubId?: string;
  /**
   * Single binary attachment. See `ComposeAttachment` for lifecycle.
   * Persists across modal close — a user who drops a file, closes,
   * then reopens still sees the chip. Cleared on save (via `clear()`)
   * or by the user clicking the chip's remove button.
   */
  attachment?: ComposeAttachment;
}

export interface ComposeContextValue {
  open: boolean;
  draft: ComposeDraft;
  openModal: () => void;
  close: () => void;
  clear: () => void;
  setDraft: (patch: Partial<ComposeDraft>) => void;
}

const EMPTY_DRAFT: ComposeDraft = { title: "", content: "", tags: [] };

const ComposeContext = createContext<ComposeContextValue>({
  open: false,
  draft: EMPTY_DRAFT,
  openModal: () => {},
  close: () => {},
  clear: () => {},
  setDraft: () => {},
});

export function ComposeProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [draft, setDraftState] = useState<ComposeDraft>(EMPTY_DRAFT);

  const openModal = useCallback(() => setOpen(true), []);
  const close = useCallback(() => setOpen(false), []);
  const clear = useCallback(() => setDraftState(EMPTY_DRAFT), []);
  const setDraft = useCallback(
    (patch: Partial<ComposeDraft>) =>
      setDraftState((prev) => ({ ...prev, ...patch })),
    [],
  );

  const value = useMemo<ComposeContextValue>(
    () => ({ open, draft, openModal, close, clear, setDraft }),
    [open, draft, openModal, close, clear, setDraft],
  );

  return (
    <ComposeContext.Provider value={value}>{children}</ComposeContext.Provider>
  );
}

export function useCompose() {
  return useContext(ComposeContext);
}
