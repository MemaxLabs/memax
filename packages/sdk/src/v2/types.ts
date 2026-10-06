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
export type ErrorBody = Schemas["Error"];
export type ErrorDetails = Schemas["ErrorDetails"];

// Request bodies
export type RememberInput = Schemas["RememberRequest"];
export type EditInput = Schemas["EditRequest"];
export type ReviewInput = Schemas["ReviewRequest"];
export type SourceInput = Schemas["SourceInput"];

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
/** The surfaces a client may declare in `X-Memax-Via`. */
export type ClientVia = components["parameters"]["Via"];
