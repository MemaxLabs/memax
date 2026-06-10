import type { Notification } from "memax-sdk";
import { isNeedsActionKind } from "@/lib/notification-kinds";

export function countPendingReviewNotifications(
  notifications: Pick<Notification, "kind" | "status">[] | undefined,
): number {
  return (notifications ?? []).filter(
    (notification) =>
      notification.status === "pending" && isNeedsActionKind(notification.kind),
  ).length;
}
