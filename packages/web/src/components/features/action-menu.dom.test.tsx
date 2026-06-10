// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import {
  render,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { ActionMenu, actionMenuEntries } from "./action-menu";

/**
 * ActionMenu — keyboard nav + async handler safety.
 *
 * Covers the codex-flagged risks:
 *  - async onSelect that rejects must not bubble as an unhandled
 *    rejection after the menu closes
 *  - keyboard users can reach items and activate them
 *  - divider entries are rendered but not activatable
 */
describe("ActionMenu", () => {
  afterEach(() => {
    cleanup();
  });

  it("opens on trigger click and renders all items", async () => {
    const onCopy = vi.fn();
    const onRename = vi.fn();
    render(
      <ActionMenu
        triggerAriaLabel="More actions"
        items={actionMenuEntries(
          { id: "copy", label: "Copy markdown", onSelect: onCopy },
          { id: "rename", label: "Rename", onSelect: onRename },
        )}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "More actions" }));

    await waitFor(() => {
      expect(screen.getByRole("menu")).toBeTruthy();
    });
    expect(
      screen.getByRole("menuitem", { name: "Copy markdown" }),
    ).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Rename" })).toBeTruthy();
  });

  it("fires onSelect and closes on item click", async () => {
    const onRename = vi.fn();
    render(
      <ActionMenu
        triggerAriaLabel="More"
        items={actionMenuEntries({
          id: "rename",
          label: "Rename",
          onSelect: onRename,
        })}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "More" }));
    await waitFor(() => screen.getByRole("menu"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }));

    await waitFor(() => {
      expect(onRename).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(screen.queryByRole("menu")).toBeNull();
    });
  });

  it("swallows async onSelect rejections without unhandled promise errors", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const onSelect = vi.fn(async () => {
      throw new Error("boom");
    });

    render(
      <ActionMenu
        triggerAriaLabel="More"
        items={actionMenuEntries({
          id: "throws",
          label: "Throws",
          onSelect,
        })}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "More" }));
    await waitFor(() => screen.getByRole("menu"));
    fireEvent.click(screen.getByRole("menuitem", { name: "Throws" }));

    await waitFor(() => {
      expect(onSelect).toHaveBeenCalledTimes(1);
    });
    // The rejection is caught locally and logged, not rethrown.
    await waitFor(() => {
      expect(consoleError).toHaveBeenCalled();
    });
    const loggedMessages = consoleError.mock.calls.map((args) => args[0]);
    expect(
      loggedMessages.some(
        (msg) =>
          typeof msg === "string" && msg.includes('ActionMenu] item "throws"'),
      ),
    ).toBe(true);
    consoleError.mockRestore();
  });

  describe("destructive items — sub-panel morph", () => {
    it("selecting a destructive item swaps to a confirm sub-panel (popover stays open)", async () => {
      const onConfirm = vi.fn<() => Promise<boolean>>(async () => true);

      render(
        <ActionMenu
          triggerAriaLabel="More"
          items={actionMenuEntries(
            { id: "copy", label: "Copy", onSelect: vi.fn() },
            "divider",
            {
              id: "forget",
              label: "Forget",
              destructive: true,
              confirmPrompt: "Forget this memory?",
              confirmLabel: "Forget",
              cancelLabel: "Keep",
              onConfirm,
            },
          )}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "More" }));
      await waitFor(() => screen.getByRole("menu"));

      fireEvent.click(screen.getByRole("menuitem", { name: "Forget" }));

      // Sub-panel mounts — alertdialog role carries the prompt.
      await waitFor(() => {
        expect(
          screen.getByRole("alertdialog", { name: "Forget this memory?" }),
        ).toBeTruthy();
      });
      // Menu list is gone while the sub-panel is mounted.
      expect(screen.queryByRole("menu")).toBeNull();
      // Both buttons present; safe (Keep) gets default focus.
      expect(screen.getByRole("button", { name: "Forget" })).toBeTruthy();
      const keep = screen.getByRole("button", { name: "Keep" });
      expect(keep).toBeTruthy();
      await waitFor(() => {
        expect(document.activeElement).toBe(keep);
      });
    });

    it("Keep returns to the menu without firing onConfirm", async () => {
      const onConfirm = vi.fn<() => Promise<boolean>>(async () => true);
      render(
        <ActionMenu
          triggerAriaLabel="More"
          items={actionMenuEntries({
            id: "forget",
            label: "Forget",
            destructive: true,
            confirmPrompt: "Forget?",
            confirmLabel: "Forget",
            cancelLabel: "Keep",
            onConfirm,
          })}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "More" }));
      await waitFor(() => screen.getByRole("menu"));
      fireEvent.click(screen.getByRole("menuitem", { name: "Forget" }));
      await waitFor(() => screen.getByRole("alertdialog"));

      fireEvent.click(screen.getByRole("button", { name: "Keep" }));

      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).toBeNull();
        expect(screen.getByRole("menu")).toBeTruthy();
      });
      expect(onConfirm).not.toHaveBeenCalled();
    });

    it("Escape inside the sub-panel returns to the menu (does not close the popover)", async () => {
      render(
        <ActionMenu
          triggerAriaLabel="More"
          items={actionMenuEntries({
            id: "forget",
            label: "Forget",
            destructive: true,
            confirmPrompt: "Forget?",
            confirmLabel: "Forget",
            cancelLabel: "Keep",
            onConfirm: async () => true,
          })}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "More" }));
      await waitFor(() => screen.getByRole("menu"));
      fireEvent.click(screen.getByRole("menuitem", { name: "Forget" }));
      const panel = await waitFor(() => screen.getByRole("alertdialog"));

      fireEvent.keyDown(panel, { key: "Escape" });

      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).toBeNull();
        expect(screen.getByRole("menu")).toBeTruthy();
      });
    });

    it("confirming calls onConfirm; resolve(true) closes the popover", async () => {
      const onConfirm = vi.fn<() => Promise<boolean>>(async () => true);
      render(
        <ActionMenu
          triggerAriaLabel="More"
          items={actionMenuEntries({
            id: "forget",
            label: "Forget",
            destructive: true,
            confirmPrompt: "Forget?",
            confirmLabel: "Forget",
            cancelLabel: "Keep",
            onConfirm,
          })}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "More" }));
      await waitFor(() => screen.getByRole("menu"));
      fireEvent.click(screen.getByRole("menuitem", { name: "Forget" }));
      await waitFor(() => screen.getByRole("alertdialog"));

      fireEvent.click(screen.getByRole("button", { name: "Forget" }));

      await waitFor(() => {
        expect(onConfirm).toHaveBeenCalledTimes(1);
      });
      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).toBeNull();
        expect(screen.queryByRole("menu")).toBeNull();
      });
    });

    it("resolve(false) keeps the sub-panel mounted for retry", async () => {
      const onConfirm = vi.fn<() => Promise<boolean>>(async () => false);
      render(
        <ActionMenu
          triggerAriaLabel="More"
          items={actionMenuEntries({
            id: "forget",
            label: "Forget",
            destructive: true,
            confirmPrompt: "Forget?",
            confirmLabel: "Forget",
            cancelLabel: "Keep",
            onConfirm,
          })}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "More" }));
      await waitFor(() => screen.getByRole("menu"));
      fireEvent.click(screen.getByRole("menuitem", { name: "Forget" }));
      await waitFor(() => screen.getByRole("alertdialog"));
      fireEvent.click(screen.getByRole("button", { name: "Forget" }));

      await waitFor(() => {
        expect(onConfirm).toHaveBeenCalledTimes(1);
      });
      // Sub-panel still mounted; popover still open.
      expect(screen.getByRole("alertdialog")).toBeTruthy();
    });
  });

  it("renders a divider as role=separator and skips it in keyboard nav", async () => {
    const onFirst = vi.fn();
    const onSecond = vi.fn();
    render(
      <ActionMenu
        triggerAriaLabel="More"
        items={actionMenuEntries(
          { id: "one", label: "One", onSelect: onFirst },
          "divider",
          { id: "two", label: "Two", onSelect: onSecond },
        )}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "More" }));
    await waitFor(() => screen.getByRole("menu"));
    // separator renders
    expect(screen.getByRole("separator")).toBeTruthy();
    // both items clickable
    fireEvent.click(screen.getByRole("menuitem", { name: "Two" }));
    await waitFor(() => {
      expect(onSecond).toHaveBeenCalledTimes(1);
    });
    expect(onFirst).not.toHaveBeenCalled();
  });
});
