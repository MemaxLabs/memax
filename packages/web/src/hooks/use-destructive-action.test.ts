// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useDestructiveAction } from "./use-destructive-action";

describe("useDestructiveAction.run boolean contract", () => {
  it("treats action resolving to void as success (backwards compat)", async () => {
    const { result } = renderHook(() => useDestructiveAction<"forget">());
    const onSuccess = vi.fn();

    act(() => result.current.start("forget"));
    expect(result.current.isConfirming("forget")).toBe(true);

    let runResult: boolean | undefined;
    await act(async () => {
      runResult = await result.current.run(
        "forget",
        async () => {
          // Returns undefined (void), same shape as a mutateAsync for a
          // server call that resolves without a structured payload.
        },
        { onSuccess },
      );
    });

    expect(runResult).toBe(true);
    expect(onSuccess).toHaveBeenCalledTimes(1);
    // closeOnSuccess defaults to true — confirmation card should close.
    expect(result.current.isConfirming("forget")).toBe(false);
  });

  it("treats action resolving to true as success", async () => {
    const { result } = renderHook(() => useDestructiveAction<"forget">());
    const onSuccess = vi.fn();

    act(() => result.current.start("forget"));

    let runResult: boolean | undefined;
    await act(async () => {
      runResult = await result.current.run("forget", async () => true, {
        onSuccess,
      });
    });

    expect(runResult).toBe(true);
    expect(onSuccess).toHaveBeenCalledTimes(1);
    expect(result.current.isConfirming("forget")).toBe(false);
  });

  it("treats action resolving to false as expected failure — confirmation stays mounted", async () => {
    // Regression guard for the destructive-action contract that supports
    // hooks like useMemoryForget, which return false for partial-skip /
    // permission-denied / single-flight-reject without raising.
    const { result } = renderHook(() => useDestructiveAction<"forget">());
    const onSuccess = vi.fn();
    const onError = vi.fn();

    act(() => result.current.start("forget"));
    expect(result.current.isConfirming("forget")).toBe(true);

    let runResult: boolean | undefined;
    await act(async () => {
      runResult = await result.current.run("forget", async () => false, {
        onSuccess,
        onError,
      });
    });

    // Wrapper reports failure.
    expect(runResult).toBe(false);
    // onSuccess must NOT fire — same contract as the throw branch.
    expect(onSuccess).not.toHaveBeenCalled();
    // onError is for thrown errors, not boolean-false — must NOT fire.
    expect(onError).not.toHaveBeenCalled();
    // Confirmation stays mounted so the user can retry or cancel.
    expect(result.current.isConfirming("forget")).toBe(true);
    // Running key is released regardless.
    expect(result.current.isRunning("forget")).toBe(false);
  });

  it("keeps confirmingKey mounted when action throws", async () => {
    // Existing behavior locked in alongside the new boolean branch.
    const { result } = renderHook(() => useDestructiveAction<"forget">());
    const onSuccess = vi.fn();
    const onError = vi.fn();

    act(() => result.current.start("forget"));

    let runResult: boolean | undefined;
    await act(async () => {
      runResult = await result.current.run(
        "forget",
        async () => {
          throw new Error("boom");
        },
        { onSuccess, onError },
      );
    });

    expect(runResult).toBe(false);
    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(1);
    expect(result.current.isConfirming("forget")).toBe(true);
  });

  it("prevents re-entry while an action is running", async () => {
    const { result } = renderHook(() => useDestructiveAction<"forget">());

    act(() => result.current.start("forget"));

    // First call starts and hangs on a deferred promise.
    let resolveAction: (value: boolean) => void;
    const pending = new Promise<boolean>((resolve) => {
      resolveAction = resolve;
    });

    let firstResult: Promise<boolean>;
    act(() => {
      firstResult = result.current.run("forget", () => pending);
    });

    // Second call during the running window must return false immediately.
    let secondResult: boolean | undefined;
    await act(async () => {
      secondResult = await result.current.run("forget", async () => true);
    });
    expect(secondResult).toBe(false);

    // Resolve the first call and let it settle.
    await act(async () => {
      resolveAction!(true);
      await firstResult!;
    });

    // After the first call resolves, the confirmation closes (it returned
    // true), so subsequent runs can proceed normally.
    expect(result.current.isConfirming("forget")).toBe(false);
  });
});
