# Real provider validation

The public Demo uses a deterministic parser and simulated payments. A release,
mock test or successful Cloud Run health check is not proof of a real AI/PayPal
integration. Configure a separate merchant Sandbox service and record these checks.

## Configuration

Supply these environment variables through Secret Manager or a local secret store:

- `ARIA_MODE=sandbox`, stable `ARIA_MERCHANT_ID`, exact HTTPS `PUBLIC_URL`.
- Independent `ARIA_SESSION_SECRET`, `ARIA_MERCHANT_TOKEN` (agent) and `ARIA_MERCHANT_LOGIN_TOKEN` (human browser login) (32+ characters, all different).
- `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET`, and a registered `PAYPAL_WEBHOOK_ID`.
- `AI_GATEWAY_MODEL` and `AI_GATEWAY_API_KEY` (or supported Vercel OIDC credential).
- Dedicated Firestore project/database/collection and runtime ADC identity.

The worker needs shared storage and PayPal credentials, but no AI key, merchant
token or browser session secret. A private Cloud Run MCP bridge separately needs
ADC invoker authority and `ARIA_CLOUD_RUN_AUDIENCE`. Never reuse relay credentials.

## End-to-end checklist

1. Install the versioned skill and verified MCP tool bundle. Run `aria-plugins doctor`.
2. In a real Aria Code session, ask the model to create a draft through Commerce.
   Confirm that it calls catalogue/draft tools and returns an unapproved order link.
3. In the connected merchant browser, verify SKUs, quantities and server prices.
   Revise a quantity and confirm the old approval no longer authorizes checkout.
4. Approve the exact invoice. Create checkout and use a separate Sandbox buyer
   account to approve the payment. Capture and compare IDs, currency and amount
   with the PayPal Sandbox merchant dashboard.
5. Replay capture and the signed capture-completed webhook. Confirm there is one
   provider capture and no duplicate audit event. A redirect alone cannot mark paid.
6. In a private test setup, lose the result write after capture and run the recovery
   job. Confirm it inspects/reconciles the existing payment without a second capture.
7. Confirm a full refund in the browser. Match the provider refund/capture IDs and
   amount. Replay the action and simulate a lost response; recovery must not issue
   a second refund. Check provider status after any out-of-band partial refund.
8. Rotate merchant/session credentials with the same stable merchant ID. Old
   credentials must fail; a new authenticated session must still read old orders.
9. Verify restart/multiple-instance continuity, Scheduler execution, alert delivery
   and a restore drill against a separate database. Keep IDs and sanitized evidence.

Use [PayPal's payment API](https://developer.paypal.com/api/payments/v2) for refund
semantics. This service supports full refunds only; partial refunds and failures
are surfaced for provider review. Refund/dispute webhook events are not claimed
as a complete lifecycle subscription; use explicit status synchronization.

No live provider calls should be reported as verified until this checklist has
been run with real Sandbox credentials and actual model access.
