package commerce

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/accounts"
	"github.com/artheras/aria-plugins/packages/agents"
	workflow "github.com/artheras/aria-plugins/packages/commerce"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
	"github.com/go-jose/go-jose/v4"
)

type testIdentityProvider struct {
	badSignature                                     bool
	server                                           *httptest.Server
	nonce, challenge, subject, issuer, audience, azp string
	expired                                          bool
	exchanges                                        int
	key                                              *rsa.PrivateKey
}

func newTestIdentityProvider(t *testing.T) *testIdentityProvider {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	p := &testIdentityProvider{key: key, subject: "alice", audience: "aria-test"}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			p.exchanges++
			r.ParseForm()
			h := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(h[:]) != p.challenge {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			issuer := p.issuer
			if issuer == "" {
				issuer = p.server.URL
			}
			exp := time.Now().Add(time.Hour)
			if p.expired {
				exp = time.Now().Add(-time.Hour)
			}
			signingKey := key
			if p.badSignature {
				signingKey, _ = rsa.GenerateKey(rand.Reader, 2048)
			}
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
			claims, _ := json.Marshal(map[string]any{"iss": issuer, "sub": p.subject, "aud": p.audience, "azp": p.azp, "exp": exp.Unix(), "iat": time.Now().Unix(), "nonce": p.nonce})
			signed, _ := signer.Sign(claims)
			raw, _ := signed.CompactSerialize()
			json.NewEncoder(w).Encode(map[string]any{"access_token": "not-forwarded-or-persisted", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}
func accountServer(t *testing.T, p *testIdentityProvider) (*Server, *httptest.Server, *accounts.Access) {
	t.Helper()
	storage, _ := store.NewFile(t.TempDir())
	a, err := accounts.NewAccess(storage, p.server.URL, "shop-one", "http://127.0.0.1", []accounts.Binding{{Subject: "alice", MerchantID: "shop-one", Role: "operator"}, {Subject: "bob", MerchantID: "shop-one", Role: "reader"}, {Subject: "carol", MerchantID: "shop-one", Role: "drafter"}})
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(web.Close)
	a.Audience = web.URL
	l, err := accounts.NewLogin(context.Background(), a, "aria-test", "test-secret", web.URL)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{LegacyTools: true, Mode: "demo", PublicURL: web.URL, MerchantID: "shop-one", SessionSecret: strings.Repeat("s", 32), Account: l, Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}, DraftLimit: 100})
	handler = s
	return s, web, a
}
func beginAccountLogin(t *testing.T, b *browser, p *testIdentityProvider) string {
	t.Helper()
	b.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := b.client.Get(b.server.URL + "/api/auth/login?order=00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 302 {
		t.Fatal("login did not redirect", res.StatusCode)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	q := loc.Query()
	p.nonce = q.Get("nonce")
	p.challenge = q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid" || p.nonce == "" || q.Get("state") == "" {
		t.Fatal("missing login protections")
	}
	return "/api/auth/callback?state=" + url.QueryEscape(q.Get("state")) + "&code=test-code"
}
func signedInAccount(t *testing.T, web *httptest.Server, p *testIdentityProvider) *browser {
	t.Helper()
	b := newBrowser(t, web)
	callback := beginAccountLogin(t, b, p)
	b.request("GET", callback, nil, 303)
	var session struct {
		CSRF    string             `json:"csrf"`
		Account accounts.Principal `json:"account"`
	}
	json.Unmarshal(b.request("GET", "/api/session", nil, 200), &session)
	if session.CSRF == "" || session.Account.UserID == "" {
		t.Fatal("no verified account session")
	}
	b.csrf = session.CSRF
	return b
}
func TestAccountLoginScopesRevocationAndNoLegacyFallback(t *testing.T) {
	p := newTestIdentityProvider(t)
	s, web, a := accountServer(t, p)
	anon := newBrowser(t, web)
	anon.request("GET", "/api/catalog", nil, 401)
	anon.request("POST", "/api/login", map[string]string{"token": "legacy"}, 403)
	b := signedInAccount(t, web, p)
	var issued struct {
		Token      string              `json:"token"`
		Connection accounts.Credential `json:"connection"`
	}
	json.Unmarshal(b.request("POST", "/api/account/connections", map[string]any{"label": "Codex", "scopes": []string{"read"}, "ttl_seconds": 3600}, 201), &issued)
	req := func(method, path, body string, status int) {
		t.Helper()
		r, _ := http.NewRequest(method, web.URL+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+issued.Token)
		res, err := web.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("%s %s got %d want %d", method, path, res.StatusCode, status)
		}
	}
	req("GET", "/api/catalog", "", 200)
	req("POST", "/api/orders", `{"items":[{"sku":"TSH-001","quantity":1}]}`, 403)
	req("POST", "/api/account/connections", `{"scopes":["read"]}`, 403)
	b.request("POST", "/api/account/connections/revoke", map[string]string{"id": issued.Connection.ID}, 200)
	req("GET", "/api/catalog", "", 401)
	// A browser session token copied into a bearer header cannot gain agent access.
	u, _ := url.Parse(web.URL)
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == accountCookie {
			issued.Token = c.Value
		}
	}
	req("GET", "/api/catalog", "", 401)
	b.request("POST", "/api/orders", map[string]any{"items": []map[string]any{{"sku": "TSH-001", "quantity": 1}}, "merchant_id": "another-shop"}, 400)
	order, err := s.service.DraftItems(context.Background(), workflow.Actor{Owner: "merchant_another-shop"}, []sdk.Selection{{SKU: "TSH-001", Quantity: 1}})
	if err != nil {
		t.Fatal(err)
	}
	b.request("GET", "/api/orders/"+order.ID, nil, 404)
	alice, _ := a.Subject("alice")
	other, _ := a.Subject("bob")
	_, c, _ := a.IssueGrant(context.Background(), other, "Other user", []string{"read"}, time.Hour)
	if alice.UserID == other.UserID {
		t.Fatal("users not distinguished")
	}
	b.request("POST", "/api/account/connections/revoke", map[string]string{"id": c.ID}, 404)
	b.request("POST", "/api/logout", map[string]any{}, 200)
	b.request("GET", "/api/catalog", nil, 401)
}

func TestOIDCRejectsUnassignedAndInvalidIdentityClaims(t *testing.T) {
	for _, name := range []string{"unassigned", "issuer", "audience", "nonce", "expired", "authorized-party", "signature"} {
		t.Run(name, func(t *testing.T) {
			p := newTestIdentityProvider(t)
			_, web, _ := accountServer(t, p)
			b := newBrowser(t, web)
			callback := beginAccountLogin(t, b, p)
			switch name {
			case "signature":
				p.badSignature = true
			case "unassigned":
				p.subject = "unknown"
			case "issuer":
				p.issuer = "https://forged.example"
			case "audience":
				p.audience = "another-application"
			case "nonce":
				p.nonce = "wrong-browser"
			case "expired":
				p.expired = true
			case "authorized-party":
				p.azp = "another-application"
			}
			b.request("GET", callback, nil, 401)
			var session struct {
				Authenticated bool `json:"authenticated"`
			}
			json.Unmarshal(b.request("GET", "/api/session", nil, 200), &session)
			if session.Authenticated {
				t.Fatal("rejected OIDC identity created a session")
			}
		})
	}
}
func TestOIDCStateIsBrowserBoundAndOneUseAcrossReplicas(t *testing.T) {
	p := newTestIdentityProvider(t)
	s, web, _ := accountServer(t, p)
	b := newBrowser(t, web)
	callback := beginAccountLogin(t, b, p)
	endpoint, _ := url.Parse(web.URL + "/api/auth/callback")
	saved := b.client.Jar.Cookies(endpoint)
	other := newBrowser(t, web)
	other.request("GET", callback, nil, 401)
	if p.exchanges != 0 {
		t.Fatal("exchanged a code without matching browser state")
	}
	bad := strings.Replace(callback, "state=", "state=tampered", 1)
	b.request("GET", bad, nil, 401)
	if p.exchanges != 0 {
		t.Fatal("exchanged a code with wrong state")
	}
	b.client.Jar.SetCookies(endpoint, saved)
	b.request("GET", callback, nil, 303)
	if p.exchanges != 1 {
		t.Fatal("missing code exchange")
	}
	// A fresh server instance sharing storage cannot replay the same valid cookie/state.
	replica := New(s.config)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", web.URL+callback, nil)
	for _, c := range saved {
		r.AddCookie(c)
	}
	replica.ServeHTTP(rr, r)
	if rr.Code != 401 || p.exchanges != 1 {
		t.Fatal("login state replay crossed replicas", rr.Code, p.exchanges)
	}
}
func TestAccountRolesAndAgentGrantStayBelowHumanPaymentAuthority(t *testing.T) {
	p := newTestIdentityProvider(t)
	_, web, _ := accountServer(t, p)
	p.subject = "bob"
	reader := signedInAccount(t, web, p)
	reader.request("GET", "/api/catalog", nil, 200)
	reader.request("POST", "/api/orders", map[string]any{"items": []map[string]any{{"sku": "TSH-001", "quantity": 1}}}, 403)
	reader.request("POST", "/api/account/connections", map[string]any{"scopes": []string{"read", "draft"}}, 403)
	reader.request("POST", "/api/catalog/update", map[string]any{}, 403)
	p.subject = "carol"
	drafter := signedInAccount(t, web, p)
	var order sdk.Order
	json.Unmarshal(drafter.request("POST", "/api/orders", map[string]any{"items": []map[string]any{{"sku": "TSH-001", "quantity": 1}}}, 201), &order)
	drafter.request("POST", "/api/orders/"+order.ID+"/approve", map[string]string{"digest": order.Digest}, 403)
	p.subject = "alice"
	operator := signedInAccount(t, web, p)
	var grant struct {
		Token string `json:"token"`
	}
	json.Unmarshal(operator.request("POST", "/api/account/connections", map[string]any{"scopes": []string{"read", "draft"}}, 201), &grant)
	req, _ := http.NewRequest("POST", web.URL+"/api/orders/"+order.ID+"/approve", strings.NewReader(`{"digest":"`+order.Digest+`"}`))
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	res, err := web.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("operator's agent approved a payment", res.StatusCode)
	}
	csrf := operator.csrf
	operator.csrf = "wrong"
	operator.request("POST", "/api/logout", map[string]any{}, 403)
	operator.request("POST", "/api/account/connections", map[string]any{"scopes": []string{"read"}}, 403)
	operator.csrf = csrf
	operator.request("POST", "/api/orders/"+order.ID+"/approve", map[string]string{"digest": order.Digest}, 200)
}
