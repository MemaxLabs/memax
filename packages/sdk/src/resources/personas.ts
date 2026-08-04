import type {
  Persona,
  PersonaRestoreResult,
  PersonaRevision,
} from "../types.js";
import type { RequestFn } from "../transport.js";

/**
 * Personas — identity objects derived from synced identity configs
 * (SOUL.md, IDENTITY.md, persona files). Personas are bound to the memax
 * agent (chat) via session persona_id / the chat_default_persona_id
 * setting — see the chats and settings resources.
 */
export class PersonasResource {
  constructor(private readonly req: RequestFn) {}

  async list(): Promise<{ personas: Persona[] }> {
    return this.req("GET", "/v1/personas");
  }

  /** Version history, newest first. Content omitted — use getRevision. */
  async listRevisions(id: string): Promise<{ revisions: PersonaRevision[] }> {
    return this.req("GET", `/v1/personas/${id}/revisions`);
  }

  /** One revision including its full content. */
  async getRevision(id: string, version: number): Promise<PersonaRevision> {
    return this.req("GET", `/v1/personas/${id}/revisions/${version}`);
  }

  /**
   * Write an old revision back into the persona's source config. History
   * is append-only: a restore creates a new head version.
   */
  async restoreRevision(
    id: string,
    version: number,
  ): Promise<PersonaRestoreResult> {
    return this.req("POST", `/v1/personas/${id}/revisions/${version}/restore`);
  }

  /** Remove the derived persona row. The source config file is untouched. */
  async delete(id: string): Promise<{ deleted: boolean }> {
    return this.req("DELETE", `/v1/personas/${id}`);
  }
}
