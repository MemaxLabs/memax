import { describe, expect, it, vi } from "vitest";
import { createKeymap, isTextEntry } from "./keymap";
import type { KeyEventLike } from "./keys";
import { KEYMAP } from "./registry";

type Init = Partial<Omit<KeyEventLike, "key">>;

function press(key: string, init: Init = {}) {
  const event = {
    key,
    code: init.code,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    defaultPrevented: false,
    target: { tagName: "BODY" },
    ...init,
    preventDefault() {
      event.defaultPrevented = true;
    },
  } satisfies KeyEventLike;
  return event;
}

function setup(platform: "mac" | "other" = "mac") {
  let clock = 0;
  const keymap = createKeymap({
    bindings: KEYMAP,
    platform,
    sequenceTimeout: 1000,
    now: () => clock,
  });
  return {
    keymap,
    tick(ms: number) {
      clock += ms;
    },
  };
}

const INPUT = { tagName: "INPUT", type: "text" };

describe("sequences", () => {
  it("fires G T, and only on the second key", () => {
    const { keymap } = setup();
    const today = vi.fn();
    keymap.root.register("go.today", today);
    expect(keymap.handle(press("g"))).toBe(false);
    expect(keymap.pending).toBe(true);
    expect(today).not.toHaveBeenCalled();
    const t = press("t");
    expect(keymap.handle(t)).toBe(true);
    expect(today).toHaveBeenCalledOnce();
    expect(t.defaultPrevented).toBe(true);
    expect(keymap.pending).toBe(false);
  });

  it("tells the four G sequences apart", () => {
    const { keymap } = setup();
    const seen: string[] = [];
    for (const id of ["go.today", "go.review", "go.brief", "go.memories"]) {
      keymap.root.register(id, () => {
        seen.push(id);
      });
    }
    for (const second of ["t", "r", "b", "m"]) {
      keymap.handle(press("g"));
      keymap.handle(press(second));
    }
    expect(seen).toEqual(["go.today", "go.review", "go.brief", "go.memories"]);
  });

  it("gives up after the timeout", () => {
    const { keymap, tick } = setup();
    const today = vi.fn();
    keymap.root.register("go.today", today);
    keymap.handle(press("g"));
    tick(1001);
    expect(keymap.pending).toBe(false);
    keymap.handle(press("t"));
    expect(today).not.toHaveBeenCalled();
  });

  it("restarts on a key that breaks the sequence", () => {
    const { keymap } = setup();
    const today = vi.fn();
    const help = vi.fn();
    keymap.root.register("go.today", today);
    keymap.root.register("help.keys", help);
    keymap.handle(press("g"));
    // ? isn't a second step of any G sequence, so it counts on its own.
    keymap.handle(press("?", { shiftKey: true }));
    expect(help).toHaveBeenCalledOnce();
    keymap.handle(press("t"));
    expect(today).not.toHaveBeenCalled();
    keymap.handle(press("g"));
    keymap.handle(press("g"));
    keymap.handle(press("t"));
    expect(today).toHaveBeenCalledOnce();
  });

  it("lets a bare modifier keypress through without breaking it", () => {
    const { keymap } = setup();
    const today = vi.fn();
    keymap.root.register("go.today", today);
    keymap.handle(press("g"));
    keymap.handle(press("Shift", { shiftKey: true }));
    keymap.handle(press("t"));
    expect(today).toHaveBeenCalledOnce();
  });
});

describe("scope precedence", () => {
  it("prefers the newest scope, then falls back when it's gone", () => {
    const { keymap } = setup();
    const app = vi.fn();
    const page = vi.fn();
    keymap.root.register("undo", app);
    const review = keymap.pushScope("review");
    review.register("undo", page);
    keymap.handle(press("z", { metaKey: true }));
    expect(page).toHaveBeenCalledOnce();
    expect(app).not.toHaveBeenCalled();
    review.dispose();
    keymap.handle(press("z", { metaKey: true }));
    expect(app).toHaveBeenCalledOnce();
  });

  it("passes the key down when a handler returns false", () => {
    const { keymap } = setup();
    const app = vi.fn();
    keymap.root.register("undo", app);
    keymap.pushScope("review").register("undo", () => false);
    keymap.handle(press("z", { metaKey: true }));
    expect(app).toHaveBeenCalledOnce();
  });

  it("silences every lower scope under a modal one", () => {
    const { keymap } = setup();
    const today = vi.fn();
    const keep = vi.fn();
    const closeCommand = vi.fn();
    keymap.root.register("go.today", today);
    keymap.pushScope("review").register("review.keep", keep);
    const command = keymap.pushScope("command", { modal: true });
    command.register("command.open", closeCommand);
    keymap.handle(press("g"));
    keymap.handle(press("t"));
    keymap.handle(press("k"));
    expect(today).not.toHaveBeenCalled();
    expect(keep).not.toHaveBeenCalled();
    keymap.handle(press("k", { metaKey: true }));
    expect(closeCommand).toHaveBeenCalledOnce();
    command.dispose();
    keymap.handle(press("k"));
    expect(keep).toHaveBeenCalledOnce();
  });

  it("keeps a modal scope on top of a page scope that mounts later", () => {
    const { keymap } = setup();
    const keep = vi.fn();
    const modal = keymap.pushScope("sheet", { modal: true });
    modal.register("help.keys", () => {});
    keymap.pushScope("review").register("review.keep", keep);
    keymap.handle(press("k"));
    expect(keep).not.toHaveBeenCalled();
  });

  it("passes the matched alternative to the handler", () => {
    const { keymap } = setup("other");
    const switchSpace = vi.fn();
    const move = vi.fn();
    keymap.root.register("space.switch", switchSpace);
    keymap.pushScope("review").register("review.move", move);
    keymap.handle(press("3", { ctrlKey: true, code: "Digit3" }));
    keymap.handle(press("ArrowUp"));
    expect(switchSpace).toHaveBeenCalledWith(expect.anything(), { index: 2 });
    expect(move).toHaveBeenCalledWith(expect.anything(), { index: 1 });
  });

  it("refuses handlers for bindings that aren't declared", () => {
    const { keymap } = setup();
    expect(() => keymap.root.register("memory.delete", () => {})).toThrow(
      /No key binding/,
    );
  });
});

describe("the IME guard", () => {
  it("ignores keys while a composition is in progress", () => {
    const { keymap } = setup();
    const today = vi.fn();
    const help = vi.fn();
    keymap.root.register("go.today", today);
    keymap.root.register("help.keys", help);
    keymap.handle(press("g", { isComposing: true }));
    keymap.handle(press("t", { isComposing: true }));
    keymap.handle(press("?", { isComposing: true, shiftKey: true }));
    expect(today).not.toHaveBeenCalled();
    expect(help).not.toHaveBeenCalled();
  });

  it("treats keyCode 229 as composing (Safari's committing Enter)", () => {
    const { keymap } = setup();
    const keep = vi.fn();
    keymap.root.register("command.keep", keep);
    keymap.handle(press("Enter", { metaKey: true, keyCode: 229 }));
    keymap.handle(press("g", { keyCode: 229 }));
    expect(keep).not.toHaveBeenCalled();
  });

  it("drops a half-typed sequence when a composition starts", () => {
    const { keymap } = setup();
    const today = vi.fn();
    keymap.root.register("go.today", today);
    keymap.handle(press("g"));
    keymap.handle(press("t", { isComposing: true }));
    keymap.handle(press("t"));
    expect(today).not.toHaveBeenCalled();
  });
});

describe("typing", () => {
  it("ignores bare keys and sequences in text fields", () => {
    const { keymap } = setup();
    const today = vi.fn();
    const help = vi.fn();
    keymap.root.register("go.today", today);
    keymap.root.register("help.keys", help);
    for (const target of [
      INPUT,
      { tagName: "INPUT", type: "search" },
      { tagName: "TEXTAREA" },
      { tagName: "SELECT" },
      { tagName: "DIV", isContentEditable: true },
    ]) {
      keymap.handle(press("g", { target }));
      keymap.handle(press("t", { target }));
      keymap.handle(press("?", { target, shiftKey: true }));
    }
    expect(today).not.toHaveBeenCalled();
    expect(help).not.toHaveBeenCalled();
  });

  it("still fires ⌘/Ctrl chords marked inInput", () => {
    const { keymap } = setup("other");
    const open = vi.fn();
    const undo = vi.fn();
    keymap.root.register("command.open", open);
    keymap.root.register("undo", undo);
    keymap.handle(press("k", { ctrlKey: true, target: INPUT }));
    // ⌘Z in a field is the field's own undo.
    keymap.handle(press("z", { ctrlKey: true, target: INPUT }));
    expect(open).toHaveBeenCalledOnce();
    expect(undo).not.toHaveBeenCalled();
  });

  it("doesn't count buttons, checkboxes and radios as typing", () => {
    expect(isTextEntry({ tagName: "BUTTON" })).toBe(false);
    expect(isTextEntry({ tagName: "INPUT", type: "checkbox" })).toBe(false);
    expect(isTextEntry({ tagName: "INPUT", type: "radio" })).toBe(false);
    expect(isTextEntry({ tagName: "INPUT" })).toBe(true);
    expect(isTextEntry(null)).toBe(false);
  });
});

describe("other guards", () => {
  it("leaves events a component already handled", () => {
    const { keymap } = setup();
    const help = vi.fn();
    keymap.root.register("help.keys", help);
    keymap.handle(press("?", { defaultPrevented: true }));
    expect(help).not.toHaveBeenCalled();
  });

  it("ignores auto-repeat unless the binding moves through a list", () => {
    const { keymap } = setup();
    const keep = vi.fn();
    const move = vi.fn();
    const review = keymap.pushScope("review");
    review.register("review.keep", keep);
    review.register("review.move", move);
    keymap.handle(press("k", { repeat: true }));
    keymap.handle(press("ArrowDown", { repeat: true }));
    expect(keep).not.toHaveBeenCalled();
    expect(move).toHaveBeenCalledOnce();
  });

  it("never binds Forget", () => {
    const { keymap } = setup();
    const forget = vi.fn();
    keymap.root.register("memory.forget", forget);
    for (const k of ["f", "Delete", "Backspace", "d"]) {
      keymap.handle(press(k));
      keymap.handle(press(k, { metaKey: true }));
    }
    expect(forget).not.toHaveBeenCalled();
  });
});
