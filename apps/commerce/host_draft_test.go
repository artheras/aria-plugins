package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

type unavailableDrafter struct{}

func (unavailableDrafter) Name() string { return "Unavailable server model" }
func (unavailableDrafter) Draft(context.Context, string, []sdk.Product) (agents.Draft, error) {
	return agents.Draft{}, errors.New("server model is unavailable")
}

func TestHostDraftWorksWithoutServerAIAndRetainsHumanBoundary(t *testing.T) {
	storage, _ := store.NewFile(t.TempDir())
	token := strings.Repeat("a", 32)
	config := Config{Mode: "demo", SessionSecret: strings.Repeat("s", 32), MerchantToken: token, MerchantLoginToken: strings.Repeat("h", 32), Store: storage, Drafter: unavailableDrafter{}, Payments: payments.Demo{}, DraftLimit: 100}
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	config.PublicURL = server.URL
	handler = New(config)
	request := func(path, body string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s: status=%d body=%s", path, res.StatusCode, data)
		}
		return data
	}
	raw := request("/api/orders", `{"items":[{"sku":"TSH-001","quantity":2},{"sku":"MUG-003","quantity":1}]}`, 201)
	var order sdk.Order
	if json.Unmarshal(raw, &order) != nil || order.Amount != 6000 || order.State != "draft" || order.Payment != nil || order.ApprovedDigest != "" {
		t.Fatalf("invalid host draft: %s", raw)
	}
	if !bytes.Contains(raw, []byte("Host-proposed selections")) {
		t.Fatal("draft provenance is missing")
	}
	request("/api/orders/"+order.ID+"/approve", `{"digest":"`+order.Digest+`"}`, 403)
	request("/api/orders/"+order.ID+"/checkout", `{}`, 403)
	for _, invalid := range []struct {
		body   string
		status int
	}{
		{`{"items":[{"sku":"TSH-001","quantity":2,"unit_amount":1}]}`, 400},
		{`{"items":[{"sku":"UNKNOWN","quantity":1}]}`, 422},
		{`{"items":[{"sku":"TSH-001","quantity":0}]}`, 422},
		{`{"items":[{"sku":"TSH-001","quantity":1.5}]}`, 400},
		{`{"items":[]}`, 422},
		{`{"items":[{"sku":"TSH-001","quantity":1}],"prompt":"two tees"}`, 400},
		{`{"items":null}`, 400},
	} {
		request("/api/orders", invalid.body, invalid.status)
	}
	// The legacy prompt path still calls the configured model and fails closed.
	request("/api/orders", `{"prompt":"two tees"}`, 422)
}
