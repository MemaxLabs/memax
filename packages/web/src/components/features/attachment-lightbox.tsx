"use client";

import { useCallback, useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { useLocale } from "@/i18n";
import type { MemoryAttachment } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useAttachmentViewURL } from "@/hooks/use-attachment-view-url";

interface Props {
  memoryID: string;
  attachments: MemoryAttachment[];
  openIndex: number | null;
  onClose: () => void;
}

/**
 * Full-screen lightbox for image attachments. Uses @base-ui/react's
 * Dialog primitive so we get focus trap, focus restoration, aria-modal,
 * Esc-to-close, and scroll locking for free — the earlier custom
 * overlay dropped all of those. The parent controls open state via
 * openIndex (null = closed), which maps to Dialog.Root's `open`
 * prop; Dialog drives the exit transition via data-[ending-style]
 * (set while closing) and keeps the popup mounted until the CSS
 * transition completes, so unmounting on close is handled by the
 * primitive, not by the caller.
 *
 * Expects callers to pass ONLY inline-eligible raster images — no
 * re-eligibility check here. Keyboard nav (← →) is additive to the
 * Dialog's built-in Esc handler. Mobile pinch-zoom is delegated to
 * the browser via `touch-action: pinch-zoom` on the image.
 */
export function AttachmentLightbox({
  memoryID,
  attachments,
  openIndex,
  onClose,
}: Props) {
  const { t } = useLocale();
  const [index, setIndex] = useState(openIndex ?? 0);

  useEffect(() => {
    if (openIndex !== null) setIndex(openIndex);
  }, [openIndex]);

  const count = attachments.length;
  const canNavigate = count > 1;
  const current = attachments[index];

  const goPrev = useCallback(() => {
    if (!canNavigate) return;
    setIndex((i) => (i - 1 + count) % count);
  }, [canNavigate, count]);
  const goNext = useCallback(() => {
    if (!canNavigate) return;
    setIndex((i) => (i + 1) % count);
  }, [canNavigate, count]);

  useEffect(() => {
    if (openIndex === null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        goPrev();
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        goNext();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [openIndex, goPrev, goNext]);

  const open = openIndex !== null;

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onClose();
      }}
    >
      <Dialog.Portal>
        {/* Base UI transition contract: `data-starting-style` is set
            pre-paint on open and `data-ending-style` is set during
            close. Steady state has neither, so CSS `transition`
            interpolates between offset (starting/ending) and at-rest
            (default). The primitive keeps the popup mounted through
            the exit transition, which is what fixes the dead-
            AnimatePresence problem the earlier custom overlay had. */}
        <Dialog.Backdrop className="fixed inset-0 z-takeover bg-black/80 backdrop-blur-sm transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup className="fixed inset-0 z-takeover flex flex-col outline-none transition-[opacity,transform] duration-150 ease-[cubic-bezier(0.16,1,0.3,1)] data-[ending-style]:scale-[0.97] data-[ending-style]:opacity-0 data-[starting-style]:scale-[0.97] data-[starting-style]:opacity-0">
          {current ? (
            <>
              <div className="flex items-center justify-between px-4 py-3 text-white">
                <Dialog.Title className="min-w-0 text-[13px] opacity-90 truncate font-normal">
                  {current.filename}
                  {count > 1 ? (
                    <span className="ml-2 opacity-70">
                      {index + 1} / {count}
                    </span>
                  ) : null}
                </Dialog.Title>
                <div className="flex items-center gap-2">
                  <DownloadButton
                    memoryID={memoryID}
                    attachment={current}
                    label={t.note.download}
                  />
                  <Dialog.Close
                    aria-label={t.note.closePreview}
                    className="flex h-9 w-9 items-center justify-center rounded-full bg-white/10 hover:bg-white/20 transition-colors"
                  >
                    <X className="h-4 w-4" />
                  </Dialog.Close>
                </div>
              </div>

              <div className="relative flex-1 flex items-center justify-center px-4 pb-8">
                {canNavigate ? (
                  <button
                    type="button"
                    aria-label={t.note.previousImage}
                    onClick={goPrev}
                    className="absolute left-4 top-1/2 -translate-y-1/2 flex h-10 w-10 items-center justify-center rounded-full bg-white/10 text-white hover:bg-white/20 transition-colors"
                  >
                    <ChevronLeft className="h-5 w-5" />
                  </button>
                ) : null}

                <LightboxImage
                  key={current.id}
                  memoryID={memoryID}
                  attachment={current}
                />

                {canNavigate ? (
                  <button
                    type="button"
                    aria-label={t.note.nextImage}
                    onClick={goNext}
                    className="absolute right-4 top-1/2 -translate-y-1/2 flex h-10 w-10 items-center justify-center rounded-full bg-white/10 text-white hover:bg-white/20 transition-colors"
                  >
                    <ChevronRight className="h-5 w-5" />
                  </button>
                ) : null}
              </div>
            </>
          ) : null}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function LightboxImage({
  memoryID,
  attachment,
}: {
  memoryID: string;
  attachment: MemoryAttachment;
}) {
  const { data } = useAttachmentViewURL(memoryID, attachment.id);
  if (!data) {
    return <div className="h-24 w-24 rounded-full bg-white/5 animate-pulse" />;
  }
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={data.url}
      alt={attachment.filename}
      style={{ touchAction: "pinch-zoom" }}
      className="max-h-full max-w-full object-contain rounded-lg shadow-2xl"
    />
  );
}

function DownloadButton({
  memoryID,
  attachment,
  label,
}: {
  memoryID: string;
  attachment: MemoryAttachment;
  label: string;
}) {
  const [busy, setBusy] = useState(false);
  const onClick = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const res = await getMemaxClient().memories.downloadAttachment(
        memoryID,
        attachment.id,
      );
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = attachment.filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } finally {
      setBusy(false);
    }
  };
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={busy}
      aria-label={label}
      className="flex h-9 items-center gap-1.5 rounded-full bg-white/10 px-3 text-[13px] hover:bg-white/20 transition-colors disabled:opacity-60"
    >
      <Download className="h-3.5 w-3.5" />
      <span>{label}</span>
    </button>
  );
}
