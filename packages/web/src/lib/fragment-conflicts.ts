import type {
  Notification,
  NotificationResolveAction,
  ReviewContradictionPayload,
} from "memax-sdk";

/**
 * Fragment conflicts — a pending `review_contradiction` notification
 * seen from ONE of its two memories. The 记忆片段 list renders a strip
 * under every memory that is party to an unresolved contradiction, and
 * resolves the SAME notification the pulse 「等你」 deck does: there is
 * no separate conflict entity, so a verdict here clears both surfaces.
 */
export interface FragmentConflict {
  notificationId: string;
  /** Which side of the pair the memory being rendered is on. */
  side: "a" | "b";
  other: { id: string; title: string };
  reason: string;
  similarity: number;
  createdAt: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isContradictionPayload(
  value: unknown,
): value is ReviewContradictionPayload {
  return (
    isRecord(value) &&
    isRecord(value.memory_a) &&
    typeof value.memory_a.id === "string" &&
    isRecord(value.memory_b) &&
    typeof value.memory_b.id === "string"
  );
}

/**
 * Index pending contradictions by memory id. Each notification lands
 * under BOTH of its memories (a memory can also sit in several pairs).
 * Non-contradiction and non-pending rows are ignored, as are payloads
 * that lost a memory ref.
 */
export function buildFragmentConflictIndex(
  notifications: readonly Notification[] | undefined,
): Map<string, FragmentConflict[]> {
  const index = new Map<string, FragmentConflict[]>();
  if (!notifications) return index;
  for (const n of notifications) {
    if (n.kind !== "review_contradiction" || n.status !== "pending") continue;
    if (!isContradictionPayload(n.payload)) continue;
    const { memory_a, memory_b, reason, similarity } = n.payload;
    const push = (
      selfId: string,
      side: "a" | "b",
      other: { id: string; title: string },
    ) => {
      const list = index.get(selfId) ?? [];
      list.push({
        notificationId: n.id,
        side,
        other: { id: other.id, title: other.title ?? "" },
        reason: typeof reason === "string" ? reason : "",
        similarity: typeof similarity === "number" ? similarity : 0,
        createdAt: n.created_at,
      });
      index.set(selfId, list);
    };
    push(memory_a.id, "a", memory_b);
    push(memory_b.id, "b", memory_a);
  }
  return index;
}

export type FragmentConflictVerdict = "keep_this" | "keep_other" | "keep_both";

/**
 * Translate a side-relative verdict into the server's a/b action. The
 * server archives the losing memory on keep_a / keep_b and leaves both
 * on keep_both.
 */
export function fragmentConflictAction(
  conflict: Pick<FragmentConflict, "side">,
  verdict: FragmentConflictVerdict,
): NotificationResolveAction {
  if (verdict === "keep_both") return "keep_both";
  const keepA =
    verdict === "keep_this" ? conflict.side === "a" : conflict.side === "b";
  return keepA ? "keep_a" : "keep_b";
}
