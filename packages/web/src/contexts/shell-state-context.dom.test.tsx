// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { ShellStateProvider, useShellState } from "./shell-state-context";

const STORAGE_KEY = "memax_shell_state";

function wrapper({ children }: { children: React.ReactNode }) {
  return <ShellStateProvider>{children}</ShellStateProvider>;
}

describe("ShellStateProvider", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("first render uses SSR-safe defaults; isHydrated flips after mount", async () => {
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden).toEqual({
      brain: false,
      memories: false,
      agents: false,
      pulse: false,
    });
  });

  it("hydrates persisted state on mount, after first render", async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        secondaryHidden: {
          brain: false,
          memories: true,
          agents: false,
          pulse: false,
        },
      }),
    );
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden.memories).toBe(true);
  });

  it("falls back to defaults when localStorage is corrupt JSON", async () => {
    localStorage.setItem(STORAGE_KEY, "not valid json {{{");
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden.memories).toBe(false);
  });

  it("tolerates a legacy payload still carrying the retired `collapsed` key", async () => {
    // Payloads written before 2026-08 persisted a rail-collapse
    // preference. The key must be ignored on read and dropped on the
    // re-canonicalization write, not crash parsing or leak through.
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        collapsed: false,
        secondaryHidden: {
          brain: false,
          memories: true,
          agents: false,
          pulse: false,
        },
      }),
    );
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden.memories).toBe(true);
    expect("collapsed" in result.current).toBe(false);
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}");
    expect("collapsed" in stored).toBe(false);
  });

  it("rejects non-boolean per-tab values and falls back to default for that tab", async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        secondaryHidden: {
          brain: "yes", // invalid
          memories: 1, // invalid
          agents: false,
          pulse: true,
        },
      }),
    );
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden).toEqual({
      brain: false, // string "yes" rejected → default false
      memories: false, // number 1 rejected → default false
      agents: false,
      pulse: true, // valid boolean preserved
    });
    // Re-canonicalized: persisted JSON should now hold the cleaned shape.
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}");
    expect(stored.secondaryHidden).toEqual({
      brain: false,
      memories: false,
      agents: false,
      pulse: true,
    });
  });

  it("drops unknown tab keys from persisted secondaryHidden", async () => {
    // Imagine a renamed-tab scenario: an older client persisted "feed"
    // which no longer exists. The provider should drop the key without
    // throwing.
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        secondaryHidden: { feed: true, memories: true },
      }),
    );
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden).toEqual({
      brain: false,
      memories: true,
      agents: false,
      pulse: false,
    });
    expect(Object.keys(result.current.secondaryHidden)).not.toContain("feed");
  });

  it("rejects an array passed as secondaryHidden", async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ secondaryHidden: [true, false] }),
    );
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    expect(result.current.secondaryHidden).toEqual({
      brain: false,
      memories: false,
      agents: false,
      pulse: false,
    });
  });

  it("toggleSecondary flips one tab without mutating others", async () => {
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    act(() => result.current.toggleSecondary("memories"));
    expect(result.current.secondaryHidden).toEqual({
      brain: false,
      memories: true,
      agents: false,
      pulse: false,
    });
  });

  it("setSecondaryHidden is a no-op when value is unchanged", async () => {
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    const beforeRef = result.current.secondaryHidden;
    act(() => result.current.setSecondaryHidden("memories", false));
    expect(result.current.secondaryHidden).toBe(beforeRef);
  });

  it("barScrollHidden is session state — never persisted", async () => {
    const { result } = renderHook(() => useShellState(), { wrapper });
    await waitFor(() => expect(result.current.isHydrated).toBe(true));
    act(() => result.current.setBarScrollHidden(true));
    expect(result.current.barScrollHidden).toBe(true);
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}");
    expect("barScrollHidden" in stored).toBe(false);
  });

  it("useShellState throws a clear error outside the provider", () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useShellState())).toThrow(
      /useShellState must be used inside a <ShellStateProvider>/,
    );
    errSpy.mockRestore();
  });
});
