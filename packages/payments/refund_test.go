package payments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/artheras/aria-plugins/packages/sdk"
)

func TestPayPalFullRefundIdentityAndRequestKey(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 3600})
			return
		}
		if r.Method == "POST" {
			if r.URL.Path != "/v2/payments/captures/CAP1/refund" || r.Header.Get("PayPal-Request-Id") != RequestID("order1", "refund") {
				t.Error("wrong refund identity or idempotency key")
			}
			var body struct {
				Amount money `json:"amount"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Amount.Value != "60.00" || body.Amount.Currency != "USD" {
				t.Error("refund amount changed")
			}
		} else if r.URL.Path != "/v2/payments/refunds/REF1" {
			t.Error("wrong refund inspection route")
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "REF1", "status": "COMPLETED", "amount": map[string]string{"currency_code": "USD", "value": "60.00"}, "links": []any{map[string]string{"rel": "up", "href": base + "/v2/payments/captures/CAP1"}}})
	}))
	defer server.Close()
	base = server.URL
	p := NewPayPal("client", "secret", "")
	p.base = base
	p.client = server.Client()
	i := sdk.Invoice{ID: "order1", Amount: 6000, Currency: "USD"}
	r, err := p.Refund(context.Background(), i, "CAP1", RequestID(i.ID, "refund"))
	if err != nil || ValidateRefund(i, "CAP1", r) != nil {
		t.Fatal(r, err)
	}
	r, err = p.InspectRefund(context.Background(), "REF1")
	if err != nil || r.ID != "REF1" {
		t.Fatal(r, err)
	}
}
