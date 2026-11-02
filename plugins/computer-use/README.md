# Aria Computer Use

Local stdio MCP service for Codex, Claude Code and Aria Code. The service is implemented in Aria Code; this plugin supplies host configuration and workflow instructions.

Requires an Aria Code build containing `aria-computer-use`, Python 3.10+, Chromium and a graphical macOS login session. Windows/Linux can inspect capabilities but actions are denied until a native approval adapter is implemented.

```sh
python -m pip install 'aria-code[browser]'
python -m playwright install chromium
```

The service entry point is new and must be present in the installed build; older published releases do not include it. For an unmerged development checkout, install `pip install -e '/absolute/path/to/aria-code[browser]'` instead. Confirm `aria-computer-use` exists before enabling the plugin.

Copy the appropriate host configuration: `.mcp.json` for Claude Code, `codex.toml` into Codex's MCP configuration, or `mcp.json` into Aria Code's MCP configuration. Set `ARIA_COMPUTER_ORIGINS` in the host environment (or add it explicitly to the host MCP server configuration) to a comma-separated list of exact origins, such as `https://example.com,http://localhost:3000`. It is unset by default, so no page can open. Do not add sites without the user's authorization. Restart/reconnect the host MCP session after editing configuration.

Start by asking the model to check `browser_capabilities`, then open an allowed page. Every navigation, click, fill and keypress asks for one-time approval in a native macOS dialog. Cancellation, timeout or an unavailable dialog denies the action. No tool can grant approval. Observe calls capture the current browser page, including potentially sensitive page content, and send it to the host model; enable only sites appropriate for that model's data handling.

The browser remains open across calls within one MCP process, uses its own temporary profile and closes at process exit. It does not attach to Chrome or reuse saved credentials. Complete login manually in the visible isolated window. Downloads are disabled, popups close, and document navigation outside the allowlist is blocked. Normal page subresources may use third-party hosts; the origin allowlist is a navigation boundary, not a full network sandbox.

Actions require an observation less than 60 seconds old with an unchanged URL and DOM. After an action, read the new observation and screenshot to verify the result. Dynamic pages may require another observation. This release does not control native desktop apps, manage background schedules or auto-publish content. OS desktop tools already in Aria Code retain their separate approval rules.
