import type {
  Board,
  BoardFeedbackVerdict,
  BoardSlot,
  BoardSlotAction,
  BoardSlotVersion,
  BoardStatus,
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
   * A slot's archived content versions, newest first (the live slot is
   * not included). Empty until a producer has replaced the card at
   * least once with different content.
   */
  async slotHistory(
    hubId: string,
    slotKey: string,
  ): Promise<{ versions: BoardSlotVersion[] }> {
    return this.req(
      "GET",
      `/v1/hubs/${hubId}/board/slots/${encodeURIComponent(slotKey)}/history`,
    );
  }

  /** One board (system or custom) with its slots. */
  async getBoard(hubId: string, boardId: string): Promise<BoardWithSlots> {
    return this.req("GET", `/v1/hubs/${hubId}/boards/${boardId}`);
  }

  /** Every board on the hub — system board first, then custom ones. */
  async listForHub(hubId: string): Promise<{ boards: Board[] }> {
    return this.req("GET", `/v1/hubs/${hubId}/boards`);
  }

  /**
   * Create a custom board from a standing instruction. The board
   * starts in the `cooking` state and flips to `active` when the next
   * dream run puts its first card on it.
   */
  async createBoard(
    hubId: string,
    input: { title: string; instruction: string },
  ): Promise<{ board: Board }> {
    return this.req("POST", `/v1/hubs/${hubId}/boards`, { body: input });
  }

  /**
   * Edit a custom board. Rewriting the instruction returns the board
   * to `cooking` — the old cards answered a different question.
   */
  async updateBoard(
    hubId: string,
    boardId: string,
    input: { title?: string; instruction?: string; status?: BoardStatus },
  ): Promise<{ board: Board }> {
    return this.req("PATCH", `/v1/hubs/${hubId}/boards/${boardId}`, {
      body: input,
    });
  }

  async deleteBoard(
    hubId: string,
    boardId: string,
  ): Promise<{ deleted: boolean }> {
    return this.req("DELETE", `/v1/hubs/${hubId}/boards/${boardId}`);
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
