export type BarNotificationType =
  | "dream"
  | "dreaming"
  | "dream_complete"
  | "dream_clean"
  | "dream_partial"
  | "update"
  | "info"
  | "success"
  | "error";

export interface BarNotification {
  type: BarNotificationType;
  message: string;
  detail?: string;
  /** Text label for action button (e.g., "Undo", "View"). Falls back to chevron icon if omitted. */
  actionLabel?: string;
  onAction?: () => void;
  onDismiss?: () => void;
  /** Auto-dismiss after N ms (0 = manual only) */
  autoDismissMs?: number;
}

/** Eager-upload file staging (ChatGPT pattern): upload starts on drop/paste, not on send. */
export type FileUploadStatus = "pending" | "uploading" | "uploaded" | "error";
export interface StagedFile {
  id: string;
  name: string;
  content: string;
  binary: boolean;
  size: number;
  /** MIME type captured at staging time from the File object. Required so
   * downstream UI (inline thumbnail gate, attachment whitelist) decides
   * from real metadata rather than extension guesses. Defaults to
   * "application/octet-stream" if the browser reported no type. */
  contentType: string;
  status: FileUploadStatus;
  /** Set once upload completes — used when creating memory on send */
  fileRef?: import("@/hooks/use-memories").FileRef;
  error?: string;
}
