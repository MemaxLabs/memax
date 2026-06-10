// ═══════════════════════════════════════════════
// NORTH STAR — memax Design System Workbench
//
// Single visual reference for all design patterns.
// Every section maps to production code.
// Switch palettes to explore color options.
//
// To add a section: create _sections/NN-name.tsx,
// export a named component, import and add below.
//
// Design spec: docs/design/memax-design-system.md
// UX strategy: docs/design/uiux-strategy.md
// ═══════════════════════════════════════════════
"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  Sun,
  Moon,
  ChevronRight,
  ArrowLeft,
  ChevronsLeft,
  ChevronsRight,
} from "lucide-react";
import { FAST } from "@/tokens/motion";
import { SIGNATURE } from "./_shared";
import {
  KitchenProvider,
  PALETTES,
  type PaletteKey,
  useKitchen,
} from "./_kitchen-context";

// ─── Sections ───
// Foundations (design tokens — read first)
import { DesignPrinciplesSection } from "./_sections/00-design-principles";
import { BrandingSection } from "./_sections/11-branding";
import { ColorTokensSection } from "./_sections/13-color-tokens";
import { TypographySection } from "./_sections/12-typography";
import { SurfacesSection } from "./_sections/14-surfaces";
import { LayoutSection } from "./_sections/20-layout";
import { MotionTokensSection } from "./_sections/18-motion-tokens";
// Components (reusable pieces)
import { ControlsSection } from "./_sections/19-controls";
import { VisualVocabSection } from "./_sections/17-visual-vocab";
import { StateMachineSection } from "./_sections/16-state-machine";
import { LoadingStatesSection } from "./_sections/08-loading-states";
import { EmptyStatesSection } from "./_sections/09-empty-states";
// Patterns (product surfaces)
import { TopicsSection } from "./_sections/01-topics";
import { MemoryRowVariantsSection } from "./_sections/04-memory-row-variants";
import { MemoryCardsSection } from "./_sections/02-memory-detail";
import { ForgetSection } from "./_sections/03-forget";
import { BatchOperationsSection } from "./_sections/25-batch-operations";
import { LoadingTextSection } from "./_sections/05-loading-text";
import { TreePanelSection } from "./_sections/06-tree-panel";
import { HubExperienceSection } from "./_sections/07-hub-experience";
import { AgentConfigsSection } from "./_sections/23-agent-configs";
import { DreamExperienceSection } from "./_sections/10-dream-experience";
// Recipes, Accessibility & Patterns
import { RecipesSection } from "./_sections/26-recipes";
import { AccessibilitySection } from "./_sections/27-accessibility";
import { AsyncActionsSection } from "./_sections/28-async-actions";
import { TopicRedesignSection } from "./_sections/29-topic-redesign";
import { MobileDockSection } from "./_sections/30-mobile-dock";
import { DevDebuggerSection } from "./_sections/32-dev-debugger";
import { MemoryMetadataSection } from "./_sections/33-memory-metadata";
import { LandingAndOnboardingSection } from "./_sections/34-landing-and-onboarding";
import { DataSectionCardSection } from "./_sections/35-data-section-card";
import { InboxSection } from "./_sections/36-inbox";
import { BarNorthStarAltSection } from "./_sections/38-bar-northstar-alt";
import { OAuthConsentSection } from "./_sections/39-oauth-consent";
import { BorderlessRedesignSection } from "./_sections/40-borderless-redesign";
import { LifecycleFlowSection } from "./_sections/41-lifecycle-flow";
import { TopicIconsSection } from "./_sections/42-topic-icons";
import { ShellV2Section } from "./_sections/43-shell-v2";
// Utilities & Archive
import { PaletteExplorerSection } from "./_sections/15-palette-explorer";
import { ExplorationsSection } from "./_sections/21-explorations";
import { CoverageGapsSection } from "./_sections/22-coverage-gaps";

// ─── Palette Switcher ───

function PaletteSwitcher() {
  const { paletteKey, setPaletteKey } = useKitchen();

  return (
    <div className="flex flex-wrap gap-1.5">
      {(Object.keys(PALETTES) as PaletteKey[]).map((key) => (
        <button
          key={key}
          onClick={() => setPaletteKey(key)}
          className={`px-2.5 py-1 rounded-md text-[12px] transition-all cursor-pointer ${
            paletteKey === key
              ? "bg-foreground text-background font-medium"
              : "bg-surface-2 text-fg-2 hover:bg-surface-3"
          }`}
        >
          <div className="flex items-center gap-1.5">
            <div
              className="w-2.5 h-2.5 rounded-full border border-border/30"
              style={{ background: PALETTES[key].signature }}
            />
            {PALETTES[key].name}
          </div>
        </button>
      ))}
    </div>
  );
}

// ─── Dark Mode Toggle ───

function DarkModeToggle() {
  const { isDark, toggleDark } = useKitchen();

  return (
    <button
      onClick={toggleDark}
      className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[12px] transition-all cursor-pointer bg-surface-2 text-fg-2 hover:bg-surface-3"
    >
      {isDark ? (
        <>
          <Moon className="size-3" /> Dark
        </>
      ) : (
        <>
          <Sun className="size-3" /> Light
        </>
      )}
    </button>
  );
}

// ─── Tree Navigation Data ───
// Nested groups — anchor-id must match auto-generated id from Section title.

interface NavItem {
  id: string;
  label: string;
}

interface NavSubgroup {
  label: string;
  items: NavItem[];
}

interface NavGroup {
  label: string;
  children: (NavItem | NavSubgroup)[];
}

function isSubgroup(node: NavItem | NavSubgroup): node is NavSubgroup {
  return "items" in node;
}

// NAV_TREE: design system hierarchy — Foundations → Components → Patterns.
// Read Foundations first (tokens + rules), then Components (reusable pieces),
// then Patterns (how they compose into product surfaces).
const NAV_TREE: NavGroup[] = [
  {
    label: "Foundations",
    children: [
      { id: "0-design-principles", label: "Design Principles" },
      { id: "11-branding", label: "Branding" },
      { id: "13-color-tokens", label: "Color System" },
      { id: "12-typography", label: "Typography" },
      { id: "14-surfaces", label: "Surfaces & Elevation" },
      { id: "20-layout", label: "Spacing & Layout" },
      { id: "18-motion-tokens", label: "Motion" },
      { id: "27-accessibility", label: "Accessibility" },
      { id: "15-palette-explorer", label: "Palette Explorer" },
    ],
  },
  {
    label: "Components",
    children: [
      { id: "19-controls", label: "Controls" },
      { id: "17-visual-vocabulary", label: "Indicators" },
      { id: "16-state-machine", label: "State Machine" },
      { id: "8-loading-states", label: "Loading States" },
      { id: "9-empty-states", label: "Empty States" },
      { id: "35-data-section-card", label: "Data Section Card" },
      { id: "28-async-action-pattern", label: "Async Actions" },
    ],
  },
  {
    label: "Patterns",
    children: [
      { id: "26-composition-recipes", label: "Composition Recipes" },
      {
        label: "Memory",
        items: [
          { id: "4-memory-row-variants", label: "Memory Rows" },
          { id: "1-topics", label: "Topics" },
          { id: "2-memory-detail-recall", label: "Memory Detail" },
          { id: "3-forget-experience", label: "Forget Flow" },
          { id: "25-batch-operations", label: "Batch Operations" },
          { id: "33-memory-metadata", label: "Memory Metadata" },
        ],
      },
      {
        label: "Bar & Input",
        items: [
          { id: "38-bar-north-star-alt", label: "Bar — North Star (alt)" },
          { id: "5-loading-text", label: "Loading Text" },
          { id: "32-dev-debugger", label: "Dev Debugger" },
        ],
      },
      {
        label: "Navigation",
        items: [
          { id: "6-tree-panel", label: "Tree Panel" },
          { id: "7-hub-experience", label: "Hub Experience" },
          { id: "23-agent-management", label: "Agent Management" },
          { id: "30-mobile-dock-navigation", label: "Mobile Dock" },
        ],
      },
      { id: "10-dream-experience", label: "Dreams" },
      { id: "29-topic-redesign", label: "Topic Redesign" },
      { id: "42-topic-icons", label: "Topic Icons (proposal)" },
      { id: "40-borderless-redesign-proposal", label: "Borderless Redesign" },
      { id: "41-lifecycle-dream-delta-e2e-flow", label: "Lifecycle Flow" },
      { id: "36-inbox", label: "Inbox" },
      { id: "34-landing-onboarding", label: "Landing + Onboarding" },
      { id: "39-oauth-consent", label: "OAuth Consent" },
    ],
  },
  {
    label: "Archive",
    children: [
      { id: "21-explorations", label: "Explorations" },
      { id: "22-coverage-gaps", label: "Coverage Gaps" },
    ],
  },
];

// Flatten all section IDs for IntersectionObserver
function collectIds(groups: NavGroup[]): string[] {
  const ids: string[] = [];
  for (const g of groups) {
    for (const child of g.children) {
      if (isSubgroup(child)) {
        for (const item of child.items) ids.push(item.id);
      } else {
        ids.push(child.id);
      }
    }
  }
  return ids;
}

const ALL_SECTION_IDS = collectIds(NAV_TREE);

// ─── Desktop Tree Sidebar ───

/** Check if a group/subgroup contains the active section */
function groupContainsId(
  children: (NavItem | NavSubgroup)[],
  id: string,
): boolean {
  return children.some((c) =>
    isSubgroup(c) ? c.items.some((i) => i.id === id) : c.id === id,
  );
}

function TreeSidebar({ activeId }: { activeId: string }) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const toggle = useCallback((key: string) => {
    setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }));
  }, []);

  const scrollTo = useCallback((id: string) => {
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth" });
  }, []);

  return (
    <nav className="flex flex-col gap-0.5 select-none">
      {NAV_TREE.map((group) => {
        const groupKey = group.label;
        const isGroupCollapsed = collapsed[groupKey];
        const hasActive = groupContainsId(group.children, activeId);

        return (
          <div key={groupKey}>
            {/* Group header — uppercase section label (Notion: ~40% inactive) */}
            <button
              onClick={() => toggle(groupKey)}
              className={`flex items-center gap-1 w-full px-2 py-1.5 rounded-md text-[10px] font-medium uppercase tracking-wider cursor-pointer transition-colors hover:bg-surface-2 ${
                hasActive ? "text-fg-2" : "text-fg-3"
              }`}
            >
              <ChevronRight
                className="size-3 shrink-0 transition-transform duration-150"
                style={{
                  transform: isGroupCollapsed
                    ? "rotate(0deg)"
                    : "rotate(90deg)",
                }}
              />
              {group.label}
            </button>

            {/* Children */}
            {!isGroupCollapsed && (
              <div className="ml-2 border-l border-border/20">
                {group.children.map((child) => {
                  if (isSubgroup(child)) {
                    const subKey = `${groupKey}/${child.label}`;
                    const isSubCollapsed = collapsed[subKey];
                    const subHasActive = child.items.some(
                      (i) => i.id === activeId,
                    );

                    return (
                      <div key={subKey}>
                        {/* Subgroup header — same level as items */}
                        <button
                          onClick={() => toggle(subKey)}
                          className={`flex items-center gap-1 w-full pl-3 pr-2 py-1 rounded-r-md text-[10px] cursor-pointer transition-colors hover:bg-surface-1 ${
                            subHasActive ? "text-fg-2" : "text-fg-3"
                          }`}
                        >
                          <ChevronRight
                            className="size-2.5 shrink-0 transition-transform duration-150"
                            style={{
                              transform: isSubCollapsed
                                ? "rotate(0deg)"
                                : "rotate(90deg)",
                            }}
                          />
                          {child.label}
                        </button>

                        {/* Subgroup items */}
                        {!isSubCollapsed && (
                          <div className="ml-3 border-l border-border/15">
                            {child.items.map((item) => (
                              <TreeLeaf
                                key={item.id}
                                item={item}
                                isActive={item.id === activeId}
                                onClick={() => scrollTo(item.id)}
                              />
                            ))}
                          </div>
                        )}
                      </div>
                    );
                  }

                  // Direct leaf item under a group
                  return (
                    <TreeLeaf
                      key={child.id}
                      item={child}
                      isActive={child.id === activeId}
                      onClick={() => scrollTo(child.id)}
                    />
                  );
                })}
              </div>
            )}
          </div>
        );
      })}
    </nav>
  );
}

/** Leaf node — clickable section link */
function TreeLeaf({
  item,
  isActive,
  onClick,
}: {
  item: NavItem;
  isActive: boolean;
  onClick: () => void;
}) {
  return (
    <a
      href={`#${item.id}`}
      onClick={(e) => {
        e.preventDefault();
        onClick();
      }}
      className={`block pl-3 pr-2 py-1 text-[12px] rounded-r-md transition-colors relative ${
        isActive
          ? "text-fg-1 font-medium bg-surface-2"
          : "text-fg-2 hover:text-fg-1 hover:bg-surface-1"
      }`}
    >
      {isActive && (
        <div
          className="absolute left-0 top-1 bottom-1 w-0.5 rounded-full"
          style={{ background: "var(--signature)" }}
        />
      )}
      {item.label}
    </a>
  );
}

// ─── Scroll-spy hook ───

function useScrollSpy(sectionIds: string[]) {
  const [activeId, setActiveId] = useState(sectionIds[0] ?? "");
  const ratioMap = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          ratioMap.current.set(entry.target.id, entry.intersectionRatio);
        }
        // Pick the section with the highest visible ratio
        let best = "";
        let bestRatio = 0;
        for (const [id, ratio] of ratioMap.current) {
          if (ratio > bestRatio) {
            best = id;
            bestRatio = ratio;
          }
        }
        if (best) setActiveId(best);
      },
      { threshold: [0, 0.1, 0.25, 0.5, 0.75, 1] },
    );

    for (const id of sectionIds) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    }

    return () => observer.disconnect();
  }, [sectionIds]);

  return activeId;
}

// ─── Section component map (for mobile single-section rendering) ───

const SECTION_COMPONENTS: Record<string, React.ComponentType> = {
  "0-design-principles": DesignPrinciplesSection,
  "26-composition-recipes": RecipesSection,
  "27-accessibility": AccessibilitySection,
  "1-topics": TopicsSection,
  "2-memory-detail-recall": MemoryCardsSection,
  "3-forget-experience": ForgetSection,
  "5-loading-text": LoadingTextSection,
  "6-tree-panel": TreePanelSection,
  "7-hub-experience": HubExperienceSection,
  "8-loading-states": LoadingStatesSection,
  "9-empty-states": EmptyStatesSection,
  "10-dream-experience": DreamExperienceSection,
  "29-topic-redesign": TopicRedesignSection,
  "30-mobile-dock-navigation": MobileDockSection,
  "11-branding": BrandingSection,
  "12-typography": TypographySection,
  "13-color-tokens": ColorTokensSection,
  "14-surfaces": SurfacesSection,
  "15-palette-explorer": PaletteExplorerSection,
  "16-state-machine": StateMachineSection,
  "17-visual-vocabulary": VisualVocabSection,
  "18-motion-tokens": MotionTokensSection,
  "19-controls": ControlsSection,
  "20-layout": LayoutSection,
  "21-explorations": ExplorationsSection,
  "22-coverage-gaps": CoverageGapsSection,
  "23-agent-management": AgentConfigsSection,
  "25-batch-operations": BatchOperationsSection,
  "28-async-action-pattern": AsyncActionsSection,
  "32-dev-debugger": DevDebuggerSection,
  "33-memory-metadata": MemoryMetadataSection,
  "34-landing-onboarding": LandingAndOnboardingSection,
  "35-data-section-card": DataSectionCardSection,
  "36-inbox": InboxSection,
  "38-bar-north-star-alt": BarNorthStarAltSection,
  "39-oauth-consent": OAuthConsentSection,
  "40-borderless-redesign-proposal": BorderlessRedesignSection,
  "41-lifecycle-dream-delta-e2e-flow": LifecycleFlowSection,
  "42-topic-icons": TopicIconsSection,
};

// Find the label for a section ID
function sectionLabel(id: string): string {
  for (const group of NAV_TREE) {
    for (const child of group.children) {
      if (isSubgroup(child)) {
        const item = child.items.find((i) => i.id === id);
        if (item) return item.label;
      } else if (child.id === id) {
        return child.label;
      }
    }
  }
  return id;
}

// ─── Mobile Subgroup (collapsible nested section) ───

function MobileSubgroup({
  subgroup,
  onSelect,
}: {
  subgroup: NavSubgroup;
  onSelect: (id: string) => void;
}) {
  const [open, setOpen] = useState(true);

  return (
    <div>
      {/* Subgroup header — tappable toggle */}
      <button
        onClick={() => setOpen((p) => !p)}
        className="flex items-center gap-1.5 w-full px-3 py-2 text-[13px] text-fg-3 cursor-pointer"
      >
        <ChevronRight
          className="size-3 shrink-0 transition-transform duration-150"
          style={{ transform: open ? "rotate(90deg)" : "rotate(0deg)" }}
        />
        {subgroup.label}
      </button>

      {/* Items */}
      {open &&
        subgroup.items.map((item) => (
          <button
            key={item.id}
            onClick={() => onSelect(item.id)}
            className="flex items-center justify-between w-full pl-8 pr-3 py-2 rounded-lg text-[14px] text-fg-2 hover:bg-surface-2 active:bg-surface-3 transition-colors cursor-pointer"
          >
            {item.label}
            <ChevronRight className="size-3.5 text-fg-4" />
          </button>
        ))}
    </div>
  );
}

// ─── Mobile Tree View (Notion-style master/detail) ───

function MobileKitchen() {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const handleSelect = useCallback((id: string) => {
    setSelectedId(id);
    // Scroll content to top when entering a section
    requestAnimationFrame(() => {
      contentRef.current?.scrollTo(0, 0);
    });
  }, []);

  const handleBack = useCallback(() => {
    setSelectedId(null);
  }, []);

  const SelectedComponent = selectedId ? SECTION_COMPONENTS[selectedId] : null;

  return (
    <div className="h-dvh bg-background overflow-hidden">
      {/* Two-panel slider: tree (left) + detail (right) */}
      <div
        className="flex h-full"
        style={{
          width: "200%",
          transform: selectedId ? "translateX(-50%)" : "translateX(0)",
          transition: "transform 0.15s var(--ease-spring)",
        }}
      >
        {/* ── Panel 1: Tree ── */}
        <div className="w-1/2 h-full overflow-y-auto px-5 pt-12 pb-12">
          {/* Header */}
          <div className="flex items-center gap-2 mb-1">
            <span style={{ color: SIGNATURE, fontSize: 14 }}>✦</span>
            <h1 className="text-[18px] font-bold text-foreground tracking-tight">
              Design Kitchen
            </h1>
          </div>
          <p className="text-[13px] text-fg-3 mb-5">North star reference</p>

          {/* Controls */}
          <div className="mb-6 flex flex-col gap-2">
            <PaletteSwitcher />
            <DarkModeToggle />
          </div>

          {/* Tree — nested groups with subgroups */}
          <nav className="flex flex-col gap-1">
            {NAV_TREE.map((group) => (
              <div key={group.label}>
                {/* Group header */}
                <div className="text-[10px] font-medium uppercase tracking-wider text-fg-4 px-3 pt-4 pb-1">
                  {group.label}
                </div>
                {group.children.map((child) => {
                  if (isSubgroup(child)) {
                    return (
                      <MobileSubgroup
                        key={child.label}
                        subgroup={child}
                        onSelect={handleSelect}
                      />
                    );
                  }
                  return (
                    <button
                      key={child.id}
                      onClick={() => handleSelect(child.id)}
                      className="flex items-center justify-between w-full px-3 py-2.5 rounded-lg text-[14px] text-fg-2 hover:bg-surface-2 active:bg-surface-3 transition-colors cursor-pointer"
                    >
                      {child.label}
                      <ChevronRight className="size-3.5 text-fg-4" />
                    </button>
                  );
                })}
              </div>
            ))}
          </nav>

          {/* Footer */}
          <div className="mt-10 px-3">
            <p className="text-[10px] text-fg-4">
              memax design kitchen · docs/design/memax-design-system.md
            </p>
          </div>
        </div>

        {/* ── Panel 2: Section Detail ── */}
        <div ref={contentRef} className="w-1/2 h-full overflow-y-auto">
          {/* Back header */}
          <div
            className="sticky top-0 z-10 flex items-center gap-2 px-4 py-3"
            style={{
              background: "oklch(from var(--background) l c h / 0.85)",
              backdropFilter: "blur(12px)",
              WebkitBackdropFilter: "blur(12px)",
            }}
          >
            <button
              onClick={handleBack}
              className="flex items-center gap-1.5 text-[14px] text-fg-2 hover:text-fg-1 transition-colors cursor-pointer -ml-1 py-1 px-1.5 rounded-md hover:bg-surface-2"
            >
              <ArrowLeft className="size-4" />
              <span>{selectedId ? sectionLabel(selectedId) : "Back"}</span>
            </button>
          </div>

          {/* Section content */}
          <div className="px-5 pb-12">
            {SelectedComponent && <SelectedComponent />}
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Desktop Layout ───
//
// Matches production topic-tree-panel.tsx visual:
// - var(--card) bg, borderRight, borderTopRightRadius 12px, shadow
// - Starts below top offset (not from page top)
// - Slide animation with spring easing
//
// Three states:
// 1. Pinned — sidebar in flow, content shifts. « collapses.
// 2. Collapsed — narrow strip with ». Hover reveals overlay.
// 3. Hover overlay — same tree visual, floats over content. Click outside dismisses.

const SIDEBAR_STORAGE_KEY = "kitchen_sidebar_open";
const SIDEBAR_W = 260;

function DesktopKitchen() {
  const activeId = useScrollSpy(ALL_SECTION_IDS);

  const [pinned, setPinned] = useState(() => {
    if (typeof window === "undefined") return true;
    try {
      return localStorage.getItem(SIDEBAR_STORAGE_KEY) !== "false";
    } catch {
      return true;
    }
  });

  const [peeking, setPeeking] = useState(false);
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const isOpen = pinned || peeking;

  const collapse = useCallback(() => {
    setPinned(false);
    setPeeking(false);
    localStorage.setItem(SIDEBAR_STORAGE_KEY, "false");
  }, []);

  const pin = useCallback(() => {
    setPinned(true);
    setPeeking(false);
    localStorage.setItem(SIDEBAR_STORAGE_KEY, "true");
  }, []);

  // Hover left edge → peek
  useEffect(() => {
    if (pinned) return;
    const onMove = (e: MouseEvent) => {
      if (e.clientX <= 12 && !peeking) {
        if (!hoverTimer.current) {
          hoverTimer.current = setTimeout(() => {
            setPeeking(true);
            hoverTimer.current = null;
          }, 150);
        }
      } else if (e.clientX > 12 && hoverTimer.current) {
        clearTimeout(hoverTimer.current);
        hoverTimer.current = null;
      }
    };
    window.addEventListener("mousemove", onMove);
    return () => window.removeEventListener("mousemove", onMove);
  }, [pinned, peeking]);

  const onSidebarEnter = useCallback(() => {
    if (leaveTimer.current) {
      clearTimeout(leaveTimer.current);
      leaveTimer.current = null;
    }
  }, []);
  const onSidebarLeave = useCallback(() => {
    if (!pinned && peeking) {
      leaveTimer.current = setTimeout(() => setPeeking(false), 300);
    }
  }, [pinned, peeking]);

  // Shared tree content
  const treeContent = (
    <>
      <div className="px-4 mb-2 flex flex-col gap-2">
        <PaletteSwitcher />
        <DarkModeToggle />
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto px-1">
        <TreeSidebar activeId={activeId} />
      </div>
      <div className="border-t border-border/30 px-4 py-2.5">
        <p className="text-[10px] text-fg-4">
          docs/design/memax-design-system.md
        </p>
      </div>
    </>
  );

  return (
    <div className="min-h-screen bg-background flex">
      {/* ── Pinned: full-height sticky sidebar ── */}
      {pinned && (
        <aside
          className="sticky top-0 h-screen shrink-0 flex flex-col"
          style={{
            width: SIDEBAR_W,
            background: "var(--card)",
            borderRight: "1px solid var(--border)",
            scrollbarWidth: "none",
            msOverflowStyle: "none",
          }}
        >
          <div className="flex items-center justify-between px-4 pt-4 pb-2">
            <div className="flex items-center gap-1.5">
              <span style={{ color: SIGNATURE, fontSize: 12 }}>✦</span>
              <span className="text-[15px] font-semibold text-fg-2">
                Kitchen
              </span>
            </div>
            <button
              onClick={collapse}
              className="p-1 rounded text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
              title="Collapse"
            >
              <ChevronsLeft className="h-3.5 w-3.5" />
            </button>
          </div>
          {treeContent}
        </aside>
      )}

      {/* ── Peeking: floating panel with top offset (like production tree) ── */}
      <AnimatePresence>
        {peeking && !pinned && (
          <>
            <motion.div
              key="backdrop"
              className="fixed inset-0 z-30"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: FAST }}
              style={{ background: "rgba(0,0,0,0.08)" }}
              onClick={() => setPeeking(false)}
            />
            <motion.aside
              key="peek-panel"
              className="fixed left-0 bottom-0 z-40 flex flex-col"
              initial={{ x: -SIDEBAR_W }}
              animate={{ x: 0 }}
              exit={{ x: -SIDEBAR_W }}
              transition={{ duration: FAST, ease: [0.16, 1, 0.3, 1] }}
              onMouseEnter={onSidebarEnter}
              onMouseLeave={onSidebarLeave}
              style={{
                top: 48,
                width: SIDEBAR_W,
                background: "var(--card)",
                borderRight: "1px solid var(--border)",
                borderTopRightRadius: 12,
                boxShadow:
                  "2px 0 16px rgba(0,0,0,0.04), 8px 0 40px rgba(0,0,0,0.02)",
              }}
            >
              <div className="flex items-center justify-between px-4 pt-3 pb-2">
                <span className="text-[15px] font-semibold text-fg-2">
                  Kitchen
                </span>
                <button
                  onClick={pin}
                  className="p-1 rounded text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
                  title="Lock open"
                >
                  <ChevronsRight className="h-3.5 w-3.5" />
                </button>
              </div>
              {treeContent}
            </motion.aside>
          </>
        )}
      </AnimatePresence>

      {/* ── Main Content — animates when sidebar pins/unpins ── */}
      <motion.main
        className={`flex-1 min-w-0 px-6 pt-12 pb-12 max-w-215 ${!pinned ? "mx-auto" : ""}`}
        layout
        transition={{ duration: FAST, ease: [0.16, 1, 0.3, 1] }}
      >
        {/* ── Foundations (tokens + rules — read first) ── */}
        <DesignPrinciplesSection />
        <BrandingSection />
        <ColorTokensSection />
        <TypographySection />
        <SurfacesSection />
        <LayoutSection />
        <MotionTokensSection />
        <AccessibilitySection />
        <PaletteExplorerSection />

        {/* ── Components (reusable pieces) ── */}
        <ControlsSection />
        <VisualVocabSection />
        <StateMachineSection />
        <LoadingStatesSection />
        <EmptyStatesSection />
        <DataSectionCardSection />
        <AsyncActionsSection />

        {/* ── Patterns (composition + product surfaces) ── */}
        <RecipesSection />
        <MemoryRowVariantsSection />
        <TopicsSection />
        <MemoryCardsSection />
        <ForgetSection />
        <BatchOperationsSection />
        <LoadingTextSection />
        <BarNorthStarAltSection />
        <DevDebuggerSection />
        <TreePanelSection />
        <HubExperienceSection />
        <AgentConfigsSection />
        <DreamExperienceSection />
        <TopicRedesignSection />
        <TopicIconsSection />
        <ShellV2Section />
        <BorderlessRedesignSection />
        <LifecycleFlowSection />
        <InboxSection />
        <MemoryMetadataSection />
        <LandingAndOnboardingSection />
        <OAuthConsentSection />
        <MobileDockSection />

        {/* ── Archive ── */}
        <ExplorationsSection />
        <CoverageGapsSection />

        {/* Footer */}
        <div className="mt-16 pt-8 border-t border-border/20 pb-12">
          <p className="text-[12px] text-fg-4">
            memax design system · Foundations → Components → Patterns ·
            docs/design/memax-design-system.md
          </p>
        </div>
      </motion.main>
    </div>
  );
}

// ─── Responsive Shell ───

function KitchenContent() {
  // Detect lg breakpoint (1024px) — matches Tailwind's `lg:`
  const [isDesktop, setIsDesktop] = useState(true);

  useEffect(() => {
    const mq = window.matchMedia("(min-width: 1024px)");
    setIsDesktop(mq.matches);
    const handler = (e: MediaQueryListEvent) => setIsDesktop(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  return isDesktop ? <DesktopKitchen /> : <MobileKitchen />;
}

export default function KitchenPage() {
  return (
    <KitchenProvider>
      <KitchenContent />
    </KitchenProvider>
  );
}
