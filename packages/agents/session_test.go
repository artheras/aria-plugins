package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/sdk"
)

func signIn(t *testing.T, home, token string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"auth_token":"`+token+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionTokenFollowsAriaHomeAndRequiresLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARIA_HOME", home)
	if _, err := AriaSessionToken(); err == nil || !strings.Contains(err.Error(), "/login") {
		t.Fatal("signed-out user was not told to log in", err)
	}
	signIn(t, home, "tok-1")
	if got, err := AriaSessionToken(); err != nil || got != "tok-1" {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"auth_token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AriaSessionToken(); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestGatewayReadsTheSessionOnEveryCall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARIA_HOME", home)
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"sku\":\"TSH-001\",\"quantity\":2}],\"summary\":\"ok\"}"}}]}`))
	}))
	defer server.Close()
	catalog := []sdk.Product{{SKU: "TSH-001", Name: "Cotton tee", Currency: "USD", UnitAmount: 2400}}
	g := Gateway{BaseURL: server.URL, Model: "m", KeyFunc: AriaSessionToken}

	if _, err := g.Draft(context.Background(), "2 tees", catalog); err == nil {
		t.Fatal("drafted without a login")
	}
	if len(seen) != 0 {
		t.Fatal("a request was sent with no credential")
	}
	signIn(t, home, "first")
	if _, err := g.Draft(context.Background(), "2 tees", catalog); err != nil {
		t.Fatal(err)
	}
	signIn(t, home, "refreshed") // /login refreshes the token in the same file
	if _, err := g.Draft(context.Background(), "2 tees", catalog); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "Bearer first" || seen[1] != "Bearer refreshed" {
		t.Fatal("token was cached instead of re-read", seen)
	}
}
