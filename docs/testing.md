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
- Plugin version agreement, escaping paths and symlink rejection, stale/missing skill mirrors, altered skill content, and Aria-compatible hash ordering.

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
7. Run a full interactive Aria agent session with a configured model, invoke the installed skill, and review the returned draft in the merchant browser.
8. Record the real flow for the competition. Demonstration mode alone does not establish AI + PayPal integration.

Docker and Vercel execution, a real Redis service, full interactive Aria/Claude agent sessions, and real AI/PayPal transactions are not claimed as verified.

## Implementation run

Verified with Go 1.26.9 on macOS ARM64: `go test -race ./...`, `go vet ./...`, and builds of both Go binaries. The embedded browser UI completed the $60.00 simulated order and restored the paid state after refresh. The actual compiled MCP binary also created and read an unapproved draft through the running Go HTTP service. These results use demo/mock providers.

## Aria catalogue compatibility

The 0.2.0 catalogue was checked against unmodified Aria Code modules from commit [51d34eac3180](https://github.com/artheras/aria-code/tree/51d34eac318065a61e434f8cc77625671b6c4bbe). Each reference file's Git blob hash matched that commit. The isolated run used temporary directories and a separate local demo server, without changing the user's Aria home.

- `discover_external_skills` returned `commerce:commerce`, version `0.2.0`, integrity `verified`.
- The existing `install_catalog` validation accepted the local snapshot with a test clone runner. Altering its policy file changed integrity to `mismatch`.
- Aria's actual `MCPToolRegistry` started the compiled Go bridge through the provided configuration, negotiated stdio MCP 2024-11-05, and discovered exactly three tools.
- Through that registry, catalogue read, draft creation, and order read returned an unapproved USD 60.00 order for 2 tees and 1 mug.
- The Go catalogue generator produced a mirror and lock accepted by the Aria loader. `make catalog-check` and all race-detected Go tests passed.

This checks the real loader and transport modules. It does not prove a model selects the skill/tools correctly in a full interactive session, a Claude installation succeeds, or real AI/payment providers work.
