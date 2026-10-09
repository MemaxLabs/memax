// Fails when the SDK's generated /v2 types (packages/sdk/src/v2/schema.gen.ts)
// differ from a fresh generation from packages/server/openapi/v2.yaml, so
// the spec and the SDK can't drift. The Go side holds the server to the
// same spec (internal/contract, internal/handler/v2api tests).
//
// Fix a failure with: pnpm --filter memax-sdk gen:v2
import process from "node:process";
import { check, OUTPUT } from "../packages/sdk/scripts/gen-v2-types.mjs";

if (!(await check())) {
  console.error(
    `${OUTPUT} is out of date with packages/server/openapi/v2.yaml.\n` +
      "The /v2 contract changed without regenerating the SDK types.\n" +
      "Run: pnpm --filter memax-sdk gen:v2, then commit both files.",
  );
  process.exit(1);
}
console.log("SDK v2 types match packages/server/openapi/v2.yaml.");
