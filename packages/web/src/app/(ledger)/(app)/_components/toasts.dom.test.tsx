// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import { useEffect, useRef } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { LedgerProvider } from "@memaxlabs/ledger";
import {
  ToastProvider,
  ToastViewport,
  useToast,
  type ShowToast,
} from "./toasts";

// Ledger's marks mean something (seal green = a person kept it, ochre =
// waiting on you), so a toast carries a mark only when it means one.

afterEach(cleanup);

function Show({ toast }: { toast: ShowToast }) {
  const show = useToast();
  const shown = useRef(false);
  useEffect(() => {
    if (shown.current) return;
    shown.current = true;
    show(toast);
  }, [show, toast]);
  return null;
}

function renderToast(toast: ShowToast) {
  return render(
    <LedgerProvider locale="en">
      <ToastProvider>
        <Show toast={toast} />
        <ToastViewport />
      </ToastProvider>
    </LedgerProvider>,
  );
}

describe("toast marks", () => {
  it("shows no mark for neutral news", async () => {
    const { container } = renderToast({ text: "Copied." });
    await act(async () => {});
    expect(screen.getByText("Copied.")).toBeTruthy();
    expect(container.ownerDocument.querySelector(".mx-state")).toBeNull();
  });

  it("shows the mark it's given", async () => {
    const { container } = renderToast({ state: "kept", text: "Kept M-0430." });
    await act(async () => {});
    expect(screen.getByText("Kept M-0430.")).toBeTruthy();
    expect(container.ownerDocument.querySelector(".mx-state")).not.toBeNull();
  });
});
