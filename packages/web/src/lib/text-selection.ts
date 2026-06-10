/**
 * True when the user currently has a non-collapsed text selection that
 * intersects `element`. Used by clickable list rows / cards to
 * distinguish "the user is dragging to copy a title" from "the user
 * clicked to navigate."
 *
 * Why range.intersectsNode and not just anchorNode: a drag-select that
 * starts in row A and ends on row B leaves the click on B, but
 * `selection.anchorNode` is still inside A. Anchor-only would return
 * false for B and let the click navigate, cancelling the selection —
 * wrong. Iterating every range and asking `intersectsNode(element)`
 * catches both forward and backward drags regardless of which row the
 * click lands on, and it also handles multi-range selections (Cmd-drag
 * on Firefox) without extra cases.
 *
 * A fresh click outside any previous selection still navigates because
 * the selection collapses on the browser's default mousedown handler
 * before the click fires — so `isCollapsed` shortcuts the check.
 *
 * Returns false on the server (no window) and in edge cases where the
 * selection API is unavailable, so the caller's default behaviour
 * (navigate) runs.
 */
export function isTextSelectionActiveIn(element: Element | null): boolean {
  if (!element || typeof window === "undefined") return false;
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
    return false;
  }
  for (let i = 0; i < selection.rangeCount; i++) {
    const range = selection.getRangeAt(i);
    if (range.intersectsNode(element)) return true;
  }
  return false;
}
