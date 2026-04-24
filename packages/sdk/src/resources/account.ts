import type { DeleteAllDataResult } from "../types.js";
import type { RequestFn } from "../transport.js";

export class AccountResource {
  constructor(private readonly req: RequestFn) {}

  async deleteAllData(): Promise<DeleteAllDataResult> {
    return this.req("DELETE", "/v1/account/data");
  }
}
