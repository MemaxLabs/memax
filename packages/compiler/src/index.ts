/**
 * @memaxlabs/compiler: compile a Memax Space's kept record into the files AI
 * agents already read (AGENTS.md, a CLAUDE.md shim, scoped rules), and parse
 * hand edits of those files back into proposals.
 */
export { compile, DEFAULT_BUDGET } from "./compile.js";
export { parseBack, parseFile, isDrifted, driftHash } from "./parse.js";
export {
  MANAGED_START,
  MANAGED_END,
  ManagedBlockError,
  findManagedBlock,
  extractManagedBlock,
  upsertManagedBlock,
  removeManagedBlock,
} from "./managed-block.js";
export {
  adapters,
  getAdapter,
  defaultTargets,
  DEFAULT_TARGET_KINDS,
} from "./adapters/index.js";
export type { Adapter, Cap, Limits, Role } from "./adapters/index.js";
export { CompileInputError, type Issue } from "./validate.js";
export { cleanLine } from "./sanitize.js";
export { sha256Hex } from "./hash.js";
export * from "./types.js";
