# Commerce quickstart

Commerce is the first plugin in the Aria Code catalogue. Its business service and reusable modules are written in Go; the browser demo uses plain HTML, CSS, and JavaScript.

## Try the browser demo

Install Go 1.26 or newer, then run from the repository root:

```sh
make build
./bin/aria-commerce
```

Open `http://127.0.0.1:4180`. Draft an order for **2 tees and 1 mug**, review the **$60.00** invoice, approve it, create checkout, simulate payment, and download the receipt. The page is labelled **Aria Commerce · Plugin Demo**. No credentials are needed for this local browser workflow.

**Demo mode makes no AI or PayPal calls and moves no money.** The page and receipt identify simulated results. Records persist in the ignored `data/` directory and survive refreshes and service restarts.

## Use the same plugin in Aria Code

Follow [Connect to Aria Code](aria-code.md) to install `commerce:commerce`, build the Go MCP bridge, and register its tools. Installing the skill alone does not start the service or configure MCP.

Aria can read products, draft orders, and inspect status. You approve the exact invoice and complete checkout in the browser.

## Configure AI Gateway and PayPal Sandbox

Copy `.env.example` to `.env` and configure locally:

| Variable | Purpose |
| --- | --- |
| ARIA_MODE | Set to `sandbox` |
| PUBLIC_URL | Exact browser origin; use HTTPS for a hosted deployment |
| PAYPAL_CLIENT_ID / PAYPAL_CLIENT_SECRET | Sandbox REST application credentials |
| AI_GATEWAY_API_KEY | Vercel AI Gateway API key |
| AI_GATEWAY_MODEL | Available model ID supporting JSON object output |
| ARIA_SESSION_SECRET | Independent random value, at least 32 characters |
| ARIA_MERCHANT_TOKEN | Another independent random value, at least 32 characters |
| PAYPAL_WEBHOOK_ID | Optional for browser capture; required for webhook processing |

The service reads exported variables and does not automatically load `.env`:

```sh
set -a
. ./.env
set +a
./bin/aria-commerce
```

Connect the browser's merchant session using the configured token. Approve an invoice, open PayPal Sandbox with a test buyer account, return to Commerce, and capture and verify the payment.

For Vercel, persistent Redis REST storage, stable session secrets, and the exact public origin are required. See the [full setup guide](setup.md) for credentials, webhooks, MCP, Docker, and deployment configuration. Live providers and cloud deployment require configuration and separate validation.
