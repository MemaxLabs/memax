"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useSettingsDialog } from "@/contexts/settings-dialog-context";

export default function SettingsPage() {
  const router = useRouter();
  const { open } = useSettingsDialog();

  useEffect(() => {
    router.replace("/home");
    const t = setTimeout(() => open({ kind: "you", section: "profile" }), 100);
    return () => clearTimeout(t);
  }, [router, open]);

  return null;
}
