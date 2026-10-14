# Commerce 0.4.0 deployment

Verified on 2026-10-14 (Asia/Shanghai).

- Public simulated Demo: https://aria-commerce-vrkp6bxpya-nw.a.run.app/
- Release: https://github.com/artheras/aria-plugins/releases/tag/v0.4.0
- Implementation: PR #3; source commit `365357ee1c04cc16a32236b96cd35210df89ddaa`, merge commit `425ff874c8c04c437d188b14b8d0ddf9409e68cb`.
- Cloud Run revision: `aria-commerce-00004-qiz`, serving 100% of traffic in `arthera-prod/europe-west2`.
- Immutable image digest: `sha256:936c8b1107808454f227b2205cbe33869365b55e592b91497564a36cd4042007`.
- Dedicated Firestore database/collections and existing service identities are retained. A stable merchant ID and a specific historical-owner transition alias preserve merchant access; credential values are not included here.
- The recovery Job uses the same image. Execution `aria-commerce-reconcile-zb6jr` completed successfully. The existing five-minute Scheduler remains in place.
- Temporary candidate tags were removed after promotion. Existing Aria relay services were not changed.

## Verification

Both PR Go checks and release CI passed. Six macOS/Linux/Windows tool bundles and checksums are published. The macOS ARM64 bundle was installed into a temporary directory, merged a test MCP configuration, negotiated three tools and passed a read-only catalogue call. The same MCP doctor connected to the public Cloud Run service with its configured merchant credential.

Local browser checks covered drafting, approval, revision/renewed approval, simulated capture/full refund, catalogue changes/current prices and immediate order-history updates. Candidate and canonical Cloud Run API checks confirmed health/readiness, a USD 60.00 draft revised to USD 24.00, old-approval rejection, simulated capture/refund, idempotent refund replay and owned-order history. These checks did not modify the merchant catalogue in the public Demo.

This deployment uses `ARIA_MODE=demo`: drafting is deterministic and all payments/refunds are simulated. No real AI or PayPal Sandbox transaction, signed live webhook, full model-driven Aria session, private Cloud Run ID-token flow or actual provider-failure recovery has been verified. Alert/backup templates are prepared but not applied. Follow [real provider validation](sandbox-validation.md), [identity migration](identity-migration.md) and [operations](operations.md) for those remaining steps.
