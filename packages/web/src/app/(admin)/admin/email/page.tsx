"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { MemaxLoader } from "@memaxlabs/ui";

function EmailRedirect() {
  const router = useRouter();
  const searchParams = useSearchParams();

  useEffect(() => {
    const query = searchParams.toString();
    router.replace(
      `/admin/communications/transactional${query ? `?${query}` : ""}`,
    );
  }, [router, searchParams]);

  return <MemaxLoader />;
}

export default function AdminEmailRedirectPage() {
  return (
    <Suspense fallback={<MemaxLoader />}>
      <EmailRedirect />
    </Suspense>
  );
}
