package commerce

import (
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/mcpauth"
)

const oauthCookie = "aria_oauth_transaction"

func oauthMachine(path string) bool {
	return slices.Contains([]string{"/mcp", "/oauth/token", "/oauth/register", "/oauth/revoke"}, path)
}
func (s *Server) mountMCP() {
	if s.config.Account == nil {
		return
	}
	s.oauth = &mcpauth.Broker{Access: s.config.Account.Access, Store: s.config.Store, Origin: s.config.PublicURL, Secret: s.config.SessionSecret}
	s.mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.resourceMetadata)
	s.mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.resourceMetadata)
	s.mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.authorizationMetadata)
	s.mux.HandleFunc("POST /oauth/register", s.registerClient)
	s.mux.HandleFunc("GET /oauth/authorize", s.oauthAuthorize)
	s.mux.HandleFunc("GET /oauth/consent", s.oauthConsent)
	s.mux.HandleFunc("POST /oauth/consent", s.oauthConsent)
	s.mux.HandleFunc("POST /oauth/token", s.oauthToken)
	s.mux.HandleFunc("POST /oauth/revoke", s.oauthRevoke)
	s.mux.HandleFunc("GET /api/account/apps", s.connectedApps)
	s.mux.HandleFunc("POST /api/account/apps/revoke", s.revokeApp)
	handler := s.remoteMCP()
	for _, method := range []string{"GET", "POST", "DELETE", "OPTIONS"} {
		s.mux.Handle(method+" /mcp", handler)
	}
}
func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"resource": s.oauth.Resource(), "authorization_servers": []string{s.config.PublicURL}, "scopes_supported": []string{"read", "draft", "offline_access"}, "bearer_methods_supported": []string{"header"}, "resource_name": "Aria Commerce Sandbox"})
}
func (s *Server) authorizationMetadata(w http.ResponseWriter, r *http.Request) {
	origin := s.config.PublicURL
	write(w, 200, map[string]any{"issuer": origin, "authorization_endpoint": origin + "/oauth/authorize", "token_endpoint": origin + "/oauth/token", "registration_endpoint": origin + "/oauth/register", "revocation_endpoint": origin + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"read", "draft", "offline_access"}, "authorization_response_iss_parameter_supported": true})
}
func oauthError(w http.ResponseWriter, status int, code string) {
	write(w, status, map[string]string{"error": code})
}
func (s *Server) registerClient(w http.ResponseWriter, r *http.Request) {
	if !s.quota(w, r, "oauth-register-minute", "all", 10, time.Minute) || !s.quota(w, r, "oauth-register-day", "all", 100, 24*time.Hour) {
		return
	}
	var in struct {
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
		Auth      string   `json:"token_endpoint_auth_method"`
		Grants    []string `json:"grant_types"`
		Responses []string `json:"response_types"`
	}
	// Registration metadata is not an identity proof. Ignore optional metadata; never fetch URLs.
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in) != nil || (in.Auth != "" && in.Auth != "none") {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	for _, g := range in.Grants {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	for _, v := range in.Responses {
		if v != "code" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	c, e := s.oauth.Register(r.Context(), in.Name, in.Redirects)
	if e != nil {
		oauthError(w, 400, "invalid_redirect_uri")
		return
	}
	write(w, 201, map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.Redirects, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}
func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		oauthError(w, 400, "invalid_request")
		return
	}
	if !s.quota(w, r, "oauth-authorize", "all", s.config.globalLimit(), time.Minute) {
		return
	}
	cookie := randomID() + randomID()
	scopes := strings.Fields(q.Get("scope"))
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}
	id, e := s.oauth.Begin(r.Context(), mcpauth.Request{ClientID: q.Get("client_id"), Redirect: q.Get("redirect_uri"), State: q.Get("state"), Challenge: q.Get("code_challenge"), Resource: q.Get("resource"), Scopes: scopes}, cookie)
	if e != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: cookie, Path: "/oauth", HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, "/oauth/consent?request="+id, 303)
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Connect Aria Commerce</title><link rel="stylesheet" href="/style.css"><body><main><h1>Connect Aria Commerce Sandbox</h1><p>The application calls itself <strong>{{.Name}}</strong>. This name is supplied by the application.</p><p>Exact callback: <code>{{.Redirect}}</code></p><p>Account role: {{.Role}}. Granted permissions: <strong>{{.Scopes}}</strong>.</p><p>This connection can read the assigned merchant's catalogue and orders. Draft access creates unapproved orders only. It cannot approve, pay, capture, refund, or manage accounts.</p><p>{{.Expiry}}</p><p><a href="/privacy.html" target="_blank" rel="noopener">Privacy policy</a></p><p>You can revoke access at any time in Your connected account → Connected apps.</p><form method="post" action="/oauth/consent"><input type="hidden" name="request" value="{{.ID}}"><input type="hidden" name="csrf" value="{{.CSRF}}"><button name="decision" value="allow">Allow connection</button> <button name="decision" value="deny">Cancel</button></form></main></body></html>`))

func (s *Server) oauthConsent(w http.ResponseWriter, r *http.Request) {
	request := r.URL.Query().Get("request")
	if r.Method == "POST" {
		if r.ParseForm() != nil {
			oauthError(w, 400, "invalid_request")
			return
		}
		request = r.PostForm.Get("request")
	}
	c, e := r.Cookie(oauthCookie)
	if e != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	q, _, e := s.oauth.Pending(r.Context(), request, c.Value)
	if e != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	// Browsers enforce form-action across the POST's redirect chain. Permit
	// only this transaction's validated callback origin, never a global wildcard.
	policyCallback, parseErr := url.Parse(q.Redirect)
	if parseErr != nil || policyCallback.Host == "" {
		oauthError(w, 400, "invalid_request")
		return
	}
	callbackOrigin := policyCallback.Scheme + "://" + policyCallback.Host
	w.Header().Set("Content-Security-Policy", strings.Replace(s.csp(), "form-action 'self'", "form-action 'self' "+callbackOrigin, 1))
	id, e := s.accountIdentity(r)
	if e != nil || id.bearer {
		if r.Method == "GET" {
			http.Redirect(w, r, "/api/auth/login?connect="+request, 303)
		} else {
			oauthError(w, 401, "login_required")
		}
		return
	}
	if r.Method == "GET" {
		client, e := s.oauth.Client(r.Context(), q.ClientID)
		if e != nil {
			oauthError(w, 400, "invalid_client")
			return
		}
		scopes := []string{}
		for _, v := range q.Scopes {
			if v == "offline_access" || id.principal.Allows(v) {
				scopes = append(scopes, v)
			}
		}
		expiry := "Expires after 15 minutes. Reconnect to continue."
		if slices.Contains(scopes, "offline_access") {
			expiry = "May renew access for up to 30 days, including while this browser is closed."
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = consentPage.Execute(w, map[string]string{"Name": client.Name, "Redirect": q.Redirect, "Role": id.principal.Role, "Scopes": strings.Join(scopes, ", "), "Expiry": expiry, "ID": request, "CSRF": id.csrf})
		return
	}
	decision := r.PostForm.Get("decision")
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(id.csrf)) != 1 || (decision != "allow" && decision != "deny") {
		oauthError(w, 403, "access_denied")
		return
	}
	q, code, e := s.oauth.Consent(r.Context(), request, c.Value, id.principal, decision == "allow")
	if e != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	callback, _ := url.Parse(q.Redirect)
	v := callback.Query()
	v.Set("state", q.State)
	v.Set("iss", s.config.PublicURL)
	if code != "" {
		v.Set("code", code)
	} else {
		v.Set("error", "access_denied")
	}
	callback.RawQuery = v.Encode()
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/oauth", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode})
	s.logger.InfoContext(r.Context(), "oauth_consent", "user_id", id.principal.UserID, "client_id", q.ClientID, "allowed", code != "")
	http.Redirect(w, r, callback.String(), 303)
}
func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if !s.quota(w, r, "oauth-token", "all", 120, time.Minute) {
		return
	}
	if r.ParseForm() != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	f := r.PostForm
	resource := f.Get("resource")
	if resource == "" && f.Get("grant_type") == "refresh_token" {
		resource = s.oauth.Resource()
	}
	token := f.Get("code")
	if f.Get("grant_type") == "refresh_token" {
		token = f.Get("refresh_token")
	}
	out, e := s.oauth.Token(r.Context(), f.Get("grant_type"), token, f.Get("client_id"), f.Get("redirect_uri"), resource, f.Get("code_verifier"), f.Get("scope"))
	if e != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	write(w, 200, out)
}
func (s *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.quota(w, r, "oauth-revoke", "all", 120, time.Minute) {
		return
	}
	if r.ParseForm() != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	if e := s.oauth.RevokeToken(r.Context(), r.PostForm.Get("token"), r.PostForm.Get("client_id")); e != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	write(w, 200, map[string]any{})
}
func (s *Server) connectedApps(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	list, e := s.oauth.Grants(r.Context(), id.principal)
	if e != nil {
		s.accountError(w, e)
		return
	}
	write(w, 200, map[string]any{"apps": list, "mcp_url": s.oauth.Resource()})
}
func (s *Server) revokeApp(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "Invalid app connection.")
		return
	}
	if e := s.oauth.Revoke(r.Context(), id.principal, in.ID); e != nil {
		s.accountError(w, e)
		return
	}
	s.logger.InfoContext(r.Context(), "oauth_connection_revoked", "user_id", id.principal.UserID, "connection_id", in.ID)
	write(w, 200, map[string]bool{"revoked": true})
}

// Return only this fixed, local route after upstream login. Never accept a return URL.
func connectReturn(raw string) string {
	if len(raw) != 32 {
		return ""
	}
	for _, c := range raw {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return ""
		}
	}
	return "/oauth/consent?request=" + raw
}
