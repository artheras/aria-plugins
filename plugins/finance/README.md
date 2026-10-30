# Arthera Finance plugin

This plugin connects Codex, Claude Code and Aria Code to the shared
[Financial Service](https://github.com/artheras/financial-service). All financial
algorithms live in that repository. This catalogue contains workflow guidance,
metadata and configuration templates.

Install the service in its own Python 3.11+ virtual environment using that
repository's README. During initial review, check out its
feat/standalone-finance-service branch; main initially contains only the README.
There is no published package or prebuilt Finance binary yet. The existing
Commerce installer does not install this Python service.

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

For the updated Aria Code host, enter:

```text
/mcp finance install "/absolute/path/to/financial-service/.venv/bin/python"
/mcp reload
/skills add artheras/aria-plugins
/skills doctor
```

Use a pinned review branch/commit with /skills add --ref while this catalogue PR
is unmerged. An already installed catalogue is not automatically overwritten.
For older hosts, merge the entry from mcp.json into the actual Aria
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
or publication capability. The updated Aria host also fixes MCP isError handling
so SDK validation failures cannot be presented as successful calculations.
