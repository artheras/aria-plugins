package paypalaccounts

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/artheras/aria-plugins/packages/store"
)

var ErrGrantChanged = errors.New("PayPal connection changed; start a new authorization")
var vaultUser = regexp.MustCompile(`^[a-f0-9]{64}$`)
var keyName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Vault stores only encrypted, independently verified merchant grants. It has
// no public token-import API. Only an approved onboarding adapter may write it.
type Vault struct {
	storage store.Store
	active  string
	keys    map[string]cipher.AEAD
}
type envelope struct {
	Version    int    `json:"version"`
	KeyID      string `json:"key_id,omitempty"`
	Nonce      string `json:"nonce,omitempty"`
	Ciphertext string `json:"ciphertext,omitempty"`
	Revoked    bool   `json:"revoked,omitempty"`
	Generation string `json:"generation,omitempty"`
}
type storedGrant struct {
	Grant Grant  `json:"grant"`
	Token string `json:"access_token"`
}

// LoadVault decodes an operator-owned secret key ring; never accept this JSON
// from a user, model, browser or MCP argument.
func LoadVault(storage store.Store, raw []byte) (*Vault, error) {
	if len(raw) > 8192 {
		return nil, ErrUnavailable
	}
	var config struct {
		Active string            `json:"active_key"`
		Keys   map[string]string `json:"keys"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, ErrUnavailable
	}
	keys := map[string][]byte{}
	for id, encoded := range config.Keys {
		key, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			return nil, ErrUnavailable
		}
		keys[id] = key
	}
	return NewVault(storage, config.Active, keys)
}

// NewVault copies the key ring so callers cannot mutate keys after construction.
// Retain old keys during rotation; all new writes use active. Keep keys in a
// dedicated secret, separate from the record store and browser session key.
func NewVault(storage store.Store, active string, keys map[string][]byte) (*Vault, error) {
	if storage == nil || !keyName.MatchString(active) || len(keys) == 0 || len(keys) > 8 {
		return nil, ErrUnavailable
	}
	v := &Vault{storage: storage, active: active, keys: map[string]cipher.AEAD{}}
	for name, key := range keys {
		if !keyName.MatchString(name) || len(key) != 32 {
			return nil, ErrUnavailable
		}
		block, e := aes.NewCipher(key)
		if e != nil {
			return nil, ErrUnavailable
		}
		a, e := cipher.NewGCM(block)
		if e != nil {
			return nil, ErrUnavailable
		}
		v.keys[name] = a
	}
	if v.keys[active] == nil {
		return nil, ErrUnavailable
	}
	return v, nil
}
func grantKey(user string) string {
	h := sha256.Sum256([]byte(user))
	return "paypal_grant_v1_" + hex.EncodeToString(h[:])
}
func revision(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func (v *Vault) current(ctx context.Context, user string) ([]byte, error) {
	if v == nil || !vaultUser.MatchString(user) {
		return nil, ErrNotConnected
	}
	raw, e := v.storage.Get(ctx, grantKey(user))
	if errors.Is(e, store.ErrNotFound) {
		return nil, nil
	}
	if e != nil || len(raw) > 128<<10 {
		return nil, ErrUnavailable
	}
	return raw, nil
}

// Begin snapshots this user's generation before redirecting for authorization.
// It does not authorize anything. Complete must see the same generation, so a
// stale callback cannot undo a disconnect or replace a newer account binding.
func (v *Vault) Begin(ctx context.Context, user string) (string, error) {
	raw, e := v.current(ctx, user)
	if e != nil {
		return "", e
	}
	return revision(raw), nil
}
func (v *Vault) Complete(ctx context.Context, user, expected string, g Grant) error {
	if !validGrant(user, g, time.Now()) || len(g.AccessToken) > 32<<10 || len(g.Capabilities) > 16 {
		return ErrNotConnected
	}
	raw, e := v.current(ctx, user)
	if e != nil {
		return e
	}
	if expected == "" || revision(raw) != expected {
		return ErrGrantChanged
	}
	body, e := json.Marshal(storedGrant{Grant: g, Token: g.AccessToken})
	if e != nil {
		return ErrUnavailable
	}
	a := v.keys[v.active]
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return ErrUnavailable
	}
	aad := []byte("paypal-grant-v1\x00" + user + "\x00" + v.active)
	sealed := a.Seal(nil, nonce, body, aad)
	next, _ := json.Marshal(envelope{Version: 1, KeyID: v.active, Nonce: base64.RawStdEncoding.EncodeToString(nonce), Ciphertext: base64.RawStdEncoding.EncodeToString(sealed)})
	e = v.storage.CAS(ctx, grantKey(user), raw, next)
	if errors.Is(e, store.ErrConflict) {
		return ErrGrantChanged
	}
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func (v *Vault) Resolve(ctx context.Context, user string) (Grant, error) {
	raw, e := v.current(ctx, user)
	if e != nil {
		return Grant{}, e
	}
	if len(raw) == 0 {
		return Grant{}, ErrNotConnected
	}
	var env envelope
	if json.Unmarshal(raw, &env) != nil || env.Version != 1 {
		return Grant{}, ErrUnavailable
	}
	if env.Revoked {
		return Grant{}, ErrNotConnected
	}
	a := v.keys[env.KeyID]
	if a == nil {
		return Grant{}, ErrUnavailable
	}
	nonce, e := base64.RawStdEncoding.DecodeString(env.Nonce)
	if e != nil || len(nonce) != a.NonceSize() {
		return Grant{}, ErrUnavailable
	}
	body, e := base64.RawStdEncoding.DecodeString(env.Ciphertext)
	if e != nil {
		return Grant{}, ErrUnavailable
	}
	clear, e := a.Open(nil, nonce, body, []byte("paypal-grant-v1\x00"+user+"\x00"+env.KeyID))
	if e != nil {
		return Grant{}, ErrUnavailable
	}
	var saved storedGrant
	if json.Unmarshal(clear, &saved) != nil {
		return Grant{}, ErrUnavailable
	}
	saved.Grant.AccessToken = saved.Token
	if !validGrant(user, saved.Grant, time.Now()) {
		return Grant{}, ErrNotConnected
	}
	return saved.Grant, nil
}

// Disconnect atomically removes stored credential material and leaves a
// tombstone. No refresh/cache can restore it; a fresh authorization is required.
// This stops Aria access. Provider-side consent must also be revoked at PayPal.
func (v *Vault) Disconnect(ctx context.Context, user string) error {
	for i := 0; i < 8; i++ {
		raw, e := v.current(ctx, user)
		if e != nil {
			return e
		}
		generation := make([]byte, 16)
		if _, e = rand.Read(generation); e != nil {
			return ErrUnavailable
		}
		next, _ := json.Marshal(envelope{Version: 1, Revoked: true, Generation: hex.EncodeToString(generation)})
		e = v.storage.CAS(ctx, grantKey(user), raw, next)
		if errors.Is(e, store.ErrConflict) {
			continue
		}
		if e != nil {
			return ErrUnavailable
		}
		return nil
	}
	return ErrGrantChanged
}
