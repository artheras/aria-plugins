# Aria Plugins

Reusable workflows, MCP integrations and client configuration for ChatGPT, Claude, Codex and Aria Code.

Choose a plugin below, follow its setup guide, and authorize the permissions it needs. Client support depends on the plugin's transport, required service and the host's available features.

## Plugins

| Plugin | Purpose | Connection and clients | Current requirements |
| --- | --- | --- | --- |
| [Commerce Remote](plugins/commerce-remote/README.md) | Check account connections and read PayPal invoices with verified permission | Remote MCP with OAuth; ChatGPT, Claude, Codex and the Aria Code Go connector | Hosted Sandbox pilot. Independent PayPal merchant onboarding is not configured; accounts report `not_connected`. |
| [Commerce Sandbox](plugins/commerce/README.md) | Propose catalogue orders for human review and Sandbox checkout | Local stdio MCP bridge; Codex, Claude Code and Aria Code | Separately configured Commerce service and agent credential. Legacy remote order tools require operator opt-in. |
| [Finance](plugins/finance/README.md) | Financial statements, disclosure evidence, risk analysis and simulated backtests | Local stdio MCP; Codex, Claude Code and Aria Code | Install the independent [Financial Service](https://github.com/artheras/financial-service). Network data and broker reads require additional configuration. |
| [Computer Use](plugins/computer-use/README.md) | Operate an isolated browser with approved actions | Local stdio MCP; Codex, Claude Code and Aria Code | An Aria Code build with `aria-computer-use`, Python and Chromium. Browser actions require the macOS approval adapter; Windows/Linux currently expose capability checks only. |

The repository includes plugin metadata, workflow instructions, configuration templates and the Go Commerce backend. Finance algorithms and the Computer Use runtime live in their linked service repositories.

## Connect Commerce Remote

Follow the [remote connection guide](plugins/commerce-remote/README.md) for your client:

- **ChatGPT and Claude web/desktop:** add the remote MCP service through the host's custom connection settings, where supported.
- **Codex and Claude Code:** register the remote MCP URL and complete browser OAuth.
- **Aria Code:** build the Go connector, run its browser login, and add its stdio configuration.

The hosted pilot uses Google sign-in and assigned merchant access. A custom connection is separate from a public directory listing; public distribution requires the host platform's review.

The default remote tools are:

| Tool | Purpose |
| --- | --- |
| `commerce_profile` | Read the current Aria account and assigned merchant context |
| `paypal_connection_status` | Check the separate PayPal merchant authorization state |
| `paypal_list_invoices` | Read invoices when both client and provider permissions are verified |

Start by asking: **“Check my PayPal connection status.”**

Invoice reads require explicit `paypal.read` client consent and a verified, user-bound PayPal merchant grant with invoice-read capability. Existing connections do not gain this permission automatically.

**Current status:** the invoice reader and encrypted grant vault are implemented and tested locally. The hosted service has no independent PayPal onboarding adapter or configured grant vault, so real user merchant authorization and invoice reads remain unverified. See [PayPal account access](docs/paypal-accounts.md) for configuration and limits.

## Install workflows and local services

Workflow installation, MCP registration and account authorization are separate steps.

For Aria Code workflows:

```text
/skills add artheras/aria-plugins
/skills doctor
```

This installs workflow instructions; it does not start a service, register every MCP server or authorize an account. Use each plugin's setup guide for its runtime and configuration.

For the separate **Commerce Sandbox stdio bridge**, the macOS/Linux installer downloads a release bundle, checks its published SHA256 checksum and registers the bridge with Aria Code:

```sh
curl -fsSL https://raw.githubusercontent.com/artheras/aria-plugins/main/install.sh | sh
```

This installer targets the Sandbox order workflow. It does not configure Commerce Remote OAuth, install Finance or Computer Use dependencies, or connect a user's PayPal account. Windows users can use the release ZIP and [bundle guide](docs/bundle-readme.md).

See the [Sandbox cross-client guide](docs/clients.md) for Codex and Claude Code, or the [Sandbox Aria Code guide](docs/aria-code.md) for manual setup.

## Account permissions

Aria sign-in identifies the user. Client OAuth grants access to selected Aria tools. PayPal merchant consent separately authorizes supported provider data. All required permissions must be present before an invoice read succeeds; shared Sandbox checkout credentials cannot substitute for a user grant.

The account reader returns a limited invoice summary. It does not provide general access to balances, all transactions or arbitrary PayPal account management. No remote MCP tools approve orders, initiate checkout, capture payments or issue refunds.

The separate Sandbox order workflow uses browser review bound to the exact order. Its fixed USD catalogue and assigned merchant setup are test-workflow limits. Existing order review links remain available. Catalogue and draft tools are excluded from the default remote tool list.

Where configured, users can revoke client connections and disconnect a stored PayPal grant from Aria. Local disconnection does not revoke PayPal-side consent. Read [account access](docs/accounts.md) and [PayPal grant storage and revocation](docs/paypal-accounts.md) before deploying.

## Development

The Commerce backend and connector use Go. From a source checkout with the toolchain declared in [go.mod](go.mod):

```sh
make build
make test
```

`make test` runs Go race tests, vet and catalogue validation. Local mock-provider tests do not establish successful live PayPal authorization or payment processing.

Edit canonical workflows inside `plugins/<name>/skills/`, then run `make catalog-sync`. Commit the generated root skill mirrors and integrity lock together.

| Path | Contents |
| --- | --- |
| `plugins/` | Workflow packages, host manifests and MCP configuration templates |
| `skills/` and `.claude-plugin/` | Aria-compatible discovery, generated mirrors and integrity hashes |
| `apps/commerce/` | HTTP service, OAuth and account/order review UI |
| `cmd/` | Service, connector, local MCP bridge and administration commands |
| `packages/` | Commerce, PayPal account access, storage and shared Go libraries |
| `contracts/` | HTTP/OpenAPI and plugin package schemas |
| `docs/` | Setup, architecture, operations and validation guides |

Further reading: [Cloud Run deployment](docs/cloud-run.md), [HTTP architecture](docs/architecture.md), [operations](docs/operations.md), [validation](docs/testing.md), and [contributing](CONTRIBUTING.md).

Licensed under [Apache License 2.0](LICENSE).
