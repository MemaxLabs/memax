// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CommandFailedError } from "@/lib/v2/data/command-error";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { SettingsSource } from "@/lib/v2/data/settings";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { renderPlace } from "../../_places/test-frame";
import { NotificationsSettings } from "./notifications-settings";

// Settings › Notifications (Notifications.png) over the demo source, its
// settings spied on: the board's matrix, an email choice, the morning
// edition as Dream's setting, a clash with an unsubscribe, and the quiet
// hours' fields.

const h = vi.hoisted(() => ({ source: null as LedgerDataSource | null }));

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
  usePathname: () => "/settings/notifications",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

function withSettings(
  over: (real: SettingsSource) => Partial<SettingsSource> = () => ({}),
): LedgerDataSource {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const source = {
    ...demo,
    settings: { ...demo.settings, ...over(demo.settings) },
  };
  h.source = source;
  return source;
}

const row = (title: string) =>
  screen.getByRole("row", { name: new RegExp(`^${title}`) });
const box = (title: string, channel: string) =>
  screen.getByRole("checkbox", { name: `${title}: ${channel}` });

describe("Settings › Notifications", () => {
  it("draws the board: every event, in app always on, the email column, phone and Slack to come", () => {
    withSettings();
    renderPlace(<NotificationsSettings />);
    expect(
      screen.getByRole("heading", { level: 1, name: "Notifications" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Memax only interrupts you for things waiting on you. Everything else waits in Today.",
      ),
    ).toBeTruthy();
    const table = screen.getByRole("table", {
      name: "When each event reaches you",
    });
    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((c) => c.textContent),
    ).toEqual(["When", "In app", "Email", "Phone", "Slack"]);
    expect(within(table).getAllByRole("rowheader")).toHaveLength(9);
    expect(
      within(row("The morning edition")).getByText(
        "What Dream changed overnight, at 08:00",
      ),
    ).toBeTruthy();
    expect(
      within(row("Proposals waiting a day")).getByText(
        "Once a day, never per proposal",
      ),
    ).toBeTruthy();

    const gate = box("An agent asks you to decide", "In app, always on");
    expect((gate as HTMLInputElement).checked).toBe(true);
    expect((gate as HTMLInputElement).disabled).toBe(true);
    const weekly = box("Weekly summary", "In app") as HTMLInputElement;
    expect(weekly.checked).toBe(false);
    expect(weekly.disabled).toBe(true);
    const emailed = screen
      .getAllByRole("checkbox", { name: /: Email$/ })
      .map((b) => (b as HTMLInputElement).checked);
    // The board's email column, then the two events it doesn't draw.
    expect(emailed).toEqual([
      true,
      true,
      true,
      false,
      false,
      true,
      true,
      false,
      true,
    ]);
    const phone = box("A compiled file drifted", "Phone, not available yet");
    expect((phone as HTMLInputElement).disabled).toBe(true);
    expect(
      screen.getByText(
        "Memax emails the morning edition today. It keeps your other choices and follows them as each email arrives. Phone and Slack aren't available yet.",
      ),
    ).toBeTruthy();

    expect(screen.getByRole("heading", { name: "Quiet hours" })).toBeTruthy();
    expect(
      (screen.getByRole("textbox", { name: "From" }) as HTMLInputElement).value,
    ).toBe("20:00");
    expect(
      (screen.getByRole("textbox", { name: "Until" }) as HTMLInputElement)
        .value,
    ).toBe("08:00");
    expect(
      (
        screen.getByRole("checkbox", {
          name: "Decision gates still reach me",
        }) as HTMLInputElement
      ).checked,
    ).toBe(true);
    expect(
      screen.getByRole("link", { name: /In America\/Vancouver/ }),
    ).toHaveProperty(
      "href",
      expect.stringContaining("/settings/account#dream"),
    );
  });

  it("changes an event's email from the version it read", async () => {
    let calls = 0;
    const source = withSettings((real) => ({
      updateNotifications: vi.fn((input) => {
        calls++;
        return real.updateNotifications(input);
      }),
    }));
    renderPlace(<NotificationsSettings />);
    const drift = box("A compiled file drifted", "Email") as HTMLInputElement;
    fireEvent.click(drift);
    await waitFor(() => expect(drift.checked).toBe(true));
    await waitFor(() => expect(calls).toBe(1));
    expect(source.settings.updateNotifications).toHaveBeenCalledWith(
      expect.objectContaining({
        change: { email: { drift: true } },
        version: 1,
        idempotencyKey: expect.any(String),
      }),
    );
    // A second change goes from the version the first wrote.
    fireEvent.click(box("Something you kept went stale", "Email"));
    await waitFor(() => expect(calls).toBe(2));
    expect(source.settings.updateNotifications).toHaveBeenLastCalledWith(
      expect.objectContaining({
        change: { email: { stale: true } },
        version: 2,
      }),
    );
  });

  it("turns the morning email off in Dream's setting, the one its unsubscribe link changes", async () => {
    const source = withSettings();
    renderPlace(<NotificationsSettings />);
    fireEvent.click(box("The morning edition", "Email"));
    await waitFor(async () =>
      expect((await source.dream.settings()).morningEmail).toBe(false),
    );
    await waitFor(() =>
      expect(
        screen.getByText("What Dream changed overnight, as soon as it's done"),
      ).toBeTruthy(),
    );
  });

  it("puts a change back and reloads when the settings changed elsewhere", async () => {
    withSettings((real) => ({
      updateNotifications: vi.fn(async () => {
        throw new CommandFailedError({ kind: "clash", currentVersion: 3 });
      }),
      notifications: vi.fn(real.notifications),
    }));
    renderPlace(<NotificationsSettings />);
    const drift = box("A compiled file drifted", "Email") as HTMLInputElement;
    fireEvent.click(drift);
    expect(
      await screen.findByText(
        "Your settings changed elsewhere, perhaps through an unsubscribe link. They're reloaded, so make your change again.",
      ),
    ).toBeTruthy();
    expect(drift.checked).toBe(false);
  });

  it("saves quiet hours when a field is left, says a bad time, and turns them off when both are cleared", async () => {
    const source = withSettings((real) => ({
      updateNotifications: vi.fn(real.updateNotifications),
    }));
    renderPlace(<NotificationsSettings />);
    const from = screen.getByRole("textbox", { name: "From" });
    const until = screen.getByRole("textbox", { name: "Until" });
    fireEvent.change(from, { target: { value: "21:30" } });
    fireEvent.blur(from);
    await waitFor(() =>
      expect(source.settings.updateNotifications).toHaveBeenCalledWith(
        expect.objectContaining({ change: { quietHours: { from: "21:30" } } }),
      ),
    );
    fireEvent.change(until, { target: { value: "7am" } });
    fireEvent.keyDown(until, { key: "Enter" });
    expect(
      await screen.findByText("Use a 24-hour time, such as 08:00"),
    ).toBeTruthy();
    expect(source.settings.updateNotifications).toHaveBeenCalledTimes(1);

    fireEvent.change(from, { target: { value: "" } });
    fireEvent.change(until, { target: { value: "" } });
    fireEvent.blur(until);
    await waitFor(() =>
      expect(source.settings.updateNotifications).toHaveBeenLastCalledWith(
        expect.objectContaining({ change: { quietHours: { on: false } } }),
      ),
    );
    expect(
      await screen.findByText("Off. Set both times to hold email overnight."),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("checkbox", { name: "Decision gates still reach me" }),
    );
    await waitFor(() =>
      expect(source.settings.updateNotifications).toHaveBeenLastCalledWith(
        expect.objectContaining({
          change: { quietHours: { gatesThrough: false } },
        }),
      ),
    );
  });
});
