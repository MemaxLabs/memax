# @memaxlabs/ledger

The React components for Memax V2's Ledger design system: tinted paper and ink, receipts on a ruled margin, a serif for what is remembered and a green seal for what a person kept. Every V2 screen is built from this package and `@memaxlabs/ledger-tokens`.

The source of truth is the V2 design handoff (`memax-internal/docs/v2/handoff/design-system`). The production hardening follows the design review (`memax-internal/docs/v2/design-review.md` §2–§5).

AGPL-3.0-only, private workspace package, source-first: there is no build step, so consumers transpile it (`transpilePackages: ["@memaxlabs/ledger"]` in Next.js).

## Use

```css
/* Tokens first (custom properties, type styles, @font-face), then the components. */
@import "@memaxlabs/ledger-tokens";
@import "@memaxlabs/ledger/ledger.css";
```

```tsx
import Link from "next/link";
import {
  LedgerProvider,
  MemoryList,
  MemoryRow,
  ReviewCard,
} from "@memaxlabs/ledger";

<html lang="zh-CN" data-theme="dark">
  <LedgerProvider locale="zh" linkComponent={Link}>
    …
  </LedgerProvider>
</html>;
```

- `data-theme="dark"` on any element switches it and its contents to Carbon. Paper (light) is the default.
- `lang` must match the locale (`en`, `zh-CN`): the Chinese type rules hang off `:lang(zh)`.
- `LedgerProvider` takes `locale` (`en` | `zh`), `strings` (deep-partial overrides of a catalogue), `agents` (entries merged over `AGENTS`) and `linkComponent` (renders every Ledger link: rail items, `Button href`, `Cite href`, `MemoryRow href`). Without a provider, components use English and plain anchors.

## Components

| Area       | Components                                                                                                       |
| ---------- | ---------------------------------------------------------------------------------------------------------------- |
| Brand      | `Logo`, `Seal`, `Icon` (`ICON_NAMES`)                                                                            |
| Primitives | `Button`, `Kbd`, `Field`, `Segmented`                                                                            |
| Provenance | `AgentStamp` (`AGENTS`, `resolveAgent`), `Receipt`, `Cite`, `Highlight`                                          |
| Memory     | `StateMark`, `MemoryRow` + `MemoryList`, `MemoryText`, `Redaction`, `Diff`, `Lineage`, `ReviewCard`, `DreamCard` |
| Agents     | `HandoffSlip`, `AgentRow` + `AgentListHead`, `SyncTarget`, `DecisionGate`                                        |
| Frame      | `CommandBar` + `CommandDialog`, `NavRail`, `Shell`, `PageHeader`, `Terminal`                                     |

The 27 handoff components keep their names, their `mx-` classes and their look. Where the handoff API baked in demo data or owned state that belongs to the app, it changed:

- **Data is required where it is data.** `ReviewCard` has no `keptBy="ZZ"`/`keptDate="Oct 5"` defaults and `agent` is required. `NavRail` needs `items` (each with an `href`), `space`, `person`, `status` and `settingsHref`. `DreamCard` needs the real `noteIds` and `factIds`. `HandoffSlip`, `SyncTarget` and `AgentRow` need their `status` or `autonomy` and counts.
- **Controlled where the app owns the truth.** `ReviewCard` takes `kept`, `pending`, `onKeep` (may return a promise), `onUndo`, `keptBy`, `keptByName`, `keptDate`, `keptId`, `keptTime`; it never keeps itself. `AgentRow` takes `autonomy` + `onAutonomyChange`. `DecisionGate`, `Segmented` and `CommandBar` are controlled or uncontrolled (`value`/`defaultValue`, `selected`/`defaultSelected`, `query`/`defaultQuery`, `mode`/`defaultMode`).
- **Composition.** React 19 `ref` as a prop. `Button` renders a link with `href` or anything with `render` (Base UI `useRender`, e.g. `render={<Menu.Trigger />}`); `NavRail` takes `spaceRender` and `askRender` the same way. `CommandBar` drops `role="dialog"`; `CommandDialog` supplies the dialog (Base UI), the scrim and the 88px placement.
- **Typed.** `IconName` replaces `string` for icons, `Segmented` is generic over its values, and `MemoryRow` doesn't accept `forgotten` (a forgotten memory has no words: use `Redaction`).
- **Words come from the catalogue.** `Redaction by` is a name ("Jiahao"), not a phrase; "at your request" is the default.

## Strings

`en` (source of truth) and `zh` in `src/i18n/`, typed by `LedgerStrings`, with `{name}` placeholders and `Plural` forms. Tests enforce key and placeholder parity, the voice rules (no AI, magic, smart, delete, save, approve, exclamation marks or emoji; in Chinese no 智能, 删除, 保存, 批准), sentence case and no space before punctuation. The Chinese reuses V1's terms (记住, 保留, 忘记, 撤销, 拒绝, 编辑, 审阅, 交接, 主导航) and keeps "Agent" and agent names in English, as V1 does.

## Accessibility

- Real `<button>`, `<a>`, `<label>`, lists and headings. Focus is a 2px `focus` ring outside a 2px `paper` gap everywhere, including portals; `night-accent` on night surfaces.
- `Segmented` and `DecisionGate` are radio groups with one tab stop and arrow-key selection (Home/End too; `DecisionGate` also takes 1–9 while focused).
- `MemoryRow` is a list item. With `href` or `onClick` the statement becomes one stretched link or button, described by its state and receipt; row actions are real buttons in a labelled group, shown on hover and on focus.
- Every state is a word: `StateMark` shows it or names its glyph; proposed, stale and merged statements expose it to assistive technology; stamps read as names, not letters; glyphs, dots and keycaps are hidden and `aria-keyshortcuts` carries the key.
- A disabled control with a reason stays focusable (`aria-disabled`) and shows the reason; a pending one ignores presses with no spinner. Touch targets reach 44px on coarse pointers.
- `ReviewCard` moves focus between Keep and Undo as they replace each other, and announces its state politely.

## Motion

The seal stamps (280ms from 1.4× and 14°, then a 520ms ink ring), the statement cross-fades from italic to roman (320ms), the redaction bar draws in 240ms, the Dream edition plays once (`animate={false}` once it has been seen), and a stale underline fades in. CSS only: React 19.2's `<ViewTransition>` is not in the stable release this repo uses. Everything is off under `prefers-reduced-motion`.

## Chinese and narrow rails

- **Proposed in Chinese (design review D1, pending designer approval).** Songti SC and Noto Serif SC have no italic, so under `:lang(zh)` a proposed statement is upright `ink-2` with a 2px `ochre` rule on its left, never a synthesised oblique. The Dream headline breaks only at its spaces in Chinese (also pending approval).
- **Receipt rail (D4).** IDs never truncate; "space · source" ends in an ellipsis, so the source goes first. The full receipt is the rail's tooltip and the row's description. Between 1024 and 1180px the rail is 176px.

## Previews

`@memaxlabs/ledger/previews` exports one preview per component (`PREVIEWS`, with each one's artboard size) for the workbench gallery and the screenshot tests against `design-system/previews/<Name>-{light,dark}.png` (2×). Each takes `theme` and `locale`. Load `@memaxlabs/ledger/previews.css` after `ledger.css`. Never import previews from product code.

## Develop

```bash
pnpm turbo lint --concurrency=1 --filter=@memaxlabs/ledger   # eslint (zero warnings) + tsc
pnpm turbo test --concurrency=1 --filter=@memaxlabs/ledger   # vitest; *.dom.test.tsx run in jsdom
```

`src/styles/` holds the handoff's `bundle.css` split by area and copied verbatim (each file names its line range), then `hardening.css` with every production addition. To change a component's look, change the handoff and re-copy; the CSS tests reject literal colours, undefined tokens, undefined `mx-` classes, gradients and glass.
