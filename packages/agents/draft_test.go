package agents

import (
	"context"
	"encoding/json"
	"github.com/artheras/aria-plugins/packages/sdk"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDemoAndGatewayBoundary(t *testing.T) {
	catalog := []sdk.Product{{SKU: "TSH-001", UnitAmount: 2400, Currency: "USD"}, {SKU: "MUG-003", UnitAmount: 1200, Currency: "USD"}}
	draft, err := (Demo{}).Draft(context.Background(), "2 cotton tees and 1 ceramic mug", catalog)
	if err != nil || len(draft.Items) != 2 {
		t.Fatal(draft, err)
	}
	response := `{"items":[{"sku":"TSH-001","quantity":2}],"summary":"Two tees"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer testing" {
			t.Error("invalid AI API request")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": response}}}})
	}))
	defer server.Close()
	g := Gateway{BaseURL: server.URL, Key: "testing", Model: "test/model", Client: server.Client()}
	if _, err = g.Draft(context.Background(), "two tees", catalog); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"items":[{"sku":"INVENTED","quantity":1}],"summary":"x"}`,
		`{"items":[{"sku":"TSH-001","quantity":1,"price":1}],"summary":"x"}`,
		`{"items":[{"sku":"TSH-001","quantity":1.5}],"summary":"x"}`,
		`{"items":[],"summary":"unclear"}`,
		`{"items":[{"sku":"TSH-001","quantity":1}],"summary":"x"} {}`,
	} {
		response = invalid
		if _, err = g.Draft(context.Background(), "malicious request", catalog); err == nil {
			t.Fatal("accepted unsafe AI draft", invalid)
		}
	}
}
