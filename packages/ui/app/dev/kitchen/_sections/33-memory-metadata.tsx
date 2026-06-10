// Kitchen 33 — Memory Metadata Redesign
// Maps to: topic-pills.tsx, destination-picker.tsx
// Production files: memories/[id]/page.tsx, batch-toolbar.tsx
//
// === LLM AGENT RULES — MEMORY METADATA ===
//
// TOPIC LOCATION:
//   - Memory has 1:1 topic relationship (not many-to-many).
//   - "Move" semantics, never "add". Same picker as batch toolbar.
//   - Always visible on detail page, even when unassigned.
//   - Single authoritative user-move contract: memories.batchMove (→ store
//     BatchMoveMemories, DELETE + INSERT on memory_topics in a transaction).
//     Picker, batch toolbar, detail route, drag-and-drop, and CLI topic set
//     all go through this one path via useMemoryMove (useMutation-based).
//   - topics.assignMemory → store AssignMemoryToTopic is the AUTO-ASSIGNMENT
//     path (ingest + dreams workers) and is confidence-gated by design. Not
//     used by any user-facing UI.
//
// DESTINATION PICKER (shared):
//   - Single reusable component for both batch and single-memory moves.
//   - Shows topics in current hub with tree indentation.
//   - Other hubs are drill-down items (click → see hub's topics, back nav).
//   - Fixed list height for stable drill animation + scroll.
//   - Used in dropdowns AND mobile sheet (same drill list).
//   - Min-h-11 (44px) touch targets, WCAG 2.5.5.
//   - Parent controls positioning via className/style.
//
// CLASSIFICATION COPY:
//   - Memory kind/stability are machine-facing retrieval axes.
//   - UI uses plain-language copy, topics, tags, and pins for corrections.
//   - Kinds drive retrieval scoring + stability decay, not hard filtering.
//
// METADATA SECTION LAYOUT:
//   - Topic location sits under provenance strip (WHERE).
//   - Classification section sits after content (WHAT).
//   - Order: classification sentence first, then tags (primary → secondary).
//   - Tags are inline pills with inline add input.
// =============================================
"use client";

import { useState } from "react";
import { Section, DemoCard } from "../_shared";
import { Pill } from "@/components/pill";
import {
  ChevronRight,
  ChevronLeft,
  FolderInput,
  ArrowRight,
  Check,
  X,
  Users,
  Terminal,
  Globe,
  FileText,
  BookOpen,
  FolderOpen,
  Settings,
} from "lucide-react";

/* ── Mock data ── */

interface MockTopic {
  id: string;
  name: string;
  icon: string;
  children: MockTopic[];
}

const MOCK_HUB_TOPICS: {
  id: string;
  name: string;
  topics: MockTopic[];
}[] = [
  {
    id: "personal",
    name: "Your Topics",
    topics: [
      { id: "t1", name: "Quick Notes", icon: "file-text", children: [] },
      {
        id: "t2",
        name: "Architecture",
        icon: "book-open",
        children: [
          {
            id: "t2a",
            name: "Frontend",
            icon: "file-text",
            children: [],
          },
          {
            id: "t2b",
            name: "Backend",
            icon: "settings",
            children: [
              {
                id: "t2b1",
                name: "API Design",
                icon: "file-text",
                children: [],
              },
              {
                id: "t2b2",
                name: "Database",
                icon: "settings",
                children: [],
              },
            ],
          },
        ],
      },
      { id: "t3", name: "Saved Links", icon: "globe", children: [] },
      {
        id: "t4",
        name: "Scratch Entries",
        icon: "folder-open",
        children: [],
      },
    ],
  },
  {
    id: "team",
    name: "Team Engineering",
    topics: [
      { id: "t5", name: "Sprint Notes", icon: "file-text", children: [] },
      {
        id: "t6",
        name: "Design System",
        icon: "book-open",
        children: [],
      },
    ],
  },
];

const TOPIC_ICONS: Record<string, React.ElementType> = {
  "file-text": FileText,
  globe: Globe,
  "folder-open": FolderOpen,
  settings: Settings,
  "book-open": BookOpen,
};

/* ── Helper: Topic Icon ── */

function TopicIconDemo({ name }: { name: string }) {
  const Icon = TOPIC_ICONS[name] ?? FileText;
  return <Icon className="h-4 w-4 text-fg-3" />;
}

/* ── Demo: Topic Location (assigned) ── */

function TopicLocationAssigned() {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex items-center gap-1.5">
      <button
        onClick={() => setOpen(!open)}
        className="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 border border-border bg-foreground/[0.03] text-[14px] text-fg-2 hover:bg-foreground/[0.06] cursor-pointer transition-colors"
      >
        <FolderOpen className="h-3.5 w-3.5 shrink-0 text-fg-3" />
        <span className="truncate max-w-[160px]">Scratch Entries</span>
        <ChevronRight className="h-3 w-3 text-fg-4 shrink-0" />
      </button>
      <button className="text-fg-4 hover:text-fg-2 cursor-pointer transition-colors p-0.5 rounded">
        <X className="h-3 w-3" />
      </button>
      {open && (
        <div className="text-[12px] text-fg-3 ml-2">
          → opens DestinationPicker
        </div>
      )}
    </div>
  );
}

/* ── Demo: Topic Location (unassigned) ── */

function TopicLocationUnassigned() {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex items-center gap-1.5">
      <button
        onClick={() => setOpen(!open)}
        className="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 border border-dashed border-border text-[14px] text-fg-3 hover:text-fg-2 hover:border-foreground/20 cursor-pointer transition-colors"
      >
        <FolderInput className="h-3.5 w-3.5 shrink-0" />
        <span>Move to topic</span>
      </button>
      {open && (
        <div className="text-[12px] text-fg-3 ml-2">
          → opens DestinationPicker
        </div>
      )}
    </div>
  );
}

/* ── Demo: Destination Picker — Hub → Topic N-level drill-down ── */

interface DrillLevel {
  label: string;
  items: MockTopic[];
}

function DestinationPickerDemo() {
  // Starts at current hub's topics (not hub list). Multi-hub users
  // can back-navigate to hub list.
  const [stack, setStack] = useState<DrillLevel[]>([
    { label: MOCK_HUB_TOPICS[0].name, items: MOCK_HUB_TOPICS[0].topics },
  ]);
  const [selected, setSelected] = useState<string | null>(null);
  const [animDir, setAnimDir] = useState<"none" | "forward" | "back">("none");

  const isAtHubs = stack.length === 0;
  const current = stack[stack.length - 1];
  const multiHub = MOCK_HUB_TOPICS.length > 1;

  function drillIntoHub(hub: (typeof MOCK_HUB_TOPICS)[0]) {
    setAnimDir("forward");
    setStack([{ label: hub.name, items: hub.topics }]);
  }

  function drillIntoTopic(topic: MockTopic) {
    if (topic.children.length > 0) {
      setAnimDir("forward");
      setStack([...stack, { label: topic.name, items: topic.children }]);
    } else {
      setSelected(topic.id);
    }
  }

  function goBack() {
    setAnimDir("back");
    setStack(stack.slice(0, -1));
  }

  // Animation key — forces re-render for CSS animation
  const animKey = stack.map((s) => s.label).join("/") || "hubs";

  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
        boxShadow:
          "0 8px 32px oklch(0 0 0 / 0.08), 0 2px 8px oklch(0 0 0 / 0.04)",
        backdropFilter: "blur(16px)",
        WebkitBackdropFilter: "blur(16px)",
        minWidth: 240,
        maxWidth: 300,
      }}
    >
      {/* Back button — deeper levels always, hub level only if multi-hub */}
      {(stack.length > 1 || (multiHub && stack.length === 1)) && (
        <button
          onClick={goBack}
          className="w-full flex items-center gap-2 min-h-11 px-3 text-[13px] text-fg-2 hover:bg-surface-1 cursor-pointer transition-colors border-b border-border/30"
        >
          <ChevronLeft className="h-3.5 w-3.5 text-fg-3" />
          <span className="font-medium truncate">{current.label}</span>
        </button>
      )}

      {/* Content — same animate-drill-* as production */}
      <div className="overflow-hidden">
        <div
          key={animKey}
          className={`max-h-64 overflow-y-auto p-1 ${
            animDir === "forward"
              ? "animate-drill-forward"
              : animDir === "back"
                ? "animate-drill-back"
                : ""
          }`}
        >
          {/* Hub list */}
          {isAtHubs &&
            MOCK_HUB_TOPICS.map((hub) => (
              <button
                key={hub.id}
                onClick={() => drillIntoHub(hub)}
                className="w-full flex items-center gap-2.5 min-h-11 px-3 rounded-xl text-[13px] text-fg-2 hover:bg-foreground/[0.04] cursor-pointer transition-colors"
              >
                <Users className="h-4 w-4 text-fg-3" />
                <span className="flex-1 text-left font-medium truncate">
                  {hub.name}
                </span>
                <ArrowRight className="h-3 w-3 text-fg-4 shrink-0" />
              </button>
            ))}

          {/* Topic list at current depth */}
          {!isAtHubs &&
            current.items.map((topic) => {
              const hasChildren = topic.children.length > 0;
              const isSelected = topic.id === selected;
              return (
                <button
                  key={topic.id}
                  onClick={() => drillIntoTopic(topic)}
                  className={`w-full flex items-center gap-2.5 min-h-11 px-3 rounded-xl text-[13px] cursor-pointer transition-colors ${
                    isSelected
                      ? "bg-foreground/[0.06] text-fg-1"
                      : "text-fg-2 hover:bg-foreground/[0.04]"
                  }`}
                >
                  <TopicIconDemo name={topic.icon} />
                  <span className="flex-1 text-left truncate">
                    {topic.name}
                  </span>
                  {hasChildren ? (
                    <ArrowRight className="h-3 w-3 text-fg-4 shrink-0" />
                  ) : isSelected ? (
                    <Check className="h-3.5 w-3.5 text-primary shrink-0" />
                  ) : null}
                </button>
              );
            })}
        </div>
      </div>
    </div>
  );
}

/* ── Demo: Full Metadata Section ── */

function ClassificationSentence({ text }: { text: string }) {
  return <div className="text-[13px] text-fg-2 mb-3 pl-4">{text}</div>;
}

function MetadataSectionDemo() {
  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{ background: "var(--background)" }}
    >
      <div className="px-6 sm:px-8 pt-6 pb-8 max-w-3xl">
        {/* Provenance strip (simplified) */}
        <div className="flex items-center flex-wrap gap-x-1.5 gap-y-0.5 text-[13px] mb-2">
          <Terminal
            className="h-3.5 w-3.5 shrink-0"
            style={{ color: "oklch(0.65 0.15 30)" }}
          />
          <span className="text-fg-2 font-medium">Claude Code</span>
          <span className="text-fg-3">captured</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">memax</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">2d ago</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-2 font-medium tabular-nums">
            recalled 47x
          </span>
        </div>

        {/* Topic location — always visible */}
        <div className="mb-6">
          <TopicLocationAssigned />
        </div>

        {/* Content placeholder */}
        <div className="h-16 rounded-lg bg-surface-1 mb-6 flex items-center justify-center text-[13px] text-fg-3">
          [ content area ]
        </div>

        {/* Classification section */}
        <div className="pt-5 border-t border-border/20">
          <div className="flex items-center gap-1.5 mb-3">
            <span className="text-[12px]" style={{ color: "var(--signature)" }}>
              ✦
            </span>
            <span className="text-[13px] text-fg-3">
              memax classified this as
            </span>
          </div>
          <ClassificationSentence text="durable reference" />
          {/* Tags (deletable + addable) */}
          <div className="flex flex-wrap gap-1.5 pl-4">
            <Pill variant="remove" onRemove={() => {}}>
              react
            </Pill>
            <Pill variant="remove" onRemove={() => {}}>
              architecture
            </Pill>
            <Pill variant="remove" onRemove={() => {}}>
              server-components
            </Pill>
            <input
              className="text-[13px] bg-transparent outline-none w-16 text-fg-3 placeholder:text-fg-4"
              placeholder="+ tag"
            />
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── Demo: Unassigned memory metadata ── */

function MetadataUnassignedDemo() {
  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{ background: "var(--background)" }}
    >
      <div className="px-6 sm:px-8 pt-6 pb-8 max-w-3xl">
        {/* Provenance strip — shared memory shows "shared" badge */}
        <div className="flex items-center flex-wrap gap-x-1.5 gap-y-0.5 text-[13px] mb-2">
          <Globe className="h-3.5 w-3.5 shrink-0 text-fg-3" />
          <span className="text-fg-3">you</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">1d ago</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3 tabular-nums">recalled 3x</span>
        </div>

        {/* Topic location — unassigned state */}
        <div className="mb-6">
          <TopicLocationUnassigned />
        </div>

        {/* Content placeholder */}
        <div className="h-16 rounded-lg bg-surface-1 mb-6 flex items-center justify-center text-[13px] text-fg-3">
          [ content area ]
        </div>

        {/* Classification section */}
        <div className="pt-5 border-t border-border/20">
          <div className="flex items-center gap-1.5 mb-3">
            <span className="text-[12px]" style={{ color: "var(--signature)" }}>
              ✦
            </span>
            <span className="text-[13px] text-fg-3">
              memax classified this as
            </span>
          </div>
          <ClassificationSentence text="recent activity" />
          <div className="flex flex-wrap gap-1.5 pl-4">
            <Pill variant="remove" onRemove={() => {}}>
              unclear-content
            </Pill>
            <Pill variant="remove" onRemove={() => {}}>
              needs-clarification
            </Pill>
            <input
              className="text-[13px] bg-transparent outline-none w-16 text-fg-3 placeholder:text-fg-4"
              placeholder="+ tag"
            />
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── Demo: Unified Drill-Down Tree ── */

/**
 * Shared drill-down tree used by BOTH tree panel (browse) and move picker (select).
 * One component, two modes. Container varies (bottom sheet, popover, pinned sidebar).
 *
 * mode="browse": tap navigates to topic page, shows memory count
 * mode="select": tap picks destination, shows checkmark on selection
 */

function DrillDownTreeDemo({ mode }: { mode: "browse" | "select" }) {
  const [stack, setStack] = useState<{ label: string; items: MockTopic[] }[]>([
    { label: MOCK_HUB_TOPICS[0].name, items: MOCK_HUB_TOPICS[0].topics },
  ]);
  const [selected, setSelected] = useState<string | null>(null);
  const [animDir, setAnimDir] = useState<"none" | "forward" | "back">("none");

  const isAtHubs = stack.length === 0;
  const current = stack[stack.length - 1];
  const multiHub = MOCK_HUB_TOPICS.length > 1;

  function drillIntoHub(hub: (typeof MOCK_HUB_TOPICS)[0]) {
    setAnimDir("forward");
    setStack([{ label: hub.name, items: hub.topics }]);
  }

  function handleTopic(topic: MockTopic) {
    if (mode === "select") {
      // Select mode: always select, even parents — user can pick top-level
      setSelected(topic.id);
    } else if (topic.children.length > 0) {
      // Browse mode: drill into children
      setAnimDir("forward");
      setStack((s) => [...s, { label: topic.name, items: topic.children }]);
    }
    // browse leaf: would navigate to /memories/topics/{id}
  }

  function drillIntoTopic(topic: MockTopic) {
    setAnimDir("forward");
    setStack((s) => [...s, { label: topic.name, items: topic.children }]);
  }

  function goBack() {
    setAnimDir("back");
    setStack((s) => s.slice(0, -1));
  }

  const canGoBack = stack.length > 1 || (multiHub && stack.length === 1);
  const animKey = stack.map((s) => s.label).join("/") || "hubs";

  // Mock memory counts
  const memoryCount: Record<string, number> = {
    t1: 12,
    t2: 47,
    t2a: 18,
    t2b: 29,
    t2b1: 15,
    t2b2: 14,
    t3: 8,
    t4: 10,
    t5: 23,
    t6: 5,
  };

  return (
    <div>
      {/* Back button */}
      {canGoBack && (
        <button
          onClick={goBack}
          className="w-full flex items-center gap-2 min-h-11 px-4 text-[14px] text-fg-2 hover:bg-surface-1 cursor-pointer transition-colors border-b border-border/30"
        >
          <ChevronLeft className="h-3.5 w-3.5 text-fg-3" />
          <span className="font-medium truncate">{current.label}</span>
        </button>
      )}

      {/* List — same animate-drill-* as production */}
      <div
        key={animKey}
        className={`overflow-y-auto p-1.5 ${
          animDir === "forward"
            ? "animate-drill-forward"
            : animDir === "back"
              ? "animate-drill-back"
              : ""
        }`}
      >
        {/* Hub list */}
        {isAtHubs &&
          MOCK_HUB_TOPICS.map((hub) => (
            <button
              key={hub.id}
              onClick={() => drillIntoHub(hub)}
              className="w-full flex items-center gap-2.5 min-h-11 px-3 rounded-xl text-[14px] text-fg-2 hover:bg-foreground/[0.04] cursor-pointer transition-colors"
            >
              <Users className="h-4 w-4 text-fg-3" />
              <span className="flex-1 text-left font-medium truncate">
                {hub.name}
              </span>
              <span className="text-[13px] text-fg-4 tabular-nums">
                {hub.topics.length}
              </span>
              <ArrowRight className="h-3 w-3 text-fg-4 shrink-0" />
            </button>
          ))}

        {/* Topics */}
        {!isAtHubs &&
          current.items.map((topic) => {
            const hasChildren = topic.children.length > 0;
            const isSelected = selected === topic.id;
            return (
              <div key={topic.id} className="flex items-center">
                <button
                  onClick={() => handleTopic(topic)}
                  className={`flex-1 flex items-center gap-2.5 min-h-11 px-3 rounded-xl text-[14px] cursor-pointer transition-colors ${
                    isSelected
                      ? "bg-foreground/[0.06] text-fg-1"
                      : "text-fg-2 hover:bg-foreground/[0.04]"
                  }`}
                >
                  <TopicIconDemo name={topic.icon} />
                  <span className="flex-1 text-left truncate">
                    {topic.name}
                  </span>

                  {/* Right side: check (selected) or count (browse) */}
                  {mode === "select" && isSelected ? (
                    <Check className="h-3.5 w-3.5 text-primary shrink-0" />
                  ) : mode === "browse" ? (
                    <>
                      <span className="text-[13px] text-fg-4 tabular-nums">
                        {memoryCount[topic.id] ?? 0}
                      </span>
                      {hasChildren && (
                        <ArrowRight className="h-3 w-3 text-fg-4 shrink-0" />
                      )}
                    </>
                  ) : null}
                </button>
                {/* Drill arrow — separate target in select mode */}
                {mode === "select" && hasChildren && (
                  <button
                    onClick={() => drillIntoTopic(topic)}
                    className="min-h-11 px-2.5 flex items-center cursor-pointer text-fg-4 hover:text-fg-2 transition-colors rounded-lg hover:bg-foreground/[0.04]"
                  >
                    <ArrowRight className="h-3 w-3" />
                  </button>
                )}
              </div>
            );
          })}
      </div>
    </div>
  );
}

/** Mobile bottom sheet container — wraps any content in the sheet UX. */
function MobileSheetMock({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className="rounded-t-2xl overflow-hidden flex flex-col"
      style={{
        background: "var(--card)",
        borderTop: "1px solid var(--border)",
        boxShadow: "0 -2px 16px rgba(0,0,0,0.06), 0 -8px 40px rgba(0,0,0,0.04)",
        maxHeight: 380,
        width: 320,
      }}
    >
      {/* Sheet handle + title */}
      <div className="flex flex-col items-center pt-2 pb-1">
        <div className="w-8 h-1 rounded-full bg-surface-3 mb-2" />
        <div className="flex items-center justify-between w-full px-4">
          <span className="text-[15px] font-semibold text-fg-2">{title}</span>
          <button className="min-h-11 px-2 rounded text-fg-3 hover:text-fg-2 transition-colors cursor-pointer">
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>
      {children}
    </div>
  );
}

/* ── Main Export ── */

export function MemoryMetadataSection() {
  return (
    <Section
      title="33. Memory Metadata"
      description="Topic location, classification framing, tags, and move flows. North star for memory detail page metadata."
    >
      {/* Topic Location states */}
      <DemoCard label="33a. Topic Location — Assigned vs Unassigned">
        <div className="space-y-4">
          <div>
            <p className="text-[12px] text-fg-3 mb-2">
              Assigned — shows topic name, click to move
            </p>
            <TopicLocationAssigned />
          </div>
          <div>
            <p className="text-[12px] text-fg-3 mb-2">
              Unassigned — dashed border, invite to assign
            </p>
            <TopicLocationUnassigned />
          </div>
        </div>
      </DemoCard>

      {/* Destination Picker */}
      <DemoCard label="33b. Destination Picker — Hub → Topic N-level drill-down">
        <p className="text-[12px] text-fg-3 mb-3">
          Starts from hubs. Click a hub → see its topics. Topics with children
          drill deeper. Topic rows are the confirm target; hub rows are browse
          only in the single-memory move flow. Same slide pattern as 33c. Try:
          Your Topics → Architecture → Backend.
        </p>
        <DestinationPickerDemo />
      </DemoCard>

      {/* Full metadata layout */}
      <DemoCard label="33c. Full Metadata Layout — Assigned Memory">
        <p className="text-[12px] text-fg-3 mb-3">
          Classification framed as AI output: &quot;✦ memax classified this
          as&quot;. Plain-language sentence only. Boundary (private) hidden —
          only shown when shared.
        </p>
        <MetadataSectionDemo />
      </DemoCard>

      <DemoCard label="33d. Full Metadata Layout — Unassigned Memory">
        <p className="text-[12px] text-fg-3 mb-3">
          Topic location always visible (dashed CTA). Same &quot;memax
          classified&quot; framing. No &quot;private&quot; noise.
        </p>
        <MetadataUnassignedDemo />
      </DemoCard>

      {/* Unified drill-down tree */}
      <DemoCard label="33e. Unified Drill-Down Tree — Browse Mode (Mobile)">
        <p className="text-[12px] text-fg-3 mb-3">
          Mobile tree panel as bottom sheet. Same drill-down as move picker. Tap
          a parent → drill into children. Shows memory counts. Try: Your Topics
          → Architecture → Backend.
        </p>
        <MobileSheetMock title="Topics">
          <DrillDownTreeDemo mode="browse" />
        </MobileSheetMock>
      </DemoCard>

      <DemoCard label="33f. Unified Drill-Down Tree — Select Mode (Move)">
        <p className="text-[12px] text-fg-3 mb-3">
          Same component, mode=&quot;select&quot;. Hub rows drill into that
          hub&apos;s topics. Topic rows confirm the destination (checkmark).
          Same bottom sheet container. Identical navigation, different action on
          tap.
        </p>
        <MobileSheetMock title="Move to topic">
          <DrillDownTreeDemo mode="select" />
        </MobileSheetMock>
      </DemoCard>

      {/* Architecture docs */}
      <DemoCard label="33g. Architecture">
        <div className="text-[13px] text-fg-2 space-y-3 leading-relaxed">
          <p>
            <strong>1:1 Topic Relationship:</strong> Each memory belongs to
            exactly one topic.{" "}
            <code className="font-mono text-fg-1">memories.batchMove</code> is
            the authoritative user-move contract — atomic DELETE + INSERT on
            memory_topics, not confidence-gated. All user surfaces (picker,
            batch toolbar, detail route, drag-and-drop, CLI) route through it
            via <code className="font-mono text-fg-1">useMemoryMove</code> on
            React Query <code className="font-mono text-fg-1">useMutation</code>
            .
          </p>
          <p>
            <strong>AssignMemoryToTopic:</strong> confidence-gated
            auto-assignment used only by ingest + dreams workers. Replaying at
            equal confidence is a no-op by design so earlier user intent sticks.
          </p>
          <p>
            <strong>Unified DrillDownTree:</strong> One component, two modes.
            mode=&quot;browse&quot;: tap navigates to topic page (tree panel).
            mode=&quot;select&quot;: tap picks destination (move picker).
            Container varies: bottom sheet (mobile), popover (desktop move),
            pinned sidebar (desktop browse uses expand/collapse instead).
            Drill-down everywhere except pinned desktop sidebar.
          </p>
          <p>
            <strong>&quot;memax classified&quot; framing:</strong> Keep the
            label, but the body is a plain-language sentence rather than a
            taxonomy control. Tags remain editable because they are directly
            useful to the user.
          </p>
          <p>
            <strong>Boundary (private/shared):</strong> Hidden when private (the
            default ~95% case). Only shown as a badge when shared. Moved to
            provenance strip context where it semantically belongs.
          </p>
        </div>
      </DemoCard>
    </Section>
  );
}
