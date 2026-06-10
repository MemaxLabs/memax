"use client";

import { useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import {
  FileText,
  Image as ImageIcon,
  Link2,
  Loader2,
  AlertCircle,
} from "lucide-react";
import { useBar } from "@/contexts/bar-context";
import type { FileUploadStatus, StagedFile } from "@/lib/bar-types";
import { useLocale } from "@/i18n";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { StagedFilePreviewDialog } from "./staged-file-preview-dialog";

const MAX_VISIBLE = 3;

// Client-side whitelist matches the backend inline whitelist in
// packages/server/internal/attachments/inline.go. SVG and HEIC are
// deliberately excluded — SVG is scriptable, HEIC has uneven browser
// support. Keep these in sync.
const RASTER_INLINE_WHITELIST = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
]);

function isInlineRaster(contentType: string): boolean {
  const media = contentType.split(";")[0]?.trim().toLowerCase() ?? "";
  return RASTER_INLINE_WHITELIST.has(media);
}

/** Inline file chips + URL chip — shown above the input row. */
export function InlineFileChips() {
  const {
    stagedFiles,
    removeStagedFile,
    clearStagedFiles,
    detectedUrl,
    clearDetectedUrl,
  } = useBar();
  const { t } = useLocale();
  // Parent owns preview state so a single dialog serves every chip; the
  // staged file carries its own blob URL, which StagedChip passes up.
  const [previewId, setPreviewId] = useState<string | null>(null);
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [previewFilename, setPreviewFilename] = useState<string>("");

  const hasFiles = stagedFiles.length > 0;
  const hasUrl = !!detectedUrl;

  if (!hasFiles && !hasUrl) return null;

  const visible = stagedFiles.slice(0, MAX_VISIBLE);
  const overflow = stagedFiles.length - MAX_VISIBLE;

  const openPreview = (file: StagedFile, url: string) => {
    setPreviewId(file.id);
    setPreviewUrl(url);
    setPreviewFilename(file.name);
  };
  const closePreview = () => setPreviewId(null);

  return (
    <motion.div
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: "auto" }}
      exit={{ opacity: 0, height: 0 }}
      transition={{ duration: FAST, ease: EASE }}
      className="overflow-hidden"
    >
      <div className="flex items-center gap-2 px-5 sm:px-6 py-2 border-b border-border/30 flex-wrap">
        {visible.map((f) => (
          <StagedChip
            key={f.id}
            file={f}
            onRemove={() => removeStagedFile(f.id)}
            onPreview={openPreview}
          />
        ))}

        {/* Overflow badge */}
        {overflow > 0 && (
          <span className="text-[13px] text-fg-3 shrink-0">+{overflow}</span>
        )}

        {/* Clear all */}
        {stagedFiles.length > 1 && (
          <button
            onClick={clearStagedFiles}
            className="text-fg-4 hover:text-fg-2 transition-colors cursor-pointer shrink-0"
          >
            ×
          </button>
        )}

        {/* URL chip — auto-detected pasted URL (kitchen 24g) */}
        {hasUrl && (
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-surface-1 text-[12px] text-fg-2">
              <Link2 className="h-3 w-3 text-fg-3" />
              <span className="truncate max-w-48">
                {detectedUrl!.replace(/^https?:\/\//, "")}
              </span>
              <button
                onClick={clearDetectedUrl}
                className="text-fg-4 hover:text-fg-2 ml-0.5 cursor-pointer"
              >
                ×
              </button>
            </div>
            <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded cursor-pointer hover:text-fg-2">
              Capture page
            </span>
          </div>
        )}
      </div>

      <StagedFilePreviewDialog
        open={previewId !== null}
        onClose={closePreview}
        filename={previewFilename}
        url={previewUrl}
      />
    </motion.div>
  );
}

// StagedChip renders one staged file. For raster-whitelisted images
// that finished uploading (or are still uploading — base64 is already
// local), we render a real thumbnail via URL.createObjectURL. For
// everything else we keep the existing icon-only chip.
//
// The blob URL is memoized per file id and revoked on unmount, so add/
// remove cycles don't leak. If content ever changes in place (it
// shouldn't — staged files are immutable after creation), the memo
// key would need to widen.
function StagedChip({
  file,
  onRemove,
  onPreview,
}: {
  file: StagedFile;
  onRemove: () => void;
  onPreview: (file: StagedFile, url: string) => void;
}) {
  const { t } = useLocale();
  const showThumbnail =
    file.binary && isInlineRaster(file.contentType) && file.status !== "error";

  const thumbnailUrl = useMemo(() => {
    if (!showThumbnail) return null;
    try {
      const binaryString = atob(file.content);
      const bytes = new Uint8Array(binaryString.length);
      for (let i = 0; i < binaryString.length; i++) {
        bytes[i] = binaryString.charCodeAt(i);
      }
      return URL.createObjectURL(new Blob([bytes], { type: file.contentType }));
    } catch {
      return null;
    }
  }, [showThumbnail, file.content, file.contentType]);

  useEffect(() => {
    if (!thumbnailUrl) return;
    return () => URL.revokeObjectURL(thumbnailUrl);
  }, [thumbnailUrl]);

  if (thumbnailUrl) {
    return (
      <div className="relative shrink-0 group">
        <button
          type="button"
          onClick={() => onPreview(file, thumbnailUrl)}
          aria-label={t.note.openPreview}
          className="block h-12 w-12 rounded-lg overflow-hidden bg-surface-1 border border-border/40 relative cursor-pointer"
        >
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={thumbnailUrl}
            alt={file.name}
            className="h-full w-full object-cover"
          />
          {/* Upload status overlay — mirrors the chip icon semantics */}
          {file.status === "pending" || file.status === "uploading" ? (
            <div className="absolute inset-0 flex items-center justify-center bg-black/40">
              <Loader2 className="h-4 w-4 text-white animate-spin" />
            </div>
          ) : null}
        </button>
        {/* Remove control: always visible on touch (no hover) per
            (hover: none) media query; dims into hover/focus on
            pointer devices so desktop chrome stays calm. Covers
            mobile fullscreen compose where the chip now lives. */}
        <button
          onClick={onRemove}
          aria-label={t.bar.removeAttachment}
          className="absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-fg-1 text-background shadow-md transition-opacity [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover:opacity-100 [@media(hover:hover)]:focus:opacity-100"
        >
          <span className="text-[11px] leading-none">×</span>
        </button>
      </div>
    );
  }

  const isImageByName =
    file.binary && !file.name.toLowerCase().endsWith(".pdf");
  return (
    <div className="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-surface-1 text-[12px] text-fg-2 max-w-32 shrink-0">
      <ChipIcon status={file.status} isImage={isImageByName} />
      <span className="truncate">{file.name}</span>
      <button
        onClick={onRemove}
        className="text-fg-4 hover:text-fg-2 ml-0.5 cursor-pointer"
      >
        ×
      </button>
    </div>
  );
}

/** Chip icon — shows upload status or file type */
function ChipIcon({
  status,
  isImage,
}: {
  status: FileUploadStatus;
  isImage: boolean;
}) {
  switch (status) {
    case "pending":
    case "uploading":
      return <Loader2 className="h-3 w-3 text-fg-3 shrink-0 animate-spin" />;
    case "error":
      return <AlertCircle className="h-3 w-3 text-destructive shrink-0" />;
    case "uploaded":
      if (isImage) {
        return <ImageIcon className="h-3 w-3 text-fg-3 shrink-0" />;
      }
      return <FileText className="h-3 w-3 text-fg-3 shrink-0" />;
  }
}
