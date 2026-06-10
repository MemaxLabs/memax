"use client";

import React from "react";
import { motion, AnimatePresence, useDragControls } from "framer-motion";
import { useCallback, useEffect, useState, useRef } from "react";
import {
  type Memory,
  useUpdateMemory,
  useMemory,
  useTrackMemoryAccessed,
} from "@/hooks/use-memories";
import { useMemoryForget } from "@/hooks/use-memory-forget";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useLocale, useInterpolate } from "@/i18n";

import {
  INSTANT,
  FAST,
  NORMAL,
  EASE,
  DISSOLVE_WAIT,
} from "@memaxlabs/ui/tokens/motion";
import { formatAge } from "@/lib/format-age";
import { formatMemoryMarkdown } from "@/lib/format-context";
import { Pill, StateIndicator } from "@memaxlabs/ui";
import { MemoryAttachments } from "@/components/features/memory-detail/memory-attachments";
import { StabilityIcon } from "@/components/features/stability-icon";
import {
  ActionMenu,
  actionMenuEntries,
} from "@/components/features/action-menu";
import {
  X,
  ArrowUpRight,
  Search,
  Link2,
  Trash2,
  Copy,
  Pencil,
  Download,
} from "lucide-react";
const ease = EASE;

interface Props {
  memory: Memory | null;
  searchQuery?: string;
  onClose: () => void;
}

export function MemoryModal({
  memory: initialMemory,
  searchQuery = "",
  onClose,
}: Props) {
  const isMobile = useIsMobile();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const age = (dateStr: string) => formatAge(dateStr, t, interpolate);
  const dragControls = useDragControls();
  const forget = useMemoryForget();
  const updateMemory = useUpdateMemory();
  const [dissolving, setDissolving] = useState(false);
  const [searching, setSearching] = useState(false);
  const [localSearch, setLocalSearch] = useState("");
  const searchInputRef = useRef<HTMLInputElement>(null);
  // Reset local state when a different memory opens
  useEffect(() => {
    setDissolving(false);
    setEditingTitle(false);
    setAddingTag(false);
    setSearching(false);
    setLocalSearch("");
  }, [initialMemory?.id]);
  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [addingTag, setAddingTag] = useState(false);
  const [tagDraft, setTagDraft] = useState("");
  const titleRef = useRef<HTMLInputElement>(null);
  const tagRef = useRef<HTMLInputElement>(null);

  // Use live memory data (auto-refetch while processing)
  const { data: liveMemory } = useMemory(initialMemory?.id ?? "");
  const memory = liveMemory ?? initialMemory;
  // Opening the modal is a deliberate view — signal it to the server.
  // GET no longer bumps access_count, so without this hook, opening a
  // memory via the modal (e.g. from recent / inbox rows) would never
  // reinforce the decay multiplier.
  useTrackMemoryAccessed(initialMemory?.id);

  // Lock body scroll when modal is open
  useEffect(() => {
    if (!memory) return;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = "";
    };
  }, [memory]);

  const handleClose = useCallback(() => {
    if (dissolving) return;
    onClose();
  }, [dissolving, onClose]);

  useEffect(() => {
    if (!memory) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (searching) {
          setSearching(false);
          setLocalSearch("");
        } else if (editingTitle) {
          setEditingTitle(false);
        } else if (addingTag) {
          setAddingTag(false);
        } else {
          handleClose();
        }
      }
      // Cmd+F to search in memory
      if ((e.metaKey || e.ctrlKey) && e.key === "f") {
        e.preventDefault();
        setSearching(true);
        setTimeout(() => searchInputRef.current?.focus(), 50);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [memory, editingTitle, addingTag, searching, dissolving, handleClose]);

  // ActionMenu destructive contract — resolves true to close the
  // popover + trigger dissolve animation + parent onClose, false to
  // keep the confirm sub-panel mounted so the user can retry. Matches
  // the detail page's handleMenuForget shape so both surfaces share
  // one destructive grammar (ActionMenu sub-panel morph) despite
  // different post-success actions (route push vs dissolve-and-close).
  const handleMenuForget = useCallback(async (): Promise<boolean> => {
    if (!memory) return false;
    const memoryId = memory.id;
    const success = await forget.forgetWithConfirm([memoryId]);
    if (success) {
      setDissolving(true);
      setTimeout(() => {
        onClose();
      }, DISSOLVE_WAIT);
    }
    return success;
  }, [memory, forget, onClose]);

  // Non-destructive menu actions (parity with the detail-page ⋯).
  // Modal already supports rename via click-to-edit on the title,
  // but the menu item is kept for keyboard-first parity and
  // discoverability — clicking Rename focuses the title input the
  // same way a title click does.
  const handleMenuCopy = useCallback(() => {
    if (!memory) return;
    navigator.clipboard.writeText(formatMemoryMarkdown(memory)).catch(() => {});
  }, [memory]);

  const handleMenuRename = useCallback(() => {
    if (!memory) return;
    setEditingTitle(true);
    setTitleDraft(memory.title);
    setTimeout(() => titleRef.current?.focus(), 50);
  }, [memory]);

  const handleMenuDownload = useCallback(() => {
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

  const handleTitleSave = async () => {
    if (!memory || !titleDraft.trim() || titleDraft === memory.title) {
      setEditingTitle(false);
      return;
    }
    try {
      await updateMemory.mutateAsync({ id: memory.id, title: titleDraft });
    } catch {}
    setEditingTitle(false);
  };

  const handleTagRemove = async (tag: string) => {
    if (!memory) return;
    const newTags = memory.tags.filter((t) => t !== tag);
    try {
      await updateMemory.mutateAsync({ id: memory.id, tags: newTags });
    } catch {}
  };

  const handleTagAdd = async () => {
    if (!memory || !tagDraft.trim()) {
      setAddingTag(false);
      setTagDraft("");
      return;
    }
    const newTags = [...memory.tags, tagDraft.trim()];
    try {
      await updateMemory.mutateAsync({ id: memory.id, tags: newTags });
    } catch {}
    setTagDraft("");
    setAddingTag(false);
  };

  const headerBg = "var(--background)";
  const sourceLabel =
    memory?.source === "claude-code"
      ? t.note.source.claudeCode
      : memory?.source === "cursor"
        ? t.note.source.cursor
        : memory?.source === "chatgpt"
          ? t.note.source.chatgpt
          : t.note.source.you;
  const ageLabel = memory ? age(memory.updated_at) : "";
  // Active search query — local search takes priority over prop
  const activeSearch = localSearch || searchQuery;
  const isProcessing = memory?.state === "processing";
  const contentTypeLabel =
    memory?.content_type === "pdf"
      ? "PDF"
      : memory?.content_type === "image"
        ? "Image"
        : memory?.content_type === "code"
          ? "Code"
          : memory?.content_type === "link"
            ? "Link"
            : null;

  return (
    <AnimatePresence>
      {memory && (
        <>
          {/* Backdrop */}
          <motion.div
            className="fixed inset-0 z-modal"
            style={{
              background: "rgba(0,0,0,0.3)",
              backdropFilter: "blur(24px) saturate(1.2)",
              WebkitBackdropFilter: "blur(24px) saturate(1.2)",
            }}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: INSTANT }}
            onClick={handleClose}
          />

          {/* Modal — centered on desktop, sheet on mobile */}
          <div
            className="fixed inset-0 z-modal flex items-end md:items-center justify-center md:px-5 md:py-8 sm:p-6"
            onClick={handleClose}
          >
            <motion.div
              className="w-full max-w-[640px] overflow-y-auto max-h-[88vh] rounded-3xl max-md:rounded-b-none max-md:rounded-t-3xl max-md:max-w-none max-md:h-[92vh] overflow-hidden shadow-soft-lg max-md:border-b-0 max-md:border-x-0"
              style={{
                background: "var(--card)",
                border: "1px solid var(--bar-border)",
                boxShadow: "var(--bar-shadow)",
                paddingBottom: "env(safe-area-inset-bottom, 0px)",
              }}
              initial={isMobile ? { opacity: 0, y: 100 } : { opacity: 0 }}
              animate={
                dissolving
                  ? { opacity: 0, filter: "blur(4px)", scale: 0.98 }
                  : { opacity: 1, y: 0 }
              }
              exit={isMobile ? { opacity: 0, y: 60 } : { opacity: 0 }}
              transition={{ duration: FAST, ease }}
              drag={isMobile ? "y" : false}
              dragControls={isMobile ? dragControls : undefined}
              dragListener={false}
              dragConstraints={{ top: 0, bottom: 0 }}
              dragElastic={{ top: 0, bottom: 0.4 }}
              onDragEnd={(_, info) => {
                if (info.offset.y > 120 || info.velocity.y > 300) {
                  handleClose();
                }
              }}
              onClick={(e) => e.stopPropagation()}
            >
              {/* Drag handle — mobile only */}
              <div
                className="md:hidden h-3 cursor-grab active:cursor-grabbing touch-none"
                style={{ backgroundColor: headerBg }}
                onPointerDown={(e) => {
                  if (isMobile) dragControls.start(e);
                }}
              >
                <div className="w-8 h-1 rounded-full bg-foreground/20 mx-auto mt-1.5" />
              </div>
              {/* Header — title + meta */}
              <div
                className="px-4 sm:px-6 md:px-8 pt-5 pb-4"
                style={{ backgroundColor: headerBg }}
              >
                {/* Title row — title + close inline */}
                <div className="flex items-start gap-3">
                  <div className="flex-1 min-w-0">
                    {editingTitle ? (
                      <input
                        ref={titleRef}
                        value={titleDraft}
                        onChange={(e) => setTitleDraft(e.target.value)}
                        onBlur={handleTitleSave}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") handleTitleSave();
                          if (e.key === "Escape") setEditingTitle(false);
                        }}
                        className="w-full text-[18px] md:text-[21px] font-bold bg-transparent outline-none border-b-2 border-foreground/20 text-foreground pb-1"
                        style={{ letterSpacing: "-0.02em" }}
                        autoFocus
                      />
                    ) : (
                      <h2
                        className="group/title text-[18px] md:text-[21px] font-bold text-foreground leading-snug cursor-text rounded-lg px-2 -mx-2 py-1 transition-colors hover:bg-surface-2"
                        style={{ letterSpacing: "-0.02em" }}
                        onClick={() => {
                          setEditingTitle(true);
                          setTitleDraft(memory.title);
                          setTimeout(() => titleRef.current?.focus(), 50);
                        }}
                      >
                        {activeSearch ? (
                          <HighlightInline
                            text={memory.title}
                            query={activeSearch}
                          />
                        ) : (
                          memory.title
                        )}
                      </h2>
                    )}
                  </div>
                  <div className="shrink-0 mt-1 flex items-center gap-2.5">
                    {/* ⋯ owns the destructive path (Forget) via the
                        ActionMenu sub-panel morph — matches the detail
                        page pattern. Kept the X close button inline for
                        the modal dismiss affordance, which is distinct
                        from memory actions. */}
                    <ActionMenu
                      triggerAriaLabel={t.note.actions.menu}
                      items={actionMenuEntries(
                        {
                          id: "copy",
                          label: t.note.actions.copyMarkdown,
                          icon: Copy,
                          onSelect: handleMenuCopy,
                        },
                        {
                          id: "rename",
                          label: t.note.actions.rename,
                          icon: Pencil,
                          onSelect: handleMenuRename,
                        },
                        {
                          id: "download",
                          label: t.note.actions.download,
                          icon: Download,
                          onSelect: handleMenuDownload,
                        },
                        "divider",
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
                    <button
                      onClick={onClose}
                      aria-label={t.note.closePreview}
                      className="text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </div>
                </div>

                {/* Meta */}
                <div className="flex items-center flex-wrap gap-x-2 gap-y-1 mt-2 text-[14px] text-fg-3">
                  <span className="inline-flex items-center gap-1.5">
                    {memory &&
                      classificationCopy(
                        memory.kind,
                        memory.stability,
                        t.note.classification,
                      )}
                    {memory && (
                      <StabilityIcon
                        stability={memory.stability}
                        className="h-3 w-3 text-fg-4 shrink-0"
                      />
                    )}
                  </span>
                  <span style={{ opacity: 0.5 }}>·</span>
                  <span>{ageLabel}</span>
                  <span style={{ opacity: 0.5 }}>·</span>
                  <span>{sourceLabel}</span>
                  <span style={{ opacity: 0.5 }}>·</span>
                  <span className="tabular-nums">v{memory.version}</span>
                  {memory.access_count > 0 && (
                    <>
                      <span style={{ opacity: 0.5 }}>·</span>
                      <span className="tabular-nums inline-flex items-center gap-0.5">
                        <ArrowUpRight className="h-3 w-3" />
                        {memory.access_count}
                      </span>
                    </>
                  )}
                  {contentTypeLabel && (
                    <>
                      <span style={{ opacity: 0.5 }}>·</span>
                      <span className="font-mono text-[12px] tracking-wide text-fg-3">
                        {contentTypeLabel}
                      </span>
                    </>
                  )}
                </div>
              </div>

              <div
                className={`relative px-4 sm:px-6 md:px-8 pb-6 sm:pb-8 rounded-b-3xl ${searching ? "pt-10" : "pt-4"}`}
                style={{ background: "var(--card)" }}
              >
                {/* Floating search — Chrome-style, absolute top-right */}
                {searching && (
                  <div
                    className="absolute top-2 right-6 z-10 flex items-center gap-2 px-3 py-1.5 rounded-lg"
                    style={{
                      background: "var(--card)",
                      border: "1px solid var(--border)",
                      boxShadow: "0 2px 12px rgba(0,0,0,0.08)",
                    }}
                  >
                    <Search className="h-3.5 w-3.5 shrink-0 text-fg-3" />
                    <input
                      ref={searchInputRef}
                      value={localSearch}
                      onChange={(e) => setLocalSearch(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Escape") {
                          setSearching(false);
                          setLocalSearch("");
                        }
                      }}
                      placeholder={t.note.find}
                      className="w-32 text-[15px] bg-transparent outline-none text-foreground placeholder:text-fg-3"
                      autoFocus
                    />
                    {localSearch && (
                      <span className="text-[13px] text-fg-3 tabular-nums whitespace-nowrap">
                        {(() => {
                          const text =
                            (memory?.summary || "") +
                            " " +
                            (memory?.content || "");
                          const escaped = localSearch.replace(
                            /[.*+?^${}()|[\]\\]/g,
                            "\\$&",
                          );
                          return (text.match(new RegExp(escaped, "gi")) || [])
                            .length;
                        })()}
                      </span>
                    )}
                    <button
                      onClick={() => {
                        setSearching(false);
                        setLocalSearch("");
                      }}
                      className="text-fg-3 hover:text-fg-2 cursor-pointer"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </div>
                )}
                {/* Content — markdown or processing */}
                {isProcessing ? (
                  <div className="mt-6 flex flex-col items-center justify-center py-12">
                    <StateIndicator
                      state="processing"
                      size="md"
                      className="text-[21px] mb-3"
                    />
                    <p className="text-[15px] text-fg-2">
                      {t.note.stillRemembering}
                    </p>
                  </div>
                ) : (
                  <>
                    {/* Reference URL bar — for link-type memories */}
                    {(memory.content_type === "link" ||
                      memory.title.startsWith("http")) && (
                      <div
                        className="mt-4 flex items-center gap-2 px-3 py-2 rounded-surface text-[15px]"
                        style={{ background: "var(--muted)" }}
                      >
                        <Link2 className="h-3.5 w-3.5 shrink-0 text-fg-3" />
                        <a
                          href={
                            memory.title.startsWith("http")
                              ? memory.title
                              : undefined
                          }
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-fg-2 hover:text-fg-1 truncate transition-colors underline underline-offset-2 decoration-foreground/20"
                        >
                          {memory.title}
                        </a>
                      </div>
                    )}
                    {/* Summary callout */}
                    {memory.summary &&
                      memory.content &&
                      memory.summary !== memory.content && (
                        <div className="mt-4 rounded-surface bg-surface-1 p-4">
                          <div className="flex items-center gap-1.5 mb-2">
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
                          <div className="memax-prose text-fg-2 text-[16px]">
                            {activeSearch ? (
                              <HighlightedContent
                                text={memory.summary}
                                query={activeSearch}
                              />
                            ) : (
                              <ReactMarkdown
                                remarkPlugins={[remarkGfm, remarkBreaks]}
                              >
                                {memory.summary}
                              </ReactMarkdown>
                            )}
                          </div>
                        </div>
                      )}
                    <MemoryAttachments memory={memory} />
                    {/* Full content — use memax-prose for proper heading/list rendering */}
                    <div
                      className={`memax-prose text-fg-1 ${memory.summary && memory.content && memory.summary !== memory.content ? "" : "mt-5"}`}
                    >
                      {activeSearch ? (
                        <HighlightedContent
                          text={
                            memory.summary &&
                            memory.content &&
                            memory.summary !== memory.content
                              ? memory.content
                              : memory.summary || memory.content
                          }
                          query={activeSearch}
                        />
                      ) : (
                        <ReactMarkdown
                          remarkPlugins={[remarkGfm, remarkBreaks]}
                        >
                          {memory.summary &&
                          memory.content &&
                          memory.summary !== memory.content
                            ? memory.content
                            : memory.summary || memory.content}
                        </ReactMarkdown>
                      )}
                    </div>
                  </>
                )}

                {/* Tags — editable */}
                <div className="mt-5 flex items-center gap-2 flex-wrap">
                  {memory.tags?.map((tag) => (
                    <Pill
                      key={tag}
                      variant="remove"
                      onRemove={() => handleTagRemove(tag)}
                      removeLabel={t.note.removeTag}
                    >
                      {tag}
                    </Pill>
                  ))}
                  {addingTag ? (
                    <input
                      ref={tagRef}
                      value={tagDraft}
                      onChange={(e) => setTagDraft(e.target.value)}
                      onBlur={handleTagAdd}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleTagAdd();
                        if (e.key === "Escape") {
                          setAddingTag(false);
                          setTagDraft("");
                        }
                      }}
                      className="text-[14px] text-foreground bg-transparent outline-none border-b border-foreground/10 px-1 py-0.5 w-20"
                      placeholder={t.note.tagPlaceholder}
                      autoFocus
                    />
                  ) : (
                    <button
                      onClick={() => setAddingTag(true)}
                      className="text-[14px] text-fg-2 hover:text-fg-2 transition-colors cursor-pointer"
                    >
                      {t.note.addTag}
                    </button>
                  )}
                </div>
              </div>
              {/* Footer — processing indicator only. Forget migrated
                  into the ⋯ ActionMenu in the header cluster (parity
                  with the detail page), so the footer no longer hosts
                  an inline destructive morph. */}
              {isProcessing && (
                <div className="px-6 sm:px-8 pb-6">
                  <div className="mt-2 flex items-center gap-1.5 text-[14px] text-fg-3">
                    <span className="h-1 w-1 rounded-full bg-foreground/40 animate-pulse" />
                    {t.note.remembering}
                  </div>
                </div>
              )}
            </motion.div>
          </div>
        </>
      )}
    </AnimatePresence>
  );
}

const MARK_CLASS =
  "bg-amber-200/70 dark:bg-amber-500/30 text-foreground rounded-sm px-0.5 py-px";

function highlightParts(text: string, query: string) {
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const parts = text.split(new RegExp(`(${escaped})`, "gi"));
  const lq = query.toLowerCase();
  return parts.map((part, i) =>
    part.toLowerCase() === lq ? (
      <mark key={i} className={MARK_CLASS}>
        {part}
      </mark>
    ) : (
      <span key={i}>{part}</span>
    ),
  );
}

function HighlightInline({ text, query }: { text: string; query: string }) {
  if (!query.trim()) return <>{text}</>;
  return <>{highlightParts(text, query)}</>;
}

// Recursively highlight text inside React children (preserves markdown structure)
function highlightChildren(
  children: React.ReactNode,
  query: string,
): React.ReactNode {
  if (!query.trim()) return children;
  return React.Children.map(children, (child) => {
    if (typeof child === "string") return <>{highlightParts(child, query)}</>;
    if (
      React.isValidElement<{ children?: React.ReactNode }>(child) &&
      child.props.children
    ) {
      return React.cloneElement(
        child,
        {},
        highlightChildren(child.props.children, query),
      );
    }
    return child;
  });
}

function HighlightedContent({ text, query }: { text: string; query: string }) {
  if (!query.trim())
    return (
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkBreaks]}>
        {text}
      </ReactMarkdown>
    );
  // Render markdown first, then highlight text within rendered elements
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, remarkBreaks]}
      components={{
        p: ({ children }) => <p>{highlightChildren(children, query)}</p>,
        li: ({ children }) => <li>{highlightChildren(children, query)}</li>,
        h1: ({ children }) => <h1>{highlightChildren(children, query)}</h1>,
        h2: ({ children }) => <h2>{highlightChildren(children, query)}</h2>,
        h3: ({ children }) => <h3>{highlightChildren(children, query)}</h3>,
        h4: ({ children }) => <h4>{highlightChildren(children, query)}</h4>,
        td: ({ children }) => <td>{highlightChildren(children, query)}</td>,
        th: ({ children }) => <th>{highlightChildren(children, query)}</th>,
      }}
    >
      {text}
    </ReactMarkdown>
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
