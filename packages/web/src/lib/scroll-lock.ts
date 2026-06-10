let lockCount = 0;
let previousBodyOverflow = "";
let previousBodyOverscrollBehavior = "";
let previousHtmlOverflow = "";
let previousHtmlOverscrollBehavior = "";

/**
 * Acquire a global body scroll lock with reference counting.
 * Multiple overlays can lock concurrently; styles restore only when all release.
 */
export function acquireBodyScrollLock(): () => void {
  if (typeof document === "undefined") {
    return () => {};
  }

  if (lockCount === 0) {
    previousBodyOverflow = document.body.style.overflow;
    previousBodyOverscrollBehavior = document.body.style.overscrollBehavior;
    previousHtmlOverflow = document.documentElement.style.overflow;
    previousHtmlOverscrollBehavior =
      document.documentElement.style.overscrollBehavior;
    document.body.style.overflow = "hidden";
    document.body.style.overscrollBehavior = "none";
    document.documentElement.style.overflow = "hidden";
    document.documentElement.style.overscrollBehavior = "none";
  }

  lockCount += 1;
  let released = false;

  return () => {
    if (released) return;
    released = true;
    lockCount = Math.max(0, lockCount - 1);
    if (lockCount === 0) {
      document.body.style.overflow = previousBodyOverflow;
      document.body.style.overscrollBehavior = previousBodyOverscrollBehavior;
      document.documentElement.style.overflow = previousHtmlOverflow;
      document.documentElement.style.overscrollBehavior =
        previousHtmlOverscrollBehavior;
    }
  };
}
