# @memaxlabs/ledger-tokens

The Ledger design tokens for Memax V2: colour, type, spacing, radius, motion and the four typefaces. Paper (light) is the default; Carbon (dark) applies inside any element with `data-theme="dark"`.

This package is Apache-2.0 so that every surface can use it: the web app, the docs site, email templates and the CLI's terminal palette. The components live in `@memaxlabs/ledger` (AGPL-3.0).

| File          | What it is                                                                                     |
| ------------- | ---------------------------------------------------------------------------------------------- |
| `tokens.css`  | Every token as a CSS custom property, for both themes, plus the `@font-face` rules             |
| `type.css`    | The type styles (`display`, `title`, `memory`, `memory-proposed`, `ui`, `label`, `receipt`, …) |
| `index.css`   | Both of the above                                                                              |
| `tokens.json` | The same tokens as data, with light and dark values                                            |
| `fonts/`      | woff2 files (SIL OFL 1.1)                                                                      |
| `assets/`     | The mark, the lockups and the state glyphs, as SVG                                             |

```css
@import "@memaxlabs/ledger-tokens";
```

## Rules

- Never use a literal colour: write `var(--seal)`, not a hex value.
- `seal` (green) means a person kept it, or it matches what they kept. `ochre` means it's waiting on a person. `vermilion` means conflict or destruction. The `night` surface is only for Dream and the terminal.
- Newsreader sets what is remembered, Schibsted Grotesk the interface, IBM Plex Mono receipts and IDs, and Gloock only the name "Memax".

The source of truth is the V2 design handoff (`memax-internal/docs/v2/handoff/design-system`). To change a token, change the handoff first and regenerate this package.
