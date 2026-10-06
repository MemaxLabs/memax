// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import type { RememberCheck } from "@/lib/v2/data/types";
import type { useRemember } from "../_lib/use-remember";
import { RememberPanel } from "./remember-panel";

// Remember's near-duplicate line (Remember.png): an agent's proposal is
// offered to keep instead, as drawn; a person's proposal too, without an
// agent to name; a memory that's already kept is pointed out, with
// nothing to keep instead.

afterEach(cleanup);

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

function draft(
  duplicate: RememberCheck["duplicate"],
): ReturnType<typeof useRemember> {
  return {
    text: "Pin shared dependency versions with the pnpm catalog.",
    space: v2,
    setSpace: vi.fn(),
    section: "conventions",
    setSection: vi.fn(),
    duplicate,
    condition: null,
    pending: false,
    keep: vi.fn(),
    keepDuplicate: vi.fn(),
  };
}

function renderPanel(duplicate: RememberCheck["duplicate"]) {
  return render(
    <LocaleProvider>
      <LedgerProvider locale="en">
        <RememberPanel
          draft={draft(duplicate)}
          homeSlug="memax-v2"
          spaces={[v2]}
          viewer={null}
          keepKey="⌘↵"
          commandKey="⌘K"
          onKeep={vi.fn()}
          onKeepDuplicate={vi.fn()}
        />
      </LedgerProvider>
    </LocaleProvider>,
  );
}

describe("Remember's near-duplicate", () => {
  it("offers an agent's proposal, as drawn", () => {
    renderPanel({
      ref: "M-0432",
      lifecycle: "proposed",
      agent: "codex",
      writtenAt: "2026-10-05T14:18:00Z",
      match: "near",
    });
    expect(screen.getByRole("status").textContent).toContain(
      "proposed nearly the same thing",
    );
    expect(screen.getByRole("button", { name: /Keep M-0432/ })).toBeTruthy();
  });

  it("offers a person's proposal without naming an agent", () => {
    renderPanel({
      ref: "M-0433",
      lifecycle: "proposed",
      agent: null,
      writtenAt: "2026-10-05T14:18:00Z",
      match: "exact",
    });
    expect(screen.getByRole("status").textContent).toContain(
      "Nearly the same thing was proposed",
    );
    expect(screen.getByRole("button", { name: /Keep M-0433/ })).toBeTruthy();
  });

  it("points out a kept memory, with nothing to keep instead", () => {
    renderPanel({
      ref: "M-0219",
      lifecycle: "kept",
      agent: null,
      writtenAt: "2026-10-01T09:00:00Z",
      match: "near",
    });
    const status = screen.getByRole("status").textContent ?? "";
    expect(status).toContain("Nearly the same thing was kept");
    expect(status).toContain("M-0219");
    expect(screen.queryByRole("button", { name: /Keep M-0219/ })).toBeNull();
  });
});
