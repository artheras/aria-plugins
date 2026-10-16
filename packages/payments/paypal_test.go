package payments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/sdk"
)

func TestPayPalCreateCaptureReconcileAndReplay(t *testing.T) {
	invoice := sdk.Invoice{ID: "local-order", Currency: "USD", Amount: 6000}
	status, currency, value := "APPROVED", "USD", "60.00"
	captures := 0
	createKeys := []string{}
	verification := "SUCCESS"
	order := func() map[string]any {
		payments := map[string]any{}
		if status == "COMPLETED" {
			payments["captures"] = []any{map[string]any{"id": "CAPTURE1", "status": "COMPLETED", "amount": map[string]string{"currency_code": currency, "value": value}}}
		}
		return map[string]any{"id": "PAYPAL1", "status": status, "purchase_units": []any{map[string]any{"custom_id": invoice.ID, "amount": map[string]string{"currency_code": currency, "value": value}, "payments": payments}}, "links": []any{map[string]string{"rel": "payer-action", "href": "https://www.sandbox.paypal.com/checkoutnow?token=PAYPAL1"}}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth2/token":
			id, secret, ok := r.BasicAuth()
			if !ok || id != "client" || secret != "secret" {
				t.Error("incorrect OAuth credentials")
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "expires_in": 3600})
			return
		case "/v1/notifications/verify-webhook-signature":
			if r.Header.Get("Authorization") != "Bearer access" {
				t.Error("missing OAuth bearer")
			}
			json.NewEncoder(w).Encode(map[string]string{"verification_status": verification})
			return
		case "/v2/checkout/orders":
			createKeys = append(createKeys, r.Header.Get("PayPal-Request-Id"))
			var body struct {
				Intent string `json:"intent"`
				Units  []struct {
					CustomID string `json:"custom_id"`
					Amount   money  `json:"amount"`
				} `json:"purchase_units"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Intent != "CAPTURE" || len(body.Units) != 1 || body.Units[0].CustomID != invoice.ID || body.Units[0].Amount.Value != "60.00" {
				t.Error("create changed invoice")
			}
		case "/v2/checkout/orders/PAYPAL1/capture":
			if r.Header.Get("PayPal-Request-Id") != RequestID(invoice.ID, "capture") {
				t.Error("missing stable capture request ID")
			}
			captures++
			status = "COMPLETED"
		case "/v2/checkout/orders/PAYPAL1":
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(order())
	}))
	defer server.Close()
	p := NewPayPal("client", "secret", "WH1")
	p.base = server.URL
	p.client = &http.Client{Timeout: time.Second}
	for range 2 {
		if _, err := p.Create(context.Background(), invoice, "https://app.test/", "https://app.test/"); err != nil {
			t.Fatal(err)
		}
	}
	if createKeys[0] == "" || createKeys[0] != createKeys[1] {
		t.Fatal("retry created new payment request key")
	}
	first, err := p.Capture(context.Background(), invoice, "PAYPAL1")
	if err != nil || first.CaptureID != "CAPTURE1" {
		t.Fatal(first, err)
	}
	if _, err = p.Capture(context.Background(), invoice, "PAYPAL1"); err != nil || captures != 1 {
		t.Fatal("capture replay charged again", err)
	}
	value = "59.99"
	if _, err = p.Capture(context.Background(), invoice, "PAYPAL1"); err == nil {
		t.Fatal("mismatched amount reconciled")
	}
	value = "60.00"
	currency = "EUR"
	if _, err = p.Capture(context.Background(), invoice, "PAYPAL1"); err == nil {
		t.Fatal("mismatched currency reconciled")
	}
	currency = "USD"
	headers := http.Header{}
	for _, key := range []string{"Paypal-Auth-Algo", "Paypal-Cert-Url", "Paypal-Transmission-Id", "Paypal-Transmission-Sig", "Paypal-Transmission-Time"} {
		headers.Set(key, "test")
	}
	raw := []byte(`{"id":"EVENT1","event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"supplementary_data":{"related_ids":{"order_id":"PAYPAL1"}}}}`)
	if hook, err := p.Verify(context.Background(), headers, raw); err != nil || hook.OrderID != "PAYPAL1" {
		t.Fatal(hook, err)
	}
	verification = "FAILURE"
	if _, err = p.Verify(context.Background(), headers, raw); err == nil {
		t.Fatal("forged webhook signature accepted")
	}
	if _, err = p.Verify(context.Background(), http.Header{}, raw); err == nil {
		t.Fatal("missing signature accepted")
	}
}
func TestPendingCaptureCannotBePaid(t *testing.T) {
	var order paypalOrder
	json.Unmarshal([]byte(`{"id":"P1","status":"COMPLETED","purchase_units":[{"custom_id":"local","amount":{"currency_code":"USD","value":"10.00"},"payments":{"captures":[{"id":"C1","status":"PENDING","amount":{"currency_code":"USD","value":"10.00"}}]}}]}`), &order)
	if _, err := snapshot(order); err == nil {
		t.Fatal("pending capture was accepted as paid")
	}
	for _, v := range []string{"1.001", "1e2", "-1.00", "NaN", "01.00"} {
		if _, err := parseCents(v); err == nil {
			t.Fatal("invalid money accepted", v)
		}
	}
}

// With PayPal's JavaScript buttons the page approves the order, so the order is
// created without a redirect experience and without a checkout link. The server
// still pins the invoice, amount and currency.
func TestPayPalEmbeddedCreateHasNoRedirectAndKeepsInvoiceBinding(t *testing.T) {
	invoice := sdk.Invoice{ID: "local-order", Currency: "USD", Amount: 6000}
	var sawSource bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth2/token":
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "expires_in": 3600})
		case "/v2/checkout/orders":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			_, sawSource = body["payment_source"]
			json.NewEncoder(w).Encode(map[string]any{"id": "PAYPAL1", "status": "CREATED", "purchase_units": []any{map[string]any{"custom_id": invoice.ID, "amount": map[string]string{"currency_code": "USD", "value": "60.00"}}}})
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
	}))
	defer server.Close()
	p := NewPayPal("client", "secret", "WH1")
	p.base, p.client, p.Embedded = server.URL, &http.Client{Timeout: time.Second}, true
	payment, err := p.Create(context.Background(), invoice, "https://app.test/", "https://app.test/")
	if err != nil {
		t.Fatal(err)
	}
	if sawSource {
		t.Fatal("embedded order still asked PayPal for a redirect flow")
	}
	if payment.OrderID != "PAYPAL1" || payment.CheckoutURL != "" || payment.Amount != 6000 {
		t.Fatal("unexpected embedded payment", payment)
	}
}
