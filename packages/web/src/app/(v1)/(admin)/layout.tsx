"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter, usePathname } from "next/navigation";
import { Suspense } from "react";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { MemaxLoader } from "@memaxlabs/ui";
import {
  ListChecks,
  Users,
  Building2,
  Activity,
  ArrowLeft,
  Bell,
  MessageSquare,
  Layers,
  Sprout,
  Server,
  Menu,
  X,
} from "lucide-react";

export default function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Suspense fallback={<MemaxLoader />}>
      <AdminShell>{children}</AdminShell>
    </Suspense>
  );
}

const ADMIN_MODULES = [
  { id: "waitlist", href: "/admin/waitlist", icon: ListChecks, enabled: true },
  { id: "users", href: "/admin/users", icon: Users, enabled: true },
  { id: "hubs", href: "/admin/hubs", icon: Building2, enabled: true },
  { id: "plans", href: "/admin/plans", icon: Layers, enabled: true },
  {
    id: "seedMemories",
    href: "/admin/seed-memories",
    icon: Sprout,
    enabled: true,
  },
  {
    id: "communications",
    href: "/admin/communications",
    icon: MessageSquare,
    enabled: true,
  },
  // Plan 18 super-notif funnel — operator analytics for the
  // checklist + digest cohorts. Single landing page today
  // (/admin/notifications/super); the `startsWith(href)` match in
  // AdminShell still highlights this entry on any nested route
  // we add later under /admin/notifications/*.
  {
    id: "notifications",
    href: "/admin/notifications/super",
    icon: Bell,
    enabled: true,
  },
  { id: "infra", href: "/admin/infra", icon: Server, enabled: true },
  { id: "system", href: "/admin/system", icon: Activity, enabled: false },
] as const;

function AdminShell({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const { t } = useLocale();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const drawerRef = useRef<HTMLElement | null>(null);
  const hamburgerRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!loading && !user) router.replace("/login");
    if (!loading && user && !user.admin_role) router.replace("/home");
  }, [user, loading, router]);

  // Auto-close the mobile drawer on route change so navigating also dismisses.
  useEffect(() => {
    setDrawerOpen(false);
  }, [pathname]);

  // Drawer focus management:
  //   - Esc closes it.
  //   - On open, move focus into the drawer so Tab cycles within it.
  //   - On close, return focus to the hamburger that opened it.
  //   - Tab wraps between first and last focusable elements of the
  //     drawer (simple keyboard containment — good enough for an
  //     operator surface, not a full modal manager).
  useEffect(() => {
    if (!drawerOpen) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    // Capture the hamburger node at effect-open time so the cleanup
    // function doesn't read a ref that may have been unmounted by the
    // time we return focus.
    const hamburgerAtOpen = hamburgerRef.current;
    const focusables = () => {
      const root = drawerRef.current;
      if (!root) return [] as HTMLElement[];
      return Array.from(
        root.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      );
    };
    // Focus first actionable item in the drawer.
    const items = focusables();
    items[0]?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setDrawerOpen(false);
        return;
      }
      if (e.key === "Tab") {
        const list = focusables();
        if (list.length === 0) return;
        const first = list[0];
        const last = list[list.length - 1];
        const active = document.activeElement as HTMLElement | null;
        if (e.shiftKey && active === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && active === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      (previouslyFocused ?? hamburgerAtOpen)?.focus?.();
    };
  }, [drawerOpen]);

  if (loading) return <MemaxLoader />;
  if (!user?.admin_role) return null;

  const nav = (
    <>
      <div className="px-5 pt-6 pb-4 flex items-center justify-between">
        <div className="text-[15px] font-semibold tracking-tight text-fg-1">
          {t.admin.title}
        </div>
        <button
          onClick={() => setDrawerOpen(false)}
          aria-label={t.admin.nav.closeMenu}
          className="md:hidden h-8 w-8 inline-flex items-center justify-center rounded-lg text-fg-3 hover:text-fg-1 hover:bg-surface-1 transition-colors"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      <nav className="flex-1 px-3 space-y-0.5 overflow-y-auto">
        {ADMIN_MODULES.filter((m) => m.enabled).map((mod) => {
          const Icon = mod.icon;
          const isActive = pathname.startsWith(mod.href);
          const label =
            t.admin.nav[mod.id as keyof typeof t.admin.nav] ?? mod.id;

          return (
            <Link
              key={mod.id}
              href={mod.href}
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-[14px] transition-colors ${
                isActive
                  ? "bg-surface-2 text-fg-1 font-medium"
                  : "text-fg-2 hover:text-fg-1 hover:bg-surface-1"
              }`}
            >
              <Icon className="h-4 w-4 shrink-0" />
              {label}
            </Link>
          );
        })}
      </nav>
      <div className="px-3 pb-5 pt-2 border-t border-border/40 mx-3">
        <Link
          href="/home"
          className="flex items-center gap-2 px-3 py-2 rounded-lg text-[13px] text-fg-2 hover:text-fg-1 hover:bg-surface-1 transition-colors"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          {t.admin.backToApp}
        </Link>
      </div>
    </>
  );

  return (
    <div className="flex h-screen bg-background">
      {/* Desktop sidebar — fixed, always visible from md: up */}
      <aside className="hidden md:flex w-60 shrink-0 border-r border-border/50 bg-background flex-col">
        {nav}
      </aside>

      {/* Mobile drawer + backdrop (below md:). Portal-free, positioned
          absolutely within the shell. Tagged as role="dialog" +
          aria-modal so screen readers announce it as a modal and Tab
          handling stays inside — see the focus-trap effect above. */}
      {drawerOpen && (
        <>
          <button
            type="button"
            aria-label={t.admin.nav.closeMenu}
            onClick={() => setDrawerOpen(false)}
            className="md:hidden fixed inset-0 z-modal bg-background/60 backdrop-blur-sm"
            style={{ transitionTimingFunction: "var(--ease-spring)" }}
          />
          <aside
            ref={drawerRef}
            role="dialog"
            aria-modal="true"
            aria-label={t.admin.title}
            className="md:hidden fixed inset-y-0 left-0 z-modal w-72 glass-strong flex flex-col shadow-xl"
          >
            {nav}
          </aside>
        </>
      )}

      {/* Main content area */}
      <main className="flex-1 overflow-y-auto min-w-0">
        {/* Mobile top bar — hamburger + brand. Hidden from md: up. */}
        <header className="md:hidden sticky top-0 z-bar-notif flex items-center gap-3 px-4 h-14 border-b border-border/50 bg-background/90 backdrop-blur">
          <button
            ref={hamburgerRef}
            onClick={() => setDrawerOpen(true)}
            aria-label={t.admin.nav.openMenu}
            aria-expanded={drawerOpen}
            className="h-9 w-9 -ml-1 inline-flex items-center justify-center rounded-lg text-fg-2 hover:text-fg-1 hover:bg-surface-1 transition-colors"
          >
            <Menu className="h-5 w-5" />
          </button>
          <div className="text-[14px] font-semibold tracking-tight text-fg-1">
            {t.admin.title}
          </div>
        </header>
        {children}
      </main>
    </div>
  );
}
