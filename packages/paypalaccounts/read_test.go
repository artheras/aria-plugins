package paypalaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type sourceFunc func(context.Context, string) (Grant, error)

func (f sourceFunc) Resolve(c context.Context, u string) (Grant, error) { return f(c, u) }
func grant(user string) Grant {
	return Grant{UserID: user, MerchantID: "merchant-" + user, Environment: "sandbox", AccessToken: "secret-" + user, Capabilities: []string{"invoices.read"}, VerifiedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
}
func TestReadsOnlyResolvedUserAndProjectsProviderFields(t *testing.T) {
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v2/invoicing/invoices" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "10" {
			t.Error("unexpected provider call")
		}
		user := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer secret-")
		if user != "alice" && user != "bob" {
			t.Error("wrong grant")
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "INV-" + user, "status": "SENT", "amount": map[string]string{"value": "12.30", "currency_code": "USD"}, "primary_recipients": []string{"private@example.com"}}}})
	}))
	defer provider.Close()
	reader := New(sourceFunc(func(ctx context.Context, u string) (Grant, error) { return grant(u), nil }))
	reader.base = provider.URL
	for _, user := range []string{"alice", "bob"} {
		page, e := reader.Invoices(context.Background(), user, 2, 10)
		if e != nil || page.Items[0].ID != "INV-"+user || page.MerchantID != "merchant-"+user || page.Source != "paypal_invoicing_v2" || page.RetrievedAt.IsZero() {
			t.Fatal(page, e)
		}
		raw, _ := json.Marshal(page)
		if strings.Contains(string(raw), "secret-") || strings.Contains(string(raw), "private@example.com") {
			t.Fatal("private provider fields leaked")
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestUnverifiedExpiredCrossUserOrRevokedGrantsNeverRead(t *testing.T) {
	for _, kind := range []string{"missing", "cross-user", "expired", "unverified", "identity-only", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			reader := New(sourceFunc(func(context.Context, string) (Grant, error) {
				g := grant("alice")
				switch kind {
				case "missing", "revoked":
					return Grant{}, ErrNotConnected
				case "cross-user":
					g.UserID = "bob"
				case "expired":
					g.ExpiresAt = time.Now()
				case "unverified":
					g.VerifiedAt = time.Time{}
				case "identity-only":
					g.Capabilities = nil
				}
				return g, nil
			}))
			reader.base = "http://127.0.0.1:1"
			_, e := reader.Invoices(context.Background(), "alice", 1, 20)
			if !errors.Is(e, ErrNotConnected) && !errors.Is(e, ErrPermission) {
				t.Fatal(e)
			}
		})
	}
	var reader *Reader
	status, e := reader.Status(context.Background(), "alice")
	if e != nil || status.Connected || status.State != "not_connected" {
		t.Fatal(status, e)
	}
}
func TestProviderRedirectDenialAndErrorsNeverLeak(t *testing.T) {
	for _, code := range []int{301, 401, 403, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://127.0.0.1:1/steal")
				w.WriteHeader(code)
				w.Write([]byte("sensitive provider error"))
			}))
			defer provider.Close()
			reader := New(sourceFunc(func(context.Context, string) (Grant, error) { return grant("alice"), nil }))
			reader.base = provider.URL
			_, e := reader.Invoices(context.Background(), "alice", 1, 20)
			if e == nil || strings.Contains(e.Error(), "sensitive") || strings.Contains(e.Error(), "steal") {
				t.Fatal(e)
			}
		})
	}
	reader := New(nil)
	if _, e := reader.Invoices(context.Background(), "alice", -1, 1000); !errors.Is(e, ErrPage) {
		t.Fatal(e)
	}
}
