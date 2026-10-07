import { describe, expect, it, vi } from "vitest";
import { MemaxError, type Memax } from "memax-sdk";
import { exportFailureText } from "@/app/(ledger)/(app)/_places/memories/use-export";
import { en } from "@/i18n/locales/en";
import { toFailure } from "./command-error";
import { createSdkMemories } from "./sdk-memories";
import type { SpaceSummary } from "./types";

const space = { slug: "memax-v2", name: "memax-v2" } as SpaceSummary;

describe("Export as Markdown on the SDK source", () => {
  it("downloads the space's archive with the action's key", async () => {
    const bytes = new Uint8Array([0x50, 0x4b, 0x05, 0x06]);
    const exportSpace = vi.fn(async () => ({
      bytes,
      filename: "memax-memax-v2-2026-10-07.zip",
      receipt: "0199a1b2-c3d4-7e5f-8a9b-00000000040e",
      replayed: false,
    }));
    const client = {
      v2: { spaces: { export: exportSpace } },
    } as unknown as Pick<Memax, "v2">;
    const got = await createSdkMemories(client, () => undefined).exportSpace({
      space,
      idempotencyKey: "k-1",
    });
    expect(exportSpace).toHaveBeenCalledWith("memax-v2", {
      idempotencyKey: "k-1",
      signal: undefined,
    });
    expect(got.filename).toBe("memax-memax-v2-2026-10-07.zip");
    expect(got.receipt).toBe("0199a1b2-c3d4-7e5f-8a9b-00000000040e");
    expect(got.blob.type).toBe("application/zip");
    expect(new Uint8Array(await got.blob.arrayBuffer())).toEqual(bytes);
  });

  it("says why an export didn't go through", () => {
    const copy = en.ledger.app.memories.exportFailed;
    const text = (err: unknown) =>
      exportFailureText(copy, toFailure(err), "memax-v2");
    expect(
      text(
        new MemaxError("no", "refused", 403, {
          policy: { effect: "refuse", code: "export_by_person" },
        }),
      ),
    ).toBe(copy.refused);
    expect(
      text(new MemaxError("slow down", "rate_limited", 429, undefined, 20)),
    ).toBe(
      "You've exported several times in a row. Export again in 20 seconds.",
    );
    expect(text(new MemaxError("down", "network_error", 0))).toBe(
      copy.unreachable,
    );
    expect(text(new Error("odd"))).toBe("memax-v2 wasn't exported. Try again.");
  });
});
