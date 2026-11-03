package mcpauth

import (
	"context"
	"github.com/artheras/aria-plugins/packages/accounts"
	"github.com/artheras/aria-plugins/packages/store"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Broker, accounts.Principal) {
	t.Helper()
	s, _ := store.NewFile(t.TempDir())
	a, e := accounts.NewAccess(s, "https://accounts.google.com", "shop-one", "https://aria.example", []accounts.Binding{{Subject: "alice", MerchantID: "shop-one", Role: "operator"}, {Subject: "bob", MerchantID: "shop-one", Role: "reader"}})
	if e != nil {
		t.Fatal(e)
	}
	p, _ := a.Subject("alice")
	return &Broker{Access: a, Store: s, Origin: "https://aria.example", Secret: strings.Repeat("x", 32)}, p
}
func authorize(t *testing.T, b *Broker, p accounts.Principal) (string, Client, string) {
	t.Helper()
	ctx := context.Background()
	c, e := b.Register(ctx, "Test app", []string{"https://claude.ai/api/mcp/auth_callback"})
	if e != nil {
		t.Fatal(e)
	}
	proof := strings.Repeat("a", 43)
	id, e := b.Begin(ctx, Request{ClientID: c.ID, Redirect: c.Redirects[0], State: "client-state", Challenge: pkce(proof), Resource: b.Resource(), Scopes: []string{"read", "draft", "offline_access"}}, "browser")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = b.Pending(ctx, id, "other-browser"); e == nil {
		t.Fatal("unbound consent")
	}
	_, code, e := b.Consent(ctx, id, "browser", p, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = b.Consent(ctx, id, "browser", p, true); e == nil {
		t.Fatal("consent reused")
	}
	return code, c, proof
}
func exchange(t *testing.T, b *Broker, code string, c Client, proof string) Tokens {
	t.Helper()
	out, e := b.Token(context.Background(), "authorization_code", code, c.ID, c.Redirects[0], b.Resource(), proof, "")
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestAuthorizationBindingsRotationRevocation(t *testing.T) {
	b, p := fixture(t)
	ctx := context.Background()
	code, c, proof := authorize(t, b, p)
	for _, v := range []struct{ client, redirect, resource, proof string }{{"wrong", c.Redirects[0], b.Resource(), proof}, {c.ID, "https://evil.example", b.Resource(), proof}, {c.ID, c.Redirects[0], "https://other/mcp", proof}, {c.ID, c.Redirects[0], b.Resource(), strings.Repeat("b", 43)}} {
		if _, e := b.Token(ctx, "authorization_code", code, v.client, v.redirect, v.resource, v.proof, ""); e == nil {
			t.Fatal("bad code binding accepted")
		}
	}
	first := exchange(t, b, code, c, proof)
	got, g, e := b.Authenticate(ctx, first.Access)
	if e != nil || got.UserID != p.UserID || !got.Allows("draft") {
		t.Fatal("access failed", e)
	}
	if _, _, e = b.Authenticate(ctx, first.Refresh); e == nil {
		t.Fatal("refresh accepted as access")
	}
	if _, _, e = b.Access.Authenticate(ctx, first.Access, "agent"); e == nil {
		t.Fatal("MCP token accepted by REST")
	}
	second, e := b.Token(ctx, "refresh_token", first.Refresh, c.ID, "", b.Resource(), "", "")
	if e != nil || second.Refresh == first.Refresh {
		t.Fatal("rotation failed", e)
	}
	forged := strings.Replace(first.Refresh, ".1.", ".0.", 1)
	if _, e = b.Token(ctx, "refresh_token", forged, c.ID, "", b.Resource(), "", ""); e == nil {
		t.Fatal("forgery accepted")
	}
	if _, _, e = b.Authenticate(ctx, second.Access); e != nil {
		t.Fatal("forgery revoked legitimate access")
	}
	if _, e = b.Token(ctx, "refresh_token", first.Refresh, c.ID, "", b.Resource(), "", ""); e == nil {
		t.Fatal("replay accepted")
	}
	if _, _, e = b.Authenticate(ctx, second.Access); e == nil {
		t.Fatal("replayed family remains usable")
	}
	code, c, proof = authorize(t, b, p)
	out := exchange(t, b, code, c, proof)
	_, g, e = b.Authenticate(ctx, out.Access)
	if e != nil {
		t.Fatal(e)
	}
	bob, _ := b.Access.Subject("bob")
	if e = b.Revoke(ctx, bob, g.ID); e == nil {
		t.Fatal("cross-user revocation")
	}
	if e = b.Revoke(ctx, p, g.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e = b.Authenticate(ctx, out.Access); e == nil {
		t.Fatal("revoked access accepted")
	}
	if _, e = b.Token(ctx, "refresh_token", out.Refresh, c.ID, "", b.Resource(), "", ""); e == nil {
		t.Fatal("revoked renewal accepted")
	}
}
func TestReaderExpiryMembershipAndResourceIsolation(t *testing.T) {
	b, _ := fixture(t)
	p, _ := b.Access.Subject("bob")
	code, c, proof := authorize(t, b, p)
	out := exchange(t, b, code, c, proof)
	ctx := context.Background()
	got, _, e := b.Authenticate(ctx, out.Access)
	if e != nil || got.Allows("draft") || out.Scope != "read offline_access" {
		t.Fatal("reader elevated", out.Scope, e)
	}
	other := *b
	other.Origin = "https://other.example"
	if _, _, e = other.Authenticate(ctx, out.Access); e == nil {
		t.Fatal("wrong audience accepted")
	}
	if _, e = b.Token(ctx, "refresh_token", out.Refresh, c.ID, "", b.Resource(), "", "read draft offline_access"); e == nil {
		t.Fatal("scope elevation")
	}
	b.Now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, _, e = b.Authenticate(ctx, out.Access); e == nil {
		t.Fatal("expired access accepted")
	}
	renewed, e := b.Token(ctx, "refresh_token", out.Refresh, c.ID, "", b.Resource(), "", "")
	if e != nil {
		t.Fatal(e)
	}
	delete(b.Access.Bindings, p.UserID)
	if _, _, e = b.Authenticate(ctx, renewed.Access); e == nil {
		t.Fatal("removed membership accepted")
	}
}
func TestCodeReuseRevokesFamilyAndMetadataValidation(t *testing.T) {
	b, p := fixture(t)
	code, c, proof := authorize(t, b, p)
	out := exchange(t, b, code, c, proof)
	ctx := context.Background()
	if _, e := b.Token(ctx, "authorization_code", code, c.ID, c.Redirects[0], b.Resource(), proof, ""); e == nil {
		t.Fatal("code reused")
	}
	if _, _, e := b.Authenticate(ctx, out.Access); e == nil {
		t.Fatal("code replay did not revoke")
	}
	for _, u := range []string{"https://evil.example/callback", "https://claude.ai.evil.example/api/mcp/auth_callback", "https://user@claude.ai/api/mcp/auth_callback", "http://localhost/callback", "https://claude.ai/api/mcp/auth_callback#fragment", "https://claude.ai/api/mcp/auth_callback?next=evil"} {
		if _, e := b.Register(ctx, "app", []string{u}); e == nil {
			t.Fatal("unsafe redirect", u)
		}
	}
	for _, u := range []string{"http://127.0.0.1:1234/callback", "https://chatgpt.com/connector_platform_oauth_redirect", "https://chatgpt.com/connector/oauth/test_id"} {
		if _, e := b.Register(ctx, "app", []string{u}); e != nil {
			t.Fatal("valid redirect denied", u)
		}
	}
}
