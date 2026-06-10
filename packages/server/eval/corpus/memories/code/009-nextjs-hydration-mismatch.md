## Stack Trace: Next.js Hydration Mismatch — Date Formatting

Hit a persistent React hydration error on the memory detail page. The error only appeared in production builds (`next build && next start`) and was invisible during `next dev` because dev mode suppresses hydration warnings.

### Error Output

```
Unhandled Runtime Error

Error: Hydration failed because the server rendered HTML didn't match the client.
See more info here: https://nextjs.org/docs/messages/react-hydration-error

Tree mismatch:
  - Server: <time>April 9, 2026</time>
  - Client: <time>Apr 9, 2026</time>

During SSR:
  at time
  at MemoryMeta (./src/components/memory-meta.tsx:14:5)
  at div
  at MemoryDetail (./src/components/memory-detail.tsx:28:3)
  at InnerLayoutRouter (./node_modules/next/dist/client/components/inner-layout-router.js:18:11)
  at RedirectBoundary
  at NotFoundBoundary
  at LoadingBoundary
  at ErrorBoundary

Warning: Text content did not match. Server: "April 9, 2026" Client: "Apr 9, 2026"
    at time
    at MemoryMeta
```

### Root Cause

The `MemoryMeta` component was formatting dates using `toLocaleDateString()` without specifying a locale:

```typescript
// BEFORE — hydration mismatch
function MemoryMeta({ memory }: { memory: Memory }) {
  const date = new Date(memory.createdAt);

  return (
    <time dateTime={memory.createdAt}>
      {date.toLocaleDateString(undefined, { dateStyle: "medium" })}
    </time>
  );
}
```

The problem: `toLocaleDateString(undefined, ...)` uses the system's default locale. On the Vercel server (Linux), the default locale resolved to `en-US` which formats as "April 9, 2026". On the client (Chrome on macOS), it resolved to `en-US` but with a different ICU dataset that formats as "Apr 9, 2026".

This is a classic SSR/CSR mismatch — the server and client environments have different locale implementations even when the locale name is the same.

### Fix

Use the explicit locale from our i18n hook, and use `Intl.DateTimeFormat` which is more deterministic:

```typescript
// AFTER — deterministic formatting
import { useLocale } from "@/hooks/use-locale";

function MemoryMeta({ memory }: { memory: Memory }) {
  const { locale } = useLocale();
  const date = new Date(memory.createdAt);

  const formatted = new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
  }).format(date);

  return <time dateTime={memory.createdAt}>{formatted}</time>;
}
```

### Additional Fix: Suppress for Relative Times

For relative time displays ("3 hours ago"), we use `suppressHydrationWarning` because these will always differ between server render time and client hydration time:

```typescript
function RelativeTime({ date }: { date: string }) {
  const { locale } = useLocale();
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

  return (
    <time dateTime={date} suppressHydrationWarning>
      {formatRelativeTime(new Date(date), rtf)}
    </time>
  );
}
```

### Lesson

- Never use `toLocaleDateString()` or `toLocaleString()` without an explicit locale in SSR components
- Use `Intl.DateTimeFormat` with explicit options for deterministic formatting
- For values that inherently differ between server and client (relative time, "now"), use `suppressHydrationWarning`
- Always test with `next build && next start` — `next dev` hides hydration mismatches
- The Vercel deployment uses Node.js with `full-icu` but ICU data versions may differ from the user's browser
