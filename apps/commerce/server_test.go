package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/approvals"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

type browser struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	csrf   string
}

func (b *browser) request(method, path string, body any, status int) []byte {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, b.server.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	if method == "POST" {
		req.Header.Set("Origin", b.server.URL)
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		b.t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, status, raw)
	}
	return raw
}
func newBrowser(t *testing.T, server *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	b := &browser{t: t, server: server, client: &http.Client{Jar: jar}}
	var session struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(b.request("GET", "/api/session", nil, 200), &session)
	b.csrf = session.CSRF
	return b
}
func TestDemoWorkflowHumanBoundaryAndPersistence(t *testing.T) {
	dir := t.TempDir()
	storage, _ := store.NewFile(dir)
	c := Config{Mode: "demo", SessionSecret: strings.Repeat("s", 32), MerchantToken: strings.Repeat("m", 32), MerchantLoginToken: strings.Repeat("h", 32), Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}}
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	c.PublicURL = server.URL
	handler = New(c)
	b := newBrowser(t, server)
	raw := b.request("POST", "/api/orders", map[string]any{"prompt": "2 tees and 1 mug", "amount": 1}, 400)
	raw = b.request("POST", "/api/orders", map[string]string{"prompt": "2 tees and 1 mug"}, 201)
	var order sdk.Order
	json.Unmarshal(raw, &order)
	if order.Amount != 6000 || order.Owner != "" {
		t.Fatal("incorrect invoice or leaked ownership", order)
	}
	path := "/api/orders/" + order.ID
	b.request("POST", path+"/checkout", map[string]any{}, 409)
	b.request("POST", path+"/approve", map[string]string{"digest": "tampered"}, 409)
	other := newBrowser(t, server)
	other.request("GET", path, nil, 404)
	old := b.csrf
	b.csrf = "wrong"
	b.request("POST", path+"/approve", map[string]string{"digest": order.Digest}, 403)
	b.csrf = old
	req, _ := http.NewRequest("POST", server.URL+path+"/approve", strings.NewReader(`{"digest":"`+order.Digest+`"}`))
	req.Header.Set("Origin", server.URL)
	req.Header.Set("Authorization", "Bearer "+c.MerchantToken)
	res, _ := server.Client().Do(req)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("agent approved without human", res.StatusCode)
	}
	b.request("POST", path+"/approve", map[string]string{"digest": order.Digest}, 200)
	b.request("POST", path+"/approve", map[string]string{"digest": order.Digest}, 200)
	b.request("POST", path+"/checkout", map[string]any{}, 200)
	b.request("POST", path+"/checkout", map[string]any{}, 200)
	b.request("POST", path+"/capture", map[string]any{}, 200)
	raw = b.request("POST", path+"/capture", map[string]any{}, 200)
	json.Unmarshal(raw, &order)
	if order.State != "paid" || order.Payment.Amount != 6000 || len(order.Events) != 4 {
		t.Fatal("replay duplicated audit or lost payment", order)
	}
	reopened, _ := store.NewFile(dir)
	c.Store = reopened
	handler = New(c)
	raw = b.request("GET", path, nil, 200)
	json.Unmarshal(raw, &order)
	if order.State != "paid" {
		t.Fatal("paid order lost on restart")
	}
	var receipt struct {
		Simulation bool `json:"simulation"`
	}
	json.Unmarshal(b.request("GET", path+"/receipt", nil, 200), &receipt)
	if !receipt.Simulation {
		t.Fatal("demo receipt not labelled")
	}
	b.request("POST", "/api/paypal/webhook", map[string]string{"event_type": "PAYMENT.CAPTURE.COMPLETED"}, 400)
	// The app must never render untrusted model content as HTML.
	page := b.request("GET", "/", nil, 200)
	if !bytes.Contains(page, []byte("Aria Plugins")) {
		t.Fatal("embedded site unavailable")
	}
}

type hookProvider struct {
	payments.Demo
	snapshot payments.Snapshot
}

func (p hookProvider) Verify(context.Context, http.Header, []byte) (payments.Webhook, error) {
	return payments.Webhook{ID: "evt1", OrderID: p.snapshot.Payment.OrderID}, nil
}
func (p hookProvider) Inspect(context.Context, string) (payments.Snapshot, error) {
	return p.snapshot, nil
}
func TestVerifiedWebhookReconcilesOnce(t *testing.T) {
	storage, _ := store.NewFile(t.TempDir())
	c := Config{Mode: "demo", PublicURL: "https://merchant.test", SessionSecret: strings.Repeat("s", 32), Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}}
	s := New(c)
	invoice := sdk.Invoice{ID: "local", Amount: 1200, Currency: "USD"}
	order := sdk.Order{Invoice: invoice, Owner: "owner", State: "awaiting_payment", ApprovedDigest: "will-set", Payment: &sdk.Payment{OrderID: "demo_local"}}
	// Use actual approval digest; webhook completion cannot replace approval.
	order.Digest = approvals.Digest(invoice)
	order.ApprovedDigest = order.Digest
	s.save(context.Background(), order, nil)
	p := hookProvider{snapshot: payments.Snapshot{InvoiceID: "local", Payment: sdk.Payment{Provider: "demo", OrderID: "demo_local", CaptureID: "c1", Status: "COMPLETED", Amount: 1200, Currency: "USD"}}}
	c.Payments = p
	s = New(c)
	for range 2 {
		r := httptest.NewRequest("POST", "/api/paypal/webhook", strings.NewReader("{}"))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	stored, _, _ := s.load(context.Background(), "local", "owner")
	if stored.State != "paid" || len(stored.Events) != 1 || len(stored.WebhookIDs) != 1 {
		t.Fatal("webhook replay duplicated audit", stored)
	}
}
