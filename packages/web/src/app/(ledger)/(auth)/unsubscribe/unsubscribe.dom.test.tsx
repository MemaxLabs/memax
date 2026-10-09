// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { UnsubscribeScreen } from "./unsubscribe-screen";

// The morning email's unsubscribe link: one press turns it off, with no
// sign-in; opening the page alone changes nothing.

const h = vi.hoisted(() => ({
  params: new URLSearchParams("token=tok_1"),
  unsubscribe: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  useSearchParams: () => h.params,
}));
vi.mock("@/lib/memax-client", () => ({
  getPublicMemaxClient: () => ({
    v2: { dream: { unsubscribe: h.unsubscribe } },
  }),
}));

afterEach(() => {
  cleanup();
  h.unsubscribe.mockReset();
  h.params = new URLSearchParams("token=tok_1");
});

function renderScreen() {
  render(
    <LocaleProvider>
      <LedgerProvider locale="en">
        <UnsubscribeScreen />
      </LedgerProvider>
    </LocaleProvider>,
  );
}

describe("unsubscribing from the morning email", () => {
  it("turns it off with the link's token, on one press", async () => {
    h.unsubscribe.mockResolvedValue({ unsubscribed: true });
    renderScreen();
    expect(h.unsubscribe).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Turn the morning email off" }),
    );
    expect(
      await screen.findByRole("heading", { name: "The morning email is off" }),
    ).toBeTruthy();
    expect(h.unsubscribe).toHaveBeenCalledWith("tok_1");
    expect(
      screen.getByRole("link", { name: "Open Settings" }).getAttribute("href"),
    ).toBe("/settings/notifications");
  });

  it("says when it didn't go through, or the link has no token", async () => {
    h.unsubscribe.mockRejectedValue(new Error("network"));
    renderScreen();
    fireEvent.click(
      screen.getByRole("button", { name: "Turn the morning email off" }),
    );
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /didn't go through/,
    );
    cleanup();
    h.params = new URLSearchParams();
    renderScreen();
    expect(screen.queryByRole("button", { name: /Turn/ })).toBeNull();
    expect(screen.getByText(/This link has no token/)).toBeTruthy();
  });
});
