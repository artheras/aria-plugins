package accounts

import (
	"context"
	"github.com/artheras/aria-plugins/packages/store"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testAccess(t *testing.T) *Access {
	t.Helper()
	s, _ := store.NewFile(t.TempDir())
	a, err := NewAccess(s, "https://id.example", "shop-one", "https://commerce.example", []Binding{{"alice", "shop-one", "operator"}, {"bob", "shop-one", "reader"}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestScopedCredentialsExpireRevokeAndNeverStoreSecrets(t *testing.T) {
	a := testAccess(t)
	ctx := context.Background()
	alice, _ := a.Subject("alice")
	bob, _ := a.Subject("bob")
	token, c, err := a.IssueGrant(ctx, alice, "Codex", []string{"read", "draft"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := a.Authenticate(ctx, token, "agent")
	if err != nil || p.UserID != alice.UserID || !p.Allows("draft") {
		t.Fatal("invalid identity", err)
	}
	if _, _, err = a.Authenticate(ctx, token, "session"); err == nil {
		t.Fatal("agent credential became a browser session")
	}
	if a.Revoke(ctx, bob, c.ID) == nil {
		t.Fatal("another user revoked the credential")
	}
	if _, _, err = a.IssueGrant(ctx, bob, "Escalate", []string{"read", "draft"}, time.Hour); err == nil {
		t.Fatal("reader gained draft scope")
	}
	if _, _, err = a.IssueGrant(ctx, alice, "Pay", []string{"approve"}, time.Hour); err == nil {
		t.Fatal("payment scope granted")
	}
	for _, ttl := range []time.Duration{0, -time.Hour, 25 * time.Hour} {
		if _, _, err = a.IssueGrant(ctx, alice, "invalid", []string{"read"}, ttl); err == nil {
			t.Fatal("invalid expiry accepted")
		}
	}
	root := a.Store.(*store.File).Dir
	files, _ := filepath.Glob(filepath.Join(root, "*.json"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if strings.Contains(string(raw), token) || strings.Contains(string(raw), strings.Split(token, ".")[1]) {
			t.Fatal("raw credential persisted")
		}
	}
	if err = a.Revoke(ctx, alice, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.Authenticate(ctx, token, "agent"); err == nil {
		t.Fatal("revoked credential accepted")
	}
	token, _, _ = a.IssueGrant(ctx, alice, "Expires", []string{"read"}, time.Second)
	a.Now = func() time.Time { return time.Now().Add(2 * time.Second) }
	if _, _, err = a.Authenticate(ctx, token, "agent"); err == nil {
		t.Fatal("expired credential accepted")
	}
}
func TestBindingsAreIssuerScopedAndCannotReuseAnotherMerchant(t *testing.T) {
	a := testAccess(t)
	if _, err := a.Subject("unknown"); err == nil {
		t.Fatal("unassigned user allowed")
	}
	if userID(a.Issuer, "alice") == userID("https://other.example", "alice") {
		t.Fatal("cross-issuer subject collision")
	}
	if _, err := NewAccess(a.Store, a.Issuer, "shop-one", "https://commerce.example", []Binding{{"alice", "other-shop", "operator"}}); err == nil {
		t.Fatal("another merchant shares configured PayPal provider")
	}
	alice, _ := a.Subject("alice")
	token, _, _ := a.IssueGrant(context.Background(), alice, "Revoked membership", []string{"read"}, time.Hour)
	delete(a.Bindings, alice.UserID)
	if _, _, err := a.Authenticate(context.Background(), token, "agent"); err == nil {
		t.Fatal("removed membership still works")
	}
}
func TestConcurrentGrantsDoNotLoseRevocations(t *testing.T) {
	a := testAccess(t)
	ctx := context.Background()
	p, _ := a.Subject("alice")
	token, c, _ := a.IssueGrant(ctx, p, "Old", []string{"read"}, time.Hour)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := a.IssueGrant(ctx, p, "New", []string{"read"}, time.Hour)
			errs <- err
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); errs <- a.Revoke(ctx, p, c.ID) }()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := a.Authenticate(ctx, token, "agent"); err == nil {
		t.Fatal("concurrent create resurrected revoked credential")
	}
	list, err := a.Grants(ctx, p)
	if err != nil || len(list) != 4 {
		t.Fatal("lost updates", len(list), err)
	}
}

func TestCredentialCannotMoveToAnotherServiceOrMerchant(t *testing.T) {
	a := testAccess(t)
	ctx := context.Background()
	p, _ := a.Subject("alice")
	token, c, _ := a.IssueGrant(ctx, p, "Bound", []string{"read"}, time.Hour)
	other, err := NewAccess(a.Store, a.Issuer, "shop-one", "https://another-service.example", []Binding{{"alice", "shop-one", "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = other.Authenticate(ctx, token, "agent"); err == nil {
		t.Fatal("credential crossed resource audience")
	}
	otherUser, _ := other.Subject("alice")
	if other.Revoke(ctx, otherUser, c.ID) == nil {
		t.Fatal("another service revoked this credential")
	}
	list, err := other.Grants(ctx, otherUser)
	if err != nil || len(list) != 0 {
		t.Fatal("another service listed this credential")
	}
	other, err = NewAccess(a.Store, a.Issuer, "shop-two", a.Audience, []Binding{{"alice", "shop-two", "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = other.Authenticate(ctx, token, "agent"); err == nil {
		t.Fatal("credential crossed merchant after reconfiguration")
	}
}
