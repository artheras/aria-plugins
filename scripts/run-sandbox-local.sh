#!/bin/sh
# Run Commerce on this machine against the PayPal Sandbox, with the Sandbox
# credentials read from Google Secret Manager. Nothing is printed or written to
# disk: the PayPal values go straight into the server's environment. The merchant
# login token printed at start is a fresh random value for this local run only.
#
#   scripts/run-sandbox-local.sh            # local parser (no AI key needed)
#   scripts/run-sandbox-local.sh aria       # draft with your signed-in Aria Code login
#
# Needs: gcloud signed in with access to the secrets, and Go. Local use only.
set -eu

project=${ARIA_GCP_PROJECT:-arthera-prod}
mode=${1:-demo}
case "$mode" in demo|aria) ;; *) echo "usage: $0 [demo|aria]" >&2; exit 2 ;; esac
command -v gcloud >/dev/null 2>&1 || { echo "gcloud is required" >&2; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo "openssl is required" >&2; exit 1; }

secret() {
  gcloud secrets versions access latest --secret="$1" --project="$project" 2>/dev/null \
    || { echo "could not read secret $1 from project $project (is gcloud signed in?)" >&2; exit 1; }
}

PAYPAL_CLIENT_ID=$(secret aria-commerce-paypal-sandbox-client-id)
PAYPAL_CLIENT_SECRET=$(secret aria-commerce-paypal-sandbox-client-secret)
# Fresh per run: this local server shares nothing with the deployed one.
ARIA_MERCHANT_TOKEN=$(openssl rand -hex 24)
ARIA_MERCHANT_LOGIN_TOKEN=$(openssl rand -hex 24)
ARIA_SESSION_SECRET=$(openssl rand -hex 24)

export PAYPAL_CLIENT_ID PAYPAL_CLIENT_SECRET ARIA_MERCHANT_TOKEN ARIA_MERCHANT_LOGIN_TOKEN ARIA_SESSION_SECRET
export ARIA_MODE=sandbox ARIA_AI="$mode" PAYPAL_BUTTONS=1
export ARIA_MERCHANT_ID=${ARIA_MERCHANT_ID:-store-1}
export PORT=${PORT:-4180}
export PUBLIC_URL=http://127.0.0.1:$PORT
export ARIA_DATA_DIR=${ARIA_DATA_DIR:-data/sandbox}

echo "Commerce (PayPal Sandbox, drafter: $mode) on $PUBLIC_URL"
echo "Merchant browser login token for this run (random, local only): $ARIA_MERCHANT_LOGIN_TOKEN"
exec go run ./cmd/server
