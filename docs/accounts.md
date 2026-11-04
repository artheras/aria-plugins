# Account access for a Sandbox merchant

Commerce supports an opt-in OIDC account pilot. A person signs in through your
configured identity provider, and the server matches the verified issuer/subject
to an operator-managed merchant assignment. It requests only `openid`, not email
or profile data. An unassigned account receives no access. Signing in does not
connect a PayPal account or grant PayPal data permissions.

## Configure the pilot

Use a registered confidential OIDC web application with discovery and RS256 ID
tokens. Register the exact callback `PUBLIC_URL/api/auth/callback`. HTTPS is
required except for loopback development. Put the client secret in your secret
manager, never source control, browser configuration or an agent conversation.

Set `ARIA_AUTH_MODE=oidc`, `ARIA_OIDC_ISSUER`, `ARIA_OIDC_CLIENT_ID`,
`ARIA_OIDC_CLIENT_SECRET`, `ARIA_ACCOUNT_BINDINGS_FILE`, a stable
`ARIA_MERCHANT_ID`, and `ARIA_SESSION_SECRET`. Keep the existing persistence,
drafter and Sandbox provider settings. Remove `ARIA_MERCHANT_TOKEN`,
`ARIA_MERCHANT_LOGIN_TOKEN` and legacy owner migration settings; account mode
rejects them instead of falling back. OIDC discovery must succeed at startup.
The recovery worker does not need identity-provider or browser credentials.

The bindings file is a private, operator-managed JSON array, for example:

```json
[
  {"subject":"IDP_SUBJECT_FROM_YOUR_IDENTITY_ADMIN","merchant_id":"shop-one","role":"operator"},
  {"subject":"ANOTHER_ASSIGNED_SUBJECT","merchant_id":"shop-one","role":"reader"}
]
```

Use the identity provider's stable subject, not an email, display name or value
sent by a model. The merchant must match this deployment's configured merchant;
a second merchant is rejected because this service has only one PayPal provider
configuration. Bindings are loaded at startup; changing assignments requires a
service restart/redeployment. Deleting an assignment then invalidates that user's
sessions and grants. Operators sharing a merchant deliberately share its orders
and catalogue, while each user manages only their own client connections.

| Role | Read assigned merchant | Create drafts | Browser approval/payment/catalogue actions |
| --- | --- | --- | --- |
| reader | Yes | No | No |
| drafter | Yes | Yes | No |
| operator | Yes | Yes | Yes, with a browser session and CSRF confirmation |

## Connect a client

Sign in in the browser and use **Your connected account** to create a named
connection with **read only** or **read and create drafts** access. Connections
expire after at most 24 hours; the default is one hour. A reader cannot grant
write access. The credential is returned once, kept only in the page's memory,
and stored server-side only as a hash. Do not paste it into a model conversation.

Load this credential securely into the MCP bridge's existing
`ARIA_MERCHANT_TOKEN` environment setting, together with your service's
`ARIA_COMMERCE_URL`. The variable name is retained for compatibility; in account
mode its value must be the current user's scoped connection credential. Native
[client configurations](clients.md) forward environment values without embedding
them. A shared **demo** credential is valid only for demonstration-mode deployments;
account-mode clients must use separate personal connection credentials.

Client tools still cannot approve, check out, capture, refund, manage connections
or acquire a browser session. Revoking a connection rejects its next request
across replicas. Expiry, the canonical service audience, merchant binding and current assignments
are checked on every request.
Revocation does not cancel a request already authorized and executing. Browser
sign-out revokes that browser session; explicitly revoke client connections when
you want those to stop too. Each user can have at most 20 active sessions and 20
active connections. Expired/revoked credentials are pruned when issuing new ones.

## Validation and deployment boundary

The code validates state, a five-minute browser-bound encrypted login attempt,
one-use state shared across replicas, PKCE S256, signature, issuer, audience,
expiry, authorized party and nonce. Authentication and connection lifecycle logs
use opaque user IDs and connection IDs, never token values or ID-token claims.
Per-user draft quotas apply in addition to merchant and global quotas.

Automated tests use a local signing identity provider and simulated payments.
They do not establish a live Google login, actual PayPal seller consent, or a
multi-merchant production deployment. The hosted Sandbox uses Google account
sign-in with operator-managed assignments; payments remain PayPal Sandbox.
Self-hosted deployments need their own verified OIDC application and assignments.
Consumed login-state records need a retention policy in long-lived deployments.

Multiple merchant deployments still require supported PayPal seller onboarding,
server-verified consent and capability checks, isolated provider credentials,
per-merchant catalogues, and tenant-aware webhook/recovery routing. Remote MCP OAuth uses a separate authorization server and scoped tokens; browser OIDC tokens are never forwarded to AI clients.

## Remote app connections

[Remote MCP OAuth](../plugins/commerce-remote/README.md) adds user-reviewed app grants, 15-minute access tokens, refresh rotation for up to 30 days, a stable opaque profile and immediate revocation from Connected apps. Google remains the upstream identity provider; its tokens and client secret are never forwarded to MCP clients. Existing manual grants continue to work only on their REST/stdio surface. Payment actions remain browser-only.

## PayPal provider access

See [PayPal account reads](paypal-accounts.md) for the separate provider grant, explicit invoice scope, default connection manager and unconfigured onboarding boundary.
