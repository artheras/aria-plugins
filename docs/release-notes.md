# Aria Plugins 0.4.0

Commerce now supports a stable merchant identity, persisted product management,
owner-scoped order history and search, invoice revision with renewed approval,
unstarted-order cancellation and expiry, manual recovery review, and verified
full-refund handling. Unknown refund outcomes are reconciled without an automatic
second refund. Partial refunds and failed capture states require provider review.

Versioned macOS, Linux and Windows bundles include precompiled Go MCP tools, an
installation/configuration helper, integrity locks, checksums and public schemas.
`aria-plugins doctor` checks configuration, MCP discovery and read-only service
connectivity. Three MCP tools remain exposed; monetary actions remain browser-only.

Existing deployments must review the stable-owner migration guide before changing
merchant identity or rotating secrets. The public Demo remains a simulation.
Real AI, PayPal Sandbox transactions and provider failure recovery require the
live checklist and credentials; local tests use deterministic/mock providers.
