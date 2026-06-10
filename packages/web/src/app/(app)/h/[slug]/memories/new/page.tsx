"use client";

/**
 * /h/<slug>/memories/new — full-screen compose surface (plan 22 §5.3).
 *
 * Mounts `<ComposeModal variant="route" defaultHubSlug={slug}>` on
 * mobile so the considered-capture flow (title + tags + hub picker +
 * editor + attachments) gets the full viewport instead of a centered
 * modal that would compete with the keyboard for vertical space.
 *
 * Desktop visitors get redirected to `/h/<slug>/memories?compose=1`,
 * which triggers the modal-variant ComposeModal via the existing
 * `?compose=1` deep-link handler in AppShellClient. Keeps desktop
 * compose as a centered modal (per plan 20) and avoids a jarring
 * full-screen route on a viewport that doesn't need it.
 *
 * Mobile detection: reads matchMedia synchronously in a useState
 * initializer so the first client render sees the real viewport
 * (unlike `useIsMobile()`, which defaults `false` for SSR-safety
 * and only corrects post-effect — that default would briefly
 * classify a cold mobile load as desktop and incorrectly redirect
 * BEFORE the matchMedia listener fires; codex H1).
 *
 * SSR returns `null` (the hook initializer detects no window),
 * which renders a blank page during server render and one frame of
 * client hydration. The blank is acceptable because compose is
 * always client-driven; SSR has no useful preview to render anyway.
 */

import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ComposeModal } from "@/components/features/compose/compose-modal";

const MOBILE_BREAKPOINT_PX = 768;

interface PageProps {
  params: Promise<{ slug: string }>;
}

export default function ComposeRoutePage({ params }: PageProps) {
  const { slug } = use(params);
  const router = useRouter();
  // null = not measured yet (SSR / pre-hydration), boolean = measured.
  // Reading matchMedia in the useState initializer means the first
  // client render already has the real value — no false-default
  // window where a cold mobile load could incorrectly redirect.
  const [isMobile, setIsMobile] = useState<boolean | null>(() => {
    if (typeof window === "undefined") return null;
    return window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`)
      .matches;
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`);
    const handler = (e: MediaQueryListEvent | MediaQueryList) =>
      setIsMobile(e.matches);
    handler(mq);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  useEffect(() => {
    // Wait for measurement before deciding on the redirect.
    if (isMobile === null || isMobile === true) return;
    router.replace(`/h/${encodeURIComponent(slug)}/memories?compose=1`);
  }, [isMobile, slug, router]);

  if (isMobile === null) return null;
  if (!isMobile) return null;
  return <ComposeModal variant="route" defaultHubSlug={slug} />;
}
