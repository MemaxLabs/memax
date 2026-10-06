import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import {
  canonicalReceipt,
  checkpointStatement,
  genesisHash,
  receiptLeaf,
  verifyReceiptChain,
} from "../index.js";
import type { ChainReceipt, V2 } from "../index.js";
import { micros } from "./verify.js";

// The server's golden vectors (packages/server/internal/receiptchain,
// TestGoldenVectors): the same receipts, the same bytes.
const space = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c";
const tenant = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d";
const memory = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70";
const person = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71";
const agent = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72";
const salt = Buffer.from("0123456789abcdef").toString("hex");
const reason = "superseded by M-0219";

const goldenCanonical0 =
  "6d656d61782e726563656970742e7631000000100199a1b2c3d47e5f8a9b0c1d2e3f4a80000000080000000000000001" +
  "000000100199a1b2c3d47e5f8a9b0c1d2e3f4a5d000000100199a1b2c3d47e5f8a9b0c1d2e3f4a5c000000066d656d6f7279" +
  "000000100199a1b2c3d47e5f8a9b0c1d2e3f4a70000000064d2d303231390000000870726f706f736564000000056167656e74" +
  "000000100199a1b2c3d47e5f8a9b0c1d2e3f4a7200000005636f646578000000036d6370ffffffff0000000763782d37663361" +
  "0000000270720000000750522023323132ffffffff0000000800065d28a47ef8400000000800065d28a47efc28" +
  "000000100199a1b2c3d47e5f8a9b0c1d2e3f4a70000000080000000000000001";
const goldenLeaf0 =
  "57d7617baa4b718608bea22bea41a539279eb4f50fb4e759a659083889a14e74";
const goldenLeaf2 =
  "f002f2b4a242974230d7de1e6cad1a57f678830e70265b2048bde890d0da83de";
const goldenGenesis =
  "104de6f72da79e28eadd427e21f6432e1974ead72ade8a49f55979c64d9b18bf";
const goldenChain =
  "0a909173075b588caacd0bc31bccaa04f554102788e18fb3a3eb25aa91d57264";
const goldenRoot =
  "417c52cb77a8b934c73d57955637de9dd8d6793de97ffffc28cd0f97390eb224";
const goldenKeyID = "ed25519:fe812c12f3ab4ce6";
const goldenPublic =
  "ea4a6c63e29c520abef5507b132ec5f9954776aebebe7b92421eea691446d22c";
const goldenSignature =
  "f56ce345bdb05dfb184b7bf88d9d95b0a35f5ecb221e7650a2cec666cfa250a838da6ec0c5d5667ea2167b3816852f46556e8eb4f7395025e71f88eeb5fb3101";

const hex = (b: Uint8Array) => Buffer.from(b).toString("hex");
const b64 = (h: string) => Buffer.from(h, "hex").toString("base64");

function receipts(): ChainReceipt[] {
  return [
    {
      id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80",
      seq: 1,
      tenant_id: tenant,
      space_id: space,
      object_kind: "memory",
      object_id: memory,
      object_ref: "M-0219",
      action: "proposed",
      actor_kind: "agent",
      actor_id: agent,
      agent: "codex",
      via: "mcp",
      session_ref: "cx-7f3a",
      source: { kind: "pr", ref: "PR #212" },
      occurred_at: "2026-10-06T09:30:00.123456Z",
      recorded_at: "2026-10-06T09:30:00.124456Z",
      stream_id: memory,
      stream_version: 1,
    },
    {
      id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a81",
      seq: 2,
      tenant_id: tenant,
      space_id: space,
      object_kind: "memory",
      object_id: memory,
      object_ref: "M-0219",
      action: "kept",
      actor_kind: "person",
      actor_id: person,
      agent: "codex",
      via: "web",
      assurance: "human_web",
      // The same instant in another zone: only the instant counts.
      occurred_at: "2026-10-06T11:31:00.123456+02:00",
      recorded_at: "2026-10-06T09:31:00.123456Z",
      stream_id: memory,
      stream_version: 2,
    },
    {
      id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a82",
      seq: 3,
      tenant_id: tenant,
      space_id: space,
      object_kind: "memory",
      object_id: memory,
      object_ref: "M-0219",
      action: "edited",
      actor_kind: "person",
      actor_id: person,
      via: "web",
      reason,
      reason_salt: salt,
      reason_sha256: createHash("sha256")
        .update(Buffer.from(salt, "hex"))
        .update(reason)
        .digest("hex"),
      occurred_at: "2026-10-06T09:32:00.123456Z",
      recorded_at: "2026-10-06T09:32:00.123456Z",
      stream_id: memory,
      stream_version: 3,
    },
  ];
}

const checkpoint: V2.Checkpoint = {
  id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a90",
  space_id: space,
  tenant_id: tenant,
  number: 1,
  position_from: 1,
  position_to: 3,
  receipts: 3,
  first_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80",
  last_receipt_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a82",
  last_seq: 3,
  prev_sha256: goldenGenesis,
  chain_sha256: goldenChain,
  merkle_root: goldenRoot,
  format: 1,
  signed: true,
  key_id: goldenKeyID,
  signature: b64(goldenSignature),
  sealed_at: "2026-10-06T09:32:01.123456Z",
};

const keys: V2.SigningKey[] = [
  { key_id: goldenKeyID, algorithm: "ed25519", public_key: b64(goldenPublic) },
];

describe("the receipt chain, format 1", () => {
  it("encodes a receipt byte for byte as the server does", async () => {
    const rs = receipts();
    expect(hex(canonicalReceipt(rs[0]))).toBe(goldenCanonical0);
    expect(hex(await receiptLeaf(rs[0]))).toBe(goldenLeaf0);
    expect(hex(await receiptLeaf(rs[2]))).toBe(goldenLeaf2);
    expect(hex(await genesisHash(space))).toBe(goldenGenesis);
    expect(checkpointStatement(checkpoint).length).toBeGreaterThan(200);
  });

  it("reads timestamps to the microsecond, in any zone", () => {
    expect(micros("2026-10-06T09:30:00.123456Z")).toBe(1791279000123456n);
    expect(micros("2026-10-06T11:30:00.123456+02:00")).toBe(1791279000123456n);
    expect(micros("2026-10-06T09:30:00.1Z")).toBe(1791279000100000n);
    expect(micros("2026-10-06T09:30:00Z")).toBe(1791279000000000n);
    expect(() => micros("yesterday")).toThrow();
  });

  it("verifies a sealed chain and its signature", async () => {
    const report = await verifyReceiptChain({
      spaceId: space,
      receipts: receipts(),
      checkpoints: [checkpoint],
      keys,
    });
    expect(report.problems).toEqual([]);
    expect(report).toMatchObject({
      ok: true,
      receipts: 3,
      head: goldenChain,
      signed: 1,
      unsigned: 0,
      signaturesChecked: true,
    });
  });

  it("catches a changed, removed or reordered receipt", async () => {
    const changed = receipts();
    changed[1] = { ...changed[1], via: "cli" };
    const removed = receipts().slice(0, 2);
    const swapped = receipts();
    [swapped[0], swapped[1]] = [swapped[1], swapped[0]];
    const kinds = async (rs: ChainReceipt[]) =>
      (
        await verifyReceiptChain({
          spaceId: space,
          receipts: rs,
          checkpoints: [checkpoint],
          keys,
        })
      ).problems.map((p) => p.kind);
    expect(await kinds(changed)).toEqual(["merkle", "chain"]);
    expect(await kinds(removed)).toEqual(["missing"]);
    expect(await kinds(swapped)).toEqual(["range", "merkle", "chain"]);
  });

  it("checks reasons, and allows a redaction only after a forget", async () => {
    const altered = receipts();
    altered[2] = { ...altered[2], reason: "a different reason" };
    let report = await verifyReceiptChain({
      spaceId: space,
      receipts: altered,
      checkpoints: [checkpoint],
      keys,
    });
    expect(report.problems.map((p) => p.kind)).toEqual(["reason"]);

    // Forget removes the reason and its salt; the commitment stays, so the
    // chain still verifies, once the object has a forgot receipt.
    const redacted = receipts();
    redacted[2] = { ...redacted[2], reason: null, reason_salt: null };
    report = await verifyReceiptChain({
      spaceId: space,
      receipts: redacted,
      checkpoints: [checkpoint],
      keys,
    });
    expect(report.problems.map((p) => p.kind)).toEqual(["redaction"]);
    const forgot: ChainReceipt = {
      ...redacted[2],
      id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a83",
      seq: 4,
      action: "forgot",
      reason_sha256: null,
      stream_version: 4,
    };
    report = await verifyReceiptChain({
      spaceId: space,
      receipts: [...redacted, forgot],
      checkpoints: [checkpoint],
      keys,
    });
    expect(report.ok).toBe(true);
    expect(report.receipts).toBe(4);
  });

  it("rejects a forged checkpoint or an unknown key", async () => {
    const forged = { ...checkpoint, merkle_root: "00".repeat(32) };
    let report = await verifyReceiptChain({
      spaceId: space,
      receipts: receipts(),
      checkpoints: [forged],
      keys,
    });
    expect(report.problems.map((p) => p.kind)).toEqual(["signature", "merkle"]);
    report = await verifyReceiptChain({
      spaceId: space,
      receipts: receipts(),
      checkpoints: [checkpoint],
      keys: [],
    });
    expect(report.problems).toEqual([
      expect.objectContaining({ kind: "signature", checkpoint: 1 }),
    ]);
    // An unsigned checkpoint still chains, and is counted as unsigned.
    report = await verifyReceiptChain({
      spaceId: space,
      receipts: receipts(),
      checkpoints: [
        {
          ...checkpoint,
          signed: false,
          key_id: undefined,
          signature: undefined,
        },
      ],
      keys,
    });
    expect(report).toMatchObject({ ok: true, signed: 0, unsigned: 1 });
  });
});
