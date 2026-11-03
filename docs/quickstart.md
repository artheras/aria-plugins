# Connect Aria Commerce

Install the [remote plugin](../plugins/commerce-remote/README.md) from your AI
client, sign in, review permissions, then ask the model to read the catalogue and
prepare an order. Users do not need to open a local demo storefront.

The plugin returns an exact-order review link. Open it only to inspect the
invoice and approve the requested action. Manage app connections in the account
console. PayPal funds remain Sandbox test funds.

For backend development, run `make build` and `make test`. Configure a deployed
HTTPS origin before connecting hosted AI clients. See [server setup](setup.md).
