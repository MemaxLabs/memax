import type {
  Persona,
  PersonaApplyRequest,
  PersonaApplyResult,
} from "../types.js";
import type { RequestFn } from "../transport.js";

/**
 * Personas — identity objects derived from synced identity configs
 * (SOUL.md, IDENTITY.md, persona files). Applying one writes it into a
 * target agent's identity config through the normal config-sync
 * machinery; the device picks it up on its next `memax agents sync`.
 */
export class PersonasResource {
  constructor(private readonly req: RequestFn) {}

  async list(): Promise<{ personas: Persona[] }> {
    return this.req("GET", "/v1/personas");
  }

  async apply(
    id: string,
    input: PersonaApplyRequest,
  ): Promise<PersonaApplyResult> {
    return this.req("POST", `/v1/personas/${id}/apply`, {
      body: {
        target_agent: input.target_agent,
        target_scope: input.target_scope,
      },
    });
  }

  /** Remove the derived persona row. The source config file is untouched. */
  async delete(id: string): Promise<{ deleted: boolean }> {
    return this.req("DELETE", `/v1/personas/${id}`);
  }
}
