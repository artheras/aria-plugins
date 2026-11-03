# Aria Commerce remote plugin

Connect your own assigned Commerce account using Google sign-in and an explicit
permission screen. The same remote MCP service supports ChatGPT, Codex and Claude;
Aria Code's current stdio client uses the included Go connection bridge.

Remote URL: `https://aria-commerce-sandbox-vrkp6bxpya-nw.a.run.app/mcp`

## ChatGPT

In Plugins, add a custom MCP server with the remote URL and OAuth authentication.
The server advertises automatic public-client registration; no Google client ID
or secret belongs in this dialog. Sign in, check the callback and permissions,
and choose Allow connection. ChatGPT can then read the catalogue, create an
unapproved order, and look up its status. Use the returned browser URL to review.

`plugin.json` and `mcp.json` are the portable package. The connection created in
ChatGPT has its own platform ID; do not invent one or reuse a Google client ID.
A custom connection is not a public directory listing. Public listing requires
platform review; account/organization availability depends on the host.

## Claude and Codex

```sh
claude mcp add --transport http aria-commerce-remote https://aria-commerce-sandbox-vrkp6bxpya-nw.a.run.app/mcp
codex mcp add aria-commerce-remote --url https://aria-commerce-sandbox-vrkp6bxpya-nw.a.run.app/mcp
codex mcp login aria-commerce-remote
```

Use Claude's `/mcp` screen to authenticate. The `.claude-plugin` manifest and
`.mcp.json` support Claude Code plugin packaging. Claude web/desktop custom
connectors use the same remote URL when supported by the user's plan/workspace.

## Aria Code

Build the Go connector with `make build`, then run:

```sh
/path/to/bin/aria-commerce-connect login
/path/to/bin/aria-commerce-connect status
```

Review the browser authorization screen. After login, merge the server entry
from `aria.mcp.example.json` into your existing MCP configuration, using your
connector's absolute executable path. Restart Aria. This bridge does not require
Python packages, a Google Cloud CLI, or a provider API key. Do not replace the
entire configuration or put credentials in the JSON.

`serve` speaks stdio MCP to Aria and remote Streamable HTTP to Commerce. The local
credential file is owner-only on POSIX. For Windows, retain the user profile's
private ACLs. `--store /absolute/path` selects a separate credential store for
another user/installation. Concurrent instances serialize refresh and atomically
save the rotated credential. Do not sync the credential file to a repository.

```sh
/path/to/bin/aria-commerce-connect logout
```

Logout revokes the remote connection before removing the local file. You can
also revoke it from Commerce → Your connected account → Connected apps; that
immediately stops access and renewal. A host's Disconnect control may only remove
its local configuration, so use the account page for guaranteed server revocation.

## Permissions

`read` reads the assigned merchant's products and orders. `draft` adds draft
creation; it never authorizes payment. `offline_access` permits renewal for a
maximum of 30 days. Access tokens last at most 15 minutes. Reader roles receive
read-only tools even if a client requested draft access. Every request checks
current account membership and revocation. Reconnect after expiry or revocation.

The profile tool returns an opaque stable account ID. This pilot supports users
assigned to one configured merchant; onboarding unrelated PayPal merchants is a
separate integration. There are no approve, checkout, capture or refund tools.

OAuth callbacks are limited to the documented ChatGPT/Claude callback paths and
exact registered loopback URLs. DCR, S256 PKCE, resource binding, one-use codes,
refresh rotation, replay detection and per-user revocation are implemented.
Client-ID metadata document fetching is not enabled.
