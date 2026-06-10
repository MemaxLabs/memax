"use client";

import { createPortal } from "react-dom";
import { ArrowLeft, Paperclip } from "lucide-react";
import { useBar } from "@/contexts/bar-context";
import { useLocale } from "@/i18n";
import { SendButton } from "./send-button";
import { InlineFileChips } from "./inline-file-chips";
import { MOBILE_OVERLAY_CONTENT_TOP } from "@/lib/layout";
import { useVisualViewportRect } from "@/hooks/use-visual-viewport-rect";

export function MobileComposeOverlay() {
  const {
    isMobile,
    closeMobileCompose,
    value,
    handleChange,
    handleKeyDown,
    handleSubmit,
    inputRef,
    sendKind,
    phase,
    stagedFiles,
    allFilesReady,
    addStagedFiles,
    pushing,
    setIsBarExpanded,
    setMobileComposeActive,
    interaction,
  } = useBar();
  const { t } = useLocale();
  const viewportRect = useVisualViewportRect();

  const slot = typeof document !== "undefined" ? document.body : null;
  if (!slot || !isMobile || interaction.mobileComposeState !== "fullscreen")
    return null;

  const hasText = value.trim().length > 0;
  const hasFiles = stagedFiles.length > 0;
  const filesUploading = hasFiles && !allFilesReady;
  const sendState =
    phase === "loading" || pushing || filesUploading
      ? "loading"
      : hasText || hasFiles
        ? "ready"
        : "disabled";

  return createPortal(
    <div
      className="fixed left-0 right-0 z-takeover bg-background"
      style={{
        top: viewportRect?.top ?? 0,
        height: viewportRect ? `${viewportRect.height}px` : "100dvh",
      }}
    >
      <div
        className="absolute inset-0 flex flex-col bg-background"
        style={{ paddingTop: MOBILE_OVERLAY_CONTENT_TOP }}
      >
        <div className="flex items-center gap-1 border-b border-border/30 px-2 pb-3">
          <button
            type="button"
            onClick={() => {
              closeMobileCompose();
              setIsBarExpanded(true);
            }}
            className="flex min-h-[44px] min-w-[44px] cursor-pointer items-center justify-center text-fg-2 transition-colors hover:text-fg-1"
            aria-label={t.bar.mobileBack}
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <div className="min-w-0 flex-1 text-[14px] font-medium text-fg-2">
            {sendKind === "remember" ? t.bar.mobileDumping : t.bar.mobileAsking}
          </div>
          <label
            className="flex min-h-[44px] min-w-[44px] cursor-pointer items-center justify-center text-fg-3 transition-colors hover:text-fg-2"
            aria-label={t.bar.mobileAddAttachment}
          >
            <Paperclip className="h-4 w-4" />
            <input
              type="file"
              accept="image/*,application/pdf,.md,.txt"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (!file) return;
                const binary =
                  file.type.startsWith("image/") ||
                  file.type === "application/pdf";
                const reader = new FileReader();
                reader.onload = () => {
                  const raw = reader.result as string;
                  const content = binary ? raw.split(",")[1] || raw : raw;
                  addStagedFiles([
                    {
                      name: file.name,
                      content,
                      binary,
                      contentType: file.type || "application/octet-stream",
                    },
                  ]);
                };
                if (binary) {
                  reader.readAsDataURL(file);
                } else {
                  reader.readAsText(file);
                }
                e.target.value = "";
              }}
            />
          </label>
          <SendButton
            kind={sendKind}
            state={sendState}
            onClick={() => {
              handleSubmit();
              if (sendKind === "remember") {
                setMobileComposeActive(false);
              }
              closeMobileCompose();
            }}
            ariaLabel={
              sendKind === "recall" ? t.bar.hint.recall : t.bar.hint.remember
            }
          />
        </div>

        <InlineFileChips />

        <div className="min-h-0 flex-1 px-4 py-4">
          <textarea
            ref={inputRef}
            value={value}
            onChange={(e) => handleChange(e.target.value)}
            onKeyDown={handleKeyDown}
            rows={12}
            className="h-full w-full resize-none bg-transparent text-[16px] leading-6 text-foreground outline-none"
            placeholder={t.bar.placeholder.default}
            autoFocus
          />
        </div>
      </div>
    </div>,
    slot,
  );
}
