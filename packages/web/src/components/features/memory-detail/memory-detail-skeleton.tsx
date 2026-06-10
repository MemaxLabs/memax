/**
 * MemoryDetailSkeleton — borderless loading skeleton for memory detail page.
 *
 * Mirrors the loaded layout exactly: back button area → provenance line →
 * content lines → footer tags. All on --background, staggered pulse delays.
 *
 * Kitchen reference: section 02, demo 2c-ii.
 */
export function MemoryDetailSkeleton() {
  return (
    <div className="mx-auto max-w-4xl">
      {/* Title skeleton */}
      <div className="flex items-start gap-3 mb-1">
        <div className="h-5 w-5 rounded bg-foreground/[0.07] shrink-0 mt-1" />
        <div className="flex-1 min-w-0 space-y-2">
          <div className="h-5 w-3/4 rounded bg-foreground/[0.07] animate-pulse" />
          <div
            className="h-3 w-1/3 rounded bg-foreground/[0.07] animate-pulse"
            style={{ animationDelay: "0.1s" }}
          />
        </div>
      </div>

      {/* Provenance skeleton */}
      <div className="pl-8 mt-3">
        <div className="flex items-center gap-2">
          <div className="h-3.5 w-3.5 rounded bg-foreground/[0.07] animate-pulse" />
          <div
            className="h-3 w-24 rounded bg-foreground/[0.07] animate-pulse"
            style={{ animationDelay: "0.15s" }}
          />
          <div
            className="h-3 w-16 rounded bg-foreground/[0.07] animate-pulse"
            style={{ animationDelay: "0.2s" }}
          />
        </div>
      </div>

      {/* Content skeleton */}
      <div className="pl-8 mt-8 space-y-3">
        {[1, 0.83, 0.67, 0.75].map((w, i) => (
          <div
            key={i}
            className="h-4 rounded bg-foreground/[0.07] animate-pulse"
            style={{ width: `${w * 100}%`, animationDelay: `${i * 0.1}s` }}
          />
        ))}
      </div>

      {/* Footer skeleton */}
      <div className="pl-8 mt-8 pt-5 border-t border-border/20">
        <div className="flex gap-2">
          <div className="h-6 w-16 rounded-lg bg-foreground/[0.07] animate-pulse" />
          <div
            className="h-6 w-20 rounded-lg bg-foreground/[0.07] animate-pulse"
            style={{ animationDelay: "0.1s" }}
          />
        </div>
      </div>
    </div>
  );
}
