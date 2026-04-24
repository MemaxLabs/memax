import type {
  Hub,
  HubDetailResult,
  HubInvite,
  HubInviteeInput,
  HubMember,
  HubOwnershipTransfer,
  HubRole,
  HubSummary,
  HubUpdateParams,
  HubWithRole,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class HubsResource {
  constructor(private readonly req: RequestFn) {}

  async list(): Promise<HubWithRole[]> {
    return this.req("GET", "/v1/hubs");
  }

  async create(name: string, slug?: string): Promise<Hub> {
    return this.req("POST", "/v1/hubs", { body: { name, slug } });
  }

  async checkSlug(
    slug: string,
  ): Promise<{ available: boolean; reason?: string }> {
    return this.req("GET", "/v1/hubs/check-slug", { query: { slug } });
  }

  async get(id: string): Promise<HubDetailResult> {
    return this.req("GET", `/v1/hubs/${id}`);
  }

  async summary(id: string): Promise<HubSummary> {
    return this.req("GET", `/v1/hubs/${id}/summary`);
  }

  async markVisit(id: string): Promise<{ status: string; hub_id: string }> {
    return this.req("POST", `/v1/hubs/${id}/visit`, { body: {} });
  }

  async update(id: string, params: HubUpdateParams): Promise<Hub> {
    return this.req("PATCH", `/v1/hubs/${id}`, { body: params });
  }

  async removeMember(
    id: string,
    userId: string,
  ): Promise<{ status: string; user_id: string }> {
    return this.req("DELETE", `/v1/hubs/${id}/members/${userId}`);
  }

  async updateMemberRole(
    id: string,
    userId: string,
    role: Exclude<HubMember["role"], "owner">,
  ): Promise<{ status: string; user_id: string; role: HubMember["role"] }> {
    return this.req("PATCH", `/v1/hubs/${id}/members/${userId}`, {
      body: { role },
    });
  }

  async leave(id: string): Promise<{ status: string; hub_id: string }> {
    return this.req("POST", `/v1/hubs/${id}/leave`, { body: {} });
  }

  async delete(id: string): Promise<{ deleted: boolean; hub_id: string }> {
    return this.req("DELETE", `/v1/hubs/${id}`);
  }

  /**
   * Creates a hub invite. When `invitee` is provided and the email
   * or user id resolves to an existing account, the server addresses
   * the invite to that user and fires a hub_invite notification into
   * their inbox. Unknown emails fall back to a link-only invite so
   * the admin can still forward the URL manually. Omitting `invitee`
   * keeps the legacy copy-link flow.
   */
  async createInvite(
    id: string,
    params: { role?: HubRole; invitee?: HubInviteeInput } = {},
  ): Promise<HubInvite> {
    const body: { role: string; invitee?: HubInviteeInput } = {
      role: params.role ?? "contributor",
    };
    if (params.invitee) {
      body.invitee = params.invitee;
    }
    return this.req("POST", `/v1/hubs/${id}/invites`, { body });
  }

  async listInvites(id: string): Promise<HubInvite[]> {
    return this.req("GET", `/v1/hubs/${id}/invites`);
  }

  async revokeInvite(
    id: string,
    inviteId: string,
  ): Promise<{ status: string; invite_id: string }> {
    return this.req("DELETE", `/v1/hubs/${id}/invites/${inviteId}`);
  }

  async regenerateInvite(id: string, inviteId: string): Promise<HubInvite> {
    return this.req("POST", `/v1/hubs/${id}/invites/${inviteId}/regenerate`, {
      body: {},
    });
  }

  async resendInvite(
    id: string,
    inviteId: string,
  ): Promise<{ status: string; email_enqueued_at: string }> {
    return this.req("POST", `/v1/hubs/${id}/invites/${inviteId}/resend`, {
      body: {},
    });
  }

  async createOwnershipTransfer(
    id: string,
    targetUserId: string,
  ): Promise<HubOwnershipTransfer> {
    return this.req("POST", `/v1/hubs/${id}/ownership-transfer`, {
      body: { target_user_id: targetUserId },
    });
  }

  async acceptOwnershipTransfer(
    id: string,
    transferId: string,
  ): Promise<HubOwnershipTransfer> {
    return this.req(
      "POST",
      `/v1/hubs/${id}/ownership-transfer/${transferId}/accept`,
      { body: {} },
    );
  }

  async cancelOwnershipTransfer(
    id: string,
    transferId: string,
  ): Promise<{ status: string; transfer_id: string }> {
    return this.req(
      "POST",
      `/v1/hubs/${id}/ownership-transfer/${transferId}/cancel`,
      { body: {} },
    );
  }
}
