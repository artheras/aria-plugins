// Package accounts binds verified OIDC subjects to operator-managed merchant access.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/identity"
	"github.com/artheras/aria-plugins/packages/store"
)

var ErrDenied = errors.New("account access denied")
var ErrUnavailable = errors.New("account storage unavailable")
var hexID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Binding struct {
	Subject    string `json:"subject"`
	MerchantID string `json:"merchant_id"`
	Role       string `json:"role"`
}
type Principal struct {
	UserID     string   `json:"user_id"`
	MerchantID string   `json:"merchant_id"`
	Role       string   `json:"role"`
	Scopes     []string `json:"scopes"`
}

func (p Principal) Allows(scope string) bool { return slices.Contains(p.Scopes, scope) }
func userID(issuer, subject string) string {
	h := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return hex.EncodeToString(h[:])
}
func roleScopes(role string) []string {
	if role == "reader" {
		return []string{"read"}
	}
	return []string{"read", "draft"}
}

type Credential struct {
	MerchantID string   `json:"merchant_id"`
	Audience   string   `json:"audience"`
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Label      string   `json:"label,omitempty"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  int64    `json:"expires_at"`
	Revoked    bool     `json:"revoked"`
}
type accessRecord struct {
	Credentials map[string]Credential `json:"credentials"`
}
type Access struct {
	Audience string
	Store    store.Store
	Issuer   string
	Bindings map[string]Principal
	Now      func() time.Time
}

func NewAccess(storage store.Store, issuer, merchantID, audience string, bindings []Binding) (*Access, error) {
	if storage == nil || issuer == "" || identity.ValidateID(merchantID) != nil || len(bindings) == 0 || len(bindings) > 1000 {
		return nil, errors.New("configure account issuer, merchant and 1 to 1000 bindings")
	}
	u, err := url.Parse(audience)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
		return nil, errors.New("configure a canonical HTTPS account audience, or loopback development origin")
	}
	a := &Access{Audience: audience, Store: storage, Issuer: issuer, Bindings: map[string]Principal{}}
	for _, b := range bindings {
		// One payment-provider configuration serves one merchant. Never attach a second merchant to it.
		if b.Subject == "" || len(b.Subject) > 255 || b.MerchantID != merchantID || !slices.Contains([]string{"reader", "drafter", "operator"}, b.Role) {
			return nil, errors.New("invalid account binding or merchant mismatch")
		}
		id := userID(issuer, b.Subject)
		if _, ok := a.Bindings[id]; ok {
			return nil, errors.New("duplicate account subject")
		}
		a.Bindings[id] = Principal{UserID: id, MerchantID: b.MerchantID, Role: b.Role, Scopes: roleScopes(b.Role)}
	}
	return a, nil
}
func (a *Access) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
func (a *Access) Subject(subject string) (Principal, error) {
	p, ok := a.Bindings[userID(a.Issuer, subject)]
	if !ok {
		return Principal{}, ErrDenied
	}
	return p, nil
}
func (a *Access) load(ctx context.Context, id string) (accessRecord, []byte, error) {
	var r accessRecord
	if !hexID.MatchString(id) {
		return r, nil, ErrDenied
	}
	raw, err := a.Store.Get(ctx, "meta_account_"+id)
	if errors.Is(err, store.ErrNotFound) {
		r.Credentials = map[string]Credential{}
		return r, nil, nil
	}
	if err != nil || json.Unmarshal(raw, &r) != nil || r.Credentials == nil {
		return r, nil, ErrUnavailable
	}
	return r, raw, nil
}
func digest(raw string) string { h := sha256.Sum256([]byte(raw)); return hex.EncodeToString(h[:]) }
func (a *Access) issue(ctx context.Context, p Principal, kind, label string, scopes []string, ttl time.Duration) (string, Credential, error) {
	current, ok := a.Bindings[p.UserID]
	if !ok || current.MerchantID != p.MerchantID || (kind != "session" && kind != "agent") || ttl <= 0 || ttl > 24*time.Hour || len(label) > 64 || len(scopes) == 0 || len(scopes) > 2 {
		return "", Credential{}, ErrDenied
	}
	seen := map[string]bool{}
	for _, v := range scopes {
		if !current.Allows(v) || seen[v] {
			return "", Credential{}, ErrDenied
		}
		seen[v] = true
	}
	if !slices.Contains(scopes, "read") {
		return "", Credential{}, ErrDenied
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", Credential{}, ErrUnavailable
	}
	prefix := "ara_"
	if kind == "session" {
		prefix = "ars_"
	}
	token := prefix + p.UserID + "." + hex.EncodeToString(b)
	c := Credential{MerchantID: current.MerchantID, Audience: a.Audience, ID: digest(token), Kind: kind, Label: label, Scopes: slices.Clone(scopes), ExpiresAt: a.now().Add(ttl).Unix()}
	for i := 0; i < 8; i++ {
		r, old, err := a.load(ctx, p.UserID)
		if err != nil {
			return "", Credential{}, err
		}
		count := 0
		for id, v := range r.Credentials {
			if v.Revoked || v.ExpiresAt <= a.now().Unix() {
				delete(r.Credentials, id)
			} else if v.Kind == kind {
				count++
			}
		}
		if count >= 20 {
			return "", Credential{}, errors.New("revoke an existing connection before creating another")
		}
		r.Credentials[c.ID] = c
		next, _ := json.Marshal(r)
		err = a.Store.CAS(ctx, "meta_account_"+p.UserID, old, next)
		if err == nil {
			return token, c, nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return "", Credential{}, ErrUnavailable
		}
	}
	return "", Credential{}, ErrUnavailable
}
func (a *Access) IssueSession(ctx context.Context, p Principal, ttl time.Duration) (string, Credential, error) {
	return a.issue(ctx, p, "session", "", roleScopes(p.Role), ttl)
}
func (a *Access) IssueGrant(ctx context.Context, p Principal, label string, scopes []string, ttl time.Duration) (string, Credential, error) {
	return a.issue(ctx, p, "agent", label, scopes, ttl)
}
func (a *Access) Authenticate(ctx context.Context, token, kind string) (Principal, Credential, error) {
	prefix := "ara_"
	if kind == "session" {
		prefix = "ars_"
	} else if kind != "agent" {
		return Principal{}, Credential{}, ErrDenied
	}
	if len(token) != 133 || !strings.HasPrefix(token, prefix) || token[68] != '.' || !hexID.MatchString(token[4:68]) || !hexID.MatchString(token[69:]) {
		return Principal{}, Credential{}, ErrDenied
	}
	id := token[4:68]
	p, ok := a.Bindings[id]
	if !ok {
		return Principal{}, Credential{}, ErrDenied
	}
	r, _, err := a.load(ctx, id)
	if err != nil {
		return Principal{}, Credential{}, err
	}
	c, ok := r.Credentials[digest(token)]
	if !ok || c.MerchantID != p.MerchantID || c.Audience != a.Audience || c.Kind != kind || c.Revoked || c.ExpiresAt <= a.now().Unix() {
		return Principal{}, Credential{}, ErrDenied
	}
	scopes := []string{}
	for _, v := range c.Scopes {
		if p.Allows(v) {
			scopes = append(scopes, v)
		}
	}
	p.Scopes = scopes
	return p, c, nil
}
func (a *Access) Revoke(ctx context.Context, p Principal, id string) error {
	if !hexID.MatchString(id) {
		return ErrDenied
	}
	for i := 0; i < 8; i++ {
		r, old, err := a.load(ctx, p.UserID)
		if err != nil {
			return err
		}
		c, ok := r.Credentials[id]
		if !ok || c.MerchantID != p.MerchantID || c.Audience != a.Audience {
			return ErrDenied
		}
		if c.Revoked {
			return nil
		}
		c.Revoked = true
		r.Credentials[id] = c
		next, _ := json.Marshal(r)
		err = a.Store.CAS(ctx, "meta_account_"+p.UserID, old, next)
		if err == nil {
			return nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return ErrUnavailable
		}
	}
	return ErrUnavailable
}
func (a *Access) Grants(ctx context.Context, p Principal) ([]Credential, error) {
	r, _, err := a.load(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := []Credential{}
	for _, v := range r.Credentials {
		if v.MerchantID == p.MerchantID && v.Audience == a.Audience && v.Kind == "agent" && !v.Revoked && v.ExpiresAt > a.now().Unix() {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(x, y Credential) int { return strings.Compare(x.ID, y.ID) })
	return out, nil
}
