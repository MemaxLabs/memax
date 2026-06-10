"use client";

export type TopicViewMode = "a" | "b" | "c";

const TOPIC_VIEW_MODE_STORAGE_PREFIX = "memax_ui_topic_view_mode";

function isValidTopicViewMode(value: unknown): value is TopicViewMode {
  return value === "a" || value === "b" || value === "c";
}

export function resolveTopicViewMode(childCount: number): TopicViewMode {
  if (childCount <= 20) return "a";
  if (childCount <= 80) return "b";
  return "c";
}

export function resolveTopicViewModeDefault(
  childCount: number,
  isMobile: boolean,
): TopicViewMode {
  if (isMobile) return "c";
  return resolveTopicViewMode(childCount);
}

/**
 * Storage keys are namespaced by viewport so a desktop preference for mode
 * "a" / "b" cannot leak onto mobile (which must default to mode "c" per
 * the borderless frame spec). Old un-namespaced keys written before this
 * change are ignored — read paths fall through to the viewport default.
 */
export function getTopicViewModeStorageKey(hubId: string, isMobile: boolean) {
  const viewport = isMobile ? "mobile" : "desktop";
  return `${TOPIC_VIEW_MODE_STORAGE_PREFIX}:${hubId}:${viewport}`;
}

export function getStoredTopicViewMode(
  hubId: string,
  isMobile: boolean,
): TopicViewMode | undefined {
  if (typeof window === "undefined") return undefined;
  const value = window.localStorage.getItem(
    getTopicViewModeStorageKey(hubId, isMobile),
  );
  return isValidTopicViewMode(value) ? value : undefined;
}

export function setStoredTopicViewMode(
  hubId: string,
  isMobile: boolean,
  mode: TopicViewMode | undefined,
) {
  if (typeof window === "undefined") return;
  const key = getTopicViewModeStorageKey(hubId, isMobile);
  if (!mode) {
    window.localStorage.removeItem(key);
    return;
  }
  window.localStorage.setItem(key, mode);
}
