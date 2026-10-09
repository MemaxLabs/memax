"use client";

import { useSearchParams, useRouter } from "next/navigation";
import { useState, useEffect, Suspense } from "react";
import { useCreateMemory } from "@/hooks/use-memories";
import { X } from "lucide-react";

function ShareContent() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const createMemory = useCreateMemory();
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  const title = searchParams.get("title") || "";
  const text = searchParams.get("text") || "";
  const url = searchParams.get("url") || "";

  // Build content from shared params
  const sharedContent = [text, url].filter(Boolean).join("\n\n");
  const sharedTitle = title || (url ? new URL(url).hostname : "Shared memory");

  const handleRemember = async () => {
    if (saving) return;
    setSaving(true);
    try {
      await createMemory.mutateAsync({
        title: sharedTitle,
        content: [sharedContent, note].filter(Boolean).join("\n\n"),
        source: "web",
        ...(url ? { reference_url: url, content_type: "link" as const } : {}),
      });
      setSaved(true);
      setTimeout(() => router.push("/home"), 1200);
    } catch {
      setSaving(false);
    }
  };

  // If no shared content, redirect to home
  useEffect(() => {
    if (!title && !text && !url) {
      router.replace("/home");
    }
  }, [title, text, url, router]);

  if (!title && !text && !url) return null;

  return (
    <div
      className="min-h-screen flex items-center justify-center px-4"
      style={{
        background: "var(--background)",
        paddingTop: "var(--safe-top, 0px)",
        paddingBottom: "var(--safe-bottom, 0px)",
      }}
    >
      <div
        className="w-full max-w-sm rounded-surface p-5"
        style={{
          background: "var(--card)",
          border: "1px solid var(--bar-border)",
          boxShadow: "var(--bar-shadow)",
        }}
      >
        {saved ? (
          <div className="text-center py-8">
            <div className="text-[18px] font-semibold text-foreground mb-1">
              Remembered.
            </div>
            <div className="text-[15px] text-fg-3">Redirecting to memax...</div>
          </div>
        ) : (
          <>
            {/* Header */}
            <div className="flex items-center justify-between mb-4">
              <h1 className="text-[18px] font-semibold text-foreground">
                Remember to memax
              </h1>
              <button
                onClick={() => router.back()}
                className="text-fg-3 active:text-fg-2 cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* Shared content preview */}
            <div
              className="rounded-surface p-3.5 mb-4"
              style={{
                background: "var(--muted)",
                border: "1px solid var(--border)",
              }}
            >
              <div className="text-[16px] font-medium text-foreground leading-snug line-clamp-2">
                {sharedTitle}
              </div>
              {url && (
                <div className="text-[14px] text-fg-3 mt-1 truncate">{url}</div>
              )}
              {text && !url && (
                <div className="text-[14px] text-fg-2 mt-1 line-clamp-3">
                  {text}
                </div>
              )}
            </div>

            {/* Optional note */}
            <textarea
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Add a note (optional)..."
              rows={2}
              className="w-full resize-none rounded-surface p-3 text-[16px] text-foreground outline-none mb-4"
              style={{
                background: "var(--muted)",
                border: "1px solid var(--border)",
              }}
            />

            {/* CTA */}
            <button
              onClick={handleRemember}
              disabled={saving}
              className="w-full py-3 rounded-surface text-[16px] font-semibold transition-colors"
              style={{
                background: "var(--foreground)",
                color: "var(--background)",
                opacity: saving ? 0.6 : 1,
              }}
            >
              {saving ? "Remembering..." : "Remember ↓"}
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export default function SharePage() {
  return (
    <Suspense
      fallback={
        <div
          className="min-h-screen flex items-center justify-center"
          style={{ background: "var(--background)" }}
        >
          <div className="text-[15px] text-fg-3">Loading...</div>
        </div>
      }
    >
      <ShareContent />
    </Suspense>
  );
}
