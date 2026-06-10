"use client";

import Link from "next/link";
import { Badge, Skeleton } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import type { AdminEmailTemplateSummary } from "@/lib/admin-client";

export function EmailTemplateList({
  templates,
  isLoading,
  selectedName,
}: {
  templates: AdminEmailTemplateSummary[];
  isLoading: boolean;
  selectedName?: string;
}) {
  const { t } = useLocale();

  return (
    <aside className="rounded-surface border border-border/50 bg-card p-3">
      <div className="border-b border-border/40 px-3 pb-3">
        <p className="text-[13px] font-medium text-fg-2">
          {t.admin.email.catalogTitle}
        </p>
        <p className="mt-1 text-[12px] text-fg-3">
          {t.admin.email.catalogDescription}
        </p>
      </div>

      <div className="mt-3 space-y-2">
        {isLoading
          ? Array.from({ length: 5 }).map((_, index) => (
              <div
                key={index}
                className="rounded-surface border border-border/40 p-3"
              >
                <Skeleton className="mb-2 h-4 w-28" />
                <Skeleton className="mb-2 h-3 w-full" />
                <Skeleton className="h-3 w-24" />
              </div>
            ))
          : templates.map((template) => {
              const isActive = template.name === selectedName;
              return (
                <Link
                  key={template.name}
                  href={`/admin/communications/transactional?template=${template.name}`}
                  className={`block rounded-surface border px-3 py-3 transition-colors ${
                    isActive
                      ? "border-foreground/20 bg-surface-2"
                      : "border-border/40 bg-surface-1 hover:border-border/70 hover:bg-surface-2"
                  }`}
                >
                  <div className="flex items-center justify-between gap-3">
                    <p className="text-[13px] font-medium text-fg-1">
                      {template.name}
                    </p>
                    <Badge
                      variant={template.has_override ? "default" : "secondary"}
                    >
                      {template.has_override
                        ? t.admin.email.status.custom
                        : t.admin.email.status.default}
                    </Badge>
                  </div>
                  <p className="mt-1 line-clamp-2 text-[12px] text-fg-3">
                    {template.description}
                  </p>
                  <div className="mt-3 flex items-center justify-between text-[11px] text-fg-3">
                    <span>{template.category}</span>
                    <span>
                      {t.admin.email.variableCount.replace(
                        "{count}",
                        String(template.variable_count),
                      )}
                    </span>
                  </div>
                </Link>
              );
            })}
      </div>
    </aside>
  );
}
