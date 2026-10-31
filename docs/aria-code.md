# Connect to Aria Code

`aria-plugins` is the plugin catalogue for Aria Code. Commerce is the first plugin; its Go service and browser demo are the tools and examples for that plugin. Additional services can live in the same catalogue, with independent names, versions, skills, and MCP configurations.

## 1. Install the skill

In an Aria Code session that supports catalogue installation, enter these commands separately:

```text
/skills add artheras/aria-plugins
/skills doctor
```

The installed skill should appear as `commerce:commerce` with `verified` integrity. To pin a release or commit:

```text
/skills add artheras/aria-plugins --ref <FULL_COMMIT_SHA_OR_RELEASE_TAG>
```

The current installer refuses to overwrite an installed catalogue and does not automatically upgrade it. Preserve any local changes before explicitly removing or updating that catalogue. This repository does not include a script that deletes user installations. If `/skills add` is unavailable, check that your client includes the [catalogue installer](https://github.com/artheras/aria-code/blob/main/src/aria_code/apps/cli/commands/model_cmds.py).

Catalogues normally live under `skill-catalogs/artheras/aria-plugins` in the Aria state directory. Aria uses `ARIA_HOME` first, then an existing legacy `~/.arthera`, or `~/.aria-code` for new users. `ARIA_SKILL_CATALOG_HOME` can override the catalogue location separately.

The `.claude-plugin/marketplace.json` directory name is the compatibility format used by the current Aria loader. Aria reads root `skills/*/SKILL.md`; the canonical files live in `plugins/commerce/skills/commerce`. The Go catalogue tool generates identical root mirrors and their integrity lock.

## 2. Build and run Commerce

Install Go 1.26 or newer. In a separate repository checkout, run:

```sh
git clone https://github.com/artheras/aria-plugins.git
cd aria-plugins
make build
make catalog-check
export ARIA_COMMERCE_URL=http://127.0.0.1:4180
export ARIA_MERCHANT_TOKEN="$(openssl rand -hex 32)"
ARIA_MERCHANT_LOGIN_TOKEN="$(openssl rand -hex 32)" ./bin/aria-commerce
```

This starts demo mode by default. There are two different secrets:

- `ARIA_MERCHANT_TOKEN` is the **agent** credential. The service and the Aria MCP process use the same value. It can read the catalogue and draft orders, never approve, capture, or refund.
- `ARIA_MERCHANT_LOGIN_TOKEN` is the **human** merchant's browser login. Set it only on the Commerce server process (as above, not exported) and keep it in your password manager. It must differ from `ARIA_MERCHANT_TOKEN`; the service refuses to start if they are equal, and `/api/login` rejects the agent token.

Do not generate different tokens while continuing to connect to the original server, and do not commit tokens to the repository.

## 3. Register the MCP tools

Merge the server entry from [mcp.json](../plugins/commerce/mcp.json) into `mcp_servers.json` in your actual Aria state directory. Preserve other server entries.

- Replace `command` with the literal absolute path to your built `bin/aria-commerce-mcp`.
- Export the same `ARIA_COMMERCE_URL` and `ARIA_MERCHANT_TOKEN` in the shell that starts Aria Code. Never export `ARIA_MERCHANT_LOGIN_TOKEN` there: everything in that shell is visible to the agent.
- The current Aria client expands `${VARIABLE_NAME}` in `args` and `env`, but not in `command`. Do not leave an environment placeholder in the command path.
- Restart the Aria session after saving the configuration so it discovers the tools again.

The tools are `commerce_catalog`, `commerce_draft_order`, and `commerce_get_order`. The model may see names prefixed with `mcp__aria-commerce__`.

## 4. Invoke the plugin and review the result

In Aria Code, enter:

```text
$commerce:commerce Draft an order for 2 tees and 1 mug.
```

After reading the catalogue and drafting the order, Aria returns an order ID. At `http://127.0.0.1:4180`, click **Merchant session**, enter the merchant login token (`ARIA_MERCHANT_LOGIN_TOKEN`, not the agent token), and open `/?order=RETURNED_ORDER_ID`. Review the invoice, then approve, check out, and simulate payment in the browser. These actions are not available to the agent through MCP.

Demo mode uses a deterministic parser, makes no AI calls, and moves no money. Configure AI Gateway and PayPal Sandbox using the [setup guide](setup.md). Missing configuration is reported rather than treated as a successful simulation.

## 5. Installation scope and future extensions

The current `/skills add` installs, discovers, and verifies skills. It does not build Go binaries, start services, import MCP configuration, or register `commands/commerce.md` as a new slash command.

A complete plugin manager needs catalogue indexing, component registration, dependency configuration, and version updates in the `aria-code` host. This repository provides the publishing structure for those components; today, connect the skill and MCP tools using the separate steps above.

Claude-format manifests and `.mcp.json` are also included. For that configuration, add the built `bin/` directory to PATH and export the service URL and token. The complete Claude Code installation flow still needs separate validation. Claude's `/plugin install` command is not an implemented Aria command.

References: [Anthropic's official plugin catalogue](https://github.com/anthropics/claude-plugins-official), [Aria skill loader](https://github.com/artheras/aria-code/blob/main/src/aria_code/packages/aria_skills/loader.py), [Aria MCP client](https://github.com/artheras/aria-code/blob/main/src/aria_code/mcp_client.py).


## Precompiled tools

Go is optional for MCP clients using [a verified release bundle](bundle-readme.md). The `aria-plugins` helper can merge the explicit MCP entry into your chosen configuration and verify connectivity with `doctor`. Use the matching release tag for skill installation. The helper does not start an Aria session or configure its model.

## Use your Aria Code login for drafting (local)

When the Commerce server runs on the same machine as a signed-in Aria Code, set
`ARIA_AI=aria` and it drafts with that account's model access: no
`AI_GATEWAY_API_KEY` to create. Sign in with `/login` first. `AI_GATEWAY_MODEL`
overrides the default model, and `ARIA_API_URL` the gateway address. The login is
read on every request, because Aria refreshes it about hourly. The server refuses
this setting on Cloud Run or Vercel: a hosted service must not hold a user's login.
Prices and approval are unchanged; the model still only proposes a draft.

