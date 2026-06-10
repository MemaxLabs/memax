"use client";

import type { KeyboardEventHandler, MutableRefObject } from "react";
import { Maximize2, Paperclip, X } from "lucide-react";
import { useLocale } from "@/i18n";
import { SendButton, type BarSendKind, type BarSendState } from "./send-button";

function readSelectedFile(
  file: File,
  onReady: (content: string, binary: boolean) => void,
) {
  const binary =
    file.type.startsWith("image/") || file.type === "application/pdf";
  const reader = new FileReader();
  reader.onload = () => {
    const raw = reader.result as string;
    const content = binary ? raw.split(",")[1] || raw : raw;
    onReady(content, binary);
  };
  if (binary) {
    reader.readAsDataURL(file);
  } else {
    reader.readAsText(file);
  }
}

export function MobileThumbBarRow({
  value,
  placeholder,
  inputRef,
  onFocus,
  onChange,
  onKeyDown,
  hasText,
  hasFiles,
  isExpanded,
  showClose = false,
  onClose,
  onExpand,
  onAddFiles,
  sendKind,
  sendState,
  onSend,
}: {
  value: string;
  placeholder: string;
  inputRef: MutableRefObject<HTMLTextAreaElement | null>;
  onFocus: () => void;
  onChange: (value: string) => void;
  onKeyDown: KeyboardEventHandler<HTMLTextAreaElement>;
  hasText: boolean;
  hasFiles: boolean;
  isExpanded: boolean;
  showClose?: boolean;
  onClose?: () => void;
  onExpand: () => void;
  onAddFiles: (
    files: {
      name: string;
      content: string;
      binary: boolean;
      contentType: string;
    }[],
  ) => void;
  sendKind: BarSendKind;
  sendState: BarSendState;
  onSend: () => void;
}) {
  const { t } = useLocale();

  return (
    <div className="flex w-full items-center gap-3">
      <div id="bar-logo-slot" className="flex shrink-0 items-center" />

      <div className="relative min-w-0 flex-1">
        {!value ? (
          <div className="pointer-events-none absolute inset-0 flex items-center overflow-hidden">
            <span className="truncate text-[16px] leading-[24px] text-fg-2">
              {placeholder}
            </span>
          </div>
        ) : null}
        <textarea
          ref={inputRef}
          value={value}
          onFocus={onFocus}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={onKeyDown}
          rows={1}
          className="block h-6 w-full resize-none overflow-hidden bg-transparent text-[16px] leading-[24px] text-foreground outline-none scrollbar-thin"
          style={{
            minHeight: 24,
            height: 24,
            lineHeight: "24px",
            paddingTop: 0,
            paddingBottom: 0,
            margin: 0,
          }}
        />
      </div>

      <div className="ml-auto flex shrink-0 items-center gap-1">
        {/* Paperclip — always available (iOS convention: attachment is persistent) */}
        {!hasFiles ? (
          <label
            className="flex min-h-[44px] min-w-[44px] shrink-0 cursor-pointer items-center justify-center text-fg-3 transition-colors hover:text-fg-2"
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
                readSelectedFile(file, (content, binary) => {
                  onAddFiles([
                    {
                      name: file.name,
                      content,
                      binary,
                      contentType: file.type || "application/octet-stream",
                    },
                  ]);
                });
                e.target.value = "";
              }}
            />
          </label>
        ) : null}

        {isExpanded && hasText ? (
          <button
            type="button"
            onClick={onExpand}
            className="flex min-h-[44px] min-w-[44px] shrink-0 items-center justify-center text-fg-3 transition-colors hover:text-fg-2"
            aria-label={t.bar.mobileExpand}
          >
            <Maximize2 className="h-4 w-4" />
          </button>
        ) : null}

        {showClose ? (
          <button
            type="button"
            onClick={onClose}
            className="flex min-h-[44px] min-w-[44px] shrink-0 items-center justify-center text-fg-3 transition-colors hover:text-fg-2"
            aria-label={t.bar.hint.close}
          >
            <X className="h-4 w-4" />
          </button>
        ) : null}

        <SendButton
          kind={sendKind}
          state={sendState}
          onClick={onSend}
          ariaLabel={
            sendKind === "recall" ? t.bar.hint.recall : t.bar.hint.remember
          }
        />
      </div>
    </div>
  );
}
