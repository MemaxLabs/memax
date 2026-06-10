const MEMORIES_SCROLL_PREFIX = "memax-memories-scroll:v1";

export function isMemoriesRootPath(pathname: string) {
  return pathname === "/memories";
}

export function isTopicPath(pathname: string) {
  return /^\/memories\/topics\/[^/]+$/.test(pathname);
}

export function isMemoryDetailPath(pathname: string) {
  return /^\/memories\/(?!topics\/)[^/]+$/.test(pathname);
}

export function isMemoriesPath(pathname: string) {
  return (
    isMemoriesRootPath(pathname) ||
    isTopicPath(pathname) ||
    isMemoryDetailPath(pathname)
  );
}

export function getMemoriesScrollStorageKey(hubId: string, pathname: string) {
  return `${MEMORIES_SCROLL_PREFIX}:${hubId}:${pathname}`;
}

export function saveMemoriesScrollPosition(
  hubId: string,
  pathname: string,
  top: number,
) {
  if (
    !hubId ||
    !isMemoriesPath(pathname) ||
    typeof globalThis.sessionStorage === "undefined"
  ) {
    return;
  }
  sessionStorage.setItem(
    getMemoriesScrollStorageKey(hubId, pathname),
    String(Math.max(0, Math.round(top))),
  );
}

export function readMemoriesScrollPosition(hubId: string, pathname: string) {
  if (
    !hubId ||
    !isMemoriesPath(pathname) ||
    typeof globalThis.sessionStorage === "undefined"
  ) {
    return null;
  }
  const raw = sessionStorage.getItem(
    getMemoriesScrollStorageKey(hubId, pathname),
  );
  if (!raw) return null;
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
}

export function shouldRestoreMemoriesScroll({
  previousPath,
  nextPath,
  isPopNavigation,
}: {
  previousPath: string;
  nextPath: string;
  isPopNavigation: boolean;
}) {
  if (previousPath === nextPath || !isMemoriesPath(nextPath)) {
    return false;
  }

  if (isPopNavigation) {
    return isMemoriesPath(previousPath);
  }

  if (isMemoriesRootPath(nextPath)) {
    return isMemoriesPath(previousPath);
  }

  if (isTopicPath(nextPath) && isMemoryDetailPath(previousPath)) {
    return true;
  }

  return false;
}
