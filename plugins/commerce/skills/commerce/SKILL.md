---
name: commerce
description: Prepare and inspect catalogue orders using the Go Aria Commerce MCP bridge; human approval and payment stay in the browser.
---

Use this skill when the user asks to draft an order from the Commerce catalogue or inspect an existing order.

The Go service must be running and the Commerce MCP bridge must be configured. In Aria Code, add this catalogue with /skills add artheras/aria-plugins, inspect it with /skills doctor, and explicitly invoke $commerce:commerce. Skill installation does not build binaries or register MCP servers. Follow the repository's docs/aria-code.zh-CN.md for those steps.

If the MCP tools are unavailable, explain which setup step is missing. Do not invent tool results or silently replace the merchant service with another provider. The runtime may prefix tool names with its server namespace; use the exposed Commerce tools matching the names below.

Available tools:

- commerce_catalog: read trusted SKUs and integer-cent prices.
- commerce_draft_order: create an unapproved draft from a request.
- commerce_get_order: read a merchant-owned order, payment status, and audit events.

Confirm uncertain quantities with the user. Use the catalogue as the price source. A draft is not a payment. Give the returned order ID to the user, who reviews it in the connected merchant browser session.

Do not bypass browser confirmation with shell commands, HTTP requests, or another tool. Never assert money moved in demo mode. State a PayPal payment succeeded only when commerce_get_order returns paid with a reconciled Sandbox capture.
