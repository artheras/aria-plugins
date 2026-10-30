# Aria Plugins 0.4.0 — Commerce tools

This package contains the Go MCP bridge, the `aria-plugins` installation helper,
the versioned Commerce workflow, integrity locks, and public JSON contracts.

1. Download the ZIP for your operating system and architecture, plus SHA256SUMS,
   from https://github.com/artheras/aria-plugins/releases/tag/v0.4.0.
2. Verify the checksum before running the extracted installation helper.
3. Install into a directory you own:

```sh
./bin/aria-plugins install --archive /absolute/path/to/bundle.zip \
  --sha256 PUBLISHED_SHA256 --dir /absolute/path/to/aria-plugin-tools \
  --config /absolute/path/to/your/aria-state/mcp_servers.json
```

The helper creates a version directory, uses a literal absolute MCP binary path,
preserves unrelated MCP servers, and creates a configuration backup. It does not
start the bridge, modify installed skills, create a server, or store credentials.
Supply `--replace` explicitly when switching an existing Commerce entry to a new
version; retain the prior backup before creating another one.

Export ARIA_COMMERCE_URL and ARIA_MERCHANT_TOKEN in the shell that starts Aria.
For private Cloud Run, also configure ADC and ARIA_CLOUD_RUN_AUDIENCE as described
in https://github.com/artheras/aria-plugins/blob/main/docs/cloud-run.md.

```sh
./bin/aria-plugins doctor --dir /absolute/path/to/aria-plugin-tools/0.4.0
```

Doctor negotiates MCP, checks the three tools, and makes one read-only catalogue
call. It does not approve, capture or refund a payment.

Install the matching skill in a compatible Aria Code session:

```text
/skills add artheras/aria-plugins --ref v0.4.0
/skills doctor
```

Restart Aria after MCP registration. Use `$commerce:commerce` to request a draft.
The minimum documented host baseline is 0.140.0; component registration is done
by this helper, while skill discovery and model invocation belong to Aria Code.
See https://github.com/artheras/aria-plugins/blob/main/docs/aria-code.md for details.

To remove only a tool version, run `aria-plugins uninstall --dir VERSION_DIR`.
Its MCP configuration entry is retained for explicit removal or disabling;
skills, credentials, other versions and order data are preserved.

The catalogue also includes the Finance skill and configuration templates. The Finance Python service is installed separately from artheras/financial-service; this bundle does not install it or enable external financial data access.
