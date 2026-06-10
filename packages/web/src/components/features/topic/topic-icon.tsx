"use client";

// Re-export of the @memaxlabs/ui primitive. Kept here so existing
// web-side imports at "@/components/features/topic/topic-icon" continue to
// work without sweeping every callsite. The icon map lives in the
// design system package so kitchen demos and shipped UI agree.
export { TopicIcon } from "@memaxlabs/ui";
