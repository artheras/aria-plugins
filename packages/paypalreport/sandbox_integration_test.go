package paypalreport

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"
)

// Runs only when Sandbox credentials are in the environment, so CI without them
// skips it. It reads, never writes, and prints field names, not account data.
func TestSandboxReadOnlyIntegration(t *testing.T) {
	id, secret := os.Getenv("PAYPAL_SANDBOX_CLIENT_ID"), os.Getenv("PAYPAL_SANDBOX_CLIENT_SECRET")
	if id == "" || secret == "" {
		t.Skip("set PAYPAL_SANDBOX_CLIENT_ID and PAYPAL_SANDBOX_CLIENT_SECRET to run against the Sandbox")
	}
	c := NewSandbox(id, secret) // never NewLive: this test must not touch a real account
	ctx := context.Background()
	if _, err := c.accessToken(ctx, true); err != nil {
		// The message says which: "unavailable" is the network (check proxy
		// settings), "rejected" is the credentials.
		t.Fatalf("could not authenticate with the Sandbox: %v", err)
	}
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}

	var rawBalances map[string]any
	if err := c.get(ctx, "/v1/reporting/balances", map[string][]string{"as_of_time": {stamp(time.Now())}}, &rawBalances); err != nil {
		t.Logf("balances: %v", err)
	} else {
		t.Logf("balances top-level keys: %v", keys(rawBalances))
		if list, _ := rawBalances["balances"].([]any); len(list) > 0 {
			if first, ok := list[0].(map[string]any); ok {
				t.Logf("balance entry keys: %v", keys(first))
			}
		}
	}
	balances, err := c.Balances(ctx, time.Now())
	t.Logf("Balances() -> %d entries, err=%v", len(balances), err)

	end := time.Now().Add(-4 * time.Hour) // activity can lag by up to three hours
	var rawTx map[string]any
	q := map[string][]string{"start_date": {stamp(end.Add(-30 * 24 * time.Hour))}, "end_date": {stamp(end)}, "fields": {"all"}, "page_size": {"100"}, "page": {"1"}}
	if err := c.get(ctx, "/v1/reporting/transactions", q, &rawTx); err != nil {
		t.Logf("transactions: %v", err)
	} else {
		t.Logf("transactions top-level keys: %v", keys(rawTx))
		if list, _ := rawTx["transaction_details"].([]any); len(list) > 0 {
			if d, ok := list[0].(map[string]any); ok {
				t.Logf("detail keys: %v", keys(d))
				if info, ok := d["transaction_info"].(map[string]any); ok {
					t.Logf("transaction_info keys: %v", keys(info))
				}
			}
		}
	}
	txs, err := c.Transactions(ctx, end.Add(-30*24*time.Hour), end)
	t.Logf("Transactions() -> %d rows, err=%v", len(txs), err)
	for _, total := range Summarize(txs) {
		t.Logf("totals %s: n=%d gross=%s fees=%s net=%s", total.Currency, total.Count, total.Gross, total.Fees, total.Net)
	}
}
