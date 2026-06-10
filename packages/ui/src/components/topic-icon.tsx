"use client";

import {
  Folder,
  Code,
  Rocket,
  Database,
  ShieldCheck,
  Layout,
  FileText,
  Book,
  Globe,
  Wrench,
  Heart,
  Star,
  Lightbulb,
  Server,
  Zap,
  Users,
  Terminal,
  Settings,
  Bug,
  type LucideIcon,
} from "lucide-react";

/**
 * TopicIcon — resolve a lucide icon name (as stored in topics.icon or
 * DreamTopicRef.icon) to its component. Pure lookup with a Folder
 * fallback for unknown names. Kept in @memaxlabs/ui so TopicChip and
 * kitchen demos can render full topic identity without depending on
 * web-side code.
 */
const ICON_MAP: Record<string, LucideIcon> = {
  folder: Folder,
  code: Code,
  rocket: Rocket,
  database: Database,
  shield: ShieldCheck,
  "shield-check": ShieldCheck,
  layout: Layout,
  "file-text": FileText,
  book: Book,
  globe: Globe,
  wrench: Wrench,
  heart: Heart,
  star: Star,
  lightbulb: Lightbulb,
  server: Server,
  zap: Zap,
  users: Users,
  terminal: Terminal,
  settings: Settings,
  bug: Bug,
};

export function TopicIcon({
  name,
  className,
}: {
  name: string;
  className?: string;
}) {
  const Icon = ICON_MAP[name] ?? Folder;
  return <Icon className={className} />;
}
