// Internal admin client — web-only, never published.
//
// Import pattern:
//
//   import * as adminClient from "@/lib/admin-client";
//   // or: import { listUsers, getUserUsage } from "@/lib/admin-client";
//
// Do NOT import admin types from "memax-sdk" — the SDK is public and
// the admin surface was deliberately extracted out.

export * from "./client";
export * from "./types";
export * from "./ops";
export * from "./ops-types";
