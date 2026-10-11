# Aria Plugins

The plugin catalogue for **Aria Code**: installable workflows, plugin metadata, and Go MCP tools. The layout follows the marketplace/package separation used by [Anthropic's official plugins](https://github.com/anthropics/claude-plugins-official).

**Commerce** is the first plugin. Its Go service turns a natural-language request into a catalogue order draft. The user reviews its exact contents in the browser, and a PayPal Sandbox capture is reconciled before the order is marked paid. The web application is an example of the plugin's tools.

## Connect to Aria Code

Inside a current Aria Code session:

```text
/skills add artheras/aria-plugins
/skills doctor
```

Then request, for example:

```text
$commerce:commerce Draft an order for 2 tees and 1 mug.
```

The current Aria installer discovers root `skills/*/SKILL.md`, reads `.claude-plugin/marketplace.json`, and verifies `skills.lock.json`. The directory name is required by the existing Aria loader; it is a compatibility format.

Skill installation does not register commands/MCP or build binaries. Configure and run the Go bridge using the [Aria Code connection guide](docs/aria-code.zh-CN.md). A full plugin manager that installs every component together belongs in the Aria Code host.

## Catalogue layout

```text
.claude-plugin/
  marketplace.json          # Plugin catalogue read by Aria Code
  skills.lock.json          # Generated Aria skill integrity hashes
plugins/
  commerce/
    .aria-plugin/plugin.json
    .claude-plugin/plugin.json
    skills/commerce/         # Canonical workflow and policy
    commands/commerce.md
    mcp.json                # Aria configuration template
    .mcp.json               # Claude-format MCP configuration
    README.md
skills/commerce/             # Generated mirror for the current Aria installer
packages/                   # Reusable Go libraries and catalogue validator
cmd/                        # Go service, MCP bridge and catalogue tooling
apps/commerce/              # Browser example
```

Edit skills inside their plugin, then run `make catalog-sync`. Commit the generated mirror and lock together. `make catalog-check` and CI reject changed hashes, stale mirrors, invalid paths, and inconsistent plugin versions. See [CONTRIBUTING.md](CONTRIBUTING.md) to add another plugin. Only implemented plugins are listed.

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
| plugins/commerce | Installable workflow, host manifests and MCP configuration |
| skills / .claude-plugin | Aria-compatible catalogue discovery and integrity |
| packages/catalog / cmd/catalog | Go catalogue validation and mirror generation |
| examples | Aria MCP configuration |
| docs | Setup, architecture, validation, and submission draft |

Plugin names such as `commerce` are stable identifiers; change `displayName` for branding. New Go or Rust tools can implement the same MCP contracts without changing the catalogue's workflow format.

## Configure and validate

- [Setup: PayPal, AI Gateway, MCP, Docker, Vercel](docs/setup.md)
- [中文配置说明](docs/setup.zh-CN.md)
- [连接 Aria Code 与添加插件](docs/aria-code.zh-CN.md)
- [Architecture and HTTP contract](docs/architecture.md)
- [Validation and remaining live checks](docs/testing.md)
- [Devpost story draft](docs/devpost-story.md)

```sh
make test
```

This starter supports one merchant and a fixed USD catalogue. Approval, checkout, and capture require a browser session and are not exposed to MCP. Local file storage supports one server process; serverless or multiple instances must use Redis. MIT licensed.

**Verification limits:** the credential-free demo and mock providers are tested locally. Real AI Gateway calls, PayPal Sandbox transactions, webhook delivery, a full interactive Aria/Claude agent session, and Commerce cloud deployment still require configuration and live validation. The Aria Code marketing site is a separate project.
