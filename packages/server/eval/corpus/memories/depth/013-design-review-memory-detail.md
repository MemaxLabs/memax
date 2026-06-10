Design review notes: memory detail page UX. Meeting held April 10, 2026. Attended by Sarah (presenting), Ziyang, Jiahao, and Mira.

## Context

The memory detail page (`/m/[id]`) shows a single memory's full content with metadata. The current implementation is a basic rendered markdown view with a metadata sidebar. Sarah proposed a redesign to make the page more useful and aligned with the Memax design language.

## Feedback and Decisions

### 1. Content Display

Sarah presented two layouts: full-width content with floating metadata (A) and split-pane with content left and metadata right (B).

**Decision:** Layout A (full-width content). The metadata panel slides in from the right when the user clicks an info icon or hovers over the right edge. On mobile, metadata is shown below the content. Rationale: memory content should be the hero — metadata is reference information, not primary reading.

### 2. Metadata Display

Current metadata is a flat key-value list. Sarah proposed grouping into sections:
- **Origin:** source, source agent, pushed by, created date
- **Classification:** kind, stability, tags, topics
- **Activity:** access count, last accessed, event dates
- **Location:** hub, project context

**Decision:** Adopt the grouped metadata layout. Each group is collapsible. "Origin" is expanded by default; others are collapsed. Tags and topics are clickable chips that navigate to filtered search.

### 3. Topic Sidebar

Mira suggested showing related memories in a sidebar panel — memories that share the same topics or have high embedding similarity. Jiahao raised concerns about the extra API call and latency.

**Decision:** Implement "related memories" as a lazy-loaded section below the main content (not a sidebar). Trigger the API call after the main content renders. Show up to 5 related memories with title, snippet, and similarity score. Use the existing `/v1/recall` endpoint with the memory's title as the query, excluding the memory itself.

### 4. Edit and Actions

Current page has no edit capability. Decisions:
- Add an "Edit" button (pencil icon) in the top-right corner for memory owners. Opens an inline Tiptap editor.
- Add a "..." menu with: Copy link, Move to hub, Archive, Delete. Archive is new — soft-delete that hides from search but preserves the memory.
- Add a "Share" button that copies a shareable link. If the memory is in a team hub, the link works for all hub members. If personal, the link returns 403 for non-owners.

### 5. Glass Styling

The content area uses `glass-subtle` background. The floating metadata panel uses `glass-strong` with `shadow-glow`. The edit button uses `shadow-premium` on hover. Entrance animation: `animate-fade-up` with `stagger-1` on content, `stagger-2` on metadata.

## Timeline

Sarah owns implementation. Target: ship to staging by April 16, production by April 18.
