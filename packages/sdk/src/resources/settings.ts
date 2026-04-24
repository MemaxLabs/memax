import type {
  PlanDefinition,
  Settings,
  SettingsUpdateInput,
  Usage,
  UsageWithLimits,
} from "../types.js";
import type { RequestFn } from "../transport.js";

export class SettingsResource {
  constructor(private readonly req: RequestFn) {}

  async get(): Promise<Settings> {
    return this.req("GET", "/v1/settings");
  }

  async update(patch: SettingsUpdateInput): Promise<Settings> {
    return this.req("PATCH", "/v1/settings", { body: patch });
  }

  /**
   * Returns current billing period usage. When the plan registry is active,
   * the response includes plan limits alongside the usage counters
   * (UsageWithLimits). The flat Usage fields are always present for
   * backward compatibility.
   */
  async usage(): Promise<UsageWithLimits | Usage> {
    return this.req("GET", "/v1/usage");
  }

  /** Returns all active plan definitions (public, no auth required). */
  async plans(): Promise<PlanDefinition[]> {
    return this.req("GET", "/v1/plans");
  }
}
