# Real PayPal Sandbox service on Cloud Run

How the Sandbox service was brought up, so it can be repeated. It is a **separate**
Cloud Run service from the simulated demo; nothing about the demo changes.

| | Demo | Sandbox |
|---|---|---|
| Service | `aria-commerce` | `aria-commerce-sandbox` |
| `ARIA_MODE` | `demo` (local parser, simulated payment) | `sandbox` (Gemini on Vertex AI, PayPal Sandbox) |
| Orders | `aria_commerce_records` | `aria_commerce_sandbox_records` (same Firestore database) |
| Merchant | `aria-commerce-demo` | `aria-commerce-sandbox` |

## Secrets (Secret Manager)

Stored **without a trailing newline** (`printf %s`, not `echo`): Cloud Run passes a secret to the
container byte for byte, so a newline makes the token the service holds differ from the one a
client read with `$(...)`, and every merchant login fails as "invalid merchant token".

- `aria-commerce-sandbox-session-secret`, `aria-commerce-sandbox-merchant-token`: random, 64 hex characters
- `aria-commerce-paypal-sandbox-client-id`, `aria-commerce-paypal-sandbox-client-secret`: the Sandbox REST app (Transaction search enabled)
- `aria-commerce-sandbox-paypal-webhook-id`: the webhook registered below

The runtime service account (`aria-commerce-runtime`) gets `secretmanager.secretAccessor` on each, and
`roles/aiplatform.user` on the project so `ARIA_AI=vertex` can call Vertex AI as itself.

## Build and deploy

```sh
gcloud builds submit --config=cloudbuild.yaml \
  --substitutions=_IMAGE=REGION-docker.pkg.dev/PROJECT/REPO/aria-commerce-sandbox,COMMIT_SHA=$(git rev-parse --short=12 HEAD) .

gcloud run deploy aria-commerce-sandbox --region=REGION --image=IMAGE:TAG \
  --service-account=aria-commerce-runtime@PROJECT.iam.gserviceaccount.com \
  --port=4180 --memory=512Mi --cpu=1 --max-instances=2 --concurrency=20 --allow-unauthenticated \
  --set-env-vars="ARIA_MODE=sandbox,ARIA_AI=vertex,PAYPAL_BUTTONS=1,ARIA_STORE=firestore,GOOGLE_CLOUD_PROJECT=PROJECT,ARIA_FIRESTORE_DATABASE=DB,ARIA_FIRESTORE_COLLECTION=aria_commerce_sandbox_records,PUBLIC_URL=https://SERVICE-HASH-REGION.a.run.app,ARIA_BIND_ALL=1,ARIA_MERCHANT_ID=aria-commerce-sandbox" \
  --set-secrets="ARIA_SESSION_SECRET=aria-commerce-sandbox-session-secret:latest,ARIA_MERCHANT_TOKEN=aria-commerce-sandbox-merchant-token:latest,PAYPAL_CLIENT_ID=aria-commerce-paypal-sandbox-client-id:latest,PAYPAL_CLIENT_SECRET=aria-commerce-paypal-sandbox-client-secret:latest"
```

`PUBLIC_URL` must be the exact origin the browser uses (cookies, Origin checks, PayPal return URLs).
Public access only exposes the page; every order action needs the merchant session.

## Webhook

Register `PUBLIC_URL/api/paypal/webhook` for `PAYMENT.CAPTURE.COMPLETED` and
`PAYMENT.CAPTURE.REFUNDED` (Developer Dashboard, or `POST /v1/notifications/webhooks` with the app's
credentials), store the returned ID as `aria-commerce-sandbox-paypal-webhook-id`, and attach it with
`gcloud run services update ... --update-secrets=PAYPAL_WEBHOOK_ID=aria-commerce-sandbox-paypal-webhook-id:latest`.
A webhook with a bad signature is answered `400`.

## Findings from the first deployment

- Gemini counts its reasoning against `max_tokens`; at 700 the JSON was cut off (`finish_reason=length`) on
  Cloud Run, though not locally. Vertex now uses 4096.
- Gemini may return `summary` as an object; it is commentary and is coerced, while `items` stay strictly validated.
- `draft_failed` now logs the drafter's cause, which is how both were found.

Not covered: the Cloud Run Job for payment recovery, alerting, load behaviour, live webhook delivery.
