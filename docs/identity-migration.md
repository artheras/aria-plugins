# Stable merchant identity and credential rotation

Set a durable `ARIA_MERCHANT_ID`, such as `store-1`. New orders belong to
`merchant_store-1`, independently of the merchant token and session signing key.
Rotating either credential invalidates old authentication while retaining order
ownership. A new Sandbox service requires this ID. Existing Demo services may
retain their original identity temporarily when the ID is absent.

## Migrate an existing merchant

1. Preserve a database backup and pause merchant writes. Anonymous Demo sessions
   have their own ownership and must not be migrated into the merchant account.
2. Using the original exported credentials locally, obtain the old owner hash:

```sh
./bin/aria-commerce-admin legacy-owner
```

The command prints only the derived ownership hash. It does not print credentials.
Record that hash before rotating the original secrets.

3. Preview the exact migration against the dedicated database:

```sh
./bin/aria-commerce-admin migrate-owner --store firestore \
  --project YOUR_PROJECT --database YOUR_COMMERCE_DATABASE \
  --collection aria_commerce_records \
  --from-owner OLD_OWNER_HASH --merchant-id store-1
```

4. Repeat with `--apply` after inspecting the preview. Each order uses
   compare-and-swap, and the invoice, approval and provider identity remain intact.
   An interrupted migration can be rerun; a scan-limit exit includes a cursor.
5. Configure `ARIA_MERCHANT_ID=store-1`. During transition, optionally set
   `ARIA_LEGACY_MERCHANT_OWNER=OLD_OWNER_HASH` to allow that specific historical
   owner. Reads and mutations still require the current authenticated merchant.
   Mutations migrate matching records to the stable owner automatically.
6. Remove the transition alias after a full migration and verify browser and MCP
   reads before resuming normal traffic. Rotate credentials separately.

For file storage, stop the single server process and use `--store file
--data-dir YOUR_DATA_DIRECTORY`. The administrator has no AI or PayPal dependency
and cannot charge or refund money. Firestore uses ADC and the configured database.

This release supports one merchant. Managed member accounts, role-based login
and multiple merchants require a separate authentication design; a transition
alias is not a substitute for multi-tenant identity.
