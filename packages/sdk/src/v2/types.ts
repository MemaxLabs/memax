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
export type BriefVersionPage = Schemas["BriefVersionPage"];
export type Target = Schemas["Target"];
export type TargetSettings = Schemas["TargetSettings"];
export type TargetList = Schemas["TargetList"];
export type TargetResult = Schemas["TargetResult"];
export type Delivered = Schemas["Delivered"];
export type DeliveredFile = Schemas["DeliveredFile"];
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
export type RequestDecisionInput = Schemas["RequestDecisionRequest"];
export type AnswerGateInput = Schemas["AnswerGateRequest"];
/** The body of withdrawing a gate: an optional reason, for the receipt. */
export type WithdrawGateInput = Schemas["ReviewRequest"];

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
/** The surfaces a client may declare in `X-Memax-Via`. */
export type ClientVia = components["parameters"]["Via"];
