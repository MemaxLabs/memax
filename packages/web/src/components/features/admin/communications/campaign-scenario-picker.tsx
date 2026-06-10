"use client";

import { Bell, Gift, Mail, type LucideIcon } from "lucide-react";

export type CampaignKind =
  | "system_notice"
  | "gift_invite_link"
  | "email_announcement";

export function CampaignScenarioPicker({
  value,
  onChange,
  labels,
}: {
  value: CampaignKind;
  onChange: (next: CampaignKind) => void;
  labels: {
    announcement: { title: string; description: string };
    gift: { title: string; description: string };
    email: { title: string; description: string };
  };
}) {
  const scenarios: {
    id: CampaignKind;
    icon: LucideIcon;
    title: string;
    description: string;
  }[] = [
    {
      id: "system_notice",
      icon: Bell,
      title: labels.announcement.title,
      description: labels.announcement.description,
    },
    {
      id: "gift_invite_link",
      icon: Gift,
      title: labels.gift.title,
      description: labels.gift.description,
    },
    {
      id: "email_announcement",
      icon: Mail,
      title: labels.email.title,
      description: labels.email.description,
    },
  ];

  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {scenarios.map((s) => {
        const Icon = s.icon;
        const isActive = value === s.id;
        return (
          <button
            key={s.id}
            type="button"
            onClick={() => onChange(s.id)}
            aria-pressed={isActive}
            className={`group flex items-start gap-3 rounded-surface border p-4 text-left transition-all cursor-pointer ${
              isActive
                ? "border-fg-1/30 bg-surface-2 shadow-sm"
                : "border-border/50 bg-surface-1 hover:border-border hover:bg-surface-2"
            }`}
          >
            <div
              className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${
                isActive
                  ? "bg-fg-1 text-background"
                  : "bg-surface-2 text-fg-2 group-hover:text-fg-1"
              }`}
            >
              <Icon className="h-4 w-4" />
            </div>
            <div className="min-w-0">
              <p className="text-[14px] font-semibold text-fg-1">{s.title}</p>
              <p className="mt-1 text-[12.5px] leading-relaxed text-fg-2">
                {s.description}
              </p>
            </div>
          </button>
        );
      })}
    </div>
  );
}
