# Aria Healthcare plugin

Public PubMed literature and evidence workflows, backed by
[healthcare-service](https://github.com/artheras/healthcare-service). No patient
records, diagnosis or prescribing. The service runs separately; installing a
skill does not install or register it.

Install the service in a Python 3.11+ venv. Replace the absolute interpreter and
private workspace paths in `mcp.json` (Aria Code), `.mcp.json` (Claude) or
`codex.toml` (Codex), then merge that entry into the host configuration. `-I`
prevents imports from an untrusted working directory. Use a separate private
workspace per owner/project. Network access defaults off; enable
`ARIA_HEALTH_ALLOW_NETWORK=1` only in the host environment for public NCBI queries.

In Aria Code, `/skills add artheras/aria-plugins` installs the catalogue workflows;
merge the MCP server configuration separately, then `/mcp reload` and `/mcp tools`.
Verify `health_capabilities` before starting a research task.

Web, desktop and iOS use the private backend's authenticated official-plugin
bridge. They do not execute the local stdio template themselves. Operators must
install the pinned service and persistent owner-isolated storage first; clients
then enable the service from the official plugin section. No arbitrary shell
command or patient data is accepted by the host setup flow.

Research records and versioned edits persist locally. Every evidence bundle needs
human review; matching quotes alone do not validate scientific claims. MCP has no
operator-review/clinical-approval tool. Consult the service README for exact
limitations, offline tests and local review tooling.
