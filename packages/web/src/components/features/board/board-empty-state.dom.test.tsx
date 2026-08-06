// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";

import { BoardComposer, BoardEmptyState } from "./board-custom-boards";

describe("BoardEmptyState", () => {
  afterEach(cleanup);

  it("shows the ✦ pitch and the three watch-one-thing example chips", () => {
    render(<BoardEmptyState onPickExample={() => {}} />);

    // (a) the pitch — what will appear here and when.
    expect(screen.getByText(/The board is quiet/)).toBeTruthy();
    expect(
      screen.getByText(/Your first cards arrive after the next dream/),
    ).toBeTruthy();
    // (b) the creation block + its example chips.
    expect(screen.getByText("Have memax watch one thing for you")).toBeTruthy();
    expect(screen.getByText("Fitness & sleep")).toBeTruthy();
    expect(screen.getByText("Project cadence")).toBeTruthy();
    expect(screen.getByText("Learning progress")).toBeTruthy();
  });

  it("chip tap hands back the full example instruction", () => {
    const onPickExample = vi.fn();
    render(<BoardEmptyState onPickExample={onPickExample} />);

    fireEvent.click(screen.getByText("Fitness & sleep"));
    expect(onPickExample).toHaveBeenCalledWith({
      title: "Fitness & sleep",
      instruction: expect.stringContaining("workout and sleep"),
    });
  });

  it("BoardComposer mounts pre-filled from an example and stays editable", () => {
    render(
      <BoardComposer
        pending={false}
        onCancel={() => {}}
        onCreate={() => {}}
        initialTitle="Fitness & sleep"
        initialInstruction="Keep an eye on my workout and sleep records."
      />,
    );

    const title = screen.getByLabelText("Board name") as HTMLInputElement;
    const instruction = screen.getByLabelText(
      "Standing instruction",
    ) as HTMLTextAreaElement;
    expect(title.value).toBe("Fitness & sleep");
    expect(instruction.value).toBe(
      "Keep an eye on my workout and sleep records.",
    );

    fireEvent.change(title, { target: { value: "Sleep only" } });
    expect(title.value).toBe("Sleep only");
  });
});
