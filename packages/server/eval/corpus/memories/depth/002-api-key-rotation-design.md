Design rationale for the API key rotation mechanism in Memax.

## Problem

API keys (`mxk_` prefix) are long-lived credentials. If a key is leaked, the only option was to delete it and create a new one, which breaks all clients using the old key simultaneously. Users asked for a way to rotate keys without downtime.

## Chosen Approach: Grant-Based Rotation with Overlap Windows

Each API key belongs to a "grant" — a logical permission set tied to a user and hub. When a user rotates a key, we:

1. Generate a new key for the same grant.
2. Mark the old key as "retiring" with a configurable grace period (default: 24 hours).
3. Both keys work during the grace period.
4. After the grace period, the old key is revoked automatically by the worker's `key_expiry` job.

This lets users update their agents and CI pipelines without a hard cutover.

## Alternatives Considered

- **Versioned keys (v1, v2 suffix):** Too confusing. Users would need to track which version is active.
- **Dual-key (primary/secondary):** AWS-style approach. Simpler but limited to exactly two keys. We wanted N keys per grant for users who deploy across many agents.
- **Short-lived keys + refresh:** More secure but adds complexity. Agents would need token refresh logic, which most MCP clients don't support. We may revisit this for enterprise tiers.

## Security Considerations

- Old keys during the grace period have full permissions. We log all usage of retiring keys with a `key_status: retiring` field so users can audit whether the old key is still in use before it expires.
- The SHA-256 hash of each key is stored; we never store the raw key after initial creation.
- Rate limits apply per-grant, not per-key, so rotating doesn't reset rate limit counters.

## Implementation Status

Shipped in the Go server as of April 8. The CLI supports `memax keys rotate <key-id>` and the web app has a rotate button on the API keys settings page. The grace period is configurable via `--grace-period` flag (CLI) or a dropdown in the web UI.
