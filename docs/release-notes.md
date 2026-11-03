# Aria Plugins 0.5.1

Commerce connects Codex, Claude Code and Aria Code to one shared MCP service.
The client model proposes catalogue SKUs and quantities; Commerce validates them,
resolves current prices and returns an unapproved order with a browser review URL.
`ARIA_AI=host` supports PayPal Sandbox without a second server-model account.

The installation helper prints native client configurations with
`aria-plugins config --client codex|claude-code|aria --binary ABSOLUTE_PATH`.
Configurations reference credentials from the launching environment. Agent and
human merchant credentials remain separate. Approval, checkout, capture and refund
are browser-only. Older services reject host proposals until they are upgraded.

The release includes six platform bundles with SHA256SUMS, portable Commerce,
Finance and Computer Use workflows, and integrity locks. External services and
client model access must be configured separately. The current Commerce service
supports one merchant; it is not multi-tenant OAuth onboarding.

Local verification covers all Go race tests and vet, native Codex tool discovery
and catalogue/draft/status calls, Claude Code MCP connectivity and manifest
validation. Native-client checks used simulated payments; they do not establish
a PayPal Sandbox transaction, model inference or a clinical/financial outcome.
