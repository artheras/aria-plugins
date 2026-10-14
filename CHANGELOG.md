# Changelog

## Commerce 0.4.0

- Stable merchant IDs, legacy-owner transition aliases and preview-first CAS migration.
- Persisted trusted catalogue, bounded owned-order listing/search, encrypted pagination cursors.
- Human invoice revision invalidates approval; unstarted cancellation, 24-hour expiry and recovery review.
- Full Sandbox/demo refunds, provider lifecycle checks and inspection-only refund recovery.
- Versioned MCP/installation bundles, safe config merge/backups, doctor and uninstall.
- OpenAPI/package contracts, release CI, migration/provider-validation/operations guides.


## Commerce 0.3.0

- Extracted transport-independent drafting, approval, checkout, capture, webhook and recovery behavior into `packages/commerce`.
- Persist payment intent before calling the provider; recover completed captures after a failed local result write without charging again.
- Added bounded recovery worker, CAS claims, exponential backoff and explicit merchant-review markers. Sandbox recovery never initiates capture.
- Added official-SDK Firestore storage with transactional CAS and dedicated quota collections for Cloud Run.
- Added shared draft/login/payment-action quotas, retry headers, stable error codes, request IDs, structured logs and dependency readiness.
- Added refreshed Google ID tokens for private Cloud Run MCP calls, separately from Commerce merchant authorization.
- Added Cloud Build image configuration and Cloud Run service/job environment templates and setup documentation.
- Preserved English UI and browser-only human payment authority. MCP still exposes only catalogue, draft and order status.

Live provider/cloud deployment is not implied by these changes. See `docs/testing.md` for local coverage and remaining validation.

