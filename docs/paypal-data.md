# Reading a PayPal account's data (read-only)

`packages/paypalreport` reads transactions and balances. It cannot create an
order, capture or refund: the client only issues `GET` requests to two reporting
endpoints. That makes it the part of this repository that can be pointed at a
business's own PayPal account without moving money.

```go
c := paypalreport.NewSandbox(clientID, secret)   // NewLive(...) is a separate, deliberate call
txs, err := c.Transactions(ctx, start, end)      // any range; split into <=31-day windows
totals := paypalreport.Summarize(txs)            // exact per-currency gross / fees / net
balances, err := c.Balances(ctx, time.Now())
```

## Behaviour that comes from PayPal's API

- Transaction Search covers at most **31 days per request**, so longer ranges are
  split into consecutive windows and de-duplicated by transaction ID.
- Timestamps are UTC with **no fractional seconds**.
- New activity can take **up to three hours** to appear. A report that must match
  the books should read a range that ended earlier than that.
- The REST app needs the **Transaction search** feature enabled in the Developer
  Dashboard, or the reporting endpoints reject it.
- Sums use exact decimal arithmetic (`0.10 + 0.20 = 0.30`); only successful
  (`S`) transactions are summed, so pending or reversed items cannot hide a gap.

## Status

Written from PayPal's documentation and their public example
[`paypaldev/hackathon-paypal-ag-grid-boilerplate`](https://github.com/paypaldev/hackathon-paypal-ag-grid-boilerplate),
which reads the same endpoints. Covered by tests against a fake server (windowing,
paging, token refresh, rate limits, exact sums). **Not yet run against a live
Sandbox account**; the response field names should be confirmed there.

## Multiple businesses

This package holds one account's credentials per `Client`. A service for many
businesses needs per-business credential storage and data isolation, and for real
accounts PayPal's own onboarding for platforms. That design is not in this
repository yet.

## Useful PayPal resources

- [`paypaldev/getting-started-with-paypal-sandbox`](https://github.com/paypaldev/getting-started-with-paypal-sandbox): creating Sandbox accounts and credentials.
- [`paypaldev/hackathon-paypal-ag-grid-boilerplate`](https://github.com/paypaldev/hackathon-paypal-ag-grid-boilerplate): reading products, plans, invoices, transactions and balances.
- [`paypaldev/server-sdk-context-plugin-preview`](https://github.com/paypaldev/server-sdk-context-plugin-preview): skills for PayPal's Server SDK (C#, Java, PHP, Python, Ruby, TypeScript; no Go). Experimental and hackathon-only.
