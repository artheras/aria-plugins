package commerce

import (
	"context"
	"encoding/json"
	"github.com/artheras/aria-plugins/packages/accounts"
	"github.com/artheras/aria-plugins/packages/mcpauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRemoteMCPFullOAuthAndHumanBoundary(t *testing.T) {
	provider := newTestIdentityProvider(t)
	s, web, a := accountServer(t, provider)
	human := signedInAccount(t, web, provider)
	ctx := context.Background()
	metadata := human.request("GET", "/.well-known/oauth-protected-resource/mcp", nil, 200)
	if !strings.Contains(string(metadata), web.URL+"/mcp") {
		t.Fatal("resource discovery")
	}
	res, e := web.Client().Get(web.URL + "/mcp")
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatal("missing OAuth challenge")
	}
	// DCR must work without a browser Origin header.
	res, e = web.Client().Post(web.URL+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Test Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"token_endpoint_auth_method":"none"}`))
	if e != nil {
		t.Fatal(e)
	}
	var client mcpauth.Client
	json.NewDecoder(res.Body).Decode(&client)
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatal("registration", res.StatusCode)
	}
	proof := oauth2.GenerateVerifier()
	q := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {client.Redirects[0]}, "state": {"test-state"}, "resource": {web.URL + "/mcp"}, "scope": {"read draft offline_access"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(proof)}, "code_challenge_method": {"S256"}}
	res, e = human.client.Get(web.URL + "/oauth/authorize?" + q.Encode())
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal("authorize", res.StatusCode)
	}
	consent := res.Header.Get("Location")
	res, e = human.client.Get(web.URL + consent)
	if e != nil {
		t.Fatal(e)
	}
	policy := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "form-action 'self' https://claude.ai") || strings.Contains(policy, "http://*") {
		t.Fatal("callback not narrowly permitted by browser CSP", policy)
	}
	html, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(html), "Allow connection") {
		t.Fatal("consent missing")
	}
	// Browser-only CSRF approval, bound to its transaction cookie.
	v, _ := url.Parse(consent)
	form := url.Values{"request": {v.Query().Get("request")}, "decision": {"allow"}, "csrf": {human.csrf}}
	req, _ := http.NewRequest("POST", web.URL+"/oauth/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", web.URL)
	res, e = human.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal("consent failed", res.StatusCode)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	if loc.Query().Get("iss") != web.URL || loc.Query().Get("state") != "test-state" {
		t.Fatal("issuer/state")
	}
	vals := url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {client.ID}, "redirect_uri": {client.Redirects[0]}, "resource": {web.URL + "/mcp"}, "code_verifier": {proof}}
	res, e = web.Client().PostForm(web.URL+"/oauth/token", vals)
	if e != nil {
		t.Fatal(e)
	}
	var tokens mcpauth.Tokens
	json.NewDecoder(res.Body).Decode(&tokens)
	res.Body.Close()
	if res.StatusCode != 200 || tokens.Refresh == "" {
		t.Fatal("token exchange", res.StatusCode)
	}
	transport := &mcp.StreamableClientTransport{Endpoint: web.URL + "/mcp", HTTPClient: oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tokens.Access}))}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, e := c.Connect(ctx, transport, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, nil)
	if e != nil || len(tools.Tools) != 6 {
		t.Fatal("tool discovery", e)
	}
	for _, tool := range tools.Tools {
		if strings.Contains(tool.Name, "approve") || strings.Contains(tool.Name, "capture") {
			t.Fatal("payment exposed")
		}
	}
	profile, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "commerce_profile", Arguments: map[string]any{}})
	if e != nil || profile.IsError {
		t.Fatal("profile", e)
	}
	providerStatus, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "paypal_connection_status", Arguments: map[string]any{}})
	if e != nil || providerStatus.IsError {
		t.Fatal("provider status unavailable", e)
	}
	statusJSON, _ := json.Marshal(providerStatus.StructuredContent)
	if !strings.Contains(string(statusJSON), `"connected":false`) {
		t.Fatal("Aria login inferred PayPal consent")
	}
	blocked, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "paypal_list_invoices", Arguments: map[string]any{}})
	if e != nil || !blocked.IsError {
		t.Fatal("old read grant gained PayPal access", e)
	}
	draft, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "commerce_draft_order", Arguments: map[string]any{"items": []map[string]any{{"sku": "TSH-001", "quantity": 1}}}})
	if e != nil || draft.IsError {
		t.Fatal("draft", draft, e)
	}
	raw, _ := json.Marshal(draft.StructuredContent)
	if !strings.Contains(string(raw), `"state":"draft"`) {
		t.Fatal("not unapproved", string(raw))
	}
	var created struct {
		ID        string `json:"id"`
		ReviewURL string `json:"review_url"`
	}
	if json.Unmarshal(raw, &created) != nil || created.ReviewURL != web.URL+"/?order="+created.ID || !reviewOrderID.MatchString(created.ID) {
		t.Fatal("missing trusted review link")
	}
	restored, readErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "commerce_get_order", Arguments: map[string]any{"order_id": created.ID}})
	if readErr != nil || restored.IsError {
		t.Fatal("read order with review link failed", readErr)
	}
	restoredRaw, _ := json.Marshal(restored.StructuredContent)
	if !strings.Contains(string(restoredRaw), created.ReviewURL) {
		t.Fatal("read result lost review link")
	}
	// MCP access, old manual grants, cookies and refresh credentials cannot cross surfaces.
	req, _ = http.NewRequest("GET", web.URL+"/api/catalog", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.Access)
	res, e = web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("MCP token reached REST")
	}
	alice, _ := a.Subject("alice")
	manual, _, _ := a.IssueGrant(ctx, alice, "legacy", []string{"read"}, time.Hour)
	for _, bad := range []string{manual, tokens.Refresh} {
		req, _ = http.NewRequest("POST", web.URL+"/mcp", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+bad)
		res, e = web.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatal("wrong token kind accepted")
		}
	}
	list := human.request("GET", "/api/account/apps", nil, 200)
	if strings.Contains(string(list), tokens.Access) || strings.Contains(string(list), tokens.Refresh) {
		t.Fatal("credential leaked to management")
	}
	_, grant, e := s.oauth.Authenticate(ctx, tokens.Access)
	if e != nil {
		t.Fatal(e)
	}
	human.request("POST", "/api/account/apps/revoke", map[string]string{"id": grant.ID}, 200)
	if _, _, e = s.oauth.Authenticate(ctx, tokens.Access); e == nil {
		t.Fatal("revocation not immediate")
	}
	if _, e = s.oauth.Token(ctx, "refresh_token", tokens.Refresh, client.ID, "", web.URL+"/mcp", "", ""); e == nil {
		t.Fatal("revoked refresh accepted")
	}
}

func TestDefaultMCPDiscoveryOmitsLegacyOrders(t *testing.T) {
	ctx := context.Background()
	p := newTestIdentityProvider(t)
	s, _, a := accountServer(t, p)
	s.config.LegacyTools = false
	principal := a.Bindings
	var user accounts.Principal
	for _, v := range principal {
		user = v
		break
	}
	server := s.mcpServer(httptest.NewRequest("POST", "http://127.0.0.1/mcp", nil), user)
	left, right := mcp.NewInMemoryTransports()
	ss, e := server.Connect(ctx, left, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, right, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	list, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	if len(names) != 3 || !names["commerce_profile"] || !names["paypal_connection_status"] || !names["paypal_list_invoices"] {
		t.Fatal(names)
	}
}
