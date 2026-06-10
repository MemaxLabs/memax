"use client";

import { getMemaxClient } from "@/lib/memax-client";
import type { FileRef } from "@/hooks/use-memories";

const PRESIGNED_UPLOAD_THRESHOLD_BYTES = 512 * 1024;

export function shouldUsePresignedUpload(
  sizeBytes: number,
  binary: boolean,
): boolean {
  return binary || sizeBytes > PRESIGNED_UPLOAD_THRESHOLD_BYTES;
}

export async function uploadMemoryObject(params: {
  name: string;
  bytes: Uint8Array;
  contentType: string;
  signal?: AbortSignal;
}): Promise<FileRef> {
  const sha256 = await sha256Hex(params.bytes);
  const payload = toArrayBuffer(params.bytes);
  const intent = await getMemaxClient().uploads.create({
    filename: params.name,
    content_type: params.contentType,
    size_bytes: params.bytes.byteLength,
    purpose: "memory_attachment",
  });

  const res = await fetch(intent.upload_url, {
    method: "PUT",
    headers: intent.headers ?? { "Content-Type": params.contentType },
    body: new Blob([payload], { type: params.contentType }),
    signal: params.signal,
  });
  if (!res.ok) {
    // S3/R2 return XML error bodies; include the first 200 chars so the
    // UI shows something actionable (e.g. "EntityTooLarge" or a content-
    // length mismatch) instead of a bare HTTP status. Without this, the
    // content-length-signature rejection on oversize uploads surfaces as
    // a generic "upload failed with status 403".
    let detail = "";
    try {
      const body = (await res.text()).trim();
      if (body) detail = ` — ${body.slice(0, 200)}`;
    } catch {
      /* body unreadable; fall through with status only */
    }
    throw new Error(`upload failed with status ${res.status}${detail}`);
  }

  return {
    object_key: intent.object_key,
    filename: intent.filename,
    content_type: intent.content_type,
    size_bytes: params.bytes.byteLength,
    sha256,
  };
}

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", toArrayBuffer(bytes));
  return Array.from(new Uint8Array(digest))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.slice().buffer as ArrayBuffer;
}

export function decodeBinaryBase64(base64: string): Uint8Array {
  const bin = atob(base64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

export function encodeTextBytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

export function detectUploadMimeType(
  name: string,
  binary: boolean,
  memoryContentType?: string,
): string {
  const lower = name.toLowerCase();
  if (lower.endsWith(".pdf")) return "application/pdf";
  if (lower.endsWith(".png")) return "image/png";
  if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) return "image/jpeg";
  if (lower.endsWith(".webp")) return "image/webp";
  if (lower.endsWith(".gif")) return "image/gif";
  if (lower.endsWith(".bmp")) return "image/bmp";
  if (lower.endsWith(".svg")) return "image/svg+xml";
  if (lower.endsWith(".md")) return "text/markdown";
  if (lower.endsWith(".html") || lower.endsWith(".htm")) return "text/html";
  if (binary) return "application/octet-stream";
  if (memoryContentType === "markdown") return "text/markdown";
  return "text/plain";
}
