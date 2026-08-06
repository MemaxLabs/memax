"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { usePathname, useRouter } from "next/navigation";
import { useIsMobile } from "@/hooks/use-is-mobile";

interface InboxSurfaceState {
  open: boolean;
  setOpen: (open: boolean) => void;
  openInbox: () => void;
  closeInbox: () => void;
}

const InboxSurfaceContext = createContext<InboxSurfaceState | null>(null);

export function InboxSurfaceProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const isMobile = useIsMobile();
  const pathname = usePathname();
  const router = useRouter();

  useEffect(() => {
    setOpen(false);
  }, [pathname]);

  useEffect(() => {
    if (isMobile) {
      setOpen(false);
    }
  }, [isMobile]);

  const closeInbox = useCallback(() => {
    setOpen(false);
  }, []);

  const openInbox = useCallback(() => {
    if (isMobile) {
      // /inbox retired in plan 25 P4 — the pulse board hosts the
      // decisions and receipts the inbox used to own.
      router.push("/pulse");
      return;
    }
    setOpen(true);
  }, [isMobile, router]);

  const value = useMemo(
    () => ({
      open,
      setOpen,
      openInbox,
      closeInbox,
    }),
    [closeInbox, open, openInbox],
  );

  return (
    <InboxSurfaceContext.Provider value={value}>
      {children}
    </InboxSurfaceContext.Provider>
  );
}

export function useInboxSurface() {
  const ctx = useContext(InboxSurfaceContext);
  if (!ctx) {
    throw new Error(
      "useInboxSurface must be used within InboxSurfaceProvider.",
    );
  }
  return ctx;
}
