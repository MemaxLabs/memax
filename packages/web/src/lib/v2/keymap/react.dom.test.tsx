// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  KeymapProvider,
  KeyScopeBoundary,
  useHotkey,
  useKeycap,
} from "./react";

afterEach(cleanup);

function Help({ onHelp }: { onHelp: () => void }) {
  useHotkey("help.keys", onHelp);
  return null;
}

describe("the keymap in React", () => {
  it("listens on the document and calls the latest handler", () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = render(
      <KeymapProvider>
        <Help onHelp={first} />
      </KeymapProvider>,
    );
    fireEvent.keyDown(document.body, { key: "?", shiftKey: true });
    expect(first).toHaveBeenCalledOnce();
    rerender(
      <KeymapProvider>
        <Help onHelp={second} />
      </KeymapProvider>,
    );
    fireEvent.keyDown(document.body, { key: "?", shiftKey: true });
    expect(second).toHaveBeenCalledOnce();
    expect(first).toHaveBeenCalledOnce();
  });

  it("stops listening when the component unmounts", () => {
    const onHelp = vi.fn();
    const { unmount } = render(
      <KeymapProvider>
        <Help onHelp={onHelp} />
      </KeymapProvider>,
    );
    unmount();
    fireEvent.keyDown(document.body, { key: "?", shiftKey: true });
    expect(onHelp).not.toHaveBeenCalled();
  });

  it("lets a mounted modal layer silence the page beneath it", () => {
    const page = vi.fn();
    const layer = vi.fn();
    function Harness() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <Help onHelp={page} />
          <button type="button" onClick={() => setOpen((o) => !o)}>
            toggle
          </button>
          {open ? (
            <KeyScopeBoundary name="sheet" modal>
              <Help onHelp={layer} />
            </KeyScopeBoundary>
          ) : null}
        </>
      );
    }
    render(
      <KeymapProvider>
        <Harness />
      </KeymapProvider>,
    );
    act(() => screen.getByRole("button").click());
    fireEvent.keyDown(document.body, { key: "?", shiftKey: true });
    expect(layer).toHaveBeenCalledOnce();
    expect(page).not.toHaveBeenCalled();
    act(() => screen.getByRole("button").click());
    fireEvent.keyDown(document.body, { key: "?", shiftKey: true });
    expect(page).toHaveBeenCalledOnce();
  });

  it("ignores keys typed in a field", () => {
    const onHelp = vi.fn();
    render(
      <KeymapProvider>
        <Help onHelp={onHelp} />
        <input aria-label="field" />
      </KeymapProvider>,
    );
    fireEvent.keyDown(screen.getByRole("textbox"), {
      key: "?",
      shiftKey: true,
    });
    expect(onHelp).not.toHaveBeenCalled();
  });

  it("shows the platform's caps", () => {
    function Cap() {
      return <span>{useKeycap("command.open")}</span>;
    }
    const platform = vi.spyOn(navigator, "platform", "get");
    platform.mockReturnValue("Win32");
    render(
      <KeymapProvider>
        <Cap />
      </KeymapProvider>,
    );
    expect(screen.getByText("Ctrl+K")).toBeTruthy();
    cleanup();
    platform.mockReturnValue("MacIntel");
    render(
      <KeymapProvider>
        <Cap />
      </KeymapProvider>,
    );
    expect(screen.getByText("⌘K")).toBeTruthy();
    platform.mockRestore();
  });
});
