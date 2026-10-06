// Package openapi embeds the /v2 API contract, v2.yaml, so tests anywhere
// in the module can check handlers against it (see internal/contract).
//
// v2.yaml is the source of truth for /v2: write the spec first, then the
// handler. The SDK's v2 types are generated from the same file
// (`pnpm --filter memax-sdk gen:v2`), and `pnpm lint` fails when the
// committed types are stale.
package openapi

import _ "embed"

// V2 is the bytes of v2.yaml.
//
//go:embed v2.yaml
var V2 []byte
