// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PREVIEWS } from "./index";

// Every component preview, in both themes and both locales: it renders, every
// control has an accessible name, and no text has a space before punctuation
// (the board defect from design review §2).

const NAMED_ROLES = [
  "button",
  "link",
  "radio",
  "radiogroup",
  "textbox",
  "img",
  "navigation",
  "region",
  "dialog",
];

/** The text of every block and receipt line, where the defect would show. */
function blockTexts(root: HTMLElement): string[] {
  const blocks = root.querySelectorAll(
    "p, li, h1, h2, h3, h4, h5, blockquote, figcaption, dd, .mx-meta, .mx-receipt, .mx-state",
  );
  return [...blocks].map((el) => el.textContent ?? "");
}

describe("previews", () => {
  it("cover all 27 components, once each", () => {
    expect(PREVIEWS).toHaveLength(27);
    expect(new Set(PREVIEWS.map((p) => p.name)).size).toBe(27);
  });

  describe.each(PREVIEWS.map((p) => [p.name, p] as const))(
    "%s",
    (_, preview) => {
      it.each(["light", "dark"] as const)(
        "renders in %s with data-theme",
        (theme) => {
          const { container } = render(<preview.Component theme={theme} />);
          const frame = container.firstElementChild as HTMLElement;
          expect(frame.dataset.theme).toBe(theme);
          expect(frame.className).toContain("pv-frame");
          expect(frame.getAttribute("lang")).toBe("en");
          expect(frame.textContent?.length).toBeGreaterThan(0);
        },
      );

      it("gives every control an accessible name", () => {
        render(<preview.Component />);
        for (const role of NAMED_ROLES) {
          const all = screen.queryAllByRole(role);
          const named = screen.queryAllByRole(role, { name: /\S/ });
          expect(named.length, `${role} without a name`).toBe(all.length);
        }
      });

      it("never puts a space before punctuation", () => {
        const { container } = render(<preview.Component />);
        for (const text of blockTexts(container)) {
          expect(/\S\s+[.,;:](\s|$)/.test(text), text).toBe(false);
        }
      });

      it("renders under zh with lang=zh-CN", () => {
        const { container } = render(
          <preview.Component locale="zh" theme="dark" />,
        );
        expect(container.firstElementChild?.getAttribute("lang")).toBe("zh-CN");
      });
    },
  );
});
