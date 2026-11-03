# PayPal Sandbox account service on Cloud Run

The hosted Commerce Sandbox uses Google account sign-in, operator-managed
merchant assignments and scoped personal client connections. The payment provider
is PayPal Sandbox; no production payments or public seller onboarding are enabled.

| Setting | Account pilot |
| --- | --- |
| Service | `aria-commerce-sandbox` |
| Payments | `ARIA_MODE=sandbox`, PayPal Sandbox credentials |
| Drafting | `ARIA_AI=host`, selections proposed by the connected client model |
| Sign-in | `ARIA_AUTH_MODE=oidc`, Google OIDC web application |
| Data | Dedicated Firestore database/collection; one configured merchant |
| Client access | Separate expiring `read`/`draft` credentials for assigned users |

## Identity and secrets

Register a confidential Google web client with the exact callback
`PUBLIC_URL/api/auth/callback`. The service requests only `openid`. Keep the
client secret in Secret Manager. Maintain the private subject/merchant/role
bindings described in [account access](accounts.md); an email is not a subject.

The runtime identity needs Firestore access and read access to these service
secrets only:

- Session signing/encryption secret.
- OIDC client secret and the private bindings file mounted read-only.
- PayPal Sandbox client ID, client secret and webhook ID.

Do not pass personal client connection credentials to the service. The service
stores only their hashes. Clients load their own credentials from a secure local
store or user-authorized secret store; native configuration files reference a
launcher/environment, not literal tokens. Host-model drafting needs no Vertex AI
or AI Gateway credential.

## Build and configure

Build the exact reviewed commit with `cloudbuild.yaml`. The Docker build uses
Go 1.26.10 or later. For an existing Sandbox service with its provider and storage
configuration already set, stage account configuration without switching traffic:

```sh
gcloud run services update aria-commerce-sandbox \
  --project=PROJECT --region=REGION --image=IMAGE_DIGEST \
  --update-env-vars="ARIA_MODE=sandbox,ARIA_AI=host,ARIA_AUTH_MODE=oidc,ARIA_OIDC_ISSUER=https://accounts.google.com,ARIA_OIDC_CLIENT_ID=WEB_CLIENT_ID,ARIA_ACCOUNT_BINDINGS_FILE=/run/secrets/account-bindings.json" \
  --remove-secrets="ARIA_MERCHANT_TOKEN,ARIA_MERCHANT_LOGIN_TOKEN" \
  --update-secrets="ARIA_OIDC_CLIENT_SECRET=OIDC_SECRET:latest,/run/secrets/account-bindings.json=BINDINGS_SECRET:latest" \
  --no-traffic --tag=account-candidate
```

Preserve `ARIA_SESSION_SECRET`, PayPal settings, quotas, persistent storage and a
stable `ARIA_MERCHANT_ID`. Remove any legacy ownership migration setting. Account
mode rejects shared demonstration credentials. `PUBLIC_URL` must be the canonical
browser origin, including when checking a tagged candidate.

Verify candidate configuration, anonymous rejection, personal read/draft access,
and rejection of legacy credentials and agent approval. A tagged candidate uses
the canonical callback; complete the real Google browser sign-in after routing to
the verified account revision. Prepare matching personal client configuration and
a rollback plan before switching. Remove legacy revision tags so old endpoints
cannot bypass account permissions. Retain credentials and order data when changing
routing.

## PayPal webhook and verification

Register `PUBLIC_URL/api/paypal/webhook` for `PAYMENT.CAPTURE.COMPLETED` and
`PAYMENT.CAPTURE.REFUNDED`, store its ID securely, and configure
`PAYPAL_WEBHOOK_ID`. Verify webhook delivery and reconciliation independently.
A draft or successful sign-in is not proof of a Sandbox capture.

Google sign-in does not grant PayPal merchant permissions. Multiple merchants
require supported seller onboarding, verified consent, isolated provider
credentials/catalogues and tenant-aware webhook/recovery routing. Personal stdio
credentials are not remote MCP OAuth authorization.
