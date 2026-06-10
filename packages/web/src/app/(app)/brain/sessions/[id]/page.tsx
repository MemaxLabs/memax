/**
 * /brain/sessions/[id] — active chat-session detail route.
 *
 * Server component shell: synchronously resolves the route param
 * and hands the id down to the client `<ChatBrainView>`. Going
 * through a server component keeps the route-level surface light
 * (no JS shipped just to read params) and makes "the URL is the
 * source of truth for active session" explicit at the boundary.
 *
 * The sessions LIST lives at /brain (mobile = full page, desktop
 * = left rail). Selecting a session navigates here so the route
 * segment is resolved on first render — eliminates the deep-link
 * race the prior `?s=` query-param flow had (no async router
 * push racing the next render's hook closures).
 */

import { ChatBrainView } from "@/components/features/chat/chat-brain-view";

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function ChatSessionPage({ params }: PageProps) {
  const { id } = await params;
  return <ChatBrainView routeSessionId={id} />;
}
