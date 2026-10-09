// verifyReceiptChain: check receipts against their signed checkpoints,
// with no dependency beyond the platform's Web Crypto (SHA-256 and
// Ed25519; Node 20+ and current browsers). It implements the server's
// receipt chain, format 1 (packages/server/internal/receiptchain), byte for
// byte; the golden vectors in verify.test.ts are the server's too.
//
//   leaf_i  = SHA-256(0x00 ‖ canonical(receipt_i))
//   h_0     = SHA-256("memax.receipts.chain.v1" ‖ 0x00 ‖ space_id)
//   h_i     = SHA-256(h_(i-1) ‖ leaf_i)
//   root    = the RFC 6962 Merkle tree hash over a checkpoint's leaves
//
// The canonical form is the magic "memax.receipt.v1" and 21 fields, each a
// 4-byte big-endian length and its bytes (0xFFFFFFFF and no bytes for an
// absent value): id, seq, tenant_id, space_id, object_kind, object_id,
// object_ref, action, actor_kind, actor_id, agent, via, assurance,
// session_ref, source.kind, source.ref, reason_sha256, occurred_at and
// recorded_at (microseconds since the epoch), stream_id, stream_version.
// uuids are 16 bytes, integers 8, strings UTF-8. A receipt commits to its
// reason through reason_sha256 = SHA-256(salt ‖ reason); Forget removes the
// reason and the salt and keeps the commitment, so the chain still verifies.
//
// `memax verify-export` uses this on an export's receipts (in chain order)
// and checkpoints.
import type { Checkpoint, SigningKey } from "./types.js";

/** A receipt as sealed. Absent and null mean SQL NULL; "" is an empty string. */
export interface ChainReceipt {
  id: string;
  seq: number;
  tenant_id: string;
  space_id: string;
  object_kind: string;
  object_id: string;
  object_ref: string;
  action: string;
  actor_kind: string;
  actor_id?: string | null;
  agent?: string | null;
  via: string;
  assurance?: string | null;
  session_ref?: string | null;
  source?: { kind: string; ref: string } | null;
  /** The reason, while it exists (checked against reason_sha256). */
  reason?: string | null;
  /** The commitment's salt (hex), while the reason exists. */
  reason_salt?: string | null;
  /** SHA-256(salt ‖ reason), hex; kept after Forget. */
  reason_sha256?: string | null;
  /** RFC 3339 with the database's microseconds. */
  occurred_at: string;
  recorded_at: string;
  stream_id: string;
  stream_version: number;
}

export type ChainProblemKind =
  | "range"
  | "chain"
  | "merkle"
  | "signature"
  | "reason"
  | "redaction"
  | "missing";

export interface ChainProblem {
  kind: ChainProblemKind;
  /** The checkpoint's number, when the problem is a checkpoint's. */
  checkpoint?: number;
  receipt?: string;
  detail: string;
}

export interface ChainReport {
  /** Nothing that was checked failed. */
  ok: boolean;
  /** Receipts walked, and the chain hash after them (hex). */
  receipts: number;
  head: string;
  checkpoints: number;
  /** Checkpoints whose signature verified, and ones with none. */
  signed: number;
  unsigned: number;
  /** False when the platform has no Ed25519: signatures went unchecked. */
  signaturesChecked: boolean;
  problems: ChainProblem[];
}

/** The parts of Web Crypto this uses. */
export interface ChainCrypto {
  digest(algorithm: "SHA-256", data: Uint8Array): Promise<ArrayBuffer>;
  importKey(
    format: "raw",
    keyData: Uint8Array,
    algorithm: { name: "Ed25519" },
    extractable: boolean,
    usages: ["verify"],
  ): Promise<unknown>;
  verify(
    algorithm: { name: "Ed25519" },
    key: unknown,
    signature: Uint8Array,
    data: Uint8Array,
  ): Promise<boolean>;
}

export interface VerifyChainInput {
  spaceId: string;
  /** Every receipt of the space, in chain order (as the export lists them). */
  receipts: readonly ChainReceipt[];
  checkpoints: readonly Checkpoint[];
  /** The public keys to trust; pin them rather than taking the server's word. */
  keys: readonly SigningKey[];
  /** Defaults to globalThis.crypto.subtle. */
  crypto?: ChainCrypto;
}

const ABSENT = 0xffffffff;
const encoder = new TextEncoder();

class Writer {
  private parts: Uint8Array[] = [];
  raw(b: Uint8Array): void {
    this.parts.push(b);
  }
  bytes(b: Uint8Array): void {
    const n = new Uint8Array(4);
    new DataView(n.buffer).setUint32(0, b.length);
    this.parts.push(n, b);
  }
  none(): void {
    const n = new Uint8Array(4);
    new DataView(n.buffer).setUint32(0, ABSENT);
    this.parts.push(n);
  }
  str(s: string): void {
    this.bytes(encoder.encode(s));
  }
  optStr(s: string | null | undefined): void {
    if (s === null || s === undefined) this.none();
    else this.str(s);
  }
  id(u: string): void {
    this.bytes(uuidBytes(u));
  }
  optID(u: string | null | undefined): void {
    if (u === null || u === undefined) this.none();
    else this.id(u);
  }
  int(n: number | bigint): void {
    const b = new Uint8Array(8);
    new DataView(b.buffer).setBigInt64(0, BigInt(n));
    this.bytes(b);
  }
  done(): Uint8Array {
    return concat(...this.parts);
  }
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

function uuidBytes(u: string): Uint8Array {
  const hex = u.replace(/-/g, "");
  if (!/^[0-9a-fA-F]{32}$/.test(hex)) throw new Error(`not a uuid: ${u}`);
  return hexBytes(hex);
}

function hexBytes(hex: string): Uint8Array {
  if (hex.length % 2 !== 0 || !/^[0-9a-fA-F]*$/.test(hex)) {
    throw new Error(`not hex: ${hex}`);
  }
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(hex.slice(2 * i, 2 * i + 2), 16);
  }
  return out;
}

function toHex(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += x.toString(16).padStart(2, "0");
  return s;
}

function base64Bytes(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

/** An RFC 3339 timestamp as microseconds since the epoch. */
export function micros(ts: string): bigint {
  const m =
    /^(\d{4})-(\d{2})-(\d{2})[Tt ](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|z|[+-]\d{2}:\d{2})$/.exec(
      ts,
    );
  if (!m) throw new Error(`not an RFC 3339 timestamp: ${ts}`);
  const [, y, mo, d, h, mi, s, frac = "", zone] = m;
  const ms = Date.UTC(+y, +mo - 1, +d, +h, +mi, +s);
  let us = BigInt(ms) * 1000n + BigInt((frac + "000000").slice(0, 6));
  if (zone !== "Z" && zone !== "z") {
    const sign = zone[0] === "-" ? -1n : 1n;
    const offset =
      BigInt(+zone.slice(1, 3) * 60 + +zone.slice(4, 6)) * 60_000_000n;
    us -= sign * offset;
  }
  return us;
}

/** A receipt's canonical form, format 1. */
export function canonicalReceipt(r: ChainReceipt): Uint8Array {
  const w = new Writer();
  w.raw(encoder.encode("memax.receipt.v1"));
  w.id(r.id);
  w.int(r.seq);
  w.id(r.tenant_id);
  w.id(r.space_id);
  w.str(r.object_kind);
  w.id(r.object_id);
  w.str(r.object_ref);
  w.str(r.action);
  w.str(r.actor_kind);
  w.optID(r.actor_id);
  w.optStr(r.agent);
  w.str(r.via);
  w.optStr(r.assurance);
  w.optStr(r.session_ref);
  w.optStr(r.source ? r.source.kind : null);
  w.optStr(r.source ? r.source.ref : null);
  if (r.reason_sha256 === null || r.reason_sha256 === undefined) w.none();
  else w.bytes(hexBytes(r.reason_sha256));
  w.int(micros(r.occurred_at));
  w.int(micros(r.recorded_at));
  w.id(r.stream_id);
  w.int(r.stream_version);
  return w.done();
}

/** The bytes a checkpoint's signature is over. */
export function checkpointStatement(c: Checkpoint): Uint8Array {
  const w = new Writer();
  w.raw(encoder.encode("memax.checkpoint.v1"));
  w.id(c.space_id);
  w.id(c.tenant_id);
  w.int(c.number);
  w.int(c.position_from);
  w.int(c.position_to);
  w.id(c.first_receipt_id);
  w.id(c.last_receipt_id);
  w.int(c.last_seq);
  w.bytes(hexBytes(c.prev_sha256));
  w.bytes(hexBytes(c.chain_sha256));
  w.bytes(hexBytes(c.merkle_root));
  w.int(micros(c.sealed_at));
  w.optStr(c.key_id || null);
  return w.done();
}

function subtleOf(crypto?: ChainCrypto): ChainCrypto {
  const c =
    crypto ??
    (globalThis as { crypto?: { subtle?: ChainCrypto } }).crypto?.subtle;
  if (!c)
    throw new Error("verifyReceiptChain needs Web Crypto (crypto.subtle)");
  return c;
}

async function sha256(
  c: ChainCrypto,
  ...parts: Uint8Array[]
): Promise<Uint8Array> {
  return new Uint8Array(await c.digest("SHA-256", concat(...parts)));
}

/** A space's chain hash before its first receipt. */
export async function genesisHash(
  spaceId: string,
  crypto?: ChainCrypto,
): Promise<Uint8Array> {
  const c = subtleOf(crypto);
  return sha256(
    c,
    encoder.encode("memax.receipts.chain.v1"),
    new Uint8Array([0]),
    uuidBytes(spaceId),
  );
}

/** A receipt's leaf hash. */
export async function receiptLeaf(
  r: ChainReceipt,
  crypto?: ChainCrypto,
): Promise<Uint8Array> {
  return sha256(subtleOf(crypto), new Uint8Array([0]), canonicalReceipt(r));
}

/** The RFC 6962 Merkle tree hash over leaf hashes. */
export async function merkleRoot(
  leaves: readonly Uint8Array[],
  crypto?: ChainCrypto,
): Promise<Uint8Array> {
  const c = subtleOf(crypto);
  const root = async (ls: readonly Uint8Array[]): Promise<Uint8Array> => {
    if (ls.length === 0) return sha256(c);
    if (ls.length === 1) return ls[0];
    let k = 1;
    while (k * 2 < ls.length) k *= 2;
    return sha256(
      c,
      new Uint8Array([1]),
      await root(ls.slice(0, k)),
      await root(ls.slice(k)),
    );
  };
  return root(leaves);
}

/**
 * Recompute the space's chain from its first receipt and check every
 * checkpoint: its range, the chain hash before and after it, its Merkle
 * root and its signature (with `keys`), and every reason against its
 * commitment. A reason may be missing only where the object was forgotten,
 * or anywhere once the whole space was (a `forgot` receipt on the space).
 */
export async function verifyReceiptChain(
  input: VerifyChainInput,
): Promise<ChainReport> {
  const c = subtleOf(input.crypto);
  const problems: ChainProblem[] = [];
  const checkpoints = [...input.checkpoints].sort(
    (a, b) => a.number - b.number,
  );
  const report: ChainReport = {
    ok: false,
    receipts: 0,
    head: "",
    checkpoints: checkpoints.length,
    signed: 0,
    unsigned: 0,
    signaturesChecked: true,
    problems,
  };

  // Signatures.
  const keys = new Map(input.keys.map((k) => [k.key_id, k]));
  for (const [i, cp] of checkpoints.entries()) {
    const want = i === 0 ? 1 : checkpoints[i - 1].position_to + 1;
    if (
      cp.number !== i + 1 ||
      cp.position_from !== want ||
      cp.position_to < cp.position_from ||
      cp.space_id !== input.spaceId
    ) {
      problems.push({
        kind: "range",
        checkpoint: cp.number,
        detail: `checkpoint ${cp.number} covers ${cp.position_from}–${cp.position_to}; the one before it ends at ${want - 1}`,
      });
    }
    if (!cp.signed || !cp.key_id || !cp.signature) {
      report.unsigned++;
      continue;
    }
    const key = keys.get(cp.key_id);
    if (!key) {
      problems.push({
        kind: "signature",
        checkpoint: cp.number,
        detail: `signed by an unknown key: ${cp.key_id}`,
      });
      continue;
    }
    let ok: boolean;
    try {
      const k = await c.importKey(
        "raw",
        base64Bytes(key.public_key),
        { name: "Ed25519" },
        false,
        ["verify"],
      );
      ok = await c.verify(
        { name: "Ed25519" },
        k,
        base64Bytes(cp.signature),
        checkpointStatement(cp),
      );
    } catch {
      report.signaturesChecked = false;
      continue;
    }
    if (ok) report.signed++;
    else
      problems.push({
        kind: "signature",
        checkpoint: cp.number,
        detail: "the signature doesn't verify",
      });
  }

  // The chain.
  let head = await genesisHash(input.spaceId, c);
  let next = 0;
  let leaves: Uint8Array[] = [];
  const forgotten = new Set<string>();
  const redacted = new Map<string, string[]>();
  // A Forget of the whole space redacts every reason in it (the server's
  // verifier allows the same).
  let spaceForgotten = false;
  for (const [i, r] of input.receipts.entries()) {
    const position = i + 1;
    const leaf = await receiptLeaf(r, c);
    const before = head;
    head = await sha256(c, head, leaf);
    if (r.action === "forgot") forgotten.add(r.object_id);
    if (
      r.action === "forgot" &&
      r.object_kind === "space" &&
      r.object_id === input.spaceId
    ) {
      spaceForgotten = true;
    }
    const hasReason = r.reason !== null && r.reason !== undefined;
    const hasCommitment =
      r.reason_sha256 !== null && r.reason_sha256 !== undefined;
    if (hasReason) {
      if (!r.reason_salt || !hasCommitment) {
        problems.push({
          kind: "reason",
          receipt: r.id,
          detail: "it has a reason but no commitment",
        });
      } else {
        const got = await sha256(
          c,
          hexBytes(r.reason_salt),
          encoder.encode(r.reason as string),
        );
        if (!equal(got, hexBytes(r.reason_sha256 as string))) {
          problems.push({
            kind: "reason",
            receipt: r.id,
            detail: "its reason doesn't match the commitment sealed with it",
          });
        }
      }
    } else if (hasCommitment) {
      redacted.set(r.object_id, [...(redacted.get(r.object_id) ?? []), r.id]);
    }
    const cp = checkpoints[next];
    if (!cp || position < cp.position_from) continue;
    if (position === cp.position_from) {
      leaves = [];
      if (r.id !== cp.first_receipt_id) {
        problems.push({
          kind: "range",
          checkpoint: cp.number,
          receipt: r.id,
          detail: "the receipt at its first position isn't the one it sealed",
        });
      }
      if (toHex(before) !== cp.prev_sha256) {
        problems.push({
          kind: "chain",
          checkpoint: cp.number,
          detail: "the chain hash before its range differs from the signed one",
        });
      }
    }
    leaves.push(leaf);
    if (position === cp.position_to) {
      if (r.id !== cp.last_receipt_id || r.seq !== cp.last_seq) {
        problems.push({
          kind: "range",
          checkpoint: cp.number,
          receipt: r.id,
          detail: "the receipt at its last position isn't the one it sealed",
        });
      }
      if (toHex(await merkleRoot(leaves, c)) !== cp.merkle_root) {
        problems.push({
          kind: "merkle",
          checkpoint: cp.number,
          detail: "the Merkle root of its receipts differs from the signed one",
        });
      }
      if (toHex(head) !== cp.chain_sha256) {
        problems.push({
          kind: "chain",
          checkpoint: cp.number,
          detail: "the chain hash at its end differs from the signed one",
        });
        // Carry on from the signed hash: later checkpoints are judged on
        // their own receipts.
        head = hexBytes(cp.chain_sha256);
      }
      next++;
    }
  }
  for (; next < checkpoints.length; next++) {
    const cp = checkpoints[next];
    problems.push({
      kind: "missing",
      checkpoint: cp.number,
      detail: `it covers positions ${cp.position_from}–${cp.position_to}, but the chain has ${input.receipts.length} receipts`,
    });
  }
  for (const [object, receipts] of redacted) {
    if (forgotten.has(object) || spaceForgotten) continue;
    for (const id of receipts) {
      problems.push({
        kind: "redaction",
        receipt: id,
        detail: `its reason is gone, but nothing forgot ${object}`,
      });
    }
  }
  report.receipts = input.receipts.length;
  report.head = toHex(head);
  report.ok = problems.length === 0;
  return report;
}
