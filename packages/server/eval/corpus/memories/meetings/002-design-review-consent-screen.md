Design review: OAuth consent screen UX. March 22, 2026. Led by Sarah, attended by Ziyang and Jiahao.

## Context
The OAuth consent screen is shown when third-party agents (Cursor, Windsurf, etc.) request access to a user's Memax memories via the MCP OAuth flow. The previous design was a plain HTML form that didn't match the Memax design language.

## Feedback

Sarah presented three mockups (A, B, C). Key discussion points:

1. **Scope display** — Mockup B groups requested scopes by category (read, write, admin) with clear icons. Ziyang preferred this over A's flat list. Decision: go with grouped scopes from mockup B.

2. **Trust indicators** — Jiahao raised concern about users blindly approving. Decision: show the requesting app's verified status (checkmark for verified publishers) and a "last used" timestamp if the app was previously authorized.

3. **Glass styling** — All three mockups used the liquid glass surface treatment. Sarah confirmed the consent card uses `glass-strong` with `shadow-glow` on the approve button. The deny button is ghost-styled. Approved as-is.

4. **Mobile responsiveness** — Ziyang flagged that mockup C's two-column layout breaks on narrow viewports. Decision: single-column stack on screens < 640px.

5. **Session memory** — Jiahao suggested remembering the approval for 30 days so agents don't re-prompt daily. Decision: implement "remember this device" checkbox, default unchecked, stores approval in a signed cookie.

## Decisions summary
- Grouped scope display (mockup B)
- Verified publisher badge + last-used timestamp
- Single-column on mobile
- 30-day approval memory via signed cookie, opt-in
- Ship target: March 26 (Sarah owns)
