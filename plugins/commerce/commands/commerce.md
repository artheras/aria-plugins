---
description: Prepare a catalogue order for human review in Aria Commerce
---

Read the commerce skill and use the configured Commerce MCP server.

1. Query commerce_catalog before proposing products or prices.
2. Ask for missing quantities rather than inventing them.
3. Call commerce_draft_order with the user's request.
4. Explain the returned line items and total. Give the order ID and Commerce website URL.
5. The user must connect the merchant browser session and review the draft there.

Never claim a draft is paid. Never approve, create checkout, or capture a payment on the user's behalf. Those actions are intentionally unavailable through MCP. In demo mode explicitly identify the local parser and simulated payment.
