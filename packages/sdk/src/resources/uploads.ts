import type { UploadIntent, UploadPurpose } from "../types.js";
import type { RequestFn } from "../transport.js";

export class UploadsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * Request a presigned upload target. `purpose` is required; the server uses
   * it to apply the right content-type allowlist and size cap:
   *  - `memory_attachment` — user-picked files (PDFs, images, text). Per-plan cap.
   *  - `agent_session`     — CLI-synced agent-session artifacts. Flat 200 MiB cap.
   */
  async create(input: {
    filename: string;
    content_type: string;
    size_bytes: number;
    purpose: UploadPurpose;
  }): Promise<UploadIntent> {
    return this.req("POST", "/v1/uploads", { body: input });
  }
}
