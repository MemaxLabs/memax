## Code Pattern: React Hook Composition in Memax Web App

Standard patterns for composing React hooks in the memax Next.js web app. These conventions ensure consistent data fetching, auth gating, and locale handling across all pages and components.

### useAuth — Authentication Guard

```typescript
import { useAuth } from "@/hooks/use-auth";

export function MemoryListPage() {
  const { user, isLoading, isAuthenticated } = useAuth();

  if (isLoading) return <MemoryListSkeleton />;
  if (!isAuthenticated) {
    redirect("/login");
    return null;
  }

  return <MemoryList userId={user.id} />;
}
```

Key rules:
- `useAuth()` reads from the auth context provided by `AuthProvider` in the root layout
- Never call `redirect()` inside a hook — only in the component body after the hook returns
- The `user` object is `null` until `isLoading` is `false`, so always guard with `isLoading` first

### useLocale — i18n Translation

```typescript
import { useLocale } from "@/hooks/use-locale";

export function HubCard({ hub }: { hub: Hub }) {
  const { t, locale } = useLocale();

  return (
    <div className="glass rounded-xl p-4">
      <h3 className="text-display-sm">{hub.name}</h3>
      <p className="text-muted-foreground text-sm">
        {t.hub.memberCount(hub.members.length)}
      </p>
      <time className="text-xs text-muted-foreground">
        {new Intl.DateTimeFormat(locale, {
          dateStyle: "medium",
        }).format(new Date(hub.createdAt))}
      </time>
    </div>
  );
}
```

Key rules:
- Every user-facing string MUST go through `t.*` — no hardcoded English text
- Use `Intl.DateTimeFormat` with the `locale` from `useLocale()` for dates
- Translation keys follow dot notation: `t.hub.memberCount`, `t.memory.title`, etc.

### useQuery — Data Fetching with TanStack Query

```typescript
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { memaxClient } from "@/lib/memax-client";

export function useMemories(hubId: string) {
  return useQuery({
    queryKey: ["memories", hubId],
    queryFn: () => memaxClient.memories.list({ hubId, limit: 50 }),
    staleTime: 30_000,        // 30s before refetch
    gcTime: 5 * 60_000,       // 5min garbage collection
    retry: 2,
    enabled: !!hubId,         // don't fetch until hubId is available
  });
}

export function usePushMemory() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: PushMemoryInput) => memaxClient.memories.push(input),
    onSuccess: (_data, variables) => {
      // Invalidate the memory list for the target hub
      queryClient.invalidateQueries({
        queryKey: ["memories", variables.hubId],
      });
    },
  });
}
```

### Composing Multiple Hooks

```typescript
export function MemoryDetail({ memoryId }: { memoryId: string }) {
  const { user, isLoading: authLoading } = useAuth();
  const { t } = useLocale();

  const { data: memory, isLoading: memoryLoading } = useQuery({
    queryKey: ["memory", memoryId],
    queryFn: () => memaxClient.memories.get(memoryId),
    enabled: !authLoading && !!user,
  });

  if (authLoading || memoryLoading) {
    return <MemoryDetailSkeleton />;
  }

  if (!memory) {
    return <EmptyState message={t.memory.notFound} />;
  }

  return (
    <article className="glass rounded-2xl p-6 animate-fade-up">
      <h1 className="text-display-md">{memory.title}</h1>
      <MemoryBody content={memory.body} />
    </article>
  );
}
```

### Anti-Patterns

- **Never fetch inside useEffect** — use `useQuery` instead. It handles caching, deduplication, and background refetching.
- **Never pass `user` as a query key** — use `user.id` or `user.hubId`. Object identity changes on every render.
- **Never nest useMutation inside useQuery** — define them at the same level and compose in the component.
- **Never call hooks conditionally** — React requires hooks to be called in the same order every render. Use the `enabled` option on `useQuery` instead of wrapping in `if`.
