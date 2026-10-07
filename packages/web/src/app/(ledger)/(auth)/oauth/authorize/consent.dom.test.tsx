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
import { MemaxError, type OAuthConsentRequest } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider, type Locale } from "@/i18n";
import { demoConsentRequest } from "@/lib/v2/data/consent-demo";
import { ConsentScreen } from "./consent-screen";

// OAuthConsent: the board's request, what each space says, the forms the
// page posts (never above memax:propose, Cancel as `deny`, "Not you?" as
// `switch`, each with the request's token), and every ending.

const h = vi.hoisted(() => ({
  params: new URLSearchParams("request_id=req_1&consent_token=tok_1"),
  load: vi.fn(),
  user: null as { id: string } | null,
}));
vi.mock("next/navigation", () => ({
  useSearchParams: () => h.params,
}));
vi.mock("@/lib/memax-client", () => ({
  getPublicMemaxClient: () => ({ auth: { getOAuthConsentRequest: h.load } }),
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: h.user }),
}));

const SUBMIT = "https://api.memax.test/oauth/authorize/consent";

function request(over: Partial<OAuthConsentRequest> = {}): OAuthConsentRequest {
  return {
    ...demoConsentRequest,
    session_id: "req_1",
    csrf_token: "tok_1",
    submit_url: SUBMIT,
    ...over,
  };
}

const [project, team, personal] = demoConsentRequest.hubs;

let submitted: HTMLFormElement[] = [];
const keep = (e: Event) => {
  e.preventDefault();
  submitted.push(e.target as HTMLFormElement);
};

beforeEach(() => {
  submitted = [];
  document.addEventListener("submit", keep);
});

afterEach(() => {
  cleanup();
  document.removeEventListener("submit", keep);
  h.load.mockReset();
  h.user = null;
  h.params = new URLSearchParams("request_id=req_1&consent_token=tok_1");
  vi.unstubAllGlobals();
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

function fields(form: HTMLFormElement) {
  return [...new FormData(form).entries()].map(([k, v]) => `${k}=${v}`);
}

function lists() {
  const can = screen.getByRole("list", { name: /will be able to/ });
  const cannot = screen.getByRole("list", { name: "It won't be able to" });
  const text = (list: HTMLElement) =>
    within(list)
      .getAllByRole("listitem")
      .map((li) => li.textContent);
  return { can: text(can), cannot: text(cannot) };
}

describe("OAuthConsent", () => {
  it("shows the board's request: the agent, who is signed in, the spaces and what's true in the chosen one", async () => {
    h.load.mockResolvedValue(request());
    renderScreen();
    expect(
      await screen.findByRole("heading", {
        name: "Codex wants to connect to Memax",
      }),
    ).toBeTruthy();
    expect(h.load).toHaveBeenCalledWith("req_1", "tok_1");
    expect(screen.getByText(/Signed in as Ziyang/)).toBeTruthy();
    const radios = screen.getAllByRole("radio");
    expect(radios.map((r) => (r as HTMLInputElement).value)).toEqual([
      project.id,
      team.id,
      personal.id,
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
    expect(screen.getByRole("button", { name: "Cancel" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Allow Codex" })).toBeTruthy();
    expect(
      screen.getByText(
        "You can allow Write, or disconnect Codex, any time in Agents.",
      ),
    ).toBeTruthy();
  });

  it("posts the chosen space with read and propose, never write, and the request's token", async () => {
    // A server that offered write would still get no more than propose.
    h.load.mockResolvedValue(
      request({ consent_scope: "memax:read memax:propose memax:write" }),
    );
    renderScreen();
    const allow = await screen.findByRole("button", { name: "Allow Codex" });
    fireEvent.click(screen.getAllByRole("radio")[1]!);
    const form = (allow as HTMLButtonElement).form!;
    expect(form.getAttribute("action")).toBe(SUBMIT);
    expect(form.getAttribute("method")).toBe("post");
    expect(fields(form)).toEqual([
      "session_id=req_1",
      "csrf_token=tok_1",
      "ui=v2",
      "permission=memax:read",
      "permission=memax:propose",
      `hub_id=${team.id}`,
    ]);
    expect((allow as HTMLButtonElement).name).toBe("decision");
    expect((allow as HTMLButtonElement).value).toBe("approve");
    fireEvent.click(allow);
    expect(submitted).toEqual([form]);
    // Sent once: Allow waits, Cancel can't be pressed meanwhile.
    expect(allow.getAttribute("aria-busy")).toBe("true");
    expect(
      (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("cancels with deny, so the agent hears access_denied", async () => {
    h.load.mockResolvedValue(request());
    renderScreen();
    const cancel = (await screen.findByRole("button", {
      name: "Cancel",
    })) as HTMLButtonElement;
    const form = cancel.form!;
    expect(form.id).toBe("consent-cancel");
    expect(form.getAttribute("action")).toBe(SUBMIT);
    expect(fields(form)).toEqual([
      "session_id=req_1",
      "csrf_token=tok_1",
      "ui=v2",
      "decision=deny",
    ]);
    fireEvent.click(cancel);
    expect(submitted).toEqual([form]);
  });

  it("says what is true where the agent only reads, and in a space still on V1", async () => {
    h.load.mockResolvedValue(
      request({
        hubs: [
          project!,
          {
            ...team!,
            autonomy: "read",
            can: ["read_brief"],
            cannot: ["propose", "gate", "forget", "other_spaces"],
          },
          {
            ...personal!,
            on_v2: false,
            memory_count: 40,
            kept_count: undefined,
            autonomy: undefined,
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
        "You can let Codex propose or write, or disconnect it, any time in Agents.",
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
    h.load.mockResolvedValue(
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
    expect(
      screen.getByText(
        "You can allow Write, or disconnect Acme Research Assistant for…, any time in Agents.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("from agents.acme.example")).toBeTruthy();
    // The fallback stamp: two letters from its name.
    expect(document.querySelector(".mx-stamp")?.textContent).toBe("AR");
  });

  it("signs this browser out, then starts the request over as someone else", async () => {
    h.load.mockResolvedValue(request());
    h.user = { id: "u1" };
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}"));
    vi.stubGlobal("fetch", fetchMock);
    const submit = vi
      .spyOn(HTMLFormElement.prototype, "submit")
      .mockImplementation(function (this: HTMLFormElement) {
        submitted.push(this);
      });
    renderScreen();
    const notYou = (await screen.findByRole("button", {
      name: "Not you?",
    })) as HTMLButtonElement;
    expect(notYou.form!.id).toBe("consent-switch");
    expect(fields(notYou.form!)).toEqual([
      "session_id=req_1",
      "csrf_token=tok_1",
      "ui=v2",
      "decision=switch",
    ]);
    fireEvent.click(notYou);
    await waitFor(() => expect(submitted).toEqual([notYou.form]));
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/logout", {
      method: "POST",
    });
    submit.mockRestore();
  });

  it("says when the request expired, ended, never came, or didn't load", async () => {
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
        new MemaxError("token", "invalid_consent_token", 403),
        "This request has ended.",
      ],
    ];
    for (const [err, title] of cases) {
      h.load.mockRejectedValueOnce(err);
      renderScreen();
      expect(await screen.findByRole("heading", { name: title })).toBeTruthy();
      expect(screen.queryByRole("button", { name: /Allow/ })).toBeNull();
      cleanup();
    }

    h.params = new URLSearchParams("request_id=req_1");
    renderScreen();
    expect(
      screen.getByRole("heading", { name: "This link has no request in it." }),
    ).toBeTruthy();
    cleanup();

    // The API sent a post back: no request is read.
    h.params = new URLSearchParams("ended=expired");
    renderScreen();
    expect(
      screen.getByRole("heading", { name: "This request expired." }),
    ).toBeTruthy();
    cleanup();
    expect(h.load).toHaveBeenCalledTimes(3);

    h.params = new URLSearchParams("request_id=req_1&consent_token=tok_1");
    h.load.mockRejectedValueOnce(new MemaxError("down", "network_error", 502));
    h.load.mockResolvedValueOnce(request());
    renderScreen();
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(
      await screen.findByRole("heading", {
        name: "Codex wants to connect to Memax",
      }),
    ).toBeTruthy();
  });

  it("ends when the request's time runs out on the page", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    h.load.mockResolvedValue(request({ expires_in: 5 }));
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
    h.load.mockResolvedValue(request({ hubs: [] }));
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
    expect(
      (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
        .form!.id,
    ).toBe("consent-cancel");
  });

  it("says why a post came back", async () => {
    h.params = new URLSearchParams(
      "request_id=req_1&consent_token=tok_1&error=space",
    );
    h.load.mockResolvedValue(request());
    renderScreen();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "That space can't be connected from this account. Choose one of the spaces here.",
    );
  });

  it("reads in Chinese", async () => {
    h.load.mockResolvedValue(request());
    renderScreen("zh");
    expect(
      await screen.findByRole("heading", { name: "Codex 想连接到 Memax" }),
    ).toBeTruthy();
    expect(screen.getByText("项目 · 214 条记忆")).toBeTruthy();
    expect(screen.getByRole("button", { name: "允许 Codex" })).toBeTruthy();
  });
});
