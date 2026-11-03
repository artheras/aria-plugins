package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogWorksForAccountReader(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" {
			w.Write([]byte(`{"authenticated":true,"merchant":false,"account":{"scopes":["read"]}}`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer s.Close()
	b := bridge{base: s.URL, token: "test", client: s.Client()}
	out := b.call(json.RawMessage(`{"name":"commerce_catalog","arguments":{}}`))
	raw, _ := json.Marshal(out)
	if string(raw) == "null" {
		t.Fatal("missing result")
	}
	var v struct {
		IsError bool `json:"isError"`
	}
	json.Unmarshal(raw, &v)
	if v.IsError {
		t.Fatal(string(raw))
	}
}
