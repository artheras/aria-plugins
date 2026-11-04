package mcpauth

import (
	"github.com/artheras/aria-plugins/packages/accounts"
	"slices"
	"testing"
)

func TestPayPalReadsNeedNewExplicitClientConsent(t *testing.T) {
	b := &Broker{}
	p := accounts.Principal{Scopes: []string{"read", "draft"}}
	old := b.effective(p, []string{"read", "draft", "offline_access"})
	if slices.Contains(old, "paypal.read") {
		t.Fatal("old consent expanded")
	}
	if !validScopes([]string{"read", "paypal.read"}) || !slices.Contains(b.effective(p, []string{"read", "paypal.read"}), "paypal.read") {
		t.Fatal("new explicit consent rejected")
	}
	if slices.Contains(b.effective(accounts.Principal{}, []string{"read", "paypal.read"}), "paypal.read") {
		t.Fatal("membership permission bypass")
	}
}
