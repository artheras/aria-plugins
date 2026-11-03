# Use Commerce in Codex, Claude Code and Aria Code

All three clients use the same Go MCP bridge and Commerce service. The connected
client reads the catalogue, interprets the user's request with its own model and
submits only SKU/quantity selections. Commerce validates those selections, uses
its current prices and returns an **unapproved** order with a browser review URL.
No additional server-side model account is needed for this path.

## Build and run

From an updated checkout containing `host_draft_items` support:

```sh
make build
```

Run `bin/aria-commerce` in a separate terminal. Default mode simulates payments.
Keep `ARIA_MERCHANT_TOKEN` identical in the service and client environment. Set a
different `ARIA_MERCHANT_LOGIN_TOKEN` on the service process only. The agent token
cannot log in as the human merchant. Keep both out of source control and logs.

For real PayPal **Sandbox** integrations, `ARIA_AI=host` uses the connected client
as the drafter and needs no AI Gateway/Vertex credentials. Configure the remaining
Sandbox, merchant and persistence settings from [setup](setup.md). Browser-only
text drafting is unavailable in this mode; create drafts in the connected client
and open their review links. No production payments are supported by this setup.

## Codex

Generate a native TOML snippet with an absolute executable path:

```sh
./bin/aria-plugins config --client codex --binary "$PWD/bin/aria-commerce-mcp"
```

Merge its `[mcp_servers.aria-commerce]` table into your Codex configuration; preserve
other servers and settings. The `env_vars` list forwards the service URL, the agent
credential and optional private Cloud Run audience from the launching environment.
It does not store literal credentials in the configuration. For Codex desktop,
ensure these variables are available to the app process as well as your terminal.
See [Codex MCP configuration](https://developers.openai.com/codex/mcp).

Check registration with `codex mcp get aria-commerce`. Restart/reconnect the client
to discover tools. Registration alone does not prove a tool call succeeded.

## Claude Code

Generate the configuration:

```sh
./bin/aria-plugins config --client claude-code --binary "$PWD/bin/aria-commerce-mcp"
```

Merge its `mcpServers` entry into the target project's `.mcp.json`. It references
`ARIA_COMMERCE_URL` and `ARIA_MERCHANT_TOKEN` from the environment rather than
storing their values. Review and approve this project server in Claude Code, then
use `/mcp` or `claude mcp get aria-commerce` to inspect connectivity. Do not use
`--env TOKEN=...` with a literal credential in shell history.

Alternatively, load the Commerce skill and MCP definition together as a local
plugin (the installed `aria-commerce-mcp` executable must be on PATH):

```sh
claude --plugin-dir ./plugins/commerce
```

Use either the standalone MCP entry or the plugin to avoid duplicate tools.
[Claude Code MCP](https://code.claude.com/docs/en/mcp) documents project approval
and environment expansion. The skill is guidance; MCP supplies executable tools.

## Aria Code

The existing [Aria Code setup](aria-code.md) remains supported. The helper can also
render its native configuration with `--client aria`.

For a verified installed bundle, `--dir /absolute/path/to/installed/version` can
replace `--binary` in these configuration commands. The helper prints a snippet;
it does not overwrite client files or install a skill implicitly.

## Use the tools

Ask your client:

> Read the Commerce catalogue and draft an order for two cotton tees and one mug.
> Use your own model to choose the items. Do not approve or pay.

The normal tool sequence is:

1. `commerce_catalog` supplies available SKUs and prices.
2. `commerce_draft_order` receives `items`, for example
   `{"items":[{"sku":"TSH-001","quantity":2},{"sku":"MUG-003","quantity":1}]}`.
3. Open the returned URL in a human merchant browser session to review and approve.
4. `commerce_get_order` reads the resulting state and audit trail.

Clarify ambiguous requests before proposing items. The optional `prompt` argument
uses the separately configured server model instead. Send exactly one of `items`
or `prompt`. The bridge refuses structured proposals if the service does not
advertise `host_draft_items`; update the service rather than falling back silently.

## Boundaries and checks

The agent cannot set prices, assert payment status, approve, create checkout,
capture, refund or manage products through MCP. These require browser authority.
A host-generated selection is a proposal, not independently verified AI output.
Human approval is invalidated by invoice changes. Sandbox payment success requires
actual provider reconciliation; a draft, redirect or demo receipt is insufficient.

Existing `aria-plugins doctor --dir VERSION_DIR` verifies installation integrity,
MCP negotiation, the three-tool surface and a read-only catalogue request. It does
not verify a client's model credentials or a real PayPal transaction. A remote
service used by several merchants still needs per-user identity and scoped
credentials; the current service is a single-merchant prototype, not shared SaaS
OAuth onboarding.
