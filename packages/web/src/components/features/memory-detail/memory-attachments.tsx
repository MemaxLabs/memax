"use client";

import { useState } from "react";
import { Download, Paperclip } from "lucide-react";
import { getMemaxClient } from "@/lib/memax-client";
import { useAttachmentViewURL } from "@/hooks/use-attachment-view-url";
import type { Memory } from "@/hooks/use-memories";
import type { MemoryAttachment } from "memax-sdk";
import { useLocale } from "@/i18n";
import { AttachmentLightbox } from "../attachment-lightbox";

// Must stay in sync with packages/server/internal/attachments/inline.go
// and inline-file-chips.tsx. SVG is excluded (scriptable). HEIC is
// deferred to phase 2 (uneven browser support). Webp is deferred to
// phase 2 (needs golang.org/x/image/webp on the server).
const RASTER_INLINE_WHITELIST = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
]);

function isInlineRaster(contentType: string): boolean {
  const media = contentType.split(";")[0]?.trim().toLowerCase() ?? "";
  return RASTER_INLINE_WHITELIST.has(media);
}

// Defense in depth: require BOTH the server-side inline_eligible flag
// AND a raster content_type client-side. Either one alone is not
// enough if a migration left rows partially populated or the
// whitelist drifts between client and server.
function isImagePreviewable(a: MemoryAttachment): boolean {
  return a.inline_eligible === true && isInlineRaster(a.content_type);
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function MemoryAttachments({ memory }: { memory: Memory }) {
  const { t } = useLocale();
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const attachments = memory.attachments ?? [];

  if (attachments.length === 0) return null;

  const imageAttachments = attachments.filter(isImagePreviewable);

  return (
    <section className="mt-6">
      <div className="text-[12px] uppercase tracking-[0.12em] text-fg-3 mb-2">
        {t.note.attachments}
      </div>
      <div className="space-y-2">
        {attachments.map((attachment) => {
          if (isImagePreviewable(attachment)) {
            const imageIdx = imageAttachments.findIndex(
              (a) => a.id === attachment.id,
            );
            return (
              <ImageAttachmentCard
                key={attachment.id}
                memoryID={memory.id}
                attachment={attachment}
                onOpen={() => setLightboxIndex(imageIdx)}
              />
            );
          }
          return (
            <FileAttachmentCard
              key={attachment.id}
              memoryID={memory.id}
              attachment={attachment}
            />
          );
        })}
      </div>

      {imageAttachments.length > 0 ? (
        <AttachmentLightbox
          memoryID={memory.id}
          attachments={imageAttachments}
          openIndex={lightboxIndex}
          onClose={() => setLightboxIndex(null)}
        />
      ) : null}
    </section>
  );
}

function ImageAttachmentCard({
  memoryID,
  attachment,
  onOpen,
}: {
  memoryID: string;
  attachment: MemoryAttachment;
  onOpen: () => void;
}) {
  const { t } = useLocale();
  const { data, error } = useAttachmentViewURL(memoryID, attachment.id);

  // If signing is disabled (503) or the attachment has gone missing
  // (404), fall back to the plain download card. This is the graceful-
  // degradation path for deployments that haven't set
  // ATTACHMENT_VIEW_SIGNING_KEY — the feature collapses to pre-feature
  // UX instead of an infinite skeleton.
  if (error) {
    return <FileAttachmentCard memoryID={memoryID} attachment={attachment} />;
  }

  // Aspect ratio from upload-time probe — reserves layout space so
  // the image load doesn't push content around. Legacy rows without
  // dimensions fall back to 16/9 until the image loads.
  const width = attachment.width ?? undefined;
  const height = attachment.height ?? undefined;
  const aspectStyle =
    width && height
      ? { aspectRatio: `${width} / ${height}` }
      : { aspectRatio: "16 / 9" };

  return (
    <div className="glass-card overflow-hidden rounded-surface">
      <button
        type="button"
        onClick={onOpen}
        disabled={!data}
        aria-label={t.note.openPreview}
        className="group relative block w-full max-w-2xl bg-foreground/[0.03] disabled:cursor-default"
        style={aspectStyle}
      >
        {data ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={data.url}
            alt={attachment.filename}
            width={width}
            height={height}
            loading="lazy"
            decoding="async"
            className="h-full w-full object-contain transition-transform duration-300 group-hover:scale-[1.01]"
          />
        ) : (
          <div className="h-full w-full animate-pulse bg-foreground/[0.04]" />
        )}
      </button>
      <div className="flex items-center gap-3 px-3 py-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13px] font-medium text-fg-1">
            {attachment.filename || t.note.originalFile}
          </div>
          <div className="text-[12px] text-fg-3">
            {attachment.size_bytes > 0
              ? formatSize(attachment.size_bytes)
              : t.note.originalFile}
          </div>
        </div>
        <DownloadIconButton
          memoryID={memoryID}
          attachment={attachment}
          label={t.note.download}
          downloadingLabel={t.note.downloading}
          compact
        />
      </div>
    </div>
  );
}

function FileAttachmentCard({
  memoryID,
  attachment,
}: {
  memoryID: string;
  attachment: MemoryAttachment;
}) {
  const { t } = useLocale();
  return (
    <div className="glass-card flex items-center gap-3 rounded-surface px-3 py-2.5">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-chrome bg-foreground/[0.04] text-fg-3">
        <Paperclip className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[14px] font-medium text-fg-1">
          {attachment.filename || t.note.originalFile}
        </div>
        <div className="text-[12px] text-fg-3">
          {attachment.size_bytes > 0
            ? formatSize(attachment.size_bytes)
            : t.note.originalFile}
        </div>
      </div>
      <DownloadIconButton
        memoryID={memoryID}
        attachment={attachment}
        label={t.note.download}
        downloadingLabel={t.note.downloading}
      />
    </div>
  );
}

function DownloadIconButton({
  memoryID,
  attachment,
  label,
  downloadingLabel,
  compact = false,
}: {
  memoryID: string;
  attachment: MemoryAttachment;
  label: string;
  downloadingLabel: string;
  compact?: boolean;
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
  if (compact) {
    return (
      <button
        type="button"
        onClick={onClick}
        disabled={busy}
        aria-label={busy ? downloadingLabel : label}
        className="flex h-8 w-8 items-center justify-center rounded-full text-fg-3 transition-colors hover:bg-foreground/[0.05] hover:text-foreground disabled:cursor-default disabled:opacity-60"
      >
        <Download className="h-3.5 w-3.5" />
      </button>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={busy}
      className="inline-flex items-center gap-1.5 rounded-chrome border border-border/80 px-2.5 py-1.5 text-[13px] text-fg-2 transition-colors hover:bg-foreground/[0.05] hover:text-foreground disabled:cursor-default disabled:opacity-60"
    >
      <Download className="h-3.5 w-3.5" />
      <span>{busy ? downloadingLabel : label}</span>
    </button>
  );
}
