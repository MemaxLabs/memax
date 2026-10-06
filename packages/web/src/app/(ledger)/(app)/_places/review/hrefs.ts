import type { ReviewItem } from "@/lib/v2/data/review";

/** Where C (and E on a conflict) goes: both sides of a conflict the judge linked. */
export function compareHref(slug: string, item: ReviewItem): string | null {
  // Only a conflict the judge linked to a decision in force has two sides.
  return item.conflictsWith
    ? `/${encodeURIComponent(slug)}/review/${encodeURIComponent(item.ref)}/compare`
    : null;
}

export function memoryHref(slug: string, ref: string): string {
  return `/${encodeURIComponent(slug)}/memories/${encodeURIComponent(ref)}`;
}
