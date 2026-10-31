package commerce

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/products"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

func managementServer(t *testing.T) (*httptest.Server, *Config, *http.Handler) {
	storage, _ := store.NewFile(t.TempDir())
	c := &Config{Mode: "demo", MerchantID: "store-1", MerchantToken: strings.Repeat("m", 32), MerchantLoginToken: strings.Repeat("h", 32), SessionSecret: strings.Repeat("s", 32), Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}}
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	c.PublicURL = server.URL
	handler = New(*c)
	t.Cleanup(server.Close)
	return server, c, &handler
}
func merchantLogin(b *browser, token string) {
	var session struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(b.request("POST", "/api/login", map[string]string{"token": token}, 200), &session)
	b.csrf = session.CSRF
}
func TestMerchantOrdersSurviveCredentialRotation(t *testing.T) {
	server, c, handler := managementServer(t)
	b := newBrowser(t, server)
	merchantLogin(b, c.MerchantLoginToken)
	var o sdk.Order
	json.Unmarshal(b.request("POST", "/api/orders", map[string]string{"prompt": "2 tees and 1 mug"}, 201), &o)
	oldToken := c.MerchantToken
	c.MerchantToken = strings.Repeat("n", 32)
	c.MerchantLoginToken = strings.Repeat("i", 32)
	c.SessionSecret = strings.Repeat("t", 32)
	*handler = New(*c)
	newSession := newBrowser(t, server)
	merchantLogin(newSession, c.MerchantLoginToken)
	var fetched sdk.Order
	json.Unmarshal(newSession.request("GET", "/api/orders/"+o.ID, nil, 200), &fetched)
	if fetched.Amount != 6000 {
		t.Fatal("rotation lost order")
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/orders/"+o.ID, nil)
	req.Header.Set("Authorization", "Bearer "+oldToken)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("revoked token accepted")
	}
}
func TestAgentTokenCannotOpenAHumanMerchantSession(t *testing.T) {
	server, c, handler := managementServer(t)
	agent := newBrowser(t, server)
	// The MCP bridge holds MerchantToken; it must never become a human login,
	// even when the request carries the right Origin.
	agent.request("POST", "/api/login", map[string]string{"token": c.MerchantToken}, 401)
	req, _ := http.NewRequest("POST", server.URL+"/api/login", strings.NewReader(`{"token":"`+c.MerchantLoginToken+`"}`))
	req.Header.Set("Origin", server.URL)
	req.Header.Set("Authorization", "Bearer "+c.MerchantToken)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("bearer request opened a human session", res.StatusCode)
	}

	human := newBrowser(t, server)
	merchantLogin(human, c.MerchantLoginToken)
	var session struct {
		Merchant bool `json:"merchant"`
	}
	json.Unmarshal(human.request("GET", "/api/session", nil, 200), &session)
	if !session.Merchant {
		t.Fatal("merchant login token did not open the merchant session")
	}

	// A Config built in code with equal secrets keeps login closed.
	c.MerchantLoginToken = c.MerchantToken
	*handler = New(*c)
	newBrowser(t, server).request("POST", "/api/login", map[string]string{"token": c.MerchantToken}, 503)
	var cfg struct {
		Login bool `json:"merchant_login_available"`
	}
	json.Unmarshal(newBrowser(t, server).request("GET", "/api/config", nil, 200), &cfg)
	if cfg.Login {
		t.Fatal("login advertised with the agent token as the login secret")
	}
}
func TestListCursorIsBoundToOwnerAndFilters(t *testing.T) {
	server, _, _ := managementServer(t)
	b := newBrowser(t, server)
	for range 3 {
		b.request("POST", "/api/orders", map[string]string{"prompt": "1 mug"}, 201)
	}
	var page struct {
		Orders []sdk.Order `json:"orders"`
		Next   string      `json:"next_cursor"`
	}
	json.Unmarshal(b.request("GET", "/api/orders?limit=1", nil, 200), &page)
	if len(page.Orders) != 1 || page.Next == "" || page.Orders[0].Owner != "" {
		t.Fatal("invalid order page")
	}
	other := newBrowser(t, server)
	other.request("GET", "/api/orders?limit=1&cursor="+page.Next, nil, 400)
	b.request("GET", "/api/orders?limit=1&state=paid&cursor="+page.Next, nil, 400)
	b.request("GET", "/api/orders?limit=1&cursor="+page.Next, nil, 200)
}
func TestCatalogueUpdateRequiresMerchantAndPreservesApprovedSnapshot(t *testing.T) {
	server, c, _ := managementServer(t)
	anonymous := newBrowser(t, server)
	var snap products.Snapshot
	json.Unmarshal(anonymous.request("GET", "/api/catalog/snapshot", nil, 200), &snap)
	anonymous.request("POST", "/api/catalog/update", snap, 403)
	b := newBrowser(t, server)
	merchantLogin(b, c.MerchantLoginToken)
	var o sdk.Order
	json.Unmarshal(b.request("POST", "/api/orders", map[string]string{"prompt": "2 tees and 1 mug"}, 201), &o)
	b.request("POST", "/api/orders/"+o.ID+"/approve", map[string]string{"digest": o.Digest}, 200)
	for i := range snap.Products {
		if snap.Products[i].SKU == "TSH-001" {
			snap.Products[i].UnitAmount = 5000
		}
	}
	b.request("POST", "/api/catalog/update", snap, 200)
	var old sdk.Order
	json.Unmarshal(b.request("GET", "/api/orders/"+o.ID, nil, 200), &old)
	if old.Amount != 6000 || old.Digest != o.Digest {
		t.Fatal("catalogue update changed an approved invoice")
	}
	var next sdk.Order
	json.Unmarshal(b.request("POST", "/api/orders", map[string]string{"prompt": "2 tees and 1 mug"}, 201), &next)
	if next.Amount != 11200 {
		t.Fatal("new draft ignored new prices", next.Amount)
	}
}
