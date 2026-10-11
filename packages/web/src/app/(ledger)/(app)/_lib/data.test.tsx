import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LedgerDataProvider, useSource } from "./data";

// The server renders every request through one module, which lives as
// long as the process. The demo it renders must be the one a fresh page
// hydrates, or React throws the server's HTML away (#418).

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, session: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));

afterEach(() => {
  vi.useRealTimers();
});

/** M-0445's judge, as Review's queue shows it: checking for its first 6 s. */
function JudgeOfM0445() {
  const item = useSource()
    .review.peekQueue?.("memax-team")
    ?.items.find((i) => i.ref === "M-0445");
  return <span>{item?.judge ?? "settled"}</span>;
}

const render = () =>
  renderToString(
    <LedgerDataProvider mode="demo">
      <JudgeOfM0445 />
    </LedgerDataProvider>,
  );

describe("the demo on the server", () => {
  it("renders each request as a fresh page sees it", () => {
    vi.useFakeTimers({ now: new Date("2026-10-11T13:00:00Z") });
    expect(render()).toBe("<span>working</span>");
    // A minute later the first visit's judge has settled. A new visit's
    // hasn't: its page starts its own demo, and so does its render.
    vi.setSystemTime(new Date("2026-10-11T13:01:00Z"));
    expect(render()).toBe("<span>working</span>");
  });
});
