import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { keycaps, parseKeys } from "./keys";
import { bindingFor, KEY_GROUPS, KEYMAP, type KeyBinding } from "./registry";

const bindings = KEYMAP as readonly KeyBinding[];

describe("the keyboard map", () => {
  it("has the board's three columns, in order, plus page keys", () => {
    const groups = [...new Set(bindings.map((b) => b.group))];
    expect(groups).toEqual(KEY_GROUPS);
    expect(
      bindings.filter((b) => b.group === "everywhere").map((b) => b.id),
    ).toEqual([
      "command.open",
      "go.today",
      "go.review",
      "go.brief",
      "go.memories",
      "space.switch",
      "help.keys",
    ]);
  });

  it("matches HANDOFF §7's keys", () => {
    const caps = (id: Parameters<typeof bindingFor>[0]) =>
      bindingFor(id).keys?.map((spec) => keycaps(spec, "mac").join(" "));
    expect(caps("command.open")).toEqual(["⌘K"]);
    expect(caps("go.today")).toEqual(["G T"]);
    expect(caps("go.review")).toEqual(["G R"]);
    expect(caps("go.brief")).toEqual(["G B"]);
    expect(caps("go.memories")).toEqual(["G M"]);
    expect(caps("space.switch")?.[0]).toBe("⌘1");
    expect(caps("space.switch")?.at(-1)).toBe("⌘9");
    expect(caps("help.keys")).toEqual(["?"]);
    expect(caps("review.move")).toEqual(["↓", "↑"]);
    expect(caps("review.keep")).toEqual(["K"]);
    expect(caps("review.edit")).toEqual(["E"]);
    expect(caps("review.reject")).toEqual(["X"]);
    expect(caps("review.compare")).toEqual(["C"]);
    expect(caps("review.source")).toEqual(["O"]);
    expect(caps("undo")).toEqual(["⌘Z"]);
    expect(caps("memory.edit")).toEqual(["E"]);
    expect(caps("memory.verify")).toEqual(["V"]);
    expect(caps("memory.cite")).toEqual(["⌘⇧C"]);
    expect(caps("memory.compile")).toEqual(["⌘⇧S"]);
    expect(caps("memory.handoff")).toEqual(["H"]);
    expect(caps("today.review")).toEqual(["R"]);
    expect(caps("command.keep")).toEqual(["⌘↵"]);
  });

  it("binds Forget to nothing, on purpose", () => {
    expect(bindingFor("memory.forget").keys).toBeNull();
    for (const binding of bindings) {
      expect(binding.id.includes("forget") && binding.keys !== null).toBe(
        false,
      );
    }
  });

  it("lets only ⌘/Ctrl chords fire while typing", () => {
    for (const binding of bindings.filter((b) => b.inInput)) {
      for (const spec of binding.keys ?? []) {
        const sequence = parseKeys(spec);
        expect(sequence, binding.id).toHaveLength(1);
        expect(sequence[0].mod, binding.id).toBe(true);
      }
    }
  });

  it("gives every binding and group a label in English and Chinese", () => {
    for (const catalogue of [en, zh]) {
      const keys = catalogue.ledger.app.keys;
      for (const binding of bindings) {
        expect(
          keys.actions[binding.id as keyof typeof keys.actions],
          binding.id,
        ).toBeTruthy();
      }
      for (const group of KEY_GROUPS) expect(keys.groups[group]).toBeTruthy();
    }
    expect(Object.keys(en.ledger.app.keys.actions).sort()).toEqual(
      bindings.map((b) => b.id).sort(),
    );
  });

  it("never gives two everywhere-keys the same chord", () => {
    const seen = new Map<string, string>();
    for (const binding of bindings.filter((b) =>
      ["everywhere", "pages"].includes(b.group),
    )) {
      for (const spec of binding.keys ?? []) {
        const key = keycaps(spec, "mac").join(" ");
        expect(seen.get(key), `${key}: ${binding.id}`).toBeUndefined();
        seen.set(key, binding.id);
      }
    }
  });
});
