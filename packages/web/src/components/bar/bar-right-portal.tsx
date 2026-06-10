"use client";

import { createPortal } from "react-dom";
import { Maximize2, Paperclip, X } from "lucide-react";
import { useBar } from "@/contexts/bar-context";
import { SendButton } from "./send-button";
import { useLocale } from "@/i18n";

export function BarRightPortal() {
  const {
    selectedMemory,
    memorySearch,
    setMemorySearch,
    setValue,
    phase,
    value,
    sendKind,
    recallQuery,
    inputRef,
    handleSubmit,
    reset,
    stagedFiles,
    allFilesReady,
    addStagedFiles,
    isMobile,
    pushing,
    isBarExpanded,
    openMobileCompose,
    interaction,
  } = useBar();
  const { t } = useLocale();

  const slot =
    typeof document !== "undefined"
      ? document.getElementById("bar-right-slot")
      : null;
  if (!slot || (isMobile && interaction.mobileComposeState !== "docked"))
    return null;

  if (selectedMemory) {
    return createPortal(
      memorySearch ? (
        <button
          type="button"
          onClick={() => {
            setMemorySearch("");
            setValue("");
          }}
          className="cursor-pointer text-fg-2 transition-colors hover:text-fg-1"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      ) : null,
      slot,
    );
  }

  const hasText = interaction.hasText;
  const hasFiles = interaction.hasFiles;
  const filesUploading = interaction.filesUploading;
  const sendState = interaction.sendState;

  const showCancel = phase === "loading" || phase === "recall-result";
  const hasEditedRecall = phase === "recall-result" && value !== recallQuery;
  const showAttachment = isMobile && !hasText && !hasFiles;
  const showExpand =
    isMobile &&
    hasText &&
    isBarExpanded &&
    interaction.mobileComposeState !== "fullscreen";

  return createPortal(
    <div className="flex items-center gap-1.5">
      {showAttachment ? (
        <label
          className="cursor-pointer p-1 text-fg-3 transition-colors hover:text-fg-2"
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
      ) : null}

      {showExpand ? (
        <button
          type="button"
          onClick={openMobileCompose}
          className="cursor-pointer p-1 text-fg-3 transition-colors hover:text-fg-2"
          aria-label={t.bar.mobileExpand}
        >
          <Maximize2 className="h-3.5 w-3.5" />
        </button>
      ) : null}

      {showCancel ? (
        <button
          type="button"
          onClick={() => {
            if (hasEditedRecall) {
              setValue(recallQuery);
              inputRef.current?.focus();
            } else {
              reset();
            }
          }}
          className="cursor-pointer p-1 text-fg-3 transition-colors hover:text-fg-2"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      ) : null}

      <SendButton
        kind={sendKind}
        state={sendState}
        onClick={handleSubmit}
        ariaLabel={
          sendKind === "recall" ? t.bar.hint.recall : t.bar.hint.remember
        }
      />
    </div>,
    slot,
  );
}
