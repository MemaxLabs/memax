"use client";

import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { AttachmentViewURL } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";

export const attachmentViewURLKey = (memoryID: string, attachmentID: string) =>
  ["attachment-view-url", memoryID, attachmentID] as const;

/**
 * Fetches a short-lived signed URL for rendering an attachment inline.
 * Server-side TTL is 10 minutes; we refetch at ~9 minutes so the URL
 * stays ahead of expiry, but reuse the same URL across every mount
 * in between. This is what makes the same image render cheaply
 * across the inbox modal, memory detail, and lightbox.
 *
 * Errors (503 = signing disabled, 404 = attachment missing) are
 * surfaced to callers via `error`; callers should fall through to
 * the authenticated blob-download path when this returns no url.
 */
export function useAttachmentViewURL(
  memoryID: string | undefined,
  attachmentID: string | undefined,
  enabled = true,
): UseQueryResult<AttachmentViewURL, Error> {
  return useQuery<AttachmentViewURL, Error>({
    queryKey: attachmentViewURLKey(memoryID ?? "", attachmentID ?? ""),
    enabled: enabled && !!memoryID && !!attachmentID,
    queryFn: async () => {
      if (!memoryID || !attachmentID) throw new Error("missing ids");
      return getMemaxClient().memories.attachmentViewURL(
        memoryID,
        attachmentID,
      );
    },
    staleTime: 9 * 60 * 1000,
    gcTime: 10 * 60 * 1000,
    retry: false,
    refetchOnWindowFocus: false,
  });
}
