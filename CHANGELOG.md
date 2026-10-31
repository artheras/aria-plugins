# Changelog

## Unreleased

### Security

- Commerce: the human browser login now needs its own `ARIA_MERCHANT_LOGIN_TOKEN`.
  The agent (MCP bridge) token `ARIA_MERCHANT_TOKEN` is rejected at `/api/login`,
  as is any request with an `Authorization` header, so the agent can no longer
  mint a human session and approve, capture or refund on its own. The service
  refuses to start when the two secrets are equal (and in sandbox when the login
  secret is missing); in demo mode an empty login secret disables merchant login.
  The MCP bridge refuses to start if the login secret is visible in its
  environment. **Upgrade:** set a new, different `ARIA_MERCHANT_LOGIN_TOKEN` on
  the Commerce service only (Secret Manager:
  `aria-commerce-sandbox-merchant-login-token`) and never export it in the shell
  that starts Aria.

### Fixes

- Installer: configuration backups are timestamped
  (`mcp_servers.json.backup-YYYYMMDDTHHMMSSZ`) and never collide, so upgrades with
  `--replace` no longer fail after the first. A failed MCP registration removes
  the version it just installed, so the install can be re-run.
- Release bundles declare the highest `minimum_aria_version` of their plugins
  (0.165.0 with Finance) instead of a hard-coded 0.140.0; marketplace metadata
  version now matches the 0.4.0 release.
- Removed the unused `packages/paypalreport` (it offered a live PayPal endpoint the
  service does not support) and its guide.
- CI validates every `plugins/*/package.json` against
  `contracts/plugin-package.schema.json` and checks that the MCP templates agree
  with it (server, command, declared env, no hard-coded env overrides) and that
  skills document and allow only package tools.

## Finance 0.1.0

- New Finance plugin: workflow skill, command entrypoint and Aria/Claude/Codex MCP
  templates for the separately installed `arthera-financial-service` (ten
  read-only analysis, simulated backtest and broker-read tools; needs Aria 0.165.0).
- The templates no longer pin `FINANCIAL_SERVICE_NETWORK=0`, which overrode the
  host environment and kept market data and real broker reads off for good. The
  host environment now decides (the service default is still off): set
  `FINANCIAL_SERVICE_NETWORK=1` and `FINANCIAL_SERVICE_BROKERS_ENABLED=1` there.
- Activating the Finance skill narrows the model to the ten finance tools
  (`allowed_tools`); both skill policies declare `schema_version`.

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

