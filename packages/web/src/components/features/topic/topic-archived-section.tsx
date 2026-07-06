"use client";

import { useState } from "react";
import { ChevronRight, ArchiveRestore } from "lucide-react";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";
import { useLocale, useInterpolate } from "@/i18n";
import { useArchivedTopics, useRestoreTopic } from "@/hooks/use-topics";
import { useBarToast } from "@/hooks/use-bar-toast";
import { formatAge } from "@/lib/format-age";
import { TopicIcon } from "./topic-icon";

/**
 * TopicArchivedSection — quiet disclosure at the bottom of the topics
 * overview listing the hub's archived topics with per-row restore.
 *
 * Deliberately understated: archived topics are out of the way by
 * definition, so the collapsed row is text-only ("Archived (3)") and
 * renders nothing at all when the hub has no archived topics. The list
 * is flat — subtrees archive atomically, so hierarchy carries no
 * information here.
 *
 * Restore is optimistic-feeling but server-driven: the row's restore
 * button fires topics.restore, query invalidation (useRestoreTopic)
 * re-syncs both the active tree and this list, and a success toast
 * confirms. Undo for *archive* lives on the archive toast in
 * topic-detail — this section is the recovery path for older archives.
 */
export function TopicArchivedSection() {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const [open, setOpen] = useState(false);
  // Fetches on mount: the section renders nothing when the hub has no
  // archived topics, and that decision needs the count up front. The
  // query shares the 30s staleTime + ["topics"] invalidation family, so
  // this is one cheap request per overview visit, not per toggle.
  const { data, isLoading } = useArchivedTopics();
  const restoreTopic = useRestoreTopic();
  const toast = useBarToast();
  const reduced = useReducedMotion();

  const topics = data?.topics ?? [];
  if (isLoading || topics.length === 0) return null;

  const handleRestore = (topicId: string) => {
    restoreTopic.mutate(topicId, {
      onSuccess: () => {
        toast.success(t.topics.restoreToast);
      },
    });
  };

  return (
    <section className="mt-2" aria-label={t.topics.archivedSection}>
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        aria-expanded={open}
        className="flex cursor-pointer items-center gap-1 py-1 text-[12px] text-fg-4 transition-colors hover:text-fg-2"
      >
        <ChevronRight
          className="h-3 w-3 transition-transform"
          style={{
            transform: open ? "rotate(90deg)" : undefined,
            transitionTimingFunction: "var(--ease-spring)",
          }}
          aria-hidden
        />
        {t.topics.archivedSection}
        <span className="text-fg-4">({topics.length})</span>
      </button>
      <AnimatePresence initial={false}>
        {open && (
          <motion.ul
            initial={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
            animate={reduced ? { opacity: 1 } : { opacity: 1, height: "auto" }}
            exit={reduced ? { opacity: 0 } : { opacity: 0, height: 0 }}
            transition={{ duration: 0.18 }}
            className="overflow-hidden"
          >
            {topics.map((topic) => (
              <li
                key={topic.id}
                className="flex min-h-9 items-center gap-2 rounded-chrome px-1 py-1 text-[13px] text-fg-3"
              >
                <TopicIcon
                  name={topic.icon}
                  className="h-3.5 w-3.5 shrink-0 text-fg-4"
                />
                <span className="min-w-0 truncate">{topic.name}</span>
                {topic.archived_at && (
                  <span className="shrink-0 text-[11px] text-fg-4">
                    {interpolate(t.topics.archivedAt, {
                      time: formatAge(topic.archived_at, t, interpolate),
                    })}
                  </span>
                )}
                <button
                  type="button"
                  onClick={() => handleRestore(topic.id)}
                  disabled={restoreTopic.isPending}
                  className="ml-auto flex shrink-0 cursor-pointer items-center gap-1 rounded-chrome px-2 py-1 text-[12px] text-fg-3 transition-colors hover:bg-foreground/6 hover:text-fg-1 disabled:cursor-wait disabled:opacity-60"
                >
                  <ArchiveRestore className="h-3 w-3" aria-hidden />
                  {restoreTopic.isPending
                    ? t.topics.restoring
                    : t.topics.restore}
                </button>
              </li>
            ))}
          </motion.ul>
        )}
      </AnimatePresence>
    </section>
  );
}
