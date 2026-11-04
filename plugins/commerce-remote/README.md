# Aria Commerce remote plugin

Connect from ChatGPT, Codex, Claude or Aria using OAuth. The default tools report
separate PayPal authorization status and read invoices only with explicit client
and verified provider permission. The hosted pilot has no independent PayPal
merchant-onboarding flow configured and reports `not_connected`.

Remote URL: `https://aria-commerce-sandbox-vrkp6bxpya-nw.a.run.app/mcp`

## ChatGPT

In Plugins, add a custom MCP server with the remote URL and OAuth authentication.
The server advertises automatic public-client registration; no Google client ID
or secret belongs in this dialog. Sign in, check the callback and permissions,
and choose Allow connection. ChatGPT can check PayPal connection status. Invoice access requires a new explicit
`paypal.read` scope and a separately verified PayPal merchant grant.

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

## Permissions and provider configuration

`read` allows Aria connection status; opt-in legacy tools may read assigned test
records. `paypal.read` is a separate invoice-read permission, never added to old
grants automatically. `offline_access` allows renewal for at most 30 days.
`draft` is only for legacy unapproved Aria orders, not PayPal account management.
The default remote tool list omits catalogue and draft tools.

After a verified provider connection exists, Aria users can explicitly request
invoice access with `aria-commerce-connect login --paypal-read`. This opens a new
client permission screen; it does not authorize PayPal by itself. For native
Codex, request scopes `read,paypal.read,offline_access` during login. ChatGPT and
Claude use the same remote service and its OAuth metadata.

Read [provider setup and limits](../../docs/paypal-accounts.md) before enabling
account data. Never paste PayPal tokens in chat or client configuration. The Go
reader requires a server-owned, user-bound grant source; the hosted service has
none configured. Existing Aria login and shared Sandbox checkout credentials
cannot satisfy it. There are no approve, checkout, capture or refund MCP tools.

OAuth callbacks are limited to registered client paths and loopback URLs. S256
PKCE, resource binding, one-use codes, refresh rotation, replay detection and
per-user revocation are enforced.
