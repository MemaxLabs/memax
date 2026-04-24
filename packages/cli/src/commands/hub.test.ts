import { describe, expect, it } from "vitest";
import type { HubInvite } from "memax-sdk";
import {
  deriveInviteURL,
  extractInviteToken,
  inviteDisplayID,
  resolveInviteReference,
} from "./hub.js";

describe("inviteDisplayID", () => {
  it("shortens invite IDs by default", () => {
    expect(inviteDisplayID("12345678-1234-1234-1234-123456789abc")).toBe(
      "12345678",
    );
  });

  it("shows full invite IDs in verbose mode", () => {
    expect(inviteDisplayID("12345678-1234-1234-1234-123456789abc", true)).toBe(
      "12345678-1234-1234-1234-123456789abc",
    );
  });
});

describe("deriveInviteURL", () => {
  it("maps localhost API to local web app", () => {
    expect(deriveInviteURL("tok", "http://localhost:8080")).toBe(
      "http://localhost:3000/invite/tok",
    );
  });

  it("strips api prefix for production", () => {
    expect(deriveInviteURL("tok", "https://api.memax.app")).toBe(
      "https://memax.app/invite/tok",
    );
  });

  it("maps staging-api to staging-app", () => {
    expect(deriveInviteURL("tok", "https://staging-api.memaxlabs.com")).toBe(
      "https://staging-app.memaxlabs.com/invite/tok",
    );
  });

  it("maps api- prefix to app- prefix", () => {
    expect(deriveInviteURL("tok", "https://api-staging.memaxlabs.com")).toBe(
      "https://app-staging.memaxlabs.com/invite/tok",
    );
  });
});

describe("extractInviteToken", () => {
  it("returns raw token unchanged", () => {
    expect(extractInviteToken("abc123")).toBe("abc123");
  });

  it("extracts token from invite URL", () => {
    expect(extractInviteToken("https://memax.app/invite/abc123")).toBe(
      "abc123",
    );
  });
});

describe("resolveInviteReference", () => {
  const invites: HubInvite[] = [
    {
      id: "12345678-1234-1234-1234-123456789abc",
      hub_id: "h1",
      token: "tok1",
      invited_by: "u1",
      role: "contributor",
      expires_at: "",
      created_at: "",
    },
    {
      id: "abcdef12-1234-1234-1234-123456789abc",
      hub_id: "h1",
      token: "tok2",
      invited_by: "u1",
      role: "viewer",
      expires_at: "",
      created_at: "",
    },
  ];

  it("resolves exact invite IDs", () => {
    expect(
      resolveInviteReference(invites, "12345678-1234-1234-1234-123456789abc")
        .id,
    ).toBe("12345678-1234-1234-1234-123456789abc");
  });

  it("resolves unique invite ID prefixes", () => {
    expect(resolveInviteReference(invites, "abcdef12").id).toBe(
      "abcdef12-1234-1234-1234-123456789abc",
    );
  });
});
