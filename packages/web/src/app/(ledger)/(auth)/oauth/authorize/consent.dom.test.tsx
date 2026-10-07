// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { MemaxError, type OAuthRequest } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider, type Locale } from "@/i18n";
import { demoConsentRequest } from "@/lib/v2/data/consent-demo";
import { ConsentScreen } from "./consent-screen";

// OAuthConsent: signing in first and coming back, the board's request,
// what each space says, the decisions the page sends as the person signed
// in (one space, Cancel as deny) and the URL it follows, "Not you?", and
// every ending.

const h = vi.hoisted(() => ({
  params: new URLSearchParams("request=req_1"),
  open: vi.fn(),
  decide: vi.fn(),
  release: vi.fn(),
  replace: vi.fn(),
  auth: { user: { id: "u1" } as { id: string } | null, loading: false },
}));
vi.mock("next/navigation", () => ({
  useSearchParams: () => h.params,
  useRouter: () => ({ replace: h.replace }),
}));
vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    auth: {
      openOAuthRequest: h.open,
      decideOAuthRequest: h.decide,
      releaseOAuthRequest: h.release,
    },
  }),
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => h.auth,
}));

function request(over: Partial<OAuthRequest> = {}): OAuthRequest {
  return { ...demoConsentRequest, request_id: "req_1", ...over };
}

const [project, team, personal] = demoConsentRequest.spaces;
let assign: ReturnType<typeof vi.fn>;

beforeEach(() => {
  assign = vi.fn();
  vi.stubGlobal("location", { ...window.location, assign });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  for (const fn of [h.open, h.decide, h.release, h.replace]) fn.mockReset();
  h.auth = { user: { id: "u1" }, loading: false };
  h.params = new URLSearchParams("request=req_1");
});

function renderScreen(locale: Locale = "en") {
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider initialLocale={locale}>
        <LedgerProvider locale={locale}>
          <ConsentScreen />
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

function lists() {
  const text = (list: HTMLElement) =>
    within(list)
      .getAllByRole("listitem")
      .map((li) => li.textContent);
  return {
    can: text(screen.getByRole("list", { name: /will be able to/ })),
    cannot: text(screen.getByRole("list", { name: "It won't be able to" })),
  };
}

describe("OAuthConsent", () => {
  it("signs in first, and comes back to the same request", () => {
    h.auth = { user: null, loading: false };
    renderScreen();
    expect(h.replace).toHaveBeenCalledWith(
      "/signin?next=%2Foauth%2Fauthorize%3Frequest%3Dreq_1",
    );
    expect(h.open).not.toHaveBeenCalled();
    cleanup();
    // While the session is still being read, nothing is decided.
    h.replace.mockReset();
    h.auth = { user: null, loading: true };
    renderScreen();
    expect(h.replace).not.toHaveBeenCalled();
  });

  it("shows the board's request: the agent, who is signed in, the spaces and what's true in the chosen one", async () => {
    h.open.mockResolvedValue(request());
    renderScreen();
    expect(
      await screen.findByRole("heading", {
        name: "Codex wants to connect to Memax",
      }),
    ).toBeTruthy();
    expect(h.open).toHaveBeenCalledWith("req_1");
    expect(screen.getByText(/Signed in as Ziyang/)).toBeTruthy();
    const radios = screen.getAllByRole("radio");
    expect(radios.map((r) => (r as HTMLInputElement).value)).toEqual([
      project!.id,
      team!.id,
      personal!.id,
    ]);
    expect((radios[0] as HTMLInputElement).checked).toBe(true);
    expect(screen.getByText("Project · 214 memories")).toBeTruthy();
    expect(screen.getByText("Team · 2 people")).toBeTruthy();
    expect(screen.getByText("Just you")).toBeTruthy();
    // Codex reads AGENTS.md; only the project compiles.
    expect(screen.getAllByText(/^compiles /).map((n) => n.textContent)).toEqual(
      ["compiles AGENTS.md"],
    );
    expect(lists()).toEqual({
      can: [
        "Read the Brief and kept memories",
        "Propose memories, which wait for you",
        "Ask you to decide at a fork",
      ],
      cannot: [
        "Keep anything without you",
        "Forget anything",
        "See your other spaces",
      ],
    });
    expect(
      screen.getByText(
        "You can allow Write, or disconnect Codex, any time in Agents.",
      ),
    ).toBeTruthy();
  });

  it("allows one space as the person signed in, and follows the URL the API answers", async () => {
    h.open.mockResolvedValue(request());
    h.decide.mockResolvedValue({
      redirect_to: "http://127.0.0.1:1455/callback?code=c1&state=s&iss=x",
    });
    renderScreen();
    const allow = await screen.findByRole("button", { name: "Allow Codex" });
    fireEvent.click(screen.getAllByRole("radio")[1]!);
    fireEvent.click(allow);
    // Sent once: Allow waits, Cancel can't be pressed meanwhile.
    expect(allow.getAttribute("aria-busy")).toBe("true");
    expect(
      (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith(
        "http://127.0.0.1:1455/callback?code=c1&state=s&iss=x",
      ),
    );
    expect(h.decide).toHaveBeenCalledTimes(1);
    expect(h.decide).toHaveBeenCalledWith("req_1", {
      decision: "approve",
      space_id: team!.id,
    });
  });

  it("cancels with deny, and the agent hears access_denied at its own URL", async () => {
    h.open.mockResolvedValue(request());
    h.decide.mockResolvedValue({
      redirect_to: "https://claude.example/cb?error=access_denied&state=s",
    });
    renderScreen();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith(
        "https://claude.example/cb?error=access_denied&state=s",
      ),
    );
    expect(h.decide).toHaveBeenCalledWith("req_1", { decision: "deny" });
  });

  it("never follows anything but an http(s) URL", async () => {
    h.open.mockResolvedValue(request());
    h.decide.mockResolvedValue({ redirect_to: "javascript:alert(1)" });
    renderScreen();
    fireEvent.click(await screen.findByRole("button", { name: "Allow Codex" }));
    expect((await screen.findByRole("alert")).textContent).toBe(
      "That didn't go through. Try again.",
    );
    expect(assign).not.toHaveBeenCalled();
  });

  it("says why an approval came back, and ends when the request did", async () => {
    h.open.mockResolvedValue(request());
    h.decide.mockRejectedValueOnce(new MemaxError("no", "consent_space", 422));
    renderScreen();
    fireEvent.click(await screen.findByRole("button", { name: "Allow Codex" }));
    expect((await screen.findByRole("alert")).textContent).toBe(
      "That space can't be connected from this account. Choose one of the spaces here.",
    );
    h.decide.mockRejectedValueOnce(
      new MemaxError("gone", "consent_request_expired", 410),
    );
    fireEvent.click(screen.getByRole("button", { name: "Allow Codex" }));
    expect(
      await screen.findByRole("heading", { name: "This request expired." }),
    ).toBeTruthy();
  });

  it("Not you? lets go of the request, signs out, and signs in again for it", async () => {
    h.open.mockResolvedValue(request());
    h.release.mockResolvedValue({ released: true });
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}"));
    vi.stubGlobal("fetch", fetchMock);
    renderScreen();
    fireEvent.click(await screen.findByRole("button", { name: "Not you?" }));
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith(
        "/signin?next=%2Foauth%2Fauthorize%3Frequest%3Dreq_1",
      ),
    );
    expect(h.release).toHaveBeenCalledWith("req_1");
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/logout", {
      method: "POST",
    });
  });

  it("says what is true where the agent only reads, and in a space still on V1", async () => {
    h.open.mockResolvedValue(
      request({
        spaces: [
          project!,
          {
            ...team!,
            autonomy: "read",
            ceiling: "propose",
            can: ["read_brief"],
            cannot: ["propose", "gate", "forget", "other_spaces"],
          },
          {
            ...personal!,
            on_v2: false,
            memories: 40,
            autonomy: undefined,
            ceiling: undefined,
            can: ["read_memories", "add", "gate"],
            cannot: ["forget", "other_spaces"],
          },
        ],
      }),
    );
    renderScreen();
    await screen.findByRole("button", { name: "Allow Codex" });
    fireEvent.click(screen.getAllByRole("radio")[1]!);
    expect(lists()).toEqual({
      can: ["Read the Brief and kept memories"],
      cannot: [
        "Propose memories",
        "Ask you to decide at a fork",
        "Forget anything",
        "See your other spaces",
      ],
    });
    expect(
      screen.getByText(
        "You can let Codex propose, or disconnect it, any time in Agents.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getAllByRole("radio")[2]!);
    expect(screen.getByText("Just you · still on V1")).toBeTruthy();
    expect(lists().can).toEqual([
      "Read the memories in it",
      "Add memories, which are kept as written",
      "Ask you to decide at a fork",
    ]);
    expect(
      screen.getByText("You can disconnect Codex any time in Agents."),
    ).toBeTruthy();
  });

  it("names an unknown client by its own words, cleaned, shortened where a line can't wrap", async () => {
    const name =
      "Acme Research Assistant for Very Long Enterprise Workflows <img src=x onerror=alert(1)>";
    h.open.mockResolvedValue(
      request({
        client_name: `‮${name}​\n`,
        agent_name: "acme-research",
        client_host: "agents.acme.example",
      }),
    );
    renderScreen();
    const heading = await screen.findByRole("heading", { level: 1 });
    // Text, never markup; no bidi or zero-width characters.
    expect(heading.textContent).toBe(`${name} wants to connect to Memax`);
    expect(document.querySelector("img")).toBeNull();
    const allow = screen.getByRole("button", { name: /^Allow / });
    expect(allow.textContent).toBe("Allow Acme Research Assistant for…");
    expect(allow.getAttribute("title")).toBe(`Allow ${name}`);
    expect(screen.getByText("from agents.acme.example")).toBeTruthy();
    // The fallback stamp: two letters from its name.
    expect(document.querySelector(".mx-stamp")?.textContent).toBe("AR");
  });

  it("says when the request expired, ended, can't be answered here, never came, or didn't load", async () => {
    const cases: [unknown, string][] = [
      [
        new MemaxError("expired", "consent_request_expired", 410),
        "This request expired.",
      ],
      [
        new MemaxError("gone", "consent_request_not_found", 404),
        "This request has ended.",
      ],
      [
        new MemaxError("cli", "consent_by_person_on_web", 403),
        "This request can't be answered from here.",
      ],
    ];
    for (const [err, title] of cases) {
      h.open.mockRejectedValueOnce(err);
      renderScreen();
      expect(await screen.findByRole("heading", { name: title })).toBeTruthy();
      expect(screen.queryByRole("button", { name: /Allow/ })).toBeNull();
      cleanup();
    }

    h.params = new URLSearchParams();
    renderScreen();
    expect(
      screen.getByRole("heading", { name: "This link has no request in it." }),
    ).toBeTruthy();
    cleanup();

    h.params = new URLSearchParams("request=req_1");
    h.open.mockRejectedValueOnce(new MemaxError("down", "network_error", 502));
    h.open.mockResolvedValueOnce(request());
    renderScreen();
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(
      await screen.findByRole("heading", {
        name: "Codex wants to connect to Memax",
      }),
    ).toBeTruthy();
  });

  it("opens a link from V1's page made before the move", async () => {
    h.params = new URLSearchParams("request_id=req_1&consent_token=old");
    h.open.mockResolvedValue(request());
    renderScreen();
    await screen.findByRole("button", { name: "Allow Codex" });
    expect(h.open).toHaveBeenCalledWith("req_1");
  });

  it("ends when the request's time runs out on the page", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    h.open.mockResolvedValue(request({ expires_in: 5 }));
    renderScreen();
    await screen.findByRole("button", { name: "Allow Codex" });
    await act(async () => {
      vi.advanceTimersByTime(5_000);
    });
    expect(
      screen.getByRole("heading", { name: "This request expired." }),
    ).toBeTruthy();
    vi.useRealTimers();
  });

  it("sends someone with no space to set one up, and still lets them cancel", async () => {
    h.open.mockResolvedValue(request({ spaces: [] }));
    h.decide.mockResolvedValue({
      redirect_to: "https://claude.example/cb?error=access_denied",
    });
    renderScreen();
    expect(
      await screen.findByRole("heading", {
        name: "You don't have a space yet.",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Set up Memax" }).getAttribute("href"),
    ).toBe("/setup");
    expect(screen.queryByRole("button", { name: /Allow/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(h.decide).toHaveBeenCalled());
  });

  it("reads in Chinese", async () => {
    h.open.mockResolvedValue(request());
    renderScreen("zh");
    expect(
      await screen.findByRole("heading", { name: "Codex 想连接到 Memax" }),
    ).toBeTruthy();
    expect(screen.getByText("项目 · 214 条记忆")).toBeTruthy();
    expect(screen.getByRole("button", { name: "允许 Codex" })).toBeTruthy();
  });
});
