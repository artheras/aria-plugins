# Aria Plugins

Reusable Go modules and Aria-compatible command/skill packages for work that needs a human decision.

The first application is **Aria Commerce**: a natural-language request becomes a catalogue order draft, the user approves its exact contents, and a PayPal Sandbox capture is checked against the invoice before the order is marked paid.

**Current verification:** credential-free demo and mock provider integration are tested locally. Real AI Gateway calls, real PayPal Sandbox transactions, webhook delivery, and a Commerce cloud deployment still require configuration and live validation. The existing Aria Code marketing site is a separate project.

## Try locally

Go 1.26 or newer; no third-party Go dependencies.

```sh
git clone https://github.com/artheras/aria-plugins.git
cd aria-plugins
make build
./bin/aria-commerce
```

Open http://127.0.0.1:4180 and create an order for **2 tees and 1 mug**. Review the $60.00 invoice, approve it, create checkout, simulate payment, and download the labelled receipt. Refreshing preserves the server's order record.

Default demo mode uses a small deterministic parser and simulated payment. It makes no AI or PayPal calls and moves no money. Sandbox mode never silently falls back to demo.

![Commerce demo: reviewed and reconciled simulated order](docs/commerce-preview.jpg)

## Repository

| Path | Role |
| --- | --- |
| apps/commerce | HTTP workflow and embedded HTML/CSS/JavaScript interface |
| cmd/server | Go service entrypoint |
| cmd/commerce-mcp | Go MCP stdio adapter: catalogue, draft, status |
| packages/sdk | Shared invoice, catalogue, payment, and audit contracts |
| packages/agents | Demo parser and AI Gateway HTTP adapter |
| packages/approvals | Exact-invoice approval digest and expiry |
| packages/payments | Provider interface, demo provider, PayPal Sandbox implementation |
| packages/store | Atomic file persistence and Redis REST compare-and-swap |
| plugins/commerce | Aria command and skill package |
| examples | Aria MCP configuration |
| docs | Setup, architecture, validation, and submission draft |

The Go HTTP service is independently usable. Commands and skills describe how Aria uses its MCP tools; they do not imply a new Go plugin installer. Future Rust implementations can expose the same HTTP or MCP contracts.

## Configure and validate

- [Setup: PayPal, AI Gateway, MCP, Docker, Vercel](docs/setup.md)
- [中文配置说明](docs/setup.zh-CN.md)
- [Architecture and HTTP contract](docs/architecture.md)
- [Validation and remaining live checks](docs/testing.md)
- [Devpost story draft](docs/devpost-story.md)

```sh
make test
```

This starter supports one merchant and a fixed USD catalogue. Approval, checkout, and capture require a browser session and are not exposed to MCP. Local file storage supports one server process; serverless or multiple instances must use Redis. MIT licensed.
