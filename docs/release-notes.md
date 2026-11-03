# Aria Plugins 0.6.0

Commerce provides a remote Streamable HTTP MCP endpoint with OAuth, Google account
sign-in, explicit user consent and per-app revocation. Resource-bound access lasts
up to 15 minutes; optional rotating renewal credentials last at most 30 days.
Authorization codes and renewal credentials have replay protection. Reader roles
receive read-only tools; membership and revocation are checked on every call.

The remote package supports ChatGPT/Codex portable manifests and Claude Code
metadata. The new Go `aria-commerce-connect` bridge provides browser login,
private local credential storage, renewal, disconnect and stdio transport for
Aria Code. Existing manual-credential clients remain supported. Public directory
publication and unrelated merchant onboarding require separate platform setup.

The four remote tools provide profile, catalogue, unapproved drafts and order
status. There are no approval, checkout, capture or refund tools. PayPal remains
Sandbox-only. Bundles include the connector, existing installation helper and
workflows with published SHA-256 checksums.
