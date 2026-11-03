package commerce

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/accounts"
	merchantidentity "github.com/artheras/aria-plugins/packages/identity"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const accountCookie = "aria_account_session"

var reviewOrderID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type loginAttempt struct {
	State, Nonce, Verifier, Return string
	Expires                        int64
}

func (s *Server) accountIdentity(r *http.Request) (identity, error) {
	auth := r.Header.Get("Authorization")
	kind := "session"
	token := ""
	if auth != "" {
		if !strings.HasPrefix(auth, "Bearer ") {
			return identity{}, accounts.ErrDenied
		}
		kind = "agent"
		token = strings.TrimPrefix(auth, "Bearer ")
	} else {
		c, err := r.Cookie(accountCookie)
		if err != nil {
			return identity{}, accounts.ErrDenied
		}
		token = c.Value
	}
	p, c, err := s.config.Account.Access.Authenticate(r.Context(), token, kind)
	if err != nil {
		return identity{}, err
	}
	csrf := ""
	if kind == "session" {
		csrf = s.sign("account-csrf:" + token)
	}
	return identity{owner: merchantidentity.Owner(p.MerchantID), csrf: csrf, bearer: kind == "agent", principal: p, credential: c.ID}, nil
}
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if s.config.Account == nil {
		fail(w, 404, "Account login is not configured.")
		return
	}
	if r.Header.Get("Authorization") != "" {
		fail(w, 403, "Open account login in your browser.")
		return
	}
	// The shared login limiter is also used by the legacy demonstration login.
	if !s.quota(w, r, "account-login", "all", s.config.globalLimit(), time.Minute) {
		return
	}
	a := loginAttempt{State: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier(), Return: "/", Expires: time.Now().Add(5 * time.Minute).Unix()}
	if id := r.URL.Query().Get("order"); reviewOrderID.MatchString(id) {
		a.Return = "/?order=" + id
	}
	if path := connectReturn(r.URL.Query().Get("connect")); path != "" {
		a.Return = path
	}
	raw, _ := json.Marshal(a)
	cipher := s.cursorCipher()
	nonce := make([]byte, cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		fail(w, 503, "Login unavailable.")
		return
	}
	value := base64.RawURLEncoding.EncodeToString(cipher.Seal(nonce, nonce, raw, []byte("oidc-login")))
	http.SetCookie(w, &http.Cookie{Name: "aria_login", Value: value, Path: "/api/auth/callback", HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, s.config.Account.OAuth.AuthCodeURL(a.State, oidc.Nonce(a.Nonce), oauth2.S256ChallengeOption(a.Verifier)), http.StatusFound)
}
func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	if s.config.Account == nil {
		fail(w, 404, "Account login is not configured.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "aria_login", Path: "/api/auth/callback", HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	deny := func() {
		fail(w, 401, "Account login was not completed or access was not granted. Start sign-in again.")
	}
	c, err := r.Cookie("aria_login")
	if err != nil || len(c.Value) > 4096 || r.Header.Get("Authorization") != "" {
		deny()
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	cipher := s.cursorCipher()
	if err != nil || len(raw) < cipher.NonceSize() {
		deny()
		return
	}
	raw, err = cipher.Open(nil, raw[:cipher.NonceSize()], raw[cipher.NonceSize():], []byte("oidc-login"))
	var a loginAttempt
	if err != nil || json.Unmarshal(raw, &a) != nil || a.Expires <= time.Now().Unix() || a.State == "" || a.State != r.URL.Query().Get("state") || r.URL.Query().Get("code") == "" || r.URL.Query().Get("error") != "" {
		deny()
		return
	}
	// Consume the browser-bound state before code exchange, across all replicas.
	if s.config.Store.CAS(r.Context(), "meta_oidc_"+s.sign(a.State), nil, []byte(`{"used":true}`)) != nil {
		deny()
		return
	}
	p, ttl, err := s.config.Account.Exchange(r.Context(), r.URL.Query().Get("code"), a.Verifier, a.Nonce)
	if err != nil {
		deny()
		return
	}
	token, _, err := s.config.Account.Access.IssueSession(r.Context(), p, ttl)
	if err != nil {
		fail(w, 503, "Account session unavailable.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: accountCookie, Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())})
	s.logger.InfoContext(r.Context(), "account_login", "user_id", p.UserID, "merchant_id", p.MerchantID)
	if a.Return != "/" && !strings.HasPrefix(a.Return, "/?order=") && !strings.HasPrefix(a.Return, "/oauth/consent?request=") {
		a.Return = "/"
	}
	http.Redirect(w, r, a.Return, http.StatusSeeOther)
}
func (s *Server) accountError(w http.ResponseWriter, err error) {
	if errors.Is(err, accounts.ErrUnavailable) {
		fail(w, 503, "Account storage unavailable.")
	} else {
		fail(w, 403, "Account permission or connection request is invalid.")
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if s.config.Account == nil {
		fail(w, 404, "Account login is not configured.")
		return
	}
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	if err := s.config.Account.Access.Revoke(r.Context(), id.principal, id.credential); err != nil {
		s.accountError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: accountCookie, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	s.logger.InfoContext(r.Context(), "account_logout", "user_id", id.principal.UserID)
	write(w, 200, map[string]bool{"authenticated": false})
}
func (s *Server) grants(w http.ResponseWriter, r *http.Request) {
	if s.config.Account == nil {
		fail(w, 404, "Account login is not configured.")
		return
	}
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	if r.Method == "GET" {
		list, err := s.config.Account.Access.Grants(r.Context(), id.principal)
		if err != nil {
			s.accountError(w, err)
			return
		}
		write(w, 200, map[string]any{"connections": list})
		return
	}
	var body struct {
		Label  string   `json:"label"`
		Scopes []string `json:"scopes"`
		TTL    int      `json:"ttl_seconds"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "Invalid connection request.")
		return
	}
	if body.TTL == 0 {
		body.TTL = 3600
	}
	token, c, err := s.config.Account.Access.IssueGrant(r.Context(), id.principal, body.Label, body.Scopes, time.Duration(body.TTL)*time.Second)
	if err != nil {
		s.accountError(w, err)
		return
	}
	s.logger.InfoContext(r.Context(), "account_connection_created", "user_id", id.principal.UserID, "connection_id", c.ID, "scopes", c.Scopes)
	write(w, 201, map[string]any{"token": token, "connection": c})
}
func (s *Server) revokeGrant(w http.ResponseWriter, r *http.Request) {
	if s.config.Account == nil {
		fail(w, 404, "Account login is not configured.")
		return
	}
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "Invalid connection request.")
		return
	}
	list, err := s.config.Account.Access.Grants(r.Context(), id.principal)
	if err != nil {
		s.accountError(w, err)
		return
	}
	found := false
	for _, c := range list {
		if c.ID == body.ID {
			found = true
		}
	}
	if !found {
		fail(w, 404, "Connection not found.")
		return
	}
	if err = s.config.Account.Access.Revoke(r.Context(), id.principal, body.ID); err != nil {
		s.accountError(w, err)
		return
	}
	s.logger.InfoContext(r.Context(), "account_connection_revoked", "user_id", id.principal.UserID, "connection_id", body.ID)
	write(w, 200, map[string]bool{"revoked": true})
}
