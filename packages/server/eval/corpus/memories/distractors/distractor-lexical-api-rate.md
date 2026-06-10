## API Design — Managing Rate of Change

When evolving the Memax API, we need to balance stability for existing SDK consumers with the need to ship new features quickly. Our approach:

- **Additive changes only** — new fields can be added to responses without a version bump. Clients should ignore unknown fields.
- **Breaking changes require a new version prefix** — if we must rename a field or change response shape, bump from `/v1/` to `/v2/` for that endpoint.
- **Deprecation window** — deprecated endpoints continue working for 90 days with a `Sunset` header. SDK prints a warning when hitting deprecated routes.
- **Rate of iteration** — aim for no more than one breaking change per quarter. Bundle breaking changes into a single version bump.

This policy helps us maintain trust with API consumers while keeping the development pace high. The SDK's auto-generated types from OpenAPI spec mean that type mismatches surface at compile time, not runtime.
