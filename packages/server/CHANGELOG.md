# Changelog

All notable server changes are documented here. The server package is not currently published as a standalone artifact.

## 0.0.2 - 2026-04-24

- Hardened org-gated auth around hub invites and registration.
- Added MCP agent activity events and SSE producer coverage for agent, hub, config, and recall/ask changes.
- Added production observability improvements for admin config, worker heartbeat, ops logs, job logging, and favicon serving.
- Fixed endpoint-specific rate-limit buckets so tight endpoint caps do not share the broader per-user operation bucket.

## 0.0.1 - 2026-04

- Initial Go API server and worker package.
- Includes auth, memory ingestion, retrieval, ask synthesis, MCP HTTP transport, hub management, plans/metering, dreams, uploads, agent config/session sync, admin operations, and PostgreSQL migrations.
