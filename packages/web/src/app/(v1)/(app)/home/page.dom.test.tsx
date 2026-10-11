// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import HomePage from "./page";

// V1's /home, the entry resolver: the active hub's memories for a V1
// person, and V2's landing for a person who sees V2 (their flag turned on
// while they were in V1, so the hint arrived with this load's profile).

const h = vi.hoisted(() => ({
  auth: {
    user: null as { id: string } | null,
    loading: true,
    ui: null as "v1" | "v2" | null,
  },
  replace: vi.fn(),
  assign: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: h.replace, push: vi.fn() }),
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => h.auth,
  useActiveHub: () => ({
    activeHub: { hub: { id: "hub-1", slug: "acme", hub_type: "team" } },
  }),
}));

const location = window.location;

beforeEach(() => {
  h.auth = { user: { id: "u-1" }, loading: false, ui: "v1" };
  h.replace.mockReset();
  h.assign.mockReset();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { ...location, replace: h.assign },
  });
});
afterEach(() => {
  cleanup();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: location,
  });
});

describe("V1's home", () => {
  it("opens the active hub's memories for a person on V1", () => {
    render(<HomePage />);
    expect(h.replace).toHaveBeenCalledWith("/h/acme/memories");
    expect(h.assign).not.toHaveBeenCalled();
  });

  it("starts a person who sees V2 at V2's landing, in one full load", () => {
    h.auth = { user: { id: "u-1" }, loading: false, ui: "v2" };
    render(<HomePage />);
    expect(h.assign).toHaveBeenCalledWith("/signin");
    expect(h.replace).not.toHaveBeenCalled();
  });

  it("waits for the profile, and treats an unknown UI as V1", () => {
    h.auth = { user: null, loading: true, ui: null };
    const { rerender } = render(<HomePage />);
    expect(h.replace).not.toHaveBeenCalled();
    expect(h.assign).not.toHaveBeenCalled();
    h.auth = { user: { id: "u-1" }, loading: false, ui: null };
    rerender(<HomePage />);
    expect(h.replace).toHaveBeenCalledWith("/h/acme/memories");
    expect(h.assign).not.toHaveBeenCalled();
  });
});
