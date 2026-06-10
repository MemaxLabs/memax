"use client";

/**
 * Memory detail view — borderless reading surface used by both routes.
 *
 * North star: kitchen section 02. Design principles:
 * 1. Borderless — no Surface card, entire page on --background
 * 2. Provenance-first — WHO captured, FROM WHERE, recalled how often
 * 3. AI distillation as hero — summary at text-fg-1, raw content collapsed at text-fg-2
 * 4. Not a dead end — topic siblings, navigable topic pills
 *
 * Two routes share this body:
 *   - v1 `/memories/<id>` → wrapped by `MemoryDetailPage` (the page file)
 *   - v2 `/h/<slug>/memories/<id>` → wrapped by `<V2HubRoute>`
 *
 * Both routes render identical UI; the redesign (sections + edit +
 * recall-time fix) is plan 21's deliverable. Lives in `components/`
 * (not in either page file) per Next 16 segment-export guidance: only
 * default + supported segment config exports belong in route files.
 */

import { useState, useEffect, useRef, useCallback } from "react";
import { useRouter, usePathname } from "next/navigation";
import { buildMemoriesPath, getHubSlugForPath } from "@/lib/route-helpers";
import {
  useMemory,
  useTrackMemoryAccessed,
  useUpdateMemory,
} from "@/hooks/use-memories";
import { useAuth } from "@/lib/auth";
import { MemoryDetailEditor } from "@/components/features/memory-detail/memory-detail-editor";
import { classifyMutationError } from "@/lib/error-copy";
import { useMemoryForget } from "@/hooks/use-memory-forget";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useLocale, useInterpolate } from "@/i18n";
import { TopicLocation } from "@/components/features/topic/topic-pills";
import {
  ProvenanceStrip,
  hasProvenanceExtras,
} from "@/components/features/provenance-strip";
import {
  ActionMenu,
  actionMenuEntries,
} from "@/components/features/action-menu";
import { StabilityIcon } from "@/components/features/stability-icon";
import { TopicSiblings } from "@/components/features/topic/topic-siblings";
import { RelatedMemories } from "@/components/features/memory-detail/related-memories";
import { MemoryAttachments } from "@/components/features/memory-detail/memory-attachments";
import { MemoryDetailSkeleton } from "@/components/features/memory-detail/memory-detail-skeleton";
import { ContentDisclosure, ContentError, Pill } from "@memaxlabs/ui";
import { formatAge } from "@/lib/format-age";
import { formatMemoryMarkdown } from "@/lib/format-context";
import { sanitizeSummary, sanitizeTitle } from "@/lib/sanitize-metadata";
import {
  ChevronLeft,
  Copy,
  Check,
  Download,
  Pencil,
  Info,
  Trash2,
} from "lucide-react";
import { PageShell } from "@/components/layout";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface MemoryDetailViewProps {
  memoryId: string;
  /**
   * Override for the back-button action. Defaults to `router.back()`
   * (canonical route navigation). Modal-variant callers
   * (`<MemoryDetail variant="modal">`) pass an `onClose` here so the
   * back arrow dismisses the dialog instead of navigating browser
   * history. Codex 5.5 caught this gap when the variant wrapper
   * landed.
   */
  onBack?: () => void;
}

export function MemoryDetailView({
  memoryId: id,
  onBack,
}: MemoryDetailViewProps) {
  const { data: memory, isLoading, isError, refetch } = useMemory(id);
  // Detail page mount is the canonical "user deliberately viewed this
  // memory" signal — fire exactly once per session per id. GET is now
  // side-effect-free, so without this the decay multiplier never gets
  // reinforced when users open a memory.
  useTrackMemoryAccessed(id);
  const forget = useMemoryForget();
  const updateMemory = useUpdateMemory();
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  const isMobile = useIsMobile();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [copied, setCopied] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  // Body edit mode (plan 21 phase 3) — flips the body section from
  // read-only ReactMarkdown to a Tiptap editor. State machine:
  //   idle      → "Edit body" pencil visible (owner only)
  //   editing   → editor mounted with current memory.content; floating
  //               Save/Cancel
  //   saving    → save mutation in flight; editor disabled, button
  //               shows spinner
  //   error     → inline banner with classified copy; user can retry
  //               or cancel without losing their draft
  // Auto-exits to idle on save success.
  const [editingBody, setEditingBody] = useState(false);
  const [bodySaveError, setBodySaveError] = useState<string | null>(null);
  // Synchronous save lock — closes the pre-render duplicate-submit
  // window that the React-state `updateMemory.isPending` can't catch.
  // Codex 5.5 H-finding (re-review): `isPending` only flips after the
  // mutation propagates through TanStack Query + React re-renders, so
  // a rapid double-click can call `mutate` twice before the editor's
  // `saving` prop flips. The ref is set synchronously inside
  // `handleSaveBody` before `mutate` and cleared on settle/error so
  // the next intentional save (post-failure retry, post-success edit
  // re-entry) isn't blocked.
  const savingBodyRef = useRef(false);
  const { user } = useAuth();
  // Owner gate: only the memory owner can edit. Server enforces this
  // via `GetMemory(id, ownerID)` in the PATCH handler — but hiding
  // the affordance up front keeps the UI honest about what's
  // actionable. (Hub-admin/contributor edit lands in a follow-up
  // plan that loosens the store-level ownership check.)
  const canEditBody = !!memory && !!user && memory.owner_id === user.id;

  // Instant back — no slide-out animation. Kitchen 29n spec caps drill
  // motion at 200ms ±16px, and the prior 250ms 30%-slide exit added
  // perceivable lag before navigation. Router.back() is now immediate on
  // both mobile and desktop.
  const handleBack = useCallback(() => {
    if (onBack) {
      onBack();
      return;
    }
    router.back();
  }, [onBack, router]);

  const handleCopy = useCallback(() => {
    if (!memory) return;
    navigator.clipboard.writeText(formatMemoryMarkdown(memory)).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [memory]);

  const handleRename = useCallback(() => {
    if (!memory) return;
    setTitleDraft(memory.title);
    setEditingTitle(true);
  }, [memory]);

  const handleEditBody = useCallback(() => {
    setBodySaveError(null);
    setEditingBody(true);
  }, []);

  const handleCancelBodyEdit = useCallback(() => {
    setEditingBody(false);
    setBodySaveError(null);
  }, []);

  const handleSaveBody = useCallback(
    (markdown: string) => {
      if (!memory) return;
      // Pre-render duplicate-submit lock — closes the window between
      // calling `mutate` and `updateMemory.isPending` flipping true
      // (TanStack propagation + React re-render). Without it, a
      // rapid double-click or repeated `⌘↵` before that render fires
      // two PATCHes. Codex 5.5 H-finding (re-review).
      if (savingBodyRef.current) return;
      const trimmed = markdown.trim();
      if (trimmed === memory.content.trim()) {
        // No-op save — exit edit mode without burning a network round-trip.
        setEditingBody(false);
        setBodySaveError(null);
        return;
      }
      // Server's PATCH treats `content === ""` as a label-only update
      // (see Update handler), which would silently drop the user's
      // intent to empty the memory. Reject inline so the user sees
      // why their save didn't go through. Codex 5.5 M-finding.
      if (trimmed === "") {
        setBodySaveError(t.note.editEmptyContent);
        return;
      }
      savingBodyRef.current = true;
      updateMemory.mutate(
        { id, content: trimmed, content_type: "markdown" },
        {
          onSuccess: () => {
            setEditingBody(false);
            setBodySaveError(null);
          },
          onError: (err) => {
            const classified = classifyMutationError(err, {
              action: t.errors.action.updateMemory,
            });
            setBodySaveError(classified?.message ?? t.note.editSaveFailed);
          },
          onSettled: () => {
            // Released on BOTH success + error paths so a post-failure
            // retry or post-success new-edit-cycle can save again.
            savingBodyRef.current = false;
          },
        },
      );
    },
    [id, memory, updateMemory, t],
  );

  const handleDownload = useCallback(() => {
    if (!memory) return;
    const content = formatMemoryMarkdown(memory);
    const blob = new Blob([content], {
      type: "text/markdown;charset=utf-8",
    });
    const url = URL.createObjectURL(blob);
    const filename =
      (memory.title || "memory")
        .replace(/[^\p{L}\p{N}\s-]/gu, "")
        .trim()
        .replace(/\s+/g, "-")
        .toLowerCase()
        .slice(0, 60) || "memory";
    const a = document.createElement("a");
    a.href = url;
    a.download = `${filename}.md`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }, [memory]);

  // ActionMenu destructive item contract: resolve `true` to close the
  // popover + advance (redirect), `false` to keep the confirm panel
  // mounted for retry. useMemoryForget returns its own boolean for
  // expected failures (partial-skip, permission denied, infra error),
  // which maps directly onto that contract.
  const handleMenuForget = useCallback(async (): Promise<boolean> => {
    const success = await forget.forgetWithConfirm([id]);
    if (success) {
      // Land back on the same hub's overview that the user was on —
      // `currentHubSlug` is null on v1, so this is a v1-shape path then;
      // under v2 it preserves the active hub.
      router.push(buildMemoriesPath(currentHubSlug));
    }
    return success;
  }, [currentHubSlug, forget, id, router]);

  // Sticky title — appears when the sentinel scrolls out of view.
  // The observer watches a stable invisible sentinel, not the <h1>, so the
  // edit-mode title input swap (editingTitle) can't leave the observer
  // attached to a detached node.
  // Plan 26 follow-up: removed the scroll-pinned compact header
  // (MemoryStickyHeader). The mask-image fade at its bottom edge read
  // as a buggy half-glass strip, and the inline back-arrow + title at
  // the top of the page already covers wayfinding without needing a
  // duplicate scroll-anchored chrome.

  const age = (dateStr: string) => formatAge(dateStr, t, interpolate);
  const titleIndent = "pl-8";

  const cleanSummary = sanitizeSummary(memory?.summary);
  // If sanitizeTitle rejects the title, show a localized fallback — do NOT
  // fall back to the original bad title (that defeats the defense-in-depth guard).
  const cleanTitle =
    sanitizeTitle(memory?.title) || (memory?.title ? t.toast.untitled : "");
  const hasSummary =
    cleanSummary && memory?.content && cleanSummary !== memory.content;

  const wordCount = memory?.content
    ? memory.content.split(/\s+/).filter(Boolean).length
    : 0;

  // ── Loading ──
  if (isLoading) {
    return (
      <PageShell innerClassName="">
        <MemoryDetailSkeleton />
      </PageShell>
    );
  }

  // ── Error ──
  if (isError) {
    return (
      <PageShell>
        <button
          onClick={handleBack}
          className="text-fg-3 hover:text-fg-2 transition-colors cursor-pointer mb-8"
        >
          <ChevronLeft className="h-5 w-5" />
        </button>
        <ContentError message={t.states.error.network} onRetry={refetch} />
      </PageShell>
    );
  }

  // ── Empty ──
  if (!memory) {
    return (
      <PageShell innerClassName="mx-auto max-w-4xl text-center py-16">
        <p className="text-[16px] text-fg-2 mb-4">{t.states.empty.transient}</p>
        <button
          onClick={handleBack}
          className="text-[14px] text-fg-3 hover:text-fg-2 cursor-pointer"
        >
          ← {t.tabs.all}
        </button>
      </PageShell>
    );
  }

  return (
    <div>
      {/* ── Page content (borderless on --background) ── */}
      <PageShell>
        {/* Title + Actions */}
        <div className="flex items-start gap-3 mb-1">
          <button
            onClick={handleBack}
            className="shrink-0 mt-1 text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
          >
            <ChevronLeft className="h-5 w-5" />
          </button>
          <div className="flex-1 min-w-0">
            {editingTitle ? (
              <input
                autoFocus
                value={titleDraft}
                onChange={(e) => setTitleDraft(e.target.value)}
                onBlur={() => {
                  if (titleDraft.trim() && titleDraft.trim() !== memory.title)
                    updateMemory.mutate({ id, title: titleDraft.trim() });
                  setEditingTitle(false);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") e.currentTarget.blur();
                  if (e.key === "Escape") setEditingTitle(false);
                }}
                className="w-full text-[21px] font-bold text-foreground bg-transparent outline-none leading-snug"
                style={{ letterSpacing: "-0.02em" }}
              />
            ) : (
              <h1
                className="text-[21px] font-bold text-foreground leading-snug transition-colors"
                style={{ letterSpacing: "-0.02em" }}
              >
                {cleanTitle}
              </h1>
            )}
          </div>
          <div className="flex items-center gap-2.5 shrink-0 mt-1.5">
            <button
              onClick={handleCopy}
              aria-label={t.note.actions.copyMarkdown}
              className="text-fg-4 hover:text-fg-2 transition-colors cursor-pointer"
            >
              {copied ? (
                <Check className="h-3.5 w-3.5" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </button>
            <ActionMenu
              triggerAriaLabel={t.note.actions.menu}
              items={actionMenuEntries(
                {
                  id: "copy",
                  label: t.note.actions.copyMarkdown,
                  icon: Copy,
                  onSelect: handleCopy,
                },
                {
                  id: "rename",
                  label: t.note.actions.rename,
                  icon: Pencil,
                  onSelect: handleRename,
                },
                // Body edit only for the memory owner. Server enforces
                // owner check on PATCH, but hiding the affordance for
                // non-owners keeps the menu honest.
                ...(canEditBody
                  ? ([
                      {
                        id: "edit-body",
                        label: t.note.actions.editBody,
                        icon: Pencil,
                        onSelect: handleEditBody,
                      },
                    ] as const)
                  : []),
                {
                  id: "download",
                  label: t.note.actions.download,
                  icon: Download,
                  onSelect: handleDownload,
                },
                // Only offer "Show details" when there are extras to
                // reveal (agent link, project, file, updated age,
                // dream history) — otherwise the item is a no-op.
                ...(hasProvenanceExtras(memory)
                  ? ([
                      "divider" as const,
                      {
                        id: "details",
                        label: showDetails
                          ? t.note.actions.hideDetails
                          : t.note.actions.showDetails,
                        icon: Info,
                        onSelect: () => setShowDetails((v) => !v),
                      },
                    ] as const)
                  : []),
                "divider",
                // Forget as a destructive menu item. The popover
                // morphs its body into a destructive-LEFT / safe-
                // RIGHT confirm panel; safe (Keep) wins default
                // focus. onConfirm redirects to /memories on true.
                {
                  id: "forget",
                  label: t.forget.button,
                  icon: Trash2,
                  destructive: true,
                  confirmPrompt: t.forget.thisMemory,
                  confirmLabel: t.forget.button,
                  cancelLabel: t.forget.keep,
                  pendingLabel: t.note.forgetting,
                  onConfirm: handleMenuForget,
                },
              )}
            />
          </div>
        </div>

        {/* ── Metadata band ── (2 rows by default)
              Row 1: identity/age/recall (left) + topic (right)
              Row 2: classification sentence + stability icon + tags + add
              Extended atoms (agent link, project, file, updated age, dream
              history) live in the inline details expansion below, driven
              by ⋯ → Show details. Per memax DNA (content > chrome) we
              collapse the band to the four anchors users actually scan.
              Tight row spacing (4px) — the two rows read as a single
              compound metadata band, not separate sections. */}
        <div className={`${titleIndent} mt-1.5 space-y-1`}>
          {/* Row 1 — identity + topic. flex-wrap so topic drops to its
                own line at narrow widths instead of crowding the row. */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <div className="flex-1 min-w-0">
              <ProvenanceStrip memory={memory} mode="compact" />
            </div>
            <div className="shrink-0">
              <TopicLocation
                memoryId={memory.id}
                hubId={memory.hub_id}
                topicId={memory.topic_id}
              />
            </div>
          </div>

          {/* Row 2 — "memax sees this as" — classification sentence +
                stability icon. Plan 21 §3.1: kind+stability gets its own
                explicit section adjacent to the metadata band so the
                user reads "✦ how memax classifies this" as content,
                not as scattered pills. Tags moved out to the bottom
                (low-signal, optional) per the same restructure. */}
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px]">
            <span
              className="inline-flex items-center gap-1.5"
              style={{ color: "var(--signature)" }}
            >
              <span className="text-[12px]">✦</span>
              <span className="inline-flex items-center gap-1.5 text-fg-2">
                {classificationCopy(
                  memory.kind,
                  memory.stability,
                  t.note.classification,
                )}
                <StabilityIcon
                  stability={memory.stability}
                  className="h-3 w-3 text-fg-3 shrink-0"
                />
              </span>
            </span>
          </div>

          {/* Details expansion — extras-only provenance on demand.
                Renders ONLY the supplementary atoms (agent link, project,
                file, updated age, dream history); identity / captured
                age / recall count already live in Row 1 so we don't
                re-emit them. Hidden by default; toggled by ⋯ menu. */}
          {showDetails && hasProvenanceExtras(memory) && (
            <div className="pt-1 animate-fade-in">
              <ProvenanceStrip memory={memory} mode="extras" />
            </div>
          )}
        </div>

        {/* ── Content ── */}
        <>
          {/* Processing indicator — shown above content while memax is working */}
          {memory.state === "processing" && (
            <div className="mt-6 mb-2 flex items-center gap-2">
              <span
                className="text-[12px] state-slow-breathe"
                style={{ color: "var(--signature)" }}
              >
                ✦
              </span>
              <span className="text-[13px] text-fg-3">
                {t.note.stillRemembering}
              </span>
            </div>
          )}

          {/* Body — read mode renders ReactMarkdown (summary as
                hero when distinct from content; raw content
                otherwise). Edit mode swaps to a Tiptap editor on the
                raw content. Edit mode bypasses the summary/content
                distinction because we're editing the SOURCE-OF-TRUTH
                content; a re-process will generate a new summary
                server-side after save. */}
          <div className="mt-8">
            {editingBody ? (
              <MemoryDetailEditor
                initialContent={memory.content}
                onSave={handleSaveBody}
                onCancel={handleCancelBodyEdit}
                saving={updateMemory.isPending}
              />
            ) : (
              <>
                {hasSummary && (
                  <div className="flex items-center gap-1.5 mb-3">
                    <span
                      className="text-[12px]"
                      style={{ color: "var(--signature)" }}
                    >
                      ✦
                    </span>
                    <span className="text-[13px] text-fg-3">
                      {t.note.summary}
                    </span>
                  </div>
                )}
                <div className="memax-prose text-fg-1 text-[16px]">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {hasSummary ? cleanSummary : cleanSummary || memory.content}
                  </ReactMarkdown>
                </div>
              </>
            )}
            {bodySaveError && editingBody ? (
              <div
                role="alert"
                aria-live="polite"
                className="mt-3 rounded-chrome bg-destructive/10 px-3 py-2 text-[13px] text-destructive"
              >
                {bodySaveError}
              </div>
            ) : null}
          </div>

          {/* Raw content — collapsed disclosure */}
          {hasSummary && (
            <div className="mt-6">
              <ContentDisclosure
                label={t.note.originalContent}
                detail={interpolate(t.note.words, {
                  n: String(wordCount),
                })}
              >
                <div className="memax-prose text-fg-2 text-[15px] mt-2">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {memory.content}
                  </ReactMarkdown>
                </div>
              </ContentDisclosure>
            </div>
          )}

          {/* Attachments */}
          {memory.attachments && memory.attachments.length > 0 && (
            <div className="mt-6">
              <MemoryAttachments memory={memory} />
            </div>
          )}

          {/* Tags — sit BEFORE the cross-memory relevancy sections so
              annotation metadata reads with the body content rather
              than competing with the structural "what else is related"
              navigation. Plan 26 follow-up: user wants tags above
              related memories. Hidden entirely when no tags AND
              we're on a read-only state, so passive viewers don't
              see an empty input. */}
          <div className="mt-6">
            <div className="flex flex-wrap items-center gap-1.5 text-[13px]">
              {memory.tags?.map((tag) => (
                <Pill
                  key={tag}
                  variant="remove"
                  size="sm"
                  removeLabel={t.note.removeTag}
                  onRemove={() => {
                    const newTags = memory.tags.filter((t) => t !== tag);
                    updateMemory.mutate({ id, tags: newTags });
                  }}
                >
                  {tag}
                </Pill>
              ))}
              <input
                placeholder={t.note.addTag}
                aria-label={t.note.addTag}
                className="text-[12px] bg-transparent outline-none w-24 text-fg-3 placeholder:text-fg-4 h-5"
                onKeyDown={(e) => {
                  const value = e.currentTarget.value.trim();
                  if (e.key === "Enter" && value) {
                    const currentTags = memory.tags ?? [];
                    if (!currentTags.includes(value)) {
                      updateMemory.mutate({
                        id,
                        tags: [...currentTags, value],
                      });
                    }
                    e.currentTarget.value = "";
                  }
                }}
              />
            </div>
          </div>

          {/* Related memories (semantic) — glass card, own chrome */}
          <RelatedMemories memoryId={memory.id} memoryState={memory.state} />

          {/* Topic siblings (folder) — glass card, own chrome */}
          {memory.topic_id && (
            <TopicSiblings
              topicId={memory.topic_id}
              currentMemoryId={memory.id}
            />
          )}
        </>
      </PageShell>
    </div>
  );
}

function classificationCopy(
  kind: string | undefined,
  stability: string | undefined,
  copy: Record<string, string>,
): string {
  if (kind === "episodic") {
    return stability === "volatile" ? copy.recentActivity : copy.pastContext;
  }
  if (kind === "procedural") return copy.howTo;
  if (kind === "rationale") return copy.decisionContext;
  if (stability === "stable") return copy.durableReference;
  return copy.reference;
}
