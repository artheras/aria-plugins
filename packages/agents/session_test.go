package agents

import (
	"context"
	"encoding/json"
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

// A real model chose to return the summary as an object. The order was right,
// and only the commentary was in the wrong shape, so the draft must survive
// while items stay strictly validated against the catalogue.
func TestGatewayAcceptsANonStringSummaryButStillValidatesItems(t *testing.T) {
	catalog := []sdk.Product{{SKU: "TSH-001", Name: "Cotton tee", Currency: "USD", UnitAmount: 2400}}
	reply := func(content string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
			w.Write(body)
		}))
	}
	good := reply(`{"items":[{"sku":"TSH-001","quantity":2}],"summary":{"total_amount":4800}}`)
	defer good.Close()
	draft, err := Gateway{BaseURL: good.URL, Model: "m", Key: "k"}.Draft(context.Background(), "2 tees", catalog)
	if err != nil || len(draft.Items) != 1 || draft.Items[0].Quantity != 2 || draft.Summary == "" {
		t.Fatal("a correct order with an object summary was rejected", draft, err)
	}
	invented := reply(`{"items":[{"sku":"NOPE-9","quantity":1}],"summary":"x"}`)
	defer invented.Close()
	if _, err = (Gateway{BaseURL: invented.URL, Model: "m", Key: "k"}).Draft(context.Background(), "x", catalog); err == nil {
		t.Fatal("an invented SKU was accepted")
	}
	extra := reply(`{"items":[{"sku":"TSH-001","quantity":1}],"summary":"ok","price":1}`)
	defer extra.Close()
	if _, err = (Gateway{BaseURL: extra.URL, Model: "m", Key: "k"}).Draft(context.Background(), "x", catalog); err == nil {
		t.Fatal("an unknown field (a price the model made up) was accepted")
	}
}
