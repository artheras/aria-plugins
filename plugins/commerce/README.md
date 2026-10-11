# Aria Commerce plugin

An Aria Code workflow that reads a merchant catalogue, prepares an order draft, and reports order status through the Go Commerce MCP bridge. The user reviews and pays in the merchant browser session.

Version: **0.2.0**. Runtime: Go 1.26+; MCP stdio. Default mode is a labelled, credential-free simulation. PayPal Sandbox and AI Gateway require credentials.

| Component | Purpose |
| --- | --- |
| .aria-plugin/plugin.json | Aria package metadata and command/skill entrypoints |
| .claude-plugin/plugin.json | Claude-format metadata for reuse |
| skills/commerce | Canonical workflow and declared policy |
| commands/commerce.md | Command instructions for hosts that load plugin commands |
| mcp.json | Aria server configuration template; replace the absolute binary path |
| .mcp.json | Claude server configuration: bridge binary on PATH and exported environment values |

In Aria Code, run `/skills add artheras/aria-plugins`, then `/skills doctor`. Invoke `$commerce:commerce` in a request. The current Aria installer loads verified skills; it does **not** automatically load the command file, build Go binaries, register MCP, or start the web service. Use the [Aria Code setup guide](../../docs/aria-code.zh-CN.md).

Build the service and MCP binaries with `make build` at the repository root. The service and bridge use the same `ARIA_MERCHANT_TOKEN` and `ARIA_COMMERCE_URL`. No approval, checkout, or capture tool is exposed to the agent.

The Claude manifest and .mcp.json are supplied for cross-client reuse; a full Claude Code installation has not been verified. Installing a plugin does not supply payment credentials or authorize a payment.
