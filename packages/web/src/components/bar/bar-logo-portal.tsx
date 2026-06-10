"use client";

import { useCallback } from "react";
import { createPortal } from "react-dom";
import { ChevronLeft } from "lucide-react";
import { usePathname } from "next/navigation";
import { useBar } from "@/contexts/bar-context";
import { ScopeIndicator, type BarScope } from "./scope-indicator";
import { useActiveHub } from "@/lib/auth";
import { useTopics, type TopicTree } from "@/hooks/use-topics";
import { useLocale, useInterpolate } from "@/i18n";
import { getTopicIdForPath } from "@/lib/route-helpers";

function findTopicLabel(
  topics: TopicTree[] | undefined,
  topicId: string,
): string | null {
  if (!topics) return null;
  const stack = [...topics];
  while (stack.length) {
    const topic = stack.shift();
    if (!topic) continue;
    if (topic.id === topicId) return topic.name;
    stack.push(...topic.children);
  }
  return null;
}

export function BarLogoPortal() {
  const {
    scope,
    peelBack,
    nextPeelBackStep,
    clearScope,
    dismissRouteTopic,
    routeTopicDismissed,
    isMobile,
  } = useBar();
  const pathname = usePathname();
  const { hubFilter } = useActiveHub();
  const { data: topicsData } = useTopics(hubFilter);
  const { t } = useLocale();
  const interpolate = useInterpolate();

  // Chip X behavior depends on which chip is showing:
  //   · Command chip (`/hub …`)   → clear the command scope.
  //   · Route-derived topic chip → dismiss the route-topic override but
  //     stay on the topic page. Recall falls back to global. No router.push.
  const handleClearScope = useCallback(() => {
    if (scope.kind === "command") {
      clearScope();
      return;
    }
    dismissRouteTopic();
  }, [clearScope, dismissRouteTopic, scope.kind]);

  const slot =
    typeof document !== "undefined"
      ? document.getElementById("bar-logo-slot")
      : null;
  if (!slot) return null;

  // Topic id from either v1 (`/memories/topics/<id>`) or v2
  // (`/h/<slug>/topics/<id>`) shape — the bar route-derived chip surfaces
  // identically under both shells.
  const topicId = getTopicIdForPath(pathname);
  const topicLabel = topicId
    ? findTopicLabel(topicsData?.topics, topicId)
    : null;

  let visibleScope: BarScope = scope;
  // Route-derived topic chip — only when the user hasn't explicitly
  // dismissed it on this route. Dismissal auto-resets on pathname change
  // (unless a restore snapshot re-seeds it).
  if (visibleScope.kind === "recall" && topicLabel && !routeTopicDismissed) {
    visibleScope = { kind: "topic", label: topicLabel };
  }

  // Show chevron when peel-back has something non-trivial to do.
  // Hidden on mobile — the compose overlay has its own back/close button.
  const showBackChevron =
    !isMobile && nextPeelBackStep !== "blur" && nextPeelBackStep !== "noop";

  return createPortal(
    <div className="flex items-center gap-1.5">
      {showBackChevron ? (
        <button
          type="button"
          onMouseDown={(event) => {
            // Keep back navigation from stealing focus and lifting the bar
            // before route transition settles.
            event.preventDefault();
          }}
          onClick={peelBack}
          className="cursor-pointer p-1 text-fg-3 transition-colors hover:text-fg-2"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
        </button>
      ) : null}
      <ScopeIndicator
        scope={visibleScope}
        title={
          visibleScope.kind === "recall"
            ? undefined
            : interpolate(t.bar.scope.clear, { label: visibleScope.label })
        }
        onClear={visibleScope.kind === "recall" ? undefined : handleClearScope}
      />
    </div>,
    slot,
  );
}
