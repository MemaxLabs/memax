import type { ActivityData } from "./activity";
import type { AgentsData } from "./agents";
import type {
  AskEvent,
  KeepResult,
  RememberCheck,
  RememberInput,
  SpaceOverview,
  SpaceSummary,
  Viewer,
} from "./types";

/**
 * Where the V2 frame gets its data. Two implementations:
 *
 * - `createSdkSource` (sdk-source.ts): memax.v2 for signed-in people.
 * - `demoSource` (demo-source.ts): the handoff's demo dataset, for dev
 *   fixtures and Playwright, deterministic and with no server.
 *
 * The app picks one in exactly one place, (ledger)/(app)/layout.tsx;
 * nothing else knows which is in use. Adding an endpoint means
 * implementing it in both.
 *
 * Each domain declares its part in its own module and is mixed in here:
 * activity.ts (Activity), agents.ts (Agents, keys).
 */
export interface LedgerDataSource extends ActivityData, AgentsData {
  readonly kind: "sdk" | "demo";
  /**
   * Data the source already holds, for the first render (server and
   * hydration) so the frame doesn't flash. The demo has it all; the
   * SDK source has none.
   */
  readonly peek?: {
    spaces(): SpaceSummary[];
    overview(slug: string): SpaceOverview | undefined;
  };
  /** The clock receipts and Today are relative to. The demo's is fixed. */
  now(): Date;
  /** The signed-in person; null while the session loads. */
  readonly viewer: Viewer | null;
  /** The viewer's spaces, in switcher order (⌘1…⌘9). */
  spaces(signal?: AbortSignal): Promise<SpaceSummary[]>;
  overview(space: SpaceSummary, signal?: AbortSignal): Promise<SpaceOverview>;
  /** A cited answer from the space, streamed. */
  ask(input: {
    space: SpaceSummary;
    question: string;
    signal?: AbortSignal;
  }): AsyncIterable<AskEvent>;
  /** The near-duplicate check Remember runs as the person types. */
  checkRemember(input: {
    space: SpaceSummary;
    statement: string;
    signal?: AbortSignal;
  }): Promise<RememberCheck>;
  /** Keep a statement the person wrote (⌘K Remember, or Ask's ⌘↵). */
  remember(input: RememberInput): Promise<KeepResult>;
  /** Keep an existing proposal instead (Remember's near-duplicate offer). */
  keepProposal(input: {
    space: SpaceSummary;
    ref: string;
    idempotencyKey: string;
  }): Promise<KeepResult>;
}
