---
name: i18n
description: "Use when writing or modifying any user-facing text in the memax web app. Ensures all strings go through the i18n translation system (t.* from useLocale()) and follow brand voice. ALWAYS trigger when adding components with visible text, modifying button labels, error messages, placeholders, tooltips, or any string a user could see. Never hardcode English strings in JSX."
---

## Rule: No Hardcoded User-Facing Strings

Every string a user can see MUST go through `t.*` from `useLocale()`. No exceptions. This includes:

- Button labels, CTA text
- Placeholder text
- Status messages, loading states
- Error messages
- Empty states
- Tooltips, aria-labels with text
- Section headers
- Confirmation dialogs (Forget/Keep)
- **Data-driven display labels** (category names, source names, content type labels) — these look like "data" but they render as user-visible text
- **Sub-component strings** — every function in a file, not just the main export

**Before writing ANY new text in JSX**, check if the key exists in `en.ts`. If not, add it to both `en.ts` and `zh.ts` first, then use `t.section.key`.

## Two-Step Process: Coverage → Quality

**Coverage and quality are different problems. Both are required.**

1. **Coverage (mechanical):** Every user-visible string goes through `t.*`. Grep audit, TypeScript type enforcement.
2. **Quality (human):** Every Chinese string reads like a native speaker wrote it, not translated it. Read `zh.ts` aloud — if it sounds like software documentation, it's wrong.

**After any batch migration, ALWAYS do a native review pass:**

- Read every new `zh.ts` string as if you're a Chinese user seeing this product for the first time
- Ask: "会有人这么说话吗？" (Would anyone actually say this?)
- Check: Is this 口语 or 书面语? Brand voice demands 口语.
- Check: Is this translated in context or in isolation? (e.g., "human" in chat context = "真人", not "人工")

## Architecture

```
packages/web/src/i18n/
  locales/
    en.ts    — source of truth, defines Translations type
    zh.ts    — Chinese translations
  index.tsx  — LocaleProvider, useLocale, useMemoryCount hooks
```

**How it works:**

- `useLocale()` returns `{ locale, setLocale, t }` — fully typed
- `t.forget.button` → "Forget" (en) / "忘记" (zh)
- Auto-detects `navigator.language`, persists in `localStorage`
- No URL routing — locale is a user preference, not a route
- `LocaleProvider` wraps the app in `providers.tsx`

## Adding a New String

1. Add the key to `en.ts` (source of truth)
2. Add the Chinese translation to `zh.ts`
3. Use `t.section.key` in the component
4. For interpolation: `interpolate(t.foo.bar, { n: count })` via `useInterpolate()`
5. **For any `{n} things` label, ALWAYS add the singular form first** — see Plurals below

```tsx
// ✓ Correct
const { t } = useLocale();
<button>{t.forget.button}</button>
<span>{t.bar.placeholder.searchIn.replace("{title}", memory.title)}</span>

// ✗ Wrong — hardcoded string
<button>Forget</button>
<span>{`Search in ${memory.title}...`}</span>
```

## Plurals (read FIRST when adding any count label)

**Rule: every `{n} X` label in English needs a singular companion.** "1 memories" / "1 topics" / "1 members" is a bug. Add the singular form the moment you add the plural — not later. English inflects, Chinese doesn't, so en.ts is where the grammar mismatch shows up.

**Naming convention:** `<key>` for the plural, `<key>One` for the singular.

```ts
// en.ts
topics: {
  memories: "{n} memories",
  memoryOne: "1 memory",
  topics: "{n} topics",
  topicOne: "1 topic",
}

// zh.ts — Chinese has no singular/plural inflection, but mirror the keys
// so Translations type parity holds. Use the same noun + literal "1".
topics: {
  memories: "{n} 条记忆",
  memoryOne: "1 条记忆",
  topics: "{n} 个主题",
  topicOne: "1 个主题",
}
```

**At the call site, use the `pluralize` helper from `@/i18n`:**

```tsx
import { pluralize, useLocale } from "@/i18n";
const { t } = useLocale();
// Correct — picks singular when n === 1, interpolates {n} otherwise
<span>{pluralize(t.topics.memoryOne, t.topics.memories, count)}</span>;
```

**Helper signature:** `pluralize(one: string, other: string, n: number)` — returns `one` verbatim when `n === 1`, otherwise `interpolate(other, { n })`.

**Decision: when do you NEED a singular variant?**

- ✅ Required: nouns that inflect in English (`{n} memories/topics/members/files/keys`). A `{n} X` where `X` changes between count 1 and >1 is in scope.
- ⚪ Optional: participles and short shorthand where "1 pushed" / "1m ago" / "5m" read fine at any count — these can stay plural-only.
- ⚪ Skip: rate-formatted counts ("5 / 100 pushes") where the divisor guarantees plural reading regardless of count.

**Check before shipping:** grep for `\{n\}\s+\w+(s|es)[\",.]` in your new keys and verify every hit has a `<key>One` companion. If a caller passes an unchecked `count` into one of these templates and `count` can legitimately be 1, you must use `pluralize`.

**Existing patterns that predate the helper** (still valid, don't rewrite):

- `useMemoryCount()` hook for `t.memory.zero/one/other` — covers the primary memory count everywhere.
- Inline ternaries for asymmetric pairs like `count === 1 ? t.toast.forgot : interpolate(t.batch.forgot, { n: count })` where the singular form is a different sentence ("Forgot.") rather than a direct `"1 X"` inflection.

## Brand Voice — Chinese (中文)

memax 是一个有温度的记忆伙伴，不是冰冷的数据库。语气：温暖、简洁、稍微俏皮。

| English            | 中文         | Notes                      |
| ------------------ | ------------ | -------------------------- |
| Remember           | 记住         | 核心动词                   |
| Recall             | 回忆         | 有诗意，符合 memory 主题   |
| Forget             | 忘记         | 简洁                       |
| Keep               | 保留         | 忘记的反义                 |
| Ask memax          | 问问 memax   | 叠词更亲切                 |
| Remembered.        | 记住了。     | 口语化，不是"已记住"       |
| Drop to remember   | 松手即记住   | 动作描述，不是指令         |
| Type anything      | 随便写点什么 | 随意感，不是"输入任何内容" |
| No memories yet    | 还没有记忆   | 温暖，不是"记忆为空"       |
| Could not generate | 没能生成     | 承认失败，不是"生成失败"   |

**Status messages — 要有生命感:**

| English         | 中文     | Feel                        |
| --------------- | -------- | --------------------------- |
| listening       | 在听     | 简短，像朋友                |
| organizing      | 整理中   | 在帮你做事                  |
| dreaming        | 遐想中   | 有品牌特色，planned feature |
| connecting dots | 串联记忆 | 用 memax 自己的语言         |
| thinking        | 想一想   | 俏皮                        |

**Translation principles:**

- 不要直译，要意译。"Type anything" ≠ "输入任何内容"，= "随便写点什么"
- 用口语，不用书面语。"记住了" not "已成功记住"
- 不要用软件说明书语言。"在上面输入" ✗ → "随便写点什么就行" ✓
- 失败用温和语气承认。"失败了" ✗ → "没能总结出来" ✓
- 按钮/标签要精简到最短自然表达。"+ 添加" ✗ → "+ 标签" ✓
- 领域词汇用对语境：human (chat context) = "真人" not "人工"
- 加"来着""吧""呢"等语气词让句子更口语
- memax 始终小写
- 技术术语保留英文：MCP, CLI, API
- Agent 名称不翻译：Claude Code, Cursor, Windsurf

**常见翻错/翻硬的模式：**

| 翻硬了               | 自然的           | 为什么                         |
| -------------------- | ---------------- | ------------------------------ |
| "写下你的记忆"       | "想记点什么"     | 没人会说"写下我的记忆"         |
| "生成摘要失败了"     | "没能总结出来"   | "失败"太冰冷，承认比报错好     |
| "在上面输入就能开始" | "随便写点什么"   | 说明书 vs 朋友提示             |
| "添加"               | "标签"           | 用名词，不用动词，界面更干净   |
| "人工"(human)        | "真人"           | "人工"是人造的意思，不是真人   |
| "对话"(chat)         | "聊天"           | "对话"是电影字幕用词           |
| "权衡"(tradeoff)     | "取舍"           | "权衡"太文绉绉                 |
| "片段"(snippet)      | "代码段"         | "片段"是文学词，代码语境要具体 |
| "谁负责账单？"       | "账单谁管的？"   | 更短更口语                     |
| "API 限速是多少？"   | "限速多少来着？" | "来着"是回忆的自然语气         |

## Adding a New Language

1. Create `locales/xx.ts` importing `type Translations` from `en.ts`
2. TypeScript enforces all keys are present
3. Add the locale to `LOCALES` map and `Locale` type in `index.tsx`
4. The settings toggle auto-cycles through all locales

## Audit Checklist

Before shipping any UI change, run BOTH audits:

**1. Coverage audit — find hardcoded strings:**

```bash
# Find potential hardcoded English in JSX (check ALL components in each file)
grep -nP '>\s*[A-Z][a-z]{2,}' packages/web/src/app/(app)/home/page-new.tsx | grep -v 'svg\|className\|import\|const\|//'

# Find hardcoded placeholder/aria-label attributes
grep -nP 'placeholder="[A-Z]|aria-label="[A-Z]' packages/web/src/

# Find data-driven labels that should be translated
grep -nP '\.label\s*(??|:)' packages/web/src/
```

**Common blind spots:**

- Sub-components defined in the same file (e.g., `SetupBlock`, `ManualConfig` inside `settings-panel.tsx`)
- Data lookup tables (`CATEGORY_META[x].label`, source name maps)
- Format functions at module scope (`formatAge`, `formatCategory`) — these produce user-visible strings too

**2. Quality audit — read zh.ts as a native speaker:**

- Read every string aloud. Does it sound like a person or a manual?
- Check for 书面语 that should be 口语
- Check for words translated in isolation that are wrong in context
- Check for overly formal verbs where nouns or shorter phrases work

**Known unmigrated files (as of April 2026):**

- Legacy UI files (`page-old.tsx`, `search-command.tsx`, `quick-push-fab.tsx`, `sidebar.tsx`, `dock.tsx`) — not migrated, only active in `?ui=legacy` mode
- `memories/[id]/page.tsx` — standalone memory page, not part of main new UI flow
