# Aria Commerce — Project Story draft

This is an editable English draft for a future submission. Verify personal/team statements before using it. Complete live AI and PayPal Sandbox validation before presenting the project as a working integration; the current credential-free demo is a simulation.

## Inspiration

Small merchants should be able to describe an order naturally and still inspect the details before authorizing a payment workflow. Aria Commerce explores how an agent can help prepare work while keeping the user responsible for the transaction.

## What it does

The application converts an order request into catalogue selections, calculates trusted prices on the server, presents an invoice for review, and binds approval to that exact invoice. Its PayPal Sandbox adapter creates checkout and reconciles a completed capture against order identity, currency, and amount. The website shows an audit trail and can export a receipt.

The default local demo uses a bounded parser and simulated payment. Configured Sandbox mode uses an AI Gateway model and PayPal Sandbox; this mode needs real account configuration and an end-to-end validation run.

## How we built it

We implemented the backend and MCP bridge in Go, using the standard library. Agent drafting, approvals, payments, and storage have separate contracts so they can be reused in later Aria services. The frontend uses HTML, CSS, and JavaScript embedded in the Go binary.

The payment adapter uses server-side OAuth, stable request identifiers, and capture verification. Webhooks are verified through PayPal before the application reads the authoritative order. Local persistence uses atomic file writes; a Redis REST adapter supports cloud instances.

## Challenges we ran into

The main engineering challenge was keeping language-model suggestions outside the payment authority boundary. Prices must come from a trusted catalogue, approvals must cover the exact invoice, and retries must not turn into duplicate payments.

## Accomplishments that we're proud of

We built a complete credential-free workflow, reusable Go interfaces, a restricted MCP bridge, and tests for ownership, invoice tampering, idempotency, pending captures, and forged webhook signatures. Live integration results should be added here after verification.

## What we learned

An agent-generated draft is only one step. A useful commerce workflow also needs explicit human approval, durable state, provider reconciliation, and clear distinctions between a simulation and an external transaction.

## What's next for Arthera

Configure and validate real AI Gateway and PayPal Sandbox accounts, connect the bridge to Aria Code, publish the application with persistent storage, and record a concise demonstration. Later work can add inventory reservation, fulfilment, stronger merchant authentication, and refund/reversal handling.

## Submission evidence to add

- Working Commerce deployment URL.
- Public source repository and license.
- Real Sandbox transaction/receipt evidence without credentials.
- AI integration and webhook verification evidence.
- Demo video link.
- Accurate team contributions and a description of new work versus existing Aria components.

Do not submit the draft with placeholder evidence or claim simulated transactions as PayPal payments.
