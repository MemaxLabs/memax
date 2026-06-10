// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { ErrorFallback } from "./error-fallback";

// Mocks: the i18n provider gives us deterministic copy, the
// reporter spies on what we ship to PostHog, and next/link stays
// as-is (jsdom-compatible).
vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      errors: {
        title: "Something went wrong",
        description: "desc",
        tryAgain: "Try again",
        backToHome: "Back to home",
        backToSignIn: "Back to sign in",
        showDetails: "Show error details",
        adminHint: "admin hint",
        digestLabel: "Digest",
      },
    },
  }),
}));

const reportMock = vi.fn();
vi.mock("@/lib/report-render-error", () => ({
  reportRenderError: (...args: unknown[]) => reportMock(...args),
}));

afterEach(() => {
  cleanup();
  reportMock.mockReset();
});

function makeError(overrides: Partial<Error & { digest?: string }> = {}) {
  const e = new Error(overrides.message ?? "boom") as Error & {
    digest?: string;
  };
  if (overrides.digest !== undefined) e.digest = overrides.digest;
  return e;
}

describe("ErrorFallback", () => {
  it("renders title + description + digest row when digest is set", () => {
    render(
      <ErrorFallback
        error={makeError({ digest: "abc123" })}
        reset={vi.fn()}
        boundary="app"
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Something went wrong" }),
    ).toBeTruthy();
    // description and digest live in distinct <p> elements; scope
    // the query to the role="alert" container so the dev-only
    // <pre> stack (which can quote the whole DOM during failure
    // formatting) doesn't match the same text.
    const alert = screen.getByRole("alert");
    expect(alert.textContent).toContain("desc");
    expect(alert.textContent).toContain("Digest: abc123");
  });

  it("omits the digest row when digest is absent", () => {
    render(
      <ErrorFallback error={makeError()} reset={vi.fn()} boundary="app" />,
    );
    const alert = screen.getByRole("alert");
    expect(alert.textContent).not.toMatch(/Digest:\s/);
  });

  it("calls unstable_retry on primary CTA when provided", () => {
    const reset = vi.fn();
    const retry = vi.fn();
    render(
      <ErrorFallback
        error={makeError()}
        reset={reset}
        unstable_retry={retry}
        boundary="app"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(retry).toHaveBeenCalledTimes(1);
    expect(reset).not.toHaveBeenCalled();
  });

  it("falls back to reset when unstable_retry is not provided", () => {
    const reset = vi.fn();
    render(<ErrorFallback error={makeError()} reset={reset} boundary="app" />);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(reset).toHaveBeenCalledTimes(1);
  });

  it("reports the error to telemetry on mount with the right boundary", () => {
    render(
      <ErrorFallback
        error={makeError({ digest: "xyz" })}
        reset={vi.fn()}
        boundary="admin"
      />,
    );
    expect(reportMock).toHaveBeenCalledTimes(1);
    const [err, ctx] = reportMock.mock.calls[0];
    expect((err as Error).message).toBe("boom");
    expect(ctx).toMatchObject({ boundary: "admin", digest: "xyz" });
  });

  it("renders the optional hint when passed", () => {
    render(
      <ErrorFallback
        error={makeError()}
        reset={vi.fn()}
        boundary="admin"
        hint="admin hint"
      />,
    );
    expect(screen.getByText("admin hint")).toBeTruthy();
  });

  it("uses backToSignIn label when homeLabelKey is set", () => {
    render(
      <ErrorFallback
        error={makeError()}
        reset={vi.fn()}
        boundary="auth"
        homeLabelKey="backToSignIn"
        homeHref="/login"
      />,
    );
    expect(screen.getByRole("link", { name: "Back to sign in" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Back to home" })).toBeNull();
  });

  it("shows dev-only details pane with the stack", () => {
    // jsdom's default NODE_ENV is "test", which is not "production"
    // — so the details block renders.
    render(
      <ErrorFallback error={makeError()} reset={vi.fn()} boundary="app" />,
    );
    expect(screen.getByText("Show error details")).toBeTruthy();
  });

  it("exposes role=alert for assistive tech", () => {
    render(
      <ErrorFallback error={makeError()} reset={vi.fn()} boundary="app" />,
    );
    const alert = screen.getByRole("alert");
    expect(alert).toBeTruthy();
    expect(alert.getAttribute("aria-live")).toBe("assertive");
  });
});
