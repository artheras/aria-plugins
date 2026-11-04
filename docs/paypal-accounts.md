# PayPal account reads

The default remote plugin exposes `commerce_profile`, `paypal_connection_status`
and `paypal_list_invoices`. The root page manages client connections and shows
PayPal authorization status. It does not fetch the Aria catalogue or order list.
Existing exact-order links still open the separate Sandbox review page. Legacy
remote catalogue and draft tools require `ARIA_COMMERCE_LEGACY_TOOLS=1`.

## Two permissions

An Aria identity and its MCP OAuth grant are separate from PayPal merchant
authorization. Invoice reads require an explicitly approved `paypal.read` MCP
scope as well as the verified merchant grant's `invoices.read` capability. Old
`read`, `draft` and renewal grants never gain `paypal.read` automatically. The
broker requires `read` alongside `paypal.read` and checks current membership.

ChatGPT and Claude custom remote connectors use the existing HTTPS `/mcp` URL
and OAuth. Review any new permission request in the client. Native clients can
request `read,paypal.read,offline_access` after a provider connection is ready.
Aria's updated Go connector can request this with
`aria-commerce-connect login --paypal-read`. The default login requests only
Aria read and optional renewal. None of these steps alone connects PayPal.

## Provider adapter

`packages/paypalaccounts.Reader` accepts a server-owned `GrantSource` and is
injected as `commerce.Config.PayPalAccounts`. The deployment currently leaves
this unset: status reports `not_connected`, and invoice reads fail without
returning sample data. There is no public PayPal onboarding endpoint or token
submission form. Do not advertise independent merchant onboarding as complete.

A production grant source must complete an applicable PayPal-approved merchant
authorization flow, validate the merchant identity and actual invoice API
permission, bind it to the authenticated Aria user, and store/refresh credentials
securely. It must resolve revocation on every call. Never mark invoice permission
from a Google login, PayPal profile login, a user-supplied merchant ID, or this
deployment's checkout app credentials. PayPal partner onboarding may require
partner approval and enabled capabilities; confirm these before enabling it.

The reader validates user binding, environment, verification time, expiry and
invoice capability before making a GET to the fixed PayPal Invoicing v2 origin.
It never follows redirects, accepts caller-supplied provider URLs or credentials,
or retries a write. Results contain invoice IDs, statuses and decimal-string
amounts, plus source, merchant, environment, retrieval time and pagination. Payer
contact data and raw upstream errors are omitted. Empty provider results remain
empty; invalid, forbidden and unavailable results never fall back to Aria data.

Client consent can be revoked on the connection page. Provider revocation and
token storage remain responsibilities of the approved onboarding/grant source.

## Verification

Tests use a local HTTP provider and identity server. They cover user isolation,
expired/unverified/revoked grants, missing invoice permission, safe provider
errors, redirect rejection, credential omission, legacy MCP defaults, old client
scope rejection and existing review links. These tests do not prove an actual
PayPal account authorization or live invoice read.

Provider references: [PayPal MCP](https://developer.paypal.com/ai-tools/mcp-server/),
[Invoicing v2](https://developer.paypal.com/api/invoicing/v2),
[software seller onboarding](https://developer.paypal.com/platforms/seller-onboarding/build-onboarding).
