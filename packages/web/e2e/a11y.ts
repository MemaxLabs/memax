import type { Page } from "@playwright/test";

// Checks written out in place of axe (not a dependency here), for pages
// in a real browser: what the eye and the finger meet, not the markup.

/**
 * Text below WCAG AA contrast (4.5:1, 3:1 at 24px or 18.66px bold)
 * against what is really behind it: backgrounds composited down to the
 * page, and the opacity of every ancestor. Pure decoration (a separator
 * hidden from assistive technology, with no letter or digit), disabled
 * controls and text nobody can see are exempt, as WCAG 1.4.3 says.
 */
export async function lowContrast(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    type RGBA = [number, number, number, number];
    const parse = (value: string): RGBA | null => {
      const m = value.match(/rgba?\(([^)]+)\)/);
      if (m) {
        const [r, g, b, a = "1"] = m[1]!.split(/[\s,/]+/).filter(Boolean);
        return [Number(r), Number(g), Number(b), Number(a)];
      }
      const c = value.match(/color\(srgb ([^)]+)\)/);
      if (c) {
        const [r, g, b, a = "1"] = c[1]!.split(/[\s/]+/).filter(Boolean);
        return [Number(r) * 255, Number(g) * 255, Number(b) * 255, Number(a)];
      }
      return null;
    };
    const over = (top: RGBA, bottom: RGBA): RGBA => {
      const a = top[3] + bottom[3] * (1 - top[3]);
      if (a === 0) return [0, 0, 0, 0];
      const mix = (i: number) =>
        (top[i]! * top[3] + bottom[i]! * bottom[3] * (1 - top[3])) / a;
      return [mix(0), mix(1), mix(2), a];
    };
    const luminance = ([r, g, b]: RGBA) => {
      const ch = (v: number) => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
    };
    const ratio = (a: RGBA, b: RGBA) => {
      const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
      return (hi! + 0.05) / (lo! + 0.05);
    };
    const backdrop = (el: Element): RGBA => {
      const layers: RGBA[] = [];
      for (let node: Element | null = el; node; node = node.parentElement) {
        const bg = parse(getComputedStyle(node).backgroundColor);
        if (bg && bg[3] > 0) {
          layers.push(bg);
          if (bg[3] >= 1) break;
        }
      }
      let colour: RGBA = [255, 255, 255, 1];
      for (const layer of layers.reverse()) colour = over(layer, colour);
      return colour;
    };
    const opacity = (el: Element) => {
      let o = 1;
      for (let node: Element | null = el; node; node = node.parentElement) {
        o *= Number(getComputedStyle(node).opacity);
      }
      return o;
    };
    const failures: string[] = [];
    const walker = document.createTreeWalker(
      document.body,
      NodeFilter.SHOW_TEXT,
    );
    const seen = new Set<Element>();
    for (let text = walker.nextNode(); text; text = walker.nextNode()) {
      const el = text.parentElement;
      if (!el || seen.has(el) || !text.textContent?.trim()) continue;
      seen.add(el);
      if (el.closest("svg, [aria-disabled='true'], :disabled, nextjs-portal")) {
        continue;
      }
      if (
        el.closest("[aria-hidden='true']") &&
        !/[\p{L}\p{N}]/u.test(text.textContent)
      ) {
        continue;
      }
      const box = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      if (box.width <= 1 || box.height <= 1 || style.visibility !== "visible") {
        continue;
      }
      // Only what's on screen: a phone's page is one screen at a time.
      if (box.bottom <= 0 || box.top >= innerHeight) continue;
      const fg = parse(style.color);
      if (!fg) continue;
      const alpha = opacity(el);
      if (alpha === 0) continue;
      const bg = backdrop(el);
      const shown: RGBA = over([fg[0], fg[1], fg[2], fg[3] * alpha], bg);
      const size = parseFloat(style.fontSize);
      const large =
        size >= 24 || (size >= 18.66 && Number(style.fontWeight) >= 700);
      const need = large ? 3 : 4.5;
      const got = ratio(shown, bg);
      if (got < need) {
        failures.push(
          `${got.toFixed(2)} < ${need}: "${text.textContent.trim().slice(0, 40)}"`,
        );
      }
    }
    return failures;
  });
}

/**
 * Controls on screen a finger can't hit squarely: under 44px either way,
 * counting the invisible hit area Ledger draws around a small control
 * (an absolutely placed ::after).
 */
export async function smallTargets(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const out: string[] = [];
    const controls = document.querySelectorAll(
      "button, a[href], [role='radio'], input, select, textarea",
    );
    for (const el of controls) {
      const box = el.getBoundingClientRect();
      if (box.width === 0 || box.height === 0) continue;
      if (box.bottom <= 0 || box.top >= innerHeight) continue;
      if (el.closest(".mx-sr, .mx-skip, nextjs-portal, [inert]")) continue;
      let w = box.width;
      let h = box.height;
      for (const pseudo of ["::after", "::before"]) {
        const s = getComputedStyle(el, pseudo);
        if (s.content !== "none" && s.position === "absolute") {
          w = Math.max(w, parseFloat(s.width) || 0);
          h = Math.max(h, parseFloat(s.height) || 0);
        }
      }
      if (w < 44 || h < 44) {
        const name = (el.getAttribute("aria-label") ?? el.textContent ?? "")
          .trim()
          .slice(0, 40);
        out.push(`${Math.round(w)}×${Math.round(h)} ${el.tagName} "${name}"`);
      }
    }
    return out;
  });
}

/**
 * Controls laid out but not shown: invisible or untouchable until a hover
 * that a touch screen never has.
 */
export async function hiddenUntilHover(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const out: string[] = [];
    for (const el of document.querySelectorAll("button, a[href]")) {
      const box = el.getBoundingClientRect();
      if (box.width === 0 || box.height === 0) continue;
      if (el.closest(".mx-sr, .mx-skip, nextjs-portal")) continue;
      for (let node: Element | null = el; node; node = node.parentElement) {
        const s = getComputedStyle(node);
        if (
          Number(s.opacity) === 0 ||
          s.visibility === "hidden" ||
          s.pointerEvents === "none"
        ) {
          out.push(
            `${el.tagName} "${(el.textContent ?? "").trim().slice(0, 40)}"`,
          );
          break;
        }
      }
    }
    return out;
  });
}

/** Animations still running (under reduced motion there should be none). */
export async function runningAnimations(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    document
      .getAnimations()
      .filter((a) => a.playState === "running")
      .map((a) => (a as CSSAnimation).animationName ?? "transition"),
  );
}
