// Maps to: topic-tree-panel.tsx, topic-tree-content.tsx, topic-tree-node.tsx
// Three modes: desktop pinned, desktop overlay, mobile bottom sheet
// ALSO a manipulation canvas: drag-to-reparent topics within the current
// hub + memory → topic drops from the grid/detail/inbox surfaces.
//
// ═══ LLM AGENT RULES — TREE PANEL ═══
//
// MOBILE SHEET: FAST (0.15s) + EASE [0.16, 1, 0.3, 1] — kitchen motion standard.
//   NOT physics spring (damping/stiffness) — those settle slower (~250ms) and overshoot.
//   motion.div elements must be DIRECT children of AnimatePresence for exit animations
//   — inlined in TopicTreePanelOverlay, not wrapped in a sub-component. Backdrop fades,
//   sheet slides from y:"100%" to y:0. Close button min-h-11 (44px touch target).
//
// INDENTATION: Capped at depth 4 to prevent layout breakage.
//   Desktop: 8px base + depth × 16px (max 72px at depth 4).
//   Mobile: 8px base + depth × 12px (max 56px at depth 4).
//   Depths 5+ render at depth-4 indent (still expandable, not deeper visually).
//   Constants: MAX_INDENT_DEPTH=4, INDENT_BASE=8, INDENT_STEP_DESKTOP=16, INDENT_STEP_MOBILE=12.
//
// EXPAND/COLLAPSE: Chevron rotates 90° via CSS transition-transform.
//   Hidden (text-transparent pointer-events-none) for leaf nodes.
//   Expanded IDs persisted to localStorage (memax_tree_expanded).
//
// DESKTOP: Peek overlay slides from x:-280 to x:0 (FAST=0.15s, spring ease).
//   300ms dismiss delay on mouse leave. Pin persists to localStorage.
//   Sidebar width: 280px. Header at CONTENT_TOP (80px).
//
// BACKDROP: bg-black/20 scrim on mobile sheet (industry standard for bottom sheets).
//   This is NOT the same as the bar's "no backdrop" rule — sheets use scrims, bars don't.
//
// HYDRATION GUARD: useIsMobile() starts false on SSR → desktop peek renders first → flash.
//   Fix: hasMounted flag (useState false → useEffect true). Return null until mounted.
//   This is the Radix/Headless UI pattern for viewport-dependent components.
//
// BODY SCROLL LOCK (iOS-safe, Radix Dialog pattern):
//   overflow:hidden alone FAILS on iOS Safari (momentum scroll penetrates).
//   Correct: position:fixed + top:-scrollY + left/right:0 + overflow:hidden.
//   Cleanup: restore all styles + window.scrollTo(0, savedScrollY).
//   Apply on mount, remove on unmount. Scope to mobile sheet only.
//
// ═══ MANIPULATION RULES — TOPIC DRAG-TO-REPARENT ═══
//
// The pinned tree is both a navigator AND a manipulation canvas:
//   - memory drag from /memories, topic detail, or the unassigned inbox
//     drops onto a topic node → routes through memories.batchMove (the
//     single authoritative user-move path) via useMemoryMove
//   - topic drag from a tree node drops onto another topic node OR the
//     "Top level" drop row → routes through useTopicMove.moveTopicWithUndo
//     which calls topics.update({parent_id, position}) atomically
//
// DRAG AFFORDANCE: left-edge grip (lucide GripVertical, h-3 w-3,
//   text-fg-4), visible on row hover only. Listeners attached to the
//   grip ONLY so clicking the row still navigates. Matches Notion.
//
// INVALID DROP FEEDBACK: TopicDndProvider computes `invalidDescendantIds`
//   once on drag-start (O(n) tree walk), stashes in React context, each
//   TopicTreeNode reads it and paints ring-destructive/30 opacity-60
//   instead of the regular isOver ring when it is the moving topic or
//   one of its descendants. Cleared on drag-end.
//
// HUB-ROOT DROP ZONE: one shared droppable row rendered at the top of
//   the tree during ANY active drag (topic OR memory). The row
//   represents the current hub as a destination and is labelled with
//   the current hub name — NOT "Top level" (that's internal tree
//   jargon) and NOT "Inbox" (too specific / misleading for generic
//   no-topic placement). The drop semantic branches by drag type:
//     - topic drag → reparent to parent_id: null (topic becomes a
//       root-level entry in the current hub's tree)
//     - memory drag → clear the memory's topic_id (memory stays in
//       the same hub, moves to the unassigned count)
//   No-op rules:
//     - topic already at hub root → silent no-op
//     - memory already unassigned → silent no-op
//   Success copy:
//     - topic: "Moved {name} to {hub}." (t.topics.topicMovedToHub)
//     - memory: "Moved to {hub}." (t.toast.movedTo via
//       useMemoryMove.moveOneSuccess(hubName))
//     - fallback when hub name is unresolved: t.topics.topicMoved
//       ("Moved {name}.") / t.toast.moved ("Moved.") — never emit an
//       empty destination string.
//   The ⋮ menu picker on each topic node surfaces the exact same
//   hub-name root destination so keyboard/touch/mobile users get the
//   same mental model as mouse/drag users.
//
// AUTO-EXPAND: 600ms hover-over-collapsed-parent during a topic drag
//   auto-expands that branch so users can drop inside. Matches Finder /
//   Linear / Notion tree DnD ergonomics.
//
// SERVER INVARIANTS (TopicsHandler.Update): reparent validation is
//   strict — cycle_detected (self-parent OR transitive descendant),
//   max_depth_subtree (parent depth + 1 + subtree max depth > 4 rejects),
//   invalid_parent (parent missing OR in a different hub — collapsed to
//   avoid hub-existence disclosure). A successful reparent flips
//   user_modified = true so the dream engine respects manual intent,
//   parity with the rename path.
//
// ACCESSIBILITY: the ⋮ menu picker on each topic node is the full
//   kbd/touch/mobile a11y path. NO dnd-kit KeyboardSensor is registered
//   on TopicDndProvider — the picker opens a filtered search surface
//   restricted to the current hub, excludes self + descendants,
//   surfaces the current hub name as the root destination (same label
//   as the HUB-ROOT DROP ZONE above), and calls the SAME
//   useTopicMove.moveTopicWithUndo code path as drag/drop (so undo +
//   error mapping + exact-position restore all work for kbd users
//   too).
//
// dnd-kit MODIFIERS: do NOT add restrictToVerticalAxis or a global
//   autoScroll modifier to TopicDndProvider. The provider wraps BOTH
//   the pinned sidebar and the main content (layout.tsx:261) — memory
//   drags from /memories → pinned tree need horizontal travel across
//   the layout boundary. A blanket modifier would regress the flow.
//   If tree-local auto-scroll is ever needed, scope it to a wrapping
//   <div> inside topic-tree-content.tsx, not the shared provider.
//
// HUB SCOPING: the ⋮ picker and the drop target set are strictly
//   same-hub. The server guards cross-hub at PATCH /v1/topics/{id}
//   via invalid_parent, but the UI must never SURFACE a cross-hub
//   destination — the picker is fed from useTopics() which is already
//   scoped to useAuth().activeHubId.
// ═══════════════════════════════════════════════
"use client";

import { useState } from "react";
import { Section, DemoCard } from "../_shared";
import {
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Code,
  ArrowLeft,
  FolderOpen,
  Plus,
  X,
  Rocket,
  ShieldCheck,
  Database,
  Layout,
  FileText,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

/* ── Mock topic tree data ── */

interface MockTreeNode {
  id: string;
  name: string;
  icon: LucideIcon;
  count: number;
  children: MockTreeNode[];
}

const MOCK_TREE: MockTreeNode[] = [
  {
    id: "1",
    name: "Deployment",
    icon: Rocket,
    count: 23,
    children: [
      { id: "1a", name: "Staging", icon: Rocket, count: 8, children: [] },
      { id: "1b", name: "Production", icon: Rocket, count: 11, children: [] },
      { id: "1c", name: "Rollback", icon: Rocket, count: 4, children: [] },
    ],
  },
  {
    id: "2",
    name: "Authentication",
    icon: ShieldCheck,
    count: 18,
    children: [
      { id: "2a", name: "OAuth", icon: ShieldCheck, count: 6, children: [] },
      { id: "2b", name: "Tokens", icon: ShieldCheck, count: 5, children: [] },
    ],
  },
  {
    id: "3",
    name: "Go Patterns",
    icon: Code,
    count: 31,
    children: [],
  },
  {
    id: "4",
    name: "Database",
    icon: Database,
    count: 15,
    children: [],
  },
  {
    id: "5",
    name: "Frontend",
    icon: Layout,
    count: 22,
    children: [],
  },
  {
    id: "6",
    name: "Team Docs",
    icon: FileText,
    count: 9,
    children: [],
  },
];

/* ── Reusable tree node (mirrors production TopicTreeNode) ── */

function DemoTreeNode({
  node,
  depth,
  activeId,
  expandedIds,
  onToggleExpand,
  onSelect,
}: {
  node: MockTreeNode;
  depth: number;
  activeId?: string;
  expandedIds: Set<string>;
  onToggleExpand: (id: string) => void;
  onSelect?: (id: string) => void;
}) {
  const hasChildren = node.children.length > 0;
  const isExpanded = expandedIds.has(node.id);
  const isActive = activeId === node.id;
  const Icon = node.icon;

  return (
    <div>
      <div
        className={`flex items-center gap-1.5 py-1.5 pr-2 rounded-lg cursor-pointer transition-colors ${
          isActive ? "bg-foreground/[0.06]" : "hover:bg-foreground/[0.03]"
        }`}
        style={{ paddingLeft: `${8 + depth * 16}px` }}
        onClick={() => onSelect?.(node.id)}
      >
        <button
          onClick={(e) => {
            e.stopPropagation();
            if (hasChildren) onToggleExpand(node.id);
          }}
          className={`p-0.5 rounded shrink-0 transition-transform ${
            hasChildren
              ? "text-fg-4 hover:text-fg-3"
              : "text-transparent pointer-events-none"
          } ${isExpanded ? "rotate-90" : ""}`}
        >
          <ChevronRight className="h-3 w-3" />
        </button>
        <Icon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        <span
          className={`text-[15px] truncate flex-1 ${
            isActive ? "text-foreground font-medium" : "text-fg-2"
          }`}
        >
          {node.name}
        </span>
        <span className="text-[13px] text-fg-4 tabular-nums shrink-0">
          {node.count}
        </span>
      </div>
      {isExpanded &&
        hasChildren &&
        node.children.map((child) => (
          <DemoTreeNode
            key={child.id}
            node={child}
            depth={depth + 1}
            activeId={activeId}
            expandedIds={expandedIds}
            onToggleExpand={onToggleExpand}
            onSelect={onSelect}
          />
        ))}
    </div>
  );
}

/* ── Tree content (mirrors TopicTreeContent) ── */

function DemoTreeContent({
  activeId,
  expandedIds,
  onToggleExpand,
  onSelect,
}: {
  activeId?: string;
  expandedIds: Set<string>;
  onToggleExpand: (id: string) => void;
  onSelect?: (id: string) => void;
}) {
  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-y-auto py-2 px-1">
        {MOCK_TREE.map((node) => (
          <DemoTreeNode
            key={node.id}
            node={node}
            depth={0}
            activeId={activeId}
            expandedIds={expandedIds}
            onToggleExpand={onToggleExpand}
            onSelect={onSelect}
          />
        ))}
      </div>
      <div className="border-t border-border/30 px-3 py-2.5">
        <div className="flex items-center gap-2 mb-2">
          <FolderOpen className="h-3 w-3 text-fg-4" />
          <span className="text-[13px] text-fg-3">3 unassigned memories</span>
        </div>
        <button className="flex items-center gap-1.5 w-full px-2 py-1.5 rounded-lg text-[14px] text-fg-3 hover:text-fg-2 hover:bg-foreground/[0.03] transition-colors cursor-pointer">
          <Plus className="h-3 w-3" />
          New topic
        </button>
      </div>
    </div>
  );
}

/* ── Main section ── */

export function TreePanelSection() {
  const [activeId, setActiveId] = useState<string | undefined>("1");
  const [expandedIds, setExpandedIds] = useState<Set<string>>(
    new Set(["1", "2"]),
  );
  const [overlayOpen, setOverlayOpen] = useState(false);
  const [mobileSelected, setMobileSelected] = useState<string | null>(null);

  const onToggleExpand = (id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <Section
      title="6. Tree Panel"
      description="Topic tree — both navigator AND manipulation canvas. Three responsive modes (desktop pinned, desktop overlay, mobile bottom sheet). Supports memory → topic drops from every memory surface + topic → topic drag-to-reparent within the current hub. Kbd/touch/mobile accessibility via the ⋮ menu picker on each node (no dnd-kit KeyboardSensor). See manipulation rules block at the top of this file for cycle/depth/hub invariants."
    >
      {/* 22a. Desktop pinned mode */}
      <DemoCard label="22a. Desktop &mdash; pinned sidebar (content shifts right)">
        <p className="text-[12px] text-fg-2 mb-4">
          Full-height sticky sidebar. Logo stays fixed (same position always).
          Tree header aligns with CONTENT_TOP. ChevronsLeft collapses. Content
          push animated via motion.div width 0&harr;280. Hover left edge reveals
          overlay. Persisted.
        </p>
        <div
          className="rounded-xl overflow-hidden flex"
          style={{
            border: "1px solid var(--border)",
            background: "var(--background)",
            height: 360,
          }}
        >
          {/* Pinned sidebar — logo stays fixed outside, tree starts at CONTENT_TOP */}
          <div
            className="w-52 shrink-0 flex flex-col h-full overflow-hidden"
            style={{
              borderRight: "1px solid oklch(from var(--border) l c h / 0.3)",
              background: "var(--card)",
            }}
          >
            {/* Header — paddingTop matches CONTENT_TOP so "Topics" aligns with content title */}
            <div
              className="flex items-center justify-between px-3 pb-1"
              style={{ paddingTop: 72 }}
            >
              <span className="text-[15px] font-semibold text-fg-2">
                Topics
              </span>
              <button
                className="p-1 rounded text-fg-4 hover:text-fg-3 transition-colors cursor-pointer"
                title="Collapse"
              >
                <ChevronsLeft className="size-3.5" />
              </button>
            </div>
            <DemoTreeContent
              activeId={activeId}
              expandedIds={expandedIds}
              onToggleExpand={onToggleExpand}
              onSelect={(id) => setActiveId(id)}
            />
          </div>
          {/* Content area */}
          <div className="flex-1 p-6 flex flex-col items-center justify-center">
            <p className="text-[14px] text-fg-3">Content shifts right</p>
            <p className="text-[12px] text-fg-4 mt-1">
              Sidebar takes space in layout flow
            </p>
          </div>
        </div>
      </DemoCard>

      {/* 22b. Desktop hover-reveal (peeking) */}
      <DemoCard label="22b. Desktop &mdash; hover-reveal overlay (peeking)">
        <p className="text-[12px] text-fg-2 mb-4">
          No toggle button. Hover left edge (12px zone, 150ms delay) reveals
          overlay. Starts at HEADER_TOP (not full height). &raquo; pins to
          layout flow. Mouse-leave dismisses after 300ms. Backdrop at 8%
          opacity.
        </p>
        <div
          className="rounded-xl overflow-hidden relative"
          style={{
            border: "1px solid var(--border)",
            background: "var(--background)",
            height: 360,
          }}
        >
          {/* Content behind */}
          <div className="absolute inset-0 flex flex-col items-center justify-center">
            <p className="text-[14px] text-fg-3">Content stays full width</p>
            <p className="text-[12px] text-fg-4 mt-1">
              Hover left edge to reveal
            </p>
          </div>

          {/* Hover zone indicator */}
          {!overlayOpen && (
            <div
              className="absolute left-0 top-0 bottom-0 w-3 z-10 cursor-pointer"
              onClick={() => setOverlayOpen(true)}
              style={{
                background: "oklch(from var(--foreground) l c h / 0.02)",
              }}
            />
          )}

          {/* Overlay — starts below header area, not full height */}
          {overlayOpen && (
            <>
              <div
                className="absolute inset-0 z-10"
                style={{ background: "rgba(0,0,0,0.08)" }}
                onClick={() => setOverlayOpen(false)}
              />
              <div
                className="absolute left-0 bottom-0 z-20 w-52 flex flex-col overflow-hidden"
                style={{
                  top: 48,
                  background: "var(--card)",
                  borderRight: "1px solid var(--border)",
                  borderTopRightRadius: "12px",
                  boxShadow:
                    "2px 0 16px rgba(0,0,0,0.04), 8px 0 40px rgba(0,0,0,0.02)",
                }}
              >
                <div className="flex items-center justify-between px-3 pt-3 pb-1">
                  <span className="text-[15px] font-semibold text-fg-2">
                    Topics
                  </span>
                  <button
                    onClick={() => setOverlayOpen(false)}
                    className="p-1 rounded text-fg-4 hover:text-fg-3 transition-colors cursor-pointer"
                    title="Pin open"
                  >
                    <ChevronsRight className="size-3.5" />
                  </button>
                </div>
                <DemoTreeContent
                  activeId={activeId}
                  expandedIds={expandedIds}
                  onToggleExpand={onToggleExpand}
                  onSelect={(id) => setActiveId(id)}
                />
              </div>
            </>
          )}
        </div>
      </DemoCard>

      {/* 22c. Mobile bottom sheet (current production) */}
      <DemoCard label="22c. Mobile &mdash; bottom sheet (current production)">
        <p className="text-[12px] text-fg-2 mb-4">
          Code icon next to &ldquo;Your Topics&rdquo; page header (right side).
          Opens bottom sheet overlay with drag handle. Toggle is contextual, not
          in fixed header.
        </p>
        <div
          className="rounded-xl overflow-hidden relative mx-auto"
          style={{
            border: "1px solid var(--border)",
            background: "var(--background)",
            width: 280,
            height: 400,
          }}
        >
          {/* Page header with Code toggle */}
          <div className="flex items-center justify-between px-4 pt-4 pb-2">
            <div>
              <h3 className="text-[16px] font-semibold text-foreground">
                Your Topics
              </h3>
              <p className="text-[12px] text-fg-3">
                10 memories &middot; 5 topics
              </p>
            </div>
            <button className="p-1.5 rounded-md text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer">
              <Code className="size-4" />
            </button>
          </div>

          {/* Bottom sheet */}
          <div
            className="absolute left-0 right-0 bottom-0 rounded-t-2xl flex flex-col"
            style={{
              background: "var(--card)",
              borderTop: "1px solid var(--border)",
              boxShadow: "0 -2px 16px rgba(0,0,0,0.06)",
              height: "55%",
            }}
          >
            <div className="flex flex-col items-center pt-2 pb-1">
              <div className="w-8 h-1 rounded-full bg-surface-3 mb-2" />
              <div className="flex items-center justify-between w-full px-4">
                <span className="text-[15px] font-semibold text-fg-2">
                  Topics
                </span>
                <button className="p-1 rounded text-fg-3">
                  <X className="size-3.5" />
                </button>
              </div>
            </div>
            <DemoTreeContent
              activeId={activeId}
              expandedIds={expandedIds}
              onToggleExpand={onToggleExpand}
              onSelect={(id) => setActiveId(id)}
            />
          </div>
        </div>
      </DemoCard>

      {/* 22d. Mobile master/detail push (target) */}
      <DemoCard label="22d. Mobile &mdash; master/detail push (target, Notion-style)">
        <p className="text-[12px] text-fg-2 mb-4">
          Tree IS the page content. Tap a topic &rarr; detail slides in from
          right. Back arrow returns. No separate toggle button. No overlay. This
          is the target pattern for mobile topic navigation.
        </p>
        <div
          className="rounded-xl overflow-hidden relative mx-auto"
          style={{
            border: "1px solid var(--border)",
            background: "var(--background)",
            width: 280,
            height: 400,
          }}
        >
          <div
            className="flex h-full"
            style={{
              width: "200%",
              transform: mobileSelected ? "translateX(-50%)" : "translateX(0)",
              transition: "transform 0.15s var(--ease-spring)",
            }}
          >
            {/* Tree panel */}
            <div className="w-1/2 h-full overflow-y-auto">
              <div className="px-4 pt-4 pb-2">
                <h3 className="text-[16px] font-semibold text-foreground">
                  Your Topics
                </h3>
                <p className="text-[12px] text-fg-3">
                  147 memories &middot; 6 topics
                </p>
              </div>
              <div className="px-1">
                {MOCK_TREE.map((node) => (
                  <div
                    key={node.id}
                    onClick={() => setMobileSelected(node.id)}
                    className="flex items-center gap-2 px-3 py-2.5 rounded-lg cursor-pointer hover:bg-surface-2 active:bg-surface-3 transition-colors"
                  >
                    <node.icon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
                    <span className="text-[15px] text-fg-2 flex-1 truncate">
                      {node.name}
                    </span>
                    <span className="text-[13px] text-fg-4 tabular-nums">
                      {node.count}
                    </span>
                    <ChevronRight className="size-3.5 text-fg-4" />
                  </div>
                ))}
              </div>
            </div>

            {/* Detail panel */}
            <div className="w-1/2 h-full overflow-y-auto">
              <div
                className="sticky top-0 flex items-center gap-2 px-3 py-2.5"
                style={{
                  background: "oklch(from var(--background) l c h / 0.85)",
                  backdropFilter: "blur(12px)",
                }}
              >
                <button
                  onClick={() => setMobileSelected(null)}
                  className="flex items-center gap-1.5 text-[14px] text-fg-2 hover:text-fg-1 transition-colors cursor-pointer py-0.5 px-1 rounded-md hover:bg-surface-2"
                >
                  <ArrowLeft className="size-3.5" />
                  {mobileSelected
                    ? (MOCK_TREE.find((t) => t.id === mobileSelected)?.name ??
                      "Back")
                    : "Back"}
                </button>
              </div>
              <div className="px-4 py-4 flex flex-col items-center justify-center h-[calc(100%-44px)]">
                <p className="text-[14px] text-fg-3">Topic detail</p>
                <p className="text-[12px] text-fg-4 mt-1">Memory list here</p>
              </div>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* 22e. Tree node states */}
      <DemoCard label="22e. Tree node states">
        <div className="grid grid-cols-2 gap-4">
          {/* Normal */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Collapsed (has children)
            </p>
            <div
              className="rounded-lg p-2"
              style={{ background: "var(--card)" }}
            >
              <div className="flex items-center gap-1.5 py-1.5 px-2 rounded-lg hover:bg-foreground/[0.03] cursor-pointer">
                <ChevronRight className="h-3 w-3 text-fg-4" />
                <Rocket className="h-3.5 w-3.5 text-fg-3" />
                <span className="text-[15px] text-fg-2 flex-1">Deployment</span>
                <span className="text-[13px] text-fg-4 tabular-nums">23</span>
              </div>
            </div>
          </div>

          {/* Active */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Active (selected)
            </p>
            <div
              className="rounded-lg p-2"
              style={{ background: "var(--card)" }}
            >
              <div className="flex items-center gap-1.5 py-1.5 px-2 rounded-lg bg-foreground/[0.06] cursor-pointer">
                <ChevronRight className="h-3 w-3 text-fg-4 rotate-90" />
                <Rocket className="h-3.5 w-3.5 text-fg-3" />
                <span className="text-[15px] text-foreground font-medium flex-1">
                  Deployment
                </span>
                <span className="text-[13px] text-fg-4 tabular-nums">23</span>
              </div>
            </div>
          </div>

          {/* Leaf (no children) */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Leaf (no children)
            </p>
            <div
              className="rounded-lg p-2"
              style={{ background: "var(--card)" }}
            >
              <div className="flex items-center gap-1.5 py-1.5 px-2 rounded-lg hover:bg-foreground/[0.03] cursor-pointer">
                <ChevronRight className="h-3 w-3 text-transparent" />
                <Code className="h-3.5 w-3.5 text-fg-3" />
                <span className="text-[15px] text-fg-2 flex-1">
                  Go Patterns
                </span>
                <span className="text-[13px] text-fg-4 tabular-nums">31</span>
              </div>
            </div>
          </div>

          {/* Drop target */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Drop target (DnD hover)
            </p>
            <div
              className="rounded-lg p-2"
              style={{ background: "var(--card)" }}
            >
              <div className="flex items-center gap-1.5 py-1.5 px-2 rounded-lg ring-1 ring-foreground/20 bg-foreground/[0.04] cursor-pointer">
                <ChevronRight className="h-3 w-3 text-fg-4" />
                <Database className="h-3.5 w-3.5 text-fg-3" />
                <span className="text-[15px] text-fg-2 flex-1">Database</span>
                <span className="text-[13px] text-fg-4 tabular-nums">15</span>
              </div>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* 22f. Empty states */}
      <DemoCard label="22f. Empty states &mdash; pre-dream, dreams off">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Waiting for first dream
            </p>
            <div
              className="rounded-xl p-6 flex flex-col items-center text-center gap-2"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <span
                className="text-[14px]"
                style={{ color: "var(--signature)" }}
              >
                ✦
              </span>
              <p className="text-[14px] text-fg-2">
                Topics appear after your first dream
              </p>
              <p className="text-[13px] text-fg-3">
                memax organizes your memories during the nightly dream cycle.
              </p>
            </div>
          </div>

          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Dreams disabled
            </p>
            <div
              className="rounded-xl p-6 flex flex-col items-center text-center gap-2"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <span
                className="text-[14px]"
                style={{ color: "oklch(0.62 0.16 290)" }}
              >
                ✦
              </span>
              <p className="text-[14px] text-fg-2">Dreams are turned off</p>
              <p className="text-[13px] text-fg-3">
                Enable dreams to organize your knowledge.
              </p>
              <button
                className="text-[13px] cursor-pointer underline mt-1"
                style={{ color: "oklch(0.62 0.16 290)" }}
              >
                Enable in Settings
              </button>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* Design notes */}
      <div className="text-[12px] text-fg-4 space-y-0.5 mt-2">
        <p>
          Production files: topic-tree-panel.tsx (provider + pinned/peek),
          topic-tree-content.tsx (data + empty states + root drop row),
          topic-tree-node.tsx (recursive node + grip + ⋮ menu + invalid-drop
          ring), topic-dnd-provider.tsx (shared DndContext, memory + topic
          discriminator, dropTargetsActive gate), topic-dnd-hooks.tsx
          (useDraggableMemory, useDraggableTopic, useDroppableTopic,
          TopicDragContext), use-topic-move.ts (mover hook),
          topic-move-picker.tsx (kbd/touch a11y path). layout.tsx
          (FloatingTreeToggle, SidebarSlot, BrandLogo).
        </p>
        <p>
          Desktop (Notion/Linear-style floating-toggle model): no permanent rail
          — main content goes full width when the tree is closed.
          FloatingTreeToggle renders a small ChevronsRight button to the right
          of the fixed BrandLogo when `!isPinned && !isDragSessionOpen`, opening
          the tree on click. SidebarSlot animates width 0&harr;280 based on
          `isPinned || isDragSessionOpen`; the close button lives inside
          PinnedTreeSidebar's top row (`marginTop: 32 + h-12 items-center`) on
          the SAME vertical band as the fixed BrandLogo, so open→close has no
          vertical jump. No redundant &ldquo;Topic Explorer&rdquo; header label
          — the content and close button already identify the sidebar. Tree is
          always mounted in SidebarSlot so droppables register from page load;
          dropTargetsActive on TopicDragContext (`isPinned ||
          isDragSessionOpen`) gates collision detection so collapsed tree nodes
          don't match drops in main content area. isPinned persisted under
          `memax_tree_pinned` localStorage key (legacy name, semantics are now
          "is the tree open"). No hover-peek.
        </p>
        <p>
          Typography (kitchen row-title rule, memory `b1f8fa7a` / §29 rules
          32-34): 14px for ALL row titles — hub row and topic rows share the
          same size. Hierarchy via opacity + weight, never size. Hub row is the
          strongest entry: `text-[14px] font-medium text-fg-1` — reads as the
          tree root anchor. Topic rows fade as scaffolding: inactive
          `font-normal text-fg-2`, active `font-medium text-fg-1`. Trailing
          memory counts are 12px `text-fg-4` metadata. No 15px bumps, no
          semibold, no size-based hierarchy.
        </p>
        <p>
          Mobile: Code icon in page header (topic-grid.tsx) opens bottom sheet
          via TopicTreePanelOverlayHost. No rail, no SidebarSlot — mobile uses
          its own drawer surface. Future target: master/detail push (22d).
        </p>
      </div>
    </Section>
  );
}
