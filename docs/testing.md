# Verification

Run `make test` for race-detected Go tests and vet, then `make build` for both binaries.

Automated coverage includes:

- Integer cents, duplicate selections, unknown SKUs, unsafe quantities/amounts.
- Approval digest mismatch, changed invoice, approval expiry.
- AI Gateway JSON response boundary, invented products, price injection, fractional quantities.
- Sandbox OAuth, create/capture request structure, stable idempotency keys, completed-capture replay.
- Wrong currency/amount, pending capture, invalid webhook signature and missing signature headers.
- Browser session, CSRF, merchant bearer rejection for human actions, order ownership isolation.
- Full local demo draft → approval → checkout → capture → receipt, persistence after reopening, idempotent audit history.
- Verified webhook reconciliation and replay.
- File storage concurrent CAS, Redis conflict/failure response handling.
- MCP 2024-11-05 compatibility, notifications, tool discovery, absent payment tools.
- Missing Sandbox credentials fail closed; Vercel rejects local file persistence.

Tests use local mock HTTP providers. They do not prove live credentials, model capabilities, PayPal merchant configuration, or cloud connectivity.

## Manual local check

Start Commerce, open the default origin, and run the preset order through all four stages. Confirm the demo banner and simulated receipt labels, total $60.00, one payment event after repeated clicks, and restoration after refresh. Try the second preset: 3 caps + 2 mugs totals $78.00. Check a narrow screen and keyboard navigation.

## Live validation still needed

1. Configure Sandbox/AI environment variables and confirm startup rejects any missing required field.
2. Create a draft using a real supported Gateway model.
3. Approve the invoice, pay with a separate Sandbox buyer, capture, and compare the receipt with the PayPal dashboard.
4. Retry capture on the same order; confirm no second transaction or audit duplicate.
5. Configure a publicly reachable webhook, verify delivery/signature, and replay the event.
6. Deploy Go on Vercel with persistent Redis and verify restart/multiple-instance order continuity.
7. Run the Go MCP binary from the actual Aria client and review the returned draft in the merchant browser.
8. Record the real flow for the competition. Demonstration mode alone does not establish AI + PayPal integration.

Docker and Vercel execution, a real Redis service, actual Aria client wiring, and real AI/PayPal transactions are not claimed as verified.

## Implementation run

Verified with Go 1.26.9 on macOS ARM64: `go test -race ./...`, `go vet ./...`, and builds of both Go binaries. The embedded browser UI completed the $60.00 simulated order and restored the paid state after refresh. The actual compiled MCP binary also created and read an unapproved draft through the running Go HTTP service. These results use demo/mock providers.
