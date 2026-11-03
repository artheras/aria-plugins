// Package mcpauth issues resource-bound, revocable credentials for remote MCP.
// The upstream OIDC session authenticates a human; its tokens are never forwarded.
package mcpauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/accounts"
	"github.com/artheras/aria-plugins/packages/store"
)

var ErrDenied = errors.New("invalid or expired authorization")
var id32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var id64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var challenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var verifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

type Broker struct {
	Access         *accounts.Access
	Store          store.Store
	Origin, Secret string
	Now            func() time.Time
}
type Client struct {
	ID        string   `json:"client_id"`
	Name      string   `json:"client_name"`
	Redirects []string `json:"redirect_uris"`
}
type Request struct {
	ClientID, Redirect, State, Challenge, Resource string
	Scopes                                         []string
	CookieHash                                     string
	Expires                                        int64
	Used                                           bool
}
type Grant struct {
	ID         string   `json:"id"`
	ClientID   string   `json:"client_id"`
	Name       string   `json:"name"`
	Redirect   string   `json:"redirect_uri"`
	Resource   string   `json:"resource"`
	MerchantID string   `json:"merchant_id"`
	Scopes     []string `json:"scopes"`
	Expires    int64    `json:"expires_at"`
	Revoked    bool     `json:"revoked"`
}

// Private storage is separate from the public grant projection.
type family struct {
	Grant
	CodeHash, Challenge string
	CodeExpires         int64
	CodeUsed            bool
	Access              map[string]int64
	RefreshHash         string
	Sequence            int
}
type record struct{ Grants map[string]family }
type Tokens struct {
	Access  string `json:"access_token"`
	Type    string `json:"token_type"`
	Expires int    `json:"expires_in"`
	Refresh string `json:"refresh_token,omitempty"`
	Scope   string `json:"scope"`
}

func (b *Broker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
func (b *Broker) Resource() string { return b.Origin + "/mcp" }
func random(n int) string {
	v := make([]byte, n)
	if _, err := rand.Read(v); err != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(v)
}
func hash(s string) string   { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func equal(a, c string) bool { return hmac.Equal([]byte(a), []byte(c)) }
func (b *Broker) save(ctx context.Context, key string, old []byte, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return b.Store.CAS(ctx, key, old, raw)
}
func allowedRedirect(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return false
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") && u.Port() != "" {
		return true
	}
	if u.Scheme != "https" || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "chatgpt.com":
		return u.Path == "/connector_platform_oauth_redirect" || regexp.MustCompile(`^/connector/oauth/[A-Za-z0-9_-]{1,200}$`).MatchString(u.Path)
	case "claude.ai", "claude.com":
		return u.Path == "/api/mcp/auth_callback"
	}
	return false
}
func (b *Broker) Register(ctx context.Context, name string, redirects []string) (Client, error) {
	if strings.TrimSpace(name) == "" || len(name) > 80 || len(redirects) == 0 || len(redirects) > 10 {
		return Client{}, ErrDenied
	}
	for _, r := range redirects {
		if len(r) > 500 || !allowedRedirect(r) {
			return Client{}, ErrDenied
		}
	}
	c := Client{random(16), name, slices.Clone(redirects)}
	return c, b.save(ctx, "meta_mcp_client_"+c.ID, nil, c)
}
func (b *Broker) Client(ctx context.Context, id string) (Client, error) {
	var c Client
	if !id32.MatchString(id) {
		return c, ErrDenied
	}
	raw, e := b.Store.Get(ctx, "meta_mcp_client_"+id)
	if e != nil || json.Unmarshal(raw, &c) != nil || c.ID != id {
		return c, ErrDenied
	}
	return c, nil
}
func validScopes(v []string) bool {
	if len(v) == 0 || len(v) > 3 || !slices.Contains(v, "read") {
		return false
	}
	seen := map[string]bool{}
	for _, s := range v {
		if seen[s] || !slices.Contains([]string{"read", "draft", "offline_access"}, s) {
			return false
		}
		seen[s] = true
	}
	return true
}
func (b *Broker) Begin(ctx context.Context, r Request, cookie string) (string, error) {
	c, e := b.Client(ctx, r.ClientID)
	if e != nil || !slices.Contains(c.Redirects, r.Redirect) || r.Resource != b.Resource() || !challenge.MatchString(r.Challenge) || len(r.State) > 2048 || !validScopes(r.Scopes) || cookie == "" {
		return "", ErrDenied
	}
	id := random(16)
	r.CookieHash = hash(cookie)
	r.Expires = b.now().Add(10 * time.Minute).Unix()
	r.Used = false
	return id, b.save(ctx, "meta_mcp_request_"+id, nil, r)
}
func (b *Broker) Pending(ctx context.Context, id, cookie string) (Request, []byte, error) {
	var r Request
	if !id32.MatchString(id) || cookie == "" {
		return r, nil, ErrDenied
	}
	raw, e := b.Store.Get(ctx, "meta_mcp_request_"+id)
	if e != nil || json.Unmarshal(raw, &r) != nil || r.Used || r.Expires <= b.now().Unix() || !equal(r.CookieHash, hash(cookie)) {
		return r, nil, ErrDenied
	}
	return r, raw, nil
}
func (b *Broker) load(ctx context.Context, user string) (record, []byte, error) {
	r := record{Grants: map[string]family{}}
	if !id64.MatchString(user) {
		return r, nil, ErrDenied
	}
	raw, e := b.Store.Get(ctx, "meta_mcp_user_"+user)
	if errors.Is(e, store.ErrNotFound) {
		return r, nil, nil
	}
	if e != nil || json.Unmarshal(raw, &r) != nil || r.Grants == nil {
		return r, nil, accounts.ErrUnavailable
	}
	return r, raw, nil
}
func (b *Broker) effective(p accounts.Principal, scopes []string) []string {
	out := []string{}
	for _, s := range scopes {
		if s == "offline_access" || p.Allows(s) {
			out = append(out, s)
		}
	}
	return out
}
func (b *Broker) Consent(ctx context.Context, id, cookie string, p accounts.Principal, allow bool) (Request, string, error) {
	q, old, e := b.Pending(ctx, id, cookie)
	if e != nil {
		return q, "", e
	}
	current, ok := b.Access.Bindings[p.UserID]
	if !ok || current.MerchantID != p.MerchantID {
		return q, "", ErrDenied
	}
	q.Used = true
	if e = b.save(ctx, "meta_mcp_request_"+id, old, q); e != nil {
		return q, "", e
	}
	if !allow {
		return q, "", nil
	}
	c, e := b.Client(ctx, q.ClientID)
	if e != nil {
		return q, "", e
	}
	fid := random(16)
	code := "mcc_" + p.UserID + "." + fid + "." + random(32)
	scopes := b.effective(current, q.Scopes)
	ttl := 15 * time.Minute
	if slices.Contains(scopes, "offline_access") {
		ttl = 30 * 24 * time.Hour
	}
	f := family{Grant: Grant{ID: fid, ClientID: c.ID, Name: c.Name, Redirect: q.Redirect, Resource: b.Resource(), MerchantID: p.MerchantID, Scopes: scopes, Expires: b.now().Add(ttl).Unix()}, CodeHash: hash(code), Challenge: q.Challenge, CodeExpires: b.now().Add(5 * time.Minute).Unix(), Access: map[string]int64{}}
	for i := 0; i < 8; i++ {
		r, prev, e := b.load(ctx, p.UserID)
		if e != nil {
			return q, "", e
		}
		for k, g := range r.Grants {
			if g.Revoked || g.Expires <= b.now().Unix() {
				delete(r.Grants, k)
			}
		}
		if len(r.Grants) >= 20 {
			return q, "", errors.New("revoke an existing app connection first")
		}
		r.Grants[fid] = f
		e = b.save(ctx, "meta_mcp_user_"+p.UserID, prev, r)
		if e == nil {
			return q, code, nil
		}
		if !errors.Is(e, store.ErrConflict) {
			return q, "", e
		}
	}
	return q, "", accounts.ErrUnavailable
}
func parse(token, prefix string) (string, string, bool) {
	v := strings.Split(token, ".")
	if len(v) < 3 || !strings.HasPrefix(v[0], prefix) || !id64.MatchString(strings.TrimPrefix(v[0], prefix)) || !id32.MatchString(v[1]) {
		return "", "", false
	}
	return strings.TrimPrefix(v[0], prefix), v[1], true
}
func (b *Broker) refresh(user, fid string, seq int) string {
	base := "mcr_" + user + "." + fid + "." + strconv.Itoa(seq) + "." + random(32)
	h := hmac.New(sha256.New, []byte(b.Secret))
	h.Write([]byte(b.Resource() + "\x00" + base))
	return base + "." + hex.EncodeToString(h.Sum(nil))
}
func (b *Broker) refreshSequence(token string) (int, bool) {
	v := strings.Split(token, ".")
	if len(v) != 5 || !id64.MatchString(v[3]) || !id64.MatchString(v[4]) {
		return 0, false
	}
	n, e := strconv.Atoi(v[2])
	if e != nil || n < 1 {
		return 0, false
	}
	h := hmac.New(sha256.New, []byte(b.Secret))
	h.Write([]byte(b.Resource() + "\x00" + strings.Join(v[:4], ".")))
	return n, equal(v[4], hex.EncodeToString(h.Sum(nil)))
}
func (b *Broker) Token(ctx context.Context, kind, token, client, redirect, resource, proof, scope string) (Tokens, error) {
	prefix := "mcc_"
	if kind == "refresh_token" {
		prefix = "mcr_"
	} else if kind != "authorization_code" {
		return Tokens{}, ErrDenied
	}
	user, fid, ok := parse(token, prefix)
	if !ok || resource != b.Resource() {
		return Tokens{}, ErrDenied
	}
	for i := 0; i < 8; i++ {
		r, old, e := b.load(ctx, user)
		if e != nil {
			return Tokens{}, e
		}
		f, ok := r.Grants[fid]
		p, member := b.Access.Bindings[user]
		if !ok || !member || f.Revoked || f.Expires <= b.now().Unix() || f.Resource != b.Resource() || f.ClientID != client || p.MerchantID != f.MerchantID {
			return Tokens{}, ErrDenied
		}
		replay := false
		if kind == "authorization_code" {
			if !equal(hash(token), f.CodeHash) || redirect != f.Redirect || !verifier.MatchString(proof) || pkce(proof) != f.Challenge || f.CodeExpires <= b.now().Unix() {
				return Tokens{}, ErrDenied
			}
			replay = f.CodeUsed
			f.CodeUsed = true
		} else {
			seq, valid := b.refreshSequence(token)
			if !valid || !slices.Contains(f.Scopes, "offline_access") {
				return Tokens{}, ErrDenied
			}
			replay = seq < f.Sequence
			if !replay && (seq != f.Sequence || !equal(hash(token), f.RefreshHash)) {
				return Tokens{}, ErrDenied
			}
		}
		if replay {
			f.Revoked = true
			r.Grants[fid] = f
			if e = b.save(ctx, "meta_mcp_user_"+user, old, r); errors.Is(e, store.ErrConflict) {
				continue
			}
			return Tokens{}, ErrDenied
		}
		f.Scopes = b.effective(p, f.Scopes)
		if scope != "" {
			requested := strings.Fields(scope)
			if !validScopes(requested) {
				return Tokens{}, ErrDenied
			}
			for _, s := range requested {
				if !slices.Contains(f.Scopes, s) {
					return Tokens{}, ErrDenied
				}
			}
			f.Scopes = requested
		}
		access := "mca_" + user + "." + fid + "." + random(32)
		for k, exp := range f.Access {
			if exp <= b.now().Unix() {
				delete(f.Access, k)
			}
		}
		if len(f.Access) >= 128 {
			return Tokens{}, ErrDenied
		}
		expiry := min(b.now().Add(15*time.Minute).Unix(), f.Expires)
		f.Access[hash(access)] = expiry
		out := Tokens{Access: access, Type: "Bearer", Expires: int(expiry - b.now().Unix()), Scope: strings.Join(f.Scopes, " ")}
		if slices.Contains(f.Scopes, "offline_access") {
			f.Sequence++
			out.Refresh = b.refresh(user, fid, f.Sequence)
			f.RefreshHash = hash(out.Refresh)
		} else {
			f.RefreshHash = ""
		}
		r.Grants[fid] = f
		e = b.save(ctx, "meta_mcp_user_"+user, old, r)
		if e == nil {
			return out, nil
		}
		if !errors.Is(e, store.ErrConflict) {
			return Tokens{}, e
		}
	}
	return Tokens{}, accounts.ErrUnavailable
}
func (b *Broker) Authenticate(ctx context.Context, token string) (accounts.Principal, Grant, error) {
	user, fid, ok := parse(token, "mca_")
	if !ok {
		return accounts.Principal{}, Grant{}, ErrDenied
	}
	r, _, e := b.load(ctx, user)
	if e != nil {
		return accounts.Principal{}, Grant{}, e
	}
	f, ok := r.Grants[fid]
	p, member := b.Access.Bindings[user]
	if !ok || !member || f.Revoked || f.Expires <= b.now().Unix() || f.Resource != b.Resource() || f.Access[hash(token)] <= b.now().Unix() || p.MerchantID != f.MerchantID {
		return accounts.Principal{}, Grant{}, ErrDenied
	}
	p.Scopes = b.effective(p, f.Scopes)
	return p, f.Grant, nil
}
func (b *Broker) Grants(ctx context.Context, p accounts.Principal) ([]Grant, error) {
	r, _, e := b.load(ctx, p.UserID)
	out := []Grant{}
	if e != nil {
		return out, e
	}
	for _, f := range r.Grants {
		if !f.Revoked && f.Expires > b.now().Unix() && f.MerchantID == p.MerchantID {
			out = append(out, f.Grant)
		}
	}
	slices.SortFunc(out, func(a, c Grant) int { return strings.Compare(a.ID, c.ID) })
	return out, nil
}
func (b *Broker) Revoke(ctx context.Context, p accounts.Principal, id string) error {
	for i := 0; i < 8; i++ {
		r, old, e := b.load(ctx, p.UserID)
		if e != nil {
			return e
		}
		f, ok := r.Grants[id]
		if !ok || f.MerchantID != p.MerchantID {
			return ErrDenied
		}
		f.Revoked = true
		r.Grants[id] = f
		e = b.save(ctx, "meta_mcp_user_"+p.UserID, old, r)
		if e == nil {
			return nil
		}
		if !errors.Is(e, store.ErrConflict) {
			return e
		}
	}
	return accounts.ErrUnavailable
}
func (b *Broker) RevokeToken(ctx context.Context, token, client string) error {
	user, fid, ok := parse(token, "mcr_")
	refresh := ok
	if !ok {
		user, fid, ok = parse(token, "mca_")
	}
	if !ok {
		return nil
	}
	r, _, e := b.load(ctx, user)
	if e != nil {
		return nil
	}
	f, ok := r.Grants[fid]
	if !ok || f.Resource != b.Resource() || f.ClientID != client {
		return nil
	}
	if refresh {
		_, valid := b.refreshSequence(token)
		if !valid {
			return nil
		}
	} else if _, exists := f.Access[hash(token)]; !exists {
		return nil
	}
	return b.Revoke(ctx, accounts.Principal{UserID: user, MerchantID: f.MerchantID}, fid)
}
