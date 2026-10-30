# Arthera Finance plugin

This plugin connects Codex, Claude Code and Aria Code to the shared
[Financial Service](https://github.com/artheras/financial-service). All financial
algorithms live in that repository. This catalogue contains workflow guidance,
metadata and configuration templates.

Install the service in its own Python 3.11+ virtual environment using that
repository's README. Install the Finance service separately from the Commerce tools.

For Codex, register the service executable:

```sh
codex mcp add arthera-finance -- /absolute/path/to/financial-service/.venv/bin/arthera-financial-service
```

For Claude Code, add this marketplace and select the finance plugin, then set
the command in .mcp.json to the literal absolute service executable path (or put
its venv/bin on PATH). Alternatively:

```sh
claude mcp add --transport stdio arthera-finance -- /absolute/path/to/financial-service/.venv/bin/arthera-financial-service
```

For Aria Code, enter:

```text
/mcp finance install "/absolute/path/to/financial-service/.venv/bin/python"
/mcp reload
/skills add artheras/aria-plugins
/skills doctor
```

An already installed catalogue is not automatically overwritten.
You can also merge the entry from mcp.json into the actual Aria
mcp_servers.json, replacing command with the literal service executable path,
then restart. Skill installation does not register commands or MCP servers.
The commands/finance.md file is a workflow entrypoint for hosts that support it;
Aria's current skill installer does not create a new /finance slash command.

The canonical skill is plugins/finance/skills/finance; skills/finance is the
generated mirror for Aria. Invoke $finance:finance after installing it in Aria.
Codex can use the MCP tools directly, or install this skill folder in its skill
directory. codex.toml and .mcp.json show the same server in host-specific formats.
No financial calculations are duplicated in these wrappers.

The service exposes six tools listed in package.json. Networking defaults to off.
Supplied-data calculations work without market credentials. Only the host can
enable FINANCIAL_SERVICE_NETWORK=1; SEC reads additionally need EDGAR_USER_AGENT.
These are read-only data/analysis tools and simulated backtests, with no trading
or publication capability. MCP validation failures are reported as errors.
