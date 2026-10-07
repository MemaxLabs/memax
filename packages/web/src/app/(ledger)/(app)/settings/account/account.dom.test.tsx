// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AccountSource } from "@/lib/v2/data/account";
import { DEMO_ACCOUNT } from "@/lib/v2/data/account-demo";
import { CommandFailedError } from "@/lib/v2/data/command-error";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { renderPlace } from "../../_places/test-frame";
import { AccountSettings } from "./account-settings";

// Settings › Account (Account.png) over the demo source: the profile, the
// ways in, passkeys and their dialog, the sessions, and forgetting the
// account, with each command's answer and refusal.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  params: new URLSearchParams(),
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
  useAuth: () => ({ user: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/settings/account",
  useSearchParams: () => h.params,
}));

let assign: ReturnType<typeof vi.fn>;

beforeEach(() => {
  assign = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      ...window.location,
      assign,
      origin: "http://localhost:3000",
      href: "http://localhost:3000/settings/account",
    },
  });
});

afterEach(() => {
  cleanup();
  h.source = null;
  h.params = new URLSearchParams();
  vi.unstubAllGlobals();
});

/** The demo source with its account changed, its calls spied on. */
function withAccount(
  change: (a: AccountSource) => Partial<AccountSource> = () => ({}),
) {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const account = { ...demo.account, ...change(demo.account) };
  const spied = Object.fromEntries(
    Object.entries(account).map(([k, v]) => [
      k,
      typeof v === "function" ? vi.fn(v as (...a: unknown[]) => unknown) : v,
    ]),
  ) as unknown as AccountSource;
  h.source = { ...demo, account: spied };
  return { account: spied, demo: h.source };
}

const panel = (name: string) => screen.getByRole("region", { name });

/** A browser with WebAuthn that makes a passkey when asked. */
function withWebAuthn(create = vi.fn()) {
  vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: { create, get: vi.fn() },
  });
  return create;
}

describe("Settings › Account", () => {
  it("draws the board: profile, sign-in methods, sessions and the danger zone", () => {
    withAccount();
    renderPlace(<AccountSettings />);
    expect(
      screen.getByRole("heading", { level: 1, name: "Account" }),
    ).toBeTruthy();
    expect(
      screen.getByText("You, how you sign in, and where you're signed in."),
    ).toBeTruthy();
    // The theme is this device's, beside the title.
    expect(screen.getByRole("radiogroup", { name: "Theme" })).toBeTruthy();

    const profile = panel("Profile");
    expect(
      (within(profile).getByLabelText("Name") as HTMLInputElement).value,
    ).toBe("Ziyang Zeng");
    expect(within(profile).getByText("Your receipts show ZZ.")).toBeTruthy();
    const email = within(profile).getByLabelText("Email") as HTMLInputElement;
    expect(email.value).toBe("ziyang@example.com");
    expect(email.readOnly).toBe(true);
    expect(
      within(profile).getByText("Receipts and Dream use it."),
    ).toBeTruthy();
    expect(
      within(profile).getByRole("radiogroup", { name: "Language" }),
    ).toBeTruthy();

    const signIn = panel("Sign in with");
    expect(within(signIn).getByText("· ziyang")).toBeTruthy();
    expect(within(signIn).getByText("Connected")).toBeTruthy();
    expect(within(signIn).getByText("Not connected")).toBeTruthy();
    expect(
      within(signIn).getByRole("button", { name: "Connect" }),
    ).toBeTruthy();
    expect(within(signIn).getByText("· added Sep 2")).toBeTruthy();
    expect(within(signIn).getByText("On")).toBeTruthy();
    expect(within(signIn).getByRole("button", { name: "Manage" })).toBeTruthy();

    const sessions = panel("Signed in on");
    const rows = within(sessions).getAllByText(
      /^(This browser|memax CLI on ziyang-mbp|Safari on iOS)/,
    );
    expect(rows.map((r) => r.textContent)).toEqual([
      "This browser",
      "memax CLI on ziyang-mbp · 2.0.0",
      "Safari on iOS",
    ]);
    expect(within(sessions).getByText("This one")).toBeTruthy();
    expect(within(sessions).getByText("2 min ago")).toBeTruthy();
    expect(within(sessions).getByText("yesterday")).toBeTruthy();
    expect(
      within(sessions).getAllByRole("button", { name: "Sign out" }),
    ).toHaveLength(2);

    const danger = panel("Forget your account");
    expect(
      within(danger).getByText(/Team spaces stay with their other owners/),
    ).toBeTruthy();
    expect(
      within(danger).getByRole("button", { name: "Forget account" }),
    ).toBeTruthy();
  });

  it("updates the name and the time zone", async () => {
    const { account, demo } = withAccount();
    const updateSettings = vi.spyOn(demo.dream, "updateSettings");
    renderPlace(<AccountSettings />);
    const profile = panel("Profile");
    const update = within(profile).getByRole("button", { name: "Update" });
    // Nothing changed yet.
    expect(
      update.hasAttribute("disabled") ||
        update.getAttribute("aria-disabled") === "true",
    ).toBe(true);
    fireEvent.change(within(profile).getByLabelText("Name"), {
      target: { value: "  Ziyang   Z. " },
    });
    fireEvent.change(within(profile).getByLabelText("Time zone"), {
      target: { value: "Europe/Paris" },
    });
    fireEvent.click(within(profile).getByRole("button", { name: "Update" }));
    await screen.findByText("Your profile is updated.");
    expect(account.rename).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Ziyang Z." }),
    );
    expect(updateSettings).toHaveBeenCalledWith(
      expect.objectContaining({ timeZone: "Europe/Paris" }),
    );
  });

  it("refuses an empty name before sending it", async () => {
    const { account } = withAccount();
    renderPlace(<AccountSettings />);
    const profile = panel("Profile");
    fireEvent.change(within(profile).getByLabelText("Name"), {
      target: { value: "   " },
    });
    fireEvent.click(within(profile).getByRole("button", { name: "Update" }));
    await screen.findByText("Write a name of 1 to 80 characters.");
    expect(account.rename).not.toHaveBeenCalled();
  });

  it("signs a session out, and everywhere else", async () => {
    const { account } = withAccount();
    renderPlace(<AccountSettings />);
    const sessions = panel("Signed in on");
    fireEvent.click(
      within(sessions).getAllByRole("button", { name: "Sign out" })[0]!,
    );
    await screen.findByText("memax CLI on ziyang-mbp is signed out.");
    expect(account.signOut).toHaveBeenCalledWith(
      expect.objectContaining({ id: "0192a7c0-0000-7000-8000-0000000005e2" }),
    );
    fireEvent.click(
      within(sessions).getByRole("button", {
        name: "Sign out everywhere else",
      }),
    );
    await screen.findByText("Signed out of 1 other session.");
    expect(account.signOutOthers).toHaveBeenCalled();
  });

  it("connects Google through its page, and keeps the last way in", async () => {
    const { account } = withAccount();
    renderPlace(<AccountSettings />);
    const signIn = panel("Sign in with");
    fireEvent.click(within(signIn).getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(assign).toHaveBeenCalled());
    expect(account.connectSignIn).toHaveBeenCalledWith(
      expect.objectContaining({
        method: "google",
        redirectUri: "http://localhost:3000/settings/account",
      }),
    );
    // GitHub is the only provider left (Google isn't linked in the demo).
    cleanup();
    withAccount();
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Sign in with")).getByRole("button", { name: "Disconnect" }),
    );
    await screen.findByText(
      "That's your last way to sign in with GitHub or Google. Connect the other first.",
    );
  });

  it("asks for a fresh sign-in to connect, when the server says so", async () => {
    withAccount(() => ({
      connectSignIn: async () => {
        throw new CommandFailedError({
          kind: "refused",
          code: "needs_sign_in",
          message: null,
        });
      },
    }));
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Sign in with")).getByRole("button", { name: "Connect" }),
    );
    const alert = await screen.findByText(
      /Adding a way in needs a sign-in from the last few minutes/,
    );
    expect(alert).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Sign in again" }).getAttribute("href"),
    ).toBe("/signin?again=1&next=%2Fsettings%2Faccount");
  });

  it("says when a provider came back linked, or why not", async () => {
    withAccount();
    h.params = new URLSearchParams(
      "account_link_error=identity_taken&provider=google",
    );
    renderPlace(<AccountSettings />);
    await screen.findByText(
      "Google wasn't connected: that account already signs in to another Memax account.",
    );
  });
});

describe("passkeys", () => {
  it("manages them: rename, remove (which may ask for the passkey), add", async () => {
    const create = withWebAuthn(
      vi.fn(async () => ({
        id: "new",
        type: "public-key",
        toJSON: () => ({
          id: "new",
          rawId: "new",
          type: "public-key",
          response: {},
        }),
      })),
    );
    const { account } = withAccount();
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Sign in with")).getByRole("button", { name: "Manage" }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Passkeys" });
    const row = within(dialog)
      .getByText("iCloud Keychain")
      .closest("li") as HTMLElement;
    expect(
      within(row).getByText(
        /Added Sep 2 · last used Oct 4 · Synced across your devices/,
      ),
    ).toBeTruthy();

    fireEvent.click(within(row).getByRole("button", { name: "Rename" }));
    fireEvent.change(within(row).getByLabelText("Name for iCloud Keychain"), {
      target: { value: "Work laptop" },
    });
    fireEvent.click(within(row).getByRole("button", { name: "Done" }));
    await screen.findByText("Renamed to Work laptop.");
    expect(account.renamePasskey).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Work laptop" }),
    );

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Add a passkey" }),
    );
    await screen.findByText(
      "Passkey added. Decisions that need you will ask for it.",
    );
    expect(account.startPasskey).toHaveBeenCalled();
    expect(create).toHaveBeenCalledTimes(1);
    const publicKey = (
      create.mock.calls[0]![0] as {
        publicKey: PublicKeyCredentialCreationOptions;
      }
    ).publicKey;
    expect(publicKey.authenticatorSelection).toMatchObject({
      residentKey: "required",
      userVerification: "required",
    });
    expect(account.addPasskey).toHaveBeenCalledWith(
      expect.objectContaining({
        credential: expect.objectContaining({ id: "new" }),
      }),
    );
  });

  it("says nothing changed when the person closes the re-check on Remove", async () => {
    withAccount(() => ({
      removePasskey: async () => {
        throw new CommandFailedError({ kind: "passkey", failure: null });
      },
    }));
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Sign in with")).getByRole("button", { name: "Manage" }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Passkeys" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    expect(
      within(dialog).getByText(
        /It's your last passkey: decisions that need you will ask for the web alone\./,
      ),
    ).toBeTruthy();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Remove iCloud Keychain" }),
    );
    await within(dialog).findByText(
      "It needs your passkey, so nothing changed.",
    );
  });

  it("with none and an old sign-in, asks to sign in again before adding one", async () => {
    const create = withWebAuthn();
    const { account } = withAccount(() => ({
      peekAccount: () => ({
        ...DEMO_ACCOUNT,
        passkeys: [],
        passkeyCheck: false,
      }),
      account: async () => ({
        ...DEMO_ACCOUNT,
        passkeys: [],
        passkeyCheck: false,
      }),
    }));
    renderPlace(<AccountSettings />);
    const signIn = panel("Sign in with");
    expect(within(signIn).getByText("Off")).toBeTruthy();
    expect(
      within(signIn).getByText(
        "Add one, and decisions only you should make ask for it.",
      ),
    ).toBeTruthy();
    fireEvent.click(
      within(signIn).getByRole("button", { name: "Add a passkey" }),
    );
    await within(signIn).findByText(
      /Adding a way in needs a sign-in from the last few minutes/,
    );
    expect(account.startPasskey).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
  });

  it("says when the browser's prompt was closed", async () => {
    const closed = Object.assign(new Error("closed"), {
      name: "NotAllowedError",
    });
    withWebAuthn(vi.fn(async () => Promise.reject(closed)));
    const { account } = withAccount(() => ({
      peekAccount: () => ({
        ...DEMO_ACCOUNT,
        passkeys: [],
        passkeyCheck: false,
        session: {
          signedInAt: DEMO_ACCOUNT.session.signedInAt,
          freshUntil: "2026-10-05T14:45:00-07:00",
        },
      }),
    }));
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Sign in with")).getByRole("button", {
        name: "Add a passkey",
      }),
    );
    await screen.findByText(
      "The passkey prompt closed before it finished. Try again when you're ready.",
    );
    expect(account.addPasskey).not.toHaveBeenCalled();
  });
});

describe("forgetting the account", () => {
  it("needs the email typed, then forgets, signs out and goes to sign-in", async () => {
    const fetchMock = vi.fn(async () => new Response("{}"));
    vi.stubGlobal("fetch", fetchMock);
    const { account } = withAccount();
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Forget your account")).getByRole("button", {
        name: "Forget account",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Forget your account?",
    });
    expect(
      within(dialog).getByText(/Team spaces stay with their members/),
    ).toBeTruthy();
    const confirm = within(dialog).getByRole("button", {
      name: "Forget account",
    });
    expect(
      confirm.getAttribute("aria-disabled") === "true" ||
        confirm.hasAttribute("disabled"),
    ).toBe(true);
    const field = within(dialog).getByLabelText(
      "Type ziyang@example.com to confirm",
    );
    fireEvent.change(field, { target: { value: "someone@else.test" } });
    fireEvent.click(confirm);
    expect(account.forget).not.toHaveBeenCalled();
    fireEvent.change(field, { target: { value: " Ziyang@Example.com " } });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Forget account" }),
    );
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/signin"));
    expect(account.forget).toHaveBeenCalledWith(
      expect.objectContaining({ confirm: "Ziyang@Example.com" }),
    );
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/logout", {
      method: "POST",
    });
  });

  it("says why when the passkey check fails", async () => {
    withAccount(() => ({
      forget: async () => {
        throw new CommandFailedError({ kind: "passkey", failure: "expired" });
      },
    }));
    renderPlace(<AccountSettings />);
    fireEvent.click(
      within(panel("Forget your account")).getByRole("button", {
        name: "Forget account",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Forget your account?",
    });
    fireEvent.change(
      within(dialog).getByLabelText("Type ziyang@example.com to confirm"),
      {
        target: { value: "ziyang@example.com" },
      },
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Forget account" }),
    );
    await within(dialog).findByText(
      "The passkey check took too long, so nothing changed. Try again.",
    );
    expect(assign).not.toHaveBeenCalled();
  });
});
