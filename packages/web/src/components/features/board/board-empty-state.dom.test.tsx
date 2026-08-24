// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";

import {
  BoardComposer,
  BoardEmptyState,
  BoardGhostCard,
} from "./board-custom-boards";

describe("BoardEmptyState", () => {
  afterEach(cleanup);

  it("shows the ✦ pitch and the three example chips", () => {
    render(<BoardEmptyState onPickExample={() => {}} />);

    // (a) the pitch — what will appear here and when.
    expect(screen.getByText(/No cards yet/)).toBeTruthy();
    expect(
      screen.getByText(/Your first cards appear after the next pass/),
    ).toBeTruthy();
    // (b) the chips block — feeds the ghost composer below the stream.
    expect(screen.getByText("Start from an example")).toBeTruthy();
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

describe("BoardGhostCard", () => {
  afterEach(cleanup);

  it("rests as a dashed ghost, morphs in place into the composer, and saves", () => {
    const onCreate = vi.fn();
    render(
      <BoardGhostCard
        pending={false}
        onCreate={onCreate}
        prefill={null}
        onPrefillConsumed={() => {}}
      />,
    );

    // At rest: the latent-card affordance, no form fields mounted.
    const ghost = screen.getByText("Have memax watch one thing");
    expect(screen.queryByLabelText("Board name")).toBeNull();

    // Tap → the composer takes the ghost's place (no modal).
    fireEvent.click(ghost);
    const title = screen.getByLabelText("Board name") as HTMLInputElement;
    const instruction = screen.getByLabelText(
      "Standing instruction",
    ) as HTMLTextAreaElement;
    expect(screen.queryByText("Have memax watch one thing")).toBeNull();

    fireEvent.change(title, { target: { value: "Competitor moves" } });
    fireEvent.change(instruction, {
      target: { value: "Watch for competitor launches." },
    });
    fireEvent.click(screen.getByText("Create board"));
    expect(onCreate).toHaveBeenCalledWith({
      title: "Competitor moves",
      instruction: "Watch for competitor launches.",
    });
  });

  it("Esc morphs the composer back into the ghost", () => {
    render(
      <BoardGhostCard
        pending={false}
        onCreate={() => {}}
        prefill={null}
        onPrefillConsumed={() => {}}
      />,
    );
    fireEvent.click(screen.getByText("Have memax watch one thing"));
    fireEvent.keyDown(screen.getByLabelText("Board name"), { key: "Escape" });
    expect(screen.queryByLabelText("Board name")).toBeNull();
    expect(screen.getByText("Have memax watch one thing")).toBeTruthy();
  });

  it("an example-chip prefill snaps the ghost open with the copy loaded", () => {
    render(
      <BoardGhostCard
        pending={false}
        onCreate={() => {}}
        prefill={{
          title: "Fitness & sleep",
          instruction: "Keep an eye on my workout and sleep records.",
        }}
        onPrefillConsumed={() => {}}
      />,
    );
    const title = screen.getByLabelText("Board name") as HTMLInputElement;
    expect(title.value).toBe("Fitness & sleep");
  });
});
