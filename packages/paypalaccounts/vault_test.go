package paypalaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/store"
)

func vaultFor(t *testing.T) (*Vault, *store.File) {
	t.Helper()
	storage, e := store.NewFile(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	v, e := NewVault(storage, "first", map[string][]byte{"first": []byte(strings.Repeat("k", 32))})
	if e != nil {
		t.Fatal(e)
	}
	return v, storage
}
func TestEncryptedGrantRoundTripRotationAndNoPlaintext(t *testing.T) {
	ctx := context.Background()
	user := strings.Repeat("a", 64)
	v, storage := vaultFor(t)
	before, e := v.Begin(ctx, user)
	if e != nil {
		t.Fatal(e)
	}
	g := grant(user)
	g.AccessToken = "private-paypal-access-token"
	if e = v.Complete(ctx, user, before, g); e != nil {
		t.Fatal(e)
	}
	raw, _ := storage.Get(ctx, grantKey(user))
	if strings.Contains(string(raw), g.AccessToken) || strings.Contains(string(raw), g.MerchantID) || strings.Contains(string(raw), user) {
		t.Fatal("plaintext credential or account metadata persisted")
	}
	v, e = NewVault(storage, "second", map[string][]byte{"first": []byte(strings.Repeat("k", 32)), "second": []byte(strings.Repeat("z", 32))})
	if e != nil {
		t.Fatal(e)
	}
	restored, e := v.Resolve(ctx, user)
	if e != nil || restored.AccessToken != g.AccessToken || restored.UserID != user {
		t.Fatal("restart/rotation read failed", e)
	}
	before, _ = v.Begin(ctx, user)
	if e = v.Complete(ctx, user, before, g); e != nil {
		t.Fatal(e)
	}
	raw, _ = storage.Get(ctx, grantKey(user))
	var env envelope
	json.Unmarshal(raw, &env)
	if env.KeyID != "second" {
		t.Fatal("new writes did not use active key")
	}
	old, _ := NewVault(storage, "first", map[string][]byte{"first": []byte(strings.Repeat("k", 32))})
	if _, e = old.Resolve(ctx, user); !errors.Is(e, ErrUnavailable) {
		t.Fatal("unknown key accepted", e)
	}
}
func TestRecordSwapAndTamperingNeverResolveAnotherAccount(t *testing.T) {
	ctx := context.Background()
	alice, bob := strings.Repeat("a", 64), strings.Repeat("b", 64)
	v, storage := vaultFor(t)
	before, _ := v.Begin(ctx, alice)
	if e := v.Complete(ctx, alice, before, grant(alice)); e != nil {
		t.Fatal(e)
	}
	raw, _ := storage.Get(ctx, grantKey(alice))
	if e := storage.CAS(ctx, grantKey(bob), nil, raw); e != nil {
		t.Fatal(e)
	}
	if _, e := v.Resolve(ctx, bob); !errors.Is(e, ErrUnavailable) {
		t.Fatal("swapped record decrypted", e)
	}
	var env envelope
	json.Unmarshal(raw, &env)
	env.Ciphertext = "corrupt"
	bad, _ := json.Marshal(env)
	storage.CAS(ctx, grantKey(alice), raw, bad)
	if _, e := v.Resolve(ctx, alice); !errors.Is(e, ErrUnavailable) {
		t.Fatal("tampering accepted", e)
	}
}
func TestDisconnectBlocksPendingCallbackAndConcurrentCompletion(t *testing.T) {
	ctx := context.Background()
	user := strings.Repeat("a", 64)
	v, storage := vaultFor(t)
	before, _ := v.Begin(ctx, user)
	if e := v.Disconnect(ctx, user); e != nil {
		t.Fatal(e)
	}
	if e := v.Complete(ctx, user, before, grant(user)); !errors.Is(e, ErrGrantChanged) {
		t.Fatal("stale callback restored connection", e)
	}
	// Disconnecting again while already disconnected must invalidate new pending
	// authorizations too, rather than reusing the previous tombstone generation.
	before, _ = v.Begin(ctx, user)
	v.Disconnect(ctx, user)
	if e := v.Complete(ctx, user, before, grant(user)); !errors.Is(e, ErrGrantChanged) {
		t.Fatal("tombstone reused", e)
	}
	before, _ = v.Begin(ctx, user)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = v.Complete(ctx, user, before, grant(user)) }()
	go func() {
		defer wg.Done()
		if e := v.Disconnect(ctx, user); e != nil {
			t.Error(e)
		}
	}()
	wg.Wait()
	if _, e := v.Resolve(ctx, user); !errors.Is(e, ErrNotConnected) {
		t.Fatal("concurrent completion undid disconnect", e)
	}
	raw, _ := storage.Get(ctx, grantKey(user))
	if strings.Contains(string(raw), "ciphertext") || strings.Contains(string(raw), "access_token") {
		t.Fatal("disconnect retained credentials")
	}
}
func TestVaultRejectsInvalidSecretAndUnverifiedGrant(t *testing.T) {
	v, storage := vaultFor(t)
	ctx := context.Background()
	user := strings.Repeat("a", 64)
	for _, raw := range []string{`{}`, `{"active_key":"x","keys":{"x":"bad"}}`, `{"active_key":"x","keys":{},"unknown":true}`} {
		if _, e := LoadVault(storage, []byte(raw)); e == nil {
			t.Fatal("invalid secret accepted")
		}
	}
	revision, _ := v.Begin(ctx, user)
	g := grant(user)
	g.VerifiedAt = time.Time{}
	if e := v.Complete(ctx, user, revision, g); !errors.Is(e, ErrNotConnected) {
		t.Fatal(e)
	}
	g = grant(user)
	g.ExpiresAt = time.Now()
	if e := v.Complete(ctx, user, revision, g); !errors.Is(e, ErrNotConnected) {
		t.Fatal(e)
	}
	g = grant(strings.Repeat("b", 64))
	if e := v.Complete(ctx, user, revision, g); !errors.Is(e, ErrNotConnected) {
		t.Fatal(e)
	}
	if _, e := v.Begin(ctx, "user-supplied-merchant-id"); !errors.Is(e, ErrNotConnected) {
		t.Fatal(e)
	}
}
