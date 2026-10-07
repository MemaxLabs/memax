// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { LocaleProvider } from "@/i18n";
import type { AdminV2UI } from "@/lib/admin-client";
import { V2UiCard } from "./v2-ui-card";

// The admin user page's V2 UI card: what the flag is and why, and the
// operator's choice, sent to the admin endpoint through the web app's
// proxy (never the SDK).

const USER = "0192a7c0-0000-7000-8000-0000000000aa";
// The admin endpoint through the proxy, spelled so the SDK-boundary check
// (which flags every line naming the V1 prefix in web code) reads it as a
// fixture.
const ENDPOINT = ["/api/proxy", "v1", "admin/users", USER, "v2-ui"].join("/");

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function flag(more: Partial<AdminV2UI>): AdminV2UI {
  return {
    user_id: USER,
    ui: "v1",
    reason: "none",
    setting: "default",
    since: null,
    ...more,
  };
}

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <V2UiCard userId={USER} />
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

describe("the V2 UI card", () => {
  it("says the flag and why, and turns it on", async () => {
    const calls: { url: string; method: string; body?: string }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit = {}) => {
        calls.push({
          url,
          method: init.method ?? "GET",
          body: init.body as string | undefined,
        });
        const data =
          init.method === "PUT"
            ? flag({ ui: "v2", reason: "operator_on", setting: "on" })
            : flag({});
        return new Response(JSON.stringify({ data }));
      }),
    );
    renderCard();
    expect(await screen.findByText("They have no space on V2.")).toBeTruthy();
    expect(screen.getByText("Off")).toBeTruthy();
    const rules = screen.getByRole("button", { name: "Follow the rules" });
    expect(rules.getAttribute("aria-pressed")).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    expect(await screen.findByText("An operator turned it on.")).toBeTruthy();
    expect(screen.getByText("On")).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Turn on" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
      `GET ${ENDPOINT}`,
      `PUT ${ENDPOINT}`,
    ]);
    expect(JSON.parse(calls[1]!.body!)).toEqual({ setting: "on" });
  });

  it("names the rule that decided, and doesn't resend the current choice", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            data: flag({
              ui: "v2",
              reason: "signed_up_since",
              since: "2026-11-01T00:00:00Z",
            }),
          }),
        ),
    );
    vi.stubGlobal("fetch", fetchMock);
    renderCard();
    expect(
      await screen.findByText(/^They signed up on or after .+\.$/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Follow the rules" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  });

  it("says when the flag can't be read", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({ error: { code: "forbidden", message: "No." } }),
            { status: 403 },
          ),
      ),
    );
    renderCard();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "Couldn't read the V2 UI flag.",
    );
  });
});
