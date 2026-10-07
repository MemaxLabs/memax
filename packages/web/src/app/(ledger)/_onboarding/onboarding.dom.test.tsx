// @vitest-environment jsdom
import type { ReactNode } from "react";
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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import { DEMO_IMPORT_ID } from "@/lib/v2/data/demo-imports-data";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { CleanupScreen } from "./cleanup";
import { CliAuthScreen } from "./cli-auth";
import { CompileDoneScreen } from "./compile-done";
import { ConnectScreen } from "./connect";
import { FirstRunScreen } from "./first-run";
import { OnboardingFrame } from "./frame";
import { SignInScreen } from "./sign-in";
import { SignInCallbackScreen } from "./sign-in-callback";

// The first session's screens on the demo dataset (the boards' data):
// SignIn and its callback, CliAuth, Connect, FirstRun, Cleanup and
// CompileDone, with the router, the session and the public client
// stubbed.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  params: new URLSearchParams(),
  pathname: "/",
  push: vi.fn(),
  replace: vi.fn(),
  user: null as { id: string; name: string; email: string } | null,
  login: vi.fn(),
  completeLogin: vi.fn(async () => true),
  token: null as string | null,
  publicClient: {} as Record<string, unknown>,
  client: {} as Record<string, unknown>,
}));

vi.mock("@/lib/v2/data/demo-source", async (load) => {
  const actual = await load<typeof import("@/lib/v2/data/demo-source")>();
  return {
    ...actual,
    get demoSource() {
      return h.source ?? actual.demoSource;
    },
  };
});
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    user: h.user,
    loading: false,
    login: h.login,
    completeLogin: h.completeLogin,
  }),
  getAccessToken: () => h.token,
}));
vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => h.client,
  getPublicMemaxClient: () => h.publicClient,
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: h.push, replace: h.replace, refresh: vi.fn() }),
  usePathname: () => h.pathname,
  useSearchParams: () => h.params,
}));

beforeEach(() => {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  h.params = new URLSearchParams();
  h.pathname = "/";
  h.push = vi.fn();
  h.replace = vi.fn();
  h.user = null;
  h.login = vi.fn();
  h.completeLogin = vi.fn(async () => true);
  h.token = null;
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function at(pathname: string, query = "") {
  h.pathname = pathname;
  h.params = new URLSearchParams(query);
}

function renderWith(node: ReactNode, { frame = true } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          {frame ? <OnboardingFrame mode="demo">{node}</OnboardingFrame> : node}
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

describe("SignIn", () => {
  it("offers GitHub, Google and email, sending the code back to this app", () => {
    at("/signin", "next=/device?code=WQRT-4821");
    renderWith(<SignInScreen />, { frame: false });
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "The context layer you own.",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    );
    expect(h.login).toHaveBeenCalledWith(
      "http://localhost:3000/signin/callback?next=%2Fdevice%3Fcode%3DWQRT-4821",
      "github",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Continue with Google" }),
    );
    expect(h.login).toHaveBeenLastCalledWith(
      expect.stringContaining("/signin/callback"),
      "google",
    );
    // Passkeys come later, and say so.
    const passkey = screen.getByRole("button", { name: "Use a passkey" });
    expect(passkey.getAttribute("aria-disabled")).toBe("true");
    expect(screen.getByText(/after signing in/).textContent).toContain(
      "/device",
    );
  });

  it("sends the email code with this app's callback, so verifying hands back a redirect", async () => {
    const requestEmailOtp = vi.fn(async () => ({
      status: "sent",
      expires_in: 600,
      email: "zz@memax.app",
      cooldown: 30,
    }));
    const verifyEmailOtp = vi.fn(async () => ({
      status: "ok",
      code: "c",
      redirect: "http://localhost:3000/signin/callback?code=c",
      user_id: "u",
    }));
    h.publicClient = { auth: { requestEmailOtp, verifyEmailOtp } };
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    at("/signin");
    renderWith(<SignInScreen />, { frame: false });
    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "not an email" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a sign-in code" }),
    );
    expect(
      await screen.findByText("Enter an email address like you@company.com."),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "ZZ@memax.app" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Email me a sign-in code" }),
    );
    await screen.findByText(/We sent a 6-digit code to zz@memax.app/);
    expect(requestEmailOtp).toHaveBeenCalledWith({
      email: "ZZ@memax.app",
      redirect_uri: expect.stringContaining("/signin/callback"),
    });
    fireEvent.change(screen.getByLabelText("Code"), {
      target: { value: "123456" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() =>
      expect(verifyEmailOtp).toHaveBeenCalledWith({
        email: "zz@memax.app",
        code: "123456",
      }),
    );
    // Never tokens in the browser from here: the callback trades the code.
    expect(h.completeLogin).not.toHaveBeenCalled();
  });

  it("goes on to `next` when already signed in", async () => {
    h.user = { id: "u", name: "Ziyang", email: "zz@memax.app" };
    at("/signin", "next=/device?code=WQRT-4821");
    renderWith(<SignInScreen />, { frame: false });
    await waitFor(() =>
      expect(h.replace).toHaveBeenCalledWith("/device?code=WQRT-4821"),
    );
  });

  it("lands a new person on FirstRun", async () => {
    h.user = { id: "u", name: "Ziyang", email: "zz@memax.app" };
    h.client = {
      v2: {
        spaces: {
          list: vi.fn(async () => ({
            items: [
              {
                id: "p",
                tenant_id: "t",
                slug: "personal",
                name: "Personal",
                kind: "personal",
                role: "owner",
              },
            ],
          })),
        },
        imports: { list: vi.fn(async () => ({ items: [], has_more: false })) },
      },
    };
    at("/signin");
    renderWith(<SignInScreen />, { frame: false });
    await waitFor(() =>
      expect(h.replace).toHaveBeenCalledWith("/setup/import"),
    );
  });

  it("signs in again when asked to, even with a session", () => {
    h.user = { id: "u", name: "Ziyang", email: "zz@memax.app" };
    at("/signin", "next=/memax-v2/agents&again=1");
    renderWith(<SignInScreen />, { frame: false });
    expect(h.replace).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    );
    expect(h.login).toHaveBeenCalled();
  });
});

describe("the sign-in callback", () => {
  it("trades the code for the web app's session, then goes on", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ data: { access_token: "a", refresh_token: "r" } }),
        ),
    );
    vi.stubGlobal("fetch", fetchMock);
    at("/signin/callback", "code=one-time&next=/memax-v2/today");
    renderWith(<SignInCallbackScreen />, { frame: false });
    await waitFor(() =>
      expect(h.replace).toHaveBeenCalledWith("/memax-v2/today"),
    );
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/exchange",
      expect.objectContaining({ method: "POST" }),
    );
    expect(h.completeLogin).toHaveBeenCalledWith("a", "r");
  });

  it("says when it didn't finish, and starts over", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 401 })),
    );
    at("/signin/callback", "code=used&next=/device?code=WQRT-4821");
    renderWith(<SignInCallbackScreen />, { frame: false });
    expect(await screen.findByText("The sign-in didn't finish.")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Sign in again" }).getAttribute("href"),
    ).toBe("/signin?next=%2Fdevice%3Fcode%3DWQRT-4821");
  });
});

describe("CliAuth", () => {
  it("shows the code and what the device says, and confirms it", async () => {
    at("/device", "code=wqrt 4821");
    renderWith(<CliAuthScreen />);
    expect(
      await screen.findByRole("group", { name: "Code WQRT-4821" }),
    ).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Connect the memax CLI",
    );
    expect(screen.getByText("ziyang-mbp · macOS")).toBeTruthy();
    // In the facts, and in the terminal as the CLI printed it.
    expect(screen.getAllByText("memax 2.0.0")).toHaveLength(2);
    expect(screen.getByText("12 seconds ago, from 203.0.113.4")).toBeTruthy();
    expect(
      screen.getByText("memax-v2 · you can switch in the terminal"),
    ).toBeTruthy();
    expect(screen.getByText("Signed in as Ziyang Zeng")).toBeTruthy();
    const terminal = screen.getByRole("region", { name: /terminal/ });
    expect(terminal.textContent).toContain("WQRT-4821");
    fireEvent.click(screen.getByRole("button", { name: /^Confirm/ }));
    expect(
      await screen.findByText("Your terminal is signing in."),
    ).toBeTruthy();
  });

  it("confirms with Enter, and declines on It doesn't match", async () => {
    at("/device", "code=WQRT-4821");
    renderWith(<CliAuthScreen />);
    await screen.findByRole("group", { name: "Code WQRT-4821" });
    fireEvent.click(screen.getByRole("button", { name: "It doesn't match" }));
    expect(await screen.findByText("Nothing was signed in.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Confirm/ })).toBeNull();
  });

  it("takes Enter as Confirm", async () => {
    at("/device", "code=WQRT-4821");
    renderWith(<CliAuthScreen />);
    await screen.findByRole("group", { name: "Code WQRT-4821" });
    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
      );
    });
    expect(
      await screen.findByText("Your terminal is signing in."),
    ).toBeTruthy();
  });

  it("says when no code like that is waiting", async () => {
    at("/device", "code=BCDF-0000");
    renderWith(<CliAuthScreen />);
    expect(
      await screen.findByText("No code like that is waiting."),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Confirm/ })).toBeNull();
  });

  it("asks for the code when the link had none", () => {
    at("/device");
    renderWith(<CliAuthScreen />);
    const field = screen.getByLabelText("Code");
    fireEvent.change(field, { target: { value: "WQR" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      screen.getByText("A code is 4 letters and 4 digits, like WQRT-4821."),
    ).toBeTruthy();
    fireEvent.change(field, { target: { value: "wqrt 4821" } });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(h.replace).toHaveBeenCalledWith("/device?code=WQRT-4821");
  });

  it("sends a session the web app wasn't issued to sign in again first", async () => {
    // A CLI login used in the browser: its token says surface cli.
    const payload = btoa(JSON.stringify({ sub: "u", surface: "cli" }));
    h.token = `x.${payload}.y`;
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    h.source = { ...demo, kind: "sdk" } as LedgerDataSource;
    at("/device", "code=WQRT-4821");
    renderWith(<CliAuthScreen />);
    expect(
      await screen.findByText("Confirm this on the web app."),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Confirm/ })).toBeNull();
    expect(
      screen.getByRole("link", { name: "Sign in again" }).getAttribute("href"),
    ).toBe("/signin?next=%2Fdevice%3Fcode%3DWQRT-4821&again=1");
  });
});

describe("Connect", () => {
  it("lists the space's agents with their autonomy, and how to connect the rest", () => {
    at("/setup/agents", "space=memax-v2");
    renderWith(<ConnectScreen />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Which agents should share this context?",
    );
    expect(screen.getByText("Set up · step 1 of 3")).toBeTruthy();
    expect(
      screen.getByRole("img", { name: "Codex is connected" }),
    ).toBeTruthy();
    const codex = screen.getByRole("radiogroup", {
      name: "Autonomy for Codex",
    });
    expect(
      within(codex)
        .getByRole("radio", { name: "Propose" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      screen.getByText("npx memax-cli setup --mcp --only copilot"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: /Read their files/ })
        .getAttribute("href"),
    ).toBe("/setup/import?space=memax-v2");
    expect(screen.getByText("What each level means")).toBeTruthy();
  });
});

describe("FirstRun", () => {
  it("follows init's import to where the files disagree", () => {
    at("/setup/import", "space=memax-v2");
    renderWith(<FirstRunScreen />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Give every agent the same context.",
    );
    expect(
      screen.getByText(
        "40 statements from 6 files became 33 proposals after folding duplicates. Three of them disagree.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: /See where they disagree/ })
        .getAttribute("href"),
    ).toBe(`/setup/cleanup?space=memax-v2&import=${DEMO_IMPORT_ID}`);
    const terminal = screen.getByRole("region", { name: /zsh/ });
    expect(terminal.textContent).toContain("6 files, 3 conflicts, 1 Brief.");
    expect(terminal.textContent).toContain(
      "7 duplicates folded · 2 secrets skipped",
    );
    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
      );
    });
    expect(h.push).toHaveBeenCalledWith(
      `/setup/cleanup?space=memax-v2&import=${DEMO_IMPORT_ID}`,
    );
  });

  it("starts with the command while init hasn't run", () => {
    at("/setup/import", "space=memax-web");
    renderWith(<FirstRunScreen />);
    expect(screen.getByText("Run memax init in your repository")).toBeTruthy();
    expect(
      screen.getByText("npx memax-cli init --space memax-web"),
    ).toBeTruthy();
    expect(screen.getByRole("region", { name: /zsh/ }).textContent).toContain(
      "Waiting for memax init on your machine…",
    );
    expect(
      screen.queryByRole("link", { name: /See where they disagree/ }),
    ).toBeNull();
  });
});

describe("Cleanup", () => {
  it("settles a disagreement once, as a group", async () => {
    at("/setup/cleanup", "space=memax-v2");
    renderWith(<CleanupScreen />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "6 files, 3 conflicts, 1 Brief.",
    );
    const tests = screen.getByRole("radiogroup", { name: "Test command" });
    expect(within(tests).getAllByRole("radio")).toHaveLength(4);
    expect(
      screen.getAllByText("Choose with 1, 2, 3 or 4.").length,
    ).toBeGreaterThan(0);
    fireEvent.click(within(tests).getByRole("radio", { name: /pnpm test/ }));
    expect(
      screen.getByText(
        "Kept as one memory and written into every file Memax compiles.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /^Keep 1/ }));
    expect(
      await screen.findByText("Kept M-0501. The others were rejected."),
    ).toBeTruthy();
  });

  it("chooses with the number keys and keeps with Enter", async () => {
    at("/setup/cleanup", "space=memax-v2");
    renderWith(<CleanupScreen />);
    const key = (k: string) =>
      act(() => {
        document.dispatchEvent(
          new KeyboardEvent("keydown", { key: k, bubbles: true }),
        );
      });
    key("2");
    key("Enter");
    expect(
      await screen.findByText("Kept M-0502. The others were rejected."),
    ).toBeTruthy();
    // The next open one takes the keys now: the suggestion, 3.
    key("3");
    key("Enter");
    expect(await screen.findByText(/in the suggested words/)).toBeTruthy();
  });

  it("shows what init read, handled and will write, and goes on to Review", () => {
    at("/setup/cleanup", "space=memax-v2");
    renderWith(<CleanupScreen />);
    expect(screen.getByText("40 statements")).toBeTruthy();
    expect(screen.getByText("33 proposals")).toBeTruthy();
    expect(screen.getByText("7 duplicates folded")).toBeTruthy();
    expect(screen.getByText("2 secrets skipped")).toBeTruthy();
    expect(
      screen.getByText(
        /CLAUDE.md:38 \(OpenAI API key\) and .cursor\/rules\/deploy.mdc:6 \(GitHub token\)/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("beside 3 rule files")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: /Review 33 proposals/ })
        .getAttribute("href"),
    ).toBe(`/memax-v2/review?filter=import&import=${DEMO_IMPORT_ID}`);
    expect(
      screen.getByRole("link", { name: "Finish later" }).getAttribute("href"),
    ).toBe("/memax-v2/today");
  });
});

describe("CompileDone", () => {
  it("shows the files written, git status and the next steps", () => {
    at("/setup/done", "space=memax-v2");
    renderWith(<CompileDoneScreen />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "memax-v2 is compiled into 3 files.",
    );
    expect(screen.getByText("Set up · done")).toBeTruthy();
    const terminal = screen.getByRole("region", { name: /git status/ });
    expect(terminal.textContent).toContain("git status --short");
    expect(terminal.textContent).toContain(" M AGENTS.md");
    expect(terminal.textContent).not.toMatch(/ D /);
    expect(
      screen.getByRole("link", { name: /Open Today/ }).getAttribute("href"),
    ).toBe("/memax-v2/today");
    expect(
      screen
        .getByRole("link", { name: /Connect a cloud agent/ })
        .getAttribute("href"),
    ).toBe("/memax-v2/agents?overlay=connect");
    expect(screen.getByText("Team spaces arrive with Team.")).toBeTruthy();
  });
});
