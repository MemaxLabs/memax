## Feature Spec: Ask Bar Redesign

**Status:** In progress (design complete, eng starting April 14)
**Owner:** Sarah Kim (PM), Ziyang (eng lead)
**Target ship date:** April 28, 2026

### Problem

The current Ask bar is a plain text input at the top of the hub view. Users report confusion about what it does vs. the capture modal. Recall usage is 0.3x/session vs. our target of 2x/session.

### Requirements

1. **Centered glass modal** -- triggered by `Cmd+K` or clicking the dock icon. Replaces the static top bar.
2. **Unified input** -- auto-detects intent. If the input looks like a question, route to recall. If it looks like a statement/note, route to capture. Show a subtle label ("asking..." or "remembering...") so the user knows which mode is active.
3. **Streaming results** -- recall results should stream in as chunks resolve, not wait for the full pipeline. Target: first result visible within 400ms.
4. **Source attribution** -- each result shows the hub name, push date, and source agent icon (Claude, Cursor, etc.)
5. **Quick actions** -- from any result, user can: copy to clipboard, open full memory, forget, or push to a different hub.

### UX Goals

- Ask bar should feel like Spotlight/Raycast, not a database query tool
- Zero learning curve -- new users should understand it in <5 seconds
- Mobile-friendly -- the modal must work on 375px screens

### Non-goals

- Filters/facets in the ask bar itself (use the hub view for filtered browsing)
- Voice input (future consideration)

### Success Metrics

- Recall usage per session increases from 0.3x to 1.5x within 2 weeks of launch
- NPS for "finding information" improves from 32 to 50+
