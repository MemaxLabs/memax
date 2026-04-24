import type { InviteAcceptResult, InviteDetails } from "../types.js";
import type { RequestFn } from "../transport.js";

export class InvitesResource {
  constructor(private readonly req: RequestFn) {}

  async get(token: string): Promise<InviteDetails> {
    return this.req("GET", `/v1/invites/${token}`);
  }

  async accept(token: string): Promise<InviteAcceptResult> {
    return this.req("POST", `/v1/invites/${token}/accept`, { body: {} });
  }
}
