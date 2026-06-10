"use client";

import { Dialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import { useLocale } from "@/i18n";

interface Props {
  open: boolean;
  onClose: () => void;
  filename: string;
  /** Object URL for the already-local file. Caller owns the blob URL
   * lifecycle; this component does not create or revoke. */
  url: string | null;
}

/**
 * Lightweight preview dialog for a staged (not yet uploaded) image in
 * the bar. Reuses @base-ui/react's Dialog for focus trap, Esc, scroll
 * lock. The image is rendered from an already-local blob URL so no
 * signed-view-URL round trip is needed — the staged file is right
 * there in memory.
 */
export function StagedFilePreviewDialog({
  open,
  onClose,
  filename,
  url,
}: Props) {
  const { t } = useLocale();
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-takeover bg-black/80 backdrop-blur-sm transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <Dialog.Popup className="fixed inset-0 z-takeover flex flex-col outline-none transition-[opacity,transform] duration-150 ease-[cubic-bezier(0.16,1,0.3,1)] data-[ending-style]:scale-[0.97] data-[ending-style]:opacity-0 data-[starting-style]:scale-[0.97] data-[starting-style]:opacity-0">
          <div className="flex items-center justify-between px-4 py-3 text-white">
            <Dialog.Title className="min-w-0 truncate text-[13px] font-normal opacity-90">
              {filename}
            </Dialog.Title>
            <Dialog.Close
              aria-label={t.note.closePreview}
              className="flex h-9 w-9 items-center justify-center rounded-full bg-white/10 transition-colors hover:bg-white/20"
            >
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>
          <div className="relative flex flex-1 items-center justify-center px-4 pb-8">
            {url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={url}
                alt={filename}
                style={{ touchAction: "pinch-zoom" }}
                className="max-h-full max-w-full rounded-lg object-contain shadow-2xl"
              />
            ) : null}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
