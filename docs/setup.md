# Setup

## 1. Local demo

Install Go 1.26+, then run `make build` and `./bin/aria-commerce`. Open `http://127.0.0.1:4180` (use the same origin, not localhost instead). No credentials are needed.

The server stores orders and its generated session key in the ignored `data/` directory, with restrictive file permissions. Keep that directory to retain orders and sessions. Run only one process against a local directory.

The application reads **exported environment variables**. It does not load .env automatically. Copy `.env.example` to `.env`, edit it locally, then load it in your shell:

```sh
set -a
. ./.env
set +a
./bin/aria-commerce
```

Keep secrets in local configuration or your hosting provider's environment settings. Do not commit .env or include credentials in screenshots.

## 2. PayPal Sandbox

1. Sign in to the [PayPal Developer Dashboard](https://developer.paypal.com/dashboard/). Create a **Sandbox REST application** and obtain its client ID and secret.
2. Create/use a Sandbox business account for the merchant and a separate personal Sandbox account for the buyer.
3. Set `ARIA_MODE=sandbox`, `PAYPAL_CLIENT_ID`, and `PAYPAL_CLIENT_SECRET`.
4. Configure the AI settings below and independent random `ARIA_SESSION_SECRET` and `ARIA_MERCHANT_TOKEN` values of at least 32 characters each.
5. Set `PUBLIC_URL` to the exact browser origin. For a local interactive checkout use `http://127.0.0.1:4180`; incoming webhooks require a publicly reachable HTTPS deployment.
6. Restart the service, connect the merchant session in the browser, draft and review an order, and create checkout.
7. Open the generated PayPal Sandbox link. Sign in with the Sandbox buyer, approve, return to Commerce, and click **Capture & verify Sandbox payment**.

Only `https://api-m.sandbox.paypal.com` is used. There is no live endpoint switch. The implementation uses [Orders v2](https://developer.paypal.com/api/orders/v2/orders-create) with server-side OAuth, deterministic request IDs, and provider-side reconciliation.

### Webhooks

In the Sandbox application's webhook configuration, add:

```text
https://YOUR_COMMERCE_HOST/api/paypal/webhook
```

Subscribe to `PAYMENT.CAPTURE.COMPLETED` and set `PAYPAL_WEBHOOK_ID` to the webhook registration ID. The service asks PayPal's [webhook verification API](https://developer.paypal.com/api/webhooks/v1) to verify headers and event payload, then reads the PayPal order again to verify invoice identity, capture status, currency, and amount. An unsigned JSON event cannot complete an order.

PAYPAL_WEBHOOK_ID is optional for browser checkout/capture, but required for webhook processing. Ignored event types return success after signature verification. Storage conflicts return 503 to allow delivery retries.

## 3. AI Gateway

Set `AI_GATEWAY_API_KEY` and `AI_GATEWAY_MODEL`. Choose an available model ID from the [live model catalogue](https://ai-gateway.vercel.sh/v1/models) that supports JSON object output. There is no hardcoded model or automatic fallback.

The Go adapter uses the [OpenAI-compatible Chat Completions endpoint](https://vercel.com/docs/ai-gateway/sdks-and-apis/openai-chat-completions) at `https://ai-gateway.vercel.sh/v1`. On Vercel, `VERCEL_OIDC_TOKEN` can replace the API key if Gateway access is enabled.

The model selects SKUs and quantities; it never supplies trusted prices or receives payment credentials. Unknown SKUs, fractional quantities, extra price fields, malformed JSON, and empty orders are rejected. Provider failure is surfaced; it does not become a simulated success.

## 4. Aria MCP bridge

Build both binaries with `make build`. Set a random `ARIA_MERCHANT_TOKEN` of at least 32 characters on both the Commerce server and the MCP process, even when testing MCP in demo mode.

Set the configuration's `command` to the **literal absolute path** of `bin/aria-commerce-mcp`, and export `ARIA_COMMERCE_URL` with the server's origin. Merge [examples/mcp_servers.json](../examples/mcp_servers.json) into the actual Aria home directory's `mcp_servers.json`; preserve other server entries. Aria home is `ARIA_HOME` if set, the existing legacy `~/.arthera` directory, or `~/.aria-code` for new users. The current Aria client expands environment placeholders in `args` and `env`, **not command**.

The stdio bridge negotiates MCP 2024-11-05 (used by the existing Aria client) or 2025-06-18 and exposes:

| Tool | Effect |
| --- | --- |
| commerce_catalog | Read catalogue |
| commerce_draft_order | Create unapproved order |
| commerce_get_order | Read order and audit |

Approval, checkout, and capture are intentionally absent. In the browser, click **Merchant session** and enter the same configured token to access MCP-created orders. Open `/?order=RETURNED_ORDER_ID` after connecting. Tokens are sent in an Authorization header by the bridge and are not part of order URLs.

[plugins/commerce](../plugins/commerce) contains the command/skill package and host configurations. Run `/skills add artheras/aria-plugins` inside Aria to install its verified workflow, then `/skills doctor`. Invoke `$commerce:commerce` after MCP registration. The [connection guide](aria-code.zh-CN.md) covers the exact installation boundary. A full interactive Aria agent run still needs a configured model and local environment.

## 5. Docker

```sh
docker build -t aria-commerce .
docker volume create aria-commerce-data
docker run --rm -p 4180:4180 \
  -e PUBLIC_URL=http://127.0.0.1:4180 \
  -v aria-commerce-data:/data aria-commerce
```

For Sandbox, supply the configured .env through `--env-file .env`. The image runs as a non-root user. The persistent volume is required for local file storage. Docker execution has not been verified in this workspace.

## 6. Vercel

The repository includes the [Go Framework Preset](https://vercel.com/docs/functions/runtimes/go) configuration. Import **aria-plugins** as its own project, root directory `.`, preset **Go**. The build produces `server` from `cmd/server` and serves the embedded UI and API together.

Configure:

- `PUBLIC_URL`: exact HTTPS deployment/production origin used in the browser.
- `ARIA_SESSION_SECRET`: stable random value, at least 32 characters.
- `UPSTASH_REDIS_REST_URL` and `UPSTASH_REDIS_REST_TOKEN`: a read/write Redis REST endpoint.
- `ARIA_MODE=demo` initially; add all Sandbox and AI settings when switching to sandbox.

The [Redis REST adapter](https://upstash.com/docs/redis/features/restapi) stores records with atomic Lua compare-and-swap. Use a database configured to retain data, with eviction disabled for these order records. Local files are deliberately rejected on Vercel because they cannot provide persistent multi-instance storage.

If deployment protection is enabled, PayPal must be able to reach the webhook URL; verify access before subscribing. Use stable production URLs for return links, cookies, and Origin validation. A preview URL with a different origin needs its own PUBLIC_URL.

No Commerce Vercel deployment or cloud resource was created as part of the credential-free implementation. Go build configuration is supplied; cloud build, Redis connectivity, and PayPal delivery require deployment verification.
