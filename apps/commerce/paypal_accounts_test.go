package commerce

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/paypalaccounts"
	"github.com/artheras/aria-plugins/packages/store"
)

func TestDefaultConnectionPageAndExactReviewLink(t *testing.T) {
	storage, _ := store.NewFile(t.TempDir())
	s := New(Config{Mode: "demo", Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}, SessionSecret: strings.Repeat("s", 32)})
	for _, path := range []string{"/", "/?order=" + strings.Repeat("a", 32), "/?order=bad", "/review.html"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		raw := w.Body.String()
		if path == "/" {
			if w.Code != 200 || !strings.Contains(raw, "Your permissions.") || strings.Contains(raw, "connection-form") || strings.Contains(raw, "id=\"approve\"") {
				t.Fatal("default page exposed legacy workflows")
			}
		}
		if strings.Contains(path, "order=aaa") && (w.Code != 200 || !strings.Contains(raw, "Approve this exact order") || strings.Contains(raw, "connection-form")) {
			t.Fatal("exact review link broken")
		}
		if path == "/?order=bad" && w.Code != 400 {
			t.Fatal(w.Code)
		}
		if path == "/review.html" && w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
}

func TestPayPalDisconnectIsUserBoundHumanOnlyAndCSRFProtected(t *testing.T) {
	p := newTestIdentityProvider(t)
	s, web, _ := accountServer(t, p)
	human := signedInAccount(t, web, p)
	vault, e := paypalaccounts.NewVault(s.config.Store, "test", map[string][]byte{"test": []byte(strings.Repeat("k", 32))})
	if e != nil {
		t.Fatal(e)
	}
	s.config.PayPalVault = vault
	s.config.PayPalAccounts = paypalaccounts.New(vault)
	var session struct {
		Account struct {
			UserID string `json:"user_id"`
		} `json:"account"`
	}
	json.Unmarshal(human.request("GET", "/api/session", nil, 200), &session)
	user := session.Account.UserID
	before, _ := vault.Begin(t.Context(), user)
	g := paypalaccounts.Grant{UserID: user, MerchantID: "verified-user-merchant", Environment: "sandbox", AccessToken: "not-an-actual-provider-token", Capabilities: []string{"invoices.read"}, VerifiedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	if e = vault.Complete(t.Context(), user, before, g); e != nil {
		t.Fatal(e)
	}
	raw := human.request("GET", "/api/account/paypal", nil, 200)
	if !strings.Contains(string(raw), `"connected":true`) || strings.Contains(string(raw), g.AccessToken) {
		t.Fatal("status invalid")
	}
	var agent struct {
		Token string `json:"token"`
	}
	json.Unmarshal(human.request("POST", "/api/account/connections", map[string]any{"label": "test", "scopes": []string{"read"}, "ttl_seconds": 3600}, 201), &agent)
	req, _ := http.NewRequest("POST", web.URL+"/api/account/paypal/disconnect", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+agent.Token)
	response, e := web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("agent disconnected provider", response.StatusCode)
	}
	human.request("POST", "/api/account/paypal/disconnect", map[string]string{"user_id": "another-user"}, 400)
	csrf := human.csrf
	human.csrf = "bad"
	human.request("POST", "/api/account/paypal/disconnect", map[string]any{}, 403)
	human.csrf = csrf
	human.request("POST", "/api/account/paypal/disconnect", map[string]any{}, 200)
	raw = human.request("GET", "/api/account/paypal", nil, 200)
	if !strings.Contains(string(raw), `"connected":false`) {
		t.Fatal("disconnect failed")
	}
	human.request("POST", "/api/account/paypal/disconnect", map[string]any{}, 200)
}
func TestPayPalStatusRequiresUserSessionAndDoesNotInferSharedProvider(t *testing.T) {
	p := newTestIdentityProvider(t)
	_, web, _ := accountServer(t, p)
	b := newBrowser(t, web)
	b.request("GET", "/api/account/paypal", nil, 401)
	b = signedInAccount(t, web, p)
	var status paypalaccounts.Status
	if json.Unmarshal(b.request("GET", "/api/account/paypal", nil, 200), &status) != nil || status.Connected || status.State != "not_connected" {
		t.Fatal(status)
	}
	response, e := web.Client().Get(web.URL + "/connections.js")
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if strings.Contains(string(raw), "/api/catalog") || strings.Contains(string(raw), "/api/orders") {
		t.Fatal("default UI fetched demonstration records")
	}
}
