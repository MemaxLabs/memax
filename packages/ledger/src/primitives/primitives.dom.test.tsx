// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { createRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { LedgerProvider, type LedgerLinkProps } from "../i18n/provider";
import { Button } from "./button";
import { Field } from "./field";
import { Segmented } from "./segmented";

const AUTONOMY = [
  { value: "read", label: "Read", hint: "Reads context. Never writes." },
  { value: "propose", label: "Propose" },
  { value: "write", label: "Write" },
] as const;

describe("Segmented", () => {
  it("is a radio group with one tab stop on the checked option", () => {
    render(
      <Segmented label="Autonomy" defaultValue="propose" options={AUTONOMY} />,
    );
    const group = screen.getByRole("radiogroup", { name: "Autonomy" });
    const radios = screen.getAllByRole("radio");
    expect(radios.every((radio) => group.contains(radio))).toBe(true);
    expect(radios.map((r) => r.getAttribute("aria-checked"))).toEqual([
      "false",
      "true",
      "false",
    ]);
    expect(radios.map((r) => r.tabIndex)).toEqual([-1, 0, -1]);
    expect(radios[1]).toHaveProperty("className", "mx-seg-opt is-on");
    expect(radios[0]!.title).toBe("Reads context. Never writes.");
  });

  it("moves focus and selection with the arrow keys, wrapping, and Home/End", () => {
    const onChange = vi.fn();
    render(
      <Segmented
        label="Autonomy"
        defaultValue="propose"
        options={AUTONOMY}
        onChange={onChange}
      />,
    );
    const [read, propose, write] = screen.getAllByRole("radio");
    propose!.focus();
    fireEvent.keyDown(propose!, { key: "ArrowRight" });
    expect(document.activeElement).toBe(write);
    expect(write!.getAttribute("aria-checked")).toBe("true");
    expect(write!.tabIndex).toBe(0);
    fireEvent.keyDown(write!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(read);
    fireEvent.keyDown(read!, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(write);
    fireEvent.keyDown(write!, { key: "Home" });
    expect(document.activeElement).toBe(read);
    fireEvent.keyDown(read!, { key: "End" });
    expect(document.activeElement).toBe(write);
    expect(onChange.mock.calls.map(([v]) => v)).toEqual([
      "write",
      "read",
      "write",
      "read",
      "write",
    ]);
  });

  it("ignores modified arrows and other keys", () => {
    const onChange = vi.fn();
    render(
      <Segmented label="Autonomy" options={AUTONOMY} onChange={onChange} />,
    );
    const [read] = screen.getAllByRole("radio");
    fireEvent.keyDown(read!, { key: "ArrowRight", metaKey: true });
    fireEvent.keyDown(read!, { key: "a" });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("selects on click and fires onChange only on a change", () => {
    const onChange = vi.fn();
    render(
      <Segmented
        label="Autonomy"
        defaultValue="read"
        options={AUTONOMY}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("radio", { name: "Read" }));
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("radio", { name: "Write" }));
    expect(onChange).toHaveBeenCalledWith("write");
    expect(
      screen.getByRole("radio", { name: "Write" }).getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("keeps a disabled option with a reason hoverable, and ignores it", () => {
    const onChange = vi.fn();
    render(
      <Segmented
        label="Autonomy"
        defaultValue="read"
        options={[
          { value: "read", label: "Read" },
          { value: "propose", label: "Propose", disabled: true },
          {
            value: "write",
            label: "Write",
            disabled: true,
            disabledReason: "API keys propose at most.",
          },
        ]}
        onChange={onChange}
      />,
    );
    const propose = screen.getByRole("radio", { name: "Propose" });
    const write = screen.getByRole("radio", { name: "Write" });
    // Without a reason: natively disabled. With one: aria-disabled and titled.
    expect(propose.hasAttribute("disabled")).toBe(true);
    expect(write.hasAttribute("disabled")).toBe(false);
    expect(write.getAttribute("aria-disabled")).toBe("true");
    expect(write.title).toBe("API keys propose at most.");
    expect(write.tabIndex).toBe(-1);
    fireEvent.click(write);
    const read = screen.getByRole("radio", { name: "Read" });
    read.focus();
    fireEvent.keyDown(read, { key: "ArrowRight" });
    expect(document.activeElement).toBe(read);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("follows a controlled value and leaves it to the parent", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <Segmented
        label="Autonomy"
        value="read"
        options={AUTONOMY}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("radio", { name: "Write" }));
    expect(onChange).toHaveBeenCalledWith("write");
    expect(
      screen.getByRole("radio", { name: "Read" }).getAttribute("aria-checked"),
    ).toBe("true");
    rerender(
      <Segmented
        label="Autonomy"
        value="write"
        options={AUTONOMY}
        onChange={onChange}
      />,
    );
    expect(
      screen.getByRole("radio", { name: "Write" }).getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("skips disabled options", () => {
    render(
      <Segmented
        label="Filter"
        defaultValue="all"
        options={[
          { value: "all", label: "All" },
          { value: "c", label: "Conflicts", disabled: true },
          { value: "e", label: "External" },
        ]}
      />,
    );
    const [all, , external] = screen.getAllByRole("radio");
    all!.focus();
    fireEvent.keyDown(all!, { key: "ArrowRight" });
    expect(document.activeElement).toBe(external);
  });

  it("works as Ask / Remember inside a form-like flow", () => {
    function Harness() {
      const [mode, setMode] = useState<"ask" | "remember">("ask");
      return (
        <>
          <Segmented
            label="Mode"
            value={mode}
            onChange={setMode}
            options={[
              { value: "ask", label: "Ask" },
              { value: "remember", label: "Remember" },
            ]}
          />
          <output>{mode}</output>
        </>
      );
    }
    render(<Harness />);
    const ask = screen.getByRole("radio", { name: "Ask" });
    ask.focus();
    fireEvent.keyDown(ask, { key: "ArrowRight" });
    expect(screen.getByRole("status").textContent).toBe("remember");
  });
});

describe("Button", () => {
  it("is a real button of type button by default, with its keycap as a shortcut", () => {
    const onClick = vi.fn();
    render(
      <Button variant="keep" kbd="K" onClick={onClick}>
        Keep
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Keep" });
    expect(button.tagName).toBe("BUTTON");
    expect(button.getAttribute("type")).toBe("button");
    expect(button.getAttribute("aria-keyshortcuts")).toBe("K");
    expect(button.className).toContain("mx-btn--keep");
    expect(button.querySelector("kbd")?.getAttribute("aria-hidden")).toBe(
      "true",
    );
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("renders as a link with href, through the provider's link component", () => {
    function AppLink(props: LedgerLinkProps) {
      return <a data-app-link="" {...props} />;
    }
    render(
      <LedgerProvider linkComponent={AppLink}>
        <Button variant="primary" href="/memax-v2/review" icon="arrow-right">
          Start review
        </Button>
      </LedgerProvider>,
    );
    const link = screen.getByRole("link", { name: "Start review" });
    expect(link.getAttribute("href")).toBe("/memax-v2/review");
    expect(link.hasAttribute("data-app-link")).toBe(true);
    expect(link.hasAttribute("type")).toBe(false);
    expect(link.className).toContain("mx-btn mx-btn--primary");
  });

  it("renders through a render element, merging props", () => {
    const onClick = vi.fn();
    render(
      <Button
        render={<a href="#forget" data-x="1" />}
        variant="danger"
        onClick={onClick}
      >
        Forget
      </Button>,
    );
    const link = screen.getByRole("link", { name: "Forget" });
    expect(link.getAttribute("data-x")).toBe("1");
    expect(link.className).toContain("mx-btn--danger");
    fireEvent.click(link);
    expect(onClick).toHaveBeenCalled();
  });

  it("passes its ref to the element", () => {
    const ref = createRef<HTMLElement>();
    render(<Button ref={ref}>Edit</Button>);
    expect(ref.current?.tagName).toBe("BUTTON");
  });

  it("is natively disabled without a reason", () => {
    render(<Button disabled>Keep</Button>);
    expect(screen.getByRole("button", { name: "Keep" })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("stays focusable with a reason, says why, and ignores presses", () => {
    const onClick = vi.fn();
    render(
      <Button
        variant="keep"
        disabled
        disabledReason="Choose 1, 2 or 3"
        onClick={onClick}
      >
        Answer
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Answer" });
    expect(button).toHaveProperty("disabled", false);
    expect(button.getAttribute("aria-disabled")).toBe("true");
    expect(button.title).toBe("Choose 1, 2 or 3");
    button.focus();
    expect(document.activeElement).toBe(button);
    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("ignores presses while pending, without a spinner", () => {
    const onClick = vi.fn();
    render(
      <Button pending onClick={onClick}>
        Keep
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Keep" });
    expect(button.getAttribute("aria-busy")).toBe("true");
    expect(button.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
    expect(button.querySelector("svg")).toBeNull();
  });

  it("is icon-only with an aria-label", () => {
    render(<Button icon="sync" aria-label="Recompile" />);
    const button = screen.getByRole("button", { name: "Recompile" });
    expect(button.className).toContain("mx-btn--icon");
  });
});

describe("Field", () => {
  it("labels its input and describes it with the hint", () => {
    const ref = createRef<HTMLInputElement>();
    render(
      <Field
        ref={ref}
        label="Space name"
        hint="Agents see this name in receipts."
        defaultValue="memax-v2"
      />,
    );
    const input = screen.getByRole("textbox", { name: "Space name" });
    expect(ref.current).toBe(input);
    expect(input).toHaveProperty("value", "memax-v2");
    const describedBy = input.getAttribute("aria-describedby")!;
    expect(document.getElementById(describedBy)?.textContent).toBe(
      "Agents see this name in receipts.",
    );
    expect(input.hasAttribute("aria-invalid")).toBe(false);
  });

  it("replaces the hint with the error and marks the input invalid", () => {
    render(
      <Field
        label="Stale after"
        hint="In days."
        error="Must be between 7 and 365 days."
      />,
    );
    const input = screen.getByRole("textbox", { name: "Stale after" });
    expect(input.getAttribute("aria-invalid")).toBe("true");
    const describedBy = input.getAttribute("aria-describedby")!;
    expect(document.getElementById(describedBy)?.textContent).toBe(
      "Must be between 7 and 365 days.",
    );
    expect(screen.queryByText("In days.")).toBeNull();
    expect(input.closest(".mx-field")?.className).toContain("is-error");
  });

  it("keeps a caller's aria-describedby and gives labels that aren't ASCII unique ids", () => {
    render(
      <>
        <span id="extra">Extra</span>
        <Field label="空间名称" hint="一" aria-describedby="extra" />
        <Field label="空间名称" />
      </>,
    );
    const [first, second] = screen.getAllByRole("textbox", {
      name: "空间名称",
    });
    expect(first!.id).not.toBe(second!.id);
    expect(first!.getAttribute("aria-describedby")).toMatch(/^extra /);
  });
});
