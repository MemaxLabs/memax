import type {
  BoardFeedbackVerdict,
  BoardSlot,
  BoardSlotAction,
  BoardWithSlots,
} from "../types.js";
import type { RequestFn } from "../transport.js";

/**
 * Boards — per-hub pulse boards (plan 25). A board is a fixed surface
 * of named slots that producers refresh with replace semantics; cards
 * carry memory citations as receipts. Boards are hub-scoped: access is
 * hub membership, and every route lives under /v1/hubs/{hubId}/board.
 */
export class BoardsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * The hub's system board plus its occupied slots. Created lazily on
   * first access; an empty `slots` array just means dreams haven't
   * produced cards yet.
   */
  async getForHub(hubId: string): Promise<BoardWithSlots> {
    return this.req("GET", `/v1/hubs/${hubId}/board`);
  }

  /**
   * Transition a card out of its live state. `ack` keeps it as a
   * resolved receipt, `dismiss` greys it out, `feedback` records a
   * per-member 准/不准 verdict (required; latest wins) then resolves.
   * Idempotent: resolving a card another member already settled
   * returns 200 with the current slot — and feedback verdicts still
   * record on settled cards.
   */
  async resolveSlot(
    hubId: string,
    slotKey: string,
    action: BoardSlotAction,
    verdict?: BoardFeedbackVerdict,
    /** For action "choose" (decision gates): the chosen option id. */
    choice?: string,
  ): Promise<{ slot: BoardSlot }> {
    return this.req(
      "POST",
      `/v1/hubs/${hubId}/board/slots/${encodeURIComponent(slotKey)}/resolve`,
      { body: { action, verdict, choice } },
    );
  }

  /**
   * Create a decision gate (等你 card): the user gets a board card +
   * needs-action ping; their choice is written back into hub memory
   * so the requesting agent can recall it. 409 gate_limit when the
   * board already has 3 open gates.
   */
  async requestDecision(
    hubId: string,
    input: {
      question: string;
      options: string[];
      context?: string;
      source_agent?: string;
    },
  ): Promise<{ slot: BoardSlot }> {
    return this.req("POST", `/v1/hubs/${hubId}/board/decision-gate`, {
      body: input,
    });
  }
}
