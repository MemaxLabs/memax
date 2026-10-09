// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { AgentStamp } from "../provenance/agent-stamp";
import { StateMark } from "../memory/state-mark";
import { LedgerProvider, useLedger, type LedgerLocale } from "./provider";

function Probe() {
  const { locale, formatNumber, formatList, strings } = useLedger();
  return (
    <output>
      {locale}|{formatNumber(1204)}|{formatList(["1", "2", "3"], "disjunction")}
      |{strings.review.keep}
    </output>
  );
}

describe("LedgerProvider", () => {
  it("defaults to English and plain anchors without a provider", () => {
    render(<Probe />);
    expect(screen.getByRole("status").textContent).toBe(
      "en|1,204|1, 2 or 3|Keep",
    );
  });

  it("switches every string between en and zh", () => {
    function Harness() {
      const [locale, setLocale] = useState<LedgerLocale>("en");
      return (
        <LedgerProvider locale={locale}>
          <button
            type="button"
            onClick={() => setLocale(locale === "en" ? "zh" : "en")}
          >
            switch
          </button>
          <StateMark state="proposed" />
          <Probe />
        </LedgerProvider>
      );
    }
    render(<Harness />);
    expect(screen.getByText("Proposed")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "switch" }));
    expect(screen.queryByText("Proposed")).toBeNull();
    expect(screen.getByText("提议")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe(
      "zh|1,204|1、2或3|保留",
    );
    fireEvent.click(screen.getByRole("button", { name: "switch" }));
    expect(screen.getByText("Proposed")).toBeTruthy();
  });

  it("renders zh with Chinese list and number formatting", () => {
    render(
      <LedgerProvider locale="zh">
        <StateMark state="proposed" />
        <Probe />
      </LedgerProvider>,
    );
    expect(screen.getByText("提议")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe(
      "zh|1,204|1、2或3|保留",
    );
  });

  it("merges string overrides over the locale", () => {
    render(
      <LedgerProvider
        locale="zh"
        strings={{ review: { keep: "收下" }, state: { proposed: "待定" } }}
      >
        <StateMark state="proposed" />
        <StateMark state="kept" />
        <Probe />
      </LedgerProvider>,
    );
    expect(screen.getByText("待定")).toBeTruthy();
    expect(screen.getByText("已保留")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toContain("|收下");
  });

  it("extends the agent registry", () => {
    render(
      <LedgerProvider
        agents={{ windsurf: { mono: "WS", name: "Windsurf", surface: "ide" } }}
      >
        <AgentStamp agent="windsurf" showName surface />
        <AgentStamp agent="codex" showName />
      </LedgerProvider>,
    );
    expect(screen.getByText("WS")).toBeTruthy();
    expect(screen.getByText("IDE")).toBeTruthy();
    expect(screen.getByText("CX")).toBeTruthy();
  });
});
