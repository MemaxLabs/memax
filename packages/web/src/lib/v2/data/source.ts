import type { ActivityData } from "./activity";
import type { AgentsData } from "./agents";
import type { BriefSource } from "./brief";
import type { DevicesSource } from "./devices";
import type { DreamSource } from "./dream";
import type { GatesSource } from "./gates";
import type { ImportsSource } from "./imports";
import type { MemoriesSource } from "./memories";
import type { ReviewSource } from "./review";
import type { SettingsSource } from "./settings";
import type { SwitchSource } from "./switch";
import type { TargetsSource } from "./targets";
import type { TodaySource } from "./today";
import type { UndoSource } from "./undo";
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
 * activity.ts (Activity), agents.ts (Agents, keys), undo.ts (Undo), or
 * hangs off it as a member (review, memories, brief, targets, today,
 * gates, dream, switch, settings).
 */
export interface LedgerDataSource extends ActivityData, AgentsData, UndoSource {
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
  /** Review's queue, cards and decisions (review.ts). */
  readonly review: ReviewSource;
  /** Memories, one memory and editing (memories.ts). */
  readonly memories: MemoriesSource;
  /** The Brief, its versions and revising it (brief.ts). */
  readonly brief: BriefSource;
  /** Where the Brief compiles to: the files, settings and drift (targets.ts). */
  readonly targets: TargetsSource;
  /** Today's waiting items, Dream, what's in flight and the agents' day (today.ts). */
  readonly today: TodaySource;
  /** Decision gates: what agents asked, answering and withdrawing (gates.ts). */
  readonly gates: GatesSource;
  /** What `memax init` imported: FirstRun, Cleanup, ReviewImport (imports.ts). */
  readonly imports: ImportsSource;
  /** Confirming the CLI's device code (devices.ts). */
  readonly devices: DevicesSource;
  /** Dream's editions, undoing them, run now and the settings (dream.ts). */
  readonly dream: DreamSource;
  /** Switching a space on V1 to the V2 record, and back (switch.ts). */
  readonly switch: SwitchSource;
  /** The person's notification settings and the Security page (settings.ts). */
  readonly settings: SettingsSource;
}
