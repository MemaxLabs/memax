import { notFound } from "next/navigation";
import { isSpaceSlug } from "@/lib/ui-gate";

// /[space]/…: a space's places. Space slugs live at the root (plan
// §6.3), so a reserved word (a V1 route such as /home/today, the V2
// areas) is never a space: 404. Whether the person can open the space
// is the frame's to say, from their spaces list.
export default async function SpaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ space: string }>;
}) {
  const { space } = await params;
  if (!isSpaceSlug(space)) notFound();
  return children;
}
