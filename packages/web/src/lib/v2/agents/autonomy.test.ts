import { describe, expect, it } from "vitest";
import type { AgentConnectionView } from "../data/agents";
import {
  autonomyReducer,
  hasUnsent,
  initialAutonomy,
  isRaising,
  unavailableLevels,
  type AutonomyState,
} from "./autonomy";

function agent(over: Partial<AgentConnectionView> = {}): AgentConnectionView {
  return {
    id: "c1",
    agent: "codex",
    name: "Codex",
    surface: "cloud",
    state: "active",
    credential: { kind: "oauth_grant", active: true },
    maxAutonomy: "write",
    mine: true,
    clientId: null,
    spaces: [],
    reads7d: null,
    writes7d: 0,
    lastSeenAt: null,
    connectedAt: "2026-09-02T09:30:00-07:00",
    connectedBy: "you",
    ...over,
  };
}

describe("unavailableLevels", () => {
  it("offers every level for your own OAuth agent", () => {
    expect(unavailableLevels(agent(), "propose", "owner")).toEqual({});
  });

  it("greys Write for an API key's agent, which proposes at most", () => {
    const key = agent({
      credential: { kind: "api_key", active: true },
      maxAutonomy: "propose",
    });
    expect(unavailableLevels(key, "read", "owner")).toEqual({
      write: "keyMaxPropose",
    });
    expect(unavailableLevels(key, "propose", "member")).toEqual({
      write: "keyMaxPropose",
    });
  });

  it("greys raising past a read-only credential", () => {
    const readOnly = agent({ maxAutonomy: "read" });
    expect(unavailableLevels(readOnly, "read", "owner")).toEqual({
      propose: "readOnlyCredential",
      write: "readOnlyCredential",
    });
  });

  it("lets an owner lower someone else's agent, never raise it", () => {
    const theirs = agent({ mine: false });
    expect(unavailableLevels(theirs, "propose", "owner")).toEqual({
      write: "notYoursRaise",
    });
    expect(unavailableLevels(theirs, "propose", "member")).toEqual({
      read: "notYours",
      write: "notYours",
    });
  });

  it("lets a viewer only lower their own agent", () => {
    expect(unavailableLevels(agent(), "propose", "viewer")).toEqual({
      write: "viewer",
    });
    expect(unavailableLevels(agent(), "read", "viewer")).toEqual({
      propose: "viewer",
      write: "viewer",
    });
  });

  it("offers nothing on a disconnected agent or a revoked credential", () => {
    expect(
      unavailableLevels(agent({ state: "disconnected" }), "read", "owner"),
    ).toEqual({
      read: "disconnected",
      propose: "disconnected",
      write: "disconnected",
    });
    expect(
      unavailableLevels(
        agent({ credential: { kind: "api_key", active: false } }),
        "propose",
        "owner",
      ),
    ).toEqual({ read: "revoked", propose: "revoked", write: "revoked" });
  });
});

function run(
  state: AutonomyState,
  ...events: Parameters<typeof autonomyReducer>[1][]
) {
  return events.reduce(autonomyReducer, state);
}

describe("autonomyReducer", () => {
  it("shows a lower at once and settles it", () => {
    let s = run(initialAutonomy("write"), { type: "choose", to: "propose" });
    expect(s.shown).toBe("propose");
    expect(hasUnsent(s)).toBe(true);
    expect(isRaising(s)).toBe(false);
    s = run(s, { type: "send", key: "k1" });
    expect(s.inflight).toEqual({ to: "propose", key: "k1", kind: "lower" });
    s = run(s, { type: "settled", key: "k1", autonomy: "propose" });
    expect(s).toEqual({
      confirmed: "propose",
      shown: "propose",
      inflight: null,
    });
  });

  it("rolls a refused raise back to what the server has", () => {
    let s = run(
      initialAutonomy("read"),
      { type: "choose", to: "propose" },
      { type: "send", key: "k1" },
    );
    expect(s.inflight?.kind).toBe("raise");
    expect(isRaising(s)).toBe(true);
    s = run(s, { type: "rolledBack", key: "k1" });
    expect(s).toEqual({ confirmed: "read", shown: "read", inflight: null });
    expect(hasUnsent(s)).toBe(false);
  });

  it("sends only the latest choice, one command at a time", () => {
    // Arrowing Read → Propose → Write before the pause ends: one command.
    let s = run(
      initialAutonomy("read"),
      { type: "choose", to: "propose" },
      { type: "choose", to: "write" },
      { type: "send", key: "k1" },
    );
    expect(s.inflight).toEqual({ to: "write", key: "k1", kind: "raise" });
    // A choice while it's on its way waits, and isn't sent twice.
    s = run(s, { type: "choose", to: "read" }, { type: "send", key: "k2" });
    expect(s.inflight?.key).toBe("k1");
    s = run(s, { type: "settled", key: "k1", autonomy: "write" });
    expect(s).toMatchObject({ confirmed: "write", shown: "read" });
    expect(hasUnsent(s)).toBe(true);
    s = run(s, { type: "send", key: "k2" });
    expect(s.inflight).toEqual({ to: "read", key: "k2", kind: "lower" });
  });

  it("drops a later choice when the command before it is refused", () => {
    const s = run(
      initialAutonomy("read"),
      { type: "choose", to: "write" },
      { type: "send", key: "k1" },
      { type: "choose", to: "propose" },
      { type: "rolledBack", key: "k1" },
    );
    expect(s).toEqual({ confirmed: "read", shown: "read", inflight: null });
  });

  it("ignores answers to commands it didn't send", () => {
    const s = run(
      initialAutonomy("read"),
      { type: "choose", to: "propose" },
      { type: "send", key: "k1" },
      { type: "settled", key: "other", autonomy: "write" },
      { type: "rolledBack", key: "other" },
    );
    expect(s.inflight?.key).toBe("k1");
    expect(s.shown).toBe("propose");
  });

  it("follows the server's data when nothing is on its way", () => {
    let s = run(initialAutonomy("read"), {
      type: "sync",
      confirmed: "propose",
    });
    expect(s).toEqual({
      confirmed: "propose",
      shown: "propose",
      inflight: null,
    });
    s = run(
      s,
      { type: "choose", to: "write" },
      { type: "send", key: "k1" },
      { type: "sync", confirmed: "read" },
    );
    expect(s.confirmed).toBe("propose");
  });
});
