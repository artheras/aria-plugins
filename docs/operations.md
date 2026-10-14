# Commerce operations

## Identity, service and job

Use independent service/worker identities and a dedicated Firestore database.
The HTTP service and recovery job must use the same schema-compatible image.
Update the job before exposing catalogue management in a new service revision:
older workers do not understand the `meta_catalog` record. Review the stable
identity migration guide before enabling a new merchant ID.

`/api/health` checks the process. `/api/ready` checks shared quota storage; neither
calls AI or PayPal. `/api/config` reports the software version, contract version
and actual simulation/Sandbox mode. Compare that version with the intended image.

## Recovery and manual review

Use the order-history **Needs review** filter. The stored operation includes its
attempt count, next due time and last error code. **Review & resume** renews exact
invoice confirmation for the same create/capture intent. It does not create a new
order or initiate a capture. Unknown refunds require **Check provider status**
and, if needed, inspection in the PayPal merchant dashboard. A recovery job never
initiates a refund. Do not clear an uncertain intent or create a replacement
payment to work around an error.

Listing examines at most 500 records per request and returns a sealed continuation
cursor, including buffered keys for Redis SCAN. Filtered pages can be empty while
still having a cursor. Pagination is not a database snapshot. Recovery scans are
also bounded; move to indexed due-operation queries when full scans become costly.

## Alerts and backups

`deploy/cloud-run/operations.tf` is an optional, reviewable Terraform configuration
for daily seven-day Firestore backups and log alerts for service/recovery failures.
It does not create email recipients, modify the Aria relay, or apply itself.
Import existing resources before applying it to avoid duplicates. Configure existing
notification channel IDs to receive alerts, and test delivery explicitly.

```sh
terraform -chdir=deploy/cloud-run init
terraform -chdir=deploy/cloud-run plan \
  -var='project_id=YOUR_PROJECT' -var='database_id=YOUR_COMMERCE_DATABASE'
```

Scheduled backups and restores have cloud charges. Review the resulting plan and
notification channels before applying. See Google's [scheduled backup guide](https://docs.cloud.google.com/firestore/native/docs/backups)
and [log-based alert documentation](https://docs.cloud.google.com/logging/docs/alerting/log-based-alerts).

Restore backups into a separate database, point a private candidate service at
that database, verify ownership, invoices and recovery intents, then record the
restore duration. Never test a restore by overwriting the active database. Avoid
provider calls while inspecting restored records; they may represent completed
operations whose results were written after the backup.

## Retention and budgets

Anonymous Demo orders and quota subjects can accumulate. Define an explicit
retention period and preview records before deletion. This release expires
unstarted orders after 24 hours but retains their records; expiry is not deletion.
Paid/refunded Sandbox records require an appropriate merchant retention policy.
No TTL deletion policy is enabled automatically on payment records.

Set a Google Cloud billing budget for the relevant project and inspect Cloud Run,
Firestore, Scheduler and build usage. Application request quotas do not represent
a monetary budget or a hard billing cap. Reduce public Demo capacity or traffic
if usage exceeds the intended limits.

Managed member login, multiple merchants, inventory reservations, tax, shipping,
subscriptions and dispute processing remain separate service extensions. The
`inventory` and `invoicing` plugins should reuse these contracts and approval
boundaries when implemented; they are not advertised as installed plugins today.
