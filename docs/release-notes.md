# Aria Plugins 0.5.0

The catalogue includes Commerce, Finance and Computer Use portable workflows.
Computer Use connects to the local `aria-computer-use` MCP executable from
Aria Code 0.166.0 or later, installed with the browser extra and Chromium.
The Go release bundles contain the workflow and host templates, not Python or
Chromium. Its browser session is isolated and restricted to user-configured
origins; every navigation and input action requires native macOS approval.
No desktop input or unattended publishing is exposed by this plugin.

Aria Code, Codex and Claude Code host templates are included. Native and Python
Aria Code paths forward browser observations and images to vision-capable models.
The installer validates the catalogue, generated skill mirror and integrity locks.

Commerce and Finance retain their separate permissions and service requirements.
No provider credentials are included, and no external transactions or publishing
are enabled by installing the catalogue.
