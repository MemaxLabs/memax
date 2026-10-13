// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Menu } from "@base-ui/react/menu";
import { NavRail } from "@memaxlabs/ledger";
import { CommandFailedError } from "@/lib/v2/data/command-error";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { MachinesSource } from "@/lib/v2/data/machines";
import type { ReviewCardData, ReviewItem } from "@/lib/v2/data/review";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import type { TargetsSource, TargetView } from "@/lib/v2/data/targets";
import { useRailProps } from "../_components/rail-props";
import { useOverview } from "../_lib/data";
import { BriefPlace } from "./brief";
import { DriftPlace } from "./drift";
import { MemoryPlace } from "./memory";
import { ConnectAgentDialog } from "./agents/connect-dialog";
import { useRecordsView } from "./records-view";
import { Touches } from "./review/touches";
import { TargetPlace } from "./target";
import { TodayPlace } from "./today";
import { PERSONAL_SPACE, renderPlace, TEST_SPACE } from "./test-frame";

// A Personal space written to two machines (demo-machines-data.ts):
// ziyang-mbp, online, with a hand edit in Codex's block; home-linux,
// offline since Friday, a compile behind, its canonical copy stopped.
// Its files are listed once each across the machines, each machine's
// copy has its page with a switcher, and the machines can be renamed and
// unlinked, each command with one key per intent.

const h = vi.hoisted(() => ({ source: null as LedgerDataSource | null }));

vi.mock("@/lib/v2/data/demo-source", async (load) => {
  const actual = await load<typeof import("@/lib/v2/data/demo-source")>();
  return {
    ...actual,
    get demoSource() {
      return h.source ?? actual.demoSource;
    },
  };
});
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  usePathname: () => "/personal/brief",
  useParams: () => ({ space: "personal" }),
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
  push.mockReset();
});

/** The demo source with its commands replaced where a test spies on them. */
function withSource({
  targets = () => ({}),
  machines = () => ({}),
}: {
  targets?: (real: TargetsSource) => Partial<TargetsSource>;
  machines?: (real: MachinesSource) => Partial<MachinesSource>;
} = {}): LedgerDataSource {
  const demo = createDemoSource({
    streamDelayMs: 0,
    commandDelayMs: 0,
    settleMs: 5,
  });
  const source = {
    ...demo,
    targets: { ...demo.targets, ...targets(demo.targets) },
    machines: { ...demo.machines, ...machines(demo.machines) },
  };
  h.source = source;
  return source;
}

const panel = (name: string) => screen.getByRole("region", { name });

function Rail() {
  const overview = useOverview(PERSONAL_SPACE);
  const nav = useRailProps({
    space: PERSONAL_SPACE,
    overview: overview.data,
    overviewFailed: overview.isError,
    route: undefined,
    inSettings: false,
    viewer: null,
  });
  return (
    <Menu.Root>
      <NavRail {...nav} />
    </Menu.Root>
  );
}

describe("Personal's compiled files", () => {
  it("lists each home file once, how it stands on every machine, opening the copy that needs you", async () => {
    withSource();
    renderPlace(<BriefPlace />, { space: PERSONAL_SPACE });
    const compiled = await screen.findByRole("region", { name: "Compiled to" });
    const links = within(compiled).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "~/.claude/rules/memax-personal.mdClaude CodeOn 1 of 2 machineshome-linux last online 3 days ago",
      "~/.codex/AGENTS.mdCodexHand edit on ziyang-mbp",
      "~/.memax/personal/AGENTS.mdCanonical copyIn sync on ziyang-mbp",
      // The drift notice's title opens both files.
      "Codex's copy on ziyang-mbp has one local edit.",
    ]);
    expect(links[0]!.getAttribute("href")).toBe(
      "/personal/brief/targets/claude-user-machine-1",
    );
    expect(links[1]!.getAttribute("href")).toBe(
      "/personal/brief/targets/codex-user-machine-1",
    );
    expect(
      within(compiled).getByText(
        /Someone changed ~\/\.codex\/AGENTS\.md by hand on ziyang-mbp today at 11:20\./,
      ),
    ).toBeTruthy();
  });

  it("says on the rail which machine has the hand edit", async () => {
    withSource();
    renderPlace(<Rail />, { space: PERSONAL_SPACE });
    const rail = screen.getByRole("navigation", { name: "Main" });
    await waitFor(() =>
      expect(within(rail).getByRole("status").textContent).toBe(
        "Hand edit on ziyang-mbp",
      ),
    );
  });

  it("counts Today's compiled context the same way", async () => {
    withSource();
    renderPlace(<TodayPlace />, { space: PERSONAL_SPACE });
    const today = await screen.findByRole("region", {
      name: "Compiled context",
    });
    expect(within(today).getAllByRole("link")).toHaveLength(3);
    expect(within(today).getByText("On 1 of 2 machines")).toBeTruthy();
  });

  it("says, with no machine linked, what to run on each computer", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    withSource({
      targets: () => ({ peekList: () => [], list: async () => [] }),
      machines: () => ({ peekList: () => [], list: async () => [] }),
    });
    renderPlace(<TodayPlace />, { space: PERSONAL_SPACE });
    const today = await screen.findByRole("region", {
      name: "Compiled context",
    });
    expect(
      within(today).getByText(
        "Your Personal space isn't on any machine yet. Run this on each computer you work on:",
      ),
    ).toBeTruthy();
    expect(within(today).getByText(/memax-cli link --home$/)).toBeTruthy();
    fireEvent.click(
      within(today).getByRole("button", { name: "Copy the command" }),
    );
    await waitFor(() => expect(writeText).toHaveBeenCalledOnce());
    expect(writeText.mock.calls[0]![0]).toMatch(/memax-cli link --home$/);
  });
});

describe("one machine's copy", () => {
  it("switches between machines, says what it is and holds a home file to 16 KB", async () => {
    const configure = vi.fn<TargetsSource["configure"]>();
    withSource({
      targets: (real) => {
        configure.mockImplementation((input) => real.configure(input));
        return { configure };
      },
    });
    renderPlace(<TargetPlace segment="claude-user-machine-1" />, {
      space: PERSONAL_SPACE,
    });
    const switcher = await screen.findByRole("navigation", {
      name: "Machines",
    });
    const machines = within(switcher).getAllByRole("link");
    expect(machines.map((m) => m.getAttribute("aria-label"))).toEqual([
      "ziyang-mbp, In sync",
      "home-linux, Offline",
    ]);
    expect(machines[0]!.getAttribute("aria-current")).toBe("page");
    expect(machines[1]!.getAttribute("href")).toBe(
      "/personal/brief/targets/claude-user-machine-2",
    );
    expect(
      screen.getByText(
        /^What Claude Code reads at the start of every session, in every project, on ziyang-mbp\./,
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Written by the memax daemon on ziyang-mbp."),
    ).toBeTruthy();
    // The other files, not this file's other machines.
    const others = panel("Other targets");
    expect(
      within(others)
        .getAllByRole("link")
        .map((l) => l.querySelector("code")?.textContent),
    ).toEqual(["~/.codex/AGENTS.md", "~/.memax/personal/AGENTS.md"]);

    const budget = screen.getByRole("textbox", { name: "Size budget" });
    fireEvent.change(budget, { target: { value: "20" } });
    fireEvent.keyDown(budget, { key: "Enter" });
    expect(await screen.findByText("Between 1 and 16 KB.")).toBeTruthy();
    expect(configure).not.toHaveBeenCalled();
  });

  it("says OpenCode reads the canonical copy on a machine whose OpenCode settings name it", async () => {
    const opencode = (t: TargetView) =>
      t.kind === "agents_md" && t.machine?.label === "machine-1"
        ? { ...t, readBy: ["opencode"] }
        : t;
    withSource({
      targets: (real) => ({
        peekList: (slug) => real.peekList?.(slug)?.map(opencode),
        list: async (input) => (await real.list(input)).map(opencode),
      }),
    });
    renderPlace(<TargetPlace segment="agents-md-machine-1" />, {
      space: PERSONAL_SPACE,
    });
    expect(
      await screen.findByText(
        "Your Personal space in one file on ziyang-mbp, for you to read and for agents that have no file of their own. OpenCode reads it in every project there: its settings name this file.",
      ),
    ).toBeTruthy();
  });

  it("stops compiling here: this file on this machine, one key per intent", async () => {
    const configure = vi.fn<TargetsSource["configure"]>();
    withSource({
      targets: (real) => {
        configure
          .mockRejectedValueOnce(
            new CommandFailedError({ kind: "unreachable" }),
          )
          .mockImplementation((input) => real.configure(input));
        return { configure };
      },
    });
    renderPlace(<TargetPlace segment="claude-user-machine-1" />, {
      space: PERSONAL_SPACE,
    });
    const stop = await screen.findByRole("button", {
      name: "Stop compiling here",
    });
    fireEvent.click(stop);
    expect(
      await screen.findByText(
        /^Memax still writes ~\/\.claude\/rules\/memax-personal\.md on ziyang-mbp\./,
      ),
    ).toBeTruthy();
    fireEvent.click(stop);
    await waitFor(() => expect(configure).toHaveBeenCalledTimes(2));
    const [first, second] = configure.mock.calls.map((c) => c[0]);
    expect(first.change).toEqual({ enabled: false });
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
    expect(
      await screen.findByText(
        "Stopped compiling ~/.claude/rules/memax-personal.md on ziyang-mbp. Memax takes it off there the next time it's online.",
      ),
    ).toBeTruthy();
    expect(
      await screen.findByText(
        "Memax stopped compiling this file on ziyang-mbp. Agents there keep reading your Personal space over MCP.",
      ),
    ).toBeTruthy();
  });

  it("shows Codex's block alone, and says the person's lines stay on the machine", async () => {
    withSource();
    renderPlace(<TargetPlace segment="codex-user-machine-1" />, {
      space: PERSONAL_SPACE,
    });
    expect(
      await screen.findByText(
        "Only Memax's block is shown. Your own lines in this file stay on ziyang-mbp; Memax doesn't read them.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("ziyang-mbp · ~/.codex/AGENTS.md")).toBeTruthy();
  });

  it("says a stopped copy on a linked machine can compile again", async () => {
    withSource();
    renderPlace(<TargetPlace segment="agents-md-machine-2" />, {
      space: PERSONAL_SPACE,
    });
    expect(
      await screen.findByText(
        "Memax stopped compiling this file on home-linux. Agents there keep reading your Personal space over MCP.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Compile it again" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Stop compiling here" }),
    ).toBeNull();
  });
});

describe("a hand edit on one machine", () => {
  it("names the machine, shows the block as it is there, and stops only there", async () => {
    const resolveDrift = vi.fn<TargetsSource["resolveDrift"]>();
    withSource({
      targets: (real) => {
        resolveDrift.mockImplementation((input) => real.resolveDrift(input));
        return { resolveDrift };
      },
    });
    renderPlace(<DriftPlace segment="codex-user-machine-1" />, {
      space: PERSONAL_SPACE,
    });
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "~/.codex/AGENTS.md has a hand edit on ziyang-mbp",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Someone changed it on ziyang-mbp today at 11:20, outside Memax. Memax won't overwrite it without you.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("On ziyang-mbp now")).toBeTruthy();
    expect(
      screen.getByText(
        "Codex keeps reading your Personal space live over MCP, and the edited file stays on ziyang-mbp as it is.",
      ),
    ).toBeTruthy();
    fireEvent.keyDown(document.body, { key: "3" });
    fireEvent.keyDown(document.body, { key: "Enter" });
    await waitFor(() => expect(resolveDrift).toHaveBeenCalledOnce());
    expect(resolveDrift.mock.calls[0]![0]).toMatchObject({
      mode: "stop",
      target: { slug: "codex-user-machine-1" },
    });
  });
});

describe("connecting an agent to Personal", () => {
  it("says which of its own files it reads Personal from, and how a machine gets it", async () => {
    withSource();
    renderPlace(
      <ConnectAgentDialog open onOpenChange={() => {}} connected={[]} />,
      { space: PERSONAL_SPACE },
    );
    const dialog = await screen.findByRole("dialog");
    const reads = within(dialog).getByText(
      (_, el) =>
        el?.tagName === "SPAN" &&
        /^Reads your Personal space from ~\/\.claude\/rules\/memax-personal\.md on each machine you link\. On a machine without it, run .*memax-cli link --home first\.$/.test(
          el.textContent ?? "",
        ),
    );
    expect(reads).toBeTruthy();
    // Write in Personal says how far it reaches.
    fireEvent.click(within(dialog).getByRole("radio", { name: "Write" }));
    expect(
      within(dialog).getByText(
        /In Personal, what it keeps reaches every agent on every machine you've linked\.$/,
      ),
    ).toBeTruthy();
  });
});

function TouchesOf(props: { item: ReviewItem; card: ReviewCardData }) {
  const view = useRecordsView();
  return <Touches view={view} {...props} editing={false} />;
}

describe("keeping a proposal that goes into Personal", () => {
  it("lists Personal's files, linking into Personal, and says a keep reaches every machine", async () => {
    const source = withSource();
    const item = source.review.peekQueue!("memax-v2")!.items.find(
      (i) => i.ref === "M-0433",
    )!;
    const card = await source.review.card({ space: TEST_SPACE, item });
    renderPlace(<TouchesOf item={item} card={card} />);
    const touches = await screen.findByRole("region", { name: "This touches" });
    // Each file once, whatever the number of machines it's on.
    expect(within(touches).getByText(/· 3 compiled files$/)).toBeTruthy();
    const files = within(touches).getAllByRole("link");
    expect(files.map((f) => f.getAttribute("href"))).toEqual([
      "/personal/brief/targets/claude-user-machine-1",
      "/personal/brief/targets/codex-user-machine-1",
      "/personal/brief/targets/agents-md-machine-1",
    ]);
    expect(
      within(touches).getByText(
        "Keeping it reaches every agent on every machine you've linked.",
      ),
    ).toBeTruthy();
  });
});

describe("a memory kept in Personal", () => {
  it("reaches its files once each, and the machines they're on", async () => {
    withSource();
    renderPlace(<MemoryPlace memoryRef="M-0027" />, { space: PERSONAL_SPACE });
    expect(
      await screen.findByText("reaches 3 files on 2 machines"),
    ).toBeTruthy();
    const reaches = screen
      .getByRole("heading", { name: "Reaches" })
      .closest("section")!;
    expect(within(reaches).getByText("3 files on 2 machines")).toBeTruthy();
    expect(within(reaches).getAllByRole("link")).toHaveLength(3);
  });
});
