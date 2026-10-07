// Named types for the /v2 API, over the types generated from
// packages/server/openapi/v2.yaml (schema.gen.ts). Import them as a
// namespace from the package root:
//
//   import type { V2 } from "memax-sdk";
//   const m: V2.Memory = …;
//
// The names here are the SDK's public API: they stay put when the spec
// renames a schema, and the compiler flags any that stop resolving.
import type { components, operations, paths } from "./schema.gen.js";

export type { components, operations, paths };

type Schemas = components["schemas"];

// Records
export type Space = Schemas["Space"];
export type Memory = Schemas["Memory"];
export type MemoryVersion = Schemas["MemoryVersion"];
export type Source = Schemas["Source"];
export type Decision = Schemas["Decision"];
export type DecisionOption = Schemas["DecisionOption"];
export type Receipt = Schemas["Receipt"];
export type ReceiptSource = Schemas["ReceiptSource"];
export type PolicyDecision = Schemas["PolicyDecision"];

// Responses (the `data` of each envelope)
export type SpaceList = Schemas["SpaceList"];
export type MemoryPage = Schemas["MemoryPage"];
export type ReviewPage = Schemas["ReviewPage"];
export type ReceiptPage = Schemas["ReceiptPage"];
export type MemoryDetail = Schemas["MemoryDetail"];
export type CommandResult = Schemas["CommandResult"];
/** A command that changed several memories: settling a conflict, an undo. */
export type MemoriesCommandResult = Schemas["MemoriesCommandResult"];
/** What a draft repeats: Remember's near-duplicate check. */
export type NearDuplicates = Schemas["NearDuplicates"];
export type NearDuplicate = Schemas["NearDuplicate"];
/** `exact`: the same words. `near`: the same thing by meaning. */
export type DuplicateMatch = Schemas["DuplicateMatch"];
export type ErrorBody = Schemas["Error"];

// The judge, links and conflicts
export type Link = Schemas["Link"];
export type LinkedMemory = Schemas["LinkedMemory"];
export type MemoryPointer = Schemas["MemoryPointer"];
/** The judge's verdict on a memory's current version. */
export type JudgeInfo = Schemas["JudgeInfo"];
/** Both sides of a conflict and the four answers (ReviewConflict). */
export type Conflict = Schemas["Conflict"];
export type ConflictOption = Schemas["ConflictOption"];
export type ConflictEffect = Schemas["ConflictEffect"];

// The Brief, targets and compiles
export type Brief = Schemas["Brief"];
export type BriefSection = Schemas["BriefSection"];
export type BriefItem = Schemas["BriefItem"];
export type BriefResult = Schemas["BriefResult"];
/** A restored version, and what the restore left out (`dropped`). */
export type RestoreBriefResult = Schemas["RestoreBriefResult"];
/** A line (or a citation) a restore left out, and why. */
export type BriefDrop = Schemas["BriefDrop"];
export type BriefVersionPage = Schemas["BriefVersionPage"];
export type Target = Schemas["Target"];
export type TargetSettings = Schemas["TargetSettings"];
export type TargetList = Schemas["TargetList"];
export type TargetResult = Schemas["TargetResult"];
export type Delivered = Schemas["Delivered"];
export type DeliveredFile = Schemas["DeliveredFile"];
/** A file a pull holds, and the proposals it waits for. */
export type TargetHold = Schemas["TargetHold"];
export type CompileRun = Schemas["CompileRun"];
export type CompileRunPage = Schemas["CompileRunPage"];
export type CompiledOutput = Schemas["CompiledOutput"];
export type CompileWarning = Schemas["CompileWarning"];
export type TargetPreview = Schemas["TargetPreview"];
export type PreviewOutput = Schemas["PreviewOutput"];
export type Observation = Schemas["Observation"];
export type ObservationResult = Schemas["ObservationResult"];
export type ChangeSet = Schemas["ChangeSet"];
export type DriftChange = Schemas["DriftChange"];
export type DriftInfo = Schemas["DriftInfo"];
export type ChangeResult = Schemas["ChangeResult"];
export type DeliveryResult = Schemas["DeliveryResult"];
export type Drift = Schemas["Drift"];
export type DriftItem = Schemas["DriftItem"];
export type DriftResolutionResult = Schemas["DriftResolutionResult"];
export type ErrorDetails = Schemas["ErrorDetails"];

// Decision gates
/** A question an agent asked a person (G-). */
export type Gate = Schemas["Gate"];
export type GateOption = Schemas["GateOption"];
export type GateAnswer = Schemas["GateAnswer"];
export type GateWithdrawal = Schemas["GateWithdrawal"];
export type GatePage = Schemas["GatePage"];
/** A gate command's answer; for an answer, `memory` is the kept decision. */
export type GateResult = Schemas["GateResult"];

// Forget (rule 7)
/** What a Forget did: never words. Read one for its steps. */
export type Tombstone = Schemas["Tombstone"];
export type TombstonePage = Schemas["TombstonePage"];
/** One step of how a memory was forgotten, with where it stands now. */
export type TombstoneStep = Schemas["TombstoneStep"];
export type TombstoneActor = Schemas["TombstoneActor"];
export type TombstoneAgent = Schemas["TombstoneAgent"];
export type TombstoneTarget = Schemas["TombstoneTarget"];
/** How many of each kind of thing held the words and are gone. */
export type TombstoneGone = Schemas["TombstoneGone"];
/** A copy Memax can't reach, said as data (git history, backups, …). */
export type UnreachableCopy = Schemas["UnreachableCopy"];
/** An outside model provider that processed the words. */
export type Processor = Schemas["Processor"];
export type ForgetResult = Schemas["ForgetResult"];
/** What a Forget would do: what goes with it, the files, the agents told. */
export type ForgetPreview = Schemas["ForgetPreview"];
/** A memory that goes with a Forget (`details.carries` of `forget_carries`). */
export type ForgetCarry = Schemas["ForgetCarry"];
/** An agent's request that a person forget a memory. */
export type ForgetRequestRecord = Schemas["ForgetRequestRecord"];
export type ForgetRequestResult = Schemas["ForgetRequestResult"];
/** Something an agent is told once: a memory or a space it read was forgotten. */
export type Notice = Schemas["Notice"];
export type NoticeList = Schemas["NoticeList"];
export type AckNoticesResult = Schemas["AckNoticesResult"];

// The sealed receipt chain
/** One signed checkpoint of a space's receipt chain. */
export type Checkpoint = Schemas["Checkpoint"];
export type CheckpointPage = Schemas["CheckpointPage"];
/** How far the chain is sealed and verified ("sealed through receipt N"). */
export type SealStatus = Schemas["SealStatus"];
/** A public key checkpoints are signed with. */
export type SigningKey = Schemas["SigningKey"];

// Reads (R-): what agents read. Not receipts.
export type Read = Schemas["Read"];
export type ReadPage = Schemas["ReadPage"];
/** How a memory has been read, and by which agents (MemoryDetail.reads). */
export type MemoryReads = Schemas["MemoryReads"];
export type MemoryReader = Schemas["MemoryReader"];
export type CompileLoadResult = Schemas["CompileLoadResult"];

// Ask
/** One event of an answer's stream (`memax.v2.ask`), discriminated by `event`. */
export type AskEvent = Schemas["AskEvent"];
export type AskSourcesEvent = Schemas["AskSourcesEvent"];
export type AskDeltaEvent = Schemas["AskDeltaEvent"];
export type AskCiteEvent = Schemas["AskCiteEvent"];
export type AskDoneEvent = Schemas["AskDoneEvent"];
export type AskErrorEvent = Schemas["AskErrorEvent"];
export type AskSources = Schemas["AskSources"];
export type AskSource = Schemas["AskSource"];
export type AskDelta = Schemas["AskDelta"];
export type AskCite = Schemas["AskCite"];
export type AskDone = Schemas["AskDone"];
export type AskUsage = Schemas["AskUsage"];
export type AskFailure = Schemas["AskFailure"];
/** answered, not_covered, unsupported (don't show it as an answer) or sources_only. */
export type AskOutcome = Schemas["AskOutcome"];
export type AskInput = Schemas["AskRequest"];

// Imports (`memax init`, Cleanup, ReviewImport)
/** One upload of statements read from agent files. */
export type Import = Schemas["Import"];
export type ImportFile = Schemas["ImportFile"];
export type ImportSkip = Schemas["ImportSkip"];
export type ImportCounts = Schemas["ImportCounts"];
export type ImportCheck = Schemas["ImportCheck"];
/** What `imports.create` returns: the import and each statement's outcome. */
export type ImportResult = Schemas["ImportResult"];
export type ImportItemResult = Schemas["ImportItemResult"];
/** One import in full: items, the memories they became, disagreements, progress. */
export type ImportView = Schemas["ImportView"];
export type ImportItem = Schemas["ImportItem"];
/** A memory an import proposed or found, and whether it can be kept in bulk. */
export type ImportMemory = Schemas["ImportMemory"];
/** A disagreement among an import's proposals, settled once as a group. */
export type ImportConflict = Schemas["ImportConflict"];
export type ImportProgress = Schemas["ImportProgress"];
export type ImportPage = Schemas["ImportPage"];
export type ImportConflictResult = Schemas["ImportConflictResult"];
/** What a bulk keep or reject did to each memory. */
export type BulkReviewResult = Schemas["BulkReviewResult"];
export type BulkReviewItem = Schemas["BulkReviewItem"];

// Device sign-in (CliAuth, /device)
/** A device asking to sign in with a code; all but `address` is what it says about itself. */
export type DeviceAuthorization = Schemas["DeviceAuthorization"];
/** pending, approved, signed_in, denied or expired. */
export type DeviceAuthorizationState = Schemas["DeviceAuthorizationState"];
export type DeviceCodeInput = Schemas["DeviceCodeRequest"];

// Sessions (Settings › Account)
/** One place you are signed in; `current` is the session asking. */
export type Session = Schemas["Session"];
/** What signed in: web, cli, device or mcp. */
export type SessionSurface = Schemas["SessionSurface"];
export type SessionList = Schemas["SessionList"];
/** How many sessions signing out everywhere else ended. */
export type SessionsRevoked = Schemas["SessionsRevoked"];

// Agents
export type AgentConnection = Schemas["AgentConnection"];
export type AgentSpace = Schemas["AgentSpace"];
export type AgentCredential = Schemas["AgentCredential"];
export type AgentList = Schemas["AgentList"];
export type AgentDetail = Schemas["AgentDetail"];
export type AgentWeek = Schemas["AgentWeek"];
export type AgentSession = Schemas["AgentSession"];
export type AgentCommandResult = Schemas["AgentCommandResult"];

// Request bodies
export type RememberInput = Schemas["RememberRequest"];
export type NearDuplicatesInput = Schemas["NearDuplicatesRequest"];
export type EditInput = Schemas["EditRequest"];
export type ReviewInput = Schemas["ReviewRequest"];
export type SourceInput = Schemas["SourceInput"];
export type AutonomyInput = Schemas["AutonomyRequest"];
export type AgentCommandInput = Schemas["AgentCommandRequest"];
export type ReviseBriefInput = Schemas["ReviseBriefRequest"];
export type BriefSectionInput = Schemas["BriefSectionInput"];
export type BriefItemInput = Schemas["BriefItemInput"];
export type CreateTargetInput = Schemas["CreateTargetRequest"];
export type ConfigureTargetInput = Schemas["ConfigureTargetRequest"];
export type TargetSettingsInput = Schemas["TargetSettingsInput"];
export type ObservationInput = Schemas["ObservationRequest"];
export type DeliveryInput = Schemas["DeliveryRequest"];
export type ResolveDriftInput = Schemas["ResolveDriftRequest"];
export type ResolveConflictInput = Schemas["ResolveConflictRequest"];
/** The body of an undo: an optional reason, for the receipt. */
export type UndoInput = Schemas["ReviewRequest"];
export type UndoEditionInput = Schemas["UndoEditionRequest"];
export type DreamSettingsInput = Schemas["DreamSettingsRequest"];
export type RequestDecisionInput = Schemas["RequestDecisionRequest"];
export type AnswerGateInput = Schemas["AnswerGateRequest"];
/** The body of withdrawing a gate: an optional reason, for the receipt. */
export type WithdrawGateInput = Schemas["ReviewRequest"];
/** A session start's report of the compile its agent loaded. */
export type CompileLoadInput = Schemas["CompileLoadRequest"];
export type ForgetInput = Schemas["ForgetRequest"];
export type AckNoticesInput = Schemas["AckNoticesRequest"];
export type CreateSpaceInput = Schemas["CreateSpaceRequest"];

// Switch to V2 (plan 25 §10) and notes (N-)
/** Where a space's Switch to V2 stands, with a fresh preview of what moves. */
export type SpaceSwitch = Schemas["SpaceSwitch"];
/** v1, running, switched, failed (send it again to resume) or off. */
export type SwitchState = Schemas["SwitchState"];
export type SwitchStep = Schemas["SwitchStep"];
/** What switching moves, read from V1: the dry run. */
export type SwitchPreview = Schemas["SwitchPreview"];
export type SwitchNotes = Schemas["SwitchNotes"];
export type SwitchMember = Schemas["SwitchMember"];
export type SwitchConfig = Schemas["SwitchConfig"];
export type SwitchAgent = Schemas["SwitchAgent"];
export type SwitchProgress = Schemas["SwitchProgress"];
export type SwitchSpaceInput = Schemas["SwitchSpaceRequest"];
/** A note: raw material (a V1 memory, persona or agent file), never compiled. */
export type Note = Schemas["Note"];
export type NotePage = Schemas["NotePage"];
export type NoteOrigin = Schemas["NoteOrigin"];
/** candidate (offered for bulk keep), fold (for Dream) or note (stays a note). */
export type NoteDisposition = Schemas["NoteDisposition"];
export type NoteHold = Schemas["NoteHold"];
export type ForgetNoteInput = Schemas["ForgetNoteRequest"];
export type NoteForgetResult = Schemas["NoteForgetResult"];
export type NoteForgetPreview = Schemas["NoteForgetPreview"];
/** A Dream run from V1: read-only history, no undo. */
export type V1DreamRun = Schemas["V1DreamRun"];
export type V1DreamRunList = Schemas["V1DreamRunList"];
export type ImportInput = Schemas["ImportRequest"];
export type ImportItemInput = Schemas["ImportItemInput"];
export type SettleImportConflictInput = Schemas["SettleImportConflictRequest"];
export type BulkReviewInput = Schemas["BulkReviewRequest"];

// Vocabulary
export type Section = Schemas["Section"];
export type MemoryKind = Schemas["MemoryKind"];
export type State = Schemas["State"];
export type Lifecycle = Schemas["Lifecycle"];
export type Flag = Schemas["Flag"];
export type Trust = Schemas["Trust"];
export type SourceKind = Schemas["SourceKind"];
export type SpaceKind = Schemas["SpaceKind"];
export type Role = Schemas["Role"];
/** What an agent may do in a space: read, propose or write. */
export type Autonomy = Schemas["Autonomy"];
export type AgentKind = Schemas["AgentKind"];
export type AgentSurface = Schemas["AgentSurface"];
export type AgentState = Schemas["AgentState"];
export type CredentialKind = Schemas["CredentialKind"];
export type TargetKind = Schemas["TargetKind"];
export type Delivery = Schemas["Delivery"];
/** in_sync, compiling, pending_delivery, drifted or off. */
export type SyncState = Schemas["SyncState"];
export type IncludeMode = Schemas["IncludeMode"];
export type StaleMode = Schemas["StaleMode"];
export type ScopedMode = Schemas["ScopedMode"];
export type CompileStatus = Schemas["CompileStatus"];
export type ObservationStatus = Schemas["ObservationStatus"];
export type DriftMode = Schemas["DriftMode"];
export type ActorKind = Schemas["ActorKind"];
export type Via = Schemas["Via"];
export type Assurance = Schemas["Assurance"];
export type ReceiptAction = Schemas["ReceiptAction"];
export type ObjectKind = Schemas["ObjectKind"];
export type Outcome = Schemas["Outcome"];
export type PolicyEffect = Schemas["PolicyEffect"];
/** Why policy decided what it did; localise by it. */
export type PolicyCode = Schemas["PolicyCode"];
export type ErrorCode = Schemas["ErrorCode"];
export type LinkKind = Schemas["LinkKind"];
export type LinkDirection = Schemas["LinkDirection"];
/** duplicate, updates, extends, contradicts, unrelated or none. */
export type Relation = Schemas["Relation"];
export type JudgeStage = Schemas["JudgeStage"];
export type VerdictOutcome = Schemas["VerdictOutcome"];
/** working (not judged yet), judged or failed. */
export type JudgeState = Schemas["JudgeState"];
export type ModelTier = Schemas["ModelTier"];
/** keep_this, keep_other, keep_both or leave_open. */
export type ConflictChoice = Schemas["ConflictChoice"];
export type ConflictChange = Schemas["ConflictChange"];
/** Why an undo was refused (`details.reason` of `undo_refused`). */
export type UndoRefusal = Schemas["UndoRefusal"];
/** waiting, answered, withdrawn or expired. */
export type GateStatus = Schemas["GateStatus"];
/** recall, search, get, list, digest or compile_load. */
export type ReadKind = Schemas["ReadKind"];
export type ReaderKind = Schemas["ReaderKind"];
/** folded, updates, cites or space. */
export type CarryReason = Schemas["CarryReason"];
/** memory, or space for a Forget of everything in a space. */
export type TombstoneKind = Schemas["TombstoneKind"];
/** propagating or done. */
export type TombstoneStatus = Schemas["TombstoneStatus"];
export type StepKind = Schemas["StepKind"];
export type StepStatus = Schemas["StepStatus"];
export type StepReason = Schemas["StepReason"];
export type UnreachableKind = Schemas["UnreachableKind"];
/** waiting, forgotten or declined. */
export type ForgetRequestStatus = Schemas["ForgetRequestStatus"];
export type NoticeKind = Schemas["NoticeKind"];
/** repository (shared with everyone who clones it) or home (this machine only). */
export type ImportLocation = Schemas["ImportLocation"];
/** proposed, folded, existing or refused. */
export type ImportOutcome = Schemas["ImportOutcome"];
/** secret, too_long or limit. */
export type ImportSkipReason = Schemas["ImportSkipReason"];
/** pending, checked, no_model, failed or skipped. */
export type ImportCheckState = Schemas["ImportCheckState"];
/** Why a proposal can't be kept in bulk. */
export type ImportHeld = Schemas["ImportHeld"];
/** keep_one, keep_all, leave_open or keep_suggestion. */
export type ImportChoice = Schemas["ImportChoice"];
/** applied, refused or failed. */
export type BulkOutcome = Schemas["BulkOutcome"];
// Dream editions (plan 25 §5.10)
/** One edition (D-): what Dream read and what it did. */
export type DreamEdition = Schemas["DreamEdition"];
export type DreamEditionPage = Schemas["DreamEditionPage"];
/** One of an edition's actions, with its memory as it is now. */
export type DreamAction = Schemas["DreamAction"];
export type DreamActionPage = Schemas["DreamActionPage"];
/** fold, propose, dedupe, conflict, stale, fade or brief. */
export type DreamActionKind = Schemas["DreamActionKind"];
export type DreamCounts = Schemas["DreamCounts"];
export type DreamSurfaced = Schemas["DreamSurfaced"];
export type DreamSchedule = Schemas["DreamSchedule"];
export type DreamUndoResult = Schemas["DreamUndoResult"];
export type UndoEditionResult = Schemas["UndoEditionResult"];
export type DreamUndoRefusal = Schemas["DreamUndoRefusal"];
export type DreamRun = Schemas["DreamRun"];
export type DreamSettings = Schemas["DreamSettings"];
export type NoteAuthorCount = Schemas["NoteAuthorCount"];
/** The surfaces a client may declare in `X-Memax-Via`. */
export type ClientVia = components["parameters"]["Via"];
