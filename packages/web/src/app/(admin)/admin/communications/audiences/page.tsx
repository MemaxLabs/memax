"use client";

import Link from "next/link";
import { Badge, Button, Skeleton } from "@memaxlabs/ui";
import { AlertCircle, Plus } from "lucide-react";
import { useLocale } from "@/i18n";
import { useAdminAudiences } from "@/hooks/use-admin-audiences";

export default function AudiencesListPage() {
  const { t } = useLocale();
  const copy = t.admin.communications.audiences;
  const { data, isLoading, error } = useAdminAudiences();
  const audiences = data?.audiences ?? [];

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-[18px] font-semibold tracking-tight text-fg-1">
            {copy.title}
          </h2>
          <p className="mt-1 max-w-2xl text-[13.5px] text-fg-2">
            {copy.description}
          </p>
        </div>
        <Link href="/admin/communications/audiences/new">
          <Button className="cursor-pointer">
            <Plus className="mr-1 h-3.5 w-3.5" />
            {copy.newButton}
          </Button>
        </Link>
      </div>

      {error ? (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5" />
          {copy.loadError}
        </div>
      ) : null}

      <div>
        {isLoading ? (
          <div className="grid gap-3 md:grid-cols-2">
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
          </div>
        ) : audiences.length === 0 ? (
          <div className="rounded-surface border border-border/50 bg-card py-10 text-center">
            <p className="text-[13px] text-fg-2">{copy.empty}</p>
          </div>
        ) : (
          <ul className="grid gap-3 md:grid-cols-2">
            {audiences.map((a) => (
              <li key={a.id}>
                <Link
                  href={`/admin/communications/audiences/${a.id}`}
                  className="block rounded-surface border border-border/50 bg-card p-5 transition-colors hover:border-border hover:bg-surface-1"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-[14px] font-semibold text-fg-1">
                        {a.name}
                      </p>
                      {a.description ? (
                        <p className="mt-0.5 line-clamp-2 text-[12.5px] text-fg-2">
                          {a.description}
                        </p>
                      ) : null}
                    </div>
                    <Badge variant="secondary" className="text-[11px]">
                      {copy.ruleTypeLabel[a.rule_type]}
                    </Badge>
                  </div>
                  <p className="mt-3 text-[11.5px] text-fg-3">
                    {new Date(a.updated_at).toLocaleString()}
                  </p>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
