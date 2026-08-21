"use client";

import {
  createContext,
  useContext,
  useEffect,
  useCallback,
  useMemo,
  useState,
  useRef,
  type ReactNode,
} from "react";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";
import { ChevronsLeft, X } from "lucide-react";
import { useRouter, usePathname } from "next/navigation";
import { buildTopicPath, getHubSlugForPath } from "@/lib/route-helpers";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useTopicTreeController } from "@/hooks/use-topic-tree-controller";
import { useLocale } from "@/i18n";
import { TopicTreeContent } from "./topic-tree-content";
import { DestinationPicker } from "../destination-picker";
import { BrandMark } from "../brand-mark";
import {
  GLASS_ENTER_DURATION,
  GLASS_ENTER_DURATION_MOBILE,
  GLASS_EXIT_DURATION,
  GLASS_BACKDROP_DURATION,
  GLASS_ENTER_EASE,
  GLASS_EXIT_EASE,
} from "@memaxlabs/ui/tokens/motion";

const PINNED_STORAGE_KEY = "memax_tree_pinned";

function getStoredPinned(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return localStorage.getItem(PINNED_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

// --- Context ---

interface TreePanelContextValue {
  /** Mobile-only: whether the centered glass modal is open. Desktop ignores this. */
  isOpen: boolean;
  toggle: () => void;
  /**
   * Desktop-only: user's explicit toggle preference for the tree panel.
   * Persisted to localStorage under `memax_tree_pinned` (legacy key name,
   * kept for back-compat — the concept is "is the desktop tree open"
   * rather than "is it pinned vs peeking"). Flipped by the BrandMark
   * and by the inline close button on the floating glass panel.
   */
  isPinned: boolean;
  pin: () => void;
  unpin: () => void;
  /**
   * Transient "drag session" auto-open — set TRUE by TopicDndProvider when
   * a drag starts while `isPinned` is false, cleared on drop/cancel. The
   * flag is lifecycle-owned by the drag provider, not by user preference,
   * and never written to localStorage. Combined with `isPinned` via
   * `dropTargetsActive = isPinned || isDragSessionOpen` in the drag
   * provider, so transient drag reveals enable drop targets without
   * mutating the user's real preference.
   */
  isDragSessionOpen: boolean;
  beginDragSession: () => void;
  endDragSession: () => void;
  /** Close mobile overlay. Does NOT touch the drag flag. */
  closeOverlay: () => void;
}

const TreePanelContext = createContext<TreePanelContextValue>({
  isOpen: false,
  toggle: () => {},
  isPinned: false,
  pin: () => {},
  unpin: () => {},
  isDragSessionOpen: false,
  beginDragSession: () => {},
  endDragSession: () => {},
  closeOverlay: () => {},
});

export function useTreePanel() {
  return useContext(TreePanelContext);
}

/**
 * TopicTreePanelProvider — manages tree panel state.
 *
 * Desktop model: a BrandMark (rendered by the app shell) opens
 * the tree as a floating glass panel. `isPinned` is the user's
 * persistent "tree open" preference. `isDragSessionOpen` is a transient
 * auto-open that fires for the duration of any memory/topic drag so
 * drop targets are reachable without the user first toggling the tree
 * open.
 *
 * Mobile model: `toggle()` opens a centered glass modal via `isOpen`.
 * Desktop ignores `isOpen`; mobile ignores `isPinned` and
 * `isDragSessionOpen`.
 *
 * Hover-peek was removed in favor of the explicit toggle. There is no
 * `isPeeking` state — the panel is either user-open (`isPinned`) or
 * transiently drag-open (`isDragSessionOpen`), and nothing else.
 */
export function TopicTreePanelProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [isPinned, setIsPinned] = useState(false);
  const [isDragSessionOpen, setIsDragSessionOpen] = useState(false);

  // Hydrate pinned state from localStorage
  useEffect(() => {
    setIsPinned(getStoredPinned());
  }, []);

  const toggle = useCallback(() => {
    setIsOpen((prev) => !prev);
  }, []);

  const pin = useCallback(() => {
    setIsPinned(true);
    setIsOpen(false);
    localStorage.setItem(PINNED_STORAGE_KEY, "true");
  }, []);

  const unpin = useCallback(() => {
    setIsPinned(false);
    localStorage.setItem(PINNED_STORAGE_KEY, "false");
  }, []);

  const beginDragSession = useCallback(() => {
    setIsDragSessionOpen(true);
  }, []);

  const endDragSession = useCallback(() => {
    setIsDragSessionOpen(false);
  }, []);

  const closeOverlay = useCallback(() => {
    setIsOpen(false);
  }, []);

  // Escape closes the mobile modal. Desktop tree close happens via the
  // BrandMark or the inline close button, not Escape — during
  // an active drag, Escape belongs to dnd-kit's onDragCancel (which
  // also calls endDragSession to clear transient open state).
  useEffect(() => {
    if (!isOpen) return;
    function handleEsc(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setIsOpen(false);
      }
    }
    window.addEventListener("keydown", handleEsc);
    return () => window.removeEventListener("keydown", handleEsc);
  }, [isOpen]);

  return (
    <TreePanelContext.Provider
      value={{
        isOpen,
        toggle,
        isPinned,
        pin,
        unpin,
        isDragSessionOpen,
        beginDragSession,
        endDragSession,
        closeOverlay,
      }}
    >
      {children}
    </TreePanelContext.Provider>
  );
}

/**
 * TopicTreePanelOverlayHost — mobile-only host for the centered glass
 * tree modal.
 *
 * Desktop renders the tree from SidebarSlot, which keeps the tree
 * subtree mounted at all times so drop targets register with dnd-kit
 * from page load. There is no desktop overlay path.
 *
 * Mobile gets a centered glass modal because a collapsible left panel
 * is wrong UX for phones. The host is mounted
 * inside TopicDndProvider so the mobile modal's tree content registers
 * with the nearest DndContext — matches the desktop SidebarSlot
 * contract. Mobile has no drag surfaces today (the inner content is
 * DestinationPicker browse mode), so this is a forward-compat hook
 * rather than a requirement.
 */
export function TopicTreePanelOverlayHost() {
  const { isOpen, closeOverlay } = useTreePanel();
  const isMobile = useIsMobile();

  if (!isMobile) return null;

  return (
    <AnimatePresence>
      {isOpen && <TopicTreePanelOverlay onClose={closeOverlay} />}
    </AnimatePresence>
  );
}

/**
 * TopicTreePanelOverlay — mobile-only wrapper that applies a scroll
 * lock and defers render until hydration, then mounts MobileTreeModal.
 *
 * Hydration guard: useIsMobile() starts false on SSR. Waiting for the
 * client mount before rendering prevents a flash where the server
 * pre-renders nothing, hydrates, and then flips isMobile to true (or
 * stays false on desktop).
 *
 * Body scroll lock: when the modal is open, document.body gets
 * position:fixed + overflow:hidden to prevent background scroll from
 * interfering with modal content scroll. The
 * cleanup restores the scroll position on unmount, so resizing the
 * viewport from mobile to desktop while the modal is open (which
 * unmounts the host synchronously) returns body styles to baseline
 * before the desktop path takes over.
 *
 * Desktop has no overlay — SidebarSlot is the sole desktop tree surface
 * and the tree subtree there is always mounted.
 */
function TopicTreePanelOverlay({ onClose }: { onClose: () => void }) {
  const { t } = useLocale();
  const [hasMounted, setHasMounted] = useState(false);

  useEffect(() => setHasMounted(true), []);

  useEffect(() => {
    if (!hasMounted) return;
    const scrollY = window.scrollY;
    document.body.style.position = "fixed";
    document.body.style.top = `-${scrollY}px`;
    document.body.style.left = "0";
    document.body.style.right = "0";
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.position = "";
      document.body.style.top = "";
      document.body.style.left = "";
      document.body.style.right = "";
      document.body.style.overflow = "";
      window.scrollTo(0, scrollY);
    };
  }, [hasMounted]);

  if (!hasMounted) return null;

  return <MobileTreeModal onClose={onClose} title={t.topics.treeTitle} />;
}

/**
 * MobileTreeModal — centered glass modal for the mobile tree.
 *
 * Shares material + motion family with the desktop PinnedTreeSidebar so
 * the two surfaces read as the same UI element at different sizes. No
 * grip bar, no bottom anchoring — the modal is a floating glass card
 * centred between top and bottom safe-area insets.
 *
 * Content is DestinationPicker in browse mode: drill-down one-level-at-
 * a-time navigation that is friendlier on touch than the desktop
 * expand/collapse tree. Mobile has no DnD today; this surface is
 * browse-only.
 *
 * Dismiss paths:
 *   - backdrop tap → onClose
 *   - Escape       → handled by TopicTreePanelProvider (scoped to isOpen)
 *   - reduced-motion → opacity-only enter/exit, no transform/blur/scale
 *
 * Motion elements are direct AnimatePresence children (required for
 * exit animations).
 */
function MobileTreeModal({
  onClose,
  title,
}: {
  onClose: () => void;
  title: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  const { t } = useLocale();
  const reduceMotion = useReducedMotion();

  // Per-lifecycle transitions live on each value-object so enter and
  // exit can use different durations + eases. framer-motion merges
  // these with its own prop resolution.
  const enterTransition = {
    duration: GLASS_ENTER_DURATION_MOBILE,
    ease: GLASS_ENTER_EASE,
  };
  const exitTransition = {
    duration: GLASS_EXIT_DURATION,
    ease: GLASS_EXIT_EASE,
  };
  const cardAnimate = reduceMotion
    ? {
        initial: { opacity: 0 },
        animate: { opacity: 1, transition: enterTransition },
        exit: { opacity: 0, transition: exitTransition },
      }
    : {
        initial: { opacity: 0, scale: 0.96, y: -8, filter: "blur(6px)" },
        animate: {
          opacity: 1,
          scale: 1,
          y: 0,
          filter: "blur(0px)",
          transition: enterTransition,
        },
        exit: {
          opacity: 0,
          scale: 0.98,
          y: -4,
          transition: exitTransition,
        },
      };

  return (
    <>
      {/* Non-darkening blurred backdrop. The page behind the modal is
          still visible but pushed through a glass layer so the
          foreground modal reads as floating. Shared `.glass-backdrop`
          utility matches the glass family used by the top-right
          chrome capsule and the hub transition overlay. */}
      <motion.div
        key="tree-modal-backdrop"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        transition={{ duration: GLASS_BACKDROP_DURATION }}
        className="glass-backdrop fixed inset-0 z-topic-tree"
        onClick={onClose}
      />
      <motion.div
        key="tree-modal"
        {...cardAnimate}
        className="glass-panel glass-panel-opaque fixed z-topic-tree flex flex-col backdrop-blur-sm"
        style={{
          insetInline: 16,
          top: "max(72px, calc(56px + var(--safe-top, 0px)))",
          bottom: "calc(88px + var(--safe-bottom, 0px))",
        }}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <div className="flex items-center justify-between px-4 h-13 shrink-0">
          <span className="text-[15px] font-semibold text-fg-1">{title}</span>
          <button
            onClick={onClose}
            className="touch-no-hover flex min-h-11 min-w-11 items-center justify-center rounded-chrome text-fg-3 transition-colors cursor-pointer hover:text-fg-2 hover:bg-foreground/6"
            aria-label={t.topics.treeClose}
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto overscroll-contain">
          <DestinationPicker
            mode="browse"
            variant="plain"
            listHeight="100%"
            onNavigate={(topicId: string) => {
              router.push(buildTopicPath(currentHubSlug, topicId), {
                scroll: false,
              });
              onClose();
            }}
            onClose={onClose}
            className="flex-1"
            style={{ height: "100%" }}
          />
        </div>
      </motion.div>
    </>
  );
}
